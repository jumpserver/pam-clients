# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ fornece o SDK de políticas de credenciais. O pam-agent em Go entrega credenciais por arquivos locais, ações fixas ou Socket local sem Python. A execução em primeiro plano funciona em Linux, macOS e Windows; a instalação systemd integrada é exclusiva do Linux.

<!-- agent-doc:start -->

# Integração JumpServer PAM Agent

Execute o PAM Agent Go independente no host da aplicação, sem Python. Siga instalação, entrega, ações de negócio e validação.

## 1. Pré-requisitos e fluxo

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Baixe a configuração de identidade e proteja AK/SK. `instance_id` é opcional.

## 2. Instalação e inicialização

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

Execute no diretório do arquivo `pam-agent.json` baixado. Os comandos do serviço Linux exigem root e systemd. Para execução em primeiro plano, use o executável adequado ao sistema e arquitetura.

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### Execução em primeiro plano

Em macOS, Windows ou Linux sem root, execute `init-local` uma vez e reutilize a configuração privada com `run --local`. Há suporte a JSON e arquivos de ambiente; ações systemd exigem o serviço Linux.

Salve o binário como `pam-agent` (`pam-agent.exe` no Windows); em Linux/macOS execute `chmod 0755 ./pam-agent`.

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

## 3. Exemplo de configuração

Este exemplo completo para Linux/systemd inclui identidade, intervalo de consulta, arquivos padrão, API local e um serviço de negócio. Substitua identidade, ID da conta, usuário do sistema, caminhos, nome do serviço e script de verificação; implante o script antes de iniciar. Para usar apenas arquivos ou a API, defina `services` como `[]`. As próximas seções explicam cada bloco do mesmo arquivo de configuração.

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

`instance_id` é opcional. Se ausente ou vazio, gera `<hostname>-<4 caracteres hexadecimais aleatórios>`, por exemplo `orders-pod-a1b2`. Um valor explícito ou `--instance-id` tem prioridade. A instalação e `init-local` salvam o ID na configuração produzida. A execução direta o mantém no estado privado e o reutiliza após reiniciar sem alterar a configuração de entrada. Réplicas K8s podem compartilhar a configuração, mas cada uma precisa de seu próprio diretório de estado gravável. Uma nova réplica sem estado gera outro ID.

`pull_interval` é o intervalo global de consulta em segundos: um inteiro positivo, com padrão 300. O PAM Agent sincroniza ao iniciar e depois verifica as versões de contas e credenciais a cada intervalo; só obtém segredos quando a versão ou a conta ativa muda. Serviços com `update_on_notification: false` são atualizados na próxima consulta; notificações não a adiam.

## 4. store

`store` grava credenciais em arquivos JSON ou de variáveis de ambiente para aplicações que leem arquivos. Define contas, diretório, formato e proprietário. A atualização da configuração de negócio ou recarga de serviços é configurada em `services`.

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

| Configurar e executar | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Somente os IDs de contas declarados explicitamente. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

Cada conta, incluindo A e B, tem seu arquivo por account_id. As credenciais ficam em texto simples com permissões 0600. Contas inativas em alteração de senha ficam temporariamente indisponíveis. A verificação de autorização remove arquivos revogados. Sem mudança de versão, só são lidos metadados; consultas reais continuam auditadas e contam na observação de rotação.

## 5. socket

`socket` configura o Socket Unix local para saúde, lista de contas, consulta de credenciais, relatório de uso e confirmação de resultados de negócio. Funciona sem `store`. As permissões seguem a autorização da aplicação no Core; `store.accounts` seleciona apenas arquivos e não limita o escopo da API.

`path` usa `/run/jms-pam-agent/agent.sock` por padrão. `owner` controla acesso ao Socket e o proprietário padrão das saídas de negócio; se omitido, usa `store.owner` e depois o usuário PAM Agent. Omitir `socket` mantém a interface padrão. `init-local` usa `<directory>/run/agent.sock` e o usuário atual.

Sem root, omita `store.owner` e `socket.owner` para usar o usuário atual; `init-local` configura ambos automaticamente. Com root, especifique o usuário da aplicação se ele precisar ler os arquivos ou acessar o Socket. `owner` não altera o usuário do processo PAM Agent.

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### Consultar contas e credenciais

Use o usuário da aplicação para CLI e Socket. `get_accounts` lista metadados e `get_secret` consulta inclusive contas somente pull. A senha vai para stdout: use um terminal privado. Ajuste o Socket no modo local.

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

### Confirmar a aplicação

Confirme após validar e trocar a conexão real. Use `app_check` com `confirm_on_success: true` ou confirmação explícita. Não chame a confirmação de um script de verificação síncrono. O resultado é persistido: `confirmed` indica aceite pelo Core; `pending` será repetido.

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. services

Adicione os objetos abaixo a `services`, com um `name` único e estável. `allow_account_switch` fica no serviço e o padrão é false: true acompanha a conta ativa de uma associação A/B inequívoca; outras contas permanecem fixas. Basta declarar A. `update_on_notification` também fica no serviço e o padrão é true. False adia ações até a próxima reconciliação, por padrão a cada 300 segundos; armazenamento e revogações continuam ativos. Cada serviço registra suas versões. A rotação só é confirmada após todos os serviços envolvidos passarem na verificação com `app_check.confirm_on_success: true`.

As configurações usam `update_config` → `reload_service` → `app_check`: atualizar o arquivo, recarregar/reiniciar o serviço (ou executar um script fixo) e validar a aplicação em execução. É necessário um PAM Agent atualizado. `services` aceita apenas esses três nomes de etapas; os nomes antigos são rejeitados.

| Configurar e executar | Atualização da configuração e rotação A/B |
| --- | --- |
| `credential_check` | Opcional: valide o novo login antes de alterar arquivos. |
| `update_config` | Atualize o arquivo por campos, modelo ou script. |
| `reload_service` | Execute reload/restart do serviço fixo ou um script fixo. |
| `app_check` | Valide a conexão da aplicação em execução e só então confirme. |

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

Em `services.accounts`, use `id` para selecionar a conta. Uma conta aceita `username` / `secret` diretamente. Várias contas exigem aliases únicos e referências como `database.username` ou `cache.secret`. O alias permanece estável na troca A/B. O `username` opcional é apenas informativo; os valores reais vêm do Core. Todas as contas devem estar prontas antes da escrita e a ação de serviço executa uma vez. Mapeamentos ausentes ou ambíguos falham antes da alteração.

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

Cada objeto de `services` representa um serviço. Escolha um método `update_config`: `file + fields_map` edita campos existentes, `files + template_file` gera arquivos completos ou `script` executa uma atualização personalizada. Substitua o bloco do serviço por um dos trechos abaixo.

#### file + fields_map

`update_config.format` usa `auto` por padrão, inclusive quando omitido. As extensões `.txt`, `.env` e `.properties` usam `env` (`KEY=value`); `.yml` e `.yaml` usam `yaml` (`KEY: value` no nível superior, valores gravados como strings entre aspas). Para outras extensões, informe `env` ou `yaml`. Cada campo deve existir exatamente uma vez; as demais configurações são preservadas. Para seções INI ou YAML aninhado, use `files` + `template_file` para gerar o arquivo inteiro, ou um `script`. Após atualizar o arquivo de variáveis, a aplicação precisa relê-lo ou reiniciar.

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

Templates geram arquivos completos, como JSON aninhado ou INI. Use `files[].format: template` e um caminho absoluto em `template_file`. Após renderizar, o arquivo é substituído atomicamente com modo 0600; `owner` usa `socket.owner` por padrão. A função `json` trata aspas e escapes JSON.

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

As chaves de `.Credentials` no template e `credentials` no script são os `services.accounts[].id` declarados, não aliases. Substitua `<primary-account-id>`. Na troca A/B, a chave continua sendo o ID A; `AccountID` (`account_id` no script) indica a conta ativa.

#### script

Declare uma saída com `target` ou várias com `targets` para backup antes da alteração. Implante `script.path` como executável local. `args` contém parâmetros fixos; as credenciais chegam por JSON em stdin. `timeout_seconds` aceita 1–300 segundos, padrão 120. O script usa o usuário PAM Agent. Código 0 significa sucesso; outro código ou timeout causa falha e nova tentativa, exigindo idempotência.

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

Exemplo de entrada padrão (trecho):

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

Este exemplo Python 3 para Linux/macOS atualiza database em um JSON existente, preserva outras opções e o proprietário e substitui atomicamente com modo 0600. Prepare o diretório e JSON inicial, implante o script e conceda execução. O script gerencia formato, permissões e proprietário.

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

O sucesso do template ou script apenas conclui a atualização do arquivo. Configure `reload_service` e `app_check` no mesmo serviço quando necessário. Valide antes de reiniciar PAM Agent:

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


### EnvironmentFile / systemd

EnvironmentFile grava usuário, senha e metadados em texto simples `KEY=value` num arquivo `.env` privado (0600). Configure `EnvironmentFile=` no serviço systemd da aplicação para carregar os valores no ambiente do processo ao iniciar. Não altera variáveis globais do host. Reinicie o serviço após atualizar; reload não substitui o ambiente do processo principal em execução.

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

## 7. Validar a integração

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Solução de problemas

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent sincroniza ao iniciar, por notificações relevantes e periodicamente, por padrão a cada 300 segundos. Falhas temporárias preservam valores autorizados. Revogação explícita ou desativação bloqueia a leitura local e remove arquivos padrão mesmo se a atualização falhar. Arquivos de negócio permanecem sob controle do administrador. Notificações não adiam a reconciliação periódica. SIGINT/SIGTERM encerra o servidor e a leitura de eventos.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Integração com o SDK Python

## 1. Pré-requisitos e fluxo

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Para pull, basta a primeira consulta. Push e A/B exigem callbacks de negócio e escuta. O SDK cuida do protocolo e resultados; a aplicação troca conexões reais. Os exemplos refletem o código atual: use builds Core/SDK compatíveis. Os pacotes publicados 1.0.2 não incluem todas as APIs novas.

## 2. Instalação do SDK e identidade

Instale o código do cliente incluído no mesmo Core; os pacotes 1.0.2 não incluem callbacks gerenciados. Substitua jumpserver.example.com pelo seu Core e mantenha o diretório jms-pam-clients, incluindo o caminho local de substituição do Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Instalação local alternativa

Se o Core não fornecer o endpoint de código ou para implantação offline, obtenha o código compatível em `/path/to/pam-clients` e execute no diretório da aplicação. Substitua pelo caminho real.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` é um arquivo de configuração da aplicação e não faz parte do pacote `jms-pam` no PyPI. Baixe-o pelo assistente de integração e salve-o ao lado do script da aplicação, ou crie-o com o conteúdo abaixo e substitua os valores pelos dados reais da aplicação. Não inclua `client_options` no repositório ou nos logs. Use um `instance_id` estável e único por réplica.

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

## 3. Obter a primeira credencial

Para testar interativamente, inicie Python no diretório de `jms_pam_config.py` e execute o exemplo abaixo. Para um script, salve-o como `app.py` no mesmo diretório e execute `python3 app.py`. Se ocorrer `ModuleNotFoundError: No module named 'jms_pam_config'`, verifique o nome e o local do arquivo; reinstalar o pacote não cria esse arquivo.

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

## 4. Manipuladores de eventos

Implemente o callback de mudança de conta e inicie o listener gerenciado. O SDK trata snapshots, consulta em tempo real, validação de conta e versão, tentativas, reivindicação de comandos e resultados. Retorne apenas após validar a conexão real, trocar o pool e fechar conexões antigas. Python e Node.js usam subclasses, Go usa OnAccountChanged e Java AccountEventListener. O exemplo falha até implementar a troca real.

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

Os callbacks executam em série com fila limitada; atualizações que falham tentam novamente com espera de 1–30 segundos. O sucesso só é informado após o callback. Falhas no envio do resultado não repetem uma ação concluída no mesmo listener. Comandos exigem reivindicação aceita e ações de comando que falham não se repetem automaticamente. Contas aplicadas são informadas automaticamente e a troca A/B substitui a conta atual. Implemente a limpeza das conexões revogadas. Relatar uso, consultar ou entregar arquivos não confirma sucesso da aplicação. APIs antigas mantêm confirmação explícita. Preserve a idempotência após reinícios.

### Ciclo de vida e encerramento

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events espera a parada; start_events não garante a sincronização inicial. Aguarde a aplicação estar pronta. Um listener por cliente. stop_events e close esperam o manipulador; ele pode parar seu próprio cliente. clone reinicializa a subclasse.

### Credenciais mais recentes e indisponibilidade do servidor

O SDK consulta primeiro a API e atualiza o valor retido sem voltar a versões anteriores. Apenas timeout, erro de rede ou HTTP 5xx permitem um valor anterior do mesmo seletor, marcado como local e sem expiração temporal. Clones e reinícios do SDK começam sem cache. HTTP 401/403/404 ou credential_not_found (400) apagam valores; 426 e respostas inválidas falham sem fallback. Uma API opcional de metadados ausente não limpa o cache. PAM Agent persiste contas declaradas em store/services; uma consulta avulsa não declarada não é salva automaticamente. Revogações explícitas valem antes da atualização HTTP, mesmo offline. Snapshots push não definem toda a autorização de armazenamento; a reconciliação bem-sucedida verifica os direitos atuais.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Com escuta gerenciada ativa, snapshot e credential.updated consultam automaticamente, substituem o valor e chamam o manipulador. Falha na atualização mantém o anterior e tenta novamente. O PAM Agent também consulta após notificações e mantém os dados durante falhas do servidor. Use as chamadas que exigem a API abaixo para atualizar ou trocar conexões; uma credencial retida não é uma versão recém-obtida nem confirma rotações automaticamente.

Quando ocioso, envia ping a cada 10 segundos; cerca de 30 segundos sem mensagens causam reconexão, com espera exponencial de 1–30 segundos e nova assinatura. O snapshot restaura o estado atual sem reproduzir eventos passados.



## 5. Métodos comuns

### Listar contas autorizadas

Os métodos de listagem consultam o escopo pull autorizado atual da aplicação e retornam metadados de contas, ativos e políticas push, sem buscar senhas nem registrar consultas de credenciais. Incluem contas sem políticas. A lista completa percorre todas as páginas; o método de página retorna `count` e `accounts` (Go retorna lista e total separadamente). Os padrões são limit 200 e offset 0; limit aceita 1–500 e search até 128 caracteres para nome da conta, usuário, nome do ativo ou endereço. Duplicatas, resultados incompletos e totais variáveis geram erro; repita a consulta completa se a autorização mudar. Para obter uma senha, use o ID da conta no método de consulta. Os exemplos exigem código do SDK com esses novos métodos; a versão publicada 1.0.2 ainda não os oferece em Python, Java ou Node.js.

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


### Informações da aplicação, réplica e políticas

get_application retorna ID, nome, organização e contagens de contas autorizadas e políticas visíveis da aplicação autenticada. get_client_info retorna somente registro, tipo e versões da réplica atual. A lista retorna todas as políticas vinculadas visíveis, incluindo desativadas; search filtra nomes ou chaves. Consulte uma política pelo ID ou pela chave da própria política, sem usar chaves push de contas. Políticas de rotação exigem autorização para ambas as contas e a conta ativa; contagens de assinatura incluem somente contas autorizadas. As consultas não retornam segredos nem buscam ou confirmam credenciais. O primeiro acesso registra a réplica. Exigem Core e código SDK atualizados; a versão publicada 1.0.2 ainda não contém os métodos.

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


### Relatório de estado da aplicação

Informe um retrato completo do estado e dos IDs das contas em uso. Estados: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Uma lista vazia remove o uso anterior. Chame nas mudanças de estado ou no heartbeat da aplicação. Python, Java e Node.js retornam false em falhas de rede/API; Go oferece `TryReportStatus`. Variantes estritas expõem os erros. Argumentos inválidos são rejeitados. As chamadas usam o timeout HTTP configurado. O relatório não busca segredos, confirma rotação nem invalida o cache. Não inclua segredos ou mensagens de exceção em `error_code`. ClientInfo fornece o último relatório e sua data.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Eventos de baixo nível e comandos

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

Use list_application_commands para consultar solicitações pendentes e execute_application_command(event, handler) para solicitar a execução. Apenas uma solicitação aceita executa o handler. O de troca verifica revisão e conta, aplica e confirma; o de reinício reinicia e verifica a saúde. Informe sucesso somente ao concluir. Falhas ao informar o resultado preservam a exceção original do handler.

As APIs com key permanecem por compatibilidade. Novas integrações usam `get_account` e `confirm_event` e exigem atualização do Core.

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

## 6. Validar a integração

1. Consulte uma conta autorizada e teste uma conexão real.
2. Verifique a sincronização inicial e uma atualização; o callback deve concluir a troca antes do sucesso.
3. Confira uso/status e resultado do evento separadamente. Consulta e relato de uso não confirmam aplicação.

### Solução de problemas

O cliente fecha sessões HTTP e fluxos de eventos com with ou close. clone cria uma sessão independente. Falhas HTTP, rede, autenticação e decodificação geram PAMError com code, status_code, detail e original_error. Repita erros transitórios na aplicação, trate rejeições de autorização e não registre credenciais.

Todos os SDKs usam protocolo versão 1; PAM Agent usa esquema de configuração versão 1. Em client_upgrade_required (HTTP 426), verifique compatibilidade e atualize. Campos opcionais e notificações desconhecidos são tolerados; não aplique nem confirme políticas não suportadas. Os recibos são automáticos e não provam aplicação de credenciais. Os metadados do cliente podem incluir server_capabilities. Ausente ou null significa desconhecido; novos nomes são tolerados. A lista descreve recursos do servidor, não permissões.

<!-- sdk-doc:end -->
