# cURL — 使用指南

使用签名 HTTP 请求读取应用授权账号的当前凭据。

## 环境要求

- Bash / cURL / OpenSSL / base64
- `demo.sh`

脚本需要 Bash、cURL、OpenSSL 和 base64。替换脚本中的 ASSET 和 ACCOUNT，并确保签名查询参数与 cURL 实际发送的编码完全一致。

## 配置与运行

设置服务端、应用身份、组织、实例 ID 和账号 ID。

```bash
cd curl
export API_URL='https://jumpserver.example.com'
export API_KEY_ID='<app-id>'
export API_KEY_SECRET='<app-secret>'
export ORG_ID='<org-id>'

bash demo.sh
```

设置服务端、应用身份、组织、实例 ID 和账号 ID。

## 请求与响应

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

响应包含账号、资产及凭据版本；密码仅通过签名取密接口返回。

## 排查问题

检查身份、授权、签名和实例 ID。

## 凭据策略接入

下方签名细节和协议表供调试参考。凭据策略接入通过 SDK 方法完成，也可通过 Go pam-agent 的 JSON 文件、EnvironmentFile 或 Unix Socket 接入。

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

按下方请求头顺序计算 HMAC-SHA256 签名。request-target 包含编码后的路径与查询参数，Digest 使用实际请求体的 SHA-256，X-JMS-Request-ID 使用唯一 UUID，Date 使用 HTTP UTC 时间；设置 X-JMS-Client-Version、X-JMS-Protocol-Version: 1 和 SDK 的 X-JMS-Config-Schema-Version: 0。API 与 WebSocket 均使用应用 AK/SK 和稳定、唯一的 instance_id，每次请求和重连重新生成签名。

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

received 回执只表示已读取事件。处理带 event_id 的业务事件前，在同一 WebSocket 发送 received；snapshot 和 pong 无需回执。每次连接或重连都对齐快照，处理 credential.secret.updated / credential.switch.published / credential.refresh.requested，并响应撤销授权和配置变化。
