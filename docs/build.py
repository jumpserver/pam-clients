"""Build translated client guides; protocol names and examples stay identical."""

import argparse
import json
import re
from pathlib import Path

CLIENTS = Path(__file__).resolve().parents[1]
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
    "java": ("java", "src/main/java/org/jumpserver/pam/HooksDemo.java"),
    "node": ("javascript", "hooks.js"),
}
LATEST_CREDENTIAL_APIS = {
    "python": """- `credential.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`""",
    "go": """- `credential.FromLocal`
- `GetCredentialFresh(ctx, selector)`""",
    "java": """- `credential.isFromLocal()`
- `getCredential(key, false)` / `getCredentialByAccountId(accountId, false)`""",
    "node": """- `credential.fromLocal`
- `getCredential({key, allowLocalFallback: false})` / `getCredential({accountId, allowLocalFallback: false})`""",
}

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

{texts["event_liveness"]}
"""


def managed_section(language, texts):
    syntax, example = HOOK_EXAMPLES[language]
    return f"""## {texts["managed_title"]}

{texts["managed_intro"]}

```{syntax}
{(CLIENTS / language / example).read_text(encoding="utf-8").rstrip()}
```

{texts["managed_behavior"]}

{LIFECYCLES[language]}

{texts[f"managed_lifecycle_{language}"]}

{latest_credentials_section(language, texts)}
"""


NATIVE_INSTALL = {
    "go": """```bash
go get github.com/jumpserver/pam-clients/go@v1.0.2
```

```go
import pam "github.com/jumpserver/pam-clients/go"
```""",
    "java": """```bash
mvn -f /path/to/pam-clients/java/pom.xml install
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
    "node": """```bash
npm install /path/to/pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
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


def native_guide(language, texts):
    label, command, runtime, example = EXAMPLES[language]
    syntax, quickstart, events = NATIVE_EXAMPLES[language]
    directory = CLIENTS / language
    methods = "\n".join(f"- `{method}`" for method in NATIVE_METHODS[language])
    return f"""# JumpServer PAM {label} SDK

{texts["native_intro"]}

## {texts["requirements"]}

- {runtime}
- `{example}`

## {texts["configure"]}

{texts["native_setup"]}

```bash
cd {language}
{NATIVE_ENVIRONMENT}
{command}
```

{texts["native_install"]}

{NATIVE_INSTALL[language]}

## {texts["response"]}

```{syntax}
{(directory / quickstart).read_text(encoding="utf-8").rstrip()}
```

## {texts["events"]}

{texts["event_results"]}

```{syntax}
{(directory / events).read_text(encoding="utf-8").rstrip()}
```

{texts["event_compatibility"]}

## {texts["commands"]}

{texts["native_commands"]}

## {texts["methods"]}

{methods}

## {texts["troubleshooting"]}

{texts["native_errors"]}

`{"PAMException" if language == "java" else "PAMError"}`

{texts["native_compatibility"]}

## {texts["agent_title"]}

{texts["native_agent"]}

{texts["agent_rules"]}

{agent_configuration_section(texts)}
"""


AGENT_SAMPLE = {'endpoint': 'https://jumpserver.example.com', 'app_id': '<application-id>', 'app_secret': '<application-secret>', 'org_id': '<org-id>', 'instance_id': 'orders-node-1', 'delivery': {'delivery_mode': 'json', 'delivery_root': '/opt/jumpserver-pam/credentials', 'app_user': 'orders'}, 'rules': []}
AGENT_RULE = {
    'accounts': [{
        'account_id': '<primary-account-id>', 'allow_account_switch': True,
    }],
    'config_update': {
        'file': '/etc/order-service/config.yml',
        'fields_map': {'DB_USER': 'username', 'DB_PASSWORD': 'secret'},
    },
    'service_action': {'unit': 'order-service.service', 'operation': 'restart'},
    'application_check': {
        'path': '/usr/local/libexec/jms-pam/check-running-db', 'confirm_on_success': True,
    },
}


def agent_configuration_section(texts):
    return f"""{texts['agent_config']}

```json
{json.dumps(AGENT_SAMPLE, indent=2)}
```

`rules`:

```json
{json.dumps([AGENT_RULE], indent=2)}
```

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```
"""


def python_guide(texts):
    return f"""# JumpServer PAM Python SDK / Agent

{texts["python_intro"]}

<!-- agent-doc:start -->

## {texts["agent_title"]}

{texts["agent_setup"]}


{texts["agent_portable"]}

{texts["agent_paths"]}

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

{texts["agent_delivery"]}

{texts["agent_rules"]}

{agent_configuration_section(texts)}

### {texts["local_api"]}

```bash
curl --fail --silent --show-error \\
  --unix-socket '/run/jms-pam-agent/agent.sock' \\
  http://localhost/v1/health

curl --fail --silent --show-error \\
  --unix-socket '/run/jms-pam-agent/agent.sock' \\
  'http://localhost/v1/credentials/<credential-key>'
```

{texts["confirmation"]}

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \\
  --socket '/run/jms-pam-agent/agent.sock'
```

{texts["agent_confirm"]}

### {texts["troubleshooting"]}

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

{texts["recovery"]}

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## {texts["sdk_title"]}

{texts["sdk_setup"]}

```bash
python3 -m pip install jms-pam
```

{texts["sdk_config"]}

<!-- python-account-example:start -->
```python
{(CLIENTS / "python" / "account_demo.py").read_text().rstrip()}
```
<!-- python-account-example:end -->

### {texts["events"]}

{texts["event_results"]}

<!-- python-events-example:start -->
```python
{(CLIENTS / "python" / "subclass_demo.py").read_text().rstrip()}
```
<!-- python-events-example:end -->

### {texts["methods"]}

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

{texts["event_compatibility"]}


<!-- sdk-doc:end -->
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
        # The complete English and Simplified Chinese Python references are maintained directly.
        if locale not in ("en", "zh-hans"):
            documents[CLIENTS / "python" / f"README.{locale}.md"] = python_guide(texts)
        else:
            path = CLIENTS / "python" / f"README.{locale}.md"
            content = path.read_text(encoding="utf-8")
            sdk = python_guide(texts).split("<!-- sdk-doc:start -->", 1)[1].split("<!-- sdk-doc:end -->", 1)[0]
            content = re.sub(r"(?<=<!-- sdk-doc:start -->).*?(?=<!-- sdk-doc:end -->)", lambda _: sdk, content, flags=re.S)
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
