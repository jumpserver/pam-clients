# JumpServer PAM Python SDK / Agent

Python 3.9+ fournit le SDK de politique d’identifiants. Le jms-pam-agent en Go livre les identifiants par fichiers locaux, actions fixes ou Socket local sans Python. Il fonctionne au premier plan sur Linux, macOS et Windows ; l’installation systemd intégrée est réservée à Linux.

<!-- agent-doc:start -->

# Intégration du Go Agent

Exécutez l’Agent Go autonome sur l’hôte applicatif, sans Python. Suivez installation, livraison, actions métier et vérification.

## 1. Prérequis et déroulement

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Download the identity configuration from the application access wizard. Give each replica a stable, unique instance ID and protect the AK/SK.

## 2. Installation et démarrage

### Linux / systemd

Core et Agent utilisent le protocole 1 et `config_version: 1`. Configurez `store` et `services` indépendamment, sans les mélanger avec l’ancienne structure `delivery` / `rules`. Core synchronise le périmètre et les versions, jamais les chemins exécutables locaux. Redémarrez Agent après modification de sa configuration privée.

Go 1.23+; `pam-clients/agent` + `pam-clients/go`.

```bash
cd /path/to/pam-clients/agent
go build -trimpath -o ./jms-pam-agent ./cmd/jms-pam-agent
sudo install -m 0755 ./jms-pam-agent /usr/local/bin/jms-pam-agent
```

Exécutez dans le dossier de `jms_pam_agent.json`. Le service Linux nécessite root et systemd. Changez l’ID par réplique. Utilisez le binaire du système au premier plan ; macOS / Windows peuvent nécessiter une compilation.

```bash
chmod 0600 ./jms_pam_agent.json
sudo /usr/local/bin/jms-pam-agent install \
  --bootstrap ./jms_pam_agent.json --instance-id app-node-1
sudo systemctl status jms-pam-agent --no-pager
```

### Exécution au premier plan

Sur macOS, Windows ou Linux sans root, exécutez une fois `init-local`, puis réutilisez la configuration privée avec `run --local`. JSON et fichiers d’environnement sont pris en charge ; les actions systemd exigent le service Linux.

Nommez le binaire `jms-pam-agent` (`jms-pam-agent.exe` sous Windows) ; sous Linux/macOS exécutez `chmod 0755 ./jms-pam-agent`.

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

`store` and `services` are independent. `store.accounts` is a string array: `["all"]` stores every authorized account, or list specific account IDs. Application notifications cover all authorized accounts when enabled. Local declarations control storage and business actions. On-demand access and scheduled synchronization also work with notifications disabled. Omit `store` for services-only use; an empty array stores nothing. Never combine `all` with IDs.

```json
{
  "config_version": 1,
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

`pull_interval` définit la période globale de consultation en secondes : un entier positif, 300 par défaut. L’Agent se synchronise au démarrage puis vérifie les versions des comptes et des identifiants à chaque période ; il ne récupère les secrets que si la version ou le compte actif change. Les services avec `update_on_notification: false` sont mis à jour à la prochaine consultation ; les notifications ne la retardent pas.

| Configuration et exécution | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Uniquement les ID de comptes explicitement déclarés. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |
| `local_api.socket_path` | `/run/jms-pam-agent/agent.sock` |

Chaque compte, y compris A et B, possède son fichier nommé par account_id. Les identifiants sont en clair, avec les permissions 0600. Un compte inactif en cours de changement de mot de passe est temporairement indisponible. La vérification des droits supprime les fichiers révoqués. `local_api.owner` contrôle le Socket et le propriétaire par défaut des fichiers métier ; sa valeur par défaut vient de `store.owner`, puis de l’utilisateur Agent. Le Socket est indépendant du stockage. Une réconciliation inchangée ne lit que les métadonnées ; les lectures réelles restent auditées et comptent pour l’observation des rotations.

### EnvironmentFile / systemd

EnvironmentFile écrit utilisateur, mot de passe et métadonnées en texte clair `KEY=value` dans un `.env` privé (0600). Configurez `EnvironmentFile=` dans le service systemd applicatif pour charger ces valeurs dans l’environnement du processus au démarrage. L’environnement global de l’hôte reste inchangé. Redémarrez après mise à jour ; reload ne remplace pas l’environnement du processus principal actif.

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

## 4. Mise à jour de la configuration et rotation A/B

Ajoutez les objets ci-dessous à `services`, avec un `name` unique et stable. `allow_account_switch`, au niveau service, vaut false par défaut : true suit le compte actif d’une association A/B non ambiguë ; les autres comptes restent fixes. Déclarer A suffit. `update_on_notification` est aussi au niveau service et vaut true par défaut. False reporte les actions à la prochaine réconciliation, toutes les 300 secondes par défaut ; stockage et révocations restent actifs. Chaque service mémorise ses versions. La rotation n’est confirmée automatiquement qu’après validation de tous les services concernés avec `app_check.confirm_on_success: true`.

Les configurations utilisent `update_config` → `reload_service` → `app_check` : modifier le fichier, recharger/redémarrer le service (ou exécuter un script fixe), puis vérifier l’application en cours. Un Agent mis à jour est nécessaire. `services` accepte uniquement ces trois noms d’étapes ; les anciens noms sont refusés.

| Configuration et exécution | Mise à jour de la configuration et rotation A/B |
| --- | --- |
| `credential_check` | Facultatif : validez le nouveau compte avant de modifier les fichiers. |
| `update_config` | Modifiez le fichier par champs, modèle ou script. |
| `reload_service` | Rechargez/redémarrez le service fixe ou lancez un script fixe. |
| `app_check` | Validez la connexion de l’application en cours puis confirmez. |

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

Dans `services.accounts`, utilisez `id` pour sélectionner le compte. Un compte unique accepte `username` / `secret`. Plusieurs comptes exigent des alias uniques et des références comme `database.username` ou `cache.secret`. Les alias restent stables lors des bascules A/B. Le `username` facultatif est indicatif ; les vraies valeurs viennent de Core. Tous les comptes doivent être disponibles avant l’écriture, puis l’action métier s’exécute une seule fois. Toute ambiguïté échoue avant modification.

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

### Format de configuration

`update_config.format` vaut `auto` par défaut, même si ce champ est omis. Les extensions `.txt`, `.env` et `.properties` utilisent `env` (`KEY=value`) ; `.yml` et `.yaml` utilisent `yaml` (`KEY: value` au premier niveau, valeurs écrites comme chaînes entre guillemets). Pour une autre extension, précisez `env` ou `yaml`. Chaque champ doit déjà exister une seule fois ; les autres paramètres sont conservés. Pour les sections INI ou le YAML imbriqué, utilisez `files` + `template_file` pour générer tout le fichier, ou un `script`. Après modification du fichier de variables, l’application doit le relire ou redémarrer.

Core et Agent utilisent le protocole 1 et `config_version: 1`. Configurez `store` et `services` indépendamment, sans les mélanger avec l’ancienne structure `delivery` / `rules`. Core synchronise le périmètre et les versions, jamais les chemins exécutables locaux. Redémarrez Agent après modification de sa configuration privée.

```bash
sudo jms-pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart jms-pam-agent
```


## 5. API locale et confirmation

### Consulter les comptes et identifiants

Utilisez l’utilisateur applicatif pour CLI et Socket. `get_accounts` liste les métadonnées ; `get_secret` consulte aussi les comptes pull uniquement. Le mot de passe va sur stdout : utilisez un terminal privé. Adaptez le Socket en mode local.

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

### Confirmer la prise en compte

Confirmez après validation et bascule de la connexion réelle. Utilisez `app_check` avec `confirm_on_success: true` ou une confirmation explicite. N’appelez pas cette API depuis un contrôle synchrone. Le résultat est conservé : `confirmed` signifie accepté par Core ; `pending` sera réessayé.

```bash
jms-pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Vérifier l’intégration

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Dépannage

```bash
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
```

Agent synchronise au démarrage, sur notification pertinente et périodiquement, toutes les 300 secondes par défaut. Une panne temporaire conserve les valeurs autorisées. Une révocation explicite ou désactivation bloque la lecture locale et supprime les fichiers standard, même si le rafraîchissement échoue. Les fichiers métier restent sous contrôle de l’administrateur. Les notifications ne repoussent pas la réconciliation périodique. SIGINT/SIGTERM ferme le serveur et la lecture des événements.


<!-- agent-doc:end -->

<!-- sdk-doc:start -->

# Intégration du SDK Python

## 1. Prérequis et déroulement

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Pour pull seul, la première consultation suffit. Push et A/B exigent des callbacks métier et une écoute. Le SDK gère protocole et résultats ; l’application change les connexions réelles. Les exemples suivent le code actuel : utilisez des builds Core/SDK compatibles. Les paquets publiés 1.0.2 ne contiennent pas toutes les nouvelles API.

## 2. Installation du SDK et identité

Installez le code client fourni par le même Core ; les paquets 1.0.2 ne contiennent pas les callbacks gérés. Remplacez jumpserver.example.com par votre Core et conservez le répertoire jms-pam-clients, y compris le chemin local de remplacement Go.

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/python/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
python3 -m pip install --upgrade --force-reinstall ./jms-pam-clients/python
```

### Installation locale alternative

Si le Core ne fournit pas l’API de téléchargement ou pour un déploiement hors ligne, obtenez le code compatible dans `/path/to/pam-clients` et exécutez depuis le dossier applicatif. Remplacez le chemin réel.

```bash
python3 -m pip install /path/to/pam-clients/python
```


`jms_pam_config.py` est un fichier de configuration de l’application, absent du paquet `jms-pam` sur PyPI. Téléchargez-le depuis l’assistant d’intégration et placez-le à côté du script de l’application, ou créez-le avec le contenu ci-dessous en remplaçant les valeurs par l’identité réelle de l’application. Ne placez pas `client_options` dans le dépôt ou les journaux. Utilisez un `instance_id` stable et unique par réplique.

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

## 3. Obtenir le premier identifiant

Pour un test interactif, lancez Python depuis le répertoire contenant `jms_pam_config.py`, puis exécutez l’exemple ci-dessous. Pour un script, enregistrez-le sous `app.py` dans le même répertoire et exécutez `python3 app.py`. En cas de `ModuleNotFoundError: No module named 'jms_pam_config'`, vérifiez le nom et l’emplacement du fichier ; réinstaller le paquet ne le crée pas.

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

## 4. Gestionnaires d’événements

Implémentez le callback de changement de compte puis démarrez l’écoute gérée. Le SDK traite les instantanés, la lecture en temps réel, la validation du compte et de sa version, les tentatives, les commandes et les résultats. Ne retournez qu’après validation de la connexion réelle, bascule du pool et fermeture des anciennes connexions. Python et Node.js utilisent des sous-classes, Go OnAccountChanged et Java AccountEventListener. L’exemple échoue tant que la bascule n’est pas implémentée.

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

Les callbacks sont exécutés en série via une file bornée ; les mises à jour échouées sont réessayées avec un délai de 1 à 30 secondes. Le succès est signalé après le callback. Une erreur d’envoi du résultat ne répète pas une action terminée dans la même écoute. Les commandes nécessitent une prise en charge acceptée ; leurs actions échouées ne sont pas répétées automatiquement. Les comptes appliqués sont signalés automatiquement et la bascule A/B remplace le compte courant. Implémentez le nettoyage des connexions révoquées. Rapporter l’usage, lire un secret ou livrer un fichier ne confirme pas le succès métier. Les anciennes API gardent la confirmation explicite. Préservez l’idempotence après redémarrage.

### Cycle de vie et arrêt

`watch_events(stop_event=...)` / `start_events(stop_event=...)`; `stop_events()` / `close()`

watch_events attend l’arrêt ; start_events ne garantit pas la synchronisation initiale. Attendez que l’application soit prête. Un écouteur par client ; stop_events et close attendent le gestionnaire actif. Il peut arrêter son propre client. clone réinitialise la sous-classe.

### Identifiants les plus récents et panne du serveur

Le SDK interroge d’abord l’API et remplace la valeur conservée sans revenir à une ancienne version. Seuls timeout, erreur réseau ou HTTP 5xx autorisent une valeur déjà obtenue pour le même sélecteur, signalée locale et sans expiration temporelle. Clones et redémarrages du SDK démarrent sans cache. HTTP 401/403/404 ou credential_not_found (400) effacent les valeurs ; 426 et réponses malformées échouent sans repli. L’absence d’une API de métadonnées facultative ne vide pas le cache. Agent persiste les comptes déclarés dans store/services ; une consultation ponctuelle non déclarée n’est pas enregistrée automatiquement. Les révocations explicites s’appliquent avant le rafraîchissement HTTP, même hors ligne. Les snapshots push ne définissent pas tous les droits de stockage ; une réconciliation réussie vérifie les droits actuels.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Avec l’écoute gérée, snapshot et credential.updated récupèrent automatiquement les identifiants actuels, remplacent la valeur puis appellent le gestionnaire. Un échec conserve la valeur précédente et retente. L’Agent récupère aussi après notification et conserve les données pendant une panne du serveur. Utilisez les appels exigeant l’API ci-dessous pour actualiser ou changer les connexions ; une valeur retenue n’est pas une version nouvellement récupérée et ne confirme pas automatiquement une rotation.

Au repos, ping est envoyé toutes les 10 secondes ; environ 30 secondes sans message déclenchent une reconnexion avec délai exponentiel de 1–30 secondes et nouvelle signature. Le snapshot rétablit l’état actuel sans rejouer l’historique.



## 5. Méthodes courantes

### Lister les comptes autorisés

Les méthodes de liste consultent le périmètre pull actuellement autorisé et renvoient les métadonnées des comptes, actifs et politiques push, sans récupérer les mots de passe ni enregistrer une lecture de secret. Les comptes sans politique sont inclus. La liste complète parcourt toutes les pages ; la méthode de page renvoie `count` et `accounts` (Go renvoie la liste et le total séparément). Les valeurs par défaut sont limit 200 et offset 0 ; limit vaut 1–500 et search accepte 128 caractères pour le nom du compte, l’utilisateur, le nom de l’actif ou l’adresse. Doublons, résultats incomplets et changements du total produisent une erreur ; relancez toute la liste si les droits changent. Pour obtenir un secret, utilisez l’ID du compte. Les exemples nécessitent le code SDK avec ces nouvelles méthodes ; la version publiée 1.0.2 ne les fournit pas encore en Python, Java ou Node.js.

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


### Informations sur l’application, la réplique et les politiques

get_application renvoie ID, nom, organisation et nombres de comptes autorisés et de politiques visibles de l’application authentifiée. get_client_info renvoie uniquement l’inscription, le type et les versions de la réplique actuelle. La liste renvoie toutes les politiques liées visibles, y compris désactivées ; search recherche les noms ou clés. Consultez une politique par son ID ou sa propre clé, sans utiliser les clés push des comptes. La rotation exige les droits sur les deux comptes et le compte actif ; les nombres d’abonnements ne comptent que les comptes autorisés. Ces appels ne renvoient pas de secrets et ne récupèrent ni ne confirment des identifiants. Le premier accès inscrit la réplique. Ils exigent Core et le code SDK mis à jour ; la version publiée 1.0.2 ne les contient pas encore.

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


### Rapport d’état de l’application

Signalez un instantané complet de l’état et des ID des comptes utilisés. États : `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Une liste vide efface les usages précédents. Appelez lors des changements d’état ou depuis le heartbeat de l’application. Python, Java et Node.js renvoient false en cas d’erreur réseau/API ; Go propose `TryReportStatus`. Les variantes strictes exposent les erreurs. Les arguments invalides sont rejetés. Le délai HTTP configuré s’applique. Le rapport ne récupère pas de secrets, ne confirme pas de rotation et n’invalide pas le cache. N’incluez ni secrets ni messages d’exception dans `error_code`. ClientInfo fournit le dernier rapport et sa date.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


### Événements de bas niveau et commandes

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

Utilisez list_application_commands pour interroger les demandes en attente et execute_application_command(event, handler) pour réclamer leur exécution. Seule une demande acceptée lance le handler. Celui de changement vérifie révision et compte, applique et confirme ; celui de redémarrage relance l’application et vérifie son état. Signalez le succès après la fin des opérations. Un échec du signalement conserve l’exception d’origine du handler.

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

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

## 6. Vérifier l’intégration

1. Consultez un compte autorisé et testez une connexion réelle.
2. Vérifiez la synchronisation initiale et une mise à jour ; le callback doit finir avant le succès.
3. Vérifiez usage/état et résultat séparément. Consultation et rapport d’usage ne confirment pas l’application.

### Dépannage

Le client ferme sessions HTTP et flux d’événements avec with ou close. clone crée une session indépendante. Les erreurs HTTP, réseau, authentification et décodage lèvent PAMError avec code, status_code, detail et original_error. Réessayez les erreurs transitoires dans l’application, gérez les refus d’autorisation et excluez les identifiants des journaux.

Tous les SDK utilisent le protocole version 1 ; la configuration Agent utilise le schéma version 1. Une réponse client_upgrade_required (HTTP 426) demande de vérifier la compatibilité et de mettre à jour. Les champs optionnels et notifications inconnus sont tolérés ; les politiques non prises en charge ne doivent pas être appliquées ou confirmées. Les accusés de réception sont automatiques et ne prouvent pas l’application. Les métadonnées client peuvent inclure server_capabilities. Un champ absent ou null signifie inconnu ; les nouveaux noms sont tolérés. Cette liste décrit les fonctions du serveur, pas les autorisations.

<!-- sdk-doc:end -->
