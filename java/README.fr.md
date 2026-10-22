# JumpServer PAM Java SDK

Implémentez le callback de changement de compte puis démarrez l’écoute gérée. Le SDK traite les instantanés, la lecture en temps réel, la validation du compte et de sa version, les tentatives, les commandes et les résultats. Ne retournez qu’après validation de la connexion réelle, bascule du pool et fermeture des anciennes connexions. Python et Node.js utilisent des sous-classes, Go OnAccountChanged et Java AccountEventListener. L’exemple échoue tant que la bascule n’est pas implémentée.

## Prérequis

- JDK 11+ / Maven / Jackson

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
curl -fLO https://github.com/jumpserver/pam-clients/releases/download/v1.0.2/jms-pam-java.tar.gz
tar -xzf jms-pam-java.tar.gz
mvn -f ./java/pom.xml install
```

```xml
<dependency>
  <groupId>org.jumpserver</groupId>
  <artifactId>jms-pam</artifactId>
  <version>1.0.2</version>
</dependency>
```

```java
import org.jumpserver.pam.Client;
```

## Requête et réponse

```java
package org.jumpserver.pam;

public final class Demo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"),
                System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"),
                System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Models.Account account =
          client.getAccount(System.getenv("JMS_ACCOUNT_ID"));
      // Pass account.getUsername() / getSecret() to the connection pool.
      System.out.println(
          "Fetched revision "
              + account.getRevision()
              + "; implement application credential switching.");
    }
  }
}
```

### Lister les comptes autorisés

Les méthodes de liste consultent le périmètre pull actuellement autorisé et renvoient les métadonnées des comptes, actifs et politiques push, sans récupérer les mots de passe ni enregistrer une lecture de secret. Les comptes sans politique sont inclus. La liste complète parcourt toutes les pages ; la méthode de page renvoie `count` et `accounts` (Go renvoie la liste et le total séparément). Les valeurs par défaut sont limit 200 et offset 0 ; limit vaut 1–500 et search accepte 128 caractères pour le nom du compte, l’utilisateur, le nom de l’actif ou l’adresse. Doublons, résultats incomplets et changements du total produisent une erreur ; relancez toute la liste si les droits changent. Pour obtenir un secret, utilisez l’ID du compte. Les exemples nécessitent le code SDK avec ces nouvelles méthodes ; la version publiée 1.0.2 ne les fournit pas encore en Python, Java ou Node.js.

```java
package org.jumpserver.pam;

public final class AccountsDemo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      for (Models.AuthorizedAccount account : client.listAccounts()) {
        System.out.println(
            account.getId() + " " + account.getUsername() + " " + account.getAsset().getName());
      }
      Models.AccountsPage page = client.listAccountsPage(20, 0, "database");
      System.out.println("Matching accounts: " + page.getCount());
      // Use an authorized account.getId() with getAccount when its secret is needed.
    }
  }
}
```


### Informations sur l’application, la réplique et les politiques

getApplication renvoie ID, nom, organisation et nombres de comptes autorisés et de politiques visibles de l’application authentifiée. getClientInfo renvoie uniquement l’inscription, le type et les versions de la réplique actuelle. La liste renvoie toutes les politiques liées visibles, y compris désactivées ; search recherche les noms ou clés. Consultez une politique par son ID ou sa propre clé, sans utiliser les clés push des comptes. La rotation exige les droits sur les deux comptes et le compte actif ; les nombres d’abonnements ne comptent que les comptes autorisés. Ces appels ne renvoient pas de secrets et ne récupèrent ni ne confirment des identifiants. Le premier accès inscrit la réplique. Ils exigent Core et le code SDK mis à jour ; la version publiée 1.0.2 ne les contient pas encore.

```java
package org.jumpserver.pam;

import java.util.List;

public final class ApplicationInfoDemo {
  public static void main(String[] args) {
    Client.Options options =
        new Client.Options(
                System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
                System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID"))
            .orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Models.ApplicationInfo application = client.getApplication();
      System.out.println(
          application.getId() + " " + application.getName() + " " + application.getAccountsCount());
      Models.ClientInfo replica = client.getClientInfo();
      System.out.println(replica.getInstanceId() + " " + replica.getClientVersion());
      List<Models.CredentialPolicy> policies = client.listPolicies();
      for (Models.CredentialPolicy policy : policies) {
        System.out.println(
            policy.getKey()
                + " "
                + policy.getName()
                + " "
                + policy.isActive()
                + " "
                + policy.getStatus());
      }
      if (!policies.isEmpty()) {
        Models.CredentialPolicy policy = client.getPolicyById(policies.get(0).getId());
        System.out.println(policy.getName() + " " + policy.getAccountsCount());
      }
    }
  }
}
```


## Événements et application des identifiants

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

```java
package org.jumpserver.pam;

import org.jumpserver.pam.Models.Account;

public final class EventsDemo {
  public static void main(String[] args) {
    Client.Options options = new Client.Options(System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
        System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID")).orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Thread stop = new Thread(client::close, "jms-pam-shutdown");
      Runtime.getRuntime().addShutdownHook(stop);
      try {
        // SDK handles snapshots, fresh fetches, versions, retries and event results.
        // Pull-only code can use client.reportAccounts(List.of(accountId)); List.of() clears usage.
        client.watchEvents(new AccountEventListener() {
          public void onAccountChanged(Account account) {
            // Validate, switch the pool and release old connections before returning.
            throw new UnsupportedOperationException("Implement application account switching");
          }
          public void onAccountRevoked(String accountId) {
            throw new UnsupportedOperationException("Implement revoked account cleanup");
          }
        });
      } catch (InterruptedException error) {
        Thread.currentThread().interrupt();
      } finally { Runtime.getRuntime().removeShutdownHook(stop); }
    }
  }
}
```

Les anciennes API key restent compatibles. Utilisez `get_account` et `confirm_event` pour les nouvelles intégrations et mettez également Core à jour.

## Commandes d’application

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

## Méthodes courantes

- `getApplication() / getClientInfo()`
- `reportAccounts(accountIds) / reportStatus(status, accountId) / reportStatus(ApplicationStatus) / reportStatusStrict(report)`
- `listPolicies() / listPolicies(search) / getPolicy(key) / getPolicyById(policyId)`
- `listAccounts() / listAccounts(search)`
- `listAccountsPage(limit, offset, search)`
- `getAccount(accountId)`
- `getAccount(accountId, false)`
- `confirmEvent(eventId, status, errorCode)`
- `watchCredentialEvents()`
- `watchEvents(listener) / startEvents(listener)`
- `EventSubscription.stop() / close() / awaitTermination()`
- `listApplicationCommands()`
- `reportApplicationCommandResult(commandId, status, errorCode)`
- `executeApplicationCommand(event, handler)`
- `syncAgent(credentials, deliveredCredentials, configDigest, syncStatus, syncError)`
- `clone() / close()`

### Rapport d’état de l’application

Signalez un instantané complet de l’état et des ID des comptes utilisés. États : `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Une liste vide efface les usages précédents. Appelez lors des changements d’état ou depuis le heartbeat de l’application. Python, Java et Node.js renvoient false en cas d’erreur réseau/API ; Go propose `TryReportStatus`. Les variantes strictes exposent les erreurs. Les arguments invalides sont rejetés. Le délai HTTP configuré s’applique. Le rapport ne récupère pas de secrets, ne confirme pas de rotation et n’invalide pas le cache. N’incluez ni secrets ni messages d’exception dans `error_code`. ClientInfo fournit le dernier rapport et sa date.

```java
boolean accepted = client.reportAccounts(java.util.List.of(account.getId()));
client.reportAccounts(java.util.Collections.emptyList());
```


## Dépannage

Les erreurs HTTP, réseau, authentification et décodage utilisent le type d’erreur SDK avec code et statut HTTP. Fermez flux et clients après utilisation ; les clones ont des cycles de vie indépendants. Réessayez les pannes transitoires dans l’application et ne journalisez ni secrets ni en-têtes d’authentification.

`PAMException`

Tous les SDK utilisent le protocole version 1 ; la configuration Agent utilise le schéma version 1. Une réponse client_upgrade_required (HTTP 426) demande de vérifier la compatibilité et de mettre à jour. Les champs optionnels et notifications inconnus sont tolérés ; les politiques non prises en charge ne doivent pas être appliquées ou confirmées. Les accusés de réception sont automatiques et ne prouvent pas l’application. Les métadonnées client peuvent inclure server_capabilities. Un champ absent ou null signifie inconnu ; les nouveaux noms sont tolérés. Cette liste décrit les fonctions du serveur, pas les autorisations.

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
