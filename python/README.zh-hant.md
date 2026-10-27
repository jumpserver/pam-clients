# JumpServer PAM Python SDK / Agent

Python 3.9+ 提供憑據策略 SDK。Go jms-pam-agent 無需 Python，可透過本機檔案、固定動作或本機 Socket 交付憑據。Linux、macOS 和 Windows 支援前景執行；內建 systemd 安裝僅支援 Linux。

<!-- agent-doc:start -->

# Go Agent 接入

在應用主機執行獨立 Go Agent，無需 Python。依序完成安裝、選擇交付方式、設定業務動作和驗收。

## 1. 接入前提與流程

1. 建立應用，授權允許按需取得的帳號（pull）。帳號 ID 是 JumpServer 帳號 ID，不是使用者名稱或策略 ID。
2. 需要主動交付更新時，再綁定改密通知或 A/B 輪換策略（push）；僅授權帳號不會主動推送憑據。
3. 在應用接入精靈下載身分配置。每個副本使用穩定、唯一的 `instance_id`，並妥善保管 AK/SK。

## 2. 安裝與啟動

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/jms-pam-agent-linux-amd64) · [Linux ARM64](/download/pam/jms-pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# 在下載的 Agent 和 SHA256SUMS 所在目錄執行。
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./jms-pam-agent-linux-${agent_arch}" /usr/local/bin/jms-pam-agent
```

在下載的 `jms_pam_agent.json` 所在目錄執行。Linux 服務命令需要 root 和 systemd；每個副本替換自己的實例 ID。前台模式須使用對應系統的二進位檔，macOS / Windows 可能需要從原始碼建置。

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### 前台執行（Linux / macOS / Windows）

內建安裝需要 Linux/root。macOS、非 root Linux 或 Windows 請在接入精靈中選擇 JSON 或 Socket 交付，依照 init-local 與 run --local --config 命令執行。只初始化一次，後續重複使用產生的私有本機設定；前景模式不執行 systemd 動作。

前台二進位檔命名為 `jms-pam-agent`（Windows 為 `jms-pam-agent.exe`）；Linux/macOS 先執行 `chmod 0755 ./jms-pam-agent`。

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/orders" --instance-id orders-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/orders/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\orders'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id orders-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 選擇交付方式

JSON 交付檔案，EnvironmentFile 對接固定 systemd 服務，Unix Socket 提供本地 API。Agent 在交付成功後儲存交付版本，應用程式驗證並使用後才儲存生效版本。Socket 屬於設定的應用程式使用者，權限為 0600，本地請求應以該使用者執行。

| 選擇交付方式 | 設定與執行 |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

Linux systemd 私有配置為 `/etc/jms-pam-agent/agent.json`（0600）。`state_file` 儲存最新憑據與進度，`event_file` 記錄事件中繼資料，`delivery_root` 與 `socket_path` 指定交付位置。前台模式以產生的 `agent.json` 路徑為準。

## 4. 更新業務配置與 A/B 輪換

下載的引導設定已包含 Agent 身分與交付設定。只需在 rules 中宣告業務使用的帳號 ID，再設定更新業務檔案、生效動作及執行中連線驗證。帳號設定 allow_account_switch 後可沿用同一規則處理 A/B 雙向輪換；可選 credential_check 於修改檔案前驗證新帳號。狀態、事件、Socket 路徑及 300 秒對帳週期皆有預設值。rules 為空時依憑據寫入預設檔案。

第一個 JSON 是完整基礎配置；更新業務檔案時，用第二個陣列取代空的 `rules`。路徑、服務名稱與腳本均為範例，啟用前須在應用主機準備好。普通更新可省略 `allow_account_switch`；A/B 輪換需啟用它並驗證真實業務連線。

| 設定與執行 | 更新業務配置與 A/B 輪換 |
| --- | --- |
| `credential_check` | 可選：修改檔案前驗證新帳號連線。 |
| `config_update` | 透過欄位映射、範本或腳本更新業務檔案。 |
| `service_action` | 重新載入或重新啟動固定服務，或執行固定腳本。 |
| `application_check` | 驗證運行中業務已使用新連線，再確認生效。 |

```json
{
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "orders-node-1",
  "delivery": {
    "delivery_mode": "json",
    "delivery_root": "/opt/jumpserver-pam/credentials",
    "app_user": "orders"
  },
  "rules": []
}
```

`rules`:

```json
[
  {
    "accounts": [
      {
        "account_id": "<primary-account-id>",
        "allow_account_switch": true
      }
    ],
    "config_update": {
      "file": "/etc/order-service/config.yml",
      "format": "auto",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "service_action": {
      "unit": "order-service.service",
      "operation": "restart"
    },
    "application_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

### 配置檔案格式

`config_update.format` 預設為 `auto`，省略與明確填寫的效果相同。依副檔名辨識：`.txt`、`.env`、`.properties` 使用 `env`，替換 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替換頂層 `KEY: value`，新值寫為帶引號的字串。其他副檔名需明確指定 `env` 或 `yaml`。映射欄位必須已存在且只出現一次，其他設定保留。INI section、巢狀 YAML 等複雜格式使用 `files` + `template_file` 完整產生，或用 `script` 自訂替換。環境變數檔案更新後，應用仍需重新讀取檔案或重新啟動才會生效。

本機 rules 設定目標檔案、JSON/EnvironmentFile 或可信範本，以及可選的 systemd reload/restart 或固定可執行腳本。腳本透過標準輸入接收憑據 JSON，參數固定、有逾時，並應在驗證業務生效後回報成功。Core 不能新增腳本路徑或擴大本機能力。修改私有設定後重新啟動 Agent。

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. 本地 API 與確認

### 查詢帳號與憑據

CLI 和 Socket 請以設定的應用使用者執行。`get_accounts` 僅列出授權帳號中繼資料；`get_secret` 按需取密，也適用於未訂閱 push 的帳號。密碼會寫到標準輸出，請在私人終端調試。前台模式請替換實際 Socket 路徑。

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

### 確認業務生效

業務驗證真實連線並完成切換後再確認事件。可用 `application_check` 成功後以 `confirm_on_success: true` 自動確認，或由業務驗證後顯式確認；同步檢查腳本不要反向呼叫確認介面。結果先持久化，`confirmed` 表示 Core 已接受，`pending` 會重試。

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 接入驗收

1. 確認實例在線且帳號授權符合預期。
2. 使用 push 時，從綁定策略發送更新並核對檔案或 Socket 的帳號與版本。
3. A/B 輪換須驗證業務已使用新帳號並釋放舊連線，再檢查事件結果。在線、已取密、已交付不能單獨證明業務生效。

### 問題排查

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent 在啟動、相關事件及每 300 秒同步。網路故障保留已取得的最新授權憑據；身分或授權被拒絕時阻止 Socket 取密，成功簽章同步後恢復。已交付檔案保留。SIGINT/SIGTERM 會關閉服務、連線及事件讀取執行緒。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 接入

## 1. 接入前提與流程

1. 建立應用，授權允許按需取得的帳號（pull）。帳號 ID 是 JumpServer 帳號 ID，不是使用者名稱或策略 ID。
2. 需要主動交付更新時，再綁定改密通知或 A/B 輪換策略（push）；僅授權帳號不會主動推送憑據。
3. 在應用接入精靈下載身分配置。每個副本使用穩定、唯一的 `instance_id`，並妥善保管 AK/SK。

僅按需取密時，完成首次取密即可；主動更新或 A/B 輪換還需實作托管業務回呼並啟動監聽。SDK 處理協議與事件結果，業務驗證並切換真實連線。範例對應目前原始碼；新增 API 需要配套的 Core 與 SDK 建置，已發行的 1.0.2 套件尚未包含全部新介面。

## 2. 安裝 SDK 與身分配置

透過下方命令安裝目前 Core 隨附的用戶端原始碼，避免已發布的 1.0.2 套件缺少託管回呼。將 jumpserver.example.com 替換為 Core 位址；保留解壓後的 jms-pam-clients 目錄，Go 模組使用本機替換路徑。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### 本機原始碼安裝（備用）

若目前 Core 尚未提供原始碼下載介面，或需離線部署，取得配套原始碼放到 `/path/to/pam-clients`，再從業務目錄執行以下命令。請替換實際路徑。

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` 是應用程式設定檔，不屬於 PyPI 的 `jms-pam` 套件。從應用程式接入精靈下載並放在應用程式腳本旁，或依下方內容建立檔案，將佔位值替換為實際身分資訊。不要提交或記錄 `client_options`。每個副本使用穩定且唯一的 `instance_id`。

```python
# jms_pam_config.py
import os

client_options = {
    "endpoint": "https://jumpserver.example.com",
    "app_id": "<application-id>",
    "app_secret": "<application-secret>",
    "org_id": "<org-id>",
}
instance_id = os.environ.get("JMS_INSTANCE_ID") or "orders-node-1"
```

## 3. 首次取得帳號憑據

互動測試時，先進入 `jms_pam_config.py` 所在目錄再啟動 Python，然後執行下方範例。腳本可儲存為同目錄的 `app.py`，以 `python3 app.py` 執行。若出現 `ModuleNotFoundError: No module named 'jms_pam_config'`，請檢查檔名及位置；重新安裝套件不會產生此檔案。

```bash
cd /path/to/your/application
chmod 0600 jms_pam_config.py
python3
```

<!-- python-account-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


with Client(instance_id=instance_id, **client_options) as client:
    account = client.get_account(account_id="<account-id>")
    username = account.username
    password = account.secret
```
<!-- python-account-example:end -->

## 4. 事件監聽與業務切換

實作下方的帳號變更回呼並啟動託管監聽即可。SDK 負責快照、即時取密、帳號與版本校驗、重試、指令認領及結果回報。業務回呼必須完成真實連線驗證、連線池切換與舊連線釋放後再返回。Python、Node.js 使用子類鉤子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未實作業務切換時，範例會明確失敗。

<!-- python-events-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


if not hasattr(Client, 'on_account_changed'):
    raise RuntimeError('Upgrade jms-pam to a version with managed account events')


class ApplicationClient(Client):
    def on_account_changed(self, account):
        # Validate a new connection, switch the pool, then release old connections.
        # Return only after the application uses the account. Never log secrets.
        raise NotImplementedError("Implement application account switching")

    def on_account_revoked(self, account_id):
        # Close the application connections for this account.
        raise NotImplementedError("Implement revoked account cleanup")


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    # SDK handles snapshots, fresh fetches, versions, retries and event results.
    # Applied accounts are reported automatically; pull-only code can use:
    # client.report_accounts([account_id])  # [] clears explicit SDK usage.
    client.watch_events()
```
<!-- python-events-example:end -->

帳號回呼透過有界事件佇列循序執行；一般更新失敗按 1–30 秒退避重試。業務回呼成功返回後才回報成功；結果上傳失敗時，同一次監聽僅重試上傳，不重複已完成的動作。指令先認領再執行，失敗的指令動作不自動重做。已處理帳號自動以完整快照回報使用情況，A/B 切換會替換目前帳號。撤銷範圍需實作連線清理回呼。使用帳號回報、取密或檔案交付本身不會確認業務生效。原始事件流與舊憑據鉤子保留顯式確認。業務處理仍需支援程序重啟後的冪等執行。

### 監聽生命週期與結束

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events 等待停止；start_events 不代表初始同步完成，服務須等待就緒。每個客戶端只有一個高層監聽器。stop_events 與 close 等待鉤子；鉤子可停止自己的客戶端。clone 重建子類別狀態。

### 最新憑據與後端不可用

取密先請求 API。成功取得新憑據後替換本地保留值，舊版本不覆蓋新版本，且不按時間過期。僅在逾時、網路故障或 HTTP 5xx 時，返回相同查詢條件下已取得的最新憑據並標記本地來源；沒有保留值則拋出原始錯誤。SDK 在目前客戶端記憶體內保留，直到更新、撤銷或關閉；clone 與重新啟動不繼承。Agent 使用既有受保護的本地狀態保存。HTTP 401/403/404 清空 SDK 保留值並報錯，成功回應格式錯誤也報錯。明確撤銷移除對應憑據；快照移除授權範圍外項目；設定變更先保留，由後續快照核對範圍。Agent 在 HTTP 同步前先執行明確撤銷或快照授權範圍縮小並保存範圍，後端故障期間或重新啟動後也會阻止相應本地取密。credential_not_found（HTTP 400）同樣清除 SDK 保留值。 HTTP 426 明確報錯並保留憑據，不用快取代替本次請求成功。可選中繼資料介面缺失（HTTP 404）不清理憑據。Agent 純 pull 值不做持久離線保留；SDK 可按原有暫時故障規則使用記憶體保留值。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

啟用高層監聽後，snapshot、credential.updated 自動取密、替換保留值，再呼叫業務函式。刷新失敗保留上一份並重試。Agent 也在更新通知後主動取密，後端故障時保留已有值。刷新與手動切換使用下方即時取得的呼叫；本地保留值不代表剛取得新版本，亦不自動確認輪換。

閒置時每 10 秒傳送應用層 ping，約 30 秒收不到訊息則重連，以 1–30 秒指數退避並重新簽章。快照恢復目前狀態，不重播歷史事件。



## 5. 常用方法

### 列出授權帳號

帳號列表方法查詢應用程式目前授權的 pull 範圍，只回傳帳號、資產及相關 push 策略的中繼資料，不取得密碼或記錄取密；未綁定策略的帳號也會回傳。完整列表方法自動讀取所有分頁；分頁方法回傳 `count` 和 `accounts`（Go 分別回傳列表與總數）。預設 limit 為 200、offset 為 0；limit 為 1–500，search 最長 128 個字元，可搜尋帳號名稱、使用者名稱、資產名稱或地址。重複、不完整或總數變動會報錯；授權變動時重新查詢完整列表。需要密碼時以帳號 ID 呼叫取密方法。範例需要包含新增方法的 SDK 原始碼；已發布的 1.0.2 尚未在 Python、Java、Node.js 提供這些方法。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    accounts = client.list_accounts()
    for account in accounts:
        print(account.id, account.username, account.asset.name)

    page = client.list_accounts_page(limit=20, offset=0, search="database")
    print("Matching accounts:", page.count)
    # Use an authorized account.id with get_account when its secret is needed.
```


### 應用程式、實例與策略資訊

get_application 回傳目前簽章應用程式的 ID、名稱、組織及授權帳號與可見策略數量；get_client_info 只回傳目前副本的登記資訊、類型和版本。策略列表一次回傳全部可見的關聯策略，包括停用策略；search 比對策略名稱或 key。單一策略以策略 ID 或策略本身的 key 查詢，不使用帳號 push key。輪換策略的兩個帳號及目前生效帳號都須有授權；訂閱數量只統計目前有權存取的帳號。查詢只回傳中繼資料，不回傳密鑰或密碼、不取密或確認生效。首次存取會登記副本。需要更新 Core 與 SDK 原始碼；已發布的 1.0.2 尚未包含這些方法。

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id

with Client(instance_id=instance_id, **client_options) as client:
    application = client.get_application()
    print(application.id, application.name, application.accounts_count)
    replica = client.get_client_info()
    print(replica.instance_id, replica.client_version)

    policies = client.list_policies()
    for policy in policies:
        print(policy.key, policy.name, policy.is_active, policy.status)
    if policies:
        policy = client.get_policy(policy_id=policies[0].id)
        # Alternatively: client.get_policy(key=policies[0].key)
        print(policy.name, policy.accounts_count)
```


### 應用狀態回報

回報應用狀態和目前使用的帳號 ID。支援 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次都是完整快照，空帳號清單會清除先前記錄。由應用在狀態變更或自己的心跳中呼叫，不會自動啟動背景回報。Python、Java、Node.js 遇到網路或 API 錯誤時預設回傳 false；Go 使用 `TryReportStatus`。嚴格模式可取得錯誤，無效參數仍會明確提示。呼叫使用設定的 HTTP 逾時，請避開影響業務回應的路徑。回報不會取得密碼、確認輪換或清除憑據快取。`error_code` 僅傳機器可讀錯誤碼，不要傳密碼或例外原文。透過 ClientInfo 查詢最後回報和時間。

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### 底層事件與應用指令

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

list_application_commands 輪詢待處理請求，execute_application_command(event, handler) 申請執行；只有 accepted 為 true 才執行處理函式。切換處理函式驗證請求帳號及版本、套用憑證並確認；重新啟動處理函式需檢查健康狀態，工作完成後才回報成功。失敗回報不會掩蓋原始例外。

舊 key API 保留相容。新整合使用 `get_account` 和 `confirm_event`，並須同步更新 Core。

### API

- `list_accounts(search="")` / `list_accounts_page(limit=200, offset=0, search="")`
- `get_application()` / `get_client_info()`
- `report_accounts(account_ids)` / `report_status("running", account_ids=[...])`
- `list_policies(search="")` / `get_policy(key=...)` / `get_policy(policy_id=...)`
- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `watch_events(stop_event=...)` / `start_events(stop_event=...)` / `stop_events()`
- `watch_credential_events(stop_event=...)`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `list_application_commands()` / `execute_application_command(event, handler)`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

## 6. 接入驗收

1. 使用已授權帳號 ID 取密並驗證真實連線，不記錄密碼。
2. 監聽事件時，核對首次同步及一次策略更新，業務回呼完成切換後才應回報成功。
3. 分別檢查使用帳號、執行狀態和事件結果；取密或使用狀態上報不代表生效確認。

### 問題排查

with 或 close 關閉 HTTP 工作階段和事件串流，clone 建立獨立工作階段。HTTP、網路、驗證及解碼錯誤拋出 PAMError，包含 code、status_code、detail 和 original_error。在應用程式邊界重試暫時故障，明確處理授權拒絕，不記錄憑證。

各 SDK 使用版本 1 協定，Agent 設定格式為版本 1。收到 client_upgrade_required（HTTP 426）時檢查相容性並升級。允許未知可選欄位及通知事件，未實作的策略類型不得套用或確認。SDK 自動傳送接收回執，不能作為憑證已生效的證明。 實例資訊可包含 server_capabilities；缺失或 null 表示未知，未知名稱可保留。能力清單描述伺服器功能，不授予權限。

<!-- sdk-doc:end -->
