# JumpServer PAM Python SDK / Agent

Python 3.9+는 자격 증명 정책 SDK를 제공합니다. Go jms-pam-agent는 Python 없이 로컬 파일, 고정 작업 또는 Socket으로 자격 증명을 전달합니다. Linux, macOS, Windows에서 포그라운드 실행을 지원하며 내장 systemd 설치는 Linux 전용입니다.

<!-- agent-doc:start -->

## Go Agent 연동

[Linux x86_64 (amd64)](/download/pam/jms-pam-agent-linux-amd64) · [Linux ARM64](/download/pam/jms-pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# Run in the directory containing the downloaded Agent and SHA256SUMS.
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./jms-pam-agent-linux-${agent_arch}" /usr/local/bin/jms-pam-agent
```


내장 설치에는 Linux와 root가 필요합니다. macOS, 비 root Linux 또는 Windows에서는 접속 마법사에서 JSON 또는 Socket을 선택하고 init-local 및 run --local --config 명령을 실행하세요. 한 번만 초기화하고 생성된 비공개 로컬 설정을 재사용하세요. 포그라운드 모드는 systemd 작업을 수행하지 않습니다.

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

JSON은 파일 전달, EnvironmentFile은 지정된 systemd 서비스, Unix Socket은 로컬 API를 제공합니다. Agent는 전달 성공 후 전달 리비전을 저장하고 애플리케이션은 검증 및 적용 후 적용 리비전을 저장합니다. Socket은 앱 사용자 소유이며 권한은 0600입니다. 해당 사용자로 요청하세요.

로컬 rules에 파일, JSON/EnvironmentFile 또는 신뢰할 수 있는 템플릿과 systemd reload/restart 또는 고정 실행 파일을 설정합니다. 스크립트는 표준 입력으로 자격 증명 JSON을 받고 고정 인수와 제한 시간을 사용하며 적용을 검증한 후 성공합니다. Core는 실행 경로나 권한을 확장할 수 없습니다. 구성을 수정한 후 Agent를 재시작합니다.

다운로드한 설정에는 Agent 인증 정보와 전달 설정이 포함됩니다. rules에 업무에서 사용하는 계정 ID, 설정 갱신, 적용 동작, 실행 중인 연결 확인을 지정합니다. allow_account_switch를 설정하면 동일한 규칙으로 A/B 양방향 교체를 처리합니다. 선택적 credential_check는 파일 변경 전에 새 로그인을 검증합니다. 상태, 이벤트, Socket 경로와 300초 조정 간격에는 기본값이 있습니다. rules가 비어 있으면 자격 증명별 기본 파일을 기록합니다.

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


### 로컬 API 및 확인

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

### 문제 해결

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

Agent는 시작 시, 관련 이벤트 수신 시, 300초마다 동기화합니다. 네트워크 오류는 이미 가져온 최신 승인 자격 증명을 유지합니다. 신원이나 권한 거부 시 Socket 조회를 차단하고 서명된 동기화가 성공하면 복구합니다. 전달된 파일은 유지됩니다. SIGINT/SIGTERM은 서비스, 연결, 읽기 스레드를 종료합니다.

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Python SDK 연동

Python 3.9+가 필요합니다. 애플리케이션의 Python 또는 가상 환경에서 PyPI나 사내 미러로 jms-pam을 설치하고 연결 마법사에서 구성을 다운로드하세요.

```bash
python3 -m pip install jms-pam
```

jms_pam_config.py를 앱이 읽을 수 있는 위치에 두세요. client_options에는 신원 정보가 있으므로 커밋하거나 로그에 쓰지 마세요. 복제본마다 안정적이고 고유한 instance_id를 사용하세요.

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

### 이벤트 및 자격 증명 적용

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

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

### 주요 메서드

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

기존 key API는 호환용으로 유지됩니다. 신규 연동은 `get_account`와 `confirm_event`를 사용하며 Core도 업데이트해야 합니다.


<!-- sdk-doc:end -->
