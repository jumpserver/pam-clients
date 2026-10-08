# JumpServer PAM Java SDK

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

## 動作要件

- JDK 11+ / Maven / Jackson
- `src/main/java/org/jumpserver/pam/Demo.java`

## 設定と実行

ソース SDK をインストールし、下記を設定します。アプリケーション管理で pull 対象のアカウントを許可し、push またはローテーションが必要な場合だけポリシーを関連付けます。接続資料から AK/SK と組織 ID を取得してください。プレースホルダーを置き換え、認証資料を安全に保管します。各レプリカには安定した一意のインスタンス ID を使い、取得にはアカウント ID またはポリシー key の一方だけを指定します。

```bash
cd java
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

mvn package dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.Demo
```

SDK は現在このリポジトリのソースからインストールし、公開パッケージレジストリには未公開です。/path/to/jumpserver を絶対パスに置き換えます。Go と Node.js のインストールはアプリケーションのディレクトリで実行し、Java の依存関係は pom.xml に追加します。リポジトリの実行例のローカルインポートは以下のパッケージインポートに置き換えてください。

```bash
mvn -f /path/to/pam-clients/java/pom.xml install
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

## リクエストとレスポンス

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

## イベントと認証情報の適用

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

```java
package org.jumpserver.pam;

import java.util.List;
import org.jumpserver.pam.Models.Account;
import org.jumpserver.pam.Models.Event;

public final class EventsDemo {
  private static void applyAccount(Account account) {
    throw new UnsupportedOperationException("Implement connection validation, pool switching and old connection cleanup");
  }
  private static void applyEvent(Client client, Event event) {
    if (event.getEvent().equals("application.restart.requested"))
      throw new UnsupportedOperationException("Implement restart and health check");
    Account account = client.getAccount(event.getAccountId(), false);
    if (account.getRevision() != event.getAccountRevision())
      throw new IllegalArgumentException("Event account version is superseded");
    applyAccount(account);
  }
  private static void handleEvent(Client client, Event event) {
    if (!event.getCommandId().isEmpty()
        && !client.reportApplicationCommandResult(event.getCommandId(), "running", null).isAccepted()) return;
    try { applyEvent(client, event); }
    catch (RuntimeException error) {
      client.confirmEvent(event.getEventId(), "failed", "application_failed");
      throw error;
    }
    client.confirmEvent(event.getEventId());
  }
  public static void main(String[] args) {
    Client.Options options = new Client.Options(System.getenv("JMS_ENDPOINT"), System.getenv("JMS_APP_ID"),
        System.getenv("JMS_APP_SECRET"), System.getenv("JMS_INSTANCE_ID")).orgId(System.getenv("JMS_ORG_ID"));
    try (Client client = new Client(options)) {
      Thread stop = new Thread(client::close, "jms-pam-shutdown");
      Runtime.getRuntime().addShutdownHook(stop);
      try (EventStream stream = client.watchCredentialEvents()) {
        for (Event event : stream) {
          List<Event> updates = !event.getCommandId().isEmpty() || event.getEvent().equals("credential.updated")
              ? List.of(event) : event.getEvent().equals("snapshot") ? event.getCredentials() : List.of();
          // Reconcile removed connections on snapshots; release revoked accounts.
          for (Event update : updates) {
            try { handleEvent(client, update); }
            catch (RuntimeException error) { System.err.println(error.getClass().getSimpleName()); }
          }
        }
      } finally { Runtime.getRuntime().removeShutdownHook(stop); }
    }
  }
}
```

従来の key API は互換用です。新規連携は `get_account` と `confirm_event` を使い、Core も更新してください。

## アプリケーションコマンド

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

## 主なメソッド

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

## トラブルシューティング

HTTP、ネットワーク、認証、デコードの失敗はエラーコードと HTTP 状態を含む SDK の例外・エラー型で通知されます。使用後はストリームとクライアントを閉じてください。複製クライアントのライフサイクルは独立しています。一時障害はアプリケーション側で再試行し、秘密情報や認証ヘッダーをログに記録しないでください。

`PAMException`

全 SDK はプロトコル版 1、Agent 設定はスキーマ版 1 を使用します。client_upgrade_required（HTTP 426）を受けたら互換性を確認し更新してください。未知の任意フィールドや通知イベントは許容しますが、未対応ポリシーは適用・確認できません。受領通知は自動送信され、認証情報の適用を証明しません。

## Go Agent の接続

認証には app_id、app_secret、org_id と安定した instance_id を使用し、認可範囲はアプリケーションに関連付けたポリシーに従います。すべてのパスとサービス操作はローカル設定です。state_file は最新パスワードを保持し、event_file は秘密を含まないイベント情報を追記します。delivery は既定の出力、rules はファイル、テンプレートと reload/restart または固定スクリプトを指定します。更新通知で最新値を取得・保存し、ファイルを原子的に置き換えてから操作を実行します。失敗は再試行します。rules の key には get_accounts の credentials[].key を使用します。購読 key にはアカウント ID が含まれます。空の rules は key ごとの既定ファイルを出力します。

ローカル rules でファイル、JSON/EnvironmentFile または信頼済みテンプレートと、systemd reload/restart や固定実行ファイルを設定します。スクリプトは標準入力で認証情報 JSON を受け取り、固定引数とタイムアウトを使い、適用を検証してから成功を返します。Core は実行パスや権限を拡張できません。設定変更後に Agent を再起動します。

ダウンロードした設定には Agent の識別情報と配信設定が含まれます。rules には業務で使うアカウント ID、設定更新、反映操作、稼働中の接続確認を指定します。allow_account_switch を設定したアカウントは A/B の双方向ローテーションに同じルールを使えます。任意の credential_check でファイル更新前に新しいログインを検証できます。状態、イベント、Socket のパスと 300 秒の照合間隔には既定値があります。rules が空なら資格情報ごとに既定ファイルを出力します。

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

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```
