# JumpServer PAM Python SDK / Agent

Python 3.9+ fournit le SDK de politique d’identifiants. Le jms-pam-agent en Go livre les identifiants par fichiers locaux, actions fixes ou Socket local sans Python. Il fonctionne au premier plan sur Linux, macOS et Windows ; l’installation systemd intégrée est réservée à Linux.

<!-- agent-doc:start -->

## Intégration du Go Agent

[Linux x86_64 (amd64)](/download/pam/jms-pam-agent-linux-amd64) · [Linux ARM64](/download/pam/jms-pam-agent-linux-arm64) · [SHA256SUMS](/download/pam/SHA256SUMS)

```bash
# Run in the directory containing the downloaded Agent and SHA256SUMS.
case "$(uname -m)" in
  x86_64) agent_arch=amd64 ;;
  aarch64|arm64) agent_arch=arm64 ;;
  *) echo "Unsupported CPU architecture"; exit 1 ;;
esac
sha256sum --ignore-missing --check SHA256SUMS && \
sudo install -m 0755 "./jms-pam-agent-linux-${agent_arch}" /usr/local/bin/jms-pam-agent
```


L’installateur intégré nécessite Linux et root. Sur macOS, Linux sans root ou Windows, choisissez JSON ou Socket dans l’assistant et suivez les commandes init-local et run --local --config. Initialisez une fois puis réutilisez la configuration locale privée ; le mode premier plan ne réalise pas d’actions systemd.

`/etc/jms-pam-agent/agent.json`: `0600`; `state_file`, `event_file`, `delivery.delivery_root`, `delivery.socket_path`.

- JSON: `/opt/jumpserver-pam/credentials/<credential-key>.json`
- EnvironmentFile: `/opt/jumpserver-pam/credentials/<credential-key>.env`
- Unix Socket: `/run/jms-pam-agent/agent.sock`

Choisissez JSON pour les fichiers, EnvironmentFile pour un service systemd fixé à l’installation ou Unix Socket pour l’API locale. L’Agent enregistre les révisions distribuées après distribution ; l’application valide et applique avant d’enregistrer la révision appliquée. Le socket appartient à l’utilisateur configuré avec le mode 0600 ; effectuez les requêtes avec cet utilisateur.

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
      "format": "auto",
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

`config_update.format` vaut `auto` par défaut, même si ce champ est omis. Les extensions `.txt`, `.env` et `.properties` utilisent `env` (`KEY=value`) ; `.yml` et `.yaml` utilisent `yaml` (`KEY: value` au premier niveau, valeurs écrites comme chaînes entre guillemets). Pour une autre extension, précisez `env` ou `yaml`. Chaque champ doit déjà exister une seule fois ; les autres paramètres sont conservés. Pour les sections INI ou le YAML imbriqué, utilisez `files` + `template_file` pour générer tout le fichier, ou un `script`. Après modification du fichier de variables, l’application doit le relire ou redémarrer.

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```


### API locale et confirmation

```bash
curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  http://localhost/v1/health

curl --fail --silent --show-error \
  --unix-socket '/run/jms-pam-agent/agent.sock' \
  'http://localhost/v1/credentials/<credential-key>'
```

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

```bash
/usr/local/bin/jms-pam-agent confirm '<event-id>' \
  --socket '/run/jms-pam-agent/agent.sock'
```

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

### Dépannage

```bash
sudo systemctl start jms-pam-agent
sudo systemctl status jms-pam-agent --no-pager
sudo journalctl -u jms-pam-agent -n 100 --no-pager
sudo systemctl restart jms-pam-agent
```

L’Agent réconcilie au démarrage, lors d’événements pertinents et toutes les 300 secondes. Les pannes réseau conservent les derniers identifiants autorisés. Un refus d’identité ou d’autorisation bloque la lecture par socket ; une synchronisation signée réussie la rétablit. Les fichiers déjà écrits restent présents. SIGINT/SIGTERM ferment serveur, connexions et thread de lecture.

<!-- agent-doc:end -->

<!-- sdk-doc:start -->

## Intégration du SDK Python

Python 3.9+ est requis. Installez jms-pam depuis PyPI ou votre miroir interne avec le Python de l’application, puis téléchargez la configuration depuis l’assistant de connexion.

```bash
python3 -m pip install jms-pam
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
instance_id = os.environ.get("JMS_INSTANCE_ID") or "orders-node-1"
```

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


### Événements et application des identifiants

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

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

### Méthodes courantes

- `list_accounts(search="")`
- `get_application()` / `get_client_info()`
- `report_status("running", account_id=...)` / `report_status("stopped")`
- `list_policies(search="")` / `get_policy(key=...)` / `get_policy(policy_id=...)`
- `list_accounts_page(limit=200, offset=0, search="")` → `page.count`, `page.accounts`
- `get_account(account_id=..., allow_local_fallback=True)`
- `account.username` / `account.secret` / `account.asset` / `account.from_local`
- `confirm_event(event_id=..., status="success" | "failed", error_code=...)`
- `watch_credential_events(stop_event=...)`
- `list_application_commands()`
- `report_application_command_result(command_id=..., status="running")`
- `clone()` / `close()`

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

### Rapport d’état de l’application

Signalez un instantané complet de l’état et des ID des comptes utilisés. États : `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Une liste vide efface les usages précédents. Appelez lors des changements d’état ou depuis le heartbeat de l’application. Python, Java et Node.js renvoient false en cas d’erreur réseau/API ; Go propose `TryReportStatus`. Les variantes strictes exposent les erreurs. Les arguments invalides sont rejetés. Le délai HTTP configuré s’applique. Le rapport ne récupère pas de secrets, ne confirme pas de rotation et n’invalide pas le cache. N’incluez ni secrets ni messages d’exception dans `error_code`. ClientInfo fournit le dernier rapport et sa date.

```python
client.report_accounts([account.id])
client.report_accounts([])  # Clear the complete explicit usage snapshot.
client.report_status("degraded", account_ids=[account.id], error_code="db_unavailable")
```


<!-- sdk-doc:end -->
