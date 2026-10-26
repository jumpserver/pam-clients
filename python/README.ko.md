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

`config_update.format`의 기본값은 `auto`이며 생략해도 동일합니다. 확장자 `.txt`, `.env`, `.properties`는 `env`(`KEY=value`), `.yml`, `.yaml`은 `yaml`(최상위 `KEY: value`, 새 값은 따옴표로 감싼 문자열)로 처리합니다. 다른 확장자는 `env` 또는 `yaml`을 명시하세요. 매핑 필드는 이미 존재하며 한 번만 나타나야 하고 다른 설정은 보존됩니다. INI 섹션이나 중첩 YAML은 `files` + `template_file`로 전체 파일을 생성하거나 `script`를 사용하세요. 환경 변수 파일을 갱신한 후에는 애플리케이션이 다시 읽거나 재시작해야 적용됩니다.

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

아래 명령으로 같은 Core에 포함된 클라이언트 소스를 설치하세요. 배포된 1.0.2에는 관리형 콜백이 없습니다. jumpserver.example.com을 Core 주소로 바꾸고 Go 로컬 대체 경로를 포함한 jms-pam-clients 디렉터리를 보관하세요.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

`jms_pam_config.py`는 애플리케이션 설정 파일이며 PyPI의 `jms-pam` 패키지에 포함되지 않습니다. 연결 마법사에서 다운로드하여 애플리케이션 스크립트 옆에 저장하거나 아래 내용으로 파일을 만들고 실제 애플리케이션 정보로 바꾸세요. `client_options`를 커밋하거나 로그에 기록하지 마세요. 복제본마다 안정적이고 고유한 `instance_id`를 사용하세요.

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

대화형 테스트는 `jms_pam_config.py`가 있는 디렉터리에서 Python을 시작한 후 아래 예제를 실행하세요. 스크립트는 같은 디렉터리에 `app.py`로 저장하고 `python3 app.py`로 실행하세요. `ModuleNotFoundError: No module named 'jms_pam_config'`가 발생하면 파일 이름과 위치를 확인하세요. 패키지를 다시 설치해도 이 파일은 생성되지 않습니다.

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

### 허가된 계정 목록

목록 메서드는 애플리케이션의 현재 pull 권한 범위를 조회합니다. 계정, 자산 및 관련 push 정책 메타데이터만 반환하며 암호를 가져오거나 자격 증명 조회를 기록하지 않습니다. 정책이 없는 계정도 포함됩니다. 전체 목록은 모든 페이지를 읽고 페이지 메서드는 `count`와 `accounts`를 반환합니다(Go는 목록과 총수를 별도로 반환). 기본 limit은 200, offset은 0이며 limit은 1–500, search는 최대 128자로 계정 이름, 사용자 이름, 자산 이름 및 주소를 검색합니다. 중복, 불완전한 결과 또는 총수 변경은 오류를 반환합니다. 권한이 변경되면 전체 목록을 다시 조회하세요. 암호가 필요하면 계정 ID로 조회 메서드를 호출하세요. 예제에는 새 메서드가 포함된 SDK 소스가 필요합니다. 배포된 1.0.2의 Python, Java, Node.js에는 아직 제공되지 않습니다.

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


### 애플리케이션, 인스턴스 및 정책 정보

get_application은 서명 애플리케이션의 ID, 이름, 조직 및 허가된 계정과 보이는 정책 수를 반환합니다. get_client_info는 현재 복제본의 등록 정보, 유형 및 버전만 반환합니다. 정책 목록은 비활성 정책을 포함한 보이는 연결 정책 전체를 반환하며 search는 이름이나 key를 검색합니다. 개별 정책은 정책 ID 또는 정책 자체의 key 중 하나로 조회하며 계정 push key를 사용하지 않습니다. 순환 정책은 두 계정과 현재 활성 계정의 권한이 필요하고 구독 수는 현재 허가된 계정만 계산합니다. 비밀이나 암호를 반환하거나 자격 증명을 조회하고 적용 확인하지 않습니다. 최초 접근 시 복제본이 등록됩니다. 업데이트된 Core와 SDK 소스가 필요하며 배포된 1.0.2에는 아직 없습니다.

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


### 이벤트 및 자격 증명 적용

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

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

### 주요 메서드

- `list_accounts(search="")`
- `get_application()` / `get_client_info()`
- `report_status("running", account_id=...)` / `report_status("stopped")`
- `list_policies(search="")` / `get_policy(key=...)` / `get_policy(policy_id=...)`
- `list_accounts_page(limit=200, offset=0, search="")` → `page.count`, `page.accounts`
- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

기존 key API는 호환용으로 유지됩니다. 신규 연동은 `get_account`와 `confirm_event`를 사용하며 Core도 업데이트해야 합니다.

### 애플리케이션 상태 보고

실행 상태와 사용 중인 계정 ID를 전체 스냅샷으로 보고합니다. 상태: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. 빈 목록은 이전 사용 기록을 지웁니다. 상태 변경이나 애플리케이션의 하트비트에서 호출합니다. Python, Java, Node.js는 네트워크/API 실패 시 false를 반환하며 Go는 `TryReportStatus`를 제공합니다. 엄격한 호출은 오류 정보를 제공합니다. 잘못된 인수는 거부됩니다. 설정된 HTTP 제한 시간을 사용합니다. 보고는 비밀 조회, 교체 확인, 캐시 무효화를 수행하지 않습니다. `error_code`에 비밀이나 예외 메시지를 넣지 마세요. ClientInfo에서 마지막 보고와 시간을 조회할 수 있습니다.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


<!-- sdk-doc:end -->
