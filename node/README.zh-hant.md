# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. 接入前提與流程

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

僅按需取密時，完成首次取密即可；主動更新或 A/B 輪換還需實作托管業務回呼並啟動監聽。SDK 處理協議與事件結果，業務驗證並切換真實連線。範例對應目前原始碼；新增 API 需要配套的 Core 與 SDK 建置，已發行的 1.0.2 套件尚未包含全部新介面。

- Node.js 20.3+ / ws

## 2. 安裝 SDK 與身分配置

安裝下方 SDK，並使用應用接入精靈下載的身分設定。保護 AK/SK，每個副本使用穩定且唯一的實例 ID。

透過下方命令安裝目前 Core 隨附的用戶端原始碼，避免已發布的 1.0.2 套件缺少託管回呼。將 jumpserver.example.com 替換為 Core 位址；保留解壓後的 jms-pam-clients 目錄，Go 模組使用本機替換路徑。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/node/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
npm install ./jms-pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

### 本機原始碼安裝（備用）

若目前 Core 尚未提供原始碼下載介面，或需離線部署，取得配套原始碼放到 `/path/to/pam-clients`，再從業務目錄執行以下命令。請替換實際路徑。

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

## 3. 首次取得帳號憑據

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

## 4. 事件監聽與業務切換

實作下方的帳號變更回呼並啟動託管監聽即可。SDK 負責快照、即時取密、帳號與版本校驗、重試、指令認領及結果回報。業務回呼必須完成真實連線驗證、連線池切換與舊連線釋放後再返回。Python、Node.js 使用子類鉤子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未實作業務切換時，範例會明確失敗。

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

帳號回呼透過有界事件佇列循序執行；一般更新失敗按 1–30 秒退避重試。業務回呼成功返回後才回報成功；結果上傳失敗時，同一次監聽僅重試上傳，不重複已完成的動作。指令先認領再執行，失敗的指令動作不自動重做。已處理帳號自動以完整快照回報使用情況，A/B 切換會替換目前帳號。撤銷範圍需實作連線清理回呼。使用帳號回報、取密或檔案交付本身不會確認業務生效。原始事件流與舊憑據鉤子保留顯式確認。業務處理仍需支援程序重啟後的冪等執行。

### 監聽生命週期與結束

`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`

watchEvents 不阻塞事件迴圈；startEvents 不代表同步完成。每個客戶端只有一個高層監聽器。非同步鉤子循序 await 並收到 AbortSignal。close 取消後，由外部等待 stop() 或 done。鉤子勿等待自己的 done。clone 返回基礎 Client。

### 最新憑據與後端不可用

SDK 取密先請求 API，成功後替換保留值，不讓舊版本覆蓋新版本。只有逾時、網路錯誤或 HTTP 5xx 可回退到相同條件下已取得的值並標記本機來源；保留值不按時間過期。SDK 使用用戶端私有記憶體，clone 和重新啟動從空狀態開始。HTTP 401/403/404 或 credential_not_found（HTTP 400）會清空憑據並報錯。HTTP 426、成功回應格式錯誤不使用回退；可選中繼資料介面缺失不清空憑據。PAM Agent 在受保護狀態保留 store/services 宣告的帳號，未宣告的按需查詢不自動持久化。明確撤銷在 HTTP 刷新前生效，離線也相同；push 快照不代表儲存帳號的完整授權，成功對帳會核對目前應用授權。

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

啟用高層監聽後，snapshot、credential.updated 自動取密、替換保留值，再呼叫業務函式。刷新失敗保留上一份並重試。PAM Agent 也在更新通知後主動取密，後端故障時保留已有值。刷新與手動切換使用下方即時取得的呼叫；本地保留值不代表剛取得新版本，亦不自動確認輪換。




## 5. 常用方法

### 列出授權帳號

帳號列表方法查詢應用程式目前授權的 pull 範圍，只回傳帳號、資產及相關 push 策略的中繼資料，不取得密碼或記錄取密；未綁定策略的帳號也會回傳。完整列表方法自動讀取所有分頁；分頁方法回傳 `count` 和 `accounts`（Go 分別回傳列表與總數）。預設 limit 為 200、offset 為 0；limit 為 1–500，search 最長 128 個字元，可搜尋帳號名稱、使用者名稱、資產名稱或地址。重複、不完整或總數變動會報錯；授權變動時重新查詢完整列表。需要密碼時以帳號 ID 呼叫取密方法。範例需要包含新增方法的 SDK 原始碼；已發布的 1.0.2 尚未在 Python、Java、Node.js 提供這些方法。

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


### 應用程式、實例與策略資訊

getApplication 回傳目前簽章應用程式的 ID、名稱、組織及授權帳號與可見策略數量；getClientInfo 只回傳目前副本的登記資訊、類型和版本。策略列表一次回傳全部可見的關聯策略，包括停用策略；search 比對策略名稱或 key。單一策略以策略 ID 或策略本身的 key 查詢，不使用帳號 push key。輪換策略的兩個帳號及目前生效帳號都須有授權；訂閱數量只統計目前有權存取的帳號。查詢只回傳中繼資料，不回傳密鑰或密碼、不取密或確認生效。首次存取會登記副本。需要更新 Core 與 SDK 原始碼；已發布的 1.0.2 尚未包含這些方法。

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


### 應用狀態回報

回報應用狀態和目前使用的帳號 ID。支援 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次都是完整快照，空帳號清單會清除先前記錄。由應用在狀態變更或自己的心跳中呼叫，不會自動啟動背景回報。Python、Java、Node.js 遇到網路或 API 錯誤時預設回傳 false；Go 使用 `TryReportStatus`。嚴格模式可取得錯誤，無效參數仍會明確提示。呼叫使用設定的 HTTP 逾時，請避開影響業務回應的路徑。回報不會取得密碼、確認輪換或清除憑據快取。`error_code` 僅傳機器可讀錯誤碼，不要傳密碼或例外原文。透過 ClientInfo 查詢最後回報和時間。

```javascript
const accepted = await client.reportAccounts([account.id])
await client.reportAccounts([])
```


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

## 6. 接入驗收

1. 使用已授權帳號 ID 取密並驗證真實連線，不記錄密碼。
2. 監聽事件時，核對首次同步及一次策略更新，業務回呼完成切換後才應回報成功。
3. 分別檢查使用帳號、執行狀態和事件結果；取密或使用狀態上報不代表生效確認。

### 問題排查

HTTP、網路、認證及解碼錯誤使用 SDK 例外或錯誤類型，包含錯誤碼與 HTTP 狀態。使用完畢後關閉事件流與客戶端，複製客戶端具有獨立生命週期。在業務邊界重試暫時故障，不記錄密碼或認證標頭。

`PAMError`

各 SDK 使用版本 1 協定，PAM Agent 設定格式為版本 1。收到 client_upgrade_required（HTTP 426）時檢查相容性並升級。允許未知可選欄位及通知事件，未實作的策略類型不得套用或確認。SDK 自動傳送接收回執，不能作為憑證已生效的證明。 實例資訊可包含 server_capabilities；缺失或 null 表示未知，未知名稱可保留。能力清單描述伺服器功能，不授予權限。

<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# 应用集成概览

## 先了解授权、通知与轮换

1. **账号授权**决定应用可以按需获取哪些账号的凭据。安装客户端前，先完成应用授权。
2. **账号凭据变更通知**是应用级开关，覆盖全部授权账号。应用需要跟随账号密码更新时开启，无需创建订阅策略。
3. **A/B 账号切换**使用独立绑定的轮换策略，先将业务切到另一个账号，再修改原账号密码。两个账号都须获得授权；周期、等待条件、取消与恢复请查看具体策略中的“轮换运行说明”。

## 选择接入方式

| 接入方式 | 适用情况 | 从哪里开始 |
| --- | --- | --- |
| SDK | 可以修改业务代码，由应用直接连接 JumpServer。 | 选择“SDK 接入”和编程语言，需要自动更新时实现托管账号变更回调。 |
| PAM Agent | 需要本地凭据文件、业务配置更新或本地 Socket，希望将 JumpServer 密钥保留在独立 Agent 中。 | 选择“Agent 接入”，安装后按需配置 store、socket 和 services。 |
| Socket API | Business-managed connections through the local Agent; no JumpServer SDK or AK/SK in the business. | Socket API access; declare a service with delivery: socket. |

## 接入与验收顺序

1. 创建应用，授权账号，下载并保护应用身份配置。
2. 选择 SDK 或 PAM Agent，安装匹配客户端，先获取一个账号并验证真实连接。
3. 需要自动更新时，开启应用通知并实现业务更新；需要 A/B 切换时，再绑定轮换策略并允许跟随当前账号。
4. 验证一次账号更新或 A/B 切换：运行中的业务采用新凭据、旧连接释放、业务处理结果成功。仅接收事件、取密或写文件不足以通过验收。

## 后续查阅位置

“Agent 接入”说明安装、文件存储、本地接口和业务动作；“SDK 接入”说明各语言安装、托管回调和 API；“事件与指令”集中解释通用事件数据、快照、回执及手动指令。每个账号切换策略内的“轮换运行说明”解释该策略的周期、阻塞和恢复。

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# 事件与指令

本参考适用于各语言 SDK 和 PAM Agent。常规接入优先使用 SDK 托管回调或 Agent 业务服务；自行实现事件消费者时再使用底层协议。

## 事件职责

| 事件 | 应用职责 |
| --- | --- |
| credential.updated、snapshot 条目 | 获取事件指定的账号、校验版本，完成业务应用后上报结果。SDK 托管回调负责协议处理。 |
| app.scope.updated | 对账范围并清理失效状态。解除轮换绑定不等于撤销账号授权。 |
| `rotation.*`（如 `rotation.started`、`rotation.completed` 等）、`credential.change.*`（如 `credential.change.started`、`credential.change.completed` 等） | 更新进度、日志或告警，不重复应用凭据，也不对这些通知确认业务成功。 |
| credential.switch.requested、app.restart.requested | 管理员针对选定实例的手动指令，须认领、执行并上报指令结果。 |

## 完整事件清单

以下逐项列出 Core 应用事件、手动指令及连接控制消息；兼容项单独标注。这里是完整事件参考，轮换详情只解释其在具体流程中的作用。

### 凭据与范围事件

credential.updated 负责应用凭据；app.scope.updated 和 snapshot 负责范围对账。绑定消失时停止跟随策略并保留仍授权账号的使用状态；实际授权消失时才停用本地取密、清理凭据并通知业务释放连接。

| 事件标识 | 含义 |
| --- | --- |
| `credential.updated` | 当前凭据或发布账号更新，按事件账号取密并应用。 |
| `app.scope.updated` | 账号授权、通知开关或轮换策略绑定变化。按 authorized_account_ids 判断账号是否仍获授权，再通过 snapshot 对账通知项和轮换绑定；解除绑定不应关闭仍获授权账号的连接。 |

### 通用改密事件

通用任务生命周期通知用于进度与告警，不因此再次取密或确认业务成功；通过 credential.updated 或 snapshot 应用凭据。

| 事件标识 | 含义 |
| --- | --- |
| `credential.change.started` | 改密任务进入等待或执行阶段，不代表远端密码已改变。 |
| `credential.change.completed` | 改密任务成功，不代表应用已经采用新凭据。 |
| `credential.change.failed` | 改密失败或结果无法核验，检查任务及远端账号。 |

### 轮换进度事件

这些通知说明轮换进度与结果。应用通过 credential.updated 或 snapshot 切换，不因每条进度通知重复执行。

| 事件标识 | 含义 |
| --- | --- |
| `rotation.verify.started` | 开始验证备用账号能否登录。 |
| `rotation.verify.skipped` | 已依憑據策略跳過備用帳號登入驗證。 |
| `rotation.verify.failed` | 备用账号验证失败，本次不发布切换。 |
| `rotation.verify.cancelled` | 备用账号验证已取消。 |
| `rotation.started` | 轮换已发布目标账号并进入切换阶段。 |
| `rotation.waiting_for_app` | 等待必需应用实例完成切换并确认。 |
| `rotation.source.waiting` | 发布切换后，观察原主账号的成功取密记录。 |
| `rotation.source.ready` | 原主账号无取密窗口和客户端确认条件达标，可以继续改密。 |
| `rotation.completed` | 原主账号改密已核验，本轮轮换完成。 |
| `rotation.failed` | 轮换进入改密失败或需要核验恢复的状态。 |
| `rotation.force_stopped` | 管理员跳过剩余恢复确认，强制结束周期。 |

### 管理员指令事件

先用 running 认领，仅在 accepted 为 true 时执行，再上报指令结果；这些指令独立于自动轮换进度通知。

| 事件标识 | 含义 |
| --- | --- |
| `credential.switch.requested` | 要求选定实例应用已经发布的账号及版本，不会启动新轮换。 |
| `app.restart.requested` | 执行应用支持的重启动作，并验证健康状态。 |

### 连接与回执消息

SDK 和 Agent 管理保活及接收回执。snapshot、pong 无需 received 回执；快照中的凭据条目单独进行应用处理。

| 事件标识 | 含义 |
| --- | --- |
| `snapshot` | Core 在连接、重连或范围变化时发送当前通知范围及授权账号 ID。 |
| `ping` | 客户端向 Core 发送应用层保活消息。 |
| `pong` | Core 对应用层 ping 的响应。 |
| `received` | 客户端确认已读取事件，不表示业务成功。 |

### 兼容事件

旧事件名和旧准备周期兼容项，不作为新接入的执行流程。当前 Core 不再发送 credential.revoked；新轮换周期验证备用账号后直接发布切换。

| 事件标识 | 含义 |
| --- | --- |
| `credential.revoked` | 已停用：当前 Core 不再发送。由 app.scope.updated 和 snapshot 统一处理；仅保留旧 Core 接入及历史记录兼容。 |
| `configuration.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `rotation.preparation.started` | 旧准备流程开始对齐应用当前账号。 |
| `rotation.accounts.aligned` | 旧准备流程中，应用账号已完成对齐。 |
| `rotation.standby.waiting` | 旧准备流程观察备用账号取密。 |
| `rotation.preparation.ready` | 旧准备流程就绪，不代表已切换或改密。 |
| `rotation.preparation.cancelled` | 旧准备流程已取消。 |
| `rotation.verification.started` | 旧事件名，兼容映射为 `rotation.verify.started`。 |
| `rotation.verification.failed` | 旧事件名，兼容映射为 `rotation.verify.failed`。 |
| `rotation.verification.cancelled` | 旧事件名，兼容映射为 `rotation.verify.cancelled`。 |
| `application.scope.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `application.restart.requested` | 旧事件名，兼容映射为 `app.restart.requested`。 |
| `rotation.waiting_for_application` | 旧事件名，兼容映射为 `rotation.waiting_for_app`。 |
| `rotation.verification.skipped` | 旧事件名，兼容映射为 `rotation.verify.skipped`。 |

## 事件字段

| 字段 | 含义 |
| --- | --- |
| event_id | 用于接收回执和业务结果的事件标识。snapshot 本身无需回执，其中的凭据条目可有各自的 event_id。 |
| event / occurred_at | 事件类型和发布时间。 |
| account_id / account_revision | 实际目标账号及密码版本；应用前实时获取的账号版本须匹配。 |
| credential_key / revision | A/B 策略的发布元数据；普通账号通知使用账号版本，不混用两种版本。 |
| credential_mode | alternating_rotation 表示 A/B 策略，subscription 表示应用级账号通知，无需订阅策略；应用范围事件可能为 null。 |
| account_switch.account_ids | A/B 切换关联的账号。 |
| operation_id | 关联同一次变更或轮换周期中的事件。 |
| command_id / expires_at | 手动指令标识和执行截止时间，使用独立的指令结果接口。 |

## 快照与授权范围

初次連線、重連及範圍變化時，Core 傳送 snapshot。credentials 包含應用開啟通知後的全部授權帳號，以及已綁定且仍有授權的有效 A/B 輪換策略。每項依 credential.updated 處理，使用 event_id、account_id 和 account_revision。快照也包含 authorized_account_ids；完整授權帳號中繼資料可透過 get_accounts 查詢。解除輪換策略綁定不等於撤銷帳號授權。

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## 接收回执与业务结果

SDK 在交出業務事件前自動盡力傳送 received 回執，應用程式無需額外傳送。回執僅表示已讀取事件，不表示憑證已經生效，也不能取代確認。應用程式應處理首次與重新連線快照；snapshot 和 pong 無需回執。

```json
{"event": "received", "event_id": "<event-id>"}
```

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

## 管理员手动指令

管理员从应用的“发送事件”操作选择目标实例及有效期。

- credential.switch.requested 要求实例应用轮换策略**已经发布**的账号及版本，不会启动新轮换，也不会选择另一个账号。
- app.restart.requested 要求应用执行支持的重启动作并验证健康状态；SDK 应用须实现相应处理函数，Agent 能力由本地服务配置决定。

SDK 托管处理函数负责指令认领和结果回报。底层消费者使用已签名的客户端 API：

1. GET /api/v1/accounts/credential-client/commands/，传入 instance_id，轮询待处理指令；已连接实例也可通过 WebSocket 接收。
2. POST /api/v1/accounts/credential-client/command-result/，提交 instance_id、command_id 和 status: running，仅在 accepted 为 true 时执行。
3. 实际完成业务切换，或重启且健康检查成功后，通过同一指令结果接口报告 status: success；失败报告 failed，并使用安全的 error_code。received 回执或仅调用 confirm_event 不能替代指令结果上报。

过期指令不能再认领，重复投递不得重复执行已认领动作。失败的指令动作不自动重做；重新发送重启前先检查应用状态。

## 连接保活与兼容

閒置時每 10 秒傳送應用層 ping，約 30 秒收不到訊息則重連，以 1–30 秒指數退避並重新簽章。快照恢復目前狀態，不重播歷史事件。

当前使用 app.scope.updated、app.restart.requested、rotation.verify.started/failed/cancelled 和 rotation.waiting_for_app。旧 application 前缀、rotation.verification 名称及 configuration.updated 兼容映射到新名称，事件 ID 不变。credential.revoked 已停止发送，仅保留旧 Core 接入及历史记录兼容。

舊 key API 保留相容。新整合使用 `get_account` 和 `confirm_event`，並須同步更新 Core。

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Socket API 接入

業務應用透過本機 PAM Agent 接入，無需安裝 JumpServer SDK，也無需在業務進程保存應用 AK/SK。底層使用 Unix Domain Socket，不是 TCP。下方提供完整英文協定參考。

## 1. Unix Socket / HTTP / WebSocket

The transport is a Unix Domain Socket (AF_UNIX), not TCP. Connect to its filesystem path; no IP address or port is opened. HTTP/JSON requests and the WebSocket event channel share this socket. The localhost URL supplies the HTTP Host only. Agent-to-Core traffic still uses HTTPS/WSS.

## 2. Agent / services

Add a socket service to the Agent configuration. Use the same account declarations and allow_account_switch rules as file delivery. The declared A ID remains the map key while account_id identifies the active A/B account. Socket services cannot also configure update_config, reload_service, credential_check or app_check. restart_supported is an explicit opt-in for business restart commands.

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

Connect, read the initial snapshot, retrieve the delivery credentials, verify new real connections, replace business connections and release old sessions, then submit success. Receiving a notification or fetching a password never proves that the business has switched. A failed callback submits failed and is retried after the Agent backoff.

Every connection starts with the latest snapshot; this is current-state reconciliation, not historical replay. delivery contains metadata only, including delivery_id, declared-account keys and exact account/policy revisions. Revocations are delivered even during a Core outage; revoked_account_ids requires closing old sessions. With no complete credentials, a delivery may be absent while refresh is pending. Each independently applied business instance needs its own service_id. Only one WebSocket connection per service is accepted.

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.updated",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution.

## 4. Commands / status

Commands are claimed by the Agent before delegation. Restart commands appear in commands only for services with restart_supported=true. Report command.result after restart and readiness validation; success or failure is persisted and Core reporting is retried without executing the command again. A/B switch commands reuse credential delivery and its business result. Expired commands cannot be acknowledged.

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

Business status is separate from credential confirmation. Use starting, running, degraded, stopping, stopped or error and a short non-secret error_code. Account usage is updated only after business success, and the Agent aggregates all service reports. GET /v1/usage returns the aggregate. The existing POST /v1/usage replaces the instance-wide explicit account declaration; it does not acknowledge events.

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix socket ownership is the access boundary. service_id identifies a business consumer, not a separate security identity: processes able to open the same socket share the application authorization. Keep unrelated trust domains on separate Agent instances/sockets. Events omit passwords; retrieve credentials explicitly and never log them. Polling the service snapshot and posting results is supported when a language has no Unix-socket WebSocket client.

## 6. Python

The Python example uses websockets, not a JumpServer SDK. By default it only observes metadata and never acknowledges business success. Implement the Business callbacks and call run(socket_path, service_id, business) for real processing. On a new business process, initialize connections from the current delivery even when it was applied by the previous process. Persist business-side delivery IDs when crash-safe de-duplication is required.

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
