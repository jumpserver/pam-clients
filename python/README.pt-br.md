# JumpServer PAM Python SDK / Agent

Python 3.9+ fornece o SDK de políticas de credenciais. O jms-pam-agent em Go entrega credenciais por arquivos locais, ações fixas ou Socket local sem Python. A execução em primeiro plano funciona em Linux, macOS e Windows; a instalação systemd integrada é exclusiva do Linux.

<!-- agent-doc:start -->

# Integração com o Go Agent

Execute o Agent Go independente no host da aplicação, sem Python. Siga instalação, entrega, ações de negócio e validação.

## 1. Pré-requisitos e fluxo

1. Crie a aplicação e autorize as contas para consulta (pull), usando IDs de conta, não nomes ou IDs de política.
2. Declare as contas em store/services; vincule políticas para notificações ou trocas A/B.
3. Baixe a identidade no assistente, use um `instance_id` estável e exclusivo por réplica e proteja AK/SK.

## 2. Instalação e inicialização

### Linux / systemd

O novo formato usa `config_version: 2` e exige atualizar Core e Agent. A versão 1 e configurações `delivery` / `rules` sem versão mantêm o comportamento anterior. Não misture formatos e atualize primeiro os binários. Core não pode adicionar caminhos executáveis locais. Reinicie Agent após editar a configuração privada.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

Execute no diretório de `jms_pam_agent.json`. O serviço Linux exige root e systemd. Troque o ID por réplica. Use o binário do sistema para execução em primeiro plano; macOS / Windows podem exigir compilação.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Execução em primeiro plano

Em macOS, Windows ou Linux sem root, execute `init-local` uma vez e reutilize a configuração privada com `run --local`. Há suporte a JSON e arquivos de ambiente; ações systemd exigem o serviço Linux.

Salve o binário como `jms-pam-agent` (`jms-pam-agent.exe` no Windows); em Linux/macOS execute `chmod 0755 ./jms-pam-agent`.

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

`store` e `services` são independentes. `store.accounts` é uma lista de strings: `["all"]` armazena todas as contas autorizadas para a aplicação; uma lista de IDs limita o armazenamento. Assinaturas fornecem notificações, mas não são necessárias para sincronizar contas autorizadas. Omita `store` para usar apenas serviços; uma lista vazia não armazena nada. Não misture `all` e IDs.

```json
{
  "config_version": 2,
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

`pull_interval` é o intervalo global de consulta em segundos: um inteiro positivo, com padrão 300. O Agent sincroniza ao iniciar e depois verifica as versões de contas e credenciais a cada intervalo; só obtém segredos quando a versão ou a conta ativa muda. Serviços com `update_on_notification: false` são atualizados na próxima consulta; notificações não a adiam.

| Configurar e executar | store |
| --- | --- |
| `store.accounts: ["all"]` | Todas as contas autorizadas para a aplicação, mesmo sem assinatura. |
| `store.accounts: ["<account-id>"]` | Somente os IDs de contas declarados explicitamente. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

Cada conta, incluindo A e B, tem seu arquivo por account_id. As credenciais ficam em texto simples com permissões 0600. Contas inativas em alteração de senha ficam temporariamente indisponíveis. A verificação de autorização remove arquivos revogados. `local_api.owner` controla o Socket e o proprietário padrão dos arquivos de negócio; se omitido, usa `store.owner` e depois o usuário do Agent. Socket e armazenamento são independentes. Sem mudança de versão, só são lidos metadados; consultas reais continuam auditadas e contam na observação de rotação.

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
        "account_id": "<primary-account-id>"
      }
    ],
    "config_update": {
      "files": [
        {
          "path": "/etc/sample-app/database.env",
          "format": "environment",
          "owner": "appuser"
        }
      ]
    },
    "service_action": {
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

## 4. Atualização da configuração e rotação A/B

Adicione os objetos abaixo a `services`, com um `name` único e estável. `allow_account_switch` fica no serviço e o padrão é false: true acompanha a conta ativa de uma associação A/B inequívoca; outras contas permanecem fixas. Basta declarar A. `update_on_notification` também fica no serviço e o padrão é true. False adia ações até a próxima reconciliação, por padrão a cada 300 segundos; armazenamento e revogações continuam ativos. Cada serviço registra suas versões. A rotação só é confirmada após todos os serviços envolvidos passarem na verificação com `application_check.confirm_on_success: true`.

| Configurar e executar | Atualização da configuração e rotação A/B |
| --- | --- |
| `credential_check` | Opcional: valide o novo login antes de alterar arquivos. |
| `config_update` | Atualize o arquivo por campos, modelo ou script. |
| `service_action` | Execute reload/restart do serviço fixo ou um script fixo. |
| `application_check` | Valide a conexão da aplicação em execução e só então confirme. |

`services`:

```json
[
  {
    "name": "sample-app",
    "allow_account_switch": true,
    "update_on_notification": true,
    "accounts": [
      {
        "account_id": "<primary-account-id>",
        "alias": "database",
        "username": "db_app_a"
      }
    ],
    "config_update": {
      "file": "/etc/sample-app/config.yml",
      "format": "yaml",
      "fields_map": {
        "DB_USER": "database.username",
        "DB_PASSWORD": "database.secret"
      }
    },
    "service_action": {
      "unit": "sample-app.service",
      "operation": "restart"
    },
    "application_check": {
      "path": "/usr/local/libexec/jms-pam/check-running-db",
      "confirm_on_success": true
    }
  }
]
```

Uma conta aceita `username` / `secret` diretamente. Várias contas exigem aliases únicos e referências como `database.username` ou `cache.secret`. O alias permanece estável na troca A/B. O `username` opcional é apenas informativo; os valores reais vêm do Core. Todas as contas devem estar prontas antes da escrita e a ação de serviço executa uma vez. Mapeamentos ausentes ou ambíguos falham antes da alteração.

```json
{
  "name": "sample-app",
  "allow_account_switch": true,
  "update_on_notification": true,
  "accounts": [
    {
      "account_id": "<primary-account-id>",
      "alias": "database"
    },
    {
      "account_id": "<cache-account-id>",
      "alias": "cache"
    }
  ],
  "config_update": {
    "file": "/etc/sample-app/config.yml",
    "format": "yaml",
    "fields_map": {
      "DB_USER": "database.username",
      "DB_PASSWORD": "database.secret",
      "CACHE_USER": "cache.username",
      "CACHE_PASSWORD": "cache.secret"
    }
  },
  "service_action": {
    "unit": "sample-app.service",
    "operation": "restart"
  },
  "application_check": {
    "path": "/usr/local/libexec/jms-pam/check-running-db",
    "confirm_on_success": true
  }
}
```

### Formato da configuração

`config_update.format` usa `auto` por padrão, inclusive quando omitido. As extensões `.txt`, `.env` e `.properties` usam `env` (`KEY=value`); `.yml` e `.yaml` usam `yaml` (`KEY: value` no nível superior, valores gravados como strings entre aspas). Para outras extensões, informe `env` ou `yaml`. Cada campo deve existir exatamente uma vez; as demais configurações são preservadas. Para seções INI ou YAML aninhado, use `files` + `template_file` para gerar o arquivo inteiro, ou um `script`. Após atualizar o arquivo de variáveis, a aplicação precisa relê-lo ou reiniciar.

O novo formato usa `config_version: 2` e exige atualizar Core e Agent. A versão 1 e configurações `delivery` / `rules` sem versão mantêm o comportamento anterior. Não misture formatos e atualize primeiro os binários. Core não pode adicionar caminhos executáveis locais. Reinicie Agent após editar a configuração privada.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. API local e confirmação

### Consultar contas e credenciais

Use o usuário da aplicação para CLI e Socket. `get_accounts` lista metadados e `get_secret` consulta inclusive contas somente pull. A senha vai para stdout: use um terminal privado. Ajuste o Socket no modo local.

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

### Confirmar a aplicação

Confirme após validar e trocar a conexão real. Use `application_check` com `confirm_on_success: true` ou confirmação explícita. Não chame a confirmação de um script de verificação síncrono. O resultado é persistido: `confirmed` indica aceite pelo Core; `pending` será repetido.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Validar a integração

1. Confira a instância online e as contas autorizadas.
2. Para push, envie uma atualização pela política e verifique conta e versão entregues.
3. Em A/B, verifique a conexão nova e a liberação das antigas antes do resultado. Consulta e entrega não comprovam aplicação.

### Solução de problemas

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent sincroniza ao iniciar, por notificações relevantes e periodicamente, por padrão a cada 300 segundos. Falhas temporárias preservam valores autorizados. Revogação explícita ou desativação bloqueia a leitura local e remove arquivos padrão mesmo se a atualização falhar. Arquivos de negócio permanecem sob controle do administrador. Notificações não adiam a reconciliação periódica. SIGINT/SIGTERM encerra o servidor e a leitura de eventos.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Integração com o SDK Python

## 1. Pré-requisitos e fluxo

1. Crie a aplicação e autorize as contas para consulta (pull), usando IDs de conta, não nomes ou IDs de política.
2. Vincule uma política de notificação ou rotação A/B para entrega automática (push).
3. Baixe a identidade no assistente, use um `instance_id` estável e exclusivo por réplica e proteja AK/SK.

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

O SDK consulta primeiro a API e atualiza o valor retido sem voltar a versões anteriores. Apenas timeout, erro de rede ou HTTP 5xx permitem um valor anterior do mesmo seletor, marcado como local e sem expiração temporal. Clones e reinícios do SDK começam sem cache. HTTP 401/403/404 ou credential_not_found (400) apagam valores; 426 e respostas inválidas falham sem fallback. Uma API opcional de metadados ausente não limpa o cache. Agent v2 persiste contas declaradas em store/services; uma consulta avulsa não declarada não é salva automaticamente. Revogações explícitas valem antes da atualização HTTP, mesmo offline. Snapshots push não definem toda a autorização de armazenamento; a reconciliação bem-sucedida verifica os direitos atuais.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Com escuta gerenciada ativa, snapshot e credential.updated consultam automaticamente, substituem o valor e chamam o manipulador. Falha na atualização mantém o anterior e tenta novamente. O Agent também consulta após notificações e mantém os dados durante falhas do servidor. Use as chamadas que exigem a API abaixo para atualizar ou trocar conexões; uma credencial retida não é uma versão recém-obtida nem confirma rotações automaticamente.

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

Todos os SDKs usam protocolo versão 1; Agent usa esquema de configuração versão 1. Em client_upgrade_required (HTTP 426), verifique compatibilidade e atualize. Campos opcionais e notificações desconhecidos são tolerados; não aplique nem confirme políticas não suportadas. Os recibos são automáticos e não provam aplicação de credenciais. Os metadados do cliente podem incluir server_capabilities. Ausente ou null significa desconhecido; novos nomes são tolerados. A lista descreve recursos do servidor, não permissões.

<!-- sdk-doc:end -->
