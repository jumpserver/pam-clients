# JumpServer PAM Python SDK / Agent

Python 3.9+ 提供憑據策略 SDK。Go jms-pam-agent 無需 Python，可透過本機檔案、固定動作或本機 Socket 交付憑據。Linux、macOS 和 Windows 支援前景執行；內建 systemd 安裝僅支援 Linux。

<!-- agent-doc:start -->

## Go Agent 接入

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


內建安裝需要 Linux/root。macOS、非 root Linux 或 Windows 請在接入精靈中選擇 JSON 或 Socket 交付，依照 init-local 與 run --local --config 命令執行。只初始化一次，後續重複使用產生的私有本機設定；前景模式不執行 systemd 動作。

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

JSON 交付檔案，EnvironmentFile 對接固定 systemd 服務，Unix Socket 提供本地 API。Agent 在交付成功後儲存交付版本，應用程式驗證並使用後才儲存生效版本。Socket 屬於設定的應用程式使用者，權限為 0600，本地請求應以該使用者執行。

本機 rules 設定目標檔案、JSON/EnvironmentFile 或可信範本，以及可選的 systemd reload/restart 或固定可執行腳本。腳本透過標準輸入接收憑據 JSON，參數固定、有逾時，並應在驗證業務生效後回報成功。Core 不能新增腳本路徑或擴大本機能力。修改私有設定後重新啟動 Agent。

下載的引導設定已包含 Agent 身分與交付設定。只需在 rules 中宣告業務使用的帳號 ID，再設定更新業務檔案、生效動作及執行中連線驗證。帳號設定 allow_account_switch 後可沿用同一規則處理 A/B 雙向輪換；可選 credential_check 於修改檔案前驗證新帳號。狀態、事件、Socket 路徑及 300 秒對帳週期皆有預設值。rules 為空時依憑據寫入預設檔案。

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

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```


### 本地 API 與確認

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

### 問題排查

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

Agent 在啟動、相關事件及每 300 秒同步。網路故障保留已取得的最新授權憑據；身分或授權被拒絕時阻止 Socket 取密，成功簽章同步後恢復。已交付檔案保留。SIGINT/SIGTERM 會關閉服務、連線及事件讀取執行緒。

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Python SDK 接入

需要 Python 3.9+。使用應用的 Python 或虛擬環境，從 PyPI 或企業內部鏡像安裝 `jms-pam`，再從接入精靈下載應用身分設定。

```bash
python3 -m pip install jms-pam
```

將 jms_pam_config.py 放在應用程式旁邊。client_options 包含應用程式身分資料，不要提交或寫入日誌。每個副本使用穩定且唯一的 instance_id.

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

### 事件與憑證生效

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

<!-- python-events-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


def apply_account(account):
    # Validate a new connection, switch the pool, then release old connections.
    # Never log account.secret or authentication headers.
    raise NotImplementedError("Implement the application's account update first")


def restart_application():
    raise NotImplementedError("Implement application restart and health check first")


class ApplicationClient(Client):
    def apply_event(self, event):
        if event.get("event") == "application.restart.requested":
            restart_application()
            return
        account = self.get_account(
            account_id=event["account_id"], allow_local_fallback=False,
        )
        if account.revision != event["account_revision"]:
            raise ValueError("The event account version is superseded")
        apply_account(account)

    def handle_event(self, event):
        event_id = event["event_id"]
        if event.get("command_id"):
            claim = self.report_application_command_result(
                command_id=event["command_id"], status="running",
            )
            if not claim.accepted:
                return
        try:
            self.apply_event(event)
        except Exception:
            self.confirm_event(
                event_id=event_id, status="failed", error_code="application_failed",
            )
            raise
        # A fetch or delivery receipt never confirms application success.
        self.confirm_event(event_id=event_id)

    def run(self):
        for event in self.watch_credential_events():
            if event.get("command_id"):
                updates = [event]
            elif event.get("event") == "snapshot":
                updates = event.get("credentials", [])
                # Release application connections absent from the new scope.
            elif event.get("event") == "credential.updated":
                updates = [event]
            elif event.get("event") == "credential.revoked":
                # Release connections for event["account_id"].
                continue
            else:
                continue
            for update in updates:
                try:
                    self.handle_event(update)
                except Exception as error:
                    self.on_event_error(error, update)


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    client.run()
```
<!-- python-events-example:end -->

### 常用方法

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

舊 key API 保留相容。新整合使用 `get_account` 和 `confirm_event`，並須同步更新 Core。


<!-- sdk-doc:end -->
