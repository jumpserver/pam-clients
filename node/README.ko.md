# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. 연동 사전 조건과 흐름

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

pull만 사용하면 첫 조회까지만 진행합니다. push나 A/B는 업무 콜백과 수신 시작이 필요합니다. SDK가 프로토콜과 결과를 처리하고 업무가 실제 연결을 전환합니다. 예제는 현재 소스용입니다. 맞는 Core 및 SDK 빌드를 사용하세요. 배포된 1.0.2에는 모든 새 API가 포함되지 않습니다.

- Node.js 20.3+ / ws

## 2. SDK 설치 및 인증 설정

Install the SDK and configure the identity below. Authorize accounts in the application for on-demand access. Enable Account credential change notifications in the application to receive changes for all authorized accounts; there is no separate account subscription policy. Bind an A/B rotation policy only when account switching is required. Each replica must use a stable, unique instance ID.

아래 명령으로 같은 Core에 포함된 클라이언트 소스를 설치하세요. 배포된 1.0.2에는 관리형 콜백이 없습니다. jumpserver.example.com을 Core 주소로 바꾸고 Go 로컬 대체 경로를 포함한 jms-pam-clients 디렉터리를 보관하세요.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/node/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
npm install ./jms-pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

### 로컬 소스 설치 (대안)

Core가 소스 다운로드 API를 제공하지 않거나 오프라인 배포 시 호환 소스를 `/path/to/pam-clients`에 준비하고 업무 디렉터리에서 실행합니다. 실제 경로로 바꾸세요.

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

## 3. 첫 계정 자격 증명 조회

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

## 4. 이벤트 처리기

아래 계정 변경 콜백을 구현하고 관리형 리스너를 시작하세요. SDK가 스냅샷, 실시간 조회, 계정과 버전 검증, 재시도, 명령 실행 권한 획득과 결과 보고를 처리합니다. 실제 연결 검증, 연결 풀 전환 및 이전 연결 해제를 완료한 뒤 반환하세요. Python과 Node.js는 하위 클래스, Go는 OnAccountChanged, Java는 AccountEventListener를 사용합니다. 미구현 예제는 실패합니다.

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

콜백은 제한된 큐에서 순차 실행되며 일반 업데이트 실패 시 1–30초 간격으로 재시도합니다. 업무 콜백 완료 후에만 성공을 보고하며 결과 업로드 실패는 같은 리스너에서 완료된 작업을 반복하지 않습니다. 명령은 실행 권한을 얻은 뒤 실행하며 실패한 명령 작업은 자동 반복하지 않습니다. 사용 계정은 자동 보고되고 A/B 전환 시 현재 계정을 교체합니다. 범위 제거 시 연결 해제 콜백을 구현하세요. 사용 보고, 조회 또는 파일 전달 자체는 업무 성공을 확인하지 않습니다. 저수준 API와 기존 훅은 명시적 확인을 유지합니다. 재시작 후에도 멱등성을 보장하세요.

### 수신 수명 주기 및 종료

`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`

watchEvents는 이벤트 루프를 막지 않습니다. startEvents는 동기화 완료를 보장하지 않습니다. 감시는 하나만 허용됩니다. async 훅은 순차 await되며 AbortSignal을 받습니다. close 뒤 외부에서 stop() 또는 done을 기다리세요. 자신의 done은 기다리지 마세요. clone은 기본 Client를 반환합니다.

### 최신 자격 증명과 백엔드 장애

SDK는 API를 먼저 요청하며 성공 시 보관 값을 갱신하고 이전 버전으로 돌아가지 않습니다. 시간 초과, 네트워크 오류, HTTP 5xx에서만 같은 선택 조건의 기존 값을 로컬 출처로 반환할 수 있습니다. 보관 값에는 시간 만료가 없고 SDK clone 및 재시작 시 메모리는 비워집니다. HTTP 401/403/404 또는 credential_not_found(400)는 보관 값을 지우고 실패합니다. 426과 잘못된 성공 응답은 대체 값을 사용하지 않으며 선택적 메타데이터 API 부재는 값을 지우지 않습니다. Agent는 store/services에 선언된 계정을 보호된 상태에 보관하며, 선언되지 않은 즉시 조회를 자동 저장하지 않습니다. 명시적 취소는 오프라인에서도 HTTP 갱신 전에 적용됩니다. push 스냅샷은 전체 저장 권한을 나타내지 않으며 동기화 성공 시 현재 애플리케이션 권한을 확인합니다.

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

관리된 감시를 켜면 snapshot과 credential.updated가 자동 조회하고 보유 값을 교체한 뒤 업무 훅을 호출합니다. 실패하면 이전 값을 유지하고 재시도합니다. Agent도 업데이트 알림으로 조회하며 장애 시 이전 값을 유지합니다. 업데이트와 수동 전환에는 아래 API 조회 필수 호출을 사용하세요. 보유 값은 새로 조회한 버전이 아니며 회전을 자동 확인하지 않습니다.

유휴 시 10초마다 ping을 보내고 약 30초간 메시지가 없으면 재연결합니다. 1–30초 지수 백오프와 새 서명을 사용합니다. snapshot은 현재 상태를 복원하며 과거 이벤트를 재생하지 않습니다.



## 5. 주요 메서드

### 허가된 계정 목록

목록 메서드는 애플리케이션의 현재 pull 권한 범위를 조회합니다. 계정, 자산 및 관련 push 정책 메타데이터만 반환하며 암호를 가져오거나 자격 증명 조회를 기록하지 않습니다. 정책이 없는 계정도 포함됩니다. 전체 목록은 모든 페이지를 읽고 페이지 메서드는 `count`와 `accounts`를 반환합니다(Go는 목록과 총수를 별도로 반환). 기본 limit은 200, offset은 0이며 limit은 1–500, search는 최대 128자로 계정 이름, 사용자 이름, 자산 이름 및 주소를 검색합니다. 중복, 불완전한 결과 또는 총수 변경은 오류를 반환합니다. 권한이 변경되면 전체 목록을 다시 조회하세요. 암호가 필요하면 계정 ID로 조회 메서드를 호출하세요. 예제에는 새 메서드가 포함된 SDK 소스가 필요합니다. 배포된 1.0.2의 Python, Java, Node.js에는 아직 제공되지 않습니다.

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


### 애플리케이션, 인스턴스 및 정책 정보

getApplication은 서명 애플리케이션의 ID, 이름, 조직 및 허가된 계정과 보이는 정책 수를 반환합니다. getClientInfo는 현재 복제본의 등록 정보, 유형 및 버전만 반환합니다. 정책 목록은 비활성 정책을 포함한 보이는 연결 정책 전체를 반환하며 search는 이름이나 key를 검색합니다. 개별 정책은 정책 ID 또는 정책 자체의 key 중 하나로 조회하며 계정 push key를 사용하지 않습니다. 순환 정책은 두 계정과 현재 활성 계정의 권한이 필요하고 구독 수는 현재 허가된 계정만 계산합니다. 비밀이나 암호를 반환하거나 자격 증명을 조회하고 적용 확인하지 않습니다. 최초 접근 시 복제본이 등록됩니다. 업데이트된 Core와 SDK 소스가 필요하며 배포된 1.0.2에는 아직 없습니다.

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


### 애플리케이션 상태 보고

실행 상태와 사용 중인 계정 ID를 전체 스냅샷으로 보고합니다. 상태: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. 빈 목록은 이전 사용 기록을 지웁니다. 상태 변경이나 애플리케이션의 하트비트에서 호출합니다. Python, Java, Node.js는 네트워크/API 실패 시 false를 반환하며 Go는 `TryReportStatus`를 제공합니다. 엄격한 호출은 오류 정보를 제공합니다. 잘못된 인수는 거부됩니다. 설정된 HTTP 제한 시간을 사용합니다. 보고는 비밀 조회, 교체 확인, 캐시 무효화를 수행하지 않습니다. `error_code`에 비밀이나 예외 메시지를 넣지 마세요. ClientInfo에서 마지막 보고와 시간을 조회할 수 있습니다.

```javascript
const accepted = await client.reportAccounts([account.id])
await client.reportAccounts([])
```


### 저수준 이벤트 및 애플리케이션 명령

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

기존 key API는 호환용으로 유지됩니다. 신규 연동은 `get_account`와 `confirm_event`를 사용하며 Core도 업데이트해야 합니다.

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

## 6. 연동 검증

1. 승인 ID로 조회하고 실제 연결을 검증합니다.
2. 수신 시 초기 동기화와 정책 업데이트를 확인하고 업무 콜백 완료 후 성공 결과를 확인합니다.
3. 사용 상태와 이벤트 결과를 따로 확인합니다. 조회나 상태 보고는 적용 확인이 아닙니다.

### 문제 해결

HTTP, 네트워크, 인증 및 디코딩 실패는 코드와 HTTP 상태를 포함한 SDK 예외/오류 형식을 사용합니다. 사용 후 스트림과 클라이언트를 닫으세요. 복제 클라이언트의 수명은 독립적입니다. 일시적 실패는 애플리케이션 경계에서 재시도하고 비밀이나 인증 헤더는 기록하지 마세요.

`PAMError`

모든 SDK는 프로토콜 버전 1을, Agent 설정은 스키마 버전 1을 사용합니다. client_upgrade_required(HTTP 426)를 받으면 호환성을 확인하고 업그레이드하세요. 알 수 없는 선택 필드와 알림 이벤트는 허용하지만 지원하지 않는 정책은 적용하거나 확인하면 안 됩니다. 수신 확인은 자동 전송되며 자격 증명 적용을 증명하지 않습니다. 클라이언트 정보에는 선택적 server_capabilities가 포함될 수 있습니다. 누락 또는 null은 알 수 없음을 의미하며 새 이름을 허용합니다. 서버 기능 목록이며 권한을 부여하지 않습니다.

<!-- sdk-doc:end -->
