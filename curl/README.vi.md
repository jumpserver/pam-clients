# cURL — Hướng dẫn sử dụng

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Yêu cầu môi trường

- Bash / cURL / OpenSSL / base64
- `demo.sh`

Cần Bash, cURL, OpenSSL và base64. Thay ASSET và ACCOUNT; bảo đảm chuỗi truy vấn dùng để ký được mã hóa đúng như cURL thực sự gửi.

## Cấu hình và chạy

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

## Yêu cầu và phản hồi

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Khắc phục sự cố

Check identity, authorization, signature and instance ID.

## Tích hợp chính sách thông tin xác thực

Chi tiết chữ ký và bảng giao thức dưới đây dùng để chẩn đoán. Dùng phương thức SDK cho chính sách thông tin xác thực hoặc Go pam-agent qua tệp JSON, EnvironmentFile hay Unix Socket.

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

Ký HMAC-SHA256 theo thứ tự tiêu đề bên dưới. request-target chứa đường dẫn và truy vấn đã mã hóa, Digest dùng SHA-256 của đúng phần thân, X-JMS-Request-ID là UUID duy nhất, Date là thời gian HTTP UTC. Đặt X-JMS-Client-Version, X-JMS-Protocol-Version: 1 và X-JMS-Config-Schema-Version: 0 cho SDK. API và WebSocket dùng AK/SK ứng dụng cùng instance_id ổn định, duy nhất; tạo lại chữ ký cho mỗi yêu cầu và lần kết nối lại.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

received chỉ có nghĩa là đã đọc sự kiện. Trước khi xử lý sự kiện nghiệp vụ có event_id, gửi received trên cùng WebSocket; snapshot và pong không cần biên nhận. Đồng bộ ảnh chụp mỗi lần kết nối/kết nối lại, xử lý credential.secret.updated / credential.switch.published / credential.refresh.requested, thu hồi quyền và thay đổi cấu hình.
