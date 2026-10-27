# JumpServer PAM Python SDK / Agent

Python 3.9+는 자격 증명 정책 SDK를 제공합니다. Go jms-pam-agent는 Python 없이 로컬 파일, 고정 작업 또는 Socket으로 자격 증명을 전달합니다. Linux, macOS, Windows에서 포그라운드 실행을 지원하며 내장 systemd 설치는 Linux 전용입니다.

<!-- agent-doc:start -->

# Go Agent 연동

애플리케이션 호스트에서 독립 Go Agent를 실행합니다. Python은 필요하지 않습니다. 설치, 전달 설정, 업무 동작, 검증 순서로 진행합니다.

## 1. 연동 사전 조건과 흐름

1. 애플리케이션을 만들고 조회할 계정을 승인합니다(pull). 사용자 이름이나 정책 ID 대신 계정 ID를 사용합니다.
2. 자동 전달이 필요하면 알림 또는 A/B 순환 정책을 연결합니다(push).
3. 연동 마법사에서 인증 설정을 받고 복제본마다 고유하고 고정된 `instance_id`를 사용하며 AK/SK를 보호합니다.

## 2. 설치 및 시작

### Linux / systemd

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

다운로드한 `jms_pam_agent.json` 디렉터리에서 실행합니다. Linux 서비스는 root와 systemd가 필요합니다. 복제본별 ID를 변경하세요. 전경 모드는 해당 OS 바이너리가 필요하며 macOS / Windows는 소스 빌드가 필요할 수 있습니다.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### 전경 실행

내장 설치에는 Linux와 root가 필요합니다. macOS, 비 root Linux 또는 Windows에서는 접속 마법사에서 JSON 또는 Socket을 선택하고 init-local 및 run --local --config 명령을 실행하세요. 한 번만 초기화하고 생성된 비공개 로컬 설정을 재사용하세요. 포그라운드 모드는 systemd 작업을 수행하지 않습니다.

바이너리를 `jms-pam-agent`(Windows: `jms-pam-agent.exe`)로 저장하고 Linux/macOS에서 `chmod 0755 ./jms-pam-agent`를 실행합니다.

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

## 3. 전달 방식 선택

JSON은 파일 전달, EnvironmentFile은 지정된 systemd 서비스, Unix Socket은 로컬 API를 제공합니다. Agent는 전달 성공 후 전달 리비전을 저장하고 애플리케이션은 검증 및 적용 후 적용 리비전을 저장합니다. Socket은 앱 사용자 소유이며 권한은 0600입니다. 해당 사용자로 요청하세요.

| 전달 방식 선택 | 설정 및 실행 |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

Linux 설정은 `/etc/jms-pam-agent/agent.json`(0600)입니다. `state_file`은 자격 증명과 진행 상태, `event_file`은 이벤트 메타데이터를 저장합니다. `delivery_root`와 `socket_path`는 전달 위치입니다. 전경 모드는 생성한 `agent.json` 경로를 사용합니다.

## 4. 애플리케이션 설정 변경 및 A/B 순환

다운로드한 설정에는 Agent 인증 정보와 전달 설정이 포함됩니다. rules에 업무에서 사용하는 계정 ID, 설정 갱신, 적용 동작, 실행 중인 연결 확인을 지정합니다. allow_account_switch를 설정하면 동일한 규칙으로 A/B 양방향 교체를 처리합니다. 선택적 credential_check는 파일 변경 전에 새 로그인을 검증합니다. 상태, 이벤트, Socket 경로와 300초 조정 간격에는 기본값이 있습니다. rules가 비어 있으면 자격 증명별 기본 파일을 기록합니다.

첫 JSON은 기본 설정입니다. 업무 파일을 변경하려면 빈 `rules`를 다음 배열로 바꿉니다. 경로, 서비스 및 스크립트는 예제이므로 미리 준비하세요. 일반 변경은 `allow_account_switch`를 생략할 수 있고 A/B는 이 설정과 실제 연결 검증이 필요합니다.

| 설정 및 실행 | 애플리케이션 설정 변경 및 A/B 순환 |
| --- | --- |
| `credential_check` | 선택: 파일 변경 전에 새 계정 연결을 검증합니다. |
| `config_update` | 필드 매핑, 템플릿 또는 스크립트로 업무 파일을 변경합니다. |
| `service_action` | 고정 서비스 reload/restart 또는 고정 스크립트를 실행합니다. |
| `application_check` | 실행 중인 업무 연결을 검증한 후 적용을 확인합니다. |

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

### 설정 파일 형식

`config_update.format`의 기본값은 `auto`이며 생략해도 동일합니다. 확장자 `.txt`, `.env`, `.properties`는 `env`(`KEY=value`), `.yml`, `.yaml`은 `yaml`(최상위 `KEY: value`, 새 값은 따옴표로 감싼 문자열)로 처리합니다. 다른 확장자는 `env` 또는 `yaml`을 명시하세요. 매핑 필드는 이미 존재하며 한 번만 나타나야 하고 다른 설정은 보존됩니다. INI 섹션이나 중첩 YAML은 `files` + `template_file`로 전체 파일을 생성하거나 `script`를 사용하세요. 환경 변수 파일을 갱신한 후에는 애플리케이션이 다시 읽거나 재시작해야 적용됩니다.

로컬 rules에 파일, JSON/EnvironmentFile 또는 신뢰할 수 있는 템플릿과 systemd reload/restart 또는 고정 실행 파일을 설정합니다. 스크립트는 표준 입력으로 자격 증명 JSON을 받고 고정 인수와 제한 시간을 사용하며 적용을 검증한 후 성공합니다. Core는 실행 경로나 권한을 확장할 수 없습니다. 구성을 수정한 후 Agent를 재시작합니다.

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
  'http://localhost/v1/credentials/<credential-key>'
```

### 애플리케이션 적용 확인

실제 연결 검증 및 전환 후 확인합니다. `application_check`와 `confirm_on_success: true` 또는 명시적 확인을 사용합니다. 동기 검사에서 확인 API를 호출하지 마세요. 결과는 저장되며 `confirmed`는 Core 수락, `pending`은 재시도입니다.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. 연동 검증

1. 인스턴스 온라인 상태와 승인 범위를 확인합니다.
2. push는 연결 정책에서 업데이트하고 전달 계정과 버전을 확인합니다.
3. A/B는 실제 연결 전환 및 이전 연결 해제 후 이벤트 결과를 확인합니다. 조회나 전달만으로 업무 적용을 증명하지 않습니다.

### 문제 해결

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent는 시작 시, 관련 이벤트 수신 시, 300초마다 동기화합니다. 네트워크 오류는 이미 가져온 최신 승인 자격 증명을 유지합니다. 신원이나 권한 거부 시 Socket 조회를 차단하고 서명된 동기화가 성공하면 복구합니다. 전달된 파일은 유지됩니다. SIGINT/SIGTERM은 서비스, 연결, 읽기 스레드를 종료합니다.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK 연동

## 1. 연동 사전 조건과 흐름

1. 애플리케이션을 만들고 조회할 계정을 승인합니다(pull). 사용자 이름이나 정책 ID 대신 계정 ID를 사용합니다.
2. 자동 전달이 필요하면 알림 또는 A/B 순환 정책을 연결합니다(push).
3. 연동 마법사에서 인증 설정을 받고 복제본마다 고유하고 고정된 `instance_id`를 사용하며 AK/SK를 보호합니다.

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "orders-node-1"
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

먼저 API를 요청합니다. 성공하면 보유한 최신 값을 교체하며 오래된 버전으로 새 값을 덮어쓰지 않고 시간 만료도 없습니다. 시간 초과, 네트워크 오류 또는 HTTP 5xx일 때만 같은 선택자의 마지막 성공 값을 로컬 표시와 함께 반환합니다. 이전 값이 없으면 원래 오류입니다. SDK는 업데이트, 취소 또는 종료까지 클라이언트 메모리에 보유하며 clone과 재시작은 빈 상태로 시작합니다. Agent는 기존의 보호된 로컬 상태에 저장합니다. HTTP 401/403/404는 SDK 값을 삭제하고 실패하며 잘못된 성공 응답도 실패합니다. 취소는 해당 값을, snapshot은 권한 범위 밖 값을 삭제합니다. 구성 변경은 다음 snapshot으로 범위를 확인할 때까지 보유합니다.Agent는 HTTP 동기화 전에 명시적 취소와 snapshot 범위 축소를 적용하고 저장하며, 백엔드 장애 중이나 재시작 후에도 해당 로컬 조회를 차단합니다.credential_not_found(HTTP 400) 응답도 SDK 보관 값을 삭제합니다. HTTP 426은 보관 값을 삭제하지 않고 명시적으로 실패하며 이 요청에 캐시 값을 반환하지 않습니다. 선택적 메타데이터 API 부재(HTTP 404)는 자격 증명을 삭제하지 않습니다. Agent는 pull 전용 값을 오프라인용으로 영구 저장하지 않습니다. SDK는 기존 임시 장애 규칙에 따라 메모리 값을 사용할 수 있습니다.

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
