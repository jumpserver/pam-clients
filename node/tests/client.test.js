'use strict'
const assert = require('node:assert/strict')
const { inspect } = require('node:util')
const { test, before, after } = require('node:test')
const { setTimeout: wait } = require('node:timers/promises')
const { Client, PAMError } = require('../index')
const { startServer } = require('../../tests/contract-server')
let server
before(async () => {
  server = await startServer()
})
after(async () => {
  assert.deepEqual(server.stats().failures, [])
  await server.close()
})
function client(instanceId = 'node', source = 'jms-pam') {
  return new Client({
    endpoint: server.endpoint,
    appId: 'contract-app',
    appSecret: 'contract-secret',
    instanceId,
    orgId: 'contract-org',
    source,
  })
}

test('compatibility errors preserve cached values without accepting stale data as a successful upgrade', async () => {
  const instance = 'node-compat-cycle'
  const sdk = client(instance)
  const control = async fault => {
    const url = new URL('/__control', server.endpoint)
    url.search = new URLSearchParams({ instance, fault })
    assert.equal((await fetch(url)).status, 200)
  }
  try {
    const initial = await sdk.getAccount({ accountId: 'account' })
    await control(426)
    for (const allowLocalFallback of [true, false]) {
      await assert.rejects(sdk.getAccount({ accountId: 'account', allowLocalFallback }), error => error instanceof PAMError && error.isCompatibilityError)
    }
    await control(503)
    const retained = await sdk.getAccount({ accountId: 'account' })
    assert.equal(retained.fromLocal, true)
    assert.equal(retained.secret, initial.secret)
    await control(401)
    await assert.rejects(sdk.getAccount({ accountId: 'account' }), error => error instanceof PAMError && !error.isCompatibilityError)
    await control(503)
    await assert.rejects(sdk.getAccount({ accountId: 'account' }), PAMError)
  } finally { sdk.close() }
})

test('optional metadata handles legacy and future capabilities without invalidating cached values', async () => {
  for (const [instance, capabilities] of [
    ['node-compat-legacy', null],
    ['node-compat-capabilities', ['account_lookup', 'application_status', 'future_optional_capability']],
  ]) {
    const sdk = client(instance)
    try { assert.deepEqual((await sdk.getClientInfo()).serverCapabilities, capabilities) }
    finally { sdk.close() }
  }
  const bad = client('node-compat-bad-capabilities')
  try { await assert.rejects(bad.getClientInfo(), PAMError) } finally { bad.close() }
  const instance = 'node-compat-missing'
  const sdk = client(instance)
  try {
    await sdk.getAccount({ accountId: 'account' })
    await assert.rejects(sdk.getClientInfo(), error => error.statusCode === 404)
    const url = new URL('/__control', server.endpoint)
    url.search = new URLSearchParams({ instance, fault: 503 })
    await fetch(url)
    assert.equal((await sdk.getAccount({ accountId: 'account' })).fromLocal, true)
  } finally { sdk.close() }
})

test('status reporting is best effort, reports complete usage and preserves cached credentials', async () => {
  const sdk = client('node-status')
  try {
    assert.equal(await sdk.reportStatus({ status: 'running', accountId: 'account' }), true)
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus, { status: 'running', accountIds: ['account'], errorCode: '' })
    assert.equal(await sdk.reportStatus({ status: 'degraded', accountIds: ['account', 'backup'], errorCode: 'db_unavailable' }), true)
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, ['account', 'backup'])
    await sdk.getCredential({ key: 'cache-503' })
    for (const errorCode of ['fault_401', 'fault_403', 'fault_404', 'fault_503', 'fault_invalid', 'fault_network']) {
      assert.equal(await sdk.reportStatus({ status: 'running', errorCode }), false)
      await assert.rejects(sdk.reportStatus({ status: 'running', errorCode, raiseOnError: true }), PAMError)
    }
    assert.equal((await sdk.getCredential({ key: 'cache-503' })).fromLocal, true)
    assert.equal(await sdk.reportStatus({ status: 'running', signal: AbortSignal.abort() }), false)
    assert.equal(await sdk.reportStatus({ status: 'stopped' }), true)
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, [])
    await assert.rejects(sdk.reportStatus({ status: 'unknown' }), TypeError)
    await assert.rejects(sdk.reportStatus({ status: 'running', accountIds: ['account', 'account'] }), TypeError)
    await assert.rejects(sdk.reportStatus({ status: 'error', errorCode: 'DO NOT SEND SECRETS' }), TypeError)
  } finally { sdk.close() }
  assert.equal(await sdk.reportStatus({ status: 'stopped' }), false)
  await assert.rejects(sdk.reportStatus({ status: 'stopped', raiseOnError: true }))
  const timeoutClient = new Client({ endpoint: server.endpoint, appId: 'contract-app', appSecret: 'contract-secret', instanceId: 'node-status-timeout', orgId: 'contract-org', timeout: 50 })
  try { assert.equal(await timeoutClient.reportStatus({ status: 'running', errorCode: 'fault_timeout' }), false) }
  finally { timeoutClient.close() }
})

test('application, current client and bound policies expose only typed metadata', async () => {
  const sdk = client('node-metadata')
  try {
    const app = await sdk.getApplication()
    assert.equal(app.id, 'contract-app')
    assert.equal(app.orgId, 'contract-org')
    assert.equal(app.accountsCount, 3)
    const replica = await sdk.getClientInfo()
    assert.equal(replica.instanceId, 'node-metadata')
    assert.equal(replica.type, 'sdk')
    assert.equal(replica.configSchemaVersion, 0)
    const policies = await sdk.listPolicies()
    assert.equal(policies.length, 2)
    assert.equal(policies[1].isActive, false)
    assert.equal(policies[1].activeAccountId, null)
    assert.deepEqual(policies[0].accountSwitch.accountIds, ['account', 'backup'])
    assert.deepEqual(await sdk.getPolicy({ key: policies[0].key }), policies[0])
    assert.deepEqual(await sdk.getPolicy({ policyId: policies[0].id }), policies[0])
    assert.deepEqual(await sdk.listPolicies({ search: 'db/空 格?=&' }), [policies[0]])
    assert.deepEqual(await sdk.listPolicies({ search: 'no-match' }), [])
    assert.ok(!JSON.stringify([app, replica, policies]).includes('DO_NOT_LOG_SECRET'))
    for (const item of [app, replica, ...policies]) assert.equal(item.secret, undefined)
    assert.ok(server.stats().requests.filter(item => item.data.instance_id === 'node-metadata')
      .every(item => ['/application/', '/client/', '/policies/', '/policy/'].some(path => item.path.endsWith(path))))
  } finally { sdk.close() }
})

test('metadata queries reject malformed responses, duplicate policies and invalid selectors', async () => {
  const sdk = client('node-invalid-metadata')
  try {
    for (const method of [() => sdk.getApplication(), () => sdk.getClientInfo(),
      () => sdk.getPolicy({ key: 'invalid' })]) {
      await assert.rejects(method(), error => error instanceof PAMError && error.code === 'ResponseError')
    }
    for (const search of ['fault:invalid-list', 'fault:duplicate', 'fault:invalid-policy'])
      await assert.rejects(sdk.listPolicies({ search }), PAMError)
    for (const options of [{}, { key: 'db', policyId: 'id' }, { key: '' }, { key: 'x'.repeat(65) }])
      assert.throws(() => sdk.getPolicy(options), TypeError)
    assert.throws(() => sdk.listPolicies({ search: 'x'.repeat(129) }), TypeError)
    await assert.rejects(sdk.getPolicy({ key: 'missing' }), error => error.statusCode === 404)
    await assert.rejects(sdk.getApplication({ signal: AbortSignal.abort() }), PAMError)
  } finally { sdk.close() }
})

test('account listing follows pages, searches and returns metadata without secrets', async () => {
  const sdk = client('node-accounts')
  try {
    const accounts = await sdk.listAccounts()
    assert.equal(accounts.length, 205)
    assert.equal(new Set(accounts.map(item => item.id)).size, 205)
    assert.equal(accounts[0].asset.platform.type, 'postgresql')
    assert.deepEqual(accounts[0].credentials[0].accountSwitch.accountIds, ['account-0', 'backup'])
    assert.equal(accounts[1].credentials[0].mode, 'subscription')
    assert.deepEqual(accounts[2].credentials, [])
    assert.equal(accounts[2].username, '')
    assert.equal(accounts[0].secret, undefined)
    assert.ok(!inspect(accounts).includes('DO_NOT_LOG_SECRET'))
    const page = await sdk.listAccountsPage({ limit: 2, offset: 1 })
    assert.equal(page.count, 205)
    assert.deepEqual(page.accounts.map(item => item.id), ['account-1', 'account-2'])
    assert.deepEqual((await sdk.listAccountsPage({ offset: 205 })).accounts, [])
    assert.deepEqual((await sdk.listAccounts({ search: 'db/空 格?=&' })).map(item => item.id), ['account-0'])
    assert.deepEqual(await sdk.listAccounts({ search: 'no-match' }), [])
    assert.deepEqual(await sdk.listAccounts({ search: 'fault:empty' }), [])
    assert.ok(server.stats().requests.filter(item => item.data.instance_id === 'node-accounts')
      .every(item => item.path.endsWith('/accounts/')))
  } finally {
    sdk.close()
  }
})

test('account listing rejects malformed pages and never returns a partial list', async () => {
  const sdk = client('node-errors-accounts')
  try {
    for (const options of [
      { limit: 0 }, { limit: 501 }, { limit: true }, { limit: 1.5 },
      { offset: -1 }, { offset: true }, { search: null }, { search: 'x'.repeat(129) },
    ]) assert.throws(() => sdk.listAccountsPage(options), TypeError)
    for (const fault of ['invalid-count', 'null-count', 'missing-policy-revision', 'missing-username', 'null-policies', 'invalid-account', 'invalid-policy', 'invalid-list',
      'oversized', 'duplicate-page', 'incomplete', 'duplicate', 'changing-count', 'forbidden']) {
      await assert.rejects(sdk.listAccounts({ search: 'fault:' + fault }), error =>
        error instanceof PAMError && error.code === (fault === 'forbidden' ? 'credential_not_authorized' : 'ResponseError'))
    }
    await assert.rejects(sdk.listAccounts({ signal: AbortSignal.abort() }), PAMError)
  } finally {
    sdk.close()
  }
})

test('automatic signing, encoded selectors and typed responses', async () => {
  const sdk = client()
  try {
    for (const selector of [{ accountId: 'account' }, { key: 'db/空 格?=&' }]) {
      const item = await sdk.getCredential(selector)
      assert.equal(item.account.secret, 'DO_NOT_LOG_SECRET')
      assert.equal(item.account.secretType, 'password')
      assert.equal(item.asset.platform.type, 'postgresql')
      assert.ok(!inspect(item).includes('DO_NOT_LOG_SECRET'))
    }
    const item = await sdk.getCredential({ key: 'db' })
    assert.equal(
      (
        await sdk.confirmCredential({
          key: item.key,
          revision: item.revision,
          accountId: item.account.id,
        })
      ).revision,
      2,
    )
  } finally {
    sdk.close()
  }
})

test('invalid selectors and revisions fail before network access', () => {
  const sdk = client()
  const count = server.stats().requests.length
  assert.throws(() => sdk.getCredential({}), TypeError)
  assert.throws(() => sdk.getCredential({ key: 'db', accountId: 'account' }), TypeError)
  for (const revision of [true, '2', -1, 0])
    assert.throws(
      () => sdk.confirmCredential({ key: 'db', accountId: 'account', revision }),
      TypeError,
    )
  assert.equal(server.stats().requests.length, count)
  sdk.close()
})

test('HTTP, upgrade, malformed responses and redirects use PAMError', async () => {
  const sdk = client()
  try {
    for (const [key, code] of [
      ['forbidden', 'credential_not_authorized'],
      ['upgrade', 'client_upgrade_required'],
      ['invalid-json', 'ResponseError'],
      ['invalid-revision', 'ResponseError'],
      ['redirect', 'NetworkError'],
    ]) {
      await assert.rejects(
        sdk.getCredential({ key }),
        (error) => error instanceof PAMError && error.code === code,
      )
    }
  } finally {
    sdk.close()
  }
})

test('synchronization validates revisions and boolean flags', async () => {
  const sdk = client('node-agent', 'jms-pam-agent')
  const options = { credentials: [{ key: 'db', revision: 2 }], deliveredCredentials: [] }
  try {
    const sync = await sdk.syncAgent(options)
    assert.equal(sync.credentials[0].changed, false)
    assert.deepEqual(sync.scope.credential_keys, ['db'])
    await assert.rejects(
      sdk.syncAgent({ ...options, syncError: 'invalid-flag' }),
      (error) => error.code === 'ResponseError',
    )
  } finally {
    sdk.close()
  }
})

test('command claims prevent duplicate execution and retain handler errors', async () => {
  const sdk = client()
  try {
    const [event] = await sdk.listApplicationCommands()
    let executed = 0
    assert.equal((await sdk.executeApplicationCommand(event, () => executed++)).status, 'success')
    assert.equal(executed, 1)
    assert.equal(
      (await sdk.executeApplicationCommand({ ...event, commandId: 'duplicate' }, () => executed++))
        .accepted,
      false,
    )
    assert.equal(executed, 1)
    const failure = new Error('business failure')
    await assert.rejects(
      sdk.executeApplicationCommand({ ...event, commandId: 'report-failure' }, () => {
        throw failure
      }),
      (error) => error === failure,
    )
  } finally {
    sdk.close()
  }
})

test(
  'event reconnection restores snapshot and sends receipts only for business events',
  { timeout: 6000 },
  async () => {
    const sdk = client('node-events')
    const events = []
    try {
      for await (const event of sdk.watchCredentialEvents()) {
        events.push(event)
        if (events.length === 4) break
      }
      assert.deepEqual(
        events.map((event) => event.event),
        ['snapshot', 'credential.updated', 'future.notification', 'snapshot'],
      )
      assert.equal(events[3].credentials[0].revision, 3)
      assert.equal(events[2].futurePayload.notice, 'preserved')
      await wait(20)
      const received = server
        .stats()
        .receipts.filter((receipt) => receipt.instance === 'node-events')
        .map((receipt) => receipt.eventId)
      assert.ok(received.includes('updated-1'))
      assert.ok(received.includes('future-1'))
      assert.ok(received.every((id) => !id.includes('snapshot')))
    } finally {
      sdk.close()
    }
  },
)

test(
  'cancellation and close stop active streams; clone remains independent',
  { timeout: 4000 },
  async () => {
    const sdk = client('node-stop')
    const clone = sdk.clone()
    const abort = new AbortController()
    const stream = sdk.watchCredentialEvents({ signal: abort.signal })
    assert.equal((await stream.next()).value.event, 'snapshot')
    abort.abort()
    await stream.return()
    sdk.close()
    await assert.rejects(sdk.getCredential({ key: 'db' }), /closed/)
    assert.equal((await clone.getCredential({ key: 'db' })).revision, 2)
    clone.close()
  },
)

 test('account projection and event results use signed account and event IDs', async () => {
   const sdk = client('node-account-event')
   try {
     const account = await sdk.getAccount({ accountId: 'account', allowLocalFallback: false })
     assert.equal(account.username, 'app'); assert.equal(account.asset.address, '127.0.0.1')
     assert.equal(account.fromLocal, false); assert.equal(account.revision, 2)
     assert.ok(!inspect(account).includes('DO_NOT_LOG_SECRET'))
     assert.equal((await sdk.confirmEvent({ eventId: 'event-one' })).accepted, true)
     assert.equal((await sdk.confirmEvent({ eventId: 'event-two', status: 'failed', errorCode: 'application_failed' })).status, 'failed')
   } finally { sdk.close() }
 })
