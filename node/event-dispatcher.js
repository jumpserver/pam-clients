'use strict'

const { AsyncLocalStorage } = require('node:async_hooks')
const { performance } = require('node:perf_hooks')
const activeHook = new AsyncLocalStorage()
const END = Symbol('end')
const IDLE = Symbol('idle')

class EventQueue {
  items = []
  ended = false
  reader = undefined
  space = undefined

  async put(event) {
    while (!this.ended && this.items.length >= 128)
      await new Promise((resolve) => { this.space = resolve })
    if (this.ended) return false
    this.items.push(event)
    this.reader?.()
    return true
  }

  async next() {
    if (!this.items.length && !this.ended) {
      let timer
      try {
        await new Promise((resolve) => {
          this.reader = resolve
          timer = setTimeout(resolve, 100)
        })
      } finally {
        clearTimeout(timer)
        this.reader = undefined
      }
    }
    if (this.items.length) {
      const event = this.items.shift()
      this.space?.()
      this.space = undefined
      return event
    }
    return this.ended ? END : IDLE
  }

  end() {
    this.ended = true
    this.reader?.()
    this.space?.()
  }
}

class EventSubscription {
  #client
  #stop = new AbortController()
  #signal
  #queue = new EventQueue()
  #pending = new Map()
  #revocations = new Map()
  #readerError
  #accountEvents
  #applied = new Map()
  #used = new Map()
  #scope
  running = true

  constructor(client, { signal, clientSignal }) {
    this.#client = client
    this.#accountEvents = client._usesAccountEvents?.() === true
    this.#signal = AbortSignal.any([this.#stop.signal, clientSignal, ...(signal ? [signal] : [])])
    this.done = this.#run()
    // A background failure remains observable via done without becoming an
    // unhandled rejection before the application reaches its shutdown path.
    this.done.catch(() => {})
  }

  stop() {
    this.#stop.abort()
    return activeHook.getStore() === this ? Promise.resolve() : this.done
  }

  async #call(handler, value) {
    return activeHook.run(this, () => handler.call(this.#client, value, { signal: this.#signal }))
  }

  async #report(error, event) {
    if (this.#signal.aborted) return
    try {
      await activeHook.run(this, () => this.#client.onEventError(error, event))
    } catch (hookError) {
      console.warn('Credential event error hook failed:', hookError?.constructor?.name || 'Error')
    }
  }

  async #read() {
    let stream
    try {
      stream = this.#client.watchCredentialEvents({ signal: this.#signal })
      for await (const event of stream) {
        if (this.#signal.aborted || !(await this.#queue.put(event))) break
      }
    } catch (error) {
      if (!this.#signal.aborted) this.#readerError = error
    } finally {
      try {
        await stream?.return?.()
      } catch (error) {
        if (!this.#signal.aborted) this.#readerError = error
      }
      this.#queue.end()
    }
  }

  #selector(update) {
    const key = update.credentialKey || update.key
    if (update.credentialMode === 'subscription' && typeof key === 'string' && key
      && typeof update.accountId === 'string' && update.accountId)
      return { key: key.endsWith(`:${update.accountId}`) ? key : `${key}:${update.accountId}` }
    if (update.credentialMode === 'alternating_rotation' && typeof key === 'string' && key)
      return { key }
  }

  async #refresh(update, event, delay = 0) {
    if (this.#signal.aborted) return
    if (this.#accountEvents) return this.#refreshAccount(update, event, delay)
    const selector = this.#selector(update)
    if (!selector) return
    const target = selector.accountId ? `account:${selector.accountId}` : `key:${selector.key}`
    const previous = this.#pending.get(target)
    if (Number.isSafeInteger(previous?.update.revision) && Number.isSafeInteger(update.revision)
      && previous.update.revision > update.revision) return
    try {
      const credential = await this.#client.getCredential({ ...selector, signal: this.#signal, allowLocalFallback: false })
      if (Number.isSafeInteger(update.revision) && credential.revision < update.revision)
        throw new Error('Credential API revision is behind the event')
      if (this.#signal.aborted) return
      await this.#call(this.#client.onCredentialChanged, credential)
      this.#pending.delete(target)
    } catch (error) {
      if (this.#signal.aborted) return
      await this.#report(error, event)
      delay = Math.min(Math.max(1000, delay * 2), 30000)
      this.#pending.set(target, { update, event, delay, due: performance.now() + delay })
    }
  }

  #accountSelector(update) {
    if (update.commandId) return ['credential.updated', 'credential.switch.requested', 'application.restart.requested'].includes(update.event)
      ? `command:${update.commandId}` : undefined
    if (update.credentialMode === 'alternating_rotation') {
      const key = update.credentialKey || update.key
      return key ? `policy:${key}` : undefined
    }
    if (update.credentialMode === 'subscription' && update.accountId) return `account:${update.accountId}`
  }

  #remember(eventId, result) {
    this.#applied.set(eventId, result)
    if (this.#applied.size > 256) this.#applied.delete(this.#applied.keys().next().value)
  }

  async #reportUsage() {
    try { await this.#client.reportAccounts([...new Set(this.#used.values())].sort(), { signal: this.#signal }) }
    catch { /* Telemetry must not repeat or fail a business action. */ }
  }

  async #refreshAccount(update, event, delay = 0) {
    const target = this.#accountSelector(update)
    if (!target || !update.eventId) return
    const usageTarget = this.#accountSelector({ ...update, commandId: undefined })
    if (update.event !== 'application.restart.requested' && this.#scope && !this.#scope.has(usageTarget)) return
    const previous = this.#pending.get(target)
    if (Number.isSafeInteger(previous?.update.revision) && previous.update.revision > update.revision) return
    try {
      let result = this.#applied.get(update.eventId)
      if (!result) {
        if (update.commandId) {
          const claim = await this.#client.reportApplicationCommandResult({ commandId: update.commandId, status: 'running', signal: this.#signal })
          if (!claim.accepted) { this.#pending.delete(target); return }
        }
        try {
          if (update.event === 'application.restart.requested')
            await activeHook.run(this, () => this.#client.onApplicationRestart({ signal: this.#signal }))
          else {
            if (!['subscription', 'alternating_rotation'].includes(update.credentialMode)) throw new Error('Unsupported credential mode')
            const account = await this.#client.getAccount({ accountId: update.accountId, allowLocalFallback: false, signal: this.#signal })
            if (account.id !== update.accountId || !Number.isSafeInteger(update.accountRevision) || account.revision !== update.accountRevision)
              throw new Error('Event account version is superseded')
            if (this.#signal.aborted) return
            await this.#call(this.#client.onAccountChanged, account)
            this.#used.set(this.#accountSelector({ ...update, commandId: undefined }), update.accountId)
          }
        } catch (error) {
          if (update.commandId) this.#remember(update.eventId, { status: 'failed', errorCode: 'application_failed' })
          else {
            try { await this.#client.confirmEvent({ eventId: update.eventId, status: 'failed', errorCode: 'application_failed', signal: this.#signal }) }
            catch { /* Preserve the original business error. */ }
          }
          throw error
        }
        result = { status: 'success', errorCode: '' }
        this.#remember(update.eventId, result)
      }
      if (this.#signal.aborted) return
      await this.#client.confirmEvent({ eventId: update.eventId, ...result, signal: this.#signal })
      this.#pending.delete(target)
      await this.#reportUsage()
    } catch (error) {
      if (this.#signal.aborted) return
      await this.#report(error, event)
      delay = Math.min(Math.max(1000, delay * 2), 30000)
      this.#pending.set(target, { update, event, delay, due: performance.now() + delay })
    }
  }

  async #revokeAccount(target, accountId, event, delay = 0) {
    if (this.#signal.aborted) return
    try {
      await this.#call(this.#client.onAccountRevoked, accountId)
      this.#revocations.delete(target)
      this.#used.delete(target)
      this.#applied.clear()
      await this.#reportUsage()
    } catch (error) {
      if (this.#signal.aborted) return
      await this.#report(error, event)
      delay = Math.min(Math.max(1000, delay * 2), 30000)
      this.#revocations.set(target, { target, accountId, event, delay, due: performance.now() + delay })
    }
  }

  async #revokeAccounts(retained, event) {
    for (const target of this.#revocations.keys())
      if (retained.has(target)) this.#revocations.delete(target)
    for (const [target, accountId] of this.#used) {
      if (retained.has(target) || this.#revocations.has(target)) continue
      await this.#revokeAccount(target, accountId, event)
    }
    await this.#reportUsage()
  }

  async #dispatch(event) {
    if (['snapshot', 'credential.revoked', 'application.scope.updated', 'configuration.updated'].includes(event.event))
      this.#pending.clear()
    try {
      await this.#call(this.#client.onEvent, event)
    } catch (error) {
      await this.#report(error, event)
    }
    if (this.#signal.aborted) return
    if (event.event === 'snapshot') {
      if (this.#accountEvents) {
        this.#scope = new Set((event.credentials || []).map(update => this.#accountSelector(update)))
        const retained = new Set([...this.#scope, ...(event.authorizedAccountIds || []).map(id => `account:${id}`)])
        await this.#revokeAccounts(retained, event)
      }
      for (const update of event.credentials || []) {
        try {
          await this.#refresh(update, event)
        } catch (error) {
          await this.#report(error, event)
        }
      }
    } else if (this.#accountEvents && event.commandId) {
      await this.#refresh(event, event)
    } else if (event.event === 'credential.updated' && !event.commandId) {
      await this.#refresh(event, event)
    } else if (event.event === 'credential.revoked') {
      if (this.#accountEvents) {
        const key = event.credentialKey || event.key
        this.#scope?.delete(`policy:${key}`)
        this.#scope?.delete(`account:${event.accountId}`)
        await this.#revokeAccounts(new Set([...this.#used].filter(([target, id]) => id !== event.accountId && target !== `policy:${key}`).map(([target]) => target)), event)
      }
      try {
        await this.#call(this.#client.onCredentialRevoked, event)
      } catch (error) {
        await this.#report(error, event)
      }
    }
  }

  async #retry() {
    let earliest
    for (const item of this.#pending.values())
      if (!earliest || item.due < earliest.due) earliest = item
    let cleanup
    for (const item of this.#revocations.values())
      if (!cleanup || item.due < cleanup.due) cleanup = item
    if (cleanup && (!earliest || cleanup.due <= earliest.due)) {
      if (cleanup.due <= performance.now())
        await this.#revokeAccount(cleanup.target, cleanup.accountId, cleanup.event, cleanup.delay)
      return
    }
    if (earliest && earliest.due <= performance.now())
      await this.#refresh(earliest.update, earliest.event, earliest.delay)
  }

  async #run() {
    const abort = () => this.#queue.end()
    this.#signal.addEventListener('abort', abort, { once: true })
    if (this.#signal.aborted) abort()
    const reader = this.#read()
    try {
      while (!this.#signal.aborted) {
        const event = await this.#queue.next()
        if (this.#signal.aborted) break
        if (event === END) {
          if (this.#readerError) {
            await this.#report(this.#readerError, undefined)
            throw this.#readerError
          }
          break
        }
        if (event !== IDLE) {
          try {
            await this.#dispatch(event)
          } catch (error) {
            await this.#report(error, event)
          }
        }
        await this.#retry()
      }
    } finally {
      this.#stop.abort()
      await reader
      this.#signal.removeEventListener('abort', abort)
      this.#pending.clear()
      this.#revocations.clear()
      this.#queue.items.length = 0
      this.running = false
    }
  }
}

module.exports = { EventSubscription }
