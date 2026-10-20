# JumpServer PAM Java SDK

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

## 動作要件

- JDK 11+ / Maven / Jackson

## 設定と実行

SDK をインストールし、下記を設定します。アプリケーション管理で pull 対象のアカウントを許可し、push またはローテーションが必要な場合だけポリシーを関連付けます。接続資料から AK/SK と組織 ID を取得してください。プレースホルダーを置き換え、認証資料を安全に保管します。各レプリカには安定した一意のインスタンス ID を使います。 取得にはアカウント ID を指定します。

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go はモジュールのバージョンタグで配布します。Java と Node.js は Release 1.0.2 の SDK ソースを利用し、インストールに Maven または Node.js が必要です。以下はアプリケーションのディレクトリで実行し、Java の依存関係は pom.xml に追加します。例はインストール済みパッケージを読み込みます。

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

### 認可されたアカウントの一覧

一覧メソッドはアプリケーションの現在の pull 認可範囲を取得します。アカウント、資産、関連する push ポリシーのメタデータのみを返し、パスワード取得や取得記録は行いません。ポリシーのないアカウントも含まれます。全件メソッドは全ページを読み取り、ページメソッドは `count` と `accounts` を返します（Go はリストと総数を別々に返します）。既定は limit 200、offset 0、limit は 1–500、search は最大128文字でアカウント名、ユーザー名、資産名、アドレスを検索します。重複、不完全な結果、総数の変化はエラーになります。認可が変わった場合は一覧全体を再取得してください。秘密が必要な場合はアカウント ID で取得メソッドを呼びます。例には新メソッドを含む SDK ソースが必要です。公開済み1.0.2の Python、Java、Node.js にはまだ含まれません。

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


### アプリケーション、インスタンス、ポリシー情報

getApplication は署名したアプリケーションの ID、名前、組織、認可アカウント数と可視ポリシー数を返します。getClientInfo は現在のレプリカの登録、種類、バージョンのみを返します。一覧は無効化されたものを含む可視な関連ポリシー全件を返し、search は名前と key を検索します。単一ポリシーは ID またはポリシー自体の key の一方で取得します。アカウントの push key は使用しません。輪換ポリシーには両アカウントと有効アカウントの認可が必要で、購読数は現在認可されたものだけです。秘密やパスワードを返さず、取得や適用確認も行いません。初回アクセスでレプリカを登録します。更新した Core と SDK ソースが必要です。公開済み1.0.2には未収録です。

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

- `getApplication() / getClientInfo()`
- `reportStatus(status, accountId) / reportStatus(ApplicationStatus) / reportStatusStrict(report)`
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

### アプリケーション状態の報告

実行状態と使用中のアカウント ID を完全なスナップショットとして報告します。状態は `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。空のリストは以前の使用記録を消去します。状態変更時またはアプリ自身のハートビートから呼び出します。Python、Java、Node.js は通信や API の失敗時に false を返し、Go は `TryReportStatus` を提供します。厳格な形式で詳細エラーを取得できます。引数エラーは拒否されます。設定した HTTP タイムアウトを使用します。資格情報の取得、ローテーションの確認、キャッシュの無効化は行いません。`error_code` に秘密や例外メッセージを入れないでください。ClientInfo から最後の報告と時刻を取得できます。

```java
boolean accepted = client.reportStatus("running", account.getId());
client.reportStatus("stopped", java.util.Collections.emptyList());
```


## トラブルシューティング

HTTP、ネットワーク、認証、デコードの失敗はエラーコードと HTTP 状態を含む SDK の例外・エラー型で通知されます。使用後はストリームとクライアントを閉じてください。複製クライアントのライフサイクルは独立しています。一時障害はアプリケーション側で再試行し、秘密情報や認証ヘッダーをログに記録しないでください。

`PAMException`

全 SDK はプロトコル版 1、Agent 設定はスキーマ版 1 を使用します。client_upgrade_required（HTTP 426）を受けたら互換性を確認し更新してください。未知の任意フィールドや通知イベントは許容しますが、未対応ポリシーは適用・確認できません。受領通知は自動送信され、認証情報の適用を証明しません。 クライアント情報の server_capabilities は任意です。欠落または null は不明を表し、未知の名前を許容します。サーバー機能の一覧であり、権限を付与しません。

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

`config_update.format` の既定値は `auto` で、省略時も同じです。拡張子 `.txt`、`.env`、`.properties` は `env`（`KEY=value`）、`.yml`、`.yaml` は `yaml`（最上位の `KEY: value`、新しい値は引用符付き文字列）として扱います。他の拡張子では `env` または `yaml` を明示します。対象フィールドは既存で一度だけ出現する必要があり、他の設定は保持されます。INI セクションやネストした YAML は `files` + `template_file` で全体を生成するか、`script` を使用します。環境変数ファイルの更新後はアプリケーションの再読み込みまたは再起動が必要です。

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```
