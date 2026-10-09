# JumpServer PAM Node.js SDK

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

## Prérequis

- Node.js 20.3+ / ws

## Configuration et exécution

Installez le SDK et renseignez la configuration ci-dessous. Autorisez les comptes pour le pull dans la gestion des applications ; associez des politiques seulement si le push ou la rotation est nécessaire. Récupérez AK/SK et l’identifiant d’organisation dans les données d’accès. Remplacez les valeurs d’exemple et protégez les secrets de déploiement. Chaque réplique nécessite un identifiant stable et unique. Récupérez par identifiant de compte.

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go est distribué par tags de module. Java et Node.js sont disponibles en archives SDK dans Release 1.0.2 ; Maven ou Node.js est nécessaire. Exécutez les commandes dans le répertoire de votre application et ajoutez la dépendance Java à pom.xml. Les exemples importent les paquets installés.

```bash
curl -fLO https://github.com/jumpserver/pam-clients/releases/download/v1.0.2/jms-pam-node.tar.gz
tar -xzf jms-pam-node.tar.gz
npm install ./node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

## Requête et réponse

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

## Événements et application des identifiants

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

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

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

## Commandes d’application

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

## Méthodes courantes

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

## Dépannage

Les erreurs HTTP, réseau, authentification et décodage utilisent le type d’erreur SDK avec code et statut HTTP. Fermez flux et clients après utilisation ; les clones ont des cycles de vie indépendants. Réessayez les pannes transitoires dans l’application et ne journalisez ni secrets ni en-têtes d’authentification.

`PAMError`

Tous les SDK utilisent le protocole version 1 ; la configuration Agent utilise le schéma version 1. Une réponse client_upgrade_required (HTTP 426) demande de vérifier la compatibilité et de mettre à jour. Les champs optionnels et notifications inconnus sont tolérés ; les politiques non prises en charge ne doivent pas être appliquées ou confirmées. Les accusés de réception sont automatiques et ne prouvent pas l’application.

## Intégration du Go Agent

L’identité utilise app_id, app_secret, org_id et un instance_id stable ; les autorisations suivent les politiques de l’application. Chemins et actions sont locaux : state_file conserve les derniers mots de passe, event_file ajoute des événements sans secrets, delivery définit la sortie et rules les fichiers, modèles et reload/restart ou scripts fixes. Après une notification, l’Agent récupère et conserve le mot de passe actuel, remplace les fichiers atomiquement puis exécute l’action ; les échecs sont retentés. Utilisez credentials[].key de get_accounts dans rules ; les clés d’abonnement incluent l’identifiant du compte. rules vide écrit un fichier par clé.

Les rules locales définissent fichiers, JSON/EnvironmentFile ou modèles fiables et une action systemd reload/restart ou un exécutable fixe. Les scripts reçoivent le JSON sur stdin, avec arguments fixes et délai limité, et vérifient l’application avant de réussir. Core ne peut pas étendre ces capacités. Redémarrez l’Agent après modification de sa configuration privée.

La configuration téléchargée contient déjà l’identité et les paramètres de livraison de l’Agent. Dans rules, indiquez les ID des comptes utilisés, la mise à jour de configuration, l’activation et la vérification de la connexion en cours. allow_account_switch permet la même règle pour la rotation A/B dans les deux sens. credential_check est facultatif pour tester le nouvel accès avant de modifier les fichiers. L’état, les événements, le Socket et la synchronisation de 300 secondes ont des valeurs par défaut. Un rules vide écrit un fichier par identifiant.

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
