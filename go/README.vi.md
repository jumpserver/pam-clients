# JumpServer PAM Go SDK

<!-- sdk-doc:start -->

## 1. Điều kiện và quy trình tích hợp

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Chỉ pull thì hoàn thành truy vấn đầu tiên. Push và A/B cần callback nghiệp vụ và lắng nghe. SDK xử lý giao thức và kết quả; ứng dụng đổi kết nối thực. Ví dụ theo mã nguồn hiện tại, cần bản Core/SDK tương thích. Gói 1.0.2 đã phát hành chưa có mọi API mới.

- Go 1.23+ / coder/websocket

## 2. Cài SDK và cấu hình danh tính

Install the SDK below and use the identity configuration downloaded from the application access wizard. Protect AK/SK and assign each replica a stable, unique instance ID.

Cài mã nguồn máy khách đi kèm cùng Core; gói 1.0.2 chưa có callback được quản lý. Thay jumpserver.example.com bằng địa chỉ Core và giữ thư mục jms-pam-clients, kể cả đường dẫn thay thế mô-đun Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/go/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
go mod edit -replace github.com/jumpserver/pam-clients/go=./jms-pam-clients/go &&
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```

### Cài mã nguồn cục bộ (thay thế)

Nếu Core chưa cung cấp API tải mã nguồn hoặc triển khai ngoại tuyến, đặt mã nguồn tương thích ở `/path/to/pam-clients` rồi chạy trong thư mục ứng dụng. Thay bằng đường dẫn thực.

```bash
go mod edit -replace=github.com/jumpserver/pam-clients/go=/path/to/pam-clients/go
go mod tidy
```


```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
```

## 3. Lấy thông tin xác thực đầu tiên

`demo.go`

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

```bash
go run ./demo.go
```

## 4. Hàm xử lý sự kiện

Triển khai hàm xử lý thay đổi tài khoản rồi bắt đầu bộ lắng nghe được quản lý. SDK xử lý ảnh chụp trạng thái, lấy dữ liệu trực tiếp, kiểm tra tài khoản và phiên bản, thử lại, nhận quyền thực thi lệnh và báo cáo kết quả. Chỉ trả về sau khi kiểm tra kết nối thật, chuyển nhóm kết nối và giải phóng kết nối cũ. Python và Node.js dùng lớp con, Go dùng OnAccountChanged, Java dùng AccountEventListener. Ví dụ sẽ thất bại cho đến khi triển khai chuyển đổi thực tế.

`events.go`

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

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// SDK handles snapshots, fresh fetches, versions, retries and event results.
	// Applied accounts are reported automatically. Pull-only code can call
	// client.ReportAccounts(ctx, []string{accountID}); an empty list clears usage.
	err = client.WatchEvents(ctx, pam.EventHandlers{
		OnAccountChanged: func(ctx context.Context, account pam.Account) error {
			// Validate, switch the connection pool and release old connections.
			// Return nil only after the application uses this account.
			return fmt.Errorf("implement application account switching")
		},
		OnAccountRevoked: func(ctx context.Context, accountID string) error {
			return fmt.Errorf("implement revoked account cleanup")
		},
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("Event stream failed: %T", err)
	}
}
```

```bash
go run ./events.go
```

Các hàm xử lý chạy tuần tự qua hàng đợi giới hạn; cập nhật thường thất bại sẽ thử lại sau 1–30 giây. Chỉ báo thành công khi hàm nghiệp vụ hoàn tất. Nếu tải kết quả lên thất bại, cùng bộ lắng nghe chỉ gửi lại kết quả, không lặp lại tác vụ đã xong. Lệnh phải được chấp nhận quyền thực thi; tác vụ lệnh thất bại không tự chạy lại. Tài khoản đã áp dụng được tự báo cáo; chuyển A/B thay tài khoản hiện dùng. Cần triển khai giải phóng kết nối bị thu hồi. Báo cáo sử dụng, lấy mật khẩu hoặc giao tệp không tự xác nhận thành công nghiệp vụ. API cũ vẫn xác nhận tường minh. Bảo đảm tính lũy đẳng sau khi khởi động lại.

### Vòng đời lắng nghe và dừng

`WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)`; `Stop()` / `Wait()`; `context.CancelFunc`

WatchEvents chờ trong goroutine gọi; StartEvents không đảm bảo đồng bộ ban đầu. Một bộ lắng nghe mỗi client. Stop và Close yêu cầu hủy; gọi Wait bên ngoài hàm xử lý. Tác vụ dài phải xử lý việc hủy context.

### Thông tin xác thực mới nhất và lỗi máy chủ

SDK gọi API trước và cập nhật giá trị giữ lại, không quay về phiên bản cũ. Chỉ timeout, lỗi mạng hoặc HTTP 5xx mới cho phép dùng giá trị đã lấy cho cùng bộ chọn, đánh dấu nguồn cục bộ và không hết hạn theo thời gian. Clone và khởi động lại SDK bắt đầu với bộ nhớ rỗng. HTTP 401/403/404 hoặc credential_not_found (400) xóa giá trị; 426 và phản hồi thành công sai định dạng không dùng dự phòng. API siêu dữ liệu tùy chọn bị thiếu không xóa bộ nhớ. PAM Agent lưu tài khoản khai báo trong store/services; truy vấn tức thời chưa khai báo không tự lưu bền. Thu hồi rõ ràng có hiệu lực trước khi làm mới HTTP, cả khi ngoại tuyến. Snapshot push không xác định toàn bộ quyền lưu trữ; đối soát thành công kiểm tra quyền ứng dụng hiện tại.

- `account.FromLocal`
- `GetAccountFresh(ctx, accountID)`

Khi bật lắng nghe được quản lý, snapshot và credential.secret.updated / credential.switch.published / credential.refresh.requested tự lấy dữ liệu, thay thế giá trị và gọi hàm nghiệp vụ. Lỗi cập nhật giữ bản trước và thử lại. PAM Agent cũng lấy lại theo thông báo, giữ dữ liệu khi máy chủ lỗi. Dùng lời gọi bắt buộc API bên dưới để cập nhật hay chuyển kết nối; giá trị đang giữ không phải phiên bản vừa lấy và không tự xác nhận luân chuyển.




## 5. Phương thức thông dụng

### Liệt kê tài khoản được cấp quyền

Các phương thức danh sách truy vấn phạm vi pull hiện được cấp quyền của ứng dụng, chỉ trả siêu dữ liệu tài khoản, tài sản và chính sách push, không lấy mật khẩu hay ghi nhận lấy bí mật. Bao gồm tài khoản không có chính sách. Danh sách đầy đủ đọc mọi trang; phương thức phân trang trả `count` và `accounts` (Go trả danh sách và tổng riêng). Mặc định limit 200, offset 0; limit 1–500, search tối đa 128 ký tự để tìm tên tài khoản, tên người dùng, tên tài sản hoặc địa chỉ. Trùng lặp, thiếu kết quả hoặc tổng thay đổi gây lỗi; truy vấn lại toàn bộ khi quyền thay đổi. Khi cần bí mật, dùng ID tài khoản với phương thức lấy tài khoản. Ví dụ cần mã SDK có phương thức mới; bản 1.0.2 đã phát hành chưa có chúng trong Python, Java hoặc Node.js.

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
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx := context.Background()
	accounts, err := client.ListAccounts(ctx, "")
	if err != nil {
		log.Fatalf("Account listing failed: %T", err)
	}
	for _, account := range accounts {
		fmt.Println(account.ID, account.Username, account.Asset.Name)
	}
	_, count, err := client.ListAccountsPage(ctx, 20, 0, "database")
	if err != nil {
		log.Fatalf("Account page failed: %T", err)
	}
	fmt.Println("Matching accounts:", count)
	// Use an authorized account.ID with GetAccount when its secret is needed.
}
```


### Thông tin ứng dụng, bản sao và chính sách

GetApplication trả ID, tên, tổ chức và số tài khoản được cấp quyền cùng chính sách hiển thị của ứng dụng đã ký. GetClientInfo chỉ trả đăng ký, loại và phiên bản của bản sao hiện tại. Danh sách trả toàn bộ chính sách liên kết hiển thị, gồm chính sách vô hiệu; search tìm theo tên hoặc key. Truy vấn chính sách bằng ID hoặc key của chính sách, không dùng key push tài khoản. Chính sách luân phiên yêu cầu quyền cho cả hai tài khoản và tài khoản đang hoạt động; số tài khoản đăng ký chỉ tính tài khoản được cấp quyền. Không trả bí mật, lấy hay xác nhận thông tin xác thực. Lần truy cập đầu đăng ký bản sao. Cần Core và mã SDK mới; bản 1.0.2 đã phát hành chưa có phương thức này.

```go
package main

import (
	"context"
	"fmt"
	pam "github.com/jumpserver/pam-clients/go"
	"log"
	"os"
)

func main() {
	client, err := pam.NewClient(pam.Options{
		Endpoint: os.Getenv("JMS_ENDPOINT"), AppID: os.Getenv("JMS_APP_ID"),
		AppSecret: os.Getenv("JMS_APP_SECRET"), InstanceID: os.Getenv("JMS_INSTANCE_ID"),
		OrgID: os.Getenv("JMS_ORG_ID"),
	})
	if err != nil {
		log.Fatal("Invalid SDK configuration")
	}
	defer client.Close()
	ctx := context.Background()
	application, err := client.GetApplication(ctx)
	if err != nil {
		log.Fatalf("Application query failed: %T", err)
	}
	fmt.Println(application.ID, application.Name, application.AccountsCount)
	replica, err := client.GetClientInfo(ctx)
	if err != nil {
		log.Fatalf("Client query failed: %T", err)
	}
	fmt.Println(replica.InstanceID, replica.ClientVersion)
	policies, err := client.ListPolicies(ctx, "")
	if err != nil {
		log.Fatalf("Policy listing failed: %T", err)
	}
	for _, policy := range policies {
		fmt.Println(policy.Key, policy.Name, policy.IsActive, policy.Status)
	}
	if len(policies) > 0 {
		policy, err := client.GetPolicy(ctx, pam.PolicySelector{PolicyID: policies[0].ID})
		if err != nil {
			log.Fatalf("Policy query failed: %T", err)
		}
		fmt.Println(policy.Name, policy.AccountsCount)
	}
}
```


### Báo cáo trạng thái ứng dụng

Báo cáo ảnh chụp đầy đủ về trạng thái và ID tài khoản đang sử dụng. Trạng thái: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Danh sách rỗng xóa thông tin sử dụng trước đó. Gọi khi trạng thái thay đổi hoặc từ heartbeat của ứng dụng. Python, Java và Node.js trả về false khi lỗi mạng/API; Go cung cấp `TryReportStatus`. Biến thể nghiêm ngặt cung cấp chi tiết lỗi. Tham số không hợp lệ bị từ chối. Lời gọi dùng thời gian chờ HTTP đã cấu hình. Báo cáo không lấy bí mật, xác nhận luân chuyển hoặc xóa bộ nhớ đệm. Không đặt bí mật hay thông báo ngoại lệ trong `error_code`. ClientInfo trả về báo cáo cuối và thời gian.

```go
err := client.ReportAccounts(ctx, []string{account.ID})
// ReportAccounts(ctx, nil) clears the complete explicit usage snapshot.
// ReportStatus(ctx, report) also reports application health.
```


### API

- `GetApplication(ctx) / GetClientInfo(ctx)`
- `ReportAccounts(ctx, accountIDs) / TryReportStatus(ctx, ApplicationStatus{Status: ..., AccountIDs: ...}) / ReportStatus(ctx, report)`
- `ListPolicies(ctx, search) / GetPolicy(ctx, PolicySelector{Key: ...})`
- `ListAccounts(ctx, search)`
- `ListAccountsPage(ctx, limit, offset, search)`
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

## 6. Kiểm tra tích hợp

1. Lấy tài khoản được cấp quyền và thử kết nối thực.
2. Kiểm tra đồng bộ ban đầu và cập nhật; callback phải hoàn tất chuyển đổi trước khi báo thành công.
3. Kiểm tra trạng thái/sử dụng và kết quả sự kiện riêng. Lấy mật khẩu hoặc báo sử dụng không xác nhận áp dụng.

### Khắc phục sự cố

Lỗi HTTP, mạng, xác thực và giải mã dùng kiểu lỗi SDK với mã và trạng thái HTTP. Đóng luồng và máy khách sau khi dùng; bản sao có vòng đời độc lập. Thử lại lỗi tạm thời ở tầng ứng dụng và không ghi bí mật hay tiêu đề xác thực vào nhật ký.

`PAMError`



<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# Application integration overview

## Authorization, notifications and rotation

1. **Account authorization** controls which accounts the application may fetch on demand. Authorize the application before installing a client.
2. **Account credential change notifications** is an application-level switch covering all authorized accounts. Use it when applications should react to password changes; no subscription policy is required.
3. **A/B rotation** is a separately bound policy for switching between two accounts before changing the original password. Both accounts must be authorized. Open the policy’s operation guide for its cycle, waiting conditions, cancellation and recovery.

## Choose an integration

Choose by who applies PAM-managed usernames and passwords to the service. Socket API is an application integration through PAM Agent: the application calls a local API while the Agent handles communication with PAM.

| Method | Integration characteristics | Responsibilities |
| --- | --- | --- |
| PAM Agent | Integrate through configuration, templates or adaptation scripts; suitable for existing services. | The Agent retrieves and refreshes the accounts required by the service. Configure field mappings, templates or adaptation scripts to update service configuration, reload or restart the service, and verify that the credentials are in use. |
| SDK | Use the SDK for the application language and call it directly from business code. | The application embeds a language-specific SDK and connects directly to PAM. The SDK handles PAM communication and event processing; the application implements connection validation, credential changes and release of old connections in business callbacks. |
| Socket API | Cross-language integration without embedding a JumpServer SDK; the Agent can be upgraded independently of the application. | The application uses HTTP / WebSocket over the local Agent's Unix Socket to retrieve credentials, receive notifications and report results. The Agent handles PAM authentication, credential refresh and reporting retries; the application validates and switches its own connections. |

Agent automation follows the configured service adaptation and checks. For every method, successful credential retrieval or delivery alone does not prove that the service has switched successfully.

## Setup and acceptance

1. Create an application, authorize accounts and download its private identity configuration.
2. Choose Agent service adaptation, an embedded SDK or the Agent's Socket API according to who will apply credentials. Install the matching components, fetch an account and verify a real connection.
3. For automatic updates, enable application notifications and configure Agent business actions or implement application connection changes. For A/B switching, also bind a rotation policy and enable following its current account.
4. Verify one account update or A/B switch: the application uses the new credential, old connections are released and the business result is successful. Reception, fetching and file delivery alone are insufficient.

## Where to find the details

Agent integration covers deployment and service adaptation: credential files, configuration updates, reload/restart actions and validation. SDK integration covers language-specific installation and business callbacks. Socket API access covers local API calls, credential retrieval, notifications, business results and A/B switching. Events and commands is the shared reference for event payloads, snapshots, receipts and administrator commands. The operation guide inside each rotation policy explains its own cycle, blockers and recovery.

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# Events and commands

This reference applies to all SDK languages and PAM Agent. Prefer managed SDK callbacks or configured Agent services for normal integration; use the raw protocol only when implementing an event consumer yourself.

## Event responsibilities

| Event | Responsibility |
| --- | --- |
| credential.secret.updated / credential.switch.published / credential.refresh.requested and snapshot items | Fetch the named account, verify its version, apply it and report the business result. Managed SDK callbacks handle the protocol. |
| app.scope.updated | Reconcile scope and remove invalid state. Removing a rotation binding does not revoke account authorization. |
| `rotation.*` (such as `rotation.started` and `rotation.completed`), `credential.change.*` (such as `credential.change.started` and `credential.change.completed`) | Update progress, logs or alerts. Do not repeat credential application or confirm business success for these notifications. |
| credential.switch.requested and app.restart.requested | Administrator commands for selected instances: claim, execute and report the command result. |

## Complete event catalog

Current application events, rotation operation commands and connection control messages. Rotation details display progress; each event has one execution responsibility.

### Credential and scope events

credential.secret.updated / credential.switch.published / credential.refresh.requested applies credentials. app.scope.updated and snapshot reconcile scope: detach a missing rotation policy while preserving use of authorized accounts. Only actual account authorization removal disables local access, clears retained credentials and asks the application to release connections.

| Event code | Meaning |
| --- | --- |
| `credential.secret.updated` | The account secret version changed. Fetch and apply the named account; this does not change the selected account. |
| `credential.switch.published` | A target account and policy revision were published for switching, including return to the source account. Fetch and apply the named target; publication is not application success. |
| `credential.refresh.requested` | Fetch and apply the currently published credentials for an authorization grant, manual refresh or snapshot reconciliation. This does not mean the password changed or a rotation started. |
| `app.scope.updated` | Account authorization, notification settings or rotation bindings changed. Use authorized_account_ids to determine account access, then snapshot to reconcile notifications and rotation following. Removing a binding must not close an otherwise authorized account connection. |

### General password-change events

General task lifecycle notifications for progress and alerts. Do not fetch again or report application success for these notifications; apply credentials on credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot.

| Event code | Meaning |
| --- | --- |
| `credential.change.started` | A password-change task is queued or running; the remote password may not have changed yet. |
| `credential.change.completed` | The password-change task succeeded; this is not proof that applications applied the credentials. |
| `credential.change.failed` | The password change failed or could not be verified; inspect the task and remote account. |

### Rotation progress events

These notifications explain cycle progress and outcomes. Application switching follows credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot, not a second action on each progress event.

| Event code | Meaning |
| --- | --- |
| `rotation.verify.started` | Backup-account login verification started. |
| `rotation.verify.skipped` | Backup-account login verification skipped by credential policy. |
| `rotation.verify.failed` | Backup verification failed; no switch is published. |
| `rotation.verify.cancelled` | Backup verification was cancelled. |
| `rotation.started` | The rotation cycle has published the target account and started switching. |
| `rotation.waiting_for_app` | Waiting for required application instances to apply and confirm the target. |
| `rotation.source.waiting` | Chỉ bắt đầu theo dõi tài khoản nguồn sau khi mọi phiên bản xác nhận hoặc bỏ qua kiểm tra ứng dụng và quản trị viên đã duyệt các lỗi. |
| `rotation.source.ready` | The original account no-fetch window and required client conditions allow password change. |
| `rotation.completed` | The original account password change was verified and the rotation completed. |
| `rotation.failed` | Rotation entered a password-change failure or recovery-required state. |
| `rotation.force_stopped` | An administrator ended the cycle without waiting for remaining recovery confirmations. |

### Manual account rotation operations

credential.switch.requested asks selected instances to apply the account and revision already published by the rotation policy. Administrators can manually apply or retry the switch; the command does not start a new rotation.

| Event code | Meaning |
| --- | --- |
| `credential.switch.requested` | Apply the already published account and version on selected instances; this does not start a rotation. |

### Application operation commands

app.restart.requested restarts the application and verifies its health. Manual account switching belongs to the account rotation operations above.

| Event code | Meaning |
| --- | --- |
| `app.restart.requested` | Execute the supported application restart action and verify health. |

### Connection and receipt messages

SDK and Agent manage liveness and receipts. snapshot and pong need no received receipt; snapshot credential items are applied separately.

| Event code | Meaning |
| --- | --- |
| `snapshot` | Core sends the current notification scope and authorized account IDs on connection, reconnection or scope changes. |
| `ping` | The client sends an application-level keepalive to Core. |
| `pong` | Core replies to an application-level ping. |
| `received` | The client acknowledges reading an event; it does not report business success. |

## Đối chiếu hoạt động và sự kiện giao thức

Nhật ký hoạt động ghi thao tác kiểm toán; nhật ký sự kiện hiển thị thông báo và lệnh cùng trạng thái giao và xử lý. Thao tác kiểm toán và thông báo là hai danh mục khác nhau. Bảng chỉ nêu liên hệ có thể có, không xác nhận đã giao; xem sự kiện và kết quả thực trong chi tiết. Biên nhận, lấy, áp dụng, bỏ qua và duyệt được ghi riêng. credential_fetched nằm trong nhật ký lấy thông tin.

| Loại hoạt động | Mã hoạt động | Thông báo / thông điệp liên quan | Meaning |
| --- | --- | --- | --- |
| Gửi lệnh ứng dụng | `command_requested` | `credential.switch.requested`<br>`app.restart.requested` | Yêu cầu phiên bản được chọn áp dụng thông tin đã công bố hoặc khởi động lại; không bắt đầu vòng luân chuyển mới. |
| Kết quả xử lý ứng dụng | `command_result` | `credential.secret.updated`<br>`credential.switch.published`<br>`credential.refresh.requested`<br>`credential.switch.requested`<br>`app.restart.requested` | Máy khách báo tiến độ và kết quả áp dụng hoặc lệnh. Lỗi luân chuyển vẫn cần quản trị viên duyệt dù thử lại thành công. |
| Tạo cấu hình tích hợp hoặc luân chuyển | `configuration_created` | `app.scope.updated` | Tạo ứng dụng hoặc chính sách; thay đổi phạm vi có thể phát app.scope.updated. |
| Sửa cấu hình tích hợp hoặc luân chuyển | `configuration_updated` | `app.scope.updated` | Thay đổi thiết lập; thay đổi phạm vi có thể phát app.scope.updated. |
| Xóa cấu hình tích hợp hoặc luân chuyển | `configuration_deleted` | Không có sự kiện giao thức cố định | Xóa ứng dụng hoặc chính sách; không phải kết quả áp dụng thông tin đăng nhập. |
| Thêm quyền hoặc liên kết thông tin đăng nhập | `authorization_granted` | `credential.refresh.requested` | Thêm quyền hoặc liên kết luân chuyển và yêu cầu làm mới; liên kết không mở rộng quyền tài khoản. |
| Gỡ quyền hoặc liên kết thông tin đăng nhập | `authorization_revoked` | `app.scope.updated` | Gỡ quyền hoặc liên kết. Chỉ giải phóng kết nối nghiệp vụ khi thực sự mất quyền tài khoản. |
| Lấy thông tin đăng nhập | `credential_fetched` | Không có sự kiện giao thức cố định | Ghi trong nhật ký lấy riêng; lấy thành công không chứng minh nghiệp vụ đã sử dụng. |
| Cập nhật phiên bản mật khẩu tài khoản | `credential_secret_updated` | `credential.secret.updated` | Phiên bản thay đổi và cần được lấy rồi áp dụng; không đổi tài khoản A/B đang chọn. |
| Công bố chuyển tài khoản | `credential_switch_published` | `credential.switch.published` | Công bố tài khoản đích hoặc quay về cùng phiên bản chính sách; không chứng minh ứng dụng đã chuyển. |
| Yêu cầu áp dụng thông tin hiện tại | `credential_refresh_requested` | `credential.refresh.requested` | Yêu cầu lấy và áp dụng phiên bản công bố; không có nghĩa mật khẩu đổi hoặc vòng luân chuyển mới bắt đầu. |
| Công bố lại thông báo thông tin đăng nhập | `credential_republished` | `credential.refresh.requested`<br>`snapshot` | Làm mới thủ công hoặc đối soát snapshot yêu cầu áp dụng lại; phiên bản mật khẩu có thể không đổi. |
| Xác nhận đã áp dụng thông tin mới | `credential_confirmed` | Không có sự kiện giao thức cố định | Máy khách xác nhận áp dụng phiên bản hiện tại; tách biệt với biên nhận và việc lấy thông tin. |
| Bỏ qua kiểm tra ứng dụng | `credential_confirmation_skipped` | Không có sự kiện giao thức cố định | Các bước giao khác thành công và máy khách báo skip cho kiểm tra tùy chọn; không phải kiểm tra đạt. |
| Quản trị viên duyệt tiếp tục | `application_failure_reviewed` | Không có sự kiện giao thức cố định | Quản trị viên ghi lý do và gỡ chặn do lỗi; giữ nguyên bản ghi lỗi gốc. |
| Kết nối luồng sự kiện thông tin đăng nhập | `credential_stream_connected` | `snapshot` | Máy khách kết nối và đối soát phạm vi qua snapshot; không chứng minh áp dụng thành công. |
| Ngắt luồng sự kiện thông tin đăng nhập | `credential_stream_disconnected` | Không có sự kiện giao thức cố định | Luồng thông báo bị ngắt; riêng việc này không chứng minh kết nối nghiệp vụ lỗi. |
| Thay đổi giai đoạn luân chuyển | `rotation_step` | `rotation.verify.started`<br>`rotation.verify.skipped`<br>`rotation.verify.failed`<br>`rotation.verify.cancelled`<br>`rotation.waiting_for_app`<br>`rotation.source.waiting`<br>`rotation.source.ready`<br>`rotation.completed`<br>`rotation.failed` | Ghi kiểm tra, chờ ứng dụng, theo dõi tài khoản nguồn và kết quả; xem giai đoạn cụ thể trong chi tiết. |
| Bắt đầu luân chuyển tài khoản | `rotation_started` | `rotation.started` | Đã công bố tài khoản đích và bắt đầu chuyển ứng dụng; vòng luân chuyển chưa hoàn tất. |
| Hủy hoặc buộc kết thúc luân chuyển | `rotation_cancelled` | `credential.switch.published`<br>`rotation.force_stopped` | Hủy sẽ chờ xác nhận quay về tài khoản nguồn; buộc kết thúc sẽ bỏ các lần chờ phục hồi còn lại. |
| Bắt đầu đổi mật khẩu tài khoản | `secret_change_started` | `credential.change.started` | Tác vụ đã xếp hàng hoặc bắt đầu; mật khẩu từ xa có thể chưa đổi. |
| Hoàn tất đổi mật khẩu tài khoản | `secret_change_completed` | `credential.change.completed` | Tác vụ thành công; không chứng minh ứng dụng đã áp dụng thông tin đăng nhập. |
| Đổi mật khẩu tài khoản thất bại | `secret_change_failed` | `credential.change.failed` | Tác vụ lỗi hoặc kết quả chưa rõ; kiểm tra tác vụ và tài khoản từ xa. |
| Đăng ký phiên bản máy khách | `client_registered` | Không có sự kiện giao thức cố định | Đăng ký phiên bản SDK hoặc Agent; không chứng minh áp dụng thông tin đăng nhập. |
| Bật phiên bản máy khách | `client_enabled` | Không có sự kiện giao thức cố định | Khôi phục truy cập trong quyền hiện có; không cấp quyền tài khoản mới. |
| Tắt phiên bản máy khách | `client_disabled` | Không có sự kiện giao thức cố định | Các lần truy cập tiếp theo bị từ chối; thao tác này không đổi mật khẩu tài khoản. |
| Giao thông báo ứng dụng | `notification` | Không có sự kiện giao thức cố định | Ghi giao Webhook và thử lại. Giao HTTP không chứng minh áp dụng thành công; xem mã sự kiện thực trong chi tiết. |
| Đặt lại khóa truy cập ứng dụng | `application_secret_reset` | Không có sự kiện giao thức cố định | Đổi khóa xác thực ứng dụng với JumpServer; tách biệt với mật khẩu tài khoản nghiệp vụ. |

## Payload fields

| Field | Meaning |
| --- | --- |
| event_id | Event identity for receipts and application results. snapshot itself has no receipt; credential items can have their own event_id. |
| event / occurred_at | Event type and publication time. |
| account_id / account_revision | Concrete target account and password version. A fresh account revision must match before application. |
| credential_key / revision | Policy publication metadata for A/B rotation. Ordinary account notifications use account revisions. Do not mix the two version domains. |
| credential_mode | alternating_rotation for an A/B policy; subscription for application-level account notifications, without a subscription policy. Application-wide events may use null. |
| account_switch.account_ids | Accounts associated with an A/B switch. |
| operation_id | Correlates events belonging to one change operation or rotation cycle. |
| command_id / expires_at | Administrator command identity and execution deadline. Commands have a separate result API. |

## Snapshots and authorization

Core gửi snapshot khi kết nối, kết nối lại và thay đổi phạm vi. credentials gồm tất cả tài khoản được cấp quyền khi bật thông báo, cùng các chính sách A/B đã liên kết, đang hoạt động và còn quyền. Xử lý từng mục như credential.secret.updated / credential.switch.published / credential.refresh.requested với event_id, account_id và account_revision. authorized_account_ids chỉ phạm vi cấp quyền; get_accounts trả siêu dữ liệu. Bỏ liên kết chính sách không thu hồi quyền tài khoản.

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## Receipts and business results

SDK tự động cố gắng gửi biên nhận received trước khi trả về sự kiện nghiệp vụ. Ứng dụng không cần gửi thêm. Biên nhận chỉ cho biết đã đọc sự kiện, không có nghĩa là đã áp dụng thông tin xác thực và không thay thế xác nhận. Đối chiếu snapshot ban đầu và khi kết nối lại; snapshot và pong không cần biên nhận.

Kết quả là success, failed hoặc skip. Service cục bộ không có app_check báo skip khi các bước khác thành công, không tính là ứng dụng đã xác nhận. failed chặn bước tiếp theo dù thử lại thành công hoặc bỏ qua. Quản trị viên phải duyệt tiếp tục kèm lý do; lỗi gốc được giữ lại.

Quản trị viên dùng phiên người dùng có quyền accounts.change_applicationcredential. Gửi failure_id hiện tại và lý do; lỗi hoặc phiên bản chính sách lỗi thời sẽ bị từ chối. Việc duyệt được ghi nhật ký và giữ nguyên kết quả khách hàng.

```http
POST /api/v1/accounts/application-credentials/<policy-id>/review-application-failure/
Content-Type: application/json

{"client_id": "<client-id>", "failure_id": "<current-failure-id>", "reason": "<review-reason>"}
```

```json
{"event": "received", "event_id": "<event-id>"}
```

Lấy tài khoản bằng `account_id`. Xác minh kết nối và áp dụng thay đổi trước khi báo `success` hoặc `failed` qua `event_id`. Nhận sự kiện hoặc lấy mật khẩu không có nghĩa là áp dụng thành công. Sự kiện và snapshot có `event_id`, `account_id`, `account_revision`; hãy kiểm tra phiên bản. Nhận lệnh bằng `running` và xử lý lặp lại an toàn.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

Khi nhàn rỗi gửi ping mỗi 10 giây; khoảng 30 giây không có tin nhắn sẽ kết nối lại, chờ tăng theo hàm mũ 1–30 giây và ký mới. snapshot khôi phục trạng thái hiện tại, không phát lại lịch sử.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Tích hợp Socket API

Ứng dụng kết nối PAM Agent cục bộ mà không cần SDK JumpServer hoặc lưu AK/SK trong tiến trình nghiệp vụ. Kết nối dùng Unix Domain Socket, không phải TCP. Tài liệu giao thức đầy đủ bằng tiếng Anh ở bên dưới.

## 1. Unix Socket / HTTP / WebSocket

The transport is a Unix Domain Socket (AF_UNIX), not TCP. Connect to its filesystem path; no IP address or port is opened. HTTP/JSON requests and the WebSocket event channel share this socket. The localhost URL supplies the HTTP Host only. Agent-to-Core traffic still uses HTTPS/WSS.

## 2. Agent / services

Add a socket service to the Agent configuration. Use the same account declarations and allow_account_switch rules as file delivery. The declared A ID remains the map key while account_id identifies the active A/B account. Socket services cannot also configure update_config, reload_service, credential_check or app_check. restart_supported is an explicit opt-in for business restart commands.

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

Connect, read the initial snapshot, retrieve the delivery credentials, verify new real connections, replace business connections and release old sessions, then submit success. Receiving a notification or fetching a password never proves that the business has switched. A failed callback submits failed and is retried after the Agent backoff.

Every connection starts with the latest snapshot; this is current-state reconciliation, not historical replay. delivery contains metadata only, including delivery_id, declared-account keys and exact account/policy revisions. Revocations are delivered even during a Core outage; revoked_account_ids requires closing old sessions. With no complete credentials, a delivery may be absent while refresh is pending. Each independently applied business instance needs its own service_id. Only one WebSocket connection per service is accepted.

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.refresh.requested",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution.

## 4. Commands / status

Commands are claimed by the Agent before delegation. Restart commands appear in commands only for services with restart_supported=true. Report command.result after restart and readiness validation; success or failure is persisted and Core reporting is retried without executing the command again. A/B switch commands reuse credential delivery and its business result. Expired commands cannot be acknowledged.

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

Business status is separate from credential confirmation. Use starting, running, degraded, stopping, stopped or error and a short non-secret error_code. Account usage is updated only after business success, and the Agent aggregates all service reports. GET /v1/usage returns the aggregate. The existing POST /v1/usage replaces the instance-wide explicit account declaration; it does not acknowledge events.

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

Truy vấn CLI/Socket và đọc thông tin xác thực của dịch vụ trả về `account`, `source`, `last_pull_at`. `account.version` là phiên bản thông tin xác thực thực tế của tài khoản; kết quả thông thường bỏ phiên bản chính sách, `account_revision` và `event_id`. `source` là `api` khi lấy trực tiếp từ Core, hoặc `local` khi dùng bộ nhớ đệm. `last_pull_at` là thời điểm RFC 3339 lấy thành công tài khoản đó từ Core gần nhất, kể cả khi phiên bản không đổi. Thời điểm được lưu cùng bộ nhớ đệm qua lần khởi động lại. Đồng bộ siêu dữ liệu, đọc bộ nhớ đệm và lỗi không cập nhật thời điểm. Bộ nhớ đệm cũ không rõ thời điểm trả null. Truy vấn theo nhu cầu không tự bật lưu thông tin xác thực ngoại tuyến.

`event_id` vẫn nằm trong siêu dữ liệu thông báo và lớp ngoài thông báo cho script để báo cáo kết quả. Snapshot dùng lại sự kiện mới nhất khớp tài khoản, phiên bản, chính sách và máy khách, hoặc tạo sự kiện phát lại. Script đọc `credentials[selector].account`. Mẫu Go, ánh xạ trường và biến EnvironmentFile giữ tên cũ. Trình đọc JSON hiện có cần chuyển sang cấu trúc account lồng nhau.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  },
  "source": "local",
  "last_pull_at": "2026-10-30T06:30:00Z"
}
```

Tệp JSON thông tin xác thực chỉ chứa `account`, không có `source` hoặc `last_pull_at`. Thời điểm lấy được xuất theo ID tài khoản thực tế vào `last_pull_at.json` cùng cấp thư mục `store.root`, đồng thời được giữ trong trạng thái riêng tư. Truy vấn trực tiếp thành công cập nhật tệp chẩn đoán ngay cả khi phiên bản không đổi, không ghi lại tệp thông tin xác thực. Kiểm tra phiên bản, đọc bộ nhớ đệm và lỗi không đổi thời điểm. Tài khoản bị thu hồi được xóa khỏi tệp chẩn đoán.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  }
}
```

`last_pull_at.json`:

```json
{
  "<active-account-id>": "2026-10-30T06:30:00Z"
}
```

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix socket ownership is the access boundary. service_id identifies a business consumer, not a separate security identity: processes able to open the same socket share the application authorization. Keep unrelated trust domains on separate Agent instances/sockets. Events omit passwords; retrieve credentials explicitly and never log them. Polling the service snapshot and posting results is supported when a language has no Unix-socket WebSocket client.

## 6. Python

The Python example uses websockets, not a JumpServer SDK. By default it only observes metadata and never acknowledges business success. Implement the Business callbacks and call run(socket_path, service_id, business) for real processing. On a new business process, initialize connections from the current delivery even when it was applied by the previous process. Persist business-side delivery IDs when crash-safe de-duplication is required.

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
