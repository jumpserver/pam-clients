# JumpServer PAM Python SDK / Agent

Python 3.9+ fornece o SDK de políticas de credenciais. O jms-pam-agent em Go entrega credenciais por arquivos locais, ações fixas ou Socket local sem Python. A execução em primeiro plano funciona em Linux, macOS e Windows; a instalação systemd integrada é exclusiva do Linux.

<!-- agent-doc:start -->

## Integração com o Go Agent

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


O instalador integrado requer Linux e root. Em macOS, Linux sem root ou Windows, escolha JSON ou Socket no assistente e siga os comandos init-local e run --local --config. Inicialize uma vez e reutilize a configuração local privada; o modo em primeiro plano não executa ações systemd.

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

Escolha JSON para arquivos, EnvironmentFile para um serviço systemd definido na instalação ou Unix Socket para a API local. O Agent registra revisões entregues após a entrega; a aplicação valida e aplica antes de registrar a revisão aplicada. O socket pertence ao usuário configurado com modo 0600; faça requisições locais como esse usuário.

As rules locais definem arquivos, JSON/EnvironmentFile ou modelos confiáveis e uma ação systemd reload/restart ou executável fixo. Scripts recebem JSON via stdin, usam argumentos fixos e prazo limitado, e verificam a aplicação antes de retornar sucesso. Core não pode ampliar essas capacidades. Reinicie o Agent após editar a configuração privada.

A configuração baixada já inclui a identidade e a entrega do Agent. Em rules, declare os IDs das contas usadas pela aplicação, a atualização da configuração, a ativação e a verificação da conexão em execução. allow_account_switch permite a mesma regra na rotação A/B em ambos os sentidos. credential_check é opcional para testar o novo acesso antes de alterar arquivos. Há valores padrão para estado, eventos, Socket e reconciliação de 300 segundos. rules vazio grava um arquivo padrão por credencial.

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


### API local e confirmação

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

### Solução de problemas

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

O Agent reconcilia ao iniciar, em eventos pertinentes e a cada 300 segundos. Falhas de rede preservam as últimas credenciais autorizadas. Rejeições de identidade ou autorização bloqueiam a leitura pelo socket; uma sincronização assinada bem-sucedida a restaura. Arquivos já gravados permanecem. SIGINT/SIGTERM encerram servidor, conexões e thread de leitura.

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Integração com o SDK Python

É necessário Python 3.9+. Instale jms-pam pelo PyPI ou espelho interno com o Python do aplicativo e baixe a configuração pelo assistente de conexão.

```bash
python3 -m pip install jms-pam
```

Coloque jms_pam_config.py junto da aplicação. client_options contém material de identidade: não o inclua no repositório nem nos logs. Use instance_id estável e único por réplica.

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

### Eventos e aplicação de credenciais

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

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

### Métodos comuns

- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

As APIs com key permanecem por compatibilidade. Novas integrações usam `get_account` e `confirm_event` e exigem atualização do Core.


<!-- sdk-doc:end -->
