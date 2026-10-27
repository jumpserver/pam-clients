# JumpServer PAM Python SDK / Agent

Python 3.9+ cung cấp SDK chính sách thông tin xác thực. Go jms-pam-agent phân phối qua tệp cục bộ, hành động cố định hoặc Socket cục bộ mà không cần Python. Chạy trực tiếp trên Linux, macOS và Windows; cài đặt systemd tích hợp chỉ dành cho Linux.

<!-- agent-doc:start -->

# Tích hợp Go Agent

Chạy Go Agent độc lập trên máy ứng dụng, không cần Python. Thực hiện lần lượt cài đặt, phân phối, thao tác nghiệp vụ và kiểm tra.

## 1. Điều kiện và quy trình tích hợp

1. Tạo ứng dụng và cấp quyền lấy tài khoản (pull), dùng ID tài khoản thay vì tên hoặc ID chính sách.
2. Liên kết chính sách thông báo hoặc luân chuyển A/B để tự động phân phối (push).
3. Tải cấu hình danh tính từ trình hướng dẫn, dùng `instance_id` ổn định, duy nhất cho mỗi bản sao và bảo vệ AK/SK.

## 2. Cài đặt và khởi động

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

Chạy tại thư mục chứa `jms_pam_agent.json`. Dịch vụ Linux cần root và systemd. Đổi ID cho mỗi bản sao. Chế độ tiền cảnh cần bản nhị phân đúng hệ điều hành; macOS / Windows có thể cần biên dịch.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Chạy tiền cảnh

Trình cài đặt tích hợp cần Linux và root. Trên macOS, Linux không có root hoặc Windows, chọn JSON hoặc Socket trong trình hướng dẫn và chạy init-local cùng run --local --config. Chỉ khởi tạo một lần rồi dùng lại cấu hình cục bộ riêng tư; chế độ chạy trực tiếp không thực hiện thao tác systemd.

Đặt tên nhị phân `jms-pam-agent` (`jms-pam-agent.exe` trên Windows); Linux/macOS chạy `chmod 0755 ./jms-pam-agent`.

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

## 3. Chọn cách phân phối

JSON giao tệp, EnvironmentFile dùng dịch vụ systemd cố định, Unix Socket cung cấp API cục bộ. Agent ghi phiên bản đã giao sau thành công; ứng dụng ghi phiên bản đã áp dụng sau kiểm tra và sử dụng. Socket thuộc người dùng ứng dụng với quyền 0600; gửi yêu cầu bằng người dùng đó.

| Chọn cách phân phối | Cấu hình và chạy |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

Cấu hình Linux: `/etc/jms-pam-agent/agent.json` (0600). `state_file` lưu mật khẩu và tiến độ; `event_file` lưu siêu dữ liệu sự kiện. `delivery_root` và `socket_path` chọn nơi phân phối. Chế độ tiền cảnh dùng đường dẫn từ `agent.json` được tạo.

## 4. Cập nhật cấu hình và luân chuyển A/B

Cấu hình tải về đã có danh tính và thiết lập phân phối của Agent. Trong rules, khai báo ID tài khoản ứng dụng sử dụng, cách cập nhật cấu hình, áp dụng thay đổi và kiểm tra kết nối đang chạy. allow_account_switch dùng cùng một quy tắc cho cả hai chiều luân phiên A/B. credential_check tùy chọn kiểm tra đăng nhập mới trước khi sửa tệp. Đường dẫn trạng thái, sự kiện, Socket và chu kỳ đối chiếu 300 giây có giá trị mặc định. rules rỗng ghi tệp mặc định cho từng thông tin xác thực.

JSON đầu là cấu hình cơ bản. Để sửa tệp nghiệp vụ, thay `rules` rỗng bằng mảng tiếp theo. Chuẩn bị trước đường dẫn, dịch vụ và script mẫu. Cập nhật thường có thể bỏ `allow_account_switch`; A/B cần tùy chọn này và kiểm tra kết nối thực.

| Cấu hình và chạy | Cập nhật cấu hình và luân chuyển A/B |
| --- | --- |
| `credential_check` | Tùy chọn: kiểm tra đăng nhập mới trước khi sửa tệp. |
| `config_update` | Cập nhật tệp bằng ánh xạ trường, mẫu hoặc script. |
| `service_action` | Reload/restart dịch vụ cố định hoặc chạy script cố định. |
| `application_check` | Kiểm tra kết nối ứng dụng đang chạy rồi mới xác nhận. |

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

### Định dạng cấu hình

`config_update.format` mặc định là `auto`, kể cả khi bỏ qua trường này. Các đuôi `.txt`, `.env`, `.properties` dùng `env` (`KEY=value`); `.yml`, `.yaml` dùng `yaml` (`KEY: value` cấp cao nhất, giá trị mới ghi dưới dạng chuỗi có dấu ngoặc kép). Với đuôi khác, chỉ định `env` hoặc `yaml`. Mỗi trường ánh xạ phải tồn tại đúng một lần; các thiết lập khác được giữ nguyên. Với section INI hoặc YAML lồng nhau, dùng `files` + `template_file` để tạo toàn bộ tệp, hoặc `script`. Sau khi cập nhật tệp biến môi trường, ứng dụng phải đọc lại tệp hoặc khởi động lại.

rules cục bộ cấu hình tệp, JSON/EnvironmentFile hoặc mẫu tin cậy cùng systemd reload/restart hoặc chương trình cố định. Script nhận JSON qua stdin, dùng đối số cố định và giới hạn thời gian, rồi kiểm tra ứng dụng trước khi báo thành công. Core không được mở rộng quyền này. Khởi động lại Agent sau khi sửa cấu hình riêng tư.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. API cục bộ và xác nhận

### Truy vấn tài khoản và thông tin xác thực

Dùng người dùng ứng dụng cho CLI và Socket. `get_accounts` chỉ liệt kê siêu dữ liệu; `get_secret` lấy cả tài khoản chỉ pull. Mật khẩu xuất ra stdout, hãy dùng thiết bị đầu cuối riêng. Đổi đường dẫn Socket khi chạy cục bộ.

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

### Xác nhận ứng dụng đã áp dụng

Chỉ xác nhận sau khi kiểm tra và đổi kết nối thực. Dùng `application_check` với `confirm_on_success: true` hoặc xác nhận tường minh. Không gọi API xác nhận từ script kiểm tra đồng bộ. Kết quả lưu trước: `confirmed` đã được Core chấp nhận, `pending` sẽ thử lại.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Kiểm tra tích hợp

1. Kiểm tra bản thể trực tuyến và tài khoản được cấp quyền.
2. Với push, gửi cập nhật từ chính sách rồi kiểm tra tài khoản và phiên bản được phân phối.
3. Với A/B, xác minh kết nối mới và giải phóng kết nối cũ trước kết quả sự kiện. Lấy hoặc phân phối chưa chứng minh áp dụng.

### Khắc phục sự cố

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent đồng bộ lúc khởi động, theo sự kiện liên quan và mỗi 300 giây. Lỗi mạng giữ thông tin xác thực mới nhất đã lấy và được cấp quyền. Khi danh tính/quyền bị từ chối, chặn lấy qua Socket; đồng bộ có chữ ký thành công sẽ khôi phục. Tệp đã giao vẫn được giữ. SIGINT/SIGTERM đóng dịch vụ, kết nối và luồng đọc.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Tích hợp Python SDK

## 1. Điều kiện và quy trình tích hợp

1. Tạo ứng dụng và cấp quyền lấy tài khoản (pull), dùng ID tài khoản thay vì tên hoặc ID chính sách.
2. Liên kết chính sách thông báo hoặc luân chuyển A/B để tự động phân phối (push).
3. Tải cấu hình danh tính từ trình hướng dẫn, dùng `instance_id` ổn định, duy nhất cho mỗi bản sao và bảo vệ AK/SK.

Chỉ pull thì hoàn thành truy vấn đầu tiên. Push và A/B cần callback nghiệp vụ và lắng nghe. SDK xử lý giao thức và kết quả; ứng dụng đổi kết nối thực. Ví dụ theo mã nguồn hiện tại, cần bản Core/SDK tương thích. Gói 1.0.2 đã phát hành chưa có mọi API mới.

## 2. Cài SDK và cấu hình danh tính

Cài mã nguồn máy khách đi kèm cùng Core; gói 1.0.2 chưa có callback được quản lý. Thay jumpserver.example.com bằng địa chỉ Core và giữ thư mục jms-pam-clients, kể cả đường dẫn thay thế mô-đun Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Cài mã nguồn cục bộ (thay thế)

Nếu Core chưa cung cấp API tải mã nguồn hoặc triển khai ngoại tuyến, đặt mã nguồn tương thích ở `/path/to/pam-clients` rồi chạy trong thư mục ứng dụng. Thay bằng đường dẫn thực.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` là tệp cấu hình ứng dụng, không thuộc gói `jms-pam` trên PyPI. Tải từ trình hướng dẫn kết nối và lưu cạnh tập lệnh ứng dụng, hoặc tạo tệp theo nội dung bên dưới và thay các giá trị bằng thông tin ứng dụng thực tế. Không đưa `client_options` vào kho mã hay nhật ký. Dùng `instance_id` ổn định, duy nhất cho mỗi bản sao.

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

## 3. Lấy thông tin xác thực đầu tiên

Để thử tương tác, khởi động Python trong thư mục chứa `jms_pam_config.py`, rồi chạy ví dụ bên dưới. Có thể lưu ví dụ thành `app.py` trong cùng thư mục và chạy `python3 app.py`. Nếu gặp `ModuleNotFoundError: No module named 'jms_pam_config'`, kiểm tra tên và vị trí tệp; cài lại gói không tạo tệp này.

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

## 4. Hàm xử lý sự kiện

Triển khai hàm xử lý thay đổi tài khoản rồi bắt đầu bộ lắng nghe được quản lý. SDK xử lý ảnh chụp trạng thái, lấy dữ liệu trực tiếp, kiểm tra tài khoản và phiên bản, thử lại, nhận quyền thực thi lệnh và báo cáo kết quả. Chỉ trả về sau khi kiểm tra kết nối thật, chuyển nhóm kết nối và giải phóng kết nối cũ. Python và Node.js dùng lớp con, Go dùng OnAccountChanged, Java dùng AccountEventListener. Ví dụ sẽ thất bại cho đến khi triển khai chuyển đổi thực tế.

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

Các hàm xử lý chạy tuần tự qua hàng đợi giới hạn; cập nhật thường thất bại sẽ thử lại sau 1–30 giây. Chỉ báo thành công khi hàm nghiệp vụ hoàn tất. Nếu tải kết quả lên thất bại, cùng bộ lắng nghe chỉ gửi lại kết quả, không lặp lại tác vụ đã xong. Lệnh phải được chấp nhận quyền thực thi; tác vụ lệnh thất bại không tự chạy lại. Tài khoản đã áp dụng được tự báo cáo; chuyển A/B thay tài khoản hiện dùng. Cần triển khai giải phóng kết nối bị thu hồi. Báo cáo sử dụng, lấy mật khẩu hoặc giao tệp không tự xác nhận thành công nghiệp vụ. API cũ vẫn xác nhận tường minh. Bảo đảm tính lũy đẳng sau khi khởi động lại.

### Vòng đời lắng nghe và dừng

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events chờ dừng; start_events không đảm bảo đồng bộ ban đầu. Chờ ứng dụng sẵn sàng. Mỗi client chỉ có một bộ lắng nghe. stop_events và close chờ hàm xử lý; hàm có thể dừng client của mình. clone khởi tạo lại lớp con.

### Thông tin xác thực mới nhất và lỗi máy chủ

Luôn gọi API trước. Kết quả thành công thay thế giá trị đang giữ; phiên bản cũ không ghi đè bản mới và không có thời hạn hết hiệu lực theo thời gian. Chỉ hết thời gian chờ, lỗi mạng hoặc HTTP 5xx mới trả về giá trị thành công gần nhất của cùng bộ chọn với cờ nguồn cục bộ. Chưa có giá trị thì báo lỗi gốc. SDK giữ trong bộ nhớ client đến khi cập nhật, thu hồi hoặc đóng; clone và khởi động lại bắt đầu trống. Agent dùng trạng thái cục bộ được bảo vệ sẵn có. HTTP 401/403/404 xóa giá trị SDK rồi báo lỗi; phản hồi thành công không hợp lệ cũng báo lỗi. Thu hồi xóa mục liên quan, snapshot xóa mục ngoài quyền. Thay đổi cấu hình giữ giá trị đến khi kiểm tra snapshot tiếp theo. Agent áp dụng thu hồi rõ ràng và thu hẹp phạm vi snapshot trước khi đồng bộ HTTP, lưu phạm vi đó và chặn các lần lấy cục bộ liên quan ngay cả khi máy chủ lỗi hoặc sau khi khởi động lại. Phản hồi credential_not_found (HTTP 400) cũng xóa giá trị SDK được giữ lại. HTTP 426 báo lỗi rõ ràng nhưng không xóa giá trị đã giữ và không trả bộ nhớ đệm cho yêu cầu đó. API siêu dữ liệu tùy chọn không tồn tại (HTTP 404) không xóa thông tin xác thực. Agent không lưu bền vững giá trị chỉ pull để dùng ngoại tuyến; SDK có thể dùng bộ nhớ theo quy tắc lỗi tạm thời hiện có.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Khi bật lắng nghe được quản lý, snapshot và credential.updated tự lấy dữ liệu, thay thế giá trị và gọi hàm nghiệp vụ. Lỗi cập nhật giữ bản trước và thử lại. Agent cũng lấy lại theo thông báo, giữ dữ liệu khi máy chủ lỗi. Dùng lời gọi bắt buộc API bên dưới để cập nhật hay chuyển kết nối; giá trị đang giữ không phải phiên bản vừa lấy và không tự xác nhận luân chuyển.

Khi nhàn rỗi gửi ping mỗi 10 giây; khoảng 30 giây không có tin nhắn sẽ kết nối lại, chờ tăng theo hàm mũ 1–30 giây và ký mới. snapshot khôi phục trạng thái hiện tại, không phát lại lịch sử.



## 5. Phương thức thông dụng

### Liệt kê tài khoản được cấp quyền

Các phương thức danh sách truy vấn phạm vi pull hiện được cấp quyền của ứng dụng, chỉ trả siêu dữ liệu tài khoản, tài sản và chính sách push, không lấy mật khẩu hay ghi nhận lấy bí mật. Bao gồm tài khoản không có chính sách. Danh sách đầy đủ đọc mọi trang; phương thức phân trang trả `count` và `accounts` (Go trả danh sách và tổng riêng). Mặc định limit 200, offset 0; limit 1–500, search tối đa 128 ký tự để tìm tên tài khoản, tên người dùng, tên tài sản hoặc địa chỉ. Trùng lặp, thiếu kết quả hoặc tổng thay đổi gây lỗi; truy vấn lại toàn bộ khi quyền thay đổi. Khi cần bí mật, dùng ID tài khoản với phương thức lấy tài khoản. Ví dụ cần mã SDK có phương thức mới; bản 1.0.2 đã phát hành chưa có chúng trong Python, Java hoặc Node.js.

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


### Thông tin ứng dụng, bản sao và chính sách

get_application trả ID, tên, tổ chức và số tài khoản được cấp quyền cùng chính sách hiển thị của ứng dụng đã ký. get_client_info chỉ trả đăng ký, loại và phiên bản của bản sao hiện tại. Danh sách trả toàn bộ chính sách liên kết hiển thị, gồm chính sách vô hiệu; search tìm theo tên hoặc key. Truy vấn chính sách bằng ID hoặc key của chính sách, không dùng key push tài khoản. Chính sách luân phiên yêu cầu quyền cho cả hai tài khoản và tài khoản đang hoạt động; số tài khoản đăng ký chỉ tính tài khoản được cấp quyền. Không trả bí mật, lấy hay xác nhận thông tin xác thực. Lần truy cập đầu đăng ký bản sao. Cần Core và mã SDK mới; bản 1.0.2 đã phát hành chưa có phương thức này.

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


### Báo cáo trạng thái ứng dụng

Báo cáo ảnh chụp đầy đủ về trạng thái và ID tài khoản đang sử dụng. Trạng thái: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Danh sách rỗng xóa thông tin sử dụng trước đó. Gọi khi trạng thái thay đổi hoặc từ heartbeat của ứng dụng. Python, Java và Node.js trả về false khi lỗi mạng/API; Go cung cấp `TryReportStatus`. Biến thể nghiêm ngặt cung cấp chi tiết lỗi. Tham số không hợp lệ bị từ chối. Lời gọi dùng thời gian chờ HTTP đã cấu hình. Báo cáo không lấy bí mật, xác nhận luân chuyển hoặc xóa bộ nhớ đệm. Không đặt bí mật hay thông báo ngoại lệ trong `error_code`. ClientInfo trả về báo cáo cuối và thời gian.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Sự kiện cấp thấp và lệnh ứng dụng

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

Dùng list_application_commands để lấy yêu cầu chờ và execute_application_command(event, handler) để nhận quyền thực thi. Chỉ chạy khi accepted là true. Hàm chuyển kiểm tra tài khoản/phiên bản yêu cầu, áp dụng và xác nhận; hàm khởi động lại phải kiểm tra sức khỏe trước khi báo thành công. Lỗi báo cáo thất bại không che lỗi xử lý ban đầu.

API key cũ được giữ để tương thích. Tích hợp mới dùng `get_account` và `confirm_event`, đồng thời cần cập nhật Core.

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

## 6. Kiểm tra tích hợp

1. Lấy tài khoản được cấp quyền và thử kết nối thực.
2. Kiểm tra đồng bộ ban đầu và cập nhật; callback phải hoàn tất chuyển đổi trước khi báo thành công.
3. Kiểm tra trạng thái/sử dụng và kết quả sự kiện riêng. Lấy mật khẩu hoặc báo sử dụng không xác nhận áp dụng.

### Khắc phục sự cố

with hoặc close đóng phiên HTTP và luồng sự kiện; clone tạo phiên độc lập. Lỗi HTTP, mạng, xác thực và phân tích tạo PAMError có code, status_code, detail, original_error. Thử lại lỗi tạm thời tại ranh giới ứng dụng, xử lý rõ việc từ chối quyền và không ghi thông tin xác thực.

Mọi SDK dùng giao thức phiên bản 1; Agent dùng lược đồ cấu hình phiên bản 1. Khi nhận client_upgrade_required (HTTP 426), kiểm tra tương thích và nâng cấp. Cho phép trường tùy chọn và thông báo chưa biết; không áp dụng hay xác nhận chính sách chưa hỗ trợ. Biên nhận được gửi tự động và không chứng minh đã áp dụng thông tin xác thực. Thông tin máy khách có thể chứa server_capabilities. Thiếu hoặc null nghĩa là chưa biết; tên mới được chấp nhận. Danh sách mô tả tính năng máy chủ, không cấp quyền.

<!-- sdk-doc:end -->
