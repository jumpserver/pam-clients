# cURL — Guia de uso

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## Requisitos

- Bash / cURL / OpenSSL / base64
- `demo.sh`

O script exige Bash, cURL, OpenSSL e base64. Substitua ASSET e ACCOUNT e confira se a consulta assinada tem exatamente a codificação enviada pelo cURL.

## Configurar e executar

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

## Requisição e resposta

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## Solução de problemas

Check identity, authorization, signature and instance ID.

## Acesso a políticas de credenciais

Os detalhes de assinatura e a tabela de protocolo abaixo são referências de diagnóstico. Use métodos do SDK para políticas de credenciais ou o Go pam-agent por arquivos JSON, EnvironmentFile ou Unix Socket.

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

Assine com HMAC-SHA256 na ordem de cabeçalhos abaixo. Inclua caminho e consulta codificados em request-target, SHA-256 do corpo exato em Digest, UUID único em X-JMS-Request-ID, data HTTP UTC, X-JMS-Client-Version, X-JMS-Protocol-Version: 1 e X-JMS-Config-Schema-Version: 0 para SDKs. API e WebSocket usam AK/SK da aplicação e instance_id estável e único; renove a assinatura em cada requisição ou reconexão.

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

Busque a conta com `account_id`. Valide e aplique a mudança antes de informar `success` ou `failed` com `event_id`. Receber ou buscar não significa aplicar. Eventos e snapshots incluem `event_id`, `account_id` e `account_revision`; verifique a versão. Reivindique comandos com `running` e use operações idempotentes.

Um recibo received indica apenas que um evento foi lido. Envie received com event_id no mesmo WebSocket antes de processar eventos de negócio; snapshot e pong não exigem recibo. Reconcilie snapshot em cada conexão ou reconexão, processe credential.secret.updated / credential.switch.published / credential.refresh.requested e trate revogações e alterações de configuração.
