# Go Agent integration

Run the standalone Go Agent on the application host; Python is not required. Follow the sections below to install it, select delivery, configure business actions and verify the result.

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve (pull). Use account IDs, not usernames or policy IDs.
2. Declare store/services to synchronize authorized accounts. Bind notifications or A/B policies when updates must be pushed.
3. Download identity/configuration from the application access wizard. Use a stable, unique `instance_id` for each replica and keep AK/SK private.

## 2. Install and start

### Linux / systemd

Core and Agent use protocol version 1 and `config_version: 1`. Configure `store` and `services` independently; do not mix them with the older `delivery` / `rules` structure. Core supplies account scope and version metadata, never local executable paths. Restart the Agent after editing its private configuration.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

Run in the directory containing the downloaded `jms_pam_agent.json`. The Linux service commands below require root and systemd. Change the instance ID for each replica. For foreground mode use the corresponding host binary; macOS/Windows binaries may require a source build.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Foreground mode

For macOS, Windows or non-root Linux, initialize once with `init-local`, then reuse the generated private configuration with `run --local`. Standard JSON and environment files are supported; systemd actions require the Linux service.

Save the foreground binary as `jms-pam-agent` (`jms-pam-agent.exe` on Windows); on Linux/macOS give it execute permission with `chmod 0755 ./jms-pam-agent`.

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

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every account authorized for this application, or specify account IDs. Subscriptions only provide change notifications; authorized accounts can be synchronized without a subscription. Omit `store` for services-only use; an explicit empty array stores nothing. Never combine `all` with IDs.

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

`pull_interval` is the global polling interval in seconds, a positive integer with a default of 300. The Agent synchronizes immediately at startup, then checks account and credential versions at this interval, fetching secrets only when versions or the active account change. Services with `update_on_notification: false` update on the next poll; notifications do not postpone it.

| Configure and run | store |
| --- | --- |
| `store.accounts: ["all"]` | All application-authorized accounts, including accounts without subscriptions. |
| `store.accounts: ["<account-id>"]` | Only the explicitly declared account IDs. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

Each stored account has its own file named by its actual account ID, including separate A/B accounts. Files contain plaintext credentials with mode 0600. A/B standby accounts being changed are temporarily unavailable. Successful authorization reconciliation removes revoked files. `local_api.owner` controls socket access and the default owner of business output files; it defaults to `store.owner`, then the Agent user. Socket access is independent of file storage. Private state and events retain their default paths. An unchanged reconciliation reads metadata without fetching passwords again; actual fetches remain audited and participate in rotation access observation.

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

## 4. Update business configuration and A/B rotation

Add the service objects below to `services`. Each service needs a unique stable `name`. `allow_account_switch` belongs to the service and defaults to false: when true, accounts with a unique A/B association follow the active account; other accounts remain fixed. Declaring A is sufficient. `update_on_notification` also belongs to the service and defaults to true. False defers business actions until scheduled reconciliation (300 seconds by default), while storage and authorization revocations still update. Each service tracks its own applied versions. Only successful application checks with `confirm_on_success: true` can automatically confirm a rotation, after every relevant service succeeds.

New configurations use `update_config` → `reload_service` → `app_check`: update the business file, reload/restart the service (or run a fixed script), then verify the running application. These names require an updated Agent. `services` accepts only these three step names; old names are rejected.

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

### Configuration format

`update_config.format` defaults to `auto`; omitting it has the same effect. Detection uses the file extension: `.txt`, `.env` and `.properties` use `env` (`KEY=value`); `.yml` and `.yaml` use `yaml` (top-level `KEY: value`, written as quoted strings). For other extensions, explicitly choose `env` or `yaml`. Mapped fields must already exist exactly once; unrelated settings are preserved. INI sections and nested YAML require `files` + `template_file` to generate the whole file, or a custom `script`. Environment-file updates take effect after the application rereads the file or restarts.

Core and Agent use protocol version 1 and `config_version: 1`. Configure `store` and `services` independently; do not mix them with the older `delivery` / `rules` structure. Core supplies account scope and version metadata, never local executable paths. Restart the Agent after editing its private configuration.

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
  'http://localhost/v1/credential?account_id=<account-id>'
```

### Confirm application success

Validate the running connection and switch the application before confirming an event. Use either a successful `app_check` with `confirm_on_success: true`, or explicit confirmation after business verification. Do not call the confirmation API from a synchronous check script. Local results are persisted: `confirmed` means Core accepted the result; `pending` is retried.

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

The Agent reconciles at startup, on relevant notifications and at the configured interval (300 seconds by default). Temporary outages retain still-authorized credentials. Explicit revocation or application disable blocks local reads and removes managed store files, even if the next refresh fails. Business configuration files remain under the application administrator’s control. Notification traffic does not postpone periodic service updates. SIGINT/SIGTERM closes the server and event reader.
