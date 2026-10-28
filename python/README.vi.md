# JumpServer PAM Python SDK / Agent

Python 3.9+ cung cấp SDK chính sách thông tin xác thực. Go jms-pam-agent phân phối qua tệp cục bộ, hành động cố định hoặc Socket cục bộ mà không cần Python. Chạy trực tiếp trên Linux, macOS và Windows; cài đặt systemd tích hợp chỉ dành cho Linux.

<!-- agent-doc:start -->

# Tích hợp Go Agent

Chạy Go Agent độc lập trên máy ứng dụng, không cần Python. Thực hiện lần lượt cài đặt, phân phối, thao tác nghiệp vụ và kiểm tra.

## 1. Điều kiện và quy trình tích hợp

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Download the identity configuration from the application access wizard. Give each replica a stable, unique instance ID and protect the AK/SK.

## 2. Cài đặt và khởi động

### Linux / systemd

Core và Agent dùng giao thức 1 và `config_version: 1`. Cấu hình `store` và `services` độc lập, không trộn với cấu trúc cũ `delivery` / `rules`. Core đồng bộ phạm vi tài khoản và phiên bản, không gửi đường dẫn thực thi cục bộ. Khởi động lại Agent sau khi sửa cấu hình riêng.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

Chạy tại thư mục chứa `jms_pam_agent.json`. Dịch vụ Linux cần root và systemd. Đổi ID cho mỗi bản sao. Chế độ tiền cảnh cần bản nhị phân đúng hệ điều hành; macOS / Windows có thể cần biên dịch.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Chạy tiền cảnh

Trên macOS, Windows hoặc Linux không có root, chạy `init-local` một lần rồi dùng lại cấu hình riêng bằng `run --local`. Hỗ trợ JSON và tệp môi trường; systemd chỉ dùng với dịch vụ Linux.

Đặt tên nhị phân `jms-pam-agent` (`jms-pam-agent.exe` trên Windows); Linux/macOS chạy `chmod 0755 ./jms-pam-agent`.

```bash
chmod 0600 ./jms_pam_agent.json
./jms-pam-agent init-local \
  --bootstrap ./jms_pam_agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app" --instance-id app-node-1
./jms-pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\jms-pam-agent.exe init-local --bootstrap (Join-Path $PWD 'jms_pam_agent.json') --directory $agentDir --instance-id app-node-1
.\jms-pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. store / local_api

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "config_version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "instance_id": "app-node-1",
  "pull_interval": 300,
  "local_api": {
    "owner": "appuser"
  },
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "services": []
}
```

`pull_interval` là chu kỳ truy vấn chung tính bằng giây, phải là số nguyên dương và mặc định là 300. Agent đồng bộ ngay khi khởi động, sau đó kiểm tra phiên bản tài khoản và thông tin xác thực theo chu kỳ này; chỉ lấy bí mật khi phiên bản hoặc tài khoản đang hoạt động thay đổi. Dịch vụ có `update_on_notification: false` cập nhật ở lần truy vấn kế tiếp; thông báo không trì hoãn chu kỳ này.

| Cấu hình và chạy | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Chỉ các ID tài khoản được khai báo rõ ràng. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

Mỗi tài khoản, kể cả A và B, có tệp riêng theo account_id. Tệp chứa thông tin xác thực dạng rõ với quyền 0600. Tài khoản không hoạt động đang đổi mật khẩu tạm thời không khả dụng. Đối soát quyền sẽ xóa tệp bị thu hồi. `local_api.owner` quản lý Socket và chủ sở hữu mặc định của tệp nghiệp vụ; mặc định lần lượt là `store.owner` rồi người dùng chạy Agent. Socket độc lập với lưu trữ. Phiên bản không đổi chỉ đọc siêu dữ liệu; lần lấy bí mật thực tế vẫn được kiểm toán và tính vào quan sát luân chuyển.

### EnvironmentFile / systemd

EnvironmentFile ghi tên người dùng, mật khẩu và siêu dữ liệu dạng văn bản thuần `KEY=value` vào tệp `.env` riêng (0600). Đặt `EnvironmentFile=` trong dịch vụ systemd của ứng dụng để nạp vào môi trường tiến trình khi khởi động. Không đổi biến toàn cục của máy. Cần khởi động lại dịch vụ sau cập nhật; reload không thay môi trường tiến trình chính đang chạy.

`services`:

```json
[
  {
    "name": "sample-app-env",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "files": [
        {
          "path": "/etc/sample-app/database.env",
          "format": "environment",
          "owner": "appuser"
        }
      ]
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    }
  }
]
```

```ini
JMS_PAM_ACCOUNT_ID="<account-id>"
JMS_PAM_USERNAME="db_user"
JMS_PAM_SECRET="<password>"
```

```ini
[Service]
EnvironmentFile=/etc/sample-app/database.env
```

## 4. Cập nhật cấu hình và luân chuyển A/B

Thêm các đối tượng dưới đây vào `services` với `name` duy nhất, ổn định. `allow_account_switch` ở cấp dịch vụ mặc định false: true theo tài khoản hiện hành của liên kết A/B rõ ràng; tài khoản khác giữ cố định. Chỉ cần khai báo A. `update_on_notification` cũng ở cấp dịch vụ, mặc định true. False đợi lần đối soát định kỳ tiếp theo, mặc định 300 giây; lưu trữ và thu hồi quyền vẫn hoạt động. Mỗi dịch vụ lưu phiên bản riêng. Chỉ tự xác nhận luân chuyển khi tất cả dịch vụ liên quan kiểm tra thành công với `app_check.confirm_on_success: true`.

Cấu hình dùng `update_config` → `reload_service` → `app_check`: cập nhật tệp, reload/restart dịch vụ (hoặc chạy script cố định), rồi kiểm tra ứng dụng đang chạy. Cần Agent đã cập nhật. Ba bước này trong `services` chỉ chấp nhận tên mới; tên cũ bị từ chối.

| Cấu hình và chạy | Cập nhật cấu hình và luân chuyển A/B |
| --- | --- |
| `credential_check` | Tùy chọn: kiểm tra đăng nhập mới trước khi sửa tệp. |
| `update_config` | Cập nhật tệp bằng ánh xạ trường, mẫu hoặc script. |
| `reload_service` | Reload/restart dịch vụ cố định hoặc chạy script cố định. |
| `app_check` | Kiểm tra kết nối ứng dụng đang chạy rồi mới xác nhận. |

`services`:

```json
[
  {
    "name": "sample-app",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "id": "<primary-account-id>"
      }
    ],
    "update_config": {
      "file": "/etc/sample-app/config.yml",
      "format": "yaml",
      "fields_map": {
        "DB_USER": "username",
        "DB_PASSWORD": "secret"
      }
    },
    "reload_service": {
      "unit": "sample-app.service",
      "operation": "restart"
    },
    "app_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

Trong `services.accounts`, dùng `id` để chọn tài khoản. Một tài khoản có thể dùng trực tiếp `username` / `secret`. Nhiều tài khoản cần alias duy nhất và tham chiếu như `database.username`, `cache.secret`. Alias không đổi khi chuyển A/B. `username` tùy chọn chỉ để đọc; giá trị thật lấy từ Core. Phải có đủ tài khoản trước khi ghi tệp, sau đó thực hiện hành động dịch vụ một lần. Ánh xạ thiếu hoặc mơ hồ phải báo lỗi trước khi sửa.

```json
{
  "name": "sample-app",
  "allow_account_switch": true,
  "update_on_notification": true,
  "accounts": [
    {
      "id": "<primary-account-id>",
      "alias": "database"
    },
    {
      "id": "<cache-account-id>",
      "alias": "cache"
    }
  ],
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_USER": "cache.username",
      "CACHE_PASSWORD": "cache.secret"
    }
  },
  "reload_service": {
    "unit": "sample-app.service",
    "operation": "restart"
  },
  "app_check": {
    "path": "/usr/local/libexec/jms-pam/check-running-db",
    "confirm_on_success": true
  }
}
```

### Định dạng cấu hình

`update_config.format` mặc định là `auto`, kể cả khi bỏ qua trường này. Các đuôi `.txt`, `.env`, `.properties` dùng `env` (`KEY=value`); `.yml`, `.yaml` dùng `yaml` (`KEY: value` cấp cao nhất, giá trị mới ghi dưới dạng chuỗi có dấu ngoặc kép). Với đuôi khác, chỉ định `env` hoặc `yaml`. Mỗi trường ánh xạ phải tồn tại đúng một lần; các thiết lập khác được giữ nguyên. Với section INI hoặc YAML lồng nhau, dùng `files` + `template_file` để tạo toàn bộ tệp, hoặc `script`. Sau khi cập nhật tệp biến môi trường, ứng dụng phải đọc lại tệp hoặc khởi động lại.

Core và Agent dùng giao thức 1 và `config_version: 1`. Cấu hình `store` và `services` độc lập, không trộn với cấu trúc cũ `delivery` / `rules`. Core đồng bộ phạm vi tài khoản và phiên bản, không gửi đường dẫn thực thi cục bộ. Khởi động lại Agent sau khi sửa cấu hình riêng.

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
  'http://localhost/v1/credential?account_id=<account-id>'
```

### Xác nhận ứng dụng đã áp dụng

Chỉ xác nhận sau khi kiểm tra và đổi kết nối thực. Dùng `app_check` với `confirm_on_success: true` hoặc xác nhận tường minh. Không gọi API xác nhận từ script kiểm tra đồng bộ. Kết quả lưu trước: `confirmed` đã được Core chấp nhận, `pending` sẽ thử lại.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Kiểm tra tích hợp

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Khắc phục sự cố

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent đồng bộ khi khởi động, có thông báo liên quan và theo chu kỳ, mặc định 300 giây. Lỗi tạm thời giữ giá trị còn quyền. Thu hồi rõ ràng hoặc vô hiệu hóa ứng dụng chặn đọc cục bộ và xóa tệp chuẩn ngay cả khi làm mới thất bại. Tệp nghiệp vụ do quản trị viên quản lý. Thông báo liên tục không trì hoãn đối soát định kỳ. SIGINT/SIGTERM đóng máy chủ và luồng đọc sự kiện.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Tích hợp Python SDK

## 1. Điều kiện và quy trình tích hợp

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
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

SDK gọi API trước và cập nhật giá trị giữ lại, không quay về phiên bản cũ. Chỉ timeout, lỗi mạng hoặc HTTP 5xx mới cho phép dùng giá trị đã lấy cho cùng bộ chọn, đánh dấu nguồn cục bộ và không hết hạn theo thời gian. Clone và khởi động lại SDK bắt đầu với bộ nhớ rỗng. HTTP 401/403/404 hoặc credential_not_found (400) xóa giá trị; 426 và phản hồi thành công sai định dạng không dùng dự phòng. API siêu dữ liệu tùy chọn bị thiếu không xóa bộ nhớ. Agent lưu tài khoản khai báo trong store/services; truy vấn tức thời chưa khai báo không tự lưu bền. Thu hồi rõ ràng có hiệu lực trước khi làm mới HTTP, cả khi ngoại tuyến. Snapshot push không xác định toàn bộ quyền lưu trữ; đối soát thành công kiểm tra quyền ứng dụng hiện tại.

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
