# cURL — 使用指南

使用签名 HTTP 请求读取应用授权账号的当前凭据。

## 環境需求

- Bash / cURL / OpenSSL / base64
- `demo.sh`

指令碼需要 Bash、cURL、OpenSSL 和 base64。替換 ASSET 和 ACCOUNT，並確保簽章查詢參數與 cURL 實際傳送的編碼完全一致。

## 設定與執行

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

## 請求與回應

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

响应包含账号、资产及凭据版本；密码仅通过签名取密接口返回。

## 問題排查

检查身份、授权、签名和实例 ID。

## 憑證策略接入

下方簽章細節和協定表供偵錯參考。憑證策略透過 SDK 方法整合，也可透過 Go pam-agent 的 JSON 檔案、EnvironmentFile 或 Unix Socket 整合。

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

依下方標頭順序計算 HMAC-SHA256 簽章。request-target 包含編碼後的路徑及查詢參數，Digest 使用實際請求本文的 SHA-256，X-JMS-Request-ID 使用唯一 UUID，Date 使用 HTTP UTC 時間；設定 X-JMS-Client-Version、X-JMS-Protocol-Version: 1 和 SDK 的 X-JMS-Config-Schema-Version: 0。API 與 WebSocket 都使用應用程式 AK/SK 和穩定、唯一的 instance_id，每次請求及重新連線重新產生簽章。

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

received 回執僅表示已讀取事件。處理帶 event_id 的業務事件前，在同一 WebSocket 傳送 received；snapshot 和 pong 無需回執。每次連線或重新連線都同步快照，處理 credential.secret.updated / credential.switch.published / credential.refresh.requested，並回應授權撤銷及設定變更。
