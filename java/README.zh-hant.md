# JumpServer PAM Java SDK

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

## 環境需求

- JDK 11+ / Maven / Jackson

## 設定與執行

安裝 SDK 並填寫下方設定。在應用程式管理中授權可 pull 的帳號；僅在需要 push 或輪換時綁定憑據策略。從接入資料取得應用程式 AK/SK 與組織 ID。替換預留值並保護身分資料，每個副本使用穩定且唯一的實例 ID。依帳號 ID 取密。

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go 透過模組版本標籤分發。Java 與 Node.js 可從 1.0.2 Release 取得 SDK 原始碼套件，安裝需 Maven 或 Node.js。請在應用目錄執行下列命令，並將 Java 依賴加入 pom.xml。範例使用已安裝套件的匯入。

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

## 請求與回應

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

### 列出授權帳號

帳號列表方法查詢應用程式目前授權的 pull 範圍，只回傳帳號、資產及相關 push 策略的中繼資料，不取得密碼或記錄取密；未綁定策略的帳號也會回傳。完整列表方法自動讀取所有分頁；分頁方法回傳 `count` 和 `accounts`（Go 分別回傳列表與總數）。預設 limit 為 200、offset 為 0；limit 為 1–500，search 最長 128 個字元，可搜尋帳號名稱、使用者名稱、資產名稱或地址。重複、不完整或總數變動會報錯；授權變動時重新查詢完整列表。需要密碼時以帳號 ID 呼叫取密方法。範例需要包含新增方法的 SDK 原始碼；已發布的 1.0.2 尚未在 Python、Java、Node.js 提供這些方法。

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


### 應用程式、實例與策略資訊

getApplication 回傳目前簽章應用程式的 ID、名稱、組織及授權帳號與可見策略數量；getClientInfo 只回傳目前副本的登記資訊、類型和版本。策略列表一次回傳全部可見的關聯策略，包括停用策略；search 比對策略名稱或 key。單一策略以策略 ID 或策略本身的 key 查詢，不使用帳號 push key。輪換策略的兩個帳號及目前生效帳號都須有授權；訂閱數量只統計目前有權存取的帳號。查詢只回傳中繼資料，不回傳密鑰或密碼、不取密或確認生效。首次存取會登記副本。需要更新 Core 與 SDK 原始碼；已發布的 1.0.2 尚未包含這些方法。

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


## 事件與憑證生效

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

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

舊 key API 保留相容。新整合使用 `get_account` 和 `confirm_event`，並須同步更新 Core。

## 應用程式指令

使用 `account_id` 取得事件指定帳號。完成連線驗證與切換後，透過 `event_id` 回報 `success` 或 `failed`；收到事件或取密成功不代表套用成功。事件與重連快照包含 `event_id`、`account_id`、`account_revision`，套用前須檢查帳號版本。指令先以 `running` 認領；處理須具冪等性。

## 常用方法

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

### 應用狀態回報

回報應用狀態和目前使用的帳號 ID。支援 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次都是完整快照，空帳號清單會清除先前記錄。由應用在狀態變更或自己的心跳中呼叫，不會自動啟動背景回報。Python、Java、Node.js 遇到網路或 API 錯誤時預設回傳 false；Go 使用 `TryReportStatus`。嚴格模式可取得錯誤，無效參數仍會明確提示。呼叫使用設定的 HTTP 逾時，請避開影響業務回應的路徑。回報不會取得密碼、確認輪換或清除憑據快取。`error_code` 僅傳機器可讀錯誤碼，不要傳密碼或例外原文。透過 ClientInfo 查詢最後回報和時間。

```java
boolean accepted = client.reportStatus("running", account.getId());
client.reportStatus("stopped", java.util.Collections.emptyList());
```


## 問題排查

HTTP、網路、認證及解碼錯誤使用 SDK 例外或錯誤類型，包含錯誤碼與 HTTP 狀態。使用完畢後關閉事件流與客戶端，複製客戶端具有獨立生命週期。在業務邊界重試暫時故障，不記錄密碼或認證標頭。

`PAMException`

各 SDK 使用版本 1 協定，Agent 設定格式為版本 1。收到 client_upgrade_required（HTTP 426）時檢查相容性並升級。允許未知可選欄位及通知事件，未實作的策略類型不得套用或確認。SDK 自動傳送接收回執，不能作為憑證已生效的證明。 實例資訊可包含 server_capabilities；缺失或 null 表示未知，未知名稱可保留。能力清單描述伺服器功能，不授予權限。

## Go Agent 接入

身分只需要 app_id、app_secret、org_id 和穩定的 instance_id；授權範圍跟隨應用程式綁定的策略。檔案路徑及服務動作全部在本機設定：state_file 保留最新密碼，event_file 追加不含密碼的事件資料，delivery 定義預設交付，rules 定義檔案、範本及 reload/restart 或固定腳本。收到更新通知後取得最新密碼，先持久化，再原子替換檔案，最後執行動作；交付失敗會重試。規則使用 get_accounts 回傳的 credentials[].key，訂閱 key 包含帳號 ID。rules 為空時預設依 key 寫檔。

本機 rules 設定目標檔案、JSON/EnvironmentFile 或可信範本，以及可選的 systemd reload/restart 或固定可執行腳本。腳本透過標準輸入接收憑據 JSON，參數固定、有逾時，並應在驗證業務生效後回報成功。Core 不能新增腳本路徑或擴大本機能力。修改私有設定後重新啟動 Agent。

下載的引導設定已包含 Agent 身分與交付設定。只需在 rules 中宣告業務使用的帳號 ID，再設定更新業務檔案、生效動作及執行中連線驗證。帳號設定 allow_account_switch 後可沿用同一規則處理 A/B 雙向輪換；可選 credential_check 於修改檔案前驗證新帳號。狀態、事件、Socket 路徑及 300 秒對帳週期皆有預設值。rules 為空時依憑據寫入預設檔案。

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

`config_update.format` 預設為 `auto`，省略與明確填寫的效果相同。依副檔名辨識：`.txt`、`.env`、`.properties` 使用 `env`，替換 `KEY=value`；`.yml`、`.yaml` 使用 `yaml`，替換頂層 `KEY: value`，新值寫為帶引號的字串。其他副檔名需明確指定 `env` 或 `yaml`。映射欄位必須已存在且只出現一次，其他設定保留。INI section、巢狀 YAML 等複雜格式使用 `files` + `template_file` 完整產生，或用 `script` 自訂替換。環境變數檔案更新後，應用仍需重新讀取檔案或重新啟動才會生效。

```bash
jms-pam-agent get_accounts
jms-pam-agent get_secret '<account-id>'
sudo jms-pam-agent check-config
sudo systemctl restart jms-pam-agent
```
