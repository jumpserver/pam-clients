# cURL — 사용 가이드

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## 환경 요구 사항

- Bash / cURL / OpenSSL / base64
- `demo.sh`

Bash, cURL, OpenSSL, base64가 필요합니다. ASSET과 ACCOUNT를 바꾸고, 서명할 쿼리의 인코딩이 cURL이 실제 전송하는 값과 일치하는지 확인하세요.

## 설정 및 실행

Set the server, application identity, organization, instance ID and account ID.

```bash
cd curl
export API_URL='https://jumpserver.example.com'
export API_KEY_ID='<app-id>'
export API_KEY_SECRET='<app-secret>'
export ORG_ID='<org-id>'

bash demo.sh
```

Set the server, application identity, organization, instance ID and account ID.

## 요청 및 응답

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## 문제 해결

Check identity, authorization, signature and instance ID.

## 자격 증명 정책 연동

아래 서명 정보와 프로토콜 표는 진단 참고 자료입니다. 자격 증명 정책에는 SDK 메서드를 사용하거나 Go pam-agent의 JSON 파일, EnvironmentFile 또는 Unix Socket을 사용하세요.

| HTTP | API | JSON / query |
| --- | --- | --- |
| GET | `/api/v1/accounts/credential-client/credential/` | `instance_id`, `account_id` |
| GET | `/api/v1/accounts/credential-client/accounts/` | `instance_id`, `limit`, `offset`, `search` |
| GET | `/api/v1/accounts/credential-client/application/` | `instance_id` |
| GET | `/api/v1/accounts/credential-client/client/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/status/` | `instance_id`, `status`, `account_ids`, `error_code` |
| GET | `/api/v1/accounts/credential-client/policies/` | `instance_id`, `search` |
| GET | `/api/v1/accounts/credential-client/policy/` | `instance_id`, `key` or `policy_id` |
| POST | `/api/v1/accounts/credential-client/event-result/` | `instance_id`, `event_id`, `status`, `error_code` |
| GET | `/api/v1/accounts/credential-client/commands/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/command-result/` | `instance_id`, `command_id`, `status`, `error_code` |
| WebSocket | `/ws/accounts/credential-events/` | `instance_id` |

아래 헤더 순서로 HMAC-SHA256 서명을 계산합니다. request-target에는 인코딩된 경로와 쿼리, Digest에는 실제 본문의 SHA-256, X-JMS-Request-ID에는 고유 UUID, Date에는 HTTP UTC 시간을 넣습니다. X-JMS-Client-Version, X-JMS-Protocol-Version: 1, SDK의 X-JMS-Config-Schema-Version: 0을 설정하세요. API와 WebSocket은 애플리케이션 AK/SK와 안정적이고 고유한 instance_id를 사용하며 요청과 재연결마다 서명을 생성합니다.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

`account_id`로 계정을 가져오고 연결 검증과 전환을 완료한 후 `event_id`로 `success` 또는 `failed`를 보고합니다. 수신 또는 조회 성공은 적용 성공이 아닙니다. 이벤트와 재연결 스냅샷에는 `event_id`, `account_id`, `account_revision`이 있습니다. 적용 전 버전을 확인하고 명령은 `running`으로 선점하세요. 처리는 멱등적이어야 합니다.

received는 이벤트를 읽었다는 뜻입니다. event_id가 있는 업무 이벤트를 처리하기 전에 같은 WebSocket에서 received를 보냅니다. snapshot과 pong에는 수신 확인이 필요 없습니다. 연결과 재연결마다 스냅샷을 동기화하고 credential.secret.updated / credential.switch.published / credential.refresh.requested, 권한 취소, 설정 변경을 처리하세요.
