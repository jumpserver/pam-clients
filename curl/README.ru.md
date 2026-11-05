# cURL — Руководство по использованию

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Требования

- Bash / cURL / OpenSSL / base64
- `demo.sh`

Нужны Bash, cURL, OpenSSL и base64. Измените ASSET и ACCOUNT и убедитесь, что подписанный запрос закодирован точно так же, как запрос, отправляемый cURL.

## Настройка и запуск

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

## Запрос и ответ

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Устранение неполадок

Check identity, authorization, signature and instance ID.

## Подключение к политикам учётных данных

Подробности подписи и таблица протокола ниже предназначены для диагностики. Для политик учётных данных используйте методы SDK или Go pam-agent через JSON-файлы, EnvironmentFile или Unix Socket.

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

Вычисляйте подпись HMAC-SHA256 в указанном порядке заголовков. request-target включает кодированный путь и запрос, Digest содержит SHA-256 фактического тела, X-JMS-Request-ID — уникальный UUID, Date — время HTTP UTC. Укажите X-JMS-Client-Version, X-JMS-Protocol-Version: 1 и X-JMS-Config-Schema-Version: 0 для SDK. API и WebSocket используют AK/SK приложения и стабильный уникальный instance_id; обновляйте подпись для каждого запроса и переподключения.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

received означает только чтение события. Перед обработкой события с event_id отправьте received по тому же WebSocket; snapshot и pong квитанции не требуют. При каждом подключении и переподключении сверяйте снимок, обрабатывайте credential.secret.updated / credential.switch.published / credential.refresh.requested, отзыв разрешений и изменения конфигурации.
