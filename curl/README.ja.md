# cURL — 利用ガイド

Use signed HTTP requests to fetch the current credentials for an application-authorized account.

## 動作要件

- Bash / cURL / OpenSSL / base64
- `demo.sh`

Bash、cURL、OpenSSL、base64 が必要です。ASSET と ACCOUNT を変更し、署名対象のクエリが cURL の送信時と同じ形式でエンコードされていることを確認してください。

## 設定と実行

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

## リクエストとレスポンス

```http
GET /api/v1/accounts/credential-client/credential/?account_id=<account-id>&instance_id=<instance-id>
```

```json
{"key":"account:<account-id>","revision":1,"asset":{"id":"<asset-id>","name":"database"},"account":{"id":"<account-id>","username":"app","secret":"<account-secret>","secret_type":"password"}}
```

The response contains the account, asset and credential revision. Secrets are returned only by the signed credential-fetch API.

## トラブルシューティング

Check identity, authorization, signature and instance ID.

## 認証情報ポリシーへの接続

以下の署名とプロトコル表は診断用の参考資料です。認証情報ポリシーは SDK メソッドで扱うか、Go pam-agent の JSON ファイル、EnvironmentFile、Unix Socket を使用してください。

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

以下のヘッダー順で HMAC-SHA256 署名を計算します。request-target にエンコード済みパスとクエリ、Digest に実際の本文の SHA-256、X-JMS-Request-ID に一意の UUID、Date に HTTP UTC 時刻を設定します。X-JMS-Client-Version、X-JMS-Protocol-Version: 1、SDK 用 X-JMS-Config-Schema-Version: 0 も必要です。API と WebSocket ではアプリケーション AK/SK と安定した一意の instance_id を使い、各リクエストと再接続で署名を再生成します。

```text
(request-target) accept date digest x-jms-request-id x-jms-org
x-jms-client-version x-jms-protocol-version x-jms-config-schema-version
```

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

received はイベントを読み取ったことだけを示します。event_id を持つ業務イベントの処理前に、同じ WebSocket で received を送信します。snapshot と pong は受領通知不要です。接続・再接続のたびに快照を同期し、credential.secret.updated / credential.switch.published / credential.refresh.requested、権限取消、設定変更を処理します。
