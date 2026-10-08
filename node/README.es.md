# JumpServer PAM Node.js SDK

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

## Requisitos

- Node.js 20.3+ / ws
- `demo.js`

## Configuración y ejecución

Instale el SDK fuente y configure los valores siguientes. Autorice las cuentas para pull en Administración de aplicaciones; vincule políticas solo si necesita push o rotación. Obtenga AK/SK e ID de organización de los materiales de acceso. Sustituya los ejemplos y proteja los secretos de despliegue. Cada réplica necesita un ID estable y único. Use un único selector: ID de cuenta o key de política.

```bash
cd node
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

npm ci
node demo.js
```

Los SDK se instalan desde el código de este repositorio y aún no se han publicado en registros públicos. Sustituya /path/to/jumpserver por una ruta absoluta. Ejecute la instalación de Go y Node.js en el directorio de la aplicación o añada la dependencia Java al pom.xml de la aplicación. Sustituya las importaciones locales de los ejemplos por las importaciones de paquetes siguientes.

```bash
npm install /path/to/pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

## Solicitud y respuesta

```javascript
'use strict'

const { Client } = require('./index')

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

## Eventos y aplicación de credenciales

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

```javascript
'use strict'
const { Client } = require('./index')

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

Las API con key se conservan por compatibilidad. Use `get_account` y `confirm_event` para nuevas integraciones y actualice también Core.

## Comandos de aplicación

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

## Métodos habituales

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

## Solución de problemas

Los fallos HTTP, de red, autenticación y decodificación usan el tipo de error SDK con código y estado HTTP. Cierre flujos y clientes tras usarlos; los clones tienen ciclos de vida independientes. Reintente fallos transitorios en la aplicación y no registre secretos ni cabeceras de autenticación.

`PAMError`

Todos los SDK usan protocolo versión 1; Agent usa esquema de configuración versión 1. Ante client_upgrade_required (HTTP 426), verifique compatibilidad y actualice. Se toleran campos opcionales y notificaciones desconocidos; no aplique ni confirme políticas no soportadas. Los recibos se envían automáticamente y no prueban la aplicación de credenciales.

## Integración del Go Agent

La identidad usa app_id, app_secret, org_id e instance_id estable; la autorización sigue las políticas de la aplicación. Las rutas y acciones son locales: state_file conserva las contraseñas actuales, event_file añade eventos sin secretos, delivery define la salida y rules define archivos, plantillas y reload/restart o scripts fijos. Ante una actualización, el Agent obtiene y guarda la contraseña actual, reemplaza los archivos de forma atómica y ejecuta la acción; los fallos se reintentan. Use credentials[].key de get_accounts en rules; las claves de suscripción incluyen el ID de cuenta. rules vacío escribe un archivo por clave.

Las rules locales configuran archivos, JSON/EnvironmentFile o plantillas fiables y una acción systemd reload/restart o un ejecutable fijo. Los scripts reciben JSON por stdin, usan argumentos fijos y un tiempo límite, y verifican la aplicación antes de devolver éxito. Core no puede ampliar estas capacidades. Reinicie el Agent tras editar la configuración privada.

La configuración descargada ya incluye la identidad y la entrega del Agent. En rules, declare los IDs de las cuentas usadas por la aplicación, la actualización de configuración, la activación y la verificación de la conexión en ejecución. allow_account_switch permite usar la misma regla para la rotación A/B en ambos sentidos. credential_check es opcional para probar el nuevo acceso antes de cambiar archivos. Estado, eventos, Socket y conciliación de 300 segundos tienen valores predeterminados. rules vacío crea un archivo por credencial.

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
