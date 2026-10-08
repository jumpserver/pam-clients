# cURL — Guia de uso

Este diretório contém um script HTTP assinado para diagnosticar a API legada account-secret. Para integrar aplicações, use o SDK Python, Go, Java ou Node.js.

## Requisitos

- Bash / cURL / OpenSSL / base64
- `demo.sh`

O script exige Bash, cURL, OpenSSL e base64. Substitua ASSET e ACCOUNT e confira se a consulta assinada tem exatamente a codificação enviada pelo cURL.

## Configurar e executar

Crie uma aplicação em Gerenciamento de aplicações e autorize as contas de destino. Configure URL, AK/SK da aplicação e ID da organização; substitua todos os valores de exemplo antes de executar.

```bash
cd curl
export API_URL='https://jumpserver.example.com'
export API_KEY_ID='<app-id>'
export API_KEY_SECRET='<app-secret>'
export ORG_ID='<org-id>'

bash demo.sh
```

Os exemplos consultam o ativo ubuntu_docker e a conta root por padrão. Substitua-os por nomes reais autorizados. Execute os comandos da raiz do repositório. A saída contém segredos; não a envie aos logs da aplicação.

## Requisição e resposta

```http
GET /api/v1/accounts/integration-applications/account-secret/?asset=ubuntu_docker&account=root
```

```json
{"id":"<app-id>","secret":"<account-secret>"}
```

O campo id identifica a aplicação, não uma revisão da conta. Um segredo null pode decorrer da configuração de visualização de segredos do servidor. Estes exemplos não recebem eventos, não informam revisões aplicadas nem executam comandos de aplicação.

## Solução de problemas

Para 401, verifique AK/SK, organização, horário do host e URL assinada. Para 403, verifique o estado da aplicação e autorização das contas. Para 400, verifique seletores e codificação da consulta. Não registre segredos nem cabeçalhos Authorization. Os nomes devem corresponder aos recursos autorizados.

## Acesso a políticas de credenciais

Os detalhes de assinatura e a tabela de protocolo abaixo são referências de diagnóstico. Use métodos do SDK para políticas de credenciais ou o Go jms-pam-agent por arquivos JSON, EnvironmentFile ou Unix Socket.

| HTTP | API | JSON / query |
| --- | --- | --- |
| GET | `/api/v1/accounts/credential-client/credential/` | `instance_id`, `account_id` |
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

Um recibo received indica apenas que um evento foi lido. Envie received com event_id no mesmo WebSocket antes de processar eventos de negócio; snapshot e pong não exigem recibo. Reconcilie snapshot em cada conexão ou reconexão, processe credential.updated e trate revogações e alterações de configuração.
