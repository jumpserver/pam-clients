# cURL — Usage guide

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Requirements

- Bash / cURL / OpenSSL / base64
- `demo.sh`

The script requires Bash, cURL, OpenSSL and base64. Replace ASSET and ACCOUNT in the script, and ensure the signed query is encoded exactly as sent by cURL.

## Configure and run

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

## Request and response

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Troubleshooting

Check identity, authorization, signature and instance ID.

## Credential policy access

The signature details and protocol table below are diagnostic references. Use SDK methods for credential policies, or use the Go pam-agent through JSON files, EnvironmentFile or Unix Socket.

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

Sign requests with HMAC-SHA256 in the header order below. Include the encoded path and query in request-target, SHA-256 of the exact body in Digest, a unique UUID in X-JMS-Request-ID, an HTTP UTC Date, X-JMS-Client-Version, X-JMS-Protocol-Version: 1 and X-JMS-Config-Schema-Version: 0 for SDKs. API and WebSocket use the application AK/SK and a stable, unique instance_id; refresh the signature for each request or reconnect.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

A received receipt only means an event was read. Send received with event_id on the same WebSocket before handling business events; snapshot and pong require no receipt. Reconcile the snapshot on every connection and reconnect, process credential.secret.updated / credential.switch.published / credential.refresh.requested, and handle revocation or configuration changes.
