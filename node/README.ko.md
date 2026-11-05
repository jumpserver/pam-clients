# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. 연동 사전 조건과 흐름

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

pull만 사용하면 첫 조회까지만 진행합니다. push나 A/B는 업무 콜백과 수신 시작이 필요합니다. SDK가 프로토콜과 결과를 처리하고 업무가 실제 연결을 전환합니다. 예제는 현재 소스용입니다. 맞는 Core 및 SDK 빌드를 사용하세요. 배포된 1.0.2에는 모든 새 API가 포함되지 않습니다.

- Node.js 20.3+ / ws

## 2. SDK 설치 및 인증 설정

Install the SDK below and use the identity configuration downloaded from the application access wizard. Protect AK/SK and assign each replica a stable, unique instance ID.

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

관리된 감시를 켜면 snapshot과 credential.secret.updated / credential.switch.published / credential.refresh.requested가 자동 조회하고 보유 값을 교체한 뒤 업무 훅을 호출합니다. 실패하면 이전 값을 유지하고 재시도합니다. Agent도 업데이트 알림으로 조회하며 장애 시 이전 값을 유지합니다. 업데이트와 수동 전환에는 아래 API 조회 필수 호출을 사용하세요. 보유 값은 새로 조회한 버전이 아니며 회전을 자동 확인하지 않습니다.




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



<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# Application integration overview

## Authorization, notifications and rotation

1. **Account authorization** controls which accounts the application may fetch on demand. Authorize the application before installing a client.
2. **Account credential change notifications** is an application-level switch covering all authorized accounts. Use it when applications should react to password changes; no subscription policy is required.
3. **A/B rotation** is a separately bound policy for switching between two accounts before changing the original password. Both accounts must be authorized. Open the policy’s operation guide for its cycle, waiting conditions, cancellation and recovery.

## Choose an integration

Choose by who applies PAM-managed usernames and passwords to the service. Socket API is an application integration through PAM Agent: the application calls a local API while the Agent handles communication with PAM.

| Method | Integration characteristics | Responsibilities |
| --- | --- | --- |
| PAM Agent | Integrate through configuration, templates or adaptation scripts; suitable for existing services. | The Agent retrieves and refreshes the accounts required by the service. Configure field mappings, templates or adaptation scripts to update service configuration, reload or restart the service, and verify that the credentials are in use. |
| SDK | Use the SDK for the application language and call it directly from business code. | The application embeds a language-specific SDK and connects directly to PAM. The SDK handles PAM communication and event processing; the application implements connection validation, credential changes and release of old connections in business callbacks. |
| Socket API | Cross-language integration without embedding a JumpServer SDK; the Agent can be upgraded independently of the application. | The application uses HTTP / WebSocket over the local Agent's Unix Socket to retrieve credentials, receive notifications and report results. The Agent handles PAM authentication, credential refresh and reporting retries; the application validates and switches its own connections. |

Agent automation follows the configured service adaptation and checks. For every method, successful credential retrieval or delivery alone does not prove that the service has switched successfully.

## Setup and acceptance

1. Create an application, authorize accounts and download its private identity configuration.
2. Choose Agent service adaptation, an embedded SDK or the Agent's Socket API according to who will apply credentials. Install the matching components, fetch an account and verify a real connection.
3. For automatic updates, enable application notifications and configure Agent business actions or implement application connection changes. For A/B switching, also bind a rotation policy and enable following its current account.
4. Verify one account update or A/B switch: the application uses the new credential, old connections are released and the business result is successful. Reception, fetching and file delivery alone are insufficient.

## Where to find the details

Agent integration covers deployment and service adaptation: credential files, configuration updates, reload/restart actions and validation. SDK integration covers language-specific installation and business callbacks. Socket API access covers local API calls, credential retrieval, notifications, business results and A/B switching. Events and commands is the shared reference for event payloads, snapshots, receipts and administrator commands. The operation guide inside each rotation policy explains its own cycle, blockers and recovery.

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# Events and commands

This reference applies to all SDK languages and PAM Agent. Prefer managed SDK callbacks or configured Agent services for normal integration; use the raw protocol only when implementing an event consumer yourself.

## Event responsibilities

| Event | Responsibility |
| --- | --- |
| credential.secret.updated / credential.switch.published / credential.refresh.requested and snapshot items | Fetch the named account, verify its version, apply it and report the business result. Managed SDK callbacks handle the protocol. |
| app.scope.updated | Reconcile scope and remove invalid state. Removing a rotation binding does not revoke account authorization. |
| `rotation.*` (such as `rotation.started` and `rotation.completed`), `credential.change.*` (such as `credential.change.started` and `credential.change.completed`) | Update progress, logs or alerts. Do not repeat credential application or confirm business success for these notifications. |
| credential.switch.requested and app.restart.requested | Administrator commands for selected instances: claim, execute and report the command result. |

## Complete event catalog

Current application events, rotation operation commands and connection control messages. Rotation details display progress; each event has one execution responsibility.

### Credential and scope events

credential.secret.updated / credential.switch.published / credential.refresh.requested applies credentials. app.scope.updated and snapshot reconcile scope: detach a missing rotation policy while preserving use of authorized accounts. Only actual account authorization removal disables local access, clears retained credentials and asks the application to release connections.

| Event code | Meaning |
| --- | --- |
| `credential.secret.updated` | The account secret version changed. Fetch and apply the named account; this does not change the selected account. |
| `credential.switch.published` | A target account and policy revision were published for switching, including return to the source account. Fetch and apply the named target; publication is not application success. |
| `credential.refresh.requested` | Fetch and apply the currently published credentials for an authorization grant, manual refresh or snapshot reconciliation. This does not mean the password changed or a rotation started. |
| `app.scope.updated` | Account authorization, notification settings or rotation bindings changed. Use authorized_account_ids to determine account access, then snapshot to reconcile notifications and rotation following. Removing a binding must not close an otherwise authorized account connection. |

### General password-change events

General task lifecycle notifications for progress and alerts. Do not fetch again or report application success for these notifications; apply credentials on credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot.

| Event code | Meaning |
| --- | --- |
| `credential.change.started` | A password-change task is queued or running; the remote password may not have changed yet. |
| `credential.change.completed` | The password-change task succeeded; this is not proof that applications applied the credentials. |
| `credential.change.failed` | The password change failed or could not be verified; inspect the task and remote account. |

### Rotation progress events

These notifications explain cycle progress and outcomes. Application switching follows credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot, not a second action on each progress event.

| Event code | Meaning |
| --- | --- |
| `rotation.verify.started` | Backup-account login verification started. |
| `rotation.verify.skipped` | Backup-account login verification skipped by credential policy. |
| `rotation.verify.failed` | Backup verification failed; no switch is published. |
| `rotation.verify.cancelled` | Backup verification was cancelled. |
| `rotation.started` | The rotation cycle has published the target account and started switching. |
| `rotation.waiting_for_app` | Waiting for required application instances to apply and confirm the target. |
| `rotation.source.waiting` | 모든 참여 인스턴스가 확인하거나 애플리케이션 검증을 건너뛰고 관리자가 실패를 승인한 후에만 원본 계정의 조회 관찰을 시작합니다. |
| `rotation.source.ready` | The original account no-fetch window and required client conditions allow password change. |
| `rotation.completed` | The original account password change was verified and the rotation completed. |
| `rotation.failed` | Rotation entered a password-change failure or recovery-required state. |
| `rotation.force_stopped` | An administrator ended the cycle without waiting for remaining recovery confirmations. |

### Manual account rotation operations

credential.switch.requested asks selected instances to apply the account and revision already published by the rotation policy. Administrators can manually apply or retry the switch; the command does not start a new rotation.

| Event code | Meaning |
| --- | --- |
| `credential.switch.requested` | Apply the already published account and version on selected instances; this does not start a rotation. |

### Application operation commands

app.restart.requested restarts the application and verifies its health. Manual account switching belongs to the account rotation operations above.

| Event code | Meaning |
| --- | --- |
| `app.restart.requested` | Execute the supported application restart action and verify health. |

### Connection and receipt messages

SDK and Agent manage liveness and receipts. snapshot and pong need no received receipt; snapshot credential items are applied separately.

| Event code | Meaning |
| --- | --- |
| `snapshot` | Core sends the current notification scope and authorized account IDs on connection, reconnection or scope changes. |
| `ping` | The client sends an application-level keepalive to Core. |
| `pong` | Core replies to an application-level ping. |
| `received` | The client acknowledges reading an event; it does not report business success. |

## 활동 로그와 프로토콜 이벤트 대응

활동 로그는 감사 동작을 기록하고 이벤트 로그는 프로토콜 알림 및 명령의 전달과 처리 상태를 표시합니다. 감사 동작과 알림은 서로 다른 분류입니다. 표는 가능한 연관 관계이며 전달 성공을 뜻하지 않습니다. 실제 이벤트와 결과는 상세 내용에 있습니다. 수신, 조회, 적용, 건너뛰기 및 관리자 승인은 따로 기록합니다. credential_fetched는 조회 로그에 표시합니다.

| 활동 유형 | 활동 코드 | 관련 알림 / 메시지 | Meaning |
| --- | --- | --- | --- |
| 애플리케이션 명령 전송 | `command_requested` | `credential.switch.requested`<br>`app.restart.requested` | 선택한 인스턴스에 게시된 자격 증명 적용 또는 재시작을 요청합니다. 새 교체 주기를 시작하지 않습니다. |
| 애플리케이션 처리 결과 | `command_result` | `credential.secret.updated`<br>`credential.switch.published`<br>`credential.refresh.requested`<br>`credential.switch.requested`<br>`app.restart.requested` | 자격 증명 적용이나 명령의 진행과 결과를 보고합니다. 재시도가 성공해도 교체 실패에는 관리자 확인이 필요합니다. |
| 통합 또는 교체 구성 생성 | `configuration_created` | `app.scope.updated` | 애플리케이션 또는 정책을 생성했습니다. 범위 변경 시 app.scope.updated를 보낼 수 있습니다. |
| 통합 또는 교체 구성 수정 | `configuration_updated` | `app.scope.updated` | 설정을 수정했습니다. 범위 변경 시 app.scope.updated를 보낼 수 있습니다. |
| 통합 또는 교체 구성 삭제 | `configuration_deleted` | 고정된 대응 이벤트 없음 | 애플리케이션 또는 정책을 삭제했습니다. 자격 증명 적용 결과가 아닙니다. |
| 자격 증명 권한 또는 연결 추가 | `authorization_granted` | `credential.refresh.requested` | 권한이나 교체 정책 연결을 추가하고 새로 고침을 요청합니다. 연결만으로 계정 권한이 확대되지 않습니다. |
| 자격 증명 권한 또는 연결 제거 | `authorization_revoked` | `app.scope.updated` | 권한이나 정책 연결을 제거했습니다. 계정 권한이 실제로 없어질 때만 업무 연결을 해제합니다. |
| 자격 증명 조회 | `credential_fetched` | 고정된 대응 이벤트 없음 | 별도의 조회 로그에 기록합니다. 조회 성공만으로 업무 사용을 증명하지 않습니다. |
| 계정 암호 버전 변경 | `credential_secret_updated` | `credential.secret.updated` | 변경된 자격 증명을 조회하고 적용해야 합니다. 현재 A/B 계정은 바뀌지 않습니다. |
| 계정 전환 게시 | `credential_switch_published` | `credential.switch.published` | 대상 또는 복귀 계정과 정책 버전을 게시합니다. 게시가 전환 완료를 뜻하지 않습니다. |
| 현재 자격 증명 적용 요청 | `credential_refresh_requested` | `credential.refresh.requested` | 게시된 버전의 조회와 적용을 요청합니다. 암호 변경이나 새 교체 주기를 뜻하지 않습니다. |
| 자격 증명 알림 다시 게시 | `credential_republished` | `credential.refresh.requested`<br>`snapshot` | 수동 새로 고침이나 snapshot 대조로 현재 자격 증명의 재적용을 요청합니다. 암호 버전은 같을 수 있습니다. |
| 새 자격 증명 적용 확인 | `credential_confirmed` | 고정된 대응 이벤트 없음 | 클라이언트가 현재 버전 적용 성공을 보고합니다. 수신 확인이나 조회와 별도로 기록합니다. |
| 애플리케이션 검사 건너뛰기 | `credential_confirmation_skipped` | 고정된 대응 이벤트 없음 | 다른 전달 단계 성공 후 선택적 검사에 skip을 보고합니다. 검사 통과를 뜻하지 않습니다. |
| 관리자 계속 진행 승인 | `application_failure_reviewed` | 고정된 대응 이벤트 없음 | 관리자가 사유를 기록하여 실패 차단을 해제합니다. 원래 실패 기록은 유지됩니다. |
| 자격 증명 이벤트 스트림 연결 | `credential_stream_connected` | `snapshot` | 알림 스트림에 연결하고 snapshot으로 범위를 대조합니다. 적용 성공을 뜻하지 않습니다. |
| 자격 증명 이벤트 스트림 해제 | `credential_stream_disconnected` | 고정된 대응 이벤트 없음 | 알림 스트림이 끊겼습니다. 이것만으로 업무 연결 실패를 뜻하지 않습니다. |
| 교체 단계 변경 | `rotation_step` | `rotation.verify.started`<br>`rotation.verify.skipped`<br>`rotation.verify.failed`<br>`rotation.verify.cancelled`<br>`rotation.waiting_for_app`<br>`rotation.source.waiting`<br>`rotation.source.ready`<br>`rotation.completed`<br>`rotation.failed` | 검증, 적용 대기, 원래 계정 조회 관찰 및 결과를 기록합니다. 실제 단계는 상세 내용을 확인하세요. |
| 계정 교체 시작 | `rotation_started` | `rotation.started` | 대상 계정을 게시하고 애플리케이션 전환을 시작했습니다. 전체 교체 완료가 아닙니다. |
| 교체 취소 또는 강제 종료 | `rotation_cancelled` | `credential.switch.published`<br>`rotation.force_stopped` | 취소는 원래 계정으로 복귀 확인을 기다립니다. 강제 종료는 남은 복구 확인을 기다리지 않습니다. |
| 계정 암호 변경 시작 | `secret_change_started` | `credential.change.started` | 작업이 대기열에 있거나 시작되었습니다. 원격 암호는 아직 바뀌지 않았을 수 있습니다. |
| 계정 암호 변경 완료 | `secret_change_completed` | `credential.change.completed` | 변경 작업이 성공했습니다. 애플리케이션의 자격 증명 적용을 증명하지 않습니다. |
| 계정 암호 변경 실패 | `secret_change_failed` | `credential.change.failed` | 작업 실패 또는 결과 불명입니다. 작업과 원격 계정을 확인하세요. |
| 클라이언트 인스턴스 등록 | `client_registered` | 고정된 대응 이벤트 없음 | SDK 또는 Agent 인스턴스가 등록되었습니다. 자격 증명 적용을 뜻하지 않습니다. |
| 클라이언트 인스턴스 활성화 | `client_enabled` | 고정된 대응 이벤트 없음 | 기존 권한 내에서 접근을 재개합니다. 새 계정 권한을 부여하지 않습니다. |
| 클라이언트 인스턴스 비활성화 | `client_disabled` | 고정된 대응 이벤트 없음 | 이후 접근을 거부합니다. 계정 암호는 변경하지 않습니다. |
| 애플리케이션 알림 전달 | `notification` | 고정된 대응 이벤트 없음 | Webhook 전달 및 재시도를 기록합니다. HTTP 전달이 자격 증명 적용 성공을 뜻하지 않습니다. 실제 이벤트 코드는 상세 내용에 있습니다. |
| 애플리케이션 접근 시크릿 재설정 | `application_secret_reset` | 고정된 대응 이벤트 없음 | JumpServer 연결용 인증 시크릿을 변경합니다. 업무 계정의 암호 교체와는 별개입니다. |

## Payload fields

| Field | Meaning |
| --- | --- |
| event_id | Event identity for receipts and application results. snapshot itself has no receipt; credential items can have their own event_id. |
| event / occurred_at | Event type and publication time. |
| account_id / account_revision | Concrete target account and password version. A fresh account revision must match before application. |
| credential_key / revision | Policy publication metadata for A/B rotation. Ordinary account notifications use account revisions. Do not mix the two version domains. |
| credential_mode | alternating_rotation for an A/B policy; subscription for application-level account notifications, without a subscription policy. Application-wide events may use null. |
| account_switch.account_ids | Accounts associated with an A/B switch. |
| operation_id | Correlates events belonging to one change operation or rotation cycle. |
| command_id / expires_at | Administrator command identity and execution deadline. Commands have a separate result API. |

## Snapshots and authorization

연결, 재연결 및 범위 변경 시 Core는 snapshot을 보냅니다. credentials에는 애플리케이션 알림이 켜진 경우 모든 허가된 계정과 연결된 유효한 A/B 회전 정책이 포함됩니다. 각 항목은 event_id, account_id, account_revision을 사용하여 credential.secret.updated / credential.switch.published / credential.refresh.requested처럼 처리합니다. authorized_account_ids는 허가 범위를 나타내며 get_accounts로 계정 정보를 조회합니다. 정책 연결 해제만으로 계정 권한이 취소되지는 않습니다.

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

## Receipts and business results

SDK는 비즈니스 이벤트를 반환하기 전에 received 수신 확인을 가능한 범위에서 자동 전송합니다. 애플리케이션은 추가로 전송할 필요가 없습니다. 수신 확인은 이벤트를 읽었다는 뜻이며 자격 증명 적용이나 적용 확인을 대신하지 않습니다. 최초 및 재연결 스냅샷을 처리하세요. snapshot과 pong에는 수신 확인이 필요하지 않습니다.

결과는 success, failed, skip입니다. app_check가 없는 로컬 service는 다른 단계가 성공하면 skip을 보고하며 애플리케이션 확인으로 계산하지 않습니다. failed는 이후 재시도가 성공하거나 건너뛰어도 다음 단계를 차단합니다. 관리자가 이유를 입력하고 진행을 승인해야 하며 원래 실패 기록은 유지됩니다.

관리자 승인은 accounts.change_applicationcredential 권한이 있는 사용자 세션을 사용합니다. 현재 failure_id와 이유를 제출하세요. 만료된 실패 또는 정책 버전은 거부됩니다. 승인은 감사에 기록되며 클라이언트 결과는 유지됩니다.

```http
POST /api/v1/accounts/application-credentials/<policy-id>/review-application-failure/
Content-Type: application/json

{"client_id": "<client-id>", "failure_id": "<current-failure-id>", "reason": "<review-reason>"}
```

```json
{"event": "received", "event_id": "<event-id>"}
```

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

유휴 시 10초마다 ping을 보내고 약 30초간 메시지가 없으면 재연결합니다. 1–30초 지수 백오프와 새 서명을 사용합니다. snapshot은 현재 상태를 복원하며 과거 이벤트를 재생하지 않습니다.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Socket API 연결

애플리케이션은 JumpServer SDK 또는 애플리케이션 내 AK/SK 없이 로컬 PAM Agent에 연결합니다. TCP가 아닌 Unix Domain Socket을 사용합니다. 아래에 전체 영어 프로토콜 설명이 제공됩니다.

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
    "event": "credential.refresh.requested",
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

CLI/Socket 비밀 조회와 서비스 자격 증명 읽기는 `account`, `source`, `last_pull_at`을 반환합니다. `account.version`은 실제 계정의 자격 증명 버전입니다. 일반 결과에는 정책 버전, `account_revision`, `event_id`가 없습니다. `source`는 Core 실시간 조회 시 `api`, 캐시이면 `local`입니다. `last_pull_at`은 해당 계정을 Core에서 마지막으로 성공적으로 가져온 RFC 3339 시각이며 버전이 같아도 갱신됩니다. 캐시와 함께 저장되어 재시작 후에도 유지됩니다. 메타데이터 동기화, 캐시 읽기, 실패한 조회는 시각을 갱신하지 않습니다. 기존 캐시의 시각을 모르면 null입니다. 온디맨드 조회만으로 오프라인 보관이 활성화되지는 않습니다.

`event_id`는 적용 결과 보고를 위해 알림 메타데이터와 스크립트 알림 외부에 유지됩니다. 스냅샷은 계정, 버전, 정책, 클라이언트가 일치하는 최신 이벤트를 재사용하고, 없으면 재발행 이벤트를 만듭니다. 스크립트는 `credentials[selector].account`를 읽습니다. 기존 Go 템플릿, 필드 매핑, EnvironmentFile 변수 이름은 유지됩니다. JSON 소비자는 중첩 account 구조에 맞춰야 합니다.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  },
  "source": "local",
  "last_pull_at": "2026-10-30T06:30:00Z"
}
```

자격 증명 JSON 파일에는 `account`만 포함하며 `source`와 `last_pull_at`은 생략합니다. 가져온 시각은 실제 계정 ID를 키로 하여 `store.root` 디렉터리와 같은 상위 경로의 `last_pull_at.json`에 저장되며 비공개 상태에도 유지됩니다. 실시간 조회 성공 시 버전이 같아도 진단 파일을 즉시 갱신하고 자격 증명 파일은 다시 쓰지 않습니다. 버전 확인, 캐시 읽기, 조회 실패 시 시각은 유지됩니다. 권한이 취소된 계정의 항목은 제거됩니다.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  }
}
```

`last_pull_at.json`:

```json
{
  "<active-account-id>": "2026-10-30T06:30:00Z"
}
```

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
