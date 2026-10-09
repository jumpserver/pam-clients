# JumpServer PAM Node.js SDK

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

## Требования

- Node.js 20.3+ / ws

## Настройка и запуск

Установите SDK и задайте параметры ниже. Разрешите аккаунты для pull в управлении приложениями; привязывайте политики только при необходимости push или ротации. Получите AK/SK и ID организации из материалов подключения. Замените шаблонные значения и защитите секреты развёртывания. Каждой реплике нужен стабильный уникальный ID. Получайте по ID учётной записи.

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go распространяется через теги модуля. Java и Node.js доступны в исходных архивах SDK Release 1.0.2; нужны Maven или Node.js. Выполняйте команды в каталоге приложения и добавьте зависимость Java в pom.xml. Примеры импортируют установленные пакеты.

```bash
curl -fLO https://github.com/jumpserver/pam-clients/releases/download/v1.0.2/jms-pam-node.tar.gz
tar -xzf jms-pam-node.tar.gz
npm install ./node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

## Запрос и ответ

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

## События и применение учётных данных

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

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

Старые API key сохранены для совместимости. Новые интеграции используют `get_account` и `confirm_event` и требуют обновления Core.

## Команды приложения

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

## Основные методы

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

## Устранение неполадок

Ошибки HTTP, сети, аутентификации и декодирования представлены типом ошибки SDK с кодом и HTTP-статусом. Закрывайте потоки и клиенты после использования; жизненные циклы копий независимы. Повторяйте временные сбои на уровне приложения и не записывайте секреты или заголовки авторизации.

`PAMError`

Все SDK используют протокол версии 1; конфигурация Agent — схему версии 1. Ответ client_upgrade_required (HTTP 426) требует проверки совместимости и обновления. Неизвестные необязательные поля и уведомления допустимы; неподдерживаемые политики нельзя применять или подтверждать. Квитанции отправляются автоматически и не доказывают применение учётных данных.

## Подключение Go Agent

Идентификация использует app_id, app_secret, org_id и стабильный instance_id; доступ определяется политиками приложения. Пути и действия задаются локально: state_file хранит последние пароли, event_file добавляет события без секретов, delivery задаёт вывод, rules — файлы, шаблоны и reload/restart либо фиксированные скрипты. После уведомления Agent получает и сохраняет актуальный пароль, атомарно заменяет файлы, затем выполняет действие. При сбое доставка повторяется. В rules используйте credentials[].key из get_accounts; ключ подписки включает ID аккаунта. Пустой rules записывает файл для каждого ключа.

Локальные rules задают файлы, JSON/EnvironmentFile или доверенные шаблоны и действие systemd reload/restart либо фиксированный исполняемый файл. Скрипты получают JSON через stdin, используют фиксированные аргументы и таймаут и проверяют применение перед успешным завершением. Core не расширяет эти возможности. После изменения приватной конфигурации перезапустите Agent.

Загруженная конфигурация уже содержит идентификацию Agent и параметры доставки. В rules укажите ID учётных записей приложения, обновление конфигурации, применение изменений и проверку рабочего соединения. allow_account_switch использует одно правило для ротации A/B в обоих направлениях. Необязательный credential_check проверяет новый вход до изменения файлов. Для путей состояния, событий, Socket и интервала сверки 300 секунд есть значения по умолчанию. Пустой rules создаёт файл для каждой учётной записи.

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
