# JumpServer PAM Node.js SDK

<!-- sdk-doc:start -->

## 1. Prérequis et déroulement

1. Créez l’application et autorisez les comptes à consulter (pull). Utilisez les ID de compte, pas les noms ni les ID de politique.
2. Liez une politique de notification ou rotation A/B pour la livraison automatique (push).
3. Téléchargez l’identité dans l’assistant, utilisez un `instance_id` stable et unique par réplique et protégez AK/SK.

Pour pull seul, la première consultation suffit. Push et A/B exigent des callbacks métier et une écoute. Le SDK gère protocole et résultats ; l’application change les connexions réelles. Les exemples suivent le code actuel : utilisez des builds Core/SDK compatibles. Les paquets publiés 1.0.2 ne contiennent pas toutes les nouvelles API.

- Node.js 20.3+ / ws

## 2. Installation du SDK et identité

Installez le SDK et renseignez la configuration ci-dessous. Autorisez les comptes pour le pull dans la gestion des applications ; associez des politiques seulement si le push ou la rotation est nécessaire. Récupérez AK/SK et l’identifiant d’organisation dans les données d’accès. Remplacez les valeurs d’exemple et protégez les secrets de déploiement. Chaque réplique nécessite un identifiant stable et unique. Récupérez par identifiant de compte.

Installez le code client fourni par le même Core ; les paquets 1.0.2 ne contiennent pas les callbacks gérés. Remplacez jumpserver.example.com par votre Core et conservez le répertoire jms-pam-clients, y compris le chemin local de remplacement Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/node/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
npm install ./jms-pam-clients/node
```

```javascript
const { Client } = require('@jumpserver/pam')
// ESM: import { Client } from '@jumpserver/pam'
```

### Installation locale alternative

Si le Core ne fournit pas l’API de téléchargement ou pour un déploiement hors ligne, obtenez le code compatible dans `/path/to/pam-clients` et exécutez depuis le dossier applicatif. Remplacez le chemin réel.

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

## 3. Obtenir le premier identifiant

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

## 4. Gestionnaires d’événements

Implémentez le callback de changement de compte puis démarrez l’écoute gérée. Le SDK traite les instantanés, la lecture en temps réel, la validation du compte et de sa version, les tentatives, les commandes et les résultats. Ne retournez qu’après validation de la connexion réelle, bascule du pool et fermeture des anciennes connexions. Python et Node.js utilisent des sous-classes, Go OnAccountChanged et Java AccountEventListener. L’exemple échoue tant que la bascule n’est pas implémentée.

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

Les callbacks sont exécutés en série via une file bornée ; les mises à jour échouées sont réessayées avec un délai de 1 à 30 secondes. Le succès est signalé après le callback. Une erreur d’envoi du résultat ne répète pas une action terminée dans la même écoute. Les commandes nécessitent une prise en charge acceptée ; leurs actions échouées ne sont pas répétées automatiquement. Les comptes appliqués sont signalés automatiquement et la bascule A/B remplace le compte courant. Implémentez le nettoyage des connexions révoquées. Rapporter l’usage, lire un secret ou livrer un fichier ne confirme pas le succès métier. Les anciennes API gardent la confirmation explicite. Préservez l’idempotence après redémarrage.

### Cycle de vie et arrêt

`watchEvents({signal})` / `startEvents({signal})`; `stopEvents()` / `await subscription.stop()` / `await subscription.done`

watchEvents ne bloque pas la boucle d’événements ; startEvents ne garantit pas la synchronisation initiale. Un écouteur par client. Les méthodes async sont attendues en série avec AbortSignal. close annule ; attendez stop() ou done à l’extérieur. Ne pas attendre son propre done. clone retourne Client.

### Identifiants les plus récents et panne du serveur

Le SDK interroge d’abord l’API et remplace la valeur conservée sans revenir à une ancienne version. Seuls timeout, erreur réseau ou HTTP 5xx autorisent une valeur déjà obtenue pour le même sélecteur, signalée locale et sans expiration temporelle. Clones et redémarrages du SDK démarrent sans cache. HTTP 401/403/404 ou credential_not_found (400) effacent les valeurs ; 426 et réponses malformées échouent sans repli. L’absence d’une API de métadonnées facultative ne vide pas le cache. Agent persiste les comptes déclarés dans store/services ; une consultation ponctuelle non déclarée n’est pas enregistrée automatiquement. Les révocations explicites s’appliquent avant le rafraîchissement HTTP, même hors ligne. Les snapshots push ne définissent pas tous les droits de stockage ; une réconciliation réussie vérifie les droits actuels.

- `account.fromLocal`
- `getAccount({accountId, allowLocalFallback: false})`

Avec l’écoute gérée, snapshot et credential.updated récupèrent automatiquement les identifiants actuels, remplacent la valeur puis appellent le gestionnaire. Un échec conserve la valeur précédente et retente. L’Agent récupère aussi après notification et conserve les données pendant une panne du serveur. Utilisez les appels exigeant l’API ci-dessous pour actualiser ou changer les connexions ; une valeur retenue n’est pas une version nouvellement récupérée et ne confirme pas automatiquement une rotation.

Au repos, ping est envoyé toutes les 10 secondes ; environ 30 secondes sans message déclenchent une reconnexion avec délai exponentiel de 1–30 secondes et nouvelle signature. Le snapshot rétablit l’état actuel sans rejouer l’historique.



## 5. Méthodes courantes

### Lister les comptes autorisés

Les méthodes de liste consultent le périmètre pull actuellement autorisé et renvoient les métadonnées des comptes, actifs et politiques push, sans récupérer les mots de passe ni enregistrer une lecture de secret. Les comptes sans politique sont inclus. La liste complète parcourt toutes les pages ; la méthode de page renvoie `count` et `accounts` (Go renvoie la liste et le total séparément). Les valeurs par défaut sont limit 200 et offset 0 ; limit vaut 1–500 et search accepte 128 caractères pour le nom du compte, l’utilisateur, le nom de l’actif ou l’adresse. Doublons, résultats incomplets et changements du total produisent une erreur ; relancez toute la liste si les droits changent. Pour obtenir un secret, utilisez l’ID du compte. Les exemples nécessitent le code SDK avec ces nouvelles méthodes ; la version publiée 1.0.2 ne les fournit pas encore en Python, Java ou Node.js.

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


### Informations sur l’application, la réplique et les politiques

getApplication renvoie ID, nom, organisation et nombres de comptes autorisés et de politiques visibles de l’application authentifiée. getClientInfo renvoie uniquement l’inscription, le type et les versions de la réplique actuelle. La liste renvoie toutes les politiques liées visibles, y compris désactivées ; search recherche les noms ou clés. Consultez une politique par son ID ou sa propre clé, sans utiliser les clés push des comptes. La rotation exige les droits sur les deux comptes et le compte actif ; les nombres d’abonnements ne comptent que les comptes autorisés. Ces appels ne renvoient pas de secrets et ne récupèrent ni ne confirment des identifiants. Le premier accès inscrit la réplique. Ils exigent Core et le code SDK mis à jour ; la version publiée 1.0.2 ne les contient pas encore.

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


### Rapport d’état de l’application

Signalez un instantané complet de l’état et des ID des comptes utilisés. États : `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Une liste vide efface les usages précédents. Appelez lors des changements d’état ou depuis le heartbeat de l’application. Python, Java et Node.js renvoient false en cas d’erreur réseau/API ; Go propose `TryReportStatus`. Les variantes strictes exposent les erreurs. Les arguments invalides sont rejetés. Le délai HTTP configuré s’applique. Le rapport ne récupère pas de secrets, ne confirme pas de rotation et n’invalide pas le cache. N’incluez ni secrets ni messages d’exception dans `error_code`. ClientInfo fournit le dernier rapport et sa date.

```javascript
const accepted = await client.reportAccounts([account.id])
await client.reportAccounts([])
```


### Événements de bas niveau et commandes

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

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

## 6. Vérifier l’intégration

1. Consultez un compte autorisé et testez une connexion réelle.
2. Vérifiez la synchronisation initiale et une mise à jour ; le callback doit finir avant le succès.
3. Vérifiez usage/état et résultat séparément. Consultation et rapport d’usage ne confirment pas l’application.

### Dépannage

Les erreurs HTTP, réseau, authentification et décodage utilisent le type d’erreur SDK avec code et statut HTTP. Fermez flux et clients après utilisation ; les clones ont des cycles de vie indépendants. Réessayez les pannes transitoires dans l’application et ne journalisez ni secrets ni en-têtes d’authentification.

`PAMError`

Tous les SDK utilisent le protocole version 1 ; la configuration Agent utilise le schéma version 1. Une réponse client_upgrade_required (HTTP 426) demande de vérifier la compatibilité et de mettre à jour. Les champs optionnels et notifications inconnus sont tolérés ; les politiques non prises en charge ne doivent pas être appliquées ou confirmées. Les accusés de réception sont automatiques et ne prouvent pas l’application. Les métadonnées client peuvent inclure server_capabilities. Un champ absent ou null signifie inconnu ; les nouveaux noms sont tolérés. Cette liste décrit les fonctions du serveur, pas les autorisations.

<!-- sdk-doc:end -->
