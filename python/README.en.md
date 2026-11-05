# JumpServer PAM Python SDK and PAM Agent

For a runnable application that reads/writes PostgreSQL and Redis and verifies SDK / PAM Agent credential switches, see the [database application demo](database_app/README.md).

Applications can connect to JumpServer directly with the Python SDK or run the PAM Agent on the application host. The PAM Agent runs in the foreground on Linux, macOS and Windows; its built-in systemd installer is Linux-only. Application authorization controls on-demand access. The application-level Account credential change notifications switch covers all authorized accounts; A/B switching uses a separately bound rotation policy. Clients receive snapshots and subsequent events over a signed WebSocket. `credential.secret.updated` signals a secret-content update, `credential.switch.published` publishes a target account switch, and `credential.refresh.requested` requests application of the current credentials. These three events, snapshots and administrator switch commands trigger credential fetching and application.

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


See the policy’s operation guide for its cycle and recovery. Administrator commands and shared protocols are documented in Events and commands below.

## Choose an integration

| Method | Use when | Application responsibility |
| --- | --- | --- |
| Python SDK | The application can change Python code and reach JumpServer directly | Listen for events, fetch changed credentials, switch connections, and report event application results |
| PAM Agent | The application should not store JumpServer keys, or needs file, EnvironmentFile, or local Socket delivery | Load and validate Agent-delivered credentials, then confirm the event after applying the account |

<!-- agent-doc:start -->

# JumpServer PAM Agent integration

Run the standalone PAM Agent on the application host; Python is not required. Follow the sections below to install it, select delivery, configure business actions and verify the result.

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Download the identity configuration from the application access wizard and protect the AK/SK. `instance_id` is optional.

## 2. Install and start

### Linux / systemd

[Linux x86_64 (amd64)](/download/pam/pam-agent-linux-amd64) · [Linux ARM64](/download/pam/pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# Run in the directory containing the downloaded PAM Agent and SHA256SUMS.
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./pam-agent-linux-${agent_arch}" /usr/local/bin/pam-agent
```

Run in the directory containing the downloaded `pam-agent.json`. The Linux service commands below require root and systemd. For foreground mode, use the executable for your operating system and architecture.

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### Foreground mode

For macOS, Windows or non-root Linux, initialize once with `init-local`, then reuse the generated private configuration with `run --local`. Standard JSON and environment files are supported; systemd actions require the Linux service.

Save the foreground binary as `pam-agent` (`pam-agent.exe` on Windows); on Linux/macOS give it execute permission with `chmod 0755 ./pam-agent`.

```bash
chmod 0600 ./pam-agent.json
./pam-agent init-local \
  --bootstrap ./pam-agent.json \
  --directory "$HOME/.jms-pam-agent/sample-app"
./pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\sample-app'
.\pam-agent.exe init-local --bootstrap (Join-Path $PWD 'pam-agent.json') --directory $agentDir
.\pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. Configuration example

The complete Linux/systemd example below includes application identity, polling, standard credential files, the local API and a business service. Replace the identity, account ID, operating-system user, paths, service name and check script with your actual settings; deploy the check script first. Set `services` to `[]` if you only need files or the local API. The following sections explain each block; all blocks belong to the same configuration file.

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  },
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
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
}
```

`instance_id` is optional. When omitted or empty, PAM Agent generates `<hostname>-<4 random hexadecimal characters>`, for example `orders-pod-a1b2`. An explicit `instance_id` or `--instance-id` takes precedence. Installation and `init-local` save the generated ID in their output configuration. Direct runs retain it in the private state file and reuse it after restart without rewriting the input configuration. K8s replicas can share configuration with this field omitted; each replica needs its own writable state directory. A new replica with no retained state generates a new ID.

`pull_interval` is the global polling interval in seconds, a positive integer with a default of 300. The PAM Agent synchronizes immediately at startup, then checks account and credential versions at this interval, fetching secrets only when versions or the active account change. Services with `update_on_notification: false` update on the next poll; notifications do not postpone it.

## 4. Account storage store

`store` delivers credentials as standard JSON or environment files for applications that read credentials from files. It controls which accounts are written, the output directory, format and file owner. Updating an existing business configuration or reloading a service belongs in `services`.

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "store": {
    "accounts": [
      "all"
    ],
    "root": "/opt/jumpserver-pam/credentials",
    "format": "json",
    "owner": "appuser"
  }
}
```

| Configure and run | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Only the explicitly declared account IDs. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

Each stored account has its own file named by its actual account ID, including separate A/B accounts. Files contain plaintext credentials with mode 0600. A/B standby accounts being changed are temporarily unavailable. Successful authorization reconciliation removes revoked files. An unchanged reconciliation reads metadata without fetching passwords again; actual fetches remain audited and participate in rotation access observation.

## 5. Local API socket

`socket` configures the local Unix Socket interface for health checks, account listing, on-demand credential retrieval, account usage reporting and application-result confirmation. Applications can query the socket without enabling `store`. Retrieval follows the application authorization in Core; `store.accounts` selects file output and does not restrict the API account scope.

`path` defaults to `/run/jms-pam-agent/agent.sock`. `owner` controls socket access and the default owner of business output files; if omitted, it uses `store.owner`, then the PAM Agent process user. Omitting `socket` uses these defaults; it does not disable the interface. `init-local` sets the socket to `<directory>/run/agent.sock` and the owner to the current user.

When running without root, omit both `store.owner` and `socket.owner` to use the current user. `init-local` sets both to the current user automatically. When running as root, specify the application user if it needs to read the files or access the socket. The owner setting does not switch the PAM Agent process user.

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### Query accounts and credentials

Run CLI and Socket requests as the configured application user. `get_accounts` lists authorized account metadata; `get_secret` retrieves a secret on demand, including pull-only accounts. It writes the secret to stdout, so use it only in a private terminal. Replace the Socket path in foreground mode.

CLI/Socket secret queries and service credential reads return `account`, `source` and `last_pull_at`. `account.version` is the actual account credential version; ordinary results omit policy revisions, `account_revision` and `event_id`. `source` is `api` for a live Core fetch and `local` for retained data. `last_pull_at` is the RFC 3339 time of the last successful Core credential fetch for that account, even when its version is unchanged. It is retained with cached credentials across restarts; metadata sync, cache reads and failed pulls do not advance it. Older caches without a known pull time trigger one real credential fetch on the next online sync, even if the version is unchanged. Failed fetches leave the time unknown (null) and are retried; after success, unchanged metadata syncs do not fetch again. Pull-only queries do not enable offline credential retention.

`event_id` stays in notification metadata and script notification envelopes for reporting application success/failure; it is not an account field or a global latest-event pointer. Snapshots reuse the latest matching account/version/policy/client event, or create a republished event if none exists. Script readers use `credentials[selector].account`; existing Go templates, field mappings and EnvironmentFile variables keep their existing field names. Existing JSON readers must adopt the nested account structure.

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

Credential JSON files contain only `account`, with no `source` or `last_pull_at`. Pull times are exported separately to `last_pull_at.json` beside the directory configured by `store.root`, keyed by the actual account ID. The private state retains the same times. A successful live credential fetch updates the diagnostic file immediately, even for the same version, without rewriting credential files. Periodic version checks, cache reads and failed pulls do not advance it. Revoked accounts are removed from the diagnostic file.

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

```bash
pam-agent get_accounts
pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credential?account_id=<account-id>'
```

### Confirm application success

Validate the running connection and switch the application before confirming an event. Use either a successful `app_check` with `confirm_on_success: true`, or explicit confirmation after business verification. Do not call the confirmation API from a synchronous check script. Local results are persisted: `confirmed` means Core accepted the result; `pending` is retried.

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Application updates service

Add the service objects below to `services`. Each service needs a unique stable `name`. `allow_account_switch` belongs to the service and defaults to false: when true, accounts with a unique A/B association follow the active account; other accounts remain fixed. Declaring A is sufficient. `update_on_notification` also belongs to the service and defaults to true. False defers business actions until scheduled reconciliation (300 seconds by default), while storage and authorization revocations still update. Each service tracks its own applied versions. Only successful application checks with `confirm_on_success: true` can automatically confirm a rotation, after every relevant service succeeds.

New configurations use `update_config` → `reload_service` → `app_check`: update the business file, reload/restart the service (or run a fixed script), then verify the running application. These names require an updated PAM Agent.

| Configure and run | Update business configuration and A/B rotation |
| --- | --- |
| `credential_check` | Optional: validate the new login before changing files. |
| `update_config` | Update the existing business file using field mappings, a template or a script. |
| `reload_service` | Reload/restart the configured service or run a fixed script. |
| `app_check` | Verify the running business connection; only then confirm application success. |

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

### accounts

In `services.accounts`, use `id` to select an account. One account may use `username` / `secret` directly. Multiple accounts require unique aliases and qualified references such as `database.username` and `cache.secret`. Alias references stay stable across A/B switches. Optional selector `username` is a display hint only; actual values come from Core. Resolve every required account before writing the file, then run the service action once. Missing or ambiguous mappings fail before mutation.

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

### update_config

Each object in `services` describes one business service. Choose exactly one `update_config` method: `file + fields_map` edits existing fields, `files + template_file` renders complete files, or `script` runs a custom update. Replace that service’s `update_config` with one of the fragments below.

#### file + fields_map

`update_config.format` defaults to `auto`; omitting it has the same effect. Detection uses the file extension: `.txt`, `.env` and `.properties` use `env` (`KEY=value`); `.yml` and `.yaml` use `yaml` (top-level `KEY: value`, written as quoted strings). For other extensions, explicitly choose `env` or `yaml`. Mapped fields must already exist exactly once; unrelated settings are preserved. INI sections and nested YAML require `files` + `template_file` to generate the whole file, or a custom `script`. Environment-file updates take effect after the application rereads the file or restarts.

```json
{
  "update_config": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "username",
      "DB_PASSWORD": "secret"
    }
  }
}
```

#### files + template_file

Use templates to generate complete nested JSON, INI or other configuration files. Set `files[].format` to `template` and `template_file` to an absolute local path. Rendering precedes atomic file replacement with mode 0600; omitted `owner` defaults to `socket.owner`. The `json` function quotes and escapes JSON values.

```json
{
  "update_config": {
    "files": [
      {
        "path": "/etc/sample-app/credentials.json",
        "format": "template",
        "template_file": "/etc/pam-agent/templates/credentials.json.tmpl",
        "owner": "appuser"
      }
    ]
  }
}
```

`/etc/pam-agent/templates/credentials.json.tmpl`:

```gotemplate
{
  "database": {
    "username": {{json (index .Credentials "<primary-account-id>").Username}},
    "password": {{json (index .Credentials "<primary-account-id>").Secret}}
  }
}
```

Template `.Credentials` and script `credentials` are keyed by the declared `services.accounts[].id`, not alias. Replace `<primary-account-id>` with the real ID. During A/B switching, the key stays the declared A ID while `AccountID` (`account_id` in scripts) identifies the active account.

#### script

Declare one output with `target`, or several with `targets`, so PAM Agent can back them up before mutation. Deploy `script.path` as a local executable. `args` holds fixed parameters; credentials arrive as JSON on stdin. `timeout_seconds` accepts 1–300 seconds, default 120. The script runs as the PAM Agent user. Exit 0 means success; nonzero exit or timeout fails and retries, so updates must be idempotent.

```json
{
  "update_config": {
    "target": "/etc/sample-app/credentials.json",
    "script": {
      "path": "/usr/local/libexec/pam-agent/update-credentials",
      "args": [
        "/etc/sample-app/credentials.json"
      ],
      "timeout_seconds": 30
    }
  }
}
```

Example stdin payload (excerpt):

```json
{
  "event": "credential.refresh.requested",
  "credentials": {
    "<primary-account-id>": {
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
      "last_pull_at": "2026-10-30T06:30:00Z",
      "event_id": "<event-id>"
    }
  }
}
```

This Linux/macOS Python 3 example updates database fields in an existing JSON file, preserves other settings and ownership, and atomically replaces it with mode 0600. Prepare the target directory and initial JSON file, deploy the executable script, and grant execute permission. Custom scripts manage their own output format, permissions and ownership.

`/usr/local/libexec/pam-agent/update-credentials`:

```python
#!/usr/bin/env python3
import json
import os
import sys
import tempfile
from pathlib import Path

payload = json.load(sys.stdin)
credential = payload["credentials"]["<primary-account-id>"]["account"]
target = Path(sys.argv[1])
original = target.stat()
with target.open(encoding="utf-8") as source:
    config = json.load(source)
config.setdefault("database", {}).update(
    username=credential["username"], password=credential["secret"]
)

fd, temporary = tempfile.mkstemp(prefix=".pam-agent-", dir=target.parent)
try:
    with os.fdopen(fd, "w", encoding="utf-8") as output:
        os.fchmod(output.fileno(), 0o600)
        os.fchown(output.fileno(), original.st_uid, original.st_gid)
        json.dump(config, output, ensure_ascii=False)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, target)
finally:
    if os.path.exists(temporary):
        os.unlink(temporary)
```

Template or script success only completes the configuration update. Configure `reload_service` and `app_check` in the same service when reload or business verification is needed. Validate the configuration before restarting PAM Agent:

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile writes the username, password and account metadata as plaintext `KEY=value` entries in a private `.env` file (0600). The business systemd service must explicitly reference this file with `EnvironmentFile=`. systemd loads it into that service process environment when starting the process; it does not set global host variables. Restart the service after an update to refresh its main process environment. Reload alone does not replace an already running process environment.

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

## 7. Verify the integration

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Troubleshooting

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

The PAM Agent reconciles at startup, on relevant notifications and at the configured interval (300 seconds by default). Temporary outages retain still-authorized credentials. Explicit revocation or application disable blocks local reads and removes managed store files, even if the next refresh fails. Business configuration files remain under the application administrator’s control. Notification traffic does not postpone periodic service updates. SIGINT/SIGTERM closes the server and event reader.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Python SDK integration

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
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

The account callback runs serially behind a bounded event queue. Failed account updates retry with 1–30 second backoff. Success is reported only after the business callback returns; a failed result upload retries the upload without repeating a completed action in the same listener. Commands execute only after an accepted claim; failed command actions are not automatically repeated. Applied accounts are reported as a complete usage snapshot; A/B switches replace the current account. Implement revoked-account cleanup for scope removals. Usage reporting, fetches and file delivery never themselves confirm application success. Business handlers must remain idempotent across restarts.

### Lifecycle and shutdown

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events waits until stopped. start_events returns before initial synchronization is complete; wait for application readiness before serving requests. One managed listener is allowed per client. stop_events and close wait for an active hook; a hook may stop its own client. clone rebuilds subclass state; override it for additional constructor dependencies.

### Latest credentials and backend outages

SDK credential getters request the API first. A successful fetch replaces the retained value without allowing revisions to move backwards. Only a timeout, network error or HTTP 5xx permits fallback to a previously fetched value for the same selector, marked as local; retained values have no time expiry. SDK caches are client-local memory: clones and restarts start empty. HTTP 401/403/404 or credential_not_found (HTTP 400) clear retained credentials and raise. HTTP 426 and malformed successful responses raise without using fallback; a missing optional metadata endpoint does not clear credentials. PAM Agent persists accounts declared by store/services in protected state; an undeclared on-demand query is not automatically retained. Explicit revocations take effect before HTTP refresh, including while offline. Push snapshots do not define the full authorization scope of stored accounts; successful reconciliation checks the current application authorization.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

With managed event listening enabled, snapshot and credential.secret.updated / credential.switch.published / credential.refresh.requested automatically fetch the current credential, replace the retained value and then invoke the business hook. A failed refresh leaves the previous value in place and retries. The PAM Agent also refetches on update notifications and retains its previous credentials during backend outages. Refresh and manual switching use the live-only calls below; a retained password must not be treated as a newly fetched revision or automatically confirmed.




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

Report a complete snapshot of the application state and the account IDs currently in use. States: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. An empty account list clears previous usage. Call on state changes or from your own heartbeat; no background reporting is started. Strict variants expose failure details. Invalid arguments are rejected. Requests use the configured HTTP timeout; keep reporting off latency-sensitive paths. Reporting never fetches secrets, confirms rotation or invalidates retained credentials. Use machine-readable `error_code` only, without secrets or exception messages. `get_client_info` / `GetClientInfo` / `getClientInfo` return the last reported snapshot and its timestamp.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


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
2. If listening for updates, verify initial synchronization and an authorized-account update or A/B switch; complete the business callback before expecting a successful event result.
3. Check the instance usage/status and event result separately. A fetch or usage report does not confirm credential application.

### Troubleshooting

The client closes HTTP sessions and event streams with with or close. clone creates an independent session. HTTP, network, authentication and decoding failures raise PAMError with code, status_code, detail and original_error. Retry transient errors at the application boundary; handle authorization denial explicitly and keep credentials out of logs.



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
| `rotation.source.waiting` | Observe successful secret fetches for the original account from switch publication onward, in parallel with application confirmation. Password change requires both conditions to be satisfied. |
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

Core sends snapshot on connection, reconnection and scope changes. Its credentials include all authorized accounts when application notifications are enabled, plus active authorized A/B rotation policies bound to the application. Process each item like credential.secret.updated / credential.switch.published / credential.refresh.requested using event_id, account_id and account_revision. The snapshot also includes authorized_account_ids; get_accounts lists authorized account metadata. Removing a rotation policy binding does not revoke account authorization.

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

The SDK automatically sends a best-effort received receipt before yielding a business event. No additional receipt is required from the application. A receipt means only that the event was read, not that credentials were applied; it does not replace confirmation. Reconcile the initial and reconnect snapshots; snapshot and pong require no receipt.

```json
{"event": "received", "event_id": "<event-id>"}
```

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

Event connections send application ping messages at 10-second intervals while idle and reconnect after approximately 30 seconds without messages. Reconnect uses 1–30 second exponential backoff and fresh signatures. Reconnect snapshots restore current state; past events are not replayed.

<!-- protocol-doc:end -->

<!-- socket-doc:start -->
# Socket API access

Use a local PAM Agent from any application without installing a JumpServer SDK or storing application AK/SK in the business process. The Agent handles Core authentication, authorization, credential refresh, A/B resolution and result retries. The application owns real connections and business validation.

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

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution. Snapshot fields received, credentials_read and result track distinct local stages. core_status separately tracks pending, confirmed, failed or not_required. Send the received operation to record a receipt; credential retrieval also records receipt. Neither operation confirms business success.

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

CLI/Socket secret queries and service credential reads return `account`, `source` and `last_pull_at`. `account.version` is the actual account credential version; ordinary results omit policy revisions, `account_revision` and `event_id`. `source` is `api` for a live Core fetch and `local` for retained data. `last_pull_at` is the RFC 3339 time of the last successful Core credential fetch for that account, even when its version is unchanged. It is retained with cached credentials across restarts; metadata sync, cache reads and failed pulls do not advance it. Older caches without a known pull time trigger one real credential fetch on the next online sync, even if the version is unchanged. Failed fetches leave the time unknown (null) and are retried; after success, unchanged metadata syncs do not fetch again. Pull-only queries do not enable offline credential retention.

`event_id` stays in notification metadata and script notification envelopes for reporting application success/failure; it is not an account field or a global latest-event pointer. Snapshots reuse the latest matching account/version/policy/client event, or create a republished event if none exists. Script readers use `credentials[selector].account`; existing Go templates, field mappings and EnvironmentFile variables keep their existing field names. Existing JSON readers must adopt the nested account structure.

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

Credential JSON files contain only `account`, with no `source` or `last_pull_at`. Pull times are exported separately to `last_pull_at.json` beside the directory configured by `store.root`, keyed by the actual account ID. The private state retains the same times. A successful live credential fetch updates the diagnostic file immediately, even for the same version, without rewriting credential files. Periodic version checks, cache reads and failed pulls do not advance it. Revoked accounts are removed from the diagnostic file.

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
