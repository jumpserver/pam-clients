# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ 提供憑據策略 SDK。Go pam-agent 無需 Python，可透過本機檔案、固定動作或本機 Socket 交付憑據。Linux、macOS 和 Windows 支援前景執行；內建 systemd 安裝僅支援 Linux。

<!-- agent-doc:start -->

# JumpServer PAM Agent 接入

在應用主機執行獨立 PAM Agent，無需 Python。依序完成安裝、選擇交付方式、設定業務動作和驗收。

## 1. 接入前提與流程

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. 在應用接入精靈下載身分設定並保護 AK/SK，`instance_id` 可省略。

## 2. 安裝與啟動

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/pam-agent-linux-amd64) · [Linux ARM64](/download/pam/pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# 在下載的 PAM Agent 和 SHA256SUMS 所在目錄執行。
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./pam-agent-linux-${agent_arch}" /usr/local/bin/pam-agent
```

在下載的 `pam-agent.json` 所在目錄執行。下方 Linux 服務命令需要 root 與 systemd。前台執行時使用符合主機系統與架構的執行檔。

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### 前台執行（Linux / macOS / Windows）

macOS、Windows 或非 root Linux 先執行一次 `init-local`，再以 `run --local` 重用私有設定。支援 JSON 和環境變數檔案；systemd 動作只適用 Linux 服務模式。

前台二進位檔命名為 `pam-agent`（Windows 為 `pam-agent.exe`）；Linux/macOS 先執行 `chmod 0755 ./pam-agent`。

```bash
chmod 0600 ./pam-agent.json
./pam-agent init-local \
  --bootstrap ./pam-agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app"
./pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\pam-agent.exe init-local --bootstrap (Join-Path $PWD 'pam-agent.json') --directory $agentDir
.\pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. 設定範例

下方為 Linux/systemd 完整設定範例，包含應用身分、拉取週期、標準憑據檔案、本機 API 與業務服務。請替換應用身分、帳號 ID、系統使用者、檔案路徑、服務名稱與檢查指令碼，並預先部署指令碼。僅需檔案交付或本機 API 時，將 `services` 設為 `[]`。後續章節分別說明各區塊，全部屬於同一份設定檔。

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "sample-app",
      "allow_account_switch": true,
      "update_on_notification": true,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ],
      "update_config": {
        "file": "/etc/sample-app/config.yml",
        "format": "yaml",
        "fields_map": {
          "DB_USER": "username",
          "DB_PASSWORD": "secret"
        }
      },
      "reload_service": {
        "unit": "sample-app.service",
        "operation": "restart"
      },
      "app_check": {
        "path": "/usr/local/libexec/jms-pam/check-running-db",
        "confirm_on_success": true
      }
    }
  ]
}
```

`instance_id` 可省略或留空，PAM Agent 會產生 `<hostname>-<4 位隨機十六進位字元>`，例如 `orders-pod-a1b2`。明確設定或傳入 `--instance-id` 時優先使用指定值。安裝與 `init-local` 將 ID 儲存至輸出設定；直接執行時儲存至私有狀態檔，重新啟動後重用，不改寫輸入設定。K8s 多副本可共用省略此欄位的設定，每個副本須有獨立、可寫入的狀態目錄；無保留狀態的新副本會產生新 ID。

`pull_interval` 是全域拉取週期，單位為秒，必須為正整數，省略時預設 300。PAM Agent 啟動時立即同步，之後按週期核對帳號與憑據版本，僅在版本或目前帳號變更時取密。`update_on_notification: false` 的業務在下次定時拉取時更新；通知不會延後該週期。

## 4. store

`store` 將憑據儲存為標準 JSON 或環境變數檔案，供應用透過檔案讀取。它決定儲存哪些帳號、輸出目錄、格式及擁有者。修改既有業務設定或重新載入服務，應在 `services` 中設定。

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  }
}
```

| 設定與執行 | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | 只儲存明確宣告的帳號 ID。 |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

每個帳號按實際 account_id 個別產生檔案，A/B 也分開儲存；檔案含明文憑據，權限為 0600。正在改密的 A/B 非目前帳號暫不可用。成功核對授權後清理撤銷帳號的檔案。版本未變時只核對中繼資料，不重複取密；實際取密仍記錄稽核及輪換存取觀察。

## 5. socket

`socket` 設定本機 Unix Socket 介面，提供健康狀態、帳號清單、按需取密、帳號使用情況回報及業務結果確認。未設定 `store` 也可使用 Socket。取密範圍由 Core 中的應用授權決定；`store.accounts` 僅選擇輸出檔案的帳號，不限制 API 的帳號範圍。

`path` 預設為 `/run/jms-pam-agent/agent.sock`。`owner` 控制 Socket 存取及業務輸出檔案的預設擁有者，省略時依序使用 `store.owner`、PAM Agent 執行使用者。省略整個 `socket` 區塊仍會使用預設介面，不會停用。`init-local` 將 Socket 設為 `<directory>/run/agent.sock`，擁有者為目前使用者。

非 root 執行時，可省略 `store.owner` 與 `socket.owner`，預設使用目前使用者；`init-local` 會自動將兩者設為目前使用者。以 root 執行且業務使用者需要讀取檔案或存取 Socket 時，再指定該使用者。`owner` 不會切換 PAM Agent 程序的執行使用者。

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### 查詢帳號與憑據

CLI 和 Socket 請以設定的應用使用者執行。`get_accounts` 僅列出授權帳號中繼資料；`get_secret` 按需取密，也適用於未訂閱 push 的帳號。密碼會寫到標準輸出，請在私人終端調試。前台模式請替換實際 Socket 路徑。

```bash
pam-agent get_accounts
pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credential?account_id=<account-id>'
```

### 確認業務生效

業務驗證真實連線並完成切換後再確認事件。可用 `app_check` 成功後以 `confirm_on_success: true` 自動確認，或由業務驗證後顯式確認；同步檢查腳本不要反向呼叫確認介面。結果先持久化，`confirmed` 表示 Core 已接受，`pending` 會重試。

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. services

將下方業務物件加入 `services`，各業務使用唯一且穩定的 `name`。service 層的 `allow_account_switch` 預設 false；開啟後，有明確 A/B 關聯者跟隨目前帳號，其他帳號保持固定，只需宣告 A。`update_on_notification` 也在 service 層，預設 true；false 會等定時同步再執行業務動作，預設 300 秒，儲存及授權撤銷不受影響。各業務獨立記錄版本；所有相關業務檢查成功且設定 `app_check.confirm_on_success: true` 後才自動確認輪換。

業務依 `update_config` → `reload_service` → `app_check` 執行：更新業務設定、重新載入或重啟服務（亦可執行固定腳本）、驗證運行中的業務。新欄位名稱需要新版 PAM Agent；`services` 的這三個步驟僅接受新名稱，舊欄位名稱會報錯。

| 設定與執行 | 更新業務配置與 A/B 輪換 |
| --- | --- |
| `credential_check` | 可選：修改檔案前驗證新帳號連線。 |
| `update_config` | 透過欄位映射、範本或腳本更新業務檔案。 |
| `reload_service` | 重新載入或重新啟動固定服務，或執行固定腳本。 |
| `app_check` | 驗證運行中業務已使用新連線，再確認生效。 |

`services`:

```json
[
  {
    "name": "sample-app",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "file": "/etc/sample-app/config.yml",
      "format": "yaml",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    },
    "app_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

### accounts

`services.accounts` 用 `id` 指定帳號。單帳號可直接使用 `username` / `secret`。多帳號必須使用唯一 alias，以 `database.username`、`cache.secret` 指定來源，A/B 切換不改 alias。宣告中的可選 `username` 只供閱讀，實際值取自 Core。全部帳號就緒才更新檔案並執行一次業務動作；缺少帳號或對應不明時，在寫入前報錯。

```json
{
  "name": "sample-app",
  "allow_account_switch": true,
  "update_on_notification": true,
  "accounts": [
    {
      "id": "<primary-account-id>",
      "alias": "database"
    },
    {
      "id": "<cache-account-id>",
      "alias": "cache"
    }
  ],
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_USER": "cache.username",
      "CACHE_PASSWORD": "cache.secret"
    }
  },
  "reload_service": {
    "unit": "sample-app.service",
    "operation": "restart"
  },
  "app_check": {
    "path": "/usr/local/libexec/jms-pam/check-running-db",
    "confirm_on_success": true
  }
}
```

### update_config

`services` 中每個物件代表一個業務服務。`update_config` 三選一：`file + fields_map` 修改既有欄位、`files + template_file` 依範本產生完整檔案、`script` 執行自訂更新。以下片段替換該服務的 `update_config`，每個服務僅選一種。

#### file + fields_map

`update_config.format` 預設為 `auto`，省略與明確填寫的效果相同。依副檔名辨識：`.txt`、`.env`、`.properties` 使用 `env`，替換 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替換頂層 `KEY: value`，新值寫為帶引號的字串。其他副檔名需明確指定 `env` 或 `yaml`。映射欄位必須已存在且只出現一次，其他設定保留。INI section、巢狀 YAML 等複雜格式使用 `files` + `template_file` 完整產生，或用 `script` 自訂替換。環境變數檔案更新後，應用仍需重新讀取檔案或重新啟動才會生效。

```json
{
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "username",
      "DB_PASSWORD": "secret"
    }
  }
}
```

#### files + template_file

範本適用於完整產生巢狀 JSON、INI 等設定。設定 `files[].format` 為 `template`，`template_file` 為本機絕對路徑。先渲染再原子替換檔案，權限 0600；省略 `owner` 時使用 `socket.owner`。`json` 函數處理 JSON 引號與跳脫字元。

```json
{
  "update_config": {
    "files": [
      {
        "path": "/etc/sample-app/credentials.json",
        "format": "template",
        "template_file": "/etc/pam-agent/templates/credentials.json.tmpl",
        "owner": "appuser"
      }
    ]
  }
}
```

`/etc/pam-agent/templates/credentials.json.tmpl`:

```gotemplate
{
  "database": {
    "username": {{json (index .Credentials "<primary-account-id>").Username}},
    "password": {{json (index .Credentials "<primary-account-id>").Secret}}
  }
}
```

範本 `.Credentials` 與指令碼 `credentials` 均以宣告的 `services.accounts[].id` 為鍵，不能使用 alias。請替換 `<primary-account-id>`。A/B 切換後鍵仍是宣告的 A ID，值中 `AccountID`（指令碼為 `account_id`）指出目前生效帳號。

#### script

以 `target` 宣告一個輸出路徑，或以 `targets` 宣告多個，供修改前備份。`script.path` 必須是預先部署的本機執行檔；`args` 僅放固定參數，憑據從標準輸入以 JSON 傳入。`timeout_seconds` 為 1–300 秒，預設 120。指令碼以 PAM Agent 使用者執行；退出碼 0 成功，非 0 或逾時會失敗並重試，因此須可重複執行。

```json
{
  "update_config": {
    "target": "/etc/sample-app/credentials.json",
    "script": {
      "path": "/usr/local/libexec/pam-agent/update-credentials",
      "args": [
        "/etc/sample-app/credentials.json"
      ],
      "timeout_seconds": 30
    }
  }
}
```

標準輸入範例（節錄）：

```json
{
  "event": "credentials.updated",
  "credentials": {
    "<primary-account-id>": {
      "account_id": "<active-account-id>",
      "username": "db_app",
      "secret": "<password>",
      "event_id": "<event-id>"
    }
  }
}
```

以下 Linux/macOS Python 3 範例更新既有 JSON 的 database 欄位，保留其他設定與擁有者，以 0600 原子替換。預先建立目錄與初始 JSON，部署指令碼並授予執行權限。自訂指令碼需自行處理輸出格式、權限及擁有者。

`/usr/local/libexec/pam-agent/update-credentials`:

```python
#!/usr/bin/env python3
import json
import os
import sys
import tempfile
from pathlib import Path

payload = json.load(sys.stdin)
credential = payload["credentials"]["<primary-account-id>"]
target = Path(sys.argv[1])
original = target.stat()
with target.open(encoding="utf-8") as source:
    config = json.load(source)
config.setdefault("database", {}).update(
    username=credential["username"], password=credential["secret"]
)

fd, temporary = tempfile.mkstemp(prefix=".pam-agent-", dir=target.parent)
try:
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o600)
        os.fchown(output.fileno(), original.st_uid, original.st_gid)
        json.dump(config, output, ensure_ascii=False)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, target)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
```

範本或指令碼成功僅代表設定更新完成。需要重新載入或業務驗證時，在同一服務設定 `reload_service`、`app_check`。先檢查設定，再重新啟動 PAM Agent：

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile 將使用者名稱、密碼和帳號中繼資料以明文 `KEY=value` 寫入私有 `.env` 檔，權限為 `0600`。業務 systemd 服務須明確配置 `EnvironmentFile=檔案路徑`，啟動時載入該服務的進程環境，不會設定主機全域環境。檔案更新後須重新啟動服務；僅 reload 不會取代已執行進程的環境變數。

`services`:

```json
[
  {
    "name": "sample-app-env",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "files": [
        {
          "path": "/etc/sample-app/database.env",
          "format": "environment",
          "owner": "appuser"
        }
      ]
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    }
  }
]
```

```ini
JMS_PAM_ACCOUNT_ID="<account-id>"
JMS_PAM_USERNAME="db_user"
JMS_PAM_SECRET="<password>"
```

```ini
[Service]
EnvironmentFile=/etc/sample-app/database.env
```

## 7. 接入驗收

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### 問題排查

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent 在啟動、相關通知及定時週期同步，預設 300 秒。暫時網路故障保留仍獲授權的憑據。明確撤銷或停用應用會封鎖本機取密並刪除對應標準檔案，即使後續刷新失敗。業務設定檔由應用管理員管理。連續通知不會推遲定時業務更新。SIGINT/SIGTERM 會關閉服務及事件讀取執行緒。


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 接入

## 1. 接入前提與流程

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
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

SDK 取密先請求 API，成功後替換保留值，不讓舊版本覆蓋新版本。只有逾時、網路錯誤或 HTTP 5xx 可回退到相同條件下已取得的值並標記本機來源；保留值不按時間過期。SDK 使用用戶端私有記憶體，clone 和重新啟動從空狀態開始。HTTP 401/403/404 或 credential_not_found（HTTP 400）會清空憑據並報錯。HTTP 426、成功回應格式錯誤不使用回退；可選中繼資料介面缺失不清空憑據。PAM Agent 在受保護狀態保留 store/services 宣告的帳號，未宣告的按需查詢不自動持久化。明確撤銷在 HTTP 刷新前生效，離線也相同；push 快照不代表儲存帳號的完整授權，成功對帳會核對目前應用授權。

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

啟用高層監聽後，snapshot、credential.updated 自動取密、替換保留值，再呼叫業務函式。刷新失敗保留上一份並重試。PAM Agent 也在更新通知後主動取密，後端故障時保留已有值。刷新與手動切換使用下方即時取得的呼叫；本地保留值不代表剛取得新版本，亦不自動確認輪換。

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

各 SDK 使用版本 1 協定，PAM Agent 設定格式為版本 1。收到 client_upgrade_required（HTTP 426）時檢查相容性並升級。允許未知可選欄位及通知事件，未實作的策略類型不得套用或確認。SDK 自動傳送接收回執，不能作為憑證已生效的證明。 實例資訊可包含 server_capabilities；缺失或 null 表示未知，未知名稱可保留。能力清單描述伺服器功能，不授予權限。

<!-- sdk-doc:end -->
