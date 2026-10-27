# JumpServer PAM Java SDK

<!-- sdk-doc:start -->

## 1. 接続の前提と流れ

1. アプリを作成し、取得可能なアカウントを許可します（pull）。ユーザー名やポリシー ID ではなくアカウント ID を使います。
2. 自動配信には通知または A/B ローテーションのポリシーを関連付けます（push）。
3. 接続ウィザードで認証設定を取得し、各レプリカに固定で一意の `instance_id` を設定して AK/SK を保護します。

pull のみなら最初の取得までで十分です。push や A/B には業務コールバックと監視が必要です。SDK がプロトコルと結果を処理し、業務が実接続を切り替えます。例は現行ソース用です。対応する Core と SDK を用意してください。公開済みの 1.0.2 には新 API がすべて含まれるわけではありません。

- JDK 11+ / Maven / Jackson

## 2. SDK のインストールと認証設定

SDK をインストールし、下記を設定します。アプリケーション管理で pull 対象のアカウントを許可し、push またはローテーションが必要な場合だけポリシーを関連付けます。接続資料から AK/SK と組織 ID を取得してください。プレースホルダーを置き換え、認証資料を安全に保管します。各レプリカには安定した一意のインスタンス ID を使います。 取得にはアカウント ID を指定します。

以下のコマンドで同じ Core に同梱されたクライアントソースをインストールします。公開済み 1.0.2 には管理コールバックがありません。jumpserver.example.com を Core のアドレスに置き換え、Go のローカル置換パスを含む jms-pam-clients ディレクトリを保持してください。

```bash
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/java/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz &&
mvn -f ./jms-pam-clients/java/pom.xml install
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

### ローカルソースからの代替インストール

Core がソース取得 API を提供していない場合やオフラインの場合は、対応ソースを `/path/to/pam-clients` に用意し、アプリのディレクトリで実行します。実際のパスに置き換えてください。

```bash
mvn -f /path/to/pam-clients/java/pom.xml install
```


```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'
```

## 3. 最初の資格情報を取得

`src/main/java/org/jumpserver/pam/Demo.java`

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

```bash
mvn compile dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.Demo
```

## 4. イベントハンドラー

以下のアカウント変更コールバックを実装し、マネージドリスナーを開始します。SDK がスナップショット、最新情報の取得、アカウントと版の検証、再試行、コマンドの実行権取得と結果報告を処理します。実接続の検証、接続プール切替、旧接続の解放を完了してから戻ってください。Python と Node.js はサブクラス、Go は OnAccountChanged、Java は AccountEventListener を使用します。未実装の例は失敗します。

`src/main/java/org/jumpserver/pam/EventsDemo.java`

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

```bash
mvn compile dependency:copy-dependencies
java -cp 'target/classes:target/dependency/*' org.jumpserver.pam.EventsDemo
```

コールバックは有界キューの後で直列実行され、通常更新の失敗は 1–30 秒で再試行します。業務処理が完了してから成功を報告し、結果送信の失敗では同じリスナー内で完了済み動作を繰り返しません。コマンドは実行権取得後に実行し、失敗した動作は自動再実行しません。使用アカウントは自動報告され、A/B 切替で現在値を置き換えます。権限範囲から外れた接続の解放処理を実装してください。使用状況報告や情報取得、ファイル配信だけでは業務成功を確認しません。低レベル API と旧フックは明示的確認を維持します。再起動後の冪等性も必要です。

### 監視のライフサイクルと終了

`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`

watchEvents は呼出元スレッドで待ちます。startEvents は同期完了を保証しません。監視は 1 つです。close は処理終了を待ち、ハンドラー自身からも close できます。awaitTermination は外部から呼びます。

### 最新の認証情報とバックエンド障害

SDK はまず API に問い合わせ、成功時に保持値を更新します。古い版には戻りません。同じ選択条件の取得済み値へフォールバックできるのはタイムアウト、ネットワーク障害、HTTP 5xx の場合だけで、ローカル由来と明示します。保持値に時間期限はなく、SDK の clone や再起動時は空になります。HTTP 401/403/404 と credential_not_found（400）は保持値を削除して失敗します。426 や不正な成功応答ではフォールバックせず、任意メタデータ API の欠如は保持値を削除しません。Agent v2 は store/services の宣言対象を保護された状態に永続化し、未宣言の都度取得は自動保存しません。明示的取消はオフラインでも HTTP 更新前に適用します。push スナップショットは保存対象の全権限を表さず、同期成功時に現在のアプリ権限を確認します。

- `account.isFromLocal()`
- `getAccount(accountId, false)`

管理された監視を有効にすると、snapshot と credential.updated が自動で取得し、保持値を更新してから業務フックを呼びます。更新失敗時は前の値を保持し再試行します。Agent も更新通知で取得し、障害時は既存値を保持します。更新や手動切替には下記の API 取得必須の呼び出しを使用してください。保持値を新しく取得したバージョンと見なしたり、ローテーションを自動確認したりしません。

アイドル時は 10 秒ごとに ping を送り、約 30 秒メッセージがなければ再接続します。待機は 1–30 秒の指数バックオフで毎回再署名します。snapshot は現在の状態を復元し、履歴イベントは再送しません。



## 5. 主なメソッド

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


### アプリケーション状態の報告

実行状態と使用中のアカウント ID を完全なスナップショットとして報告します。状態は `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。空のリストは以前の使用記録を消去します。状態変更時またはアプリ自身のハートビートから呼び出します。Python、Java、Node.js は通信や API の失敗時に false を返し、Go は `TryReportStatus` を提供します。厳格な形式で詳細エラーを取得できます。引数エラーは拒否されます。設定した HTTP タイムアウトを使用します。資格情報の取得、ローテーションの確認、キャッシュの無効化は行いません。`error_code` に秘密や例外メッセージを入れないでください。ClientInfo から最後の報告と時刻を取得できます。

```java
boolean accepted = client.reportAccounts(java.util.List.of(account.getId()));
client.reportAccounts(java.util.Collections.emptyList());
```


### 低レベルイベントとアプリコマンド

`account_id` で対象アカウントを取得し、接続検証と切り替えが完了してから `event_id` で `success` または `failed` を報告します。受信や取得の成功は適用成功ではありません。イベントと再接続スナップショットには `event_id`、`account_id`、`account_revision` が含まれます。適用前に版を検証し、コマンドは `running` で取得してください。再実行に対応した処理が必要です。

従来の key API は互換用です。新規連携は `get_account` と `confirm_event` を使い、Core も更新してください。

### API

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

## 6. 接続の検証

1. 許可された ID で取得し実接続を検証します。
2. 監視では初回同期とポリシー更新を確認し、業務処理完了後に成功を確認します。
3. 使用状態とイベント結果を別々に確認します。取得や状態報告は適用確認ではありません。

### トラブルシューティング

HTTP、ネットワーク、認証、デコードの失敗はエラーコードと HTTP 状態を含む SDK の例外・エラー型で通知されます。使用後はストリームとクライアントを閉じてください。複製クライアントのライフサイクルは独立しています。一時障害はアプリケーション側で再試行し、秘密情報や認証ヘッダーをログに記録しないでください。

`PAMException`

全 SDK はプロトコル版 1、Agent 設定はスキーマ版 1 を使用します。client_upgrade_required（HTTP 426）を受けたら互換性を確認し更新してください。未知の任意フィールドや通知イベントは許容しますが、未対応ポリシーは適用・確認できません。受領通知は自動送信され、認証情報の適用を証明しません。 クライアント情報の server_capabilities は任意です。欠落または null は不明を表し、未知の名前を許容します。サーバー機能の一覧であり、権限を付与しません。

<!-- sdk-doc:end -->
