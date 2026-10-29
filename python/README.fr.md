# JumpServer PAM Python SDK / PAM Agent

Python 3.9+ fournit le SDK de politique d’identifiants. Le pam-agent en Go livre les identifiants par fichiers locaux, actions fixes ou Socket local sans Python. Il fonctionne au premier plan sur Linux, macOS et Windows ; l’installation systemd intégrée est réservée à Linux.

<!-- agent-doc:start -->

# Intégration JumpServer PAM Agent

Exécutez l’PAM Agent Go autonome sur l’hôte applicatif, sans Python. Suivez installation, livraison, actions métier et vérification.

## 1. Prérequis et déroulement

1. Create an application and authorize the accounts it may retrieve. Account IDs below are JumpServer account IDs.
2. Declare accounts in store/services. Enable Account credential change notifications in the application for immediate updates to all authorized accounts; bind an A/B rotation policy only when switching is required.
3. Téléchargez la configuration d’identité et protégez les AK/SK. `instance_id` est facultatif.

## 2. Installation et démarrage

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

Exécutez dans le dossier du fichier `pam-agent.json` téléchargé. Les commandes du service Linux nécessitent root et systemd. Au premier plan, utilisez l’exécutable correspondant au système et à l’architecture.

```bash
chmod 0600 ./pam-agent.json
sudo /usr/local/bin/pam-agent install \
  --bootstrap ./pam-agent.json
sudo systemctl status pam-agent --no-pager
```

### Exécution au premier plan

Sur macOS, Windows ou Linux sans root, exécutez une fois `init-local`, puis réutilisez la configuration privée avec `run --local`. JSON et fichiers d’environnement sont pris en charge ; les actions systemd exigent le service Linux.

Nommez le binaire `pam-agent` (`pam-agent.exe` sous Windows) ; sous Linux/macOS exécutez `chmod 0755 ./pam-agent`.

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

## 3. Exemple de configuration

Cet exemple complet Linux/systemd contient l’identité de l’application, la fréquence de lecture, les fichiers standard, l’API locale et un service métier. Remplacez l’identité, l’ID du compte, l’utilisateur système, les chemins, le nom du service et le script de vérification ; déployez ce script au préalable. Pour utiliser uniquement les fichiers ou l’API, définissez `services` sur `[]`. Les sections suivantes expliquent chaque bloc du même fichier de configuration.

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

`instance_id` est facultatif. S’il est absent ou vide, PAM Agent génère `<hostname>-<4 caractères hexadécimaux aléatoires>`, par exemple `orders-pod-a1b2`. Une valeur explicite ou `--instance-id` est prioritaire. L’installation et `init-local` enregistrent l’ID dans la configuration produite. Une exécution directe le conserve dans l’état privé et le réutilise au redémarrage sans modifier la configuration d’entrée. Les réplicas K8s peuvent partager cette configuration, avec un répertoire d’état distinct et accessible en écriture par réplica. Un nouveau réplica sans état génère un nouvel ID.

`pull_interval` définit la période globale de consultation en secondes : un entier positif, 300 par défaut. L’PAM Agent se synchronise au démarrage puis vérifie les versions des comptes et des identifiants à chaque période ; il ne récupère les secrets que si la version ou le compte actif change. Les services avec `update_on_notification: false` sont mis à jour à la prochaine consultation ; les notifications ne la retardent pas.

## 4. Stockage des comptes store

`store` écrit les identifiants dans des fichiers JSON ou de variables d’environnement pour les applications qui les lisent depuis un fichier. Il définit les comptes, le répertoire, le format et le propriétaire. La mise à jour d’une configuration métier ou le rechargement d’un service se configure dans `services`.

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

| Configuration et exécution | store |
| --- | --- |
| `store.accounts: ["all"]` | All accounts authorized for this application, independently of the notification setting. |
| `store.accounts: ["<account-id>"]` | Uniquement les ID de comptes explicitement déclarés. |
| `store.format: json` | `<store.root>/<account-id>.json` |
| `store.format: environment` | `<store.root>/<account-id>.env` |

Chaque compte, y compris A et B, possède son fichier nommé par account_id. Les identifiants sont en clair, avec les permissions 0600. Un compte inactif en cours de changement de mot de passe est temporairement indisponible. La vérification des droits supprime les fichiers révoqués. Une réconciliation inchangée ne lit que les métadonnées ; les lectures réelles restent auditées et comptent pour l’observation des rotations.

## 5. API locale socket

`socket` configure le Socket Unix local pour la santé, la liste des comptes, la lecture des identifiants, le suivi d’utilisation et la confirmation des résultats métier. Il fonctionne sans `store`. Les droits de lecture proviennent de l’autorisation de l’application dans Core ; `store.accounts` ne sélectionne que les fichiers et ne limite pas la portée de l’API.

`path` vaut par défaut `/run/jms-pam-agent/agent.sock`. `owner` contrôle l’accès au Socket et le propriétaire par défaut des sorties métier ; à défaut, il utilise `store.owner`, puis l’utilisateur PAM Agent. Omettre `socket` conserve l’interface avec ses valeurs par défaut. `init-local` utilise `<directory>/run/agent.sock` et l’utilisateur courant.

Sans root, omettez `store.owner` et `socket.owner` pour utiliser l’utilisateur courant ; `init-local` définit automatiquement les deux. Avec root, indiquez l’utilisateur métier s’il doit lire les fichiers ou accéder au Socket. `owner` ne change pas l’utilisateur du processus PAM Agent.

```json
{
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  }
}
```

### Consulter les comptes et identifiants

Utilisez l’utilisateur applicatif pour CLI et Socket. `get_accounts` liste les métadonnées ; `get_secret` consulte aussi les comptes pull uniquement. Le mot de passe va sur stdout : utilisez un terminal privé. Adaptez le Socket en mode local.

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

### Confirmer la prise en compte

Confirmez après validation et bascule de la connexion réelle. Utilisez `app_check` avec `confirm_on_success: true` ou une confirmation explicite. N’appelez pas cette API depuis un contrôle synchrone. Le résultat est conservé : `confirmed` signifie accepté par Core ; `pending` sera réessayé.

```bash
pam-agent confirm '<event-id>' --socket '/run/jms-pam-agent/agent.sock'
```

## 6. Mises à jour de l’application service

Ajoutez les objets ci-dessous à `services`, avec un `name` unique et stable. `allow_account_switch`, au niveau service, vaut false par défaut : true suit le compte actif d’une association A/B non ambiguë ; les autres comptes restent fixes. Déclarer A suffit. `update_on_notification` est aussi au niveau service et vaut true par défaut. False reporte les actions à la prochaine réconciliation, toutes les 300 secondes par défaut ; stockage et révocations restent actifs. Chaque service mémorise ses versions. La rotation n’est confirmée automatiquement qu’après validation de tous les services concernés avec `app_check.confirm_on_success: true`.

Les configurations utilisent `update_config` → `reload_service` → `app_check` : modifier le fichier, recharger/redémarrer le service (ou exécuter un script fixe), puis vérifier l’application en cours. Un PAM Agent mis à jour est nécessaire. `services` accepte uniquement ces trois noms d’étapes ; les anciens noms sont refusés.

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

### accounts

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

### update_config

Chaque objet `services` décrit un service métier. Choisissez une seule méthode `update_config` : `file + fields_map` modifie les champs existants, `files + template_file` génère des fichiers complets, `script` exécute une mise à jour personnalisée. Remplacez le bloc du service par un fragment ci-dessous.

#### file + fields_map

`update_config.format` vaut `auto` par défaut, même si ce champ est omis. Les extensions `.txt`, `.env` et `.properties` utilisent `env` (`KEY=value`) ; `.yml` et `.yaml` utilisent `yaml` (`KEY: value` au premier niveau, valeurs écrites comme chaînes entre guillemets). Pour une autre extension, précisez `env` ou `yaml`. Chaque champ doit déjà exister une seule fois ; les autres paramètres sont conservés. Pour les sections INI ou le YAML imbriqué, utilisez `files` + `template_file` pour générer tout le fichier, ou un `script`. Après modification du fichier de variables, l’application doit le relire ou redémarrer.

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

Les modèles génèrent des fichiers complets, notamment JSON imbriqué ou INI. Utilisez `files[].format: template` et un chemin absolu `template_file`. Le rendu précède le remplacement atomique en mode 0600 ; `owner` utilise `socket.owner` par défaut. La fonction `json` gère les guillemets et échappements JSON.

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

Les clés de `.Credentials` dans le modèle et `credentials` dans le script sont les `services.accounts[].id` déclarés, pas les alias. Remplacez `<primary-account-id>`. En rotation A/B, la clé reste l’ID A déclaré ; `AccountID` (`account_id` dans le script) indique le compte actif.

#### script

Déclarez une sortie avec `target` ou plusieurs avec `targets` pour la sauvegarde avant modification. Déployez `script.path` comme exécutable local. `args` contient des paramètres fixes ; les identifiants arrivent en JSON sur stdin. `timeout_seconds` accepte 1–300 secondes, par défaut 120. Le script utilise l’utilisateur PAM Agent. Le code 0 signifie succès ; un autre code ou un délai dépassé provoque un échec et une nouvelle tentative, exigeant un traitement idempotent.

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

Exemple d’entrée standard (extrait) :

```json
{
  "event": "credential.updated",
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

Cet exemple Python 3 pour Linux/macOS modifie database dans un JSON existant, préserve les autres paramètres et le propriétaire, puis remplace atomiquement le fichier en mode 0600. Préparez le répertoire et le JSON initial, déployez le script et accordez le droit d’exécution. Le script gère format, droits et propriétaire.

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

Le succès du modèle ou du script signifie seulement la mise à jour du fichier. Configurez `reload_service` et `app_check` dans le même service si nécessaire. Vérifiez la configuration avant de redémarrer PAM Agent :

```bash
sudo pam-agent check-config --config /etc/jms-pam-agent/agent.json
sudo systemctl restart pam-agent
```


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

## 7. Vérifier l’intégration

1. Verify the instance is online and its authorized account scope is correct.
2. Enable application notifications, change an authorized account credential and check its version in the delivered file or Socket. Inspect per-instance receipts and results in the application event history.
3. For A/B switching, verify that the running application uses the new account and releases old connections before confirming success. Receipt, fetch and delivery do not prove business success.

### Dépannage

```bash
sudo systemctl status pam-agent --no-pager
sudo journalctl -u pam-agent -n 100 --no-pager
```

PAM Agent synchronise au démarrage, sur notification pertinente et périodiquement, toutes les 300 secondes par défaut. Une panne temporaire conserve les valeurs autorisées. Une révocation explicite ou désactivation bloque la lecture locale et supprime les fichiers standard, même si le rafraîchissement échoue. Les fichiers métier restent sous contrôle de l’administrateur. Les notifications ne repoussent pas la réconciliation périodique. SIGINT/SIGTERM ferme le serveur et la lecture des événements.


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

Le SDK interroge d’abord l’API et remplace la valeur conservée sans revenir à une ancienne version. Seuls timeout, erreur réseau ou HTTP 5xx autorisent une valeur déjà obtenue pour le même sélecteur, signalée locale et sans expiration temporelle. Clones et redémarrages du SDK démarrent sans cache. HTTP 401/403/404 ou credential_not_found (400) effacent les valeurs ; 426 et réponses malformées échouent sans repli. L’absence d’une API de métadonnées facultative ne vide pas le cache. PAM Agent persiste les comptes déclarés dans store/services ; une consultation ponctuelle non déclarée n’est pas enregistrée automatiquement. Les révocations explicites s’appliquent avant le rafraîchissement HTTP, même hors ligne. Les snapshots push ne définissent pas tous les droits de stockage ; une réconciliation réussie vérifie les droits actuels.

- `account.from_local`
- `get_account(account_id=..., allow_local_fallback=False)`

Avec l’écoute gérée, snapshot et credential.updated récupèrent automatiquement les identifiants actuels, remplacent la valeur puis appellent le gestionnaire. Un échec conserve la valeur précédente et retente. L’PAM Agent récupère aussi après notification et conserve les données pendant une panne du serveur. Utilisez les appels exigeant l’API ci-dessous pour actualiser ou changer les connexions ; une valeur retenue n’est pas une version nouvellement récupérée et ne confirme pas automatiquement une rotation.




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

Tous les SDK utilisent le protocole version 1 ; la configuration PAM Agent utilise le schéma version 1. Une réponse client_upgrade_required (HTTP 426) demande de vérifier la compatibilité et de mettre à jour. Les champs optionnels et notifications inconnus sont tolérés ; les politiques non prises en charge ne doivent pas être appliquées ou confirmées. Les accusés de réception sont automatiques et ne prouvent pas l’application. Les métadonnées client peuvent inclure server_capabilities. Un champ absent ou null signifie inconnu ; les nouveaux noms sont tolérés. Cette liste décrit les fonctions du serveur, pas les autorisations.

<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# Application integration overview

## Authorization, notifications and rotation

1. **Account authorization** controls which accounts the application may fetch on demand. Authorize the application before installing a client.
2. **Account credential change notifications** is an application-level switch covering all authorized accounts. Use it when applications should react to password changes; no subscription policy is required.
3. **A/B rotation** is a separately bound policy for switching between two accounts before changing the original password. Both accounts must be authorized. Open the policy’s operation guide for its cycle, waiting conditions, cancellation and recovery.

## Choose an integration

| Method | Use when | Start here |
| --- | --- | --- |
| SDK | You can change the application code and let it connect to JumpServer. | Select SDK integration and your programming language. Implement a managed account-change callback for automatic updates. |
| PAM Agent | You need local credential files, business configuration updates or a local Socket, and want to keep JumpServer keys outside business code. | Select Agent integration, install the Agent and configure store, socket and services as needed. |
| Socket API | Business-managed connections through the local Agent; no JumpServer SDK or AK/SK in the business. | Socket API access; declare a service with delivery: socket. |

## Setup and acceptance

1. Create an application, authorize accounts and download its private identity configuration.
2. Choose SDK or PAM Agent, install the matching client, fetch an account and verify a real connection.
3. For automatic updates, enable application notifications and implement the business change. For A/B switching, also bind a rotation policy and enable following its current account.
4. Verify one account update or A/B switch: the application uses the new credential, old connections are released and the business result is successful. Reception, fetching and file delivery alone are insufficient.

## Where to find the details

Agent integration covers deployment, files, local APIs and business actions. SDK integration covers installation, managed callbacks and language-specific APIs. Events and commands is the shared reference for event payloads, snapshots, receipts and administrator commands. The operation guide inside each rotation policy explains its own cycle, blockers and recovery.

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# Events and commands

This reference applies to all SDK languages and PAM Agent. Prefer managed SDK callbacks or configured Agent services for normal integration; use the raw protocol only when implementing an event consumer yourself.

## Event responsibilities

| Event | Responsibility |
| --- | --- |
| credential.updated and snapshot items | Fetch the named account, verify its version, apply it and report the business result. Managed SDK callbacks handle the protocol. |
| app.scope.updated | Reconcile scope and remove invalid state. Removing a rotation binding does not revoke account authorization. |
| `rotation.*` (such as `rotation.started` and `rotation.completed`), `credential.change.*` (such as `credential.change.started` and `credential.change.completed`) | Update progress, logs or alerts. Do not repeat credential application or confirm business success for these notifications. |
| credential.switch.requested and app.restart.requested | Administrator commands for selected instances: claim, execute and report the command result. |

## Complete event catalog

Every Core application event, administrator command and connection control message is listed below, with compatibility entries separated. This is the complete reference; rotation details explain how the events relate to that policy workflow.

### Credential and scope events

credential.updated applies credentials. app.scope.updated and snapshot reconcile scope: detach a missing rotation policy while preserving use of authorized accounts. Only actual account authorization removal disables local access, clears retained credentials and asks the application to release connections.

| Event code | Meaning |
| --- | --- |
| `credential.updated` | Current credentials or published account changed; fetch and apply the named account. |
| `app.scope.updated` | Account authorization, notification settings or rotation bindings changed. Use authorized_account_ids to determine account access, then snapshot to reconcile notifications and rotation following. Removing a binding must not close an otherwise authorized account connection. |

### General password-change events

General task lifecycle notifications for progress and alerts. Do not fetch again or report application success for these notifications; apply credentials on credential.updated or snapshot.

| Event code | Meaning |
| --- | --- |
| `credential.change.started` | A password-change task is queued or running; the remote password may not have changed yet. |
| `credential.change.completed` | The password-change task succeeded; this is not proof that applications applied the credentials. |
| `credential.change.failed` | The password change failed or could not be verified; inspect the task and remote account. |

### Rotation progress events

These notifications explain cycle progress and outcomes. Application switching follows credential.updated or snapshot, not a second action on each progress event.

| Event code | Meaning |
| --- | --- |
| `rotation.verify.started` | Backup-account login verification started. |
| `rotation.verify.skipped` | Backup-account login verification skipped by credential policy. |
| `rotation.verify.failed` | Backup verification failed; no switch is published. |
| `rotation.verify.cancelled` | Backup verification was cancelled. |
| `rotation.started` | The rotation cycle has published the target account and started switching. |
| `rotation.waiting_for_app` | Waiting for required application instances to apply and confirm the target. |
| `rotation.source.waiting` | Observing successful secret fetches for the original account after publication. |
| `rotation.source.ready` | The original account no-fetch window and required client conditions allow password change. |
| `rotation.completed` | The original account password change was verified and the rotation completed. |
| `rotation.failed` | Rotation entered a password-change failure or recovery-required state. |
| `rotation.force_stopped` | An administrator ended the cycle without waiting for remaining recovery confirmations. |

### Administrator command events

Claim with running, execute only when accepted is true, then report the command result. These are independent of automatic rotation progress.

| Event code | Meaning |
| --- | --- |
| `credential.switch.requested` | Apply the already published account and version on selected instances; this does not start a rotation. |
| `app.restart.requested` | Execute the supported application restart action and verify health. |

### Connection and receipt messages

SDK and Agent manage liveness and receipts. snapshot and pong need no received receipt; snapshot credential items are applied separately.

| Event code | Meaning |
| --- | --- |
| `snapshot` | Core sends the current notification scope and authorized account IDs on connection, reconnection or scope changes. |
| `ping` | The client sends an application-level keepalive to Core. |
| `pong` | Core replies to an application-level ping. |
| `received` | The client acknowledges reading an event; it does not report business success. |

### Compatibility events

Legacy event names and old preparation-cycle entries, not a workflow for new integrations. Current Core no longer emits credential.revoked; new rotation cycles verify the backup before publishing the switch.

| Event code | Meaning |
| --- | --- |
| `credential.revoked` | Retired: current Core no longer sends this event. Use app.scope.updated and snapshot; retained only for older Core integrations and historical records. |
| `configuration.updated` | Legacy event name, normalized to `app.scope.updated`. |
| `rotation.preparation.started` | Legacy preparation began aligning applications to the current account. |
| `rotation.accounts.aligned` | Applications finished alignment in the legacy preparation flow. |
| `rotation.standby.waiting` | Legacy preparation is observing secret fetches for the standby account. |
| `rotation.preparation.ready` | Legacy preparation is ready; no switch or password change is implied. |
| `rotation.preparation.cancelled` | Legacy preparation was cancelled. |
| `rotation.verification.started` | Legacy event name, normalized to `rotation.verify.started`. |
| `rotation.verification.failed` | Legacy event name, normalized to `rotation.verify.failed`. |
| `rotation.verification.cancelled` | Legacy event name, normalized to `rotation.verify.cancelled`. |
| `application.scope.updated` | Legacy event name, normalized to `app.scope.updated`. |
| `application.restart.requested` | Legacy event name, normalized to `app.restart.requested`. |
| `rotation.waiting_for_application` | Legacy event name, normalized to `rotation.waiting_for_app`. |
| `rotation.verification.skipped` | Legacy event name, normalized to `rotation.verify.skipped`. |

## Payload fields

| Field | Meaning |
| --- | --- |
| event_id | Event identity for receipts and application results. snapshot itself has no receipt; credential items can have their own event_id. |
| event / occurred_at | Event type and publication time. |
| account_id / account_revision | Concrete target account and password version. A fresh account revision must match before application. |
| credential_key / revision | Policy publication metadata for A/B rotation. Ordinary account notifications use account revisions. Do not mix the two version domains. |
| credential_mode | alternating_rotation for an A/B policy; subscription for application-level account notifications, without a subscription policy. Application-wide events may use null. |
| account_switch.account_ids | Accounts associated with an A/B switch. |
| operation_id | Correlates events belonging to one change operation or rotation cycle. |
| command_id / expires_at | Administrator command identity and execution deadline. Commands have a separate result API. |

## Snapshots and authorization

Core envoie snapshot à la connexion, à la reconnexion et lors des changements de périmètre. credentials contient tous les comptes autorisés si les notifications sont activées, ainsi que les politiques A/B liées, actives et autorisées. Traitez chaque élément comme credential.updated avec event_id, account_id et account_revision. authorized_account_ids décrit les autorisations ; get_accounts fournit les métadonnées. Délier une politique ne révoque pas les droits du compte.

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## Receipts and business results

Le SDK tente automatiquement d’envoyer un accusé received avant de transmettre un événement métier. L’application n’a pas à le renvoyer. Il indique uniquement la lecture, pas l’application des identifiants, et ne remplace pas la confirmation. Réconciliez les snapshots initiaux et de reconnexion ; snapshot et pong ne nécessitent aucun accusé.

```json
{"event": "received", "event_id": "<event-id>"}
```

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

## Administrator commands

Administrators send commands from the application’s Send event action and choose instances and an expiry time.

- credential.switch.requested asks the instance to apply the rotation policy’s **already published** account and version. It does not start a rotation or select another account.
- app.restart.requested asks the application to run its supported restart action and verify health. The SDK application must implement that handler; Agent support depends on its local service configuration.

Managed SDK handlers claim and report commands automatically. Raw consumers use the signed client API:

1. GET /api/v1/accounts/credential-client/commands/ with instance_id to poll pending commands; connected instances can also receive them over WebSocket.
2. POST /api/v1/accounts/credential-client/command-result/ with instance_id, command_id and status: running. Execute only if accepted is true.
3. After actual application switching or a successful restart health check, report status: success through the same command-result API; on failure report failed with a safe error_code. A received receipt or confirm_event alone does not replace command-result reporting.

Expired commands cannot be claimed. Duplicate delivery must not repeat a claimed action. Failed command actions are not automatically repeated; inspect the application before sending a new restart request.

## Connection liveness and compatibility

Au repos, ping est envoyé toutes les 10 secondes ; environ 30 secondes sans message déclenchent une reconnexion avec délai exponentiel de 1–30 secondes et nouvelle signature. Le snapshot rétablit l’état actuel sans rejouer l’historique.

Current names are app.scope.updated, app.restart.requested, rotation.verify.started/failed/cancelled and rotation.waiting_for_app. Old application prefixes, rotation.verification names and configuration.updated normalize to the new names without changing event IDs. credential.revoked is no longer emitted and remains only for older Core integrations and historical records.

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Intégration Socket API

L’application utilise le PAM Agent local sans SDK JumpServer ni AK/SK dans le processus métier. Le transport est un Unix Domain Socket, pas TCP. La référence complète du protocole en anglais figure ci-dessous.

## 1. Unix Socket / HTTP / WebSocket

The transport is a Unix Domain Socket (AF_UNIX), not TCP. Connect to its filesystem path; no IP address or port is opened. HTTP/JSON requests and the WebSocket event channel share this socket. The localhost URL supplies the HTTP Host only. Agent-to-Core traffic still uses HTTPS/WSS.

## 2. Agent / services

Add a socket service to the Agent configuration. Use the same account declarations and allow_account_switch rules as file delivery. The declared A ID remains the map key while account_id identifies the active A/B account. Socket services cannot also configure update_config, reload_service, credential_check or app_check. restart_supported is an explicit opt-in for business restart commands.

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

Connect, read the initial snapshot, retrieve the delivery credentials, verify new real connections, replace business connections and release old sessions, then submit success. Receiving a notification or fetching a password never proves that the business has switched. A failed callback submits failed and is retried after the Agent backoff.

Every connection starts with the latest snapshot; this is current-state reconciliation, not historical replay. delivery contains metadata only, including delivery_id, declared-account keys and exact account/policy revisions. Revocations are delivered even during a Core outage; revoked_account_ids requires closing old sessions. With no complete credentials, a delivery may be absent while refresh is pending. Each independently applied business instance needs its own service_id. Only one WebSocket connection per service is accepted.

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.updated",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution.

## 4. Commands / status

Commands are claimed by the Agent before delegation. Restart commands appear in commands only for services with restart_supported=true. Report command.result after restart and readiness validation; success or failure is persisted and Core reporting is retried without executing the command again. A/B switch commands reuse credential delivery and its business result. Expired commands cannot be acknowledged.

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

Business status is separate from credential confirmation. Use starting, running, degraded, stopping, stopped or error and a short non-secret error_code. Account usage is updated only after business success, and the Agent aggregates all service reports. GET /v1/usage returns the aggregate. The existing POST /v1/usage replaces the instance-wide explicit account declaration; it does not acknowledge events.

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix socket ownership is the access boundary. service_id identifies a business consumer, not a separate security identity: processes able to open the same socket share the application authorization. Keep unrelated trust domains on separate Agent instances/sockets. Events omit passwords; retrieve credentials explicitly and never log them. Polling the service snapshot and posting results is supported when a language has no Unix-socket WebSocket client.

## 6. Python

The Python example uses websockets, not a JumpServer SDK. By default it only observes metadata and never acknowledges business success. Implement the Business callbacks and call run(socket_path, service_id, business) for real processing. On a new business process, initialize connections from the current delivery even when it was applied by the previous process. Persist business-side delivery IDs when crash-safe de-duplication is required.

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
