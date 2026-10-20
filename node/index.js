'use strict'

const crypto = require('node:crypto')
const { inspect } = require('node:util')
const { performance } = require('node:perf_hooks')
const { setTimeout: wait } = require('node:timers/promises')
const { WebSocket, createWebSocketStream } = require('ws')
const { EventSubscription } = require('./event-dispatcher')

const VERSION = '1.0.2'
const PROTOCOL_VERSION = 1
const CONFIG_SCHEMA_VERSION = 1
const CLIENT_PATH = '/api/v1/accounts/credential-client'
const SIGNATURE_HEADERS = [
  '(request-target)',
  'accept',
  'date',
  'digest',
  'x-jms-request-id',
  'x-jms-org',
  'x-jms-client-version',
  'x-jms-protocol-version',
  'x-jms-config-schema-version',
]

class PAMError extends Error {
  constructor(code, message, { statusCode, detail, requestId, cause } = {}) {
    super(`[${code}] ${detail || message}`, { cause })
    this.name = 'PAMError'
    this.code = code
    this.statusCode = statusCode
    this.detail = detail
    this.requestId = requestId
  }

  get isCompatibilityError() {
    if ([401, 403, 404].includes(this.statusCode)) return false
    return this.statusCode === 426 || this.code === 'client_upgrade_required'
  }
}

function requiredString(value, name) {
  if (typeof value !== 'string' || !value) throw new TypeError(`${name} must be a non-empty string`)
  return value
}

function revision(value, minimum = 0) {
  if (!Number.isSafeInteger(value) || value < minimum) throw new TypeError('Invalid revision')
  return value
}

function object(value) {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new TypeError('Expected an object')
  return value
}

function camelize(value) {
  if (Array.isArray(value)) return value.map(camelize)
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(
    Object.entries(value).map(([key, item]) => [
      key.replace(/_([a-z])/g, (_, letter) => letter.toUpperCase()),
      camelize(item),
    ]),
  )
}

function credential(data) {
  object(data)
  for (const name of ['key']) requiredString(data[name], name)
  revision(data.revision)
  for (const [item, names] of [
    [data.asset, ['id', 'name', 'address']],
    [data.asset?.platform, ['id', 'name', 'category', 'type']],
    [data.account, ['id', 'name', 'username', 'secret_type', 'secret']],
  ]) {
    object(item)
    for (const name of names) requiredString(item[name], name)
  }
  if (data.account.revision !== undefined) revision(data.account.revision)
  const result = camelize(data)
  result.fromLocal = false
  Object.defineProperty(result.account, inspect.custom, {
    value: () => ({ ...result.account, secret: '[REDACTED]' }),
  })
  return result
}

function copyCredential(value, fromLocal) {
  const result = structuredClone(value)
  result.fromLocal = fromLocal
  Object.defineProperty(result.account, inspect.custom, {
    value: () => ({ ...result.account, secret: '[REDACTED]' }),
  })
  return result
}

function commandResult(data) {
  if (typeof data.accepted !== 'boolean') throw new TypeError('accepted must be a boolean')
  requiredString(data.status, 'status')
  return camelize(data)
}

function authorizedAccount(data) {
  object(data)
  for (const name of ['id', 'name', 'secret_type']) requiredString(data[name], name)
  if (typeof data.username !== 'string') throw new TypeError('username must be a string')
  for (const [item, names] of [
    [data.asset, ['id', 'name', 'address']],
    [data.asset?.platform, ['id', 'name', 'category', 'type']],
  ]) {
    object(item)
    for (const name of names) requiredString(item[name], name)
  }
  if (!Array.isArray(data.credentials)) throw new TypeError('Invalid account policies')
  const credentials = data.credentials.map((policy) => {
    object(policy)
    requiredString(policy.key, 'key')
    if (!['subscription', 'alternating_rotation'].includes(policy.mode))
      throw new TypeError('Invalid account policy mode')
    revision(policy.revision)
    const result = { key: policy.key, mode: policy.mode, revision: policy.revision }
    if (policy.account_switch != null) {
      object(policy.account_switch)
      const ids = policy.account_switch.account_ids
      if (!Array.isArray(ids)) throw new TypeError('Invalid account switch')
      ids.forEach((id) => requiredString(id, 'accountId'))
      result.accountSwitch = { accountIds: [...ids] }
    }
    return result
  })
  // Explicit metadata projection keeps secrets and unknown fields out of listings.
  const { id, name, username, secret_type: secretType } = data
  const { id: assetId, name: assetName, address, platform } = data.asset
  const asset = { id: assetId, name: assetName, address,
    platform: { id: platform.id, name: platform.name, category: platform.category, type: platform.type } }
  return { id, name, username, secretType, asset, credentials }
}

function applicationInfo(data) {
  object(data)
  for (const name of ['id', 'name', 'org_id']) requiredString(data[name], name)
  if (typeof data.is_active !== 'boolean') throw new TypeError('Invalid application status')
  revision(data.accounts_count)
  revision(data.policies_count)
  return { id: data.id, name: data.name, orgId: data.org_id, isActive: data.is_active,
    accountsCount: data.accounts_count, policiesCount: data.policies_count }
}

function clientInfo(data) {
  object(data)
  for (const name of ['id', 'instance_id', 'client_version']) requiredString(data[name], name)
  if (!['sdk', 'agent'].includes(data.type) || typeof data.is_active !== 'boolean')
    throw new TypeError('Invalid client status')
  revision(data.protocol_version)
  const capabilities = data.server_capabilities ?? null
  if (capabilities !== null && (!Array.isArray(capabilities)
    || capabilities.some(value => typeof value !== 'string' || !value)
    || new Set(capabilities).size !== capabilities.length))
    throw new TypeError('Invalid server capabilities')
  if (data.config_schema_version != null) revision(data.config_schema_version)
  let applicationStatus = null
  if (data.application_status != null) {
    const report = object(data.application_status)
    if (!['starting', 'running', 'degraded', 'stopping', 'stopped', 'error'].includes(report.status) ||
        !Array.isArray(report.account_ids) || report.account_ids.some(id => typeof id !== 'string' || !id) ||
        typeof report.error_code !== 'string') throw new TypeError('Invalid application status')
    applicationStatus = { status: report.status, accountIds: [...report.account_ids], errorCode: report.error_code }
  }
  if (data.date_status_reported != null) requiredString(data.date_status_reported, 'dateStatusReported')
  return { id: data.id, instanceId: data.instance_id, type: data.type,
    isActive: data.is_active, clientVersion: data.client_version,
    protocolVersion: data.protocol_version, configSchemaVersion: data.config_schema_version ?? null,
    applicationStatus, dateStatusReported: data.date_status_reported ?? null,
    serverCapabilities: capabilities === null ? null : [...capabilities] }
}

function credentialPolicy(data) {
  object(data)
  for (const name of ['id', 'key', 'name', 'status']) requiredString(data[name], name)
  if (!['subscription', 'alternating_rotation'].includes(data.mode)
    || typeof data.is_active !== 'boolean' || typeof data.subscription_all_authorized !== 'boolean')
    throw new TypeError('Invalid policy metadata')
  revision(data.revision)
  revision(data.accounts_count)
  if (data.active_account_id != null) requiredString(data.active_account_id, 'activeAccountId')
  let accountSwitch = null
  if (data.account_switch != null) {
    const ids = object(data.account_switch).account_ids
    if (!Array.isArray(ids)) throw new TypeError('Invalid account switch')
    ids.forEach(id => requiredString(id, 'accountId'))
    accountSwitch = { accountIds: [...ids] }
  }
  return { id: data.id, key: data.key, name: data.name, mode: data.mode,
    isActive: data.is_active, status: data.status, revision: data.revision,
    accountsCount: data.accounts_count, subscriptionAllAuthorized: data.subscription_all_authorized,
    activeAccountId: data.active_account_id ?? null, accountSwitch }
}

/** One application replica. Each request and WebSocket reconnect gets a fresh signature. */
class Client {
  #options
  #closed = new AbortController()
  #streams = new Set()
  #eventSubscription
  #latestCredentials = new Map()
  #credentialGeneration = 0

  constructor({
    endpoint,
    appId,
    appSecret,
    instanceId,
    orgId = '00000000-0000-0000-0000-000000000002',
    timeout = 10000,
    source = 'jms-pam',
  }) {
    if (Object.hasOwn(arguments[0], 'configurationId'))
      throw new TypeError('configurationId was removed; use application identity and instanceId')
    requiredString(endpoint, 'endpoint')
    const url = new URL(endpoint)
    if (
      !['http:', 'https:'].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    ) {
      throw new TypeError('endpoint must be an HTTP(S) URL without credentials, query or fragment')
    }
    requiredString(appId, 'appId')
    requiredString(appSecret, 'appSecret')
    requiredString(instanceId, 'instanceId')
    if (instanceId.trim() !== instanceId || instanceId.length > 128)
      throw new TypeError('Invalid instanceId')
    if (!Number.isSafeInteger(timeout) || timeout <= 0)
      throw new TypeError('timeout must be positive milliseconds')
    this.#options = {
      endpoint: endpoint.replace(/\/+$/, ''),
      appId,
      appSecret,
      instanceId,
      orgId,
      timeout,
      source,
    }
  }

  #url(path, params = {}) {
    const url = new URL(this.#options.endpoint + path)
    for (const [name, value] of Object.entries(params)) {
      if (value !== undefined && value !== null) url.searchParams.set(name, value)
    }
    return url
  }

  #identity(data) {
    const result = { ...data, instance_id: this.#options.instanceId }
    return result
  }

  #headers(method, url, body = '') {
    const options = this.#options
    const headers = {
      accept: 'application/json',
      date: new Date().toUTCString(),
      digest: 'SHA-256=' + crypto.createHash('sha256').update(body).digest('base64'),
      'x-jms-request-id': crypto.randomUUID(),
      'x-jms-org': options.orgId,
      'x-jms-client-version': VERSION,
      'x-jms-protocol-version': String(PROTOCOL_VERSION),
      'x-jms-config-schema-version':
        options.source === 'jms-pam-agent' ? String(CONFIG_SCHEMA_VERSION) : '0',
      'x-source': options.source,
    }
    const signing = SIGNATURE_HEADERS.map(
      (name) =>
        `${name}: ${
          name === '(request-target)'
            ? method.toLowerCase() + ' ' + url.pathname + url.search
            : headers[name]
        }`,
    ).join('\n')
    const signature = crypto
      .createHmac('sha256', options.appSecret)
      .update(signing)
      .digest('base64')
    headers.authorization = `Signature keyId="${options.appId}",algorithm="hmac-sha256",signature="${signature}",headers="${SIGNATURE_HEADERS.join(' ')}"`
    return headers
  }

  async #request(method, path, data, parse, { signal, invalidateCredentials = true } = {}) {
    if (this.#closed.signal.aborted) throw new Error('Client is closed')
    data = this.#identity(data)
    const url = this.#url(CLIENT_PATH + path, method === 'GET' ? data : {})
    const body = method === 'GET' ? undefined : JSON.stringify(data)
    const headers = this.#headers(method, url, body)
    if (body !== undefined) headers['content-type'] = 'application/json'
    const signals = [this.#closed.signal, AbortSignal.timeout(this.#options.timeout)]
    if (signal) signals.push(signal)
    let response
    try {
      response = await fetch(url, {
        method,
        headers,
        body,
        redirect: 'error',
        signal: AbortSignal.any(signals),
      })
    } catch (cause) {
      throw new PAMError('NetworkError', 'HTTP request failed', { cause })
    }
    const optionalMetadata = ['/application/', '/client/', '/policies/', '/policy/'].includes(path)
    if (invalidateCredentials && ([401, 403].includes(response.status) || (response.status === 404 && !optionalMetadata))) {
      this.#latestCredentials.clear()
      this.#credentialGeneration++
    }
    let dataObject
    try {
      dataObject = await response.json()
    } catch (cause) {
      // A timeout or dropped connection can occur after the response headers.
      const invalid = cause instanceof SyntaxError
      throw new PAMError(invalid ? 'ResponseError' : 'NetworkError',
        invalid ? 'The server returned invalid JSON' : 'HTTP response failed', {
        statusCode: response.status,
        cause,
      })
    }
    try {
      dataObject = object(dataObject)
    } catch (cause) {
      throw new PAMError('ResponseError', 'The server response must be a JSON object', {
        statusCode: response.status, cause,
      })
    }
    if (!response.ok) {
      if (invalidateCredentials && response.status === 400 && dataObject.code === 'credential_not_found') {
        this.#latestCredentials.clear(); this.#credentialGeneration++
      }
      throw new PAMError(
        typeof dataObject.code === 'string' && dataObject.code ? dataObject.code : 'HTTPError',
        'HTTP request failed',
        {
          statusCode: response.status,
          detail: typeof dataObject.detail === 'string' ? dataObject.detail : undefined,
          requestId: typeof dataObject.request_id === 'string' ? dataObject.request_id : undefined,
        },
      )
    }
    try {
      return parse(dataObject)
    } catch (cause) {
      throw new PAMError('ResponseError', 'The server returned an invalid response', {
        statusCode: response.status,
        cause,
      })
    }
  }

  getApplication({ signal } = {}) {
    return this.#request('GET', '/application/', {}, applicationInfo, { signal })
  }

  getClientInfo({ signal } = {}) {
    return this.#request('GET', '/client/', {}, clientInfo, { signal })
  }

  async reportStatus({ status, accountId, accountIds, errorCode = '', raiseOnError = false, signal } = {}) {
    if (!['starting', 'running', 'degraded', 'stopping', 'stopped', 'error'].includes(status))
      throw new TypeError('Invalid application status')
    if (typeof raiseOnError !== 'boolean') throw new TypeError('raiseOnError must be a boolean')
    if (accountId !== undefined && accountIds !== undefined) throw new TypeError('Use accountId or accountIds, not both')
    const accounts = accountId === undefined ? (accountIds ?? []) : [accountId]
    if (!Array.isArray(accounts) || accounts.length > 200 || accounts.some(id => typeof id !== 'string' || !id) || new Set(accounts).size !== accounts.length)
      throw new TypeError('Use at most 200 unique non-empty account IDs')
    if (typeof errorCode !== 'string' || errorCode.length > 128 || !/^[A-Za-z0-9_.:-]*$/.test(errorCode))
      throw new TypeError('Invalid machine-readable error code')
    try {
      if (this.#closed.signal.aborted) {
        if (raiseOnError) throw new Error('Client is closed')
        return false
      }
      return await this.#request('POST', '/status/', { status, account_ids: accounts, error_code: errorCode }, data => {
        if (typeof data.accepted !== 'boolean') throw new TypeError('Invalid status acknowledgment')
        return data.accepted
      }, { signal, invalidateCredentials: false })
    } catch (error) {
      if (raiseOnError || !(error instanceof PAMError)) throw error
      return false
    }
  }

  listPolicies({ search = '', signal } = {}) {
    if (typeof search !== 'string' || [...search].length > 128)
      throw new TypeError('search must be a string of at most 128 characters')
    return this.#request('GET', '/policies/', { search }, data => {
      if (!Array.isArray(data.policies)) throw new TypeError('Invalid policy list')
      const policies = data.policies.map(credentialPolicy)
      if (new Set(policies.map(item => item.id)).size !== policies.length)
        throw new TypeError('Duplicate policy in list')
      return policies
    }, { signal })
  }

  getPolicy({ key, policyId, signal } = {}) {
    if ((key === undefined) === (policyId === undefined))
      throw new TypeError('Provide exactly one of key or policyId')
    const data = key !== undefined ? { key: requiredString(key, 'key') }
      : { policy_id: requiredString(policyId, 'policyId') }
    if (key !== undefined && [...key].length > 64) throw new TypeError('Invalid policy key')
    return this.#request('GET', '/policy/', data, credentialPolicy, { signal })
  }

  async listAccounts({ search = '', signal } = {}) {
    const accounts = []
    const seen = new Set()
    let count
    for (;;) {
      const page = await this.listAccountsPage({ offset: accounts.length, search, signal })
      count ??= page.count
      if (page.count !== count)
        throw new PAMError('ResponseError', 'Account count changed during pagination; retry listing')
      if (!page.accounts.length && accounts.length < count)
        throw new PAMError('ResponseError', 'Incomplete account list')
      for (const account of page.accounts) {
        if (seen.has(account.id))
          throw new PAMError('ResponseError', 'Duplicate account in paginated list')
        seen.add(account.id)
        accounts.push(account)
      }
      if (accounts.length === count) return accounts
    }
  }

  listAccountsPage({ limit = 200, offset = 0, search = '', signal } = {}) {
    if (!Number.isSafeInteger(limit) || limit < 1 || limit > 500)
      throw new TypeError('limit must be an integer between 1 and 500')
    if (!Number.isSafeInteger(offset) || offset < 0)
      throw new TypeError('offset must be a non-negative integer')
    if (typeof search !== 'string' || [...search].length > 128)
      throw new TypeError('search must be a string of at most 128 characters')
    return this.#request('GET', '/accounts/', { limit, offset, search }, (data) => {
      if (!Number.isSafeInteger(data.count) || data.count < 0 || !Array.isArray(data.accounts))
        throw new TypeError('Invalid account page')
      const accounts = data.accounts.map(authorizedAccount)
      if (accounts.length > Math.min(limit, Math.max(data.count - offset, 0)))
        throw new TypeError('Account page exceeds limit or total count')
      if (new Set(accounts.map((account) => account.id)).size !== accounts.length)
        throw new TypeError('Duplicate account in page')
      return { count: data.count, accounts }
    }, { signal })
  }

  getCredential({ key, accountId, signal, allowLocalFallback = true } = {}) {
    if ((key === undefined) === (accountId === undefined))
      throw new TypeError('Exactly one of key or accountId is required')
    requiredString(key === undefined ? accountId : key, 'selector')
    if (typeof allowLocalFallback !== 'boolean') throw new TypeError('allowLocalFallback must be boolean')
    return this.#getCredential({ key, accountId, signal, allowLocalFallback })
  }

  async #getCredential({ key, accountId, signal, allowLocalFallback }) {
    const selector = key === undefined ? `account:${accountId}` : `key:${key}`
    const generation = this.#credentialGeneration
    try {
      const value = await this.#request('GET', '/credential/', { key, account_id: accountId }, credential, { signal })
      if (!this.#closed.signal.aborted && generation === this.#credentialGeneration) {
        const latest = this.#latestCredentials.get(selector)
        if (latest && value.revision < latest.revision)
          throw new PAMError('ResponseError', 'Credential revision moved backwards', { statusCode: 200 })
        this.#latestCredentials.set(selector, copyCredential(value, false))
      }
      return value
    } catch (error) {
      const denied = [401, 403, 404].includes(error.statusCode)
        || (error.statusCode === 400 && error.code === 'credential_not_found')
      const temporary = error instanceof PAMError && !error.isCompatibilityError && (error.code === 'NetworkError'
        || (error.statusCode >= 500 && error.statusCode < 600))
      if (denied) { this.#latestCredentials.clear(); this.#credentialGeneration++ }
      const latest = this.#latestCredentials.get(selector)
      if (allowLocalFallback && temporary && !denied && !signal?.aborted && !this.#closed.signal.aborted && latest)
        return copyCredential(latest, true)
      throw error
    }
  }

  #reconcileLatestCredentials(event) {
    if (!['snapshot', 'credential.revoked', 'configuration.updated'].includes(event.event)) return
    this.#credentialGeneration++
    if (event.event === 'configuration.updated') {
      for (const selector of this.#latestCredentials.keys()) if (selector.startsWith('account:')) this.#latestCredentials.delete(selector)
      return
    }
    if (event.event !== 'snapshot') {
      const rawKey = event.credentialKey || event.key
      const key = typeof rawKey === 'string' && rawKey ? rawKey : undefined
      const accountId = typeof event.accountId === 'string' && event.accountId ? event.accountId : undefined
      if (!key && !accountId) this.#latestCredentials.clear()
      else for (const [selector, credential] of this.#latestCredentials)
        if ((key && (selector === `key:${key}` || credential.key === key || credential.key.startsWith(`${key}:`)))
          || (accountId && (!key || key.startsWith('account:'))
            && (selector === `account:${accountId}` || credential.account.id === accountId)))
          this.#latestCredentials.delete(selector)
      return
    }
    if (!Array.isArray(event.credentials)) {
      this.#latestCredentials.clear()
      return
    }
    const keys = new Set(event.credentials.map((item) => item?.credentialKey || item?.key))
    for (const selector of this.#latestCredentials.keys())
      if (selector.startsWith('key:') && !keys.has(selector.slice(4)))
        this.#latestCredentials.delete(selector)
  }

  async getAccount({ accountId, signal, allowLocalFallback = true } = {}) {
    requiredString(accountId, 'accountId')
    const value = await this.getCredential({ accountId, signal, allowLocalFallback })
    const account = { ...value.account, revision: value.account.revision ?? 0, asset: value.asset, fromLocal: value.fromLocal }
    Object.defineProperty(account, inspect.custom, { value: () => ({ ...account, secret: '[REDACTED]' }) })
    return account
  }

  confirmEvent({ eventId, status = 'success', errorCode = '', signal }) {
    requiredString(eventId, 'eventId')
    if (!['success', 'failed'].includes(status)) throw new TypeError('Invalid event status')
    return this.#request('POST', '/event-result/', { event_id: eventId, status, error_code: errorCode }, (data) => {
      if (typeof data.accepted !== 'boolean' || typeof data.status !== 'string') throw new TypeError('Invalid event result')
      return camelize(data)
    }, { signal })
  }

  confirmCredential({ key, revision: value, accountId, signal }) {
    requiredString(key, 'key')
    requiredString(accountId, 'accountId')
    revision(value, 1)
    return this.#request(
      'POST',
      '/confirm/',
      { key, revision: value, account_id: accountId },
      (data) => {
        requiredString(data.key, 'key')
        revision(data.revision)
        return camelize(data)
      },
      { signal },
    )
  }

  syncAgent({
    credentials,
    deliveredCredentials,
    configDigest = '',
    syncStatus = '',
    syncError = '',
    signal,
  }) {
    const revisions = (values) => {
      if (!Array.isArray(values)) throw new TypeError('Known revisions must be an array')
      return values.map((item) => ({
        key: requiredString(item.key, 'key'),
        revision: revision(item.revision),
      }))
    }
    return this.#request(
      'POST',
      '/agent/sync/',
      {
        credentials: revisions(credentials),
        delivered_credentials: revisions(deliveredCredentials),
        config_digest: configDigest,
        sync_status: syncStatus,
        sync_error: syncError,
      },
      (data) => {
        requiredString(data.config_digest, 'config_digest')
        requiredString(data.date_last_synced, 'date_last_synced')
        if (!Array.isArray(data.credentials) || !Array.isArray(data.removed_keys))
          throw new TypeError('Invalid synchronization metadata')
        for (const item of data.credentials) {
          requiredString(item.key, 'key')
          revision(item.revision)
          if (typeof item.available !== 'boolean' || typeof item.changed !== 'boolean')
            throw new TypeError('Invalid revision flags')
        }
        if (new Set(data.credentials.map((item) => item.key)).size !== data.credentials.length)
          throw new TypeError('Duplicate revision keys')
        data.removed_keys.forEach((key) => requiredString(key, 'removed key'))
        if (data.scope !== undefined && data.scope !== null)
          object(data.scope)
        // Agent scope uses protocol field names, matching Python's scope mapping.
        return { ...camelize(data), scope: data.scope }
      },
      { signal },
    )
  }

  listApplicationCommands({ signal } = {}) {
    return this.#request(
      'GET',
      '/commands/',
      {},
      (data) => {
        if (!Array.isArray(data.commands)) throw new TypeError('commands must be an array')
        data.commands.forEach(object)
        return camelize(data.commands)
      },
      { signal },
    )
  }

  reportApplicationCommandResult({ commandId, status, errorCode, signal }) {
    requiredString(commandId, 'commandId')
    if (!['running', 'success', 'failed'].includes(status))
      throw new TypeError('Invalid command status')
    return this.#request(
      'POST',
      '/command-result/',
      { command_id: commandId, status, error_code: errorCode },
      commandResult,
      { signal },
    )
  }

  async executeApplicationCommand(event, handler, options = {}) {
    const claim = await this.reportApplicationCommandResult({
      commandId: event.commandId,
      status: 'running',
      ...options,
    })
    if (!claim.accepted) return claim
    try {
      await handler(event)
    } catch (error) {
      try {
        await this.reportApplicationCommandResult({
          commandId: event.commandId,
          status: 'failed',
          errorCode: 'execution_failed',
          ...options,
        })
      } catch {}
      throw error
    }
    return this.reportApplicationCommandResult({
      commandId: event.commandId,
      status: 'success',
      ...options,
    })
  }

  startEvents({ signal } = {}) {
    if (this.#closed.signal.aborted) throw new Error('Client is closed')
    if (this.#eventSubscription?.running) throw new Error('An event listener is already running')
    this.#eventSubscription = new EventSubscription(this, { signal, clientSignal: this.#closed.signal })
    return this.#eventSubscription
  }

  async watchEvents(options = {}) {
    await this.startEvents(options).done
  }

  async stopEvents() {
    await this.#eventSubscription?.stop()
  }

  onEvent(event, options) {}
  onCredentialChanged(credential, options) {}
  onCredentialRevoked(event, options) {}
  onEventError(error, event) {
    console.warn('Credential event handler failed:', error?.constructor?.name || 'Error')
  }

  async *watchCredentialEvents({ signal } = {}) {
    if (this.#closed.signal.aborted) throw new Error('Client is closed')
    const stop = new AbortController()
    this.#streams.add(stop)
    const signals = [stop.signal, this.#closed.signal]
    if (signal) signals.push(signal)
    const combined = AbortSignal.any(signals)
    let delay = 1000
    try {
      while (!combined.aborted) {
        const url = this.#url('/ws/accounts/credential-events/', this.#identity({}))
        url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:'
        const socket = new WebSocket(url, {
          headers: { ...this.#headers('GET', url), 'x-jms-event-receipts': '1' },
          handshakeTimeout: this.#options.timeout,
          followRedirects: false,
          maxPayload: 16 * 1024 * 1024,
        })
        const stream = createWebSocketStream(socket, {
          readableObjectMode: true,
          highWaterMark: 16,
        })
        const terminate = () => socket.terminate()
        combined.addEventListener('abort', terminate, { once: true })
        if (combined.aborted) terminate()
        let lastMessage = performance.now()
        const received = () => { lastMessage = performance.now() }
        socket.on('message', received)
        const ping = setInterval(() => {
          if (socket.readyState === WebSocket.OPEN && performance.now() - lastMessage >= 30000) {
            socket.terminate()
          } else if (socket.readyState === WebSocket.OPEN && performance.now() - lastMessage >= 10000) {
            socket.send(JSON.stringify({ event: 'ping' }), () => {})
          }
        }, 10000)
        try {
          for await (const frame of stream) {
            if (combined.aborted) break
            const event = object(JSON.parse(frame.toString()))
            requiredString(event.event, 'event')
            delay = 1000
            if (event.event === 'pong') continue
            if (
              event.event !== 'snapshot' &&
              event.event_id &&
              socket.readyState === WebSocket.OPEN
            ) {
              await new Promise((resolve) =>
                socket.send(JSON.stringify({ event: 'received', event_id: event.event_id }), () =>
                  resolve(),
                ),
              )
            }
            const message = camelize(event)
            this.#reconcileLatestCredentials(message)
            yield message
          }
        } catch {
          // Reconnect without logging response bodies, URLs or authorization material.
        } finally {
          clearInterval(ping)
          socket.off('message', received)
          combined.removeEventListener('abort', terminate)
          stream.destroy()
          socket.terminate()
        }
        if (!combined.aborted) {
          try {
            await wait(delay, undefined, { signal: combined })
          } catch {}
          delay = Math.min(delay * 2, 30000)
        }
      }
    } finally {
      stop.abort()
      this.#streams.delete(stop)
    }
  }

  clone() {
    return new Client(this.#options)
  }

  close() {
    this.#closed.abort()
    for (const stream of this.#streams) stream.abort()
    this.#latestCredentials.clear()
    this.#credentialGeneration++
  }
}

module.exports = { Client, PAMError, VERSION, PROTOCOL_VERSION, CONFIG_SCHEMA_VERSION }
