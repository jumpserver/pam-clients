# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. 接入前提與流程

1. 建立應用，授權允許按需取得的帳號（pull）。帳號 ID 是 JumpServer 帳號 ID，不是使用者名稱或策略 ID。
2. 需要主動交付更新時，再綁定改密通知或 A/B 輪換策略（push）；僅授權帳號不會主動推送憑據。
3. 在應用接入精靈下載身分配置。每個副本使用穩定、唯一的 `instance_id`，並妥善保管 AK/SK。

僅按需取密時，完成首次取密即可；主動更新或 A/B 輪換還需實作托管業務回呼並啟動監聽。SDK 處理協議與事件結果，業務驗證並切換真實連線。範例對應目前原始碼；新增 API 需要配套的 Core 與 SDK 建置，已發行的 1.0.2 套件尚未包含全部新介面。

- Node.js 20.3+ / ws

## 2. 安裝 SDK 與身分配置

安裝 SDK 並填寫下方設定。在應用程式管理中授權可 pull 的帳號；僅在需要 push 或輪換時綁定憑據策略。從接入資料取得應用程式 AK/SK 與組織 ID。替換預留值並保護身分資料，每個副本使用穩定且唯一的實例 ID。依帳號 ID 取密。

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

取密先請求 API。成功取得新憑據後替換本地保留值，舊版本不覆蓋新版本，且不按時間過期。僅在逾時、網路故障或 HTTP 5xx 時，返回相同查詢條件下已取得的最新憑據並標記本地來源；沒有保留值則拋出原始錯誤。SDK 在目前客戶端記憶體內保留，直到更新、撤銷或關閉；clone 與重新啟動不繼承。Agent 使用既有受保護的本地狀態保存。HTTP 401/403/404 清空 SDK 保留值並報錯，成功回應格式錯誤也報錯。明確撤銷移除對應憑據；快照移除授權範圍外項目；設定變更先保留，由後續快照核對範圍。Agent 在 HTTP 同步前先執行明確撤銷或快照授權範圍縮小並保存範圍，後端故障期間或重新啟動後也會阻止相應本地取密。credential_not_found（HTTP 400）同樣清除 SDK 保留值。 HTTP 426 明確報錯並保留憑據，不用快取代替本次請求成功。可選中繼資料介面缺失（HTTP 404）不清理憑據。Agent 純 pull 值不做持久離線保留；SDK 可按原有暫時故障規則使用記憶體保留值。

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

啟用高層監聽後，snapshot、credential.updated 自動取密、替換保留值，再呼叫業務函式。刷新失敗保留上一份並重試。Agent 也在更新通知後主動取密，後端故障時保留已有值。刷新與手動切換使用下方即時取得的呼叫；本地保留值不代表剛取得新版本，亦不自動確認輪換。

閒置時每 10 秒傳送應用層 ping，約 30 秒收不到訊息則重連，以 1–30 秒指數退避並重新簽章。快照恢復目前狀態，不重播歷史事件。



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


### 底層事件與應用指令

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

舊 key API 保留相容。新整合使用 `get_account` 和 `confirm_event`，並須同步更新 Core。

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

各 SDK 使用版本 1 協定，Agent 設定格式為版本 1。收到 client_upgrade_required（HTTP 426）時檢查相容性並升級。允許未知可選欄位及通知事件，未實作的策略類型不得套用或確認。SDK 自動傳送接收回執，不能作為憑證已生效的證明。 實例資訊可包含 server_capabilities；缺失或 null 表示未知，未知名稱可保留。能力清單描述伺服器功能，不授予權限。

<!-- sdk-doc:end -->
