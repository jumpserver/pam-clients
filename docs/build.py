"""Build translated client guides; protocol names and examples stay identical."""

import argparse
import json
import re
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
SOURCE_DIRECTORY = "jms-pam-clients"

def source_archive(language):
    return f"curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/{language}/ -o jms-pam-clients.tar.gz &&\ntar -xzf jms-pam-clients.tar.gz"

PYTHON_INSTALL = f"{source_archive('python')} &&\npython3 -m pip install --upgrade --force-reinstall ./{SOURCE_DIRECTORY}/python"
LOCALES = ("en", "zh-hans", "zh-hant", "ja", "ko", "pt-br", "ru", "vi", "es", "fr")
EXAMPLES = {
    "go": (
        "Go",
        "go mod download\ngo run ./cmd/demo",
        "Go 1.23+ / coder/websocket",
        "cmd/demo/main.go",
    ),
    "java": (
        "Java",
        "mvn package dependency:copy-dependencies\njava -cp 'target/classes:target/dependency/*' org.jumpserver.pam.Demo",
        "JDK 11+ / Maven / Jackson",
        "src/main/java/org/jumpserver/pam/Demo.java",
    ),
    "node": ("Node.js", "npm ci\nnode demo.js", "Node.js 20.3+ / ws", "demo.js"),
    "curl": ("cURL", "bash demo.sh", "Bash / cURL / OpenSSL / base64", "demo.sh"),
}
NATIVE_METHODS = {
    "go": [
        "GetApplication(ctx) / GetClientInfo(ctx)",
        "ReportAccounts(ctx, accountIDs) / TryReportStatus(ctx, ApplicationStatus{Status: ..., AccountIDs: ...}) / ReportStatus(ctx, report)",
        "ListPolicies(ctx, search) / GetPolicy(ctx, PolicySelector{Key: ...})",
        "ListAccounts(ctx, search)",
        "ListAccountsPage(ctx, limit, offset, search)",
        "GetAccount(ctx, accountID)",
        "GetAccountFresh(ctx, accountID)",

        "ConfirmEvent(ctx, eventID, status, errorCode)",
        "WatchEvents(ctx, EventHandlers{...}) / StartEvents(ctx, handlers)",
        "EventWatcher.Stop() / EventWatcher.Wait()",
        "WatchCredentialEvents(ctx, handler)",
        "ListApplicationCommands(ctx)",
        "ReportApplicationCommandResult(ctx, commandID, status, errorCode)",
        "ExecuteApplicationCommand(ctx, event, handler)",
        "SyncAgent(ctx, AgentSyncOptions{...})",
        "Clone() / Close()",
    ],
    "java": [
        "getApplication() / getClientInfo()",
        "reportAccounts(accountIds) / reportStatus(status, accountId) / reportStatus(ApplicationStatus) / reportStatusStrict(report)",
        "listPolicies() / listPolicies(search) / getPolicy(key) / getPolicyById(policyId)",
        "listAccounts() / listAccounts(search)",
        "listAccountsPage(limit, offset, search)",
        "getAccount(accountId)",
        "getAccount(accountId, false)",

        "confirmEvent(eventId, status, errorCode)",
        "watchCredentialEvents()",
        "watchEvents(listener) / startEvents(listener)",
        "EventSubscription.stop() / close() / awaitTermination()",
        "listApplicationCommands()",
        "reportApplicationCommandResult(commandId, status, errorCode)",
        "executeApplicationCommand(event, handler)",
        "syncAgent(credentials, deliveredCredentials, configDigest, syncStatus, syncError)",
        "clone() / close()",
    ],
    "node": [
        "getApplication({signal}) / getClientInfo({signal})",
        "reportAccounts(accountIds) / reportStatus({status, accountId, accountIds, errorCode, raiseOnError, signal})",
        "listPolicies({search, signal}) / getPolicy({key, policyId, signal})",
        "listAccounts({search, signal})",
        "listAccountsPage({limit, offset, search, signal})",
        "getAccount({accountId})",
        "getAccount({accountId, allowLocalFallback: false})",

        "confirmEvent({eventId, status, errorCode})",
        "watchCredentialEvents({signal})",
        "watchEvents({signal}) / startEvents({signal}) / stopEvents()",
        "EventSubscription.stop() / done",
        "listApplicationCommands()",
        "reportApplicationCommandResult({commandId, status, errorCode})",
        "executeApplicationCommand(event, handler)",
        "syncAgent({credentials, deliveredCredentials, ...})",
        "clone() / close()",
    ],
}
NATIVE_EXAMPLES = {
    "go": ("go", "cmd/demo/main.go", "cmd/events/main.go"),
    "java": (
        "java",
        "src/main/java/org/jumpserver/pam/Demo.java",
        "src/main/java/org/jumpserver/pam/EventsDemo.java",
    ),
    "node": ("javascript", "demo.js", "events.js"),
}
HOOK_EXAMPLES = {
    "python": ("python", "subclass_demo.py"),
    "go": ("go", "cmd/hooks/main.go"),
    "java": ("java", "src/main/java/org/jumpserver/pam/EventsDemo.java"),
    "node": ("javascript", "hooks.js"),
}
LATEST_CREDENTIAL_APIS = {
    "python": """- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`""",
    "go": """- `account.FromLocal`
- `GetAccountFresh(ctx, accountID)`""",
    "java": """- `account.isFromLocal()`
- `getAccount(accountId, false)`""",
    "node": """- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`""",
}

ACCOUNT_LIST_EXAMPLES = {
    "python": ("python", "list_accounts.py"),
    "go": ("go", "cmd/accounts/main.go"),
    "java": ("java", "src/main/java/org/jumpserver/pam/AccountsDemo.java"),
    "node": ("javascript", "accounts.js"),
}


def account_listing_section(language, texts):
    syntax, example = ACCOUNT_LIST_EXAMPLES[language]
    code = (CLIENTS / language / example).read_text(encoding="utf-8").rstrip()
    if language == "node":
        code = code.replace("require('./index')", "require('@jumpserver/pam')")
    return f"""### {texts["account_listing_title"]}

{texts["account_listing"]}

```{syntax}
{code}
```
"""

APPLICATION_INFO_EXAMPLES = {
    "python": ("python", "application_info.py"),
    "go": ("go", "cmd/applicationinfo/main.go"),
    "java": ("java", "src/main/java/org/jumpserver/pam/ApplicationInfoDemo.java"),
    "node": ("javascript", "application-info.js"),
}


STATUS_EXAMPLES = {
    'python': ('python', 'client.report_accounts([account.id])\nclient.report_accounts([])  # Clear the complete explicit usage snapshot.\nclient.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")'),
    'go': ('go', 'err := client.ReportAccounts(ctx, []string{account.ID})\n// ReportAccounts(ctx, nil) clears the complete explicit usage snapshot.\n// ReportStatus(ctx, report) also reports application health.'),
    'java': ('java', 'boolean accepted = client.reportAccounts(java.util.List.of(account.getId()));\nclient.reportAccounts(java.util.Collections.emptyList());'),
    'node': ('javascript', "const accepted = await client.reportAccounts([account.id])\nawait client.reportAccounts([])"),
}


def status_reporting_section(language, texts):
    syntax, example = STATUS_EXAMPLES[language]
    return f'''### {texts["status_reporting_title"]}

{texts["status_reporting"]}

```{syntax}
{example}
```
'''


def application_info_section(language, texts):
    syntax, example = APPLICATION_INFO_EXAMPLES[language]
    code = (CLIENTS / language / example).read_text(encoding="utf-8").rstrip()
    application_method, client_method = {
        "python": ("get_application", "get_client_info"),
        "go": ("GetApplication", "GetClientInfo"),
        "java": ("getApplication", "getClientInfo"),
        "node": ("getApplication", "getClientInfo"),
    }[language]
    description = texts["application_info"].replace("get_application", application_method).replace("get_client_info", client_method)
    if language == "node":
        code = code.replace("require('./index')", "require('@jumpserver/pam')")
    return f"""### {texts["application_info_title"]}

{description}

```{syntax}
{code}
```
"""

LIFECYCLES = {
    "python": "`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`",
    "go": "`WatchEvents(ctx, handlers)` / `StartEvents(ctx, handlers)`; `Stop()` / `Wait()`; `context.CancelFunc`",
    "java": "`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`",
    "node": "`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`",
}


def latest_credentials_section(language, texts):
    return f"""### {texts["latest_credentials_title"]}

{texts["latest_credentials_behavior"]}

{LATEST_CREDENTIAL_APIS[language]}

{texts["live_refresh"]}

"""


def managed_section(language, texts):
    syntax, example = HOOK_EXAMPLES[language]
    code = (CLIENTS / language / example).read_text(encoding="utf-8").rstrip()
    if language == "node":
        code = code.replace("require('./index')", "require('@jumpserver/pam')")
    snippet = f"```{syntax}\n{code}\n```"
    if language == "python":
        snippet = f"<!-- python-events-example:start -->\n{snippet}\n<!-- python-events-example:end -->"
    else:
        snippet = native_example_section(language, example, snippet)
    return f"""## {texts["managed_title"]}

{texts["managed_intro"]}

{snippet}

{texts["managed_behavior"]}

### {texts["lifecycle_title"]}

{LIFECYCLES[language]}

{texts[f"managed_lifecycle_{language}"]}

{latest_credentials_section(language, texts)}
"""


NATIVE_INSTALL = {
    "go": f"""```bash
{source_archive('go')} &&
go mod edit -replace github.com/jumpserver/pam-clients/go=./jms-pam-clients/go &&
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```""",
    "java": f"""```bash
{source_archive("java")} &&
mvn -f ./{SOURCE_DIRECTORY}/java/pom.xml install
```

```xml
<dependency>
  <groupId>org.jumpserver</groupId>
  <artifactId>jms-pam</artifactId>
  <version>1.0.2</version>
</dependency>
```

```java
import org.jumpserver.pam.Client;
```""",
    "node": f"""```bash
{source_archive("node")} &&
npm install ./{SOURCE_DIRECTORY}/node
```

```javascript
const {{ Client }} = require('@jumpserver/pam')
// ESM: import {{ Client }} from '@jumpserver/pam'
```""",
}
NATIVE_ENVIRONMENT = """export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
"""

API_TABLE = """| HTTP | API | JSON / query |
| --- | --- | --- |
| GET | `/api/v1/accounts/credential-client/credential/` | `instance_id`, `account_id` |
| GET | `/api/v1/accounts/credential-client/accounts/` | `instance_id`, `limit`, `offset`, `search` |
| GET | `/api/v1/accounts/credential-client/application/` | `instance_id` |
| GET | `/api/v1/accounts/credential-client/client/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/status/` | `instance_id`, `status`, `account_ids`, `error_code` |
| GET | `/api/v1/accounts/credential-client/policies/` | `instance_id`, `search` |
| GET | `/api/v1/accounts/credential-client/policy/` | `instance_id`, `key` or `policy_id` |
| POST | `/api/v1/accounts/credential-client/event-result/` | `instance_id`, `event_id`, `status`, `error_code` |
| GET | `/api/v1/accounts/credential-client/commands/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/command-result/` | `instance_id`, `command_id`, `status`, `error_code` |
| WebSocket | `/ws/accounts/credential-events/` | `instance_id` |
"""

ENVIRONMENT = """export API_URL='https://jumpserver.example.com'
export API_KEY_ID='<app-id>'
export API_KEY_SECRET='<app-secret>'
export ORG_ID='<org-id>'
"""

PYTHON_CONFIG = """import os

client_options = {
    "endpoint": "https://jumpserver.example.com",
    "app_id": "<application-id>",
    "app_secret": "<application-secret>",
    "org_id": "<org-id>",
}
instance_id = os.environ.get("JMS_INSTANCE_ID") or "app-node-1"
"""

HEADERS = """(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version"""


def legacy_guide(language, texts):
    label, command, runtime, example = EXAMPLES[language]
    extra = texts["curl_note"]
    return f"""# {label} — {texts["guide"]}

{texts["legacy_intro"]}

## {texts["requirements"]}

- {runtime}
- `{example}`

{extra}

## {texts["configure"]}

{texts["legacy_config"]}

```bash
cd {language}
{ENVIRONMENT}
{command}
```

{texts["targets"]}

## {texts["response"]}

```http
GET /api/v1/accounts/integration-applications/account-secret/?asset=ubuntu_docker&account=root
```

```json
{{"id":"<app-id>","secret":"<account-secret>"}}
```

{texts["legacy_response"]}

## {texts["troubleshooting"]}

{texts["legacy_errors"]}

## {texts["credential_policies"]}

{texts["policy_access"]}

{API_TABLE}
{texts["signature"]}

```text
{HEADERS}
```

{texts["confirmation"]}

{texts["receipts"]}
"""


SOURCE_INSTALL = {
    'python': 'python3 -m pip install /path/to/pam-clients/python',
    'go': 'go mod edit -replace=github.com/jumpserver/pam-clients/go=/path/to/pam-clients/go\ngo mod tidy',
    'java': 'mvn -f /path/to/pam-clients/java/pom.xml install',
    'node': 'npm install /path/to/pam-clients/node',
}


def source_installation_section(language, texts):
    return f"""### {texts['source_install_title']}

{texts['source_install']}

```bash
{SOURCE_INSTALL[language]}
```
"""


def native_example_section(language, example, snippet):
    filename = {
        'go': 'events.go' if '/hooks/' in example else 'demo.go',
        'java': example,
        'node': Path(example).name,
    }[language]
    command = {
        'go': f'go run ./{filename}',
        'java': (
            'mvn compile dependency:copy-dependencies\n'
            f"java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.{Path(example).stem}"
        ),
        'node': f'node {filename}',
    }[language]
    return f"`{filename}`\n\n{snippet}\n\n```bash\n{command}\n```"


def overview_guide(texts):
    return f"""<!-- overview-doc:start -->

# {texts['overview_title']}

## {texts['overview_relationships_title']}

{texts['overview_relationships']}

## {texts['overview_choices_title']}

{texts['overview_choices']}

## {texts['overview_flow_title']}

{texts['overview_flow']}

## {texts['overview_reading_title']}

{texts['overview_reading']}

<!-- overview-doc:end -->
"""


PROTOCOL_EVENT_GROUPS = [
    (
        'accounts',
        [
            'credential.updated',
            'app.scope.updated',
        ],
    ),
    (
        'changes',
        [
            'credential.change.started',
            'credential.change.completed',
            'credential.change.failed',
        ],
    ),
    (
        'rotation',
        [
            'rotation.verify.started',
            'rotation.verify.skipped',
            'rotation.verify.failed',
            'rotation.verify.cancelled',
            'rotation.started',
            'rotation.waiting_for_app',
            'rotation.source.waiting',
            'rotation.source.ready',
            'rotation.completed',
            'rotation.failed',
            'rotation.force_stopped',
        ],
    ),
    (
        'commands',
        [
            'credential.switch.requested',
            'app.restart.requested',
        ],
    ),
    (
        'connection',
        [
            'snapshot',
            'ping',
            'pong',
            'received',
        ],
    ),
    (
        'legacy',
        [
            'credential.revoked',
            'configuration.updated',
            'rotation.preparation.started',
            'rotation.accounts.aligned',
            'rotation.standby.waiting',
            'rotation.preparation.ready',
            'rotation.preparation.cancelled',
            'rotation.verification.started',
            'rotation.verification.failed',
            'rotation.verification.cancelled',
            'application.scope.updated',
            'application.restart.requested',
            'rotation.waiting_for_application',
            'rotation.verification.skipped',
        ],
    ),
]


def protocol_event_catalog(texts):
    sections = [f"## {texts['protocol_catalog_title']}\n\n{texts['protocol_catalog_intro']}"]
    for group, events in PROTOCOL_EVENT_GROUPS:
        rows = "\n".join(
            f"| `{event}` | {texts['protocol_event_' + event.replace('.', '_')]} |"
            for event in events
        )
        sections.append(
            f"### {texts['protocol_catalog_' + group + '_title']}\n\n"
            f"{texts['protocol_catalog_' + group + '_help']}\n\n"
            f"| {texts['protocol_event_name']} | {texts['protocol_event_meaning']} |\n| --- | --- |\n{rows}"
        )
    return "\n\n".join(sections)


def protocol_guide(texts):
    return f"""<!-- protocol-doc:start -->

# {texts['protocol_title']}

{texts['protocol_intro']}

## {texts['protocol_events_title']}

{texts['protocol_events']}

{protocol_event_catalog(texts)}

## {texts['protocol_fields_title']}

{texts['protocol_fields']}

## {texts['protocol_snapshot_title']}

{texts['protocol_snapshot']}

```json
{{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }}]
}}
```

## {texts['protocol_results_title']}

{texts['sdk_receipts']}

```json
{{"event": "received", "event_id": "<event-id>"}}
```

{texts['event_results']}

## {texts['protocol_commands_title']}

{texts['protocol_commands']}

## {texts['protocol_connection_title']}

{texts['event_liveness']}

{texts['protocol_scope_legacy']}

{texts['event_compatibility']}

<!-- protocol-doc:end -->
"""


def native_guide(language, texts):
    label, _, runtime, _ = EXAMPLES[language]
    syntax, quickstart, _ = NATIVE_EXAMPLES[language]
    quickstart_code = (CLIENTS / language / quickstart).read_text(encoding="utf-8").rstrip()
    if language == "node":
        quickstart_code = quickstart_code.replace("require('./index')", "require('@jumpserver/pam')")
    methods = "\n".join(f"- `{method}`" for method in NATIVE_METHODS[language])
    quickstart_example = native_example_section(language, quickstart, f'```{syntax}\n{quickstart_code}\n```')
    return f"""# JumpServer PAM {label} SDK

<!-- sdk-doc:start -->

## 1. {texts["onboarding_title"]}

{texts["onboarding"]}

{texts["sdk_workflow"]}

- {runtime}

## 2. {texts["sdk_install_title"]}

{texts["native_setup"]}

{texts["native_install"]}

{NATIVE_INSTALL[language]}

{source_installation_section(language, texts)}

```bash
{NATIVE_ENVIRONMENT.rstrip()}
```

## 3. {texts["first_account_title"]}

{quickstart_example}

{managed_section(language, texts).replace('## ', '## 4. ', 1)}

## 5. {texts["methods"]}

{account_listing_section(language, texts)}

{application_info_section(language, texts)}

{status_reporting_section(language, texts)}

### API

{methods}

## 6. {texts["validation_title"]}

{texts["sdk_validation"]}

### {texts["troubleshooting"]}

{texts["native_errors"]}

`{"PAMException" if language == "java" else "PAMError"}`

{texts["native_compatibility"]}

<!-- sdk-doc:end -->

{overview_guide(texts)}

{protocol_guide(texts)}

{socket_guide(texts)}
"""


AGENT_BASE = {
    'version': 1, 'endpoint': 'https://jumpserver.example.com',
    'app_id': '<application-id>', 'app_secret': '<application-secret>',
    'org_id': '<org-id>', 'pull_interval': 300,
}
AGENT_STORE = {
    'store': {'accounts': ['all'], 'root': '/opt/jumpserver-pam/credentials', 'format': 'json', 'owner': 'appuser'},
}
AGENT_SOCKET = {
    'socket': {'path': '/run/jms-pam-agent/agent.sock', 'owner': 'appuser'},
}
AGENT_RULE = {
    'name': 'sample-app', 'allow_account_switch': True, 'update_on_notification': True,
    'accounts': [{'id': '<primary-account-id>'}],
    'update_config': {
        'file': '/etc/sample-app/config.yml', 'format': 'yaml',
        'fields_map': {'DB_USER': 'username', 'DB_PASSWORD': 'secret'},
    },
    'reload_service': {'unit': 'sample-app.service', 'operation': 'restart'},
    'app_check': {'path': '/usr/local/libexec/jms-pam/check-running-db', 'confirm_on_success': True},
}
AGENT_MULTI_SERVICE = {
    **AGENT_RULE,
    'accounts': [
        {'id': '<primary-account-id>', 'alias': 'database'},
        {'id': '<cache-account-id>', 'alias': 'cache'},
    ],
    'update_config': {
        'file': '/etc/sample-app/config.yml', 'format': 'yaml',
        'fields_map': {'DB_USER': 'database.username', 'DB_PASSWORD': 'database.secret', 'CACHE_USER': 'cache.username', 'CACHE_PASSWORD': 'cache.secret'},
    },
}
AGENT_ENVIRONMENT_RULE = {
    'name': 'sample-app-env', 'allow_account_switch': True, 'update_on_notification': True,
    'accounts': [{'id': '<primary-account-id>'}],
    'update_config': {'files': [{'path': '/etc/sample-app/database.env', 'format': 'environment', 'owner': 'appuser'}]},
    'reload_service': {'unit': 'sample-app.service', 'operation': 'restart'},
}
AGENT_SAMPLE = {
    **AGENT_BASE, **AGENT_STORE, **AGENT_SOCKET,
    'services': [AGENT_RULE],
}
AGENT_UPDATE_TEMPLATE = {
    'files': [{
        'path': '/etc/sample-app/credentials.json', 'format': 'template',
        'template_file': '/etc/pam-agent/templates/credentials.json.tmpl', 'owner': 'appuser',
    }],
}
AGENT_TEMPLATE = '''{
  "database": {
    "username": {{json (index .Credentials "<primary-account-id>").Username}},
    "password": {{json (index .Credentials "<primary-account-id>").Secret}}
  }
}'''
AGENT_UPDATE_SCRIPT = {
    'target': '/etc/sample-app/credentials.json',
    'script': {
        'path': '/usr/local/libexec/pam-agent/update-credentials',
        'args': ['/etc/sample-app/credentials.json'], 'timeout_seconds': 30,
    },
}
AGENT_SCRIPT_PAYLOAD = {
    'event': 'credential.updated',
    'credentials': {
        '<primary-account-id>': {
            'account_id': '<active-account-id>', 'username': 'db_app',
            'secret': '<password>', 'event_id': '<event-id>',
        },
    },
}
AGENT_UPDATE_SCRIPT_SOURCE = '''#!/usr/bin/env python3
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
'''


def agent_configuration_section(texts):
    return f"""{texts['services_model']}

{texts['services_field_names']}

| {texts['configure']} | {texts['agent_rules_title']} |
| --- | --- |
| `credential_check` | {texts['credential_check_step']} |
| `update_config` | {texts['update_config_step']} |
| `reload_service` | {texts['reload_service_step']} |
| `app_check` | {texts['app_check_step']} |

`services`:

```json
{json.dumps([AGENT_RULE], indent=2)}
```

### accounts

{texts['aliases_model']}

```json
{json.dumps(AGENT_MULTI_SERVICE, indent=2)}
```

### update_config

{texts['update_config_methods']}

#### file + fields_map

{texts['agent_config_format']}

```json
{json.dumps({'update_config': AGENT_RULE['update_config']}, indent=2)}
```

#### files + template_file

{texts['update_config_template']}

```json
{json.dumps({'update_config': AGENT_UPDATE_TEMPLATE}, indent=2)}
```

`/etc/pam-agent/templates/credentials.json.tmpl`:

```gotemplate
{AGENT_TEMPLATE}
```

{texts['update_config_payload_keys']}

#### script

{texts['update_config_script']}

```json
{json.dumps({'update_config': AGENT_UPDATE_SCRIPT}, indent=2)}
```

{texts['update_config_script_payload']}

```json
{json.dumps(AGENT_SCRIPT_PAYLOAD, indent=2)}
```

{texts['update_config_script_example']}

`/usr/local/libexec/pam-agent/update-credentials`:

```python
{AGENT_UPDATE_SCRIPT_SOURCE.rstrip()}
```

{texts['update_config_apply']}

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```
"""


def socket_guide(texts):
    config = {
        **AGENT_BASE,
        'socket': {'path': '/run/jms-pam-agent/agent.sock', 'owner': 'appuser'},
        'services': [{
            'name': 'orders', 'delivery': 'socket', 'allow_account_switch': True,
            'restart_supported': False, 'accounts': [{'id': '<primary-account-id>'}],
        }],
    }
    return f'''<!-- socket-doc:start -->
# {texts['socket_title']}

{texts['socket_intro']}

## 1. Unix Socket / HTTP / WebSocket

{texts['socket_transport']}

## 2. Agent / services

{texts['socket_config']}

```json
{json.dumps(config, indent=2)}
```

## 3. Events / A/B

{texts['socket_workflow']}

{texts['socket_snapshots']}

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {{
    "delivery_id": "<delivery-id>",
    "event": "credential.updated",
    "service_id": "orders",
    "credentials": {{
      "<primary-account-id>": {{
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }}
    }},
    "revoked_account_ids": []
  }},
  "commands": []
}}
```

```json
{{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}}
```

```json
{{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}}
```

```json
{{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}}
```

{texts['socket_results']}

## 4. Commands / status

{texts['socket_commands']}

```json
{{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}}
```

{texts['socket_status']}

```json
{{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}}
```

## 5. API

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
Every request includes `request_id`; responses contain the same ID and `result` or `error: {{code, status}}`. Snapshots can arrive between responses. `{{"event":"ping"}}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \\
  http://localhost/v1/services/orders
```

{texts['socket_boundary']}

## 6. Python

{texts['socket_example']}

```bash
python3 -m pip install 'websockets>=11'
{source_archive('socket')}
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
{(CLIENTS / 'socket' / 'python_example.py').read_text().rstrip()}
```
<!-- socket-doc:end -->
'''


def agent_guide(texts):
    return f"""# {texts['agent_title']}

{texts['agent_overview']}

## 1. {texts['onboarding_title']}

{texts['agent_onboarding']}

## 2. {texts['agent_install_title']}

### Linux / systemd

{texts['agent_setup']}

{texts['agent_bootstrap']}

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \\
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### {texts['foreground_title']}

{texts['services_portable']}

{texts['foreground_binary']}

```bash
chmod 0600 ./pam-agent.json
./pam-agent init-local \\
  --bootstrap ./pam-agent.json \\
  --directory "$HOME/.jms-pam-agent/sample-app"
./pam-agent run --local --config "$HOME/.jms-pam-agent/sample-app/agent.json"
```

```powershell
$agentDir = Join-Path $HOME '.jms-pam-agent\\sample-app'
.\\pam-agent.exe init-local --bootstrap (Join-Path $PWD 'pam-agent.json') --directory $agentDir
.\\pam-agent.exe run --local --config (Join-Path $agentDir 'agent.json')
```

## 3. {texts['agent_sample_title']}

{texts['configuration_blocks_help']}

```json
{json.dumps(AGENT_SAMPLE, indent=2)}
```

{texts['agent_instance_id']}

{texts['pull_interval']}

## 4. {texts['agent_store_title']}

{texts['store_purpose']}

{texts['store_model']}

```json
{json.dumps(AGENT_STORE, indent=2)}
```

| {texts['configure']} | {texts['agent_default_output_title']} |
| --- | --- |
| `store.accounts: ["all"]` | {texts['store_all_label']} |
| `store.accounts: ["<account-id>"]` | {texts['store_selected_label']} |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

{texts['store_layout']}

## 5. {texts['agent_socket_title']}

{texts['socket_purpose']}

{texts['socket_model']}

```json
{json.dumps(AGENT_SOCKET, indent=2)}
```

### {texts['account_lookup_title']}

{texts['agent_queries']}

```bash
pam-agent get_accounts
pam-agent get_secret '<account-id>'

curl --fail --silent --show-error \\
  --unix-socket '/run/jms-pam-agent/agent.sock' \\
  http://localhost/v1/health

curl --fail --silent --show-error \\
  --unix-socket '/run/jms-pam-agent/agent.sock' \\
  'http://localhost/v1/credential?account_id=<account-id>'
```

### {texts['confirmation_title']}

{texts['agent_confirmation']}

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. {texts['agent_service_title']}

{agent_configuration_section(texts)}

### EnvironmentFile / systemd

{texts['agent_environment_file']}

`services`:

```json
{json.dumps([AGENT_ENVIRONMENT_RULE], indent=2)}
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

## 7. {texts['validation_title']}

{texts['agent_validation']}

### {texts['troubleshooting']}

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

{texts['recovery']}
"""


def python_guide(texts):
    return f"""# JumpServer PAM Python SDK / PAM Agent

{texts["python_intro"]}

<!-- agent-doc:start -->

{agent_guide(texts)}

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# {texts['sdk_title']}

## 1. {texts["onboarding_title"]}

{texts["onboarding"]}

{texts["sdk_workflow"]}

## 2. {texts["sdk_install_title"]}

{texts["sdk_setup"]}

```bash
{PYTHON_INSTALL}
```

{source_installation_section('python', texts)}

{texts["sdk_config"]}

```python
# jms_pam_config.py
{PYTHON_CONFIG.rstrip()}
```

## 3. {texts["first_account_title"]}

{texts["sdk_run"]}

```bash
cd /path/to/your/application
chmod 0600 jms_pam_config.py
python3
```

<!-- python-account-example:start -->
```python
{(CLIENTS / "python" / "account_demo.py").read_text().rstrip()}
```
<!-- python-account-example:end -->

{managed_section("python", texts).replace('## ', '## 4. ', 1)}

## 5. {texts["methods"]}

{account_listing_section("python", texts)}

{application_info_section("python", texts)}

{status_reporting_section("python", texts)}

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

## 6. {texts["validation_title"]}

{texts["sdk_validation"]}

### {texts["troubleshooting"]}

{texts["sdk_errors"]}

{texts["native_compatibility"]}

<!-- sdk-doc:end -->

{overview_guide(texts)}

{protocol_guide(texts)}

{socket_guide(texts)}
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true")
    args = parser.parse_args()
    differences = []
    for locale in LOCALES:
        texts = json.loads(
            (Path(__file__).parent / "translations" / f"{locale}.json").read_text(
                encoding="utf-8"
            )
        )
        documents = {
            CLIENTS / language / f"README.{locale}.md": (
                legacy_guide(language, texts)
                if language == "curl"
                else native_guide(language, texts)
            )
            for language in EXAMPLES
        }
        if locale in ("en", "zh-hans"):
            documents[CLIENTS / "agent" / f"README.{locale}.md"] = agent_guide(texts)
        documents[CLIENTS / "socket" / f"README.{locale}.md"] = socket_guide(texts)
        # The complete English and Simplified Chinese Python references are maintained directly.
        if locale not in ("en", "zh-hans"):
            documents[CLIENTS / "python" / f"README.{locale}.md"] = python_guide(texts)
        else:
            path = CLIENTS / "python" / f"README.{locale}.md"
            content = path.read_text(encoding="utf-8")
            guide = python_guide(texts)
            for tab in ("agent", "sdk", "overview", "protocol", "socket"):
                start = f"<!-- {tab}-doc:start -->"
                end = f"<!-- {tab}-doc:end -->"
                block = guide.split(start, 1)[1].split(end, 1)[0]
                if tab in ("overview", "protocol", "socket") and start not in content:
                    content = content.rstrip() + "\n\n" + start + block + end + "\n"
                content, count = re.subn(
                    re.escape(start) + r".*?" + re.escape(end),
                    lambda _: start + block + end, content, flags=re.S,
                )
                if count != 1:
                    raise ValueError(f"Expected one {tab} document block in {path}")
            for kind, example in (("account", "account_demo.py"), ("events", "subclass_demo.py")):
                start = f"<!-- python-{kind}-example:start -->"
                end = f"<!-- python-{kind}-example:end -->"
                snippet = (CLIENTS / "python" / example).read_text().rstrip()
                replacement = f"{start}\n```python\n{snippet}\n```\n{end}"
                # Keep prose hand-maintained while checking shared examples for drift.
                content, count = re.subn(
                    re.escape(start) + r".*?" + re.escape(end),
                    lambda _: replacement, content, flags=re.S,
                )
                if not count:
                    raise ValueError(f"Missing shared {kind} example in {path}")
            documents[path] = content
        for path, content in documents.items():
            content = content.rstrip() + "\n"
            if args.check:
                if not path.is_file() or path.read_text(encoding="utf-8") != content:
                    differences.append(str(path.relative_to(CLIENTS)))
            else:
                path.write_text(content, encoding="utf-8")
    if differences:
        parser.exit(1, "\n".join(differences) + "\n")


if __name__ == "__main__":
    main()
