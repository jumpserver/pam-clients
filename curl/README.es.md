# cURL — Guía de uso

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Requisitos

- Bash / cURL / OpenSSL / base64
- `demo.sh`

El script requiere Bash, cURL, OpenSSL y base64. Cambie ASSET y ACCOUNT y compruebe que la consulta firmada tenga exactamente la codificación enviada por cURL.

## Configuración y ejecución

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

## Solicitud y respuesta

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Solución de problemas

Check identity, authorization, signature and instance ID.

## Acceso a políticas de credenciales

Los detalles de firma y la tabla de protocolo siguientes son referencias de diagnóstico. Use métodos del SDK para las políticas de credenciales o Go pam-agent mediante archivos JSON, EnvironmentFile o Unix Socket.

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

Firme con HMAC-SHA256 en el orden de cabeceras indicado. Incluya ruta y consulta codificadas en request-target, SHA-256 del cuerpo exacto en Digest, UUID único en X-JMS-Request-ID, fecha HTTP UTC, X-JMS-Client-Version, X-JMS-Protocol-Version: 1 y X-JMS-Config-Schema-Version: 0 para SDK. API y WebSocket usan AK/SK de la aplicación y un instance_id estable y único; renueve la firma en cada solicitud o reconexión.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Obtenga la cuenta mediante `account_id`. Valide y aplique el cambio antes de informar `success` o `failed` con `event_id`. Recibir o consultar no equivale a aplicar. Los eventos y las instantáneas incluyen `event_id`, `account_id` y `account_revision`; compruebe la versión antes de aplicar. Reclame los comandos con `running` y use manejadores idempotentes.

Un recibo received solo indica que se leyó un evento. Envíe received con event_id en el mismo WebSocket antes de procesar eventos de negocio; snapshot y pong no requieren recibo. Concilie snapshot en cada conexión o reconexión, procese credential.secret.updated / credential.switch.published / credential.refresh.requested y gestione revocaciones y cambios de configuración.
