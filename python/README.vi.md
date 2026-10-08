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

Với luân phiên hai tài khoản, xác minh kết nối thật, chuyển nhóm kết nối và giải phóng kết nối cũ trước khi xác nhận đúng key, revision và account_id. Đăng ký thay đổi thông tin xác thực không cần xác nhận. Không xác nhận khi kiểm tra kết nối thất bại.

```bash
/usr/local/bin/jms-pam-agent confirm '<credential-key>' \
  --revision '<revision>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

Chỉ luân phiên cần confirm. Xác nhận cục bộ được lưu bền vững trước; confirmed nghĩa là Core đã nhận, pending nghĩa là sẽ thử lại. Không xác nhận chỉ vì ghi tệp hoặc khởi động lại dịch vụ thành công.

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

Đặt jms_pam_config.py nơi ứng dụng có thể nhập. client_options chứa danh tính ứng dụng: không đưa vào kho mã hay nhật ký. Mỗi bản sao có instance_id ổn định, duy nhất. get_credential nhận đúng một bộ chọn: account_id cho tài khoản hoặc key cho chính sách luân phiên.

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


with Client(instance_id=instance_id, **client_options) as client:
    credential = client.get_credential(account_id="<account-id>")
    username = credential.account.username
    password = credential.account.secret
```

### Sự kiện và áp dụng thông tin xác thực

Xử lý snapshot ban đầu/khi kết nối lại và credential.updated. Ví dụ tách subscription với alternating_rotation. Triển khai apply_credential để xác minh kết nối thật, chuyển nhóm kết nối và giải phóng kết nối cũ. Hàm giữ chỗ phát sinh lỗi để ngăn xác nhận thông tin chưa áp dụng.

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


def apply_credential(credential):
    raise NotImplementedError('Hãy triển khai và xác minh việc chuyển thông tin xác thực của ứng dụng')


with Client(instance_id=instance_id, **client_options) as client:
    for event in client.watch_credential_events():
        if event.get("event") == "snapshot":
            updates = event.get("credentials", [])
        elif event.get("event") == "credential.updated":
            updates = [event]
        else:
            continue
        for update in updates:
            mode = update.get("credential_mode")
            key = update.get("credential_key") or update.get("key")
            account_id = update.get("account_id")
            if mode == "subscription" and account_id and key:
                policy_key = key if key.endswith(f":{account_id}") else f"{key}:{account_id}"
                credential = client.get_credential(key=policy_key, allow_local_fallback=False)
            elif mode == "alternating_rotation" and key:
                credential = client.get_credential(key=key, allow_local_fallback=False)
            else:
                continue
            apply_credential(credential)
            if mode == "alternating_rotation":
                client.confirm_credential(
                    key=credential.key,
                    revision=credential.revision,
                    account_id=credential.account.id,
                )
```

Với luân phiên hai tài khoản, xác minh kết nối thật, chuyển nhóm kết nối và giải phóng kết nối cũ trước khi xác nhận đúng key, revision và account_id. Đăng ký thay đổi thông tin xác thực không cần xác nhận. Không xác nhận khi kiểm tra kết nối thất bại.

SDK tự động cố gắng gửi biên nhận received trước khi trả về sự kiện nghiệp vụ. Ứng dụng không cần gửi thêm. Biên nhận chỉ cho biết đã đọc sự kiện, không có nghĩa là đã áp dụng thông tin xác thực và không thay thế xác nhận. Đối chiếu snapshot ban đầu và khi kết nối lại; snapshot và pong không cần biên nhận.

## Hàm xử lý sự kiện

Khởi tạo trạng thái cục bộ trước khi lắng nghe. Python và Node.js dùng lớp con, Go dùng EventHandlers, Java dùng CredentialEventListener. Hãy triển khai việc chuyển kết nối thật trong ví dụ. API cũ vẫn được giữ lại.

```python
from jms_pam import Client
from jms_pam_config import client_options, instance_id


def apply_credential(response):
    # Replace this function with your application's connection update:
    # build and verify a new connection, switch to it, then release the old one.
    # Never write response.account.secret or authentication headers to logs.
    raise NotImplementedError("Implement the application connection update first")


def restart_application():
    # Implement restart and its health check, then return only after it succeeds.
    raise NotImplementedError("Implement the application restart first")


def handle_command(client, event):
    if event["event"] == "application.restart.requested":
        restart_application()
    elif event["event"] == "credential.switch.requested":
        response = client.get_credential(key=event["credential_key"], allow_local_fallback=False)
        if (
            response.revision != event["revision"]
            or response.account.id != event["account_id"]
        ):
            raise ValueError("Requested account version is superseded")
        apply_credential(response)
        client.confirm_credential(
            key=response.key, revision=response.revision, account_id=response.account.id
        )
    else:
        raise ValueError("Unsupported application command")


class ApplicationClient(Client):
    def __init__(self, *args, **options):
        super().__init__(*args, **options)
        self.credentials = {}
        self.credential_modes = {}

    def on_event(self, event):
        if event.get("command_id"):
            self.execute_application_command(
                event, lambda command: handle_command(self, command)
            )
        elif event.get("event") == "snapshot":
            self.credential_modes = {
                item["key"]: item["credential_mode"]
                for item in event.get("credentials", [])
            }
            for key in self.credentials.keys() - self.credential_modes.keys():
                # Also release the application's connections for the removed key.
                del self.credentials[key]
        elif event.get("event") == "credential.updated":
            key = event.get("credential_key") or event.get("key")
            if key:
                self.credential_modes[key] = event.get("credential_mode")

    def on_credential_changed(self, credential):
        apply_credential(credential)
        if self.credential_modes.get(credential.key) == "alternating_rotation":
            self.confirm_credential(
                key=credential.key,
                revision=credential.revision,
                account_id=credential.account.id,
            )
        self.credentials[credential.key] = credential

    def on_credential_revoked(self, event):
        key = event.get("credential_key")
        self.credentials.pop(key, None)
        # Release affected connections. The following snapshot reconciles all keys.


with ApplicationClient(instance_id=instance_id, **client_options) as client:
    client.watch_events()
```

snapshot ban đầu, khi kết nối lại và credential.updated lấy thông tin theo chế độ rồi gọi hàm xử lý tuần tự. Bộ đọc dùng hàng đợi giới hạn 128 sự kiện; khi đầy sẽ tạo áp lực ngược. Lỗi lấy hoặc áp dụng được thử lại với thời gian chờ tăng theo hàm mũ 1–30 giây, mỗi lần đều lấy lại. Sự kiện mới thay thế lần thử của cùng mục tiêu; snapshot đặt lại phạm vi, thu hồi và thay đổi cấu hình hủy các lần thử. Hàm xử lý phải gọi lặp lại an toàn. Hàm quan sát và thu hồi không tự thử lại; lệnh phải được nhận quyền thực thi. received chỉ nghĩa là đã đọc; SDK không tự xác nhận luân chuyển. Sự kiện phiên bản cũ không hủy lần lấy lại đang chờ cho phiên bản mới hơn.

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events chờ dừng; start_events không đảm bảo đồng bộ ban đầu. Chờ ứng dụng sẵn sàng. Mỗi client chỉ có một bộ lắng nghe. stop_events và close chờ hàm xử lý; hàm có thể dừng client của mình. clone khởi tạo lại lớp con.

### Thông tin xác thực mới nhất và lỗi máy chủ

Luôn gọi API trước. Kết quả thành công thay thế giá trị đang giữ; phiên bản cũ không ghi đè bản mới và không có thời hạn hết hiệu lực theo thời gian. Chỉ hết thời gian chờ, lỗi mạng hoặc HTTP 5xx mới trả về giá trị thành công gần nhất của cùng bộ chọn với cờ nguồn cục bộ. Chưa có giá trị thì báo lỗi gốc. SDK giữ trong bộ nhớ client đến khi cập nhật, thu hồi hoặc đóng; clone và khởi động lại bắt đầu trống. Agent dùng trạng thái cục bộ được bảo vệ sẵn có. HTTP 401/403/404 hoặc client_upgrade_required xóa giá trị SDK rồi báo lỗi; phản hồi thành công không hợp lệ cũng báo lỗi. Thu hồi xóa mục liên quan, snapshot xóa mục ngoài quyền. Thay đổi cấu hình giữ giá trị đến khi kiểm tra snapshot tiếp theo. Agent áp dụng thu hồi rõ ràng và thu hẹp phạm vi snapshot trước khi đồng bộ HTTP, lưu phạm vi đó và chặn các lần lấy cục bộ liên quan ngay cả khi máy chủ lỗi hoặc sau khi khởi động lại. Phản hồi credential_not_found (HTTP 400) cũng xóa giá trị SDK được giữ lại. Pull trực tiếp bằng account_id luôn cần phản hồi API trực tiếp; snapshot push không xác nhận quyền dùng giá trị pull đã lưu.

- `credential.from_local`
- `get_credential(key=..., allow_local_fallback=False)` / `get_credential(account_id=..., allow_local_fallback=False)`

Khi bật lắng nghe được quản lý, snapshot và credential.updated tự lấy dữ liệu, thay thế giá trị và gọi hàm nghiệp vụ. Lỗi cập nhật giữ bản trước và thử lại. Agent cũng lấy lại theo thông báo, giữ dữ liệu khi máy chủ lỗi. Dùng lời gọi bắt buộc API bên dưới để cập nhật hay chuyển kết nối; giá trị đang giữ không phải phiên bản vừa lấy và không tự xác nhận luân chuyển.

Khi nhàn rỗi gửi ping mỗi 10 giây; khoảng 30 giây không có tin nhắn sẽ kết nối lại, chờ tăng theo hàm mũ 1–30 giây và ký mới. snapshot khôi phục trạng thái hiện tại, không phát lại lịch sử.



### Lệnh ứng dụng

Dùng list_application_commands để lấy yêu cầu chờ và execute_application_command(event, handler) để nhận quyền thực thi. Chỉ chạy khi accepted là true. Hàm chuyển kiểm tra tài khoản/phiên bản yêu cầu, áp dụng và xác nhận; hàm khởi động lại phải kiểm tra sức khỏe trước khi báo thành công. Lỗi báo cáo thất bại không che lỗi xử lý ban đầu.

### Phương thức thông dụng

- `get_credential(key=...)` / `get_credential(account_id=...)`
- `confirm_credential(key=..., revision=..., account_id=...)`
- `watch_credential_events(stop_event=...)`
- `watch_events(stop_event=...)` / `start_events(stop_event=...)` / `stop_events()`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status=...)`
- `execute_application_command(event, handler)`
- `sync_agent(credentials=..., delivered_credentials=...)`
- `clone()` / `close()`

with hoặc close đóng phiên HTTP và luồng sự kiện; clone tạo phiên độc lập. Lỗi HTTP, mạng, xác thực và phân tích tạo PAMError có code, status_code, detail, original_error. Thử lại lỗi tạm thời tại ranh giới ứng dụng, xử lý rõ việc từ chối quyền và không ghi thông tin xác thực.

Mã mới dùng phương thức từ khóa snake_case và thuộc tính dataclass. API đối tượng yêu cầu credential.v1 cũ vẫn có với DeprecationWarning. SDK và Agent dùng giao thức phiên bản 1; sync_agent nhận KnownRevision cho phiên bản được giữ lại và đã giao.

<!-- sdk-doc:end -->
