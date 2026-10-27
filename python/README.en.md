# JumpServer PAM Python SDK and Go Agent

For a runnable application that reads/writes PostgreSQL and Redis and verifies SDK / Agent credential switches, see the [database application demo](database_app/README.md).

Applications can connect to JumpServer directly with the Python SDK or run the Go Agent on the application host. The Agent runs in the foreground on Linux, macOS and Windows; its built-in systemd installer is Linux-only. Clients fetch rotation credentials at startup and keep a signed Credential Event Stream WebSocket open for subscription-account snapshots and later updates. Credential updates, newer reconnect snapshots and manual account switch requests trigger a fetch.

## Python interface conventions

Methods, parameters, and response attributes use `snake_case`; classes use `CapWords`. Configure the client with keyword arguments, pass operation arguments directly, and use typed responses. A context manager closes the HTTP session:

First prepare `jms_pam_config.py` as described in the Python SDK integration section below. It is your application's identity configuration, not a module shipped in `jms-pam`; installing the package alone does not make it importable.

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

`get_account` returns the account directly, including the secret, asset and cache provenance.

Implement the account-change callback below, then start the managed listener. The SDK handles snapshots, fresh account lookup, account/version validation, retry, command claims and event results. Return only after validating the real connection, switching the application pool and releasing old connections. Python and Node.js use subclass hooks, Go uses OnAccountChanged, and Java uses AccountEventListener. The placeholder fails until business switching is implemented.

The original request-object API under `jms_pam.credential.v1` remains available for compatibility and emits `DeprecationWarning`. New integrations use the `Client` API shown here. Regenerate SDK access configuration when migrating to it.

## Manually start a policy cycle

Use **Start new cycle** in the policy's Basic settings or Event reception. The new cycle's timeline opens automatically:

- Credential subscriptions publish `credential.updated` again for currently authorized accounts, sharing a new `operation_id`. Passwords and revisions stay unchanged, and no secret-change start, success or failure events are generated. SDK clients fetch using the event account ID. The Agent also refetches unchanged revisions without repeating delivery or restarting services. Receipts indicate notification reception. Offline clients obtain the current snapshot when reconnecting.
- Account rotation starts a new cycle after the previous cycle has completed or been cancelled. JumpServer verifies the backup account, then publishes the switch. Clients apply and confirm the backup. Applications that recently used the original account through the legacy secret API must also fetch the backup after publication. The original account must have no successful JumpServer secret fetches for the configured period (7 days by default) after publication before its secret can be changed. Another source-account fetch restarts the window. Verification, switching, observation, and secret change share one cycle.

Administrator API: `POST /api/v1/accounts/application-credentials/<policy-id>/start-cycle/` requires policy change permission and, for account rotation, account verification permission. It returns `credential` and `cycle_id`. Disabled policies and unfinished rotations cannot start another cycle. Subscription accounts must finish any running secret changes before republishing.

## Manually send application events

Use **More > Send event** in the application list or **Event processing > Send event** in application details. Select the event, connection instances and deadline.

- `credential.switch.requested` asks an application to apply the currently published account and version. It does not change the policy's active account; use the rotation workflow for that. Fetch the credential, verify the requested account and revision, apply it, call `confirm_event(event_id=...)`, then report success.
- `application.restart.requested` invokes an SDK application's own restart handler and health check. The Agent only restarts the systemd service configured for EnvironmentFile delivery with the restart action, then checks that it is active.

The WebSocket receipt means received, not executed. `execute_application_command(event, handler)` claims the request before calling the handler. Only an `accepted: true` claim runs it; duplicate delivery never repeats the handler. A normal return reports success; an exception reports failure. Implement application-level idempotency and health checks in the handler. Agent switch requests succeed only after the application confirms the successfully applied event.

Offline instances receive requests on reconnect before their deadline. API applications can instead poll with AK/SK signatures and a stable `instance_id`; the first poll registers an instance that administrators can subsequently target:

1. `GET /api/v1/accounts/credential-client/commands/?instance_id=<instance-id>` returns outstanding requests (`list_application_commands()` in the SDK). Use the credential client's signed headers and protocol version 1.
2. `POST /api/v1/accounts/credential-client/command-result/` with `instance_id`, `command_id` and `status: running`. Execute only when the response says `accepted: true`.
3. Report `status: success` or `status: failed` to the same endpoint, optionally including a non-sensitive `error_code` (`report_application_command_result()` in the SDK).

Expired requests cannot be claimed. If a restart terminates the reporting process, check the application's state before an administrator sends another request. The existing Secret API remains unchanged and does not itself receive manual events.

## Choose an integration

| Method | Use when | Application responsibility |
| --- | --- | --- |
| Python SDK | The application can change Python code and reach JumpServer directly | Listen for events, fetch changed credentials, switch connections, and report event application results |
| Go Agent | The application should not store JumpServer keys, or needs file, EnvironmentFile, or local Socket delivery | Load and validate Agent-delivered credentials, then confirm the event after applying the account |

<!-- agent-doc:start -->

# Go Agent integration

Run the standalone Go Agent on the application host; Python is not required. Follow the sections below to install it, select delivery, configure business actions and verify the result.

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve (pull). Use account IDs, not usernames or policy IDs.
2. Bind a credential notification or A/B rotation policy only when updates must be pushed. Authorization alone does not enable proactive delivery.
3. Download identity/configuration from the application access wizard. Use a stable, unique `instance_id` for each replica and keep AK/SK private.

## 2. Install and start

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

Run in the directory containing the downloaded `jms_pam_agent.json`. The Linux service commands below require root and systemd. Change the instance ID for each replica. For foreground mode use the corresponding host binary; macOS/Windows binaries may require a source build.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id orders-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Foreground mode

The built-in installer requires Linux and root. For macOS, non-root Linux or Windows, select JSON or Socket delivery in the access wizard and follow its init-local and run --local --config commands. Initialize once and reuse the generated private local configuration; foreground mode performs no systemd actions.

Save the foreground binary as `jms-pam-agent` (`jms-pam-agent.exe` on Windows); on Linux/macOS give it execute permission with `chmod 0755 ./jms-pam-agent`.

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

## 3. Delivery methods

Choose JSON for files, EnvironmentFile for a pinned systemd service, or Unix Socket for local API access. The Agent records delivered revisions after delivery succeeds; the application validates and applies credentials before recording an applied revision. The socket belongs to the configured application user with mode 0600; make local requests as that user.

| Delivery methods | Configure and run |
| --- | --- |
| JSON | `delivery.delivery_mode: json` → `/opt/jumpserver-pam/credentials/<credential-key>.json` |
| EnvironmentFile (Linux / systemd) | `delivery.delivery_mode: environment` → `/opt/jumpserver-pam/credentials/<credential-key>.env` |
| Socket | `delivery.delivery_mode: socket` → `/run/jms-pam-agent/agent.sock` |

The Linux systemd configuration is `/etc/jms-pam-agent/agent.json` (0600). `state_file` stores retained credentials and progress; `event_file` stores event metadata; `delivery_root` and `socket_path` select local output locations. Foreground initialization generates its own private paths: use those from the generated `agent.json`.

## 4. Update business configuration and A/B rotation

The downloaded bootstrap contains Agent identity and delivery settings. Edit rules to declare the account IDs used by the business, update its configuration, activate the change and verify the running connection. An account selector with allow_account_switch also follows A/B rotation in either direction. Optional credential_check validates the new login before file changes. The Agent supplies default state, event and socket paths and a 300-second reconciliation interval. Empty rules write one default file per delivered credential.

The first JSON block shows the complete base configuration. To enable business file updates, replace its empty `rules` array with the second block. Paths, service names and executable scripts are examples: prepare them on the application host before enabling the rule. Omit `allow_account_switch` for ordinary account updates; A/B rotation requires it and a real application check.

| Configure and run | Update business configuration and A/B rotation |
| --- | --- |
| `credential_check` | Optional: validate the new login before changing files. |
| `config_update` | Update the existing business file using field mappings, a template or a script. |
| `service_action` | Reload/restart the configured service or run a fixed script. |
| `application_check` | Verify the running business connection; only then confirm application success. |

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

### Configuration format

`config_update.format` defaults to `auto`; omitting it has the same effect. Detection uses the file extension: `.txt`, `.env` and `.properties` use `env` (`KEY=value`); `.yml` and `.yaml` use `yaml` (top-level `KEY: value`, written as quoted strings). For other extensions, explicitly choose `env` or `yaml`. Mapped fields must already exist exactly once; unrelated settings are preserved. INI sections and nested YAML require `files` + `template_file` to generate the whole file, or a custom `script`. Environment-file updates take effect after the application rereads the file or restarts.

Local rules configure target files, JSON/EnvironmentFile rendering or trusted templates, and an optional systemd reload/restart or fixed executable. Scripts receive credential JSON on stdin, use fixed arguments, have a bounded timeout and must validate their application before returning success. Core cannot add script paths or expand local capabilities. Restart the Agent after editing its private configuration.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. Local API and confirmation

### Query accounts and credentials

Run CLI and Socket requests as the configured application user. `get_accounts` lists authorized account metadata; `get_secret` retrieves a secret on demand, including pull-only accounts. It writes the secret to stdout, so use it only in a private terminal. Replace the Socket path in foreground mode.

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

### Confirm application success

Validate the running connection and switch the application before confirming an event. Use either a successful `application_check` with `confirm_on_success: true`, or explicit confirmation after business verification. Do not call the confirmation API from a synchronous check script. Local results are persisted: `confirmed` means Core accepted the result; `pending` is retried.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Verify the integration

1. Verify that the instance is online and the expected accounts are authorized.
2. For push, send an update from the bound policy and verify the delivered file or Socket value and version.
3. For A/B rotation, verify that the running application uses the new account and has released old connections; then check the event result. Online, fetched and delivered states alone do not prove application success.

### Troubleshooting

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

The Agent reconciles on startup, on relevant events and every 300 seconds. Network failures retain the latest authorized credentials. Identity or authorization rejection blocks socket retrieval; successful signed synchronization restores it. Previously written files remain. SIGINT/SIGTERM closes the server, connections and reader thread.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK integration

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve (pull). Use account IDs, not usernames or policy IDs.
2. Bind a credential notification or A/B rotation policy only when updates must be pushed. Authorization alone does not enable proactive delivery.
3. Download identity/configuration from the application access wizard. Use a stable, unique `instance_id` for each replica and keep AK/SK private.

Use only the retrieval section for pull-only access. For pushed updates or A/B rotation, implement the managed business callbacks and start listening. The SDK handles protocol and event results; the application validates and switches real connections. These examples reflect the current source: install a matching Core and SDK build before using new APIs; released 1.0.2 packages do not include all of them.

## 2. Installation and identity

Requires Python 3.9+. Use the application Python or virtual environment. Install the client source shipped by the same Core using the commands below; released v1.0.2 packages do not include managed callbacks. Replace jumpserver.example.com with your Core address. Keep the extracted jms-pam-clients directory with your application, including the Go module replacement path.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Local source installation (alternative)

If this Core does not provide the source download endpoint, or for offline deployment, obtain its matching client source at `/path/to/pam-clients` and run the following from the application directory. Replace the path with your actual checkout.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` is an application configuration file, not part of the `jms-pam` PyPI package. Download it from the application access wizard and save it next to your application script, or create it with the content below and replace every placeholder with the actual application identity. Do not commit or log `client_options`. Use a stable, unique `instance_id` per replica.

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

## 3. Fetch the first account

For an interactive test, start Python from the directory containing `jms_pam_config.py`, then run the example below. For a script, save the example as `app.py` alongside the configuration and run `python3 app.py` instead. If `ModuleNotFoundError: No module named 'jms_pam_config'` occurs, check the file name and location; reinstalling `jms-pam` will not create this file.

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

## 4. Listen for updates and switch connections

Implement the account-change callback below, then start the managed listener. The SDK handles snapshots, fresh account lookup, account/version validation, retry, command claims and event results. Return only after validating the real connection, switching the application pool and releasing old connections. Python and Node.js use subclass hooks, Go uses OnAccountChanged, and Java uses AccountEventListener. The placeholder fails until business switching is implemented.

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

The account callback runs serially behind a bounded event queue. Failed account updates retry with 1–30 second backoff. Success is reported only after the business callback returns; a failed result upload retries the upload without repeating a completed action in the same listener. Commands execute only after an accepted claim; failed command actions are not automatically repeated. Applied accounts are reported as a complete usage snapshot; A/B switches replace the current account. Implement revoked-account cleanup for scope removals. Usage reporting, fetches and file delivery never themselves confirm application success. Low-level streams and legacy credential hooks retain explicit confirmation. Business handlers must remain idempotent across restarts.

### Lifecycle and shutdown

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events waits until stopped. start_events returns before initial synchronization is complete; wait for application readiness before serving requests. One managed listener is allowed per client. stop_events and close wait for an active hook; a hook may stop its own client. clone rebuilds subclass state; override it for additional constructor dependencies.

### Latest credentials and backend outages

Credential getters always request the API first. A successful fetch replaces the retained latest credential; older revisions never overwrite a newer one. The retained value has no time expiry. Only a timeout, network failure or HTTP 5xx may return this value for the same selector, marked as coming from local state. Without a previously fetched value, the original error is raised. SDK values stay in the current client’s memory until replacement, revocation or close; clones and process restarts start empty. The Agent retains its latest credentials in its existing protected local state. HTTP 401/403/404 clear SDK retained values and raise; malformed successful responses also raise. Explicit revocations remove affected credentials, and snapshots remove push entries outside the subscribed scope. A configuration notification retains existing values until the following snapshot reconciles scope. The Agent applies explicit revocation and reduced snapshot scope before HTTP synchronization, persists the reduced scope, and blocks affected local reads even during a backend outage or after restart. A credential_not_found response (HTTP 400) also clears retained SDK values. HTTP 426 fails explicitly without clearing retained values or returning cache for that request. A missing optional metadata endpoint (HTTP 404) does not clear credentials. Agent pull-only values are not persisted for offline use; SDK getters may use retained memory values only under the existing temporary-failure rules.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

With managed event listening enabled, snapshot and credential.updated automatically fetch the current credential, replace the retained value and then invoke the business hook. A failed refresh leaves the previous value in place and retries. The Agent also refetches on update notifications and retains its previous credentials during backend outages. Refresh and manual switching use the live-only calls below; a retained password must not be treated as a newly fetched revision or automatically confirmed.

Event connections send application ping messages at 10-second intervals while idle and reconnect after approximately 30 seconds without messages. Reconnect uses 1–30 second exponential backoff and fresh signatures. Reconnect snapshots restore current state; past events are not replayed.



## 5. Common methods

### List authorized accounts

Use the account listing methods to discover the application’s current authorized pull scope. Listings return account and asset metadata plus related push policies, without retrieving passwords or recording credential fetches. Accounts without policies are included. The full-list method follows all pages; the page method returns `count` and `accounts` (Go returns the slice and total separately). Defaults are limit 200 and offset 0; limit is 1–500 and search is at most 128 characters, matching account name, username, asset name or address. Duplicate accounts, incomplete results and changing totals raise an error; retry the whole listing if authorization changes. Use an account ID with the account-fetch method when its secret is needed. Examples require SDK source containing these new methods; published version 1.0.2 does not yet provide them in Python, Java or Node.js.

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


### Application, replica and policy information

Read the authenticated application ID, name, organization and authorized account/policy counts with get_application; get_client_info returns only the current replica’s registration, type and versions. Policy listing returns the entire visible bound list, including disabled policies; search matches policy names or keys. Fetch a policy using exactly one of its ID or base policy key, rather than an account push key. Rotation policies require authorization for both accounts and the active account. Subscription counts include only accounts currently authorized to this application. These calls return metadata without secrets and do not fetch credentials or confirm application changes. As with account queries, the replica is registered on first access. They require updated Core and SDK source; the published 1.0.2 does not yet contain these methods.

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


### Application status reporting

Report a complete snapshot of the application state and the account IDs currently in use. States: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. An empty account list clears previous usage. Call on state changes or from your own heartbeat; no background reporting is started. Python, Java and Node.js return false on network/API failures, including an older Core without this endpoint; Go provides `TryReportStatus` for the same behavior. Strict variants expose failure details. Invalid arguments are rejected. Requests use the configured HTTP timeout; keep reporting off latency-sensitive paths. Reporting never fetches secrets, confirms rotation or invalidates retained credentials. Use machine-readable `error_code` only, without secrets or exception messages. `get_client_info` / `GetClientInfo` / `getClientInfo` return the last reported snapshot and its timestamp.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Low-level events and commands

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

Use list_application_commands to poll pending requests and execute_application_command(event, handler) to claim execution. Only an accepted claim runs the handler. A switch handler verifies the requested revision and account, applies the credential and confirms it; a restart handler restarts the application and checks health. Report success only after the work completes. Failed result reporting preserves the original handler exception.

`get_credential` and key-based confirmation remain available for older integrations. New integrations use `get_account` and `confirm_event`. The event-result workflow requires the corresponding Core update; SDK 1.0.2 alone cannot add this server capability.

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

## 6. Verify the integration

1. Replace the account placeholder with an authorized account ID and verify a real connection without logging the password.
2. If listening for updates, verify initial synchronization and a policy update; complete the business callback before expecting a successful event result.
3. Check the instance usage/status and event result separately. A fetch or usage report does not confirm credential application.

### Troubleshooting

The client closes HTTP sessions and event streams with with or close. clone creates an independent session. HTTP, network, authentication and decoding failures raise PAMError with code, status_code, detail and original_error. Retry transient errors at the application boundary; handle authorization denial explicitly and keep credentials out of logs.

All SDKs use protocol version 1; Agent configuration uses schema version 1. A client_upgrade_required response (HTTP 426) requires checking compatibility and upgrading. Unknown optional fields and notification events are tolerated; unsupported policy modes must not be applied or confirmed. Received event receipts are sent automatically and do not prove credential application. Client metadata may include server_capabilities. Missing or null means unknown; unknown names are tolerated. Capabilities describe server features, not authorization.

<!-- sdk-doc:end -->
