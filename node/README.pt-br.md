# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. Pré-requisitos e fluxo

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Para pull, basta a primeira consulta. Push e A/B exigem callbacks de negócio e escuta. O SDK cuida do protocolo e resultados; a aplicação troca conexões reais. Os exemplos refletem o código atual: use builds Core/SDK compatíveis. Os pacotes publicados 1.0.2 não incluem todas as APIs novas.

- Node.js 20.3+ / ws

## 2. Instalação do SDK e identidade

Install the SDK and configure the identity below. Authorize accounts in the application for on-demand access. Enable Account credential change notifications in the application to receive changes for all authorized accounts; there is no separate account subscription policy. Bind an A/B rotation policy only when account switching is required. Each replica must use a stable, unique instance ID.

Instale o código do cliente incluído no mesmo Core; os pacotes 1.0.2 não incluem callbacks gerenciados. Substitua jumpserver.example.com pelo seu Core e mantenha o diretório jms-pam-clients, incluindo o caminho local de substituição do Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/node/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
npm install ./jms-pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

### Instalação local alternativa

Se o Core não fornecer o endpoint de código ou para implantação offline, obtenha o código compatível em `/path/to/pam-clients` e execute no diretório da aplicação. Substitua pelo caminho real.

```bash
npm install /path/to/pam-clients/node
```


```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
```

## 3. Obter a primeira credencial

`demo.js`

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const account = await client.getAccount({ accountId: process.env.JMS_ACCOUNT_ID })
    // Pass account.username / secret to the application connection pool.
    console.log(
      `Fetched revision ${account.revision}; implement application credential switching.`,
    )
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```

```bash
node demo.js
```

## 4. Manipuladores de eventos

Implemente o callback de mudança de conta e inicie o listener gerenciado. O SDK trata snapshots, consulta em tempo real, validação de conta e versão, tentativas, reivindicação de comandos e resultados. Retorne apenas após validar a conexão real, trocar o pool e fechar conexões antigas. Python e Node.js usam subclasses, Go usa OnAccountChanged e Java AccountEventListener. O exemplo falha até implementar a troca real.

`hooks.js`

```javascript
'use strict'
const { Client } = require('@jumpserver/pam')

if (typeof Client.prototype.onAccountChanged !== 'function')
  throw new Error('Upgrade @jumpserver/pam to a version with managed account events')

class ApplicationClient extends Client {
  async onAccountChanged(account, { signal }) {
    // Validate, switch the connection pool and release old connections.
    // Return only after the application uses this account. Never log secrets.
    throw new Error('Implement application account switching')
  }
  async onAccountRevoked(accountId, { signal }) {
    throw new Error('Implement revoked account cleanup')
  }
}
async function main() {
  const client = new ApplicationClient({ endpoint: process.env.JMS_ENDPOINT, appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET, instanceId: process.env.JMS_INSTANCE_ID, orgId: process.env.JMS_ORG_ID })
  const stop = () => client.close()
  process.once('SIGINT', stop); process.once('SIGTERM', stop)
  // SDK handles snapshots, fresh fetches, versions, retries and event results.
  // Pull-only code can call client.reportAccounts([accountId]); [] clears usage.
  try { await client.watchEvents() }
  finally { await client.close(); process.off('SIGINT', stop); process.off('SIGTERM', stop) }
}
if (require.main === module) main().catch((error) => { console.error(error.code || error.name); process.exitCode = 1 })
```

```bash
node hooks.js
```

Os callbacks executam em série com fila limitada; atualizações que falham tentam novamente com espera de 1–30 segundos. O sucesso só é informado após o callback. Falhas no envio do resultado não repetem uma ação concluída no mesmo listener. Comandos exigem reivindicação aceita e ações de comando que falham não se repetem automaticamente. Contas aplicadas são informadas automaticamente e a troca A/B substitui a conta atual. Implemente a limpeza das conexões revogadas. Relatar uso, consultar ou entregar arquivos não confirma sucesso da aplicação. APIs antigas mantêm confirmação explícita. Preserve a idempotência após reinícios.

### Ciclo de vida e encerramento

`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`

watchEvents não bloqueia o loop de eventos; startEvents não garante a sincronização inicial. Um listener por cliente. Métodos async são aguardados em série com AbortSignal. close cancela; aguarde stop() ou done externamente. Não aguarde seu próprio done. clone retorna Client.

### Credenciais mais recentes e indisponibilidade do servidor

O SDK consulta primeiro a API e atualiza o valor retido sem voltar a versões anteriores. Apenas timeout, erro de rede ou HTTP 5xx permitem um valor anterior do mesmo seletor, marcado como local e sem expiração temporal. Clones e reinícios do SDK começam sem cache. HTTP 401/403/404 ou credential_not_found (400) apagam valores; 426 e respostas inválidas falham sem fallback. Uma API opcional de metadados ausente não limpa o cache. Agent persiste contas declaradas em store/services; uma consulta avulsa não declarada não é salva automaticamente. Revogações explícitas valem antes da atualização HTTP, mesmo offline. Snapshots push não definem toda a autorização de armazenamento; a reconciliação bem-sucedida verifica os direitos atuais.

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

Com escuta gerenciada ativa, snapshot e credential.updated consultam automaticamente, substituem o valor e chamam o manipulador. Falha na atualização mantém o anterior e tenta novamente. O Agent também consulta após notificações e mantém os dados durante falhas do servidor. Use as chamadas que exigem a API abaixo para atualizar ou trocar conexões; uma credencial retida não é uma versão recém-obtida nem confirma rotações automaticamente.

Quando ocioso, envia ping a cada 10 segundos; cerca de 30 segundos sem mensagens causam reconexão, com espera exponencial de 1–30 segundos e nova assinatura. O snapshot restaura o estado atual sem reproduzir eventos passados.



## 5. Métodos comuns

### Listar contas autorizadas

Os métodos de listagem consultam o escopo pull autorizado atual da aplicação e retornam metadados de contas, ativos e políticas push, sem buscar senhas nem registrar consultas de credenciais. Incluem contas sem políticas. A lista completa percorre todas as páginas; o método de página retorna `count` e `accounts` (Go retorna lista e total separadamente). Os padrões são limit 200 e offset 0; limit aceita 1–500 e search até 128 caracteres para nome da conta, usuário, nome do ativo ou endereço. Duplicatas, resultados incompletos e totais variáveis geram erro; repita a consulta completa se a autorização mudar. Para obter uma senha, use o ID da conta no método de consulta. Os exemplos exigem código do SDK com esses novos métodos; a versão publicada 1.0.2 ainda não os oferece em Python, Java ou Node.js.

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const accounts = await client.listAccounts()
    for (const account of accounts) {
      console.log(account.id, account.username, account.asset.name)
    }
    const page = await client.listAccountsPage({ limit: 20, offset: 0, search: 'database' })
    console.log('Matching accounts:', page.count)
    // Use an authorized account.id with getAccount when its secret is needed.
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```


### Informações da aplicação, réplica e políticas

getApplication retorna ID, nome, organização e contagens de contas autorizadas e políticas visíveis da aplicação autenticada. getClientInfo retorna somente registro, tipo e versões da réplica atual. A lista retorna todas as políticas vinculadas visíveis, incluindo desativadas; search filtra nomes ou chaves. Consulte uma política pelo ID ou pela chave da própria política, sem usar chaves push de contas. Políticas de rotação exigem autorização para ambas as contas e a conta ativa; contagens de assinatura incluem somente contas autorizadas. As consultas não retornam segredos nem buscam ou confirmam credenciais. O primeiro acesso registra a réplica. Exigem Core e código SDK atualizados; a versão publicada 1.0.2 ainda não contém os métodos.

```javascript
'use strict'

const { Client } = require('@jumpserver/pam')

async function main() {
  const client = new Client({
    endpoint: process.env.JMS_ENDPOINT,
    appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET,
    instanceId: process.env.JMS_INSTANCE_ID,
    orgId: process.env.JMS_ORG_ID,
  })
  try {
    const application = await client.getApplication()
    console.log(application.id, application.name, application.accountsCount)
    const replica = await client.getClientInfo()
    console.log(replica.instanceId, replica.clientVersion)
    const policies = await client.listPolicies()
    for (const policy of policies) {
      console.log(policy.key, policy.name, policy.isActive, policy.status)
    }
    if (policies.length) {
      const policy = await client.getPolicy({ policyId: policies[0].id })
      console.log(policy.name, policy.accountsCount)
    }
  } finally {
    client.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error.code || error.name)
    process.exitCode = 1
  })
```


### Relatório de estado da aplicação

Informe um retrato completo do estado e dos IDs das contas em uso. Estados: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Uma lista vazia remove o uso anterior. Chame nas mudanças de estado ou no heartbeat da aplicação. Python, Java e Node.js retornam false em falhas de rede/API; Go oferece `TryReportStatus`. Variantes estritas expõem os erros. Argumentos inválidos são rejeitados. As chamadas usam o timeout HTTP configurado. O relatório não busca segredos, confirma rotação nem invalida o cache. Não inclua segredos ou mensagens de exceção em `error_code`. ClientInfo fornece o último relatório e sua data.

```javascript
const accepted = await client.reportAccounts([account.id])
await client.reportAccounts([])
```


### Eventos de baixo nível e comandos

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

As APIs com key permanecem por compatibilidade. Novas integrações usam `get_account` e `confirm_event` e exigem atualização do Core.

### API

- `getApplication({signal}) / getClientInfo({signal})`
- `reportAccounts(accountIds) / reportStatus({status, accountId, accountIds, errorCode, raiseOnError, signal})`
- `listPolicies({search, signal}) / getPolicy({key, policyId, signal})`
- `listAccounts({search, signal})`
- `listAccountsPage({limit, offset, search, signal})`
- `getAccount({accountId})`
- `getAccount({accountId, allowLocalFallback: false})`
- `confirmEvent({eventId, status, errorCode})`
- `watchCredentialEvents({signal})`
- `watchEvents({signal}) / startEvents({signal}) / stopEvents()`
- `EventSubscription.stop() / done`
- `listApplicationCommands()`
- `reportApplicationCommandResult({commandId, status, errorCode})`
- `executeApplicationCommand(event, handler)`
- `syncAgent({credentials, deliveredCredentials, ...})`
- `clone() / close()`

## 6. Validar a integração

1. Consulte uma conta autorizada e teste uma conexão real.
2. Verifique a sincronização inicial e uma atualização; o callback deve concluir a troca antes do sucesso.
3. Confira uso/status e resultado do evento separadamente. Consulta e relato de uso não confirmam aplicação.

### Solução de problemas

Falhas HTTP, rede, autenticação e decodificação usam o tipo de erro SDK com código e estado HTTP. Feche fluxos e clientes após usar; clones têm ciclos de vida independentes. Repita falhas transitórias na aplicação e não registre segredos nem cabeçalhos de autenticação.

`PAMError`

Todos os SDKs usam protocolo versão 1; Agent usa esquema de configuração versão 1. Em client_upgrade_required (HTTP 426), verifique compatibilidade e atualize. Campos opcionais e notificações desconhecidos são tolerados; não aplique nem confirme políticas não suportadas. Os recibos são automáticos e não provam aplicação de credenciais. Os metadados do cliente podem incluir server_capabilities. Ausente ou null significa desconhecido; novos nomes são tolerados. A lista descreve recursos do servidor, não permissões.

<!-- sdk-doc:end -->
