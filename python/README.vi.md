# JumpServer PAM Python SDK / Agent

Python 3.9+ cung cấp SDK chính sách thông tin xác thực. Go jms-pam-agent phân phối qua tệp cục bộ, hành động cố định hoặc Socket cục bộ mà không cần Python. Chạy trực tiếp trên Linux, macOS và Windows; cài đặt systemd tích hợp chỉ dành cho Linux.

<!-- agent-doc:start -->

## Tích hợp Go Agent

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


Trình cài đặt tích hợp cần Linux và root. Trên macOS, Linux không có root hoặc Windows, chọn JSON hoặc Socket trong trình hướng dẫn và chạy init-local cùng run --local --config. Chỉ khởi tạo một lần rồi dùng lại cấu hình cục bộ riêng tư; chế độ chạy trực tiếp không thực hiện thao tác systemd.

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

JSON giao tệp, EnvironmentFile dùng dịch vụ systemd cố định, Unix Socket cung cấp API cục bộ. Agent ghi phiên bản đã giao sau thành công; ứng dụng ghi phiên bản đã áp dụng sau kiểm tra và sử dụng. Socket thuộc người dùng ứng dụng với quyền 0600; gửi yêu cầu bằng người dùng đó.

rules cục bộ cấu hình tệp, JSON/EnvironmentFile hoặc mẫu tin cậy cùng systemd reload/restart hoặc chương trình cố định. Script nhận JSON qua stdin, dùng đối số cố định và giới hạn thời gian, rồi kiểm tra ứng dụng trước khi báo thành công. Core không được mở rộng quyền này. Khởi động lại Agent sau khi sửa cấu hình riêng tư.

Cấu hình tải về đã có danh tính và thiết lập phân phối của Agent. Trong rules, khai báo ID tài khoản ứng dụng sử dụng, cách cập nhật cấu hình, áp dụng thay đổi và kiểm tra kết nối đang chạy. allow_account_switch dùng cùng một quy tắc cho cả hai chiều luân phiên A/B. credential_check tùy chọn kiểm tra đăng nhập mới trước khi sửa tệp. Đường dẫn trạng thái, sự kiện, Socket và chu kỳ đối chiếu 300 giây có giá trị mặc định. rules rỗng ghi tệp mặc định cho từng thông tin xác thực.

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

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```


### API cục bộ và xác nhận

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

### Khắc phục sự cố

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

Agent đồng bộ lúc khởi động, theo sự kiện liên quan và mỗi 300 giây. Lỗi mạng giữ thông tin xác thực mới nhất đã lấy và được cấp quyền. Khi danh tính/quyền bị từ chối, chặn lấy qua Socket; đồng bộ có chữ ký thành công sẽ khôi phục. Tệp đã giao vẫn được giữ. SIGINT/SIGTERM đóng dịch vụ, kết nối và luồng đọc.

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Tích hợp Python SDK

Cần Python 3.9+. Cài jms-pam từ PyPI hoặc kho nội bộ bằng Python của ứng dụng, rồi tải cấu hình từ trình hướng dẫn kết nối.

```bash
python3 -m pip install jms-pam
```

Đặt jms_pam_config.py nơi ứng dụng có thể nhập. client_options chứa danh tính ứng dụng: không đưa vào kho mã hay nhật ký. Mỗi bản sao có instance_id ổn định, duy nhất.

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

### Sự kiện và áp dụng thông tin xác thực

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

<!-- python-events-example:start -->
```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


def apply_account(account):
    # Validate a new connection, switch the pool, then release old connections.
    # Never log account.secret or authentication headers.
    raise NotImplementedError("Implement the application's account update first")


def restart_application():
    raise NotImplementedError("Implement application restart and health check first")


class ApplicationClient(Client):
    def apply_event(self, event):
        if event.get("event") == "application.restart.requested":
            restart_application()
            return
        account = self.get_account(
            account_id=event["account_id"], allow_local_fallback=False,
        )
        if account.revision != event["account_revision"]:
            raise ValueError("The event account version is superseded")
        apply_account(account)

    def handle_event(self, event):
        event_id = event["event_id"]
        if event.get("command_id"):
            claim = self.report_application_command_result(
                command_id=event["command_id"], status="running",
            )
            if not claim.accepted:
                return
        try:
            self.apply_event(event)
        except Exception:
            self.confirm_event(
                event_id=event_id, status="failed", error_code="application_failed",
            )
            raise
        # A fetch or delivery receipt never confirms application success.
        self.confirm_event(event_id=event_id)

    def run(self):
        for event in self.watch_credential_events():
            if event.get("command_id"):
                updates = [event]
            elif event.get("event") == "snapshot":
                updates = event.get("credentials", [])
                # Release application connections absent from the new scope.
            elif event.get("event") == "credential.updated":
                updates = [event]
            elif event.get("event") == "credential.revoked":
                # Release connections for event["account_id"].
                continue
            else:
                continue
            for update in updates:
                try:
                    self.handle_event(update)
                except Exception as error:
                    self.on_event_error(error, update)


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    client.run()
```
<!-- python-events-example:end -->

### Phương thức thông dụng

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

API key cũ được giữ để tương thích. Tích hợp mới dùng `get_account` và `confirm_event`, đồng thời cần cập nhật Core.


<!-- sdk-doc:end -->
