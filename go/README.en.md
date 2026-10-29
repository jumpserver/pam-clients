# JumpServer PAM Go SDK

<!-- sdk-doc:start -->

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Use only the retrieval section for pull-only access. For pushed updates or A/B rotation, implement the managed business callbacks and start listening. The SDK handles protocol and event results; the application validates and switches real connections. These examples reflect the current source: install a matching Core and SDK build before using new APIs; released 1.0.2 packages do not include all of them.

- Go 1.23+ / coder/websocket

## 2. Installation and identity

Install the SDK and configure the identity below. Authorize accounts in the application for on-demand access. Enable Account credential change notifications in the application to receive changes for all authorized accounts; there is no separate account subscription policy. Bind an A/B rotation policy only when account switching is required. Each replica must use a stable, unique instance ID.

Install the client source shipped by the same Core using the commands below; released v1.0.2 packages do not include managed callbacks. Replace jumpserver.example.com with your Core address. Keep the extracted jms-pam-clients directory with your application, including the Go module replacement path.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/go/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
go mod edit -replace github.com/jumpserver/pam-clients/go=./jms-pam-clients/go &&
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```

### Local source installation (alternative)

If this Core does not provide the source download endpoint, or for offline deployment, obtain its matching client source at `/path/to/pam-clients` and run the following from the application directory. Replace the path with your actual checkout.

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

## 3. Fetch the first account

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

## 4. Listen for updates and switch connections

Implement the account-change callback below, then start the managed listener. The SDK handles snapshots, fresh account lookup, account/version validation, retry, command claims and event results. Return only after validating the real connection, switching the application pool and releasing old connections. Python and Node.js use subclass hooks, Go uses OnAccountChanged, and Java uses AccountEventListener. The placeholder fails until business switching is implemented.

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

The account callback runs serially behind a bounded event queue. Failed account updates retry with 1–30 second backoff. Success is reported only after the business callback returns; a failed result upload retries the upload without repeating a completed action in the same listener. Commands execute only after an accepted claim; failed command actions are not automatically repeated. Applied accounts are reported as a complete usage snapshot; A/B switches replace the current account. Implement revoked-account cleanup for scope removals. Usage reporting, fetches and file delivery never themselves confirm application success. Low-level streams and legacy credential hooks retain explicit confirmation. Business handlers must remain idempotent across restarts.

### Lifecycle and shutdown

`WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)`; `Stop()` / `Wait()`; `context.CancelFunc`

WatchEvents waits in the calling goroutine. StartEvents returns before initial synchronization is complete. One managed listener is allowed per client. Stop and Client.Close request cancellation; call Wait externally to join handlers. Do not call Wait from a handler, and make long operations honor the context.

### Latest credentials and backend outages

SDK credential getters request the API first. A successful fetch replaces the retained value without allowing revisions to move backwards. Only a timeout, network error or HTTP 5xx permits fallback to a previously fetched value for the same selector, marked as local; retained values have no time expiry. SDK caches are client-local memory: clones and restarts start empty. HTTP 401/403/404 or credential_not_found (HTTP 400) clear retained credentials and raise. HTTP 426 and malformed successful responses raise without using fallback; a missing optional metadata endpoint does not clear credentials. PAM Agent persists accounts declared by store/services in protected state; an undeclared on-demand query is not automatically retained. Explicit revocations take effect before HTTP refresh, including while offline. Push snapshots do not define the full authorization scope of stored accounts; successful reconciliation checks the current application authorization.

- `account.FromLocal`
- `GetAccountFresh(ctx, accountID)`

With managed event listening enabled, snapshot and credential.updated automatically fetch the current credential, replace the retained value and then invoke the business hook. A failed refresh leaves the previous value in place and retries. The PAM Agent also refetches on update notifications and retains its previous credentials during backend outages. Refresh and manual switching use the live-only calls below; a retained password must not be treated as a newly fetched revision or automatically confirmed.

Event connections send application ping messages at 10-second intervals while idle and reconnect after approximately 30 seconds without messages. Reconnect uses 1–30 second exponential backoff and fresh signatures. Reconnect snapshots restore current state; past events are not replayed.



## 5. Common methods

### List authorized accounts

Use the account listing methods to discover the application’s current authorized pull scope. Listings return account and asset metadata plus related push policies, without retrieving passwords or recording credential fetches. Accounts without policies are included. The full-list method follows all pages; the page method returns `count` and `accounts` (Go returns the slice and total separately). Defaults are limit 200 and offset 0; limit is 1–500 and search is at most 128 characters, matching account name, username, asset name or address. Duplicate accounts, incomplete results and changing totals raise an error; retry the whole listing if authorization changes. Use an account ID with the account-fetch method when its secret is needed. Examples require SDK source containing these new methods; published version 1.0.2 does not yet provide them in Python, Java or Node.js.

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


### Application, replica and policy information

Read the authenticated application ID, name, organization and authorized account/policy counts with GetApplication; GetClientInfo returns only the current replica’s registration, type and versions. Policy listing returns the entire visible bound list, including disabled policies; search matches policy names or keys. Fetch a policy using exactly one of its ID or base policy key, rather than an account push key. Rotation policies require authorization for both accounts and the active account. Subscription counts include only accounts currently authorized to this application. These calls return metadata without secrets and do not fetch credentials or confirm application changes. As with account queries, the replica is registered on first access. They require updated Core and SDK source; the published 1.0.2 does not yet contain these methods.

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


### Application status reporting

Report a complete snapshot of the application state and the account IDs currently in use. States: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. An empty account list clears previous usage. Call on state changes or from your own heartbeat; no background reporting is started. Python, Java and Node.js return false on network/API failures, including an older Core without this endpoint; Go provides `TryReportStatus` for the same behavior. Strict variants expose failure details. Invalid arguments are rejected. Requests use the configured HTTP timeout; keep reporting off latency-sensitive paths. Reporting never fetches secrets, confirms rotation or invalidates retained credentials. Use machine-readable `error_code` only, without secrets or exception messages. `get_client_info` / `GetClientInfo` / `getClientInfo` return the last reported snapshot and its timestamp.

```go
err := client.ReportAccounts(ctx, []string{account.ID})
// ReportAccounts(ctx, nil) clears the complete explicit usage snapshot.
// ReportStatus(ctx, report) also reports application health.
```


### Low-level events and commands

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

`get_credential` and key-based confirmation remain available for older integrations. New integrations use `get_account` and `confirm_event`. The event-result workflow requires the corresponding Core update; SDK 1.0.2 alone cannot add this server capability.

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

## 6. Verify the integration

1. Replace the account placeholder with an authorized account ID and verify a real connection without logging the password.
2. If listening for updates, verify initial synchronization and a policy update; complete the business callback before expecting a successful event result.
3. Check the instance usage/status and event result separately. A fetch or usage report does not confirm credential application.

### Troubleshooting

HTTP, network, authentication and response decoding failures use the SDK exception/error type with code and HTTP status. Close streams and clients when finished. Clones have independent lifecycles. Retry transient failures at the application boundary and never log secrets or authentication headers.

`PAMError`

All SDKs use protocol version 1; PAM Agent configuration uses schema version 1. A client_upgrade_required response (HTTP 426) requires checking compatibility and upgrading. Unknown optional fields and notification events are tolerated; unsupported policy modes must not be applied or confirmed. Received event receipts are sent automatically and do not prove credential application. Client metadata may include server_capabilities. Missing or null means unknown; unknown names are tolerated. Capabilities describe server features, not authorization.

<!-- sdk-doc:end -->
