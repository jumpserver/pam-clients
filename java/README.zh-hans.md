# JumpServer PAM Java SDK

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

## 环境要求

- JDK 11+ / Maven / Jackson

## 配置与运行

安装 SDK，填写下方配置。在应用管理中授权可 pull 的账号；只有需要 push 或轮换时才绑定凭据策略。从应用接入材料获取应用 AK/SK 和组织 ID。替换占位符，将身份材料保存在部署密钥中，每个副本使用稳定、唯一的实例 ID。按账号 ID 取密。

```bash
export JMS_ENDPOINT='https://jumpserver.example.com'
export JMS_APP_ID='<app-id>'
export JMS_APP_SECRET='<app-secret>'
export JMS_ORG_ID='<org-id>'
export JMS_INSTANCE_ID='app-node-1'
export JMS_ACCOUNT_ID='<account-id>'

```

Go 通过模块版本标签分发。Java 和 Node.js 可从 1.0.2 Release 获取 SDK 源码包，安装需要 Maven 或 Node.js。以下安装命令在应用目录执行；Java 依赖添加到 pom.xml。下方示例使用安装后的包导入。

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

## 请求与响应

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

## 事件与凭据生效

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

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

`get_credential` 和基于 key 的确认接口保留兼容旧接入；新接入使用 `get_account` 和 `confirm_event`。事件结果流程需要同步更新 Core，仅升级 SDK 1.0.2 不会增加服务端能力。

## 应用指令

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

## 常用方法

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

## 排查问题

HTTP、网络、认证和响应解码错误使用 SDK 异常或错误类型，包含错误码及 HTTP 状态。使用完成后关闭事件流与客户端，克隆客户端具有独立生命周期。在业务边界重试临时故障，不记录密码或认证请求头。

`PAMException`

各 SDK 使用版本 1 协议，Agent 配置格式为版本 1。收到 client_upgrade_required（HTTP 426）时检查兼容性并升级。允许未知可选字段及通知事件，未实现的策略类型不得应用或确认。事件接收回执由 SDK 自动发送，不能作为凭据已经生效的证明。

## Go Agent 接入

身份只需要 app_id、app_secret、org_id 和稳定的 instance_id；应用授权控制 pull 范围，绑定策略控制 push 范围。文件路径和服务动作全部在本机配置：state_file 始终保留最新密码，event_file 追加不含密码的事件元数据，delivery 定义默认交付，rules 定义文件、模板及 reload/restart 或固定脚本。收到更新通知后主动取最新密码，先持久化，再原子替换文件，最后执行动作；交付失败会重试。规则使用 get_accounts 返回的 credentials[].key，订阅 push 的 key 为 account:<account-id>，不包含策略 key。rules 为空时默认按 key 写文件。

本机 rules 配置目标文件、JSON/EnvironmentFile 或可信模板，以及可选的 systemd reload/restart 或固定可执行脚本。脚本通过标准输入接收凭据 JSON，参数固定、有超时，并应在验证业务生效后返回成功。Core 不能新增脚本路径或扩大本机能力。修改私有配置后重启 Agent。

下载的引导配置已包含 Agent 身份和交付设置。只需在 rules 中声明业务使用的账号 ID，再配置更新业务文件、生效动作和运行中连接验证。账号设置 allow_account_switch 后可沿用同一规则处理 A/B 双向轮换；可选 credential_check 用于改文件前验证新账号。状态、事件、Socket 路径和 300 秒对账周期均有默认值。rules 为空时按凭据写默认文件。

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
