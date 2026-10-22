'use strict'
const assert = require('node:assert/strict')
const { test, before, after } = require('node:test')
const { setTimeout: wait } = require('node:timers/promises')
const { Client } = require('../index')
const { startServer } = require('../../tests/contract-server')
let server
before(async () => { server = await startServer() })
after(async () => { assert.deepEqual(server.stats().failures, []); await server.close() })

class ApplicationClient extends Client {
  applied = []
  failures = 0
  async onAccountChanged(account) {
    assert.equal(account.fromLocal, false)
    if (this.failures-- > 0) throw new Error('business unavailable')
    this.applied.push({ id: account.id, revision: account.revision })
  }
  onAccountRevoked() {}
  onEventError() {}
}
function client(instanceId) {
  return new ApplicationClient({ endpoint: server.endpoint, appId: 'contract-app', appSecret: 'contract-secret',
    instanceId, orgId: 'contract-org' })
}
async function control(instance, values) {
  const url = new URL('/__control', server.endpoint)
  url.search = new URLSearchParams({ instance, ...values })
  assert.equal((await fetch(url)).status, 200)
}
async function until(check) {
  const deadline = Date.now() + 5000
  while (Date.now() < deadline) {
    if (await check()) return
    await wait(25)
  }
  assert.fail('managed event did not converge')
}

test('managed callback runs once when result delivery fails and reconnect repeats the event', async () => {
  const id = 'managed-node-result'
  await control(id, { result_failures: 1 })
  const sdk = client(id)
  const subscription = sdk.startEvents()
  try {
    await until(async () => (await sdk.getClientInfo()).applicationStatus?.accountIds?.includes('account'))
    assert.deepEqual(sdk.applied, [{ id: 'account', revision: 2 }])
    const results = server.stats().requests.filter(request => request.path.endsWith('/event-result/') && request.data.instance_id === id)
    assert.ok(results.length >= 2)
    assert.ok(results.every(request => request.data.status === 'success'))
  } finally { await subscription.stop(); sdk.close() }
})

test('managed updates retry business failure and A/B changes replace current usage', async () => {
  const id = 'managed-node-rotation'
  const sdk = client(id)
  sdk.failures = 1
  const subscription = sdk.startEvents()
  try {
    await until(async () => (await sdk.getClientInfo()).applicationStatus?.accountIds?.includes('account'))
    assert.equal(sdk.applied.length, 1)
    await control(id, { revision: 3, account_id: 'backup', event: 'credential.updated' })
    await until(async () => (await sdk.getClientInfo()).applicationStatus?.accountIds?.includes('backup'))
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, ['backup'])
    assert.deepEqual(sdk.applied.at(-1), { id: 'backup', revision: 3 })
    assert.ok(server.stats().requests.some(request => request.path.endsWith('/event-result/') && request.data.instance_id === id && request.data.status === 'failed'))
  } finally { await subscription.stop(); sdk.close() }
})

test('explicit usage reporting neither fetches nor confirms credentials', async () => {
  const sdk = client('managed-node-explicit')
  try {
    assert.equal(await sdk.reportAccounts(['account']), true)
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, ['account'])
    await sdk.reportAccounts([])
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, [])
    assert.equal(server.stats().requests.filter(request => request.data.instance_id === 'managed-node-explicit' && ['/credential/', '/event-result/'].some(path => request.path.endsWith(path))).length, 0)
  } finally { sdk.close() }
})

test('business callback mutations cannot change the reported event account identity', async () => {
  const sdk = client('managed-node-mutation')
  sdk.onAccountChanged = async account => { account.id = 'consumer-mutation' }
  const subscription = sdk.startEvents()
  try {
    await until(async () => (await sdk.getClientInfo()).applicationStatus?.accountIds?.includes('account'))
    assert.deepEqual((await sdk.getClientInfo()).applicationStatus.accountIds, ['account'])
  } finally { await subscription.stop(); sdk.close() }
})

test('a removed push scope rejects queued updates until a new snapshot regrants it', async () => {
  const sdk = client('managed-node-scope')
  const revoked = []
  const update = { event: 'credential.updated', eventId: 'scope-event', key: 'database',
    credentialMode: 'alternating_rotation', accountId: 'account', accountRevision: 2, revision: 2 }
  sdk.onAccountRevoked = accountId => revoked.push(accountId)
  sdk.watchCredentialEvents = async function* () {
    yield { event: 'snapshot', credentials: [update] }
    yield { event: 'snapshot', credentials: [] }
    yield update
    yield { event: 'snapshot', credentials: [update] }
  }
  try {
    await sdk.watchEvents()
    assert.deepEqual(revoked, ['account'])
    assert.equal(sdk.applied.length, 2)
  } finally { sdk.close() }
})
