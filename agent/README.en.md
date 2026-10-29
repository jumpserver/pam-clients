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

## 4. store

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

## 5. socket

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

## 6. services

Add the service objects below to `services`. Each service needs a unique stable `name`. `allow_account_switch` belongs to the service and defaults to false: when true, accounts with a unique A/B association follow the active account; other accounts remain fixed. Declaring A is sufficient. `update_on_notification` also belongs to the service and defaults to true. False defers business actions until scheduled reconciliation (300 seconds by default), while storage and authorization revocations still update. Each service tracks its own applied versions. Only successful application checks with `confirm_on_success: true` can automatically confirm a rotation, after every relevant service succeeds.

New configurations use `update_config` → `reload_service` → `app_check`: update the business file, reload/restart the service (or run a fixed script), then verify the running application. These names require an updated PAM Agent. `services` accepts only these three step names; old names are rejected.

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
  "event": "credentials.updated",
  "credentials": {
    "<primary-account-id>": {
      "account_id": "<active-account-id>",
      "username": "db_app",
      "secret": "<password>",
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
credential = payload["credentials"]["<primary-account-id>"]
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
