# JumpServer PAM Node.js SDK

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

## Requisitos

- Node.js 20.3+ / ws

## Configurar e executar

Instale o SDK e configure os valores abaixo. Autorize contas para pull em Gerenciamento de aplicações; vincule políticas somente quando precisar de push ou rotação. Obtenha AK/SK e ID da organização nos materiais de acesso. Substitua os exemplos e proteja os segredos de implantação. Cada réplica precisa de um ID estável e único. Obtenha por ID da conta.

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go é distribuído por tags de módulo. Java e Node.js estão disponíveis como arquivos fonte do SDK no Release 1.0.2; exigem Maven ou Node.js. Execute os comandos no diretório da aplicação e adicione a dependência Java ao pom.xml. Os exemplos importam os pacotes instalados.

```bash
curl -fLO https://github.com/jumpserver/pam-clients/releases/download/v1.0.2/jms-pam-node.tar.gz
tar -xzf jms-pam-node.tar.gz
npm install ./node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

## Requisição e resposta

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

## Eventos e aplicação de credenciais

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

```javascript
'use strict'
const { Client } = require('@jumpserver/pam')

async function applyAccount(account) {
  // Validate a new connection, switch the pool, then release old connections.
  throw new Error('Implement application account switching')
}
async function applyEvent(client, event) {
  if (event.event === 'application.restart.requested')
    throw new Error('Implement application restart and health check')
  const account = await client.getAccount({ accountId: event.accountId, allowLocalFallback: false })
  if (account.revision !== event.accountRevision) throw new Error('Event account version is superseded')
  await applyAccount(account)
}
async function handleEvent(client, event) {
  if (event.commandId) {
    const claim = await client.reportApplicationCommandResult({ commandId: event.commandId, status: 'running' })
    if (!claim.accepted) return
  }
  try { await applyEvent(client, event) }
  catch (error) {
    await client.confirmEvent({ eventId: event.eventId, status: 'failed', errorCode: 'application_failed' })
    throw error
  }
  await client.confirmEvent({ eventId: event.eventId })
}
async function main() {
  const client = new Client({ endpoint: process.env.JMS_ENDPOINT, appId: process.env.JMS_APP_ID,
    appSecret: process.env.JMS_APP_SECRET, instanceId: process.env.JMS_INSTANCE_ID, orgId: process.env.JMS_ORG_ID })
  const stop = () => client.close()
  process.once('SIGINT', stop); process.once('SIGTERM', stop)
  try {
    for await (const event of client.watchCredentialEvents()) {
      const updates = event.commandId || event.event === 'credential.updated' ? [event]
        : event.event === 'snapshot' ? event.credentials || [] : []
      // Reconcile removed connections on snapshots; release revoked accounts.
      for (const update of updates) {
        try { await handleEvent(client, update) }
        catch (error) { console.error(error.code || error.name) }
      }
    }
  } finally { await client.close(); process.off('SIGINT', stop); process.off('SIGTERM', stop) }
}
if (require.main === module) main().catch((error) => { console.error(error.code || error.name); process.exitCode = 1 })
```

As APIs com key permanecem por compatibilidade. Novas integrações usam `get_account` e `confirm_event` e exigem atualização do Core.

## Comandos de aplicação

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

## Métodos comuns

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

## Solução de problemas

Falhas HTTP, rede, autenticação e decodificação usam o tipo de erro SDK com código e estado HTTP. Feche fluxos e clientes após usar; clones têm ciclos de vida independentes. Repita falhas transitórias na aplicação e não registre segredos nem cabeçalhos de autenticação.

`PAMError`

Todos os SDKs usam protocolo versão 1; Agent usa esquema de configuração versão 1. Em client_upgrade_required (HTTP 426), verifique compatibilidade e atualize. Campos opcionais e notificações desconhecidos são tolerados; não aplique nem confirme políticas não suportadas. Os recibos são automáticos e não provam aplicação de credenciais.

## Integração com o Go Agent

A identidade usa app_id, app_secret, org_id e instance_id estável; as permissões seguem as políticas da aplicação. Caminhos e ações são locais: state_file mantém as senhas atuais, event_file registra eventos sem segredos, delivery define a saída padrão e rules define arquivos, modelos e reload/restart ou scripts fixos. Ao receber uma atualização, o Agent busca e salva a senha atual, substitui os arquivos atomicamente e executa a ação; falhas são tentadas novamente. Use credentials[].key de get_accounts nas regras; chaves de assinatura incluem o ID da conta. rules vazio grava um arquivo por chave.

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
