# JumpServer PAM Go SDK

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

## Yêu cầu môi trường

- Go 1.23+ / coder/websocket

## Cấu hình và chạy

Cài SDK và cấu hình bên dưới. Cấp quyền tài khoản cho pull trong quản lý ứng dụng; chỉ gắn chính sách khi cần push hoặc luân phiên thông tin xác thực. Lấy AK/SK và ID tổ chức từ tài liệu kết nối. Thay các giá trị mẫu và bảo vệ bí mật triển khai. Mỗi bản sao cần ID ổn định và duy nhất. Lấy theo ID tài khoản.

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go được phân phối bằng thẻ phiên bản mô-đun. Java và Node.js có gói nguồn SDK trong Release 1.0.2; cần Maven hoặc Node.js để cài. Chạy các lệnh trong thư mục ứng dụng và thêm phụ thuộc Java vào pom.xml. Ví dụ dùng gói đã cài.

```bash
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```

## Yêu cầu và phản hồi

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	pam "github.com/jumpserver/pam-clients/go"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint:   os.Getenv("JMS_ENDPOINT"),
		AppID:      os.Getenv("JMS_APP_ID"),
		AppSecret:  os.Getenv("JMS_APP_SECRET"),
		InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID:      os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	account, err := client.GetAccount(context.Background(), os.Getenv("JMS_ACCOUNT_ID"))
	if err != nil {
		log.Fatalf("Credential fetch failed: %T", err)
	}
	// Pass account.Username / Secret to the application connection pool.
	fmt.Printf("Fetched revision %d; implement application credential switching.\n", account.Revision)
}
```

## Sự kiện và áp dụng thông tin xác thực

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	pam "github.com/jumpserver/pam-clients/go"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func applyAccount(account pam.Account) error {
	return fmt.Errorf("implement connection validation, pool switching and old connection cleanup")
}
func applyEvent(ctx context.Context, client *pam.Client, event pam.Event) error {
	if event.Event == "application.restart.requested" {
		return fmt.Errorf("implement restart and health check")
	}
	account, err := client.GetAccountFresh(ctx, event.AccountID)
	if err != nil {
		return err
	}
	if account.Revision != event.AccountRevision {
		return fmt.Errorf("event account version is superseded")
	}
	return applyAccount(account)
}
func handleEvent(ctx context.Context, client *pam.Client, event pam.Event) error {
	if event.CommandID != "" {
		claim, err := client.ReportApplicationCommandResult(ctx, event.CommandID, "running", "")
		if err != nil {
			return err
		}
		if !claim.Accepted {
			return nil
		}
	}
	if err := applyEvent(ctx, client, event); err != nil {
		_, _ = client.ConfirmEvent(ctx, event.EventID, "failed", "application_failed")
		return err
	}
	_, err := client.ConfirmEvent(ctx, event.EventID, "success", "")
	return err
}
func main() {
	client, err := pam.NewClient(pam.Options{Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"), AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"), OrgID: os.Getenv("JMS_ORG_ID")})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = client.WatchCredentialEvents(ctx, func(event pam.Event) error {
		var updates []pam.Event
		if event.CommandID != "" || event.Event == "credential.updated" {
			updates = []pam.Event{event}
		} else if event.Event == "snapshot" {
			updates = event.Credentials /* Reconcile removed connections. */
		}
		// Release affected connections on credential.revoked.
		for _, update := range updates {
			if err := handleEvent(ctx, client, update); err != nil {
				log.Printf("Event processing failed: %T", err)
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Event stream failed: %T", err)
	}
}
```

API key cũ được giữ để tương thích. Tích hợp mới dùng `get_account` và `confirm_event`, đồng thời cần cập nhật Core.

## Lệnh ứng dụng

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

## Phương thức thông dụng

- `GetAccount(ctx, accountID)`
- `GetAccountFresh(ctx, accountID)`
- `ConfirmEvent(ctx, eventID, status, errorCode)`
- `WatchEvents(ctx, EventHandlers{...}) / StartEvents(ctx, handlers)`
- `EventWatcher.Stop() / EventWatcher.Wait()`
- `WatchCredentialEvents(ctx, handler)`
- `ListApplicationCommands(ctx)`
- `ReportApplicationCommandResult(ctx, commandID, status, errorCode)`
- `ExecuteApplicationCommand(ctx, event, handler)`
- `SyncAgent(ctx, AgentSyncOptions{...})`
- `Clone() / Close()`

## Khắc phục sự cố

Lỗi HTTP, mạng, xác thực và giải mã dùng kiểu lỗi SDK với mã và trạng thái HTTP. Đóng luồng và máy khách sau khi dùng; bản sao có vòng đời độc lập. Thử lại lỗi tạm thời ở tầng ứng dụng và không ghi bí mật hay tiêu đề xác thực vào nhật ký.

`PAMError`

Mọi SDK dùng giao thức phiên bản 1; Agent dùng lược đồ cấu hình phiên bản 1. Khi nhận client_upgrade_required (HTTP 426), kiểm tra tương thích và nâng cấp. Cho phép trường tùy chọn và thông báo chưa biết; không áp dụng hay xác nhận chính sách chưa hỗ trợ. Biên nhận được gửi tự động và không chứng minh đã áp dụng thông tin xác thực.

## Tích hợp Go Agent

Danh tính dùng app_id, app_secret, org_id và instance_id ổn định; quyền theo chính sách gắn với ứng dụng. Đường dẫn và thao tác dịch vụ đều ở máy cục bộ: state_file giữ mật khẩu mới nhất, event_file ghi sự kiện không có bí mật, delivery chọn đầu ra mặc định, rules đặt tệp, mẫu và reload/restart hoặc tập lệnh cố định. Khi nhận thông báo, Agent lấy và lưu mật khẩu hiện tại, thay tệp nguyên tử rồi chạy thao tác; lỗi sẽ được thử lại. Dùng credentials[].key từ get_accounts cho rules; key đăng ký chứa ID tài khoản. rules rỗng ghi một tệp cho mỗi key.

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
