# cURL — Guide d’utilisation

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Prérequis

- Bash / cURL / OpenSSL / base64
- `demo.sh`

Le script nécessite Bash, cURL, OpenSSL et base64. Remplacez ASSET et ACCOUNT et vérifiez que la requête signée utilise exactement l’encodage envoyé par cURL.

## Configuration et exécution

Set the server, application identity, organization, instance ID and account ID.

```bash
cd curl
export API_URL='https://jumpserver.example.com'
export API_KEY_ID='<app-id>'
export API_KEY_SECRET='<app-secret>'
export ORG_ID='<org-id>'

bash demo.sh
```

Set the server, application identity, organization, instance ID and account ID.

## Requête et réponse

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Dépannage

Check identity, authorization, signature and instance ID.

## Accès aux politiques d’identifiants

Les détails de signature et le tableau du protocole ci-dessous servent de référence pour le diagnostic. Utilisez les méthodes du SDK pour les politiques d’identifiants, ou Go pam-agent via des fichiers JSON, EnvironmentFile ou Unix Socket.

| HTTP | API | JSON / query |
| --- | --- | --- |
| GET | `/api/v1/accounts/credential-client/credential/` | `instance_id`, `account_id` |
| GET | `/api/v1/accounts/credential-client/accounts/` | `instance_id`, `limit`, `offset`, `search` |
| GET | `/api/v1/accounts/credential-client/application/` | `instance_id` |
| GET | `/api/v1/accounts/credential-client/client/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/status/` | `instance_id`, `status`, `account_ids`, `error_code` |
| GET | `/api/v1/accounts/credential-client/policies/` | `instance_id`, `search` |
| GET | `/api/v1/accounts/credential-client/policy/` | `instance_id`, `key` or `policy_id` |
| POST | `/api/v1/accounts/credential-client/event-result/` | `instance_id`, `event_id`, `status`, `error_code` |
| GET | `/api/v1/accounts/credential-client/commands/` | `instance_id` |
| POST | `/api/v1/accounts/credential-client/command-result/` | `instance_id`, `command_id`, `status`, `error_code` |
| WebSocket | `/ws/accounts/credential-events/` | `instance_id` |

Signez avec HMAC-SHA256 dans l’ordre des en-têtes ci-dessous. Incluez chemin et requête encodés dans request-target, SHA-256 du corps exact dans Digest, UUID unique dans X-JMS-Request-ID, date HTTP UTC, X-JMS-Client-Version, X-JMS-Protocol-Version: 1 et X-JMS-Config-Schema-Version: 0 pour les SDK. API et WebSocket utilisent AK/SK de l’application et instance_id stable et unique ; renouvelez la signature à chaque requête ou reconnexion.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Récupérez le compte via `account_id`. Validez et appliquez le changement avant de signaler `success` ou `failed` avec `event_id`. Réception et récupération ne prouvent pas l’application. Les événements et instantanés contiennent `event_id`, `account_id` et `account_revision` ; vérifiez la version. Réservez les commandes avec `running` et rendez les traitements idempotents.

Un accusé received indique uniquement qu’un événement a été lu. Envoyez received avec event_id sur le même WebSocket avant le traitement métier ; snapshot et pong ne nécessitent aucun accusé. Réconciliez snapshot à chaque connexion ou reconnexion, traitez credential.secret.updated / credential.switch.published / credential.refresh.requested et gérez révocations et changements de configuration.
