# JumpServer PAM Python SDK / Agent

Python 3.9+는 자격 증명 정책 SDK를 제공합니다. Go jms-pam-agent는 Python 없이 로컬 파일, 고정 작업 또는 Socket으로 자격 증명을 전달합니다. Linux, macOS, Windows에서 포그라운드 실행을 지원하며 내장 systemd 설치는 Linux 전용입니다.

<!-- agent-doc:start -->

# Go Agent 연동

애플리케이션 호스트에서 독립 Go Agent를 실행합니다. Python은 필요하지 않습니다. 설치, 전달 설정, 업무 동작, 검증 순서로 진행합니다.

## 1. 연동 사전 조건과 흐름

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Download the identity configuration from the application access wizard. Give each replica a stable, unique instance ID and protect the AK/SK.

## 2. 설치 및 시작

### Linux / systemd

Core와 Agent의 프로토콜 버전 및 `config_version`은 1입니다. `store`와 `services`를 독립적으로 설정하고 기존 `delivery` / `rules` 구조와 혼합하지 마세요. Core는 계정 범위와 버전 메타데이터만 동기화하며 로컬 실행 경로를 전달하지 않습니다. 비공개 설정 변경 후 Agent를 재시작하세요.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

다운로드한 `jms_pam_agent.json` 디렉터리에서 실행합니다. Linux 서비스는 root와 systemd가 필요합니다. 복제본별 ID를 변경하세요. 전경 모드는 해당 OS 바이너리가 필요하며 macOS / Windows는 소스 빌드가 필요할 수 있습니다.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### 전경 실행

macOS, Windows 또는 비 root Linux는 `init-local`을 한 번 실행한 뒤 `run --local`로 비공개 설정을 재사용합니다. JSON 및 환경 변수 파일을 지원합니다. systemd 동작은 Linux 서비스 전용입니다.

바이너리를 `jms-pam-agent`(Windows: `jms-pam-agent.exe`)로 저장하고 Linux/macOS에서 `chmod 0755 ./jms-pam-agent`를 실행합니다.

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app" --instance-id app-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id app-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. store / local_api

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "config_version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "app-node-1",
  "pull_interval": 300,
  "local_api": {
    "owner": "appuser"
  },
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "services": []
}
```

`pull_interval`은 전체 폴링 주기이며 단위는 초입니다. 양의 정수를 사용하고 생략하면 기본값은 300입니다. Agent는 시작 즉시 동기화한 뒤 이 주기로 계정과 자격 증명 버전을 확인하며, 버전이나 활성 계정이 변경된 경우에만 비밀 값을 가져옵니다. `update_on_notification: false`인 서비스는 다음 폴링 시 업데이트되며 알림은 이 주기를 늦추지 않습니다.

| 설정 및 실행 | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | 명시적으로 선언한 계정 ID만 저장. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

실제 account_id별로 파일을 만들며 A/B도 별도로 저장합니다. 평문 자격 증명 파일의 권한은 0600입니다. 암호 변경 중인 비활성 계정은 일시적으로 사용할 수 없습니다. 권한 확인 후 취소된 파일을 삭제합니다. `local_api.owner`는 Socket과 업무 출력의 기본 소유자이며, 생략 시 `store.owner`, 다음으로 Agent 실행 사용자를 사용합니다. Socket과 파일 저장은 독립적입니다. 버전이 같으면 메타데이터만 확인하며, 실제 암호 조회는 감사 및 교체 접근 관찰에 기록합니다.

### EnvironmentFile / systemd

EnvironmentFile은 사용자 이름, 암호 및 메타데이터를 평문 `KEY=value`로 전용 `.env` 파일(0600)에 저장합니다. 업무 systemd 서비스에 `EnvironmentFile=`을 설정하면 시작 시 해당 프로세스 환경으로 읽습니다. 호스트 전역 환경을 바꾸지 않습니다. 갱신 후 서비스를 재시작해야 하며 reload만으로 실행 중인 프로세스 환경이 바뀌지 않습니다.

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

## 4. 애플리케이션 설정 변경 및 A/B 순환

아래 업무 객체를 `services`에 추가하고 고유하고 안정적인 `name`을 지정합니다. service의 `allow_account_switch`는 기본 false입니다. true이면 관련 A/B의 현재 계정을 따르고 관련 없는 계정은 고정합니다. A만 선언하면 됩니다. `update_on_notification`도 service에 두며 기본 true입니다. false이면 다음 정기 동기화(기본 300초)에 업무를 갱신합니다. 저장 및 권한 취소 처리는 계속됩니다. 업무별 버전을 기록하며 모든 관련 업무의 검증 성공과 `app_check.confirm_on_success: true`가 충족되어야 교체를 자동 확인합니다.

설정은 `update_config` → `reload_service` → `app_check` 순으로 업무 설정을 갱신하고, 서비스를 reload/restart하거나 고정 스크립트를 실행한 뒤 실행 중인 애플리케이션을 검증합니다. 업데이트된 Agent가 필요합니다. `services`의 이 세 단계는 새 이름만 허용하며 기존 이름은 오류로 처리됩니다.

| 설정 및 실행 | 애플리케이션 설정 변경 및 A/B 순환 |
| --- | --- |
| `credential_check` | 선택: 파일 변경 전에 새 계정 연결을 검증합니다. |
| `update_config` | 필드 매핑, 템플릿 또는 스크립트로 업무 파일을 변경합니다. |
| `reload_service` | 고정 서비스 reload/restart 또는 고정 스크립트를 실행합니다. |
| `app_check` | 실행 중인 업무 연결을 검증한 후 적용을 확인합니다. |

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

`services.accounts`에서 `id`로 계정을 지정합니다. 단일 계정은 `username` / `secret`을 직접 사용할 수 있습니다. 여러 계정은 고유 alias와 `database.username`, `cache.secret` 같은 참조가 필요합니다. A/B 전환에도 alias는 유지됩니다. 선택 사항인 `username`은 표시용이며 실제 값은 Core에서 가져옵니다. 모든 계정이 준비된 후 파일을 갱신하고 업무 동작을 한 번 실행합니다. 누락되거나 모호한 매핑은 쓰기 전에 실패합니다.

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

### 설정 파일 형식

`update_config.format`의 기본값은 `auto`이며 생략해도 동일합니다. 확장자 `.txt`, `.env`, `.properties`는 `env`(`KEY=value`), `.yml`, `.yaml`은 `yaml`(최상위 `KEY: value`, 새 값은 따옴표로 감싼 문자열)로 처리합니다. 다른 확장자는 `env` 또는 `yaml`을 명시하세요. 매핑 필드는 이미 존재하며 한 번만 나타나야 하고 다른 설정은 보존됩니다. INI 섹션이나 중첩 YAML은 `files` + `template_file`로 전체 파일을 생성하거나 `script`를 사용하세요. 환경 변수 파일을 갱신한 후에는 애플리케이션이 다시 읽거나 재시작해야 적용됩니다.

Core와 Agent의 프로토콜 버전 및 `config_version`은 1입니다. `store`와 `services`를 독립적으로 설정하고 기존 `delivery` / `rules` 구조와 혼합하지 마세요. Core는 계정 범위와 버전 메타데이터만 동기화하며 로컬 실행 경로를 전달하지 않습니다. 비공개 설정 변경 후 Agent를 재시작하세요.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. 로컬 API 및 확인

### 계정 및 자격 증명 조회

설정한 애플리케이션 사용자로 CLI 및 Socket을 사용합니다. `get_accounts`는 메타데이터만, `get_secret`는 pull 전용 계정도 조회합니다. 암호를 표준 출력하므로 비공개 터미널에서 사용하고 전경 모드에서는 Socket 경로를 변경합니다.

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credential?account_id=<account-id>'
```

### 애플리케이션 적용 확인

실제 연결 검증 및 전환 후 확인합니다. `app_check`와 `confirm_on_success: true` 또는 명시적 확인을 사용합니다. 동기 검사에서 확인 API를 호출하지 마세요. 결과는 저장되며 `confirmed`는 Core 수락, `pending`은 재시도입니다.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 연동 검증

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### 문제 해결

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent는 시작, 관련 알림, 정기 주기(기본 300초)에 동기화합니다. 일시 장애 시 허가된 값을 보관합니다. 명시적 권한 취소 또는 앱 비활성화는 갱신 실패 시에도 로컬 읽기를 차단하고 표준 저장 파일을 삭제합니다. 업무 설정 파일은 관리자가 관리합니다. 연속 알림은 정기 업무 갱신을 연기하지 않습니다. SIGINT/SIGTERM은 서버와 이벤트 읽기를 종료합니다.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 연동

## 1. 연동 사전 조건과 흐름

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

pull만 사용하면 첫 조회까지만 진행합니다. push나 A/B는 업무 콜백과 수신 시작이 필요합니다. SDK가 프로토콜과 결과를 처리하고 업무가 실제 연결을 전환합니다. 예제는 현재 소스용입니다. 맞는 Core 및 SDK 빌드를 사용하세요. 배포된 1.0.2에는 모든 새 API가 포함되지 않습니다.

## 2. SDK 설치 및 인증 설정

아래 명령으로 같은 Core에 포함된 클라이언트 소스를 설치하세요. 배포된 1.0.2에는 관리형 콜백이 없습니다. jumpserver.example.com을 Core 주소로 바꾸고 Go 로컬 대체 경로를 포함한 jms-pam-clients 디렉터리를 보관하세요.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### 로컬 소스 설치 (대안)

Core가 소스 다운로드 API를 제공하지 않거나 오프라인 배포 시 호환 소스를 `/path/to/pam-clients`에 준비하고 업무 디렉터리에서 실행합니다. 실제 경로로 바꾸세요.

```bash
python3 -m pip install /path/to/pam-clients/python
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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
```

## 3. 첫 계정 자격 증명 조회

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

## 4. 이벤트 처리기

아래 계정 변경 콜백을 구현하고 관리형 리스너를 시작하세요. SDK가 스냅샷, 실시간 조회, 계정과 버전 검증, 재시도, 명령 실행 권한 획득과 결과 보고를 처리합니다. 실제 연결 검증, 연결 풀 전환 및 이전 연결 해제를 완료한 뒤 반환하세요. Python과 Node.js는 하위 클래스, Go는 OnAccountChanged, Java는 AccountEventListener를 사용합니다. 미구현 예제는 실패합니다.

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

콜백은 제한된 큐에서 순차 실행되며 일반 업데이트 실패 시 1–30초 간격으로 재시도합니다. 업무 콜백 완료 후에만 성공을 보고하며 결과 업로드 실패는 같은 리스너에서 완료된 작업을 반복하지 않습니다. 명령은 실행 권한을 얻은 뒤 실행하며 실패한 명령 작업은 자동 반복하지 않습니다. 사용 계정은 자동 보고되고 A/B 전환 시 현재 계정을 교체합니다. 범위 제거 시 연결 해제 콜백을 구현하세요. 사용 보고, 조회 또는 파일 전달 자체는 업무 성공을 확인하지 않습니다. 저수준 API와 기존 훅은 명시적 확인을 유지합니다. 재시작 후에도 멱등성을 보장하세요.

### 수신 수명 주기 및 종료

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events는 중지까지 기다립니다. start_events는 초기 동기화 완료를 보장하지 않으므로 준비 상태를 기다리세요. 클라이언트당 감시 하나만 허용됩니다. stop_events와 close는 처리 종료를 기다립니다. 훅 안에서도 중지할 수 있습니다. clone은 서브클래스 상태를 초기화합니다.

### 최신 자격 증명과 백엔드 장애

SDK는 API를 먼저 요청하며 성공 시 보관 값을 갱신하고 이전 버전으로 돌아가지 않습니다. 시간 초과, 네트워크 오류, HTTP 5xx에서만 같은 선택 조건의 기존 값을 로컬 출처로 반환할 수 있습니다. 보관 값에는 시간 만료가 없고 SDK clone 및 재시작 시 메모리는 비워집니다. HTTP 401/403/404 또는 credential_not_found(400)는 보관 값을 지우고 실패합니다. 426과 잘못된 성공 응답은 대체 값을 사용하지 않으며 선택적 메타데이터 API 부재는 값을 지우지 않습니다. Agent는 store/services에 선언된 계정을 보호된 상태에 보관하며, 선언되지 않은 즉시 조회를 자동 저장하지 않습니다. 명시적 취소는 오프라인에서도 HTTP 갱신 전에 적용됩니다. push 스냅샷은 전체 저장 권한을 나타내지 않으며 동기화 성공 시 현재 애플리케이션 권한을 확인합니다.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

관리된 감시를 켜면 snapshot과 credential.updated가 자동 조회하고 보유 값을 교체한 뒤 업무 훅을 호출합니다. 실패하면 이전 값을 유지하고 재시도합니다. Agent도 업데이트 알림으로 조회하며 장애 시 이전 값을 유지합니다. 업데이트와 수동 전환에는 아래 API 조회 필수 호출을 사용하세요. 보유 값은 새로 조회한 버전이 아니며 회전을 자동 확인하지 않습니다.

유휴 시 10초마다 ping을 보내고 약 30초간 메시지가 없으면 재연결합니다. 1–30초 지수 백오프와 새 서명을 사용합니다. snapshot은 현재 상태를 복원하며 과거 이벤트를 재생하지 않습니다.



## 5. 주요 메서드

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


### 애플리케이션 상태 보고

실행 상태와 사용 중인 계정 ID를 전체 스냅샷으로 보고합니다. 상태: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. 빈 목록은 이전 사용 기록을 지웁니다. 상태 변경이나 애플리케이션의 하트비트에서 호출합니다. Python, Java, Node.js는 네트워크/API 실패 시 false를 반환하며 Go는 `TryReportStatus`를 제공합니다. 엄격한 호출은 오류 정보를 제공합니다. 잘못된 인수는 거부됩니다. 설정된 HTTP 제한 시간을 사용합니다. 보고는 비밀 조회, 교체 확인, 캐시 무효화를 수행하지 않습니다. `error_code`에 비밀이나 예외 메시지를 넣지 마세요. ClientInfo에서 마지막 보고와 시간을 조회할 수 있습니다.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### 저수준 이벤트 및 애플리케이션 명령

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

list_application_commands로 대기 요청을 조회하고 execute_application_command(event, handler)로 실행을 신청하세요. accepted가 true일 때만 처리합니다. 전환 처리는 요청한 계정/리비전을 확인하고 적용 및 확인하며 재시작 처리는 재시작과 상태 검증 후 성공을 보고합니다. 실패 보고 오류는 원래 처리 예외를 가리지 않습니다.

기존 key API는 호환용으로 유지됩니다. 신규 연동은 `get_account`와 `confirm_event`를 사용하며 Core도 업데이트해야 합니다.

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

## 6. 연동 검증

1. 승인 ID로 조회하고 실제 연결을 검증합니다.
2. 수신 시 초기 동기화와 정책 업데이트를 확인하고 업무 콜백 완료 후 성공 결과를 확인합니다.
3. 사용 상태와 이벤트 결과를 따로 확인합니다. 조회나 상태 보고는 적용 확인이 아닙니다.

### 문제 해결

with 또는 close로 HTTP 세션과 이벤트 스트림을 닫고 clone으로 독립 세션을 만듭니다. HTTP, 네트워크, 인증, 해석 오류는 code, status_code, detail, original_error가 있는 PAMError로 반환됩니다. 일시 오류를 재시도하고 권한 거부를 명확하게 처리하며 자격 증명을 기록하지 마세요.

모든 SDK는 프로토콜 버전 1을, Agent 설정은 스키마 버전 1을 사용합니다. client_upgrade_required(HTTP 426)를 받으면 호환성을 확인하고 업그레이드하세요. 알 수 없는 선택 필드와 알림 이벤트는 허용하지만 지원하지 않는 정책은 적용하거나 확인하면 안 됩니다. 수신 확인은 자동 전송되며 자격 증명 적용을 증명하지 않습니다. 클라이언트 정보에는 선택적 server_capabilities가 포함될 수 있습니다. 누락 또는 null은 알 수 없음을 의미하며 새 이름을 허용합니다. 서버 기능 목록이며 권한을 부여하지 않습니다.

<!-- sdk-doc:end -->
