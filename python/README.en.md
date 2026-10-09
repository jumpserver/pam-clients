# JumpServer PAM Python SDK and Go Agent

Applications can connect to JumpServer directly with the Python SDK or run the Go Agent on the application host. The Agent runs in the foreground on Linux, macOS and Windows; its built-in systemd installer is Linux-only. Clients fetch rotation credentials at startup and keep a signed Credential Event Stream WebSocket open for subscription-account snapshots and later updates. Credential updates, newer reconnect snapshots and manual account switch requests trigger a fetch.

## Python interface conventions

Methods, parameters, and response attributes use `snake_case`; classes use `CapWords`. Configure the client with keyword arguments, pass operation arguments directly, and use typed responses. A context manager closes the HTTP session:

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

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

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

## Go Agent integration

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

Download the Agent for your application host CPU architecture from this JumpServer: In Application Management, open the application access wizard, select Agent, and download the configuration. Follow the deployment commands generated by the wizard using a stable, unique instance ID. For Linux systemd installation, use a host with systemd and administrator privileges. After startup, verify that the instance is online in the wizard.

For macOS, non-root Linux or Windows foreground use, select JSON or Socket delivery in the wizard and follow its `init-local` and `run --local --config` commands. Initialize only once, then reuse the generated local configuration on restart. Foreground mode uses the current user and does not perform systemd actions; Windows files use private ACLs instead of POSIX mode bits.

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

Choose JSON for files, EnvironmentFile for a pinned systemd service, or Unix Socket for local API access. The Agent records delivered revisions after delivery succeeds; the application validates and applies credentials before recording an applied revision. The socket belongs to the configured application user with mode 0600; make local requests as that user.

Local rules configure target files, JSON/EnvironmentFile rendering or trusted templates, and an optional systemd reload/restart or fixed executable. Scripts receive credential JSON on stdin, use fixed arguments, have a bounded timeout and must validate their application before returning success. Core cannot add script paths or expand local capabilities. Restart the Agent after editing its private configuration.

The downloaded bootstrap contains Agent identity and delivery settings. Edit rules to declare the account IDs used by the business, update its configuration, activate the change and verify the running connection. An account selector with allow_account_switch also follows A/B rotation in either direction. Optional credential_check validates the new login before file changes. The Agent supplies default state, event and socket paths and a 300-second reconciliation interval. Empty rules write one default file per delivered credential.

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

### Local API and confirmation

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

For alternating rotation, validate a real connection, switch the application connection pool and release old connections before reporting success using the event ID. A failed connection check must prevent confirmation.

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

Use the event ID to report application results. A local confirmation is persisted first; status confirmed means Core accepted it, while pending means it will be retried. Do not confirm merely because a file was written or a service restarted.

### Troubleshooting

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

The Agent reconciles on startup, on relevant events and every 300 seconds. Network failures retain the latest authorized credentials. Identity or authorization rejection blocks socket retrieval; successful signed synchronization restores it. Previously written files remain. SIGINT/SIGTERM closes the server, connections and reader thread.

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Python SDK integration

Requires Python 3.9+. Install jms-pam from PyPI or your internal mirror using the application Python or virtual environment, then download the application identity configuration from the access wizard.

```bash
python3 -m pip install jms-pam
```

Place jms_pam_config.py next to the application. Its client_options contain application identity material: do not commit or log it. Use one stable, unique instance_id per replica.

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

### Events and credential application

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

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

### Common methods

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

`get_credential` and key-based confirmation remain available for older integrations. New integrations use `get_account` and `confirm_event`. The event-result workflow requires the corresponding Core update; SDK 1.0.2 alone cannot add this server capability.


<!-- sdk-doc:end -->
