# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号。
2. 需要及时更新时，在应用中开启“账号凭据变更通知”，覆盖全部授权账号。A/B 账号切换仍绑定独立轮换策略。
3. 下载应用接入材料，保护 AK/SK；每个副本使用稳定、唯一的实例 ID。

仅按需取密时，完成“首次获取账号凭据”即可；需要主动更新或 A/B 轮换时，再实现托管业务回调并启动监听。SDK 负责协议和事件结果，业务负责验证并切换真实连接。本页示例对应当前源码，新增 API 需要配套的 Core 和 SDK 构建；已发布的 1.0.2 安装包尚未包含全部新接口，请先部署匹配版本再运行这些示例。

- Node.js 20.3+ / ws

## 2. 安装 SDK 与身份配置

安装 SDK，填写下方配置。在应用中授权允许按需获取的账号；需要及时通知时开启“账号凭据变更通知”，覆盖应用全部授权账号，无需单独创建订阅策略。只有需要 A/B 账号切换时才绑定轮换策略。从应用接入材料获取 AK/SK 和组织 ID，并保存在受保护的配置中；每个副本使用稳定、唯一的实例 ID。

通过下方命令安装当前 Core 随附的客户端源码，避免已发布的 1.0.2 包缺少托管回调。将 jumpserver.example.com 替换为 Core 地址；保留解压后的 jms-pam-clients 目录，Go 模块使用本地替换路径。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/node/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
npm install ./jms-pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

### 本地源码安装（备用）

当前 Core 尚未提供上述源码下载接口，或需要离线部署时，取得配套客户端源码，放到 `/path/to/pam-clients`，再从业务应用目录执行以下命令。请替换为实际源码路径；托管回调需要包含对应方法的 SDK。

```bash
npm install /path/to/pam-clients/node
```


```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
```

## 3. 首次获取账号凭据

`demo.js`

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const account = await client.getAccount({ accountId: process.env.JMS_ACCOUNT_ID })
    // Pass account.username / secret to the application connection pool.
    console.log(
      `Fetched revision ${account.revision}; implement application credential switching.`,
    )
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```

```bash
node demo.js
```

## 4. 事件监听与业务切换

实现下面的账号变更回调并启动托管监听即可。SDK 负责快照、实时取密、账号与版本校验、重试、指令认领和结果回报。业务回调必须完成真实连接验证、连接池切换和旧连接释放后再返回。Python、Node.js 使用子类钩子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未实现业务切换时，示例会明确失败。

`hooks.js`

```javascript
'use strict'
const { Client } = require('@jumpserver/pam')

if (typeof Client.prototype.onAccountChanged !== 'function')
  throw new Error('Upgrade @jumpserver/pam to a version with managed account events')

class ApplicationClient extends Client {
  async onAccountChanged(account, { signal }) {
    // Validate, switch the connection pool and release old connections.
    // Return only after the application uses this account. Never log secrets.
    throw new Error('Implement application account switching')
  }
  async onAccountRevoked(accountId, { signal }) {
    throw new Error('Implement revoked account cleanup')
  }
}
async function main() {
  const client = new ApplicationClient({ endpoint: process.env.JMS_ENDPOINT, appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET, instanceId: process.env.JMS_INSTANCE_ID, orgId: process.env.JMS_ORG_ID })
  const stop = () => client.close()
  process.once('SIGINT', stop); process.once('SIGTERM', stop)
  // SDK handles snapshots, fresh fetches, versions, retries and event results.
  // Pull-only code can call client.reportAccounts([accountId]); [] clears usage.
  try { await client.watchEvents() }
  finally { await client.close(); process.off('SIGINT', stop); process.off('SIGTERM', stop) }
}
if (require.main === module) main().catch((error) => { console.error(error.code || error.name); process.exitCode = 1 })
```

```bash
node hooks.js
```

账号回调通过有界事件队列串行执行；普通更新失败按 1–30 秒退避重试。业务回调成功返回后才上报成功；结果上传失败时，同一次监听仅重试上传，不重复已完成的动作。指令先认领再执行，失败的指令动作不自动重做。已处理账号自动按完整快照上报使用情况，A/B 切换会替换当前账号。撤销范围需实现账号连接清理回调。使用账号上报、取密或文件交付本身不会确认业务生效。原始事件流和旧凭据钩子保留显式确认方式。业务处理仍需支持进程重启后的幂等执行。

### 监听生命周期与退出

`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`

watchEvents 等待完成且不阻塞事件循环；startEvents 返回不代表初始同步完成。每个客户端允许一个高层监听器。异步钩子逐个 await，并收到 AbortSignal。close 同步请求取消，由外部 await subscription.stop() 或 done 等待结束。钩子可 await stopEvents，但不要等待自己的 done。clone 返回新的基础 Client。

### 最新凭据与后端不可用

SDK 取密先请求 API，成功后替换保留值，不允许旧版本覆盖新版本。仅超时、网络错误或 HTTP 5xx 可回退到相同选择条件下已获取的值，并标记本地来源；保留值不按时间过期。SDK 使用客户端私有内存，clone 和重启从空状态开始。HTTP 401/403/404 或 credential_not_found（HTTP 400）清空保留凭据并报错；HTTP 426、成功响应格式错误均不使用回退，可选元数据接口缺失不会清空凭据。Agent 在受保护的状态中保留 store/services 声明的账号，未声明的按需查询不会自动持久化。明确撤销在 HTTP 刷新前生效，离线也不例外；push 快照不代表已存储账号的完整授权范围，成功对账会核对当前应用授权。

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

启用高层事件监听后，snapshot、credential.updated 会自动获取当前凭据并替换本地保留值，再调用业务处理函数。刷新失败时保留上一份凭据并重试。Agent 同样在更新通知后主动取密，后端故障期间保留已有凭据。事件刷新和手动切换使用下方必须实时获取的调用；保留的密码不能被当成刚获取的新版本，也不会自动确认轮换。

事件连接空闲时每 10 秒发送应用层 ping，约 30 秒收不到消息则重连。重连采用 1–30 秒指数退避并重新签名。重连快照恢复当前状态，不重放历史事件。



## 5. 常用方法

### 列出授权账号

使用账号列表方法查询应用当前授权的 pull 范围。列表只返回账号、资产和关联 push 策略的元数据，不获取密码，不记录取密；未绑定策略的授权账号也会返回。完整列表方法自动读取所有分页；分页方法返回 `count` 和 `accounts`（Go 分别返回列表和总数）。默认 limit 为 200、offset 为 0；limit 范围为 1–500，search 最长 128 个字符，可匹配账号名称、用户名、资产名称或地址。重复账号、分页不完整或总数变化会报错；授权范围变化时重新查询完整列表。需要密码时，再使用账号 ID 调用取密方法。示例需要包含这些新增方法的 SDK 源码；已发布的 1.0.2 尚未在 Python、Java、Node.js 中提供这些方法。

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const accounts = await client.listAccounts()
    for (const account of accounts) {
      console.log(account.id, account.username, account.asset.name)
    }
    const page = await client.listAccountsPage({ limit: 20, offset: 0, search: 'database' })
    console.log('Matching accounts:', page.count)
    // Use an authorized account.id with getAccount when its secret is needed.
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```


### 应用、实例与策略信息

getApplication 返回当前签名应用的 ID、名称、组织以及授权账号和可见策略数量；getClientInfo 只返回当前副本的登记信息、类型和版本。策略列表一次返回全部可见的关联策略，包括停用策略；search 匹配策略名称或 key。查询单个策略时只提供策略 ID 或策略本身的 key，不使用账号 push key。轮换策略要求两个账号及当前生效账号均在应用授权范围内；普通账号通知由应用开关控制，覆盖全部授权账号。这些查询只返回元数据，不返回密钥或密码，不取密、不确认凭据生效。和账号查询一样，首次访问会登记当前副本。需要同步更新 Core 和 SDK 源码；已发布的 1.0.2 尚未包含这些方法。

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const application = await client.getApplication()
    console.log(application.id, application.name, application.accountsCount)
    const replica = await client.getClientInfo()
    console.log(replica.instanceId, replica.clientVersion)
    const policies = await client.listPolicies()
    for (const policy of policies) {
      console.log(policy.key, policy.name, policy.isActive, policy.status)
    }
    if (policies.length) {
      const policy = await client.getPolicy({ policyId: policies[0].id })
      console.log(policy.name, policy.accountsCount)
    }
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```


### 应用状态上报

上报应用运行状态和当前使用的账号 ID，支持 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次上报都是完整快照，空账号列表会清除此前使用记录。由应用在状态变化或自己的心跳中调用，不会自动启动后台上报。Python、Java、Node.js 遇到网络、API 错误或旧 Core 不支持接口时默认返回 false；Go 使用 `TryReportStatus` 获得相同行为。严格调用方式可以获取具体错误，参数错误仍会明确提示。调用遵循配置的 HTTP 超时，请放在不影响业务响应的执行路径。上报不会取密、确认轮换或清空已有凭据缓存。`error_code` 只传机器可读错误码，不要传密码或异常原文。`get_client_info` / `GetClientInfo` / `getClientInfo` 可以读取最后一次上报及其时间。

```javascript
const accepted = await client.reportAccounts([account.id])
await client.reportAccounts([])
```


### 底层事件与应用指令

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

`get_credential` 和基于 key 的确认接口保留兼容旧接入；新接入使用 `get_account` 和 `confirm_event`。事件结果流程需要同步更新 Core，仅升级 SDK 1.0.2 不会增加服务端能力。

### API

- `getApplication({signal}) / getClientInfo({signal})`
- `reportAccounts(accountIds) / reportStatus({status, accountId, accountIds, errorCode, raiseOnError, signal})`
- `listPolicies({search, signal}) / getPolicy({key, policyId, signal})`
- `listAccounts({search, signal})`
- `listAccountsPage({limit, offset, search, signal})`
- `getAccount({accountId})`
- `getAccount({accountId, allowLocalFallback: false})`
- `confirmEvent({eventId, status, errorCode})`
- `watchCredentialEvents({signal})`
- `watchEvents({signal}) / startEvents({signal}) / stopEvents()`
- `EventSubscription.stop() / done`
- `listApplicationCommands()`
- `reportApplicationCommandResult({commandId, status, errorCode})`
- `executeApplicationCommand(event, handler)`
- `syncAgent({credentials, deliveredCredentials, ...})`
- `clone() / close()`

## 6. 接入验收

1. 将占位值替换为已授权的账号 ID，取密后验证真实连接，不记录密码。
2. 使用事件监听时，核对首次同步和一次策略更新；业务回调完成真实切换后，事件才应显示成功。
3. 分别检查实例使用账号、运行状态和事件结果。取密或使用状态上报不会确认凭据生效。

### 排查问题

HTTP、网络、认证和响应解码错误使用 SDK 异常或错误类型，包含错误码及 HTTP 状态。使用完成后关闭事件流与客户端，克隆客户端具有独立生命周期。在业务边界重试临时故障，不记录密码或认证请求头。

`PAMError`

各 SDK 使用版本 1 协议，Agent 配置格式为版本 1。收到 client_upgrade_required（HTTP 426）时检查兼容性并升级。允许未知可选字段及通知事件，未实现的策略类型不得应用或确认。事件接收回执由 SDK 自动发送，不能作为凭据已经生效的证明。 实例信息可包含 server_capabilities；缺失或 null 表示未知，未知名称可保留。能力列表描述服务端功能，不授予权限。

<!-- sdk-doc:end -->
