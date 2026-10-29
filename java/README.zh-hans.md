# JumpServer PAM Java SDK

<!-- sdk-doc:start -->

## 1. 接入前提与流程

1. 创建应用，授权允许按需获取的账号。
2. 需要及时更新时，在应用中开启“账号凭据变更通知”，覆盖全部授权账号。A/B 账号切换仍绑定独立轮换策略。
3. 下载应用接入材料，保护 AK/SK；每个副本使用稳定、唯一的实例 ID。

仅按需取密时，完成“首次获取账号凭据”即可；需要主动更新或 A/B 轮换时，再实现托管业务回调并启动监听。SDK 负责协议和事件结果，业务负责验证并切换真实连接。本页示例对应当前源码，新增 API 需要配套的 Core 和 SDK 构建；已发布的 1.0.2 安装包尚未包含全部新接口，请先部署匹配版本再运行这些示例。

- JDK 11+ / Maven / Jackson

## 2. 安装 SDK 与身份配置

安装下方 SDK，并使用应用接入向导下载的身份配置。保护 AK/SK，每个副本使用稳定且唯一的实例 ID。

通过下方命令安装当前 Core 随附的客户端源码，避免已发布的 1.0.2 包缺少托管回调。将 jumpserver.example.com 替换为 Core 地址；保留解压后的 jms-pam-clients 目录，Go 模块使用本地替换路径。

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

### 本地源码安装（备用）

当前 Core 尚未提供上述源码下载接口，或需要离线部署时，取得配套客户端源码，放到 `/path/to/pam-clients`，再从业务应用目录执行以下命令。请替换为实际源码路径；托管回调需要包含对应方法的 SDK。

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

## 3. 首次获取账号凭据

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

## 4. 事件监听与业务切换

实现下面的账号变更回调并启动托管监听即可。SDK 负责快照、实时取密、账号与版本校验、重试、指令认领和结果回报。业务回调必须完成真实连接验证、连接池切换和旧连接释放后再返回。Python、Node.js 使用子类钩子，Go 使用 OnAccountChanged，Java 使用 AccountEventListener。未实现业务切换时，示例会明确失败。

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

账号回调通过有界事件队列串行执行；普通更新失败按 1–30 秒退避重试。业务回调成功返回后才上报成功；结果上传失败时，同一次监听仅重试上传，不重复已完成的动作。指令先认领再执行，失败的指令动作不自动重做。已处理账号自动按完整快照上报使用情况，A/B 切换会替换当前账号。撤销范围需实现账号连接清理回调。使用账号上报、取密或文件交付本身不会确认业务生效。原始事件流和旧凭据钩子保留显式确认方式。业务处理仍需支持进程重启后的幂等执行。

### 监听生命周期与退出

`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`

watchEvents 等待当前线程；startEvents 返回不代表初始同步完成。每个客户端允许一个高层监听器。EventSubscription.close 和 Client.close 停止并等待处理函数；处理函数可以关闭自己的订阅或客户端。awaitTermination 由外部调用。

### 最新凭据与后端不可用

SDK 取密先请求 API，成功后替换保留值，不允许旧版本覆盖新版本。仅超时、网络错误或 HTTP 5xx 可回退到相同选择条件下已获取的值，并标记本地来源；保留值不按时间过期。SDK 使用客户端私有内存，clone 和重启从空状态开始。HTTP 401/403/404 或 credential_not_found（HTTP 400）清空保留凭据并报错；HTTP 426、成功响应格式错误均不使用回退，可选元数据接口缺失不会清空凭据。PAM Agent 在受保护的状态中保留 store/services 声明的账号，未声明的按需查询不会自动持久化。明确撤销在 HTTP 刷新前生效，离线也不例外；push 快照不代表已存储账号的完整授权范围，成功对账会核对当前应用授权。

- `account.isFromLocal()`
- `getAccount(accountId, false)`

启用高层事件监听后，snapshot、credential.updated 会自动获取当前凭据并替换本地保留值，再调用业务处理函数。刷新失败时保留上一份凭据并重试。PAM Agent 同样在更新通知后主动取密，后端故障期间保留已有凭据。事件刷新和手动切换使用下方必须实时获取的调用；保留的密码不能被当成刚获取的新版本，也不会自动确认轮换。




## 5. 常用方法

### 列出授权账号

使用账号列表方法查询应用当前授权的 pull 范围。列表只返回账号、资产和关联 push 策略的元数据，不获取密码，不记录取密；未绑定策略的授权账号也会返回。完整列表方法自动读取所有分页；分页方法返回 `count` 和 `accounts`（Go 分别返回列表和总数）。默认 limit 为 200、offset 为 0；limit 范围为 1–500，search 最长 128 个字符，可匹配账号名称、用户名、资产名称或地址。重复账号、分页不完整或总数变化会报错；授权范围变化时重新查询完整列表。需要密码时，再使用账号 ID 调用取密方法。示例需要包含这些新增方法的 SDK 源码；已发布的 1.0.2 尚未在 Python、Java、Node.js 中提供这些方法。

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


### 应用、实例与策略信息

getApplication 返回当前签名应用的 ID、名称、组织以及授权账号和可见策略数量；getClientInfo 只返回当前副本的登记信息、类型和版本。策略列表一次返回全部可见的关联策略，包括停用策略；search 匹配策略名称或 key。查询单个策略时只提供策略 ID 或策略本身的 key，不使用账号 push key。轮换策略要求两个账号及当前生效账号均在应用授权范围内；普通账号通知由应用开关控制，覆盖全部授权账号。这些查询只返回元数据，不返回密钥或密码，不取密、不确认凭据生效。和账号查询一样，首次访问会登记当前副本。需要同步更新 Core 和 SDK 源码；已发布的 1.0.2 尚未包含这些方法。

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


### 应用状态上报

上报应用运行状态和当前使用的账号 ID，支持 `starting`、`running`、`degraded`、`stopping`、`stopped`、`error`。每次上报都是完整快照，空账号列表会清除此前使用记录。由应用在状态变化或自己的心跳中调用，不会自动启动后台上报。Python、Java、Node.js 遇到网络、API 错误或旧 Core 不支持接口时默认返回 false；Go 使用 `TryReportStatus` 获得相同行为。严格调用方式可以获取具体错误，参数错误仍会明确提示。调用遵循配置的 HTTP 超时，请放在不影响业务响应的执行路径。上报不会取密、确认轮换或清空已有凭据缓存。`error_code` 只传机器可读错误码，不要传密码或异常原文。`get_client_info` / `GetClientInfo` / `getClientInfo` 可以读取最后一次上报及其时间。

```java
boolean accepted = client.reportAccounts(java.util.List.of(account.getId()));
client.reportAccounts(java.util.Collections.emptyList());
```


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

## 6. 接入验收

1. 将占位值替换为已授权的账号 ID，取密后验证真实连接，不记录密码。
2. 使用事件监听时，核对首次同步和一次授权账号更新或 A/B 切换；业务回调完成真实切换后，事件才应显示成功。
3. 分别检查实例使用账号、运行状态和事件结果。取密或使用状态上报不会确认凭据生效。

### 排查问题

HTTP、网络、认证和响应解码错误使用 SDK 异常或错误类型，包含错误码及 HTTP 状态。使用完成后关闭事件流与客户端，克隆客户端具有独立生命周期。在业务边界重试临时故障，不记录密码或认证请求头。

`PAMException`

各 SDK 使用版本 1 协议，PAM Agent 配置格式为版本 1。收到 client_upgrade_required（HTTP 426）时检查兼容性并升级。允许未知可选字段及通知事件，未实现的策略类型不得应用或确认。事件接收回执由 SDK 自动发送，不能作为凭据已经生效的证明。 实例信息可包含 server_capabilities；缺失或 null 表示未知，未知名称可保留。能力列表描述服务端功能，不授予权限。

<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# 应用集成概览

## 先了解授权、通知与轮换

1. **账号授权**决定应用可以按需获取哪些账号的凭据。安装客户端前，先完成应用授权。
2. **账号凭据变更通知**是应用级开关，覆盖全部授权账号。应用需要跟随账号密码更新时开启，无需创建订阅策略。
3. **A/B 账号切换**使用独立绑定的轮换策略，先将业务切到另一个账号，再修改原账号密码。两个账号都须获得授权；周期、等待条件、取消与恢复请查看具体策略中的“轮换运行说明”。

## 选择接入方式

| 接入方式 | 适用情况 | 从哪里开始 |
| --- | --- | --- |
| SDK | 可以修改业务代码，由应用直接连接 JumpServer。 | 选择“SDK 接入”和编程语言，需要自动更新时实现托管账号变更回调。 |
| PAM Agent | 需要本地凭据文件、业务配置更新或本地 Socket，希望将 JumpServer 密钥保留在独立 Agent 中。 | 选择“Agent 接入”，安装后按需配置 store、socket 和 services。 |
| Socket API | 业务代码通过本机 Agent 取密，自行处理事件和连接切换，不持有应用密钥。 | 选择“Socket API 接入”，声明 delivery: socket 的业务服务。 |

## 接入与验收顺序

1. 创建应用，授权账号，下载并保护应用身份配置。
2. 选择 SDK 或 PAM Agent，安装匹配客户端，先获取一个账号并验证真实连接。
3. 需要自动更新时，开启应用通知并实现业务更新；需要 A/B 切换时，再绑定轮换策略并允许跟随当前账号。
4. 验证一次账号更新或 A/B 切换：运行中的业务采用新凭据、旧连接释放、业务处理结果成功。仅接收事件、取密或写文件不足以通过验收。

## 后续查阅位置

“Agent 接入”说明安装、文件存储、本地接口和业务动作；“SDK 接入”说明各语言安装、托管回调和 API；“事件与指令”集中解释通用事件数据、快照、回执及手动指令。每个账号切换策略内的“轮换运行说明”解释该策略的周期、阻塞和恢复。 “Socket API 接入”单独说明本机双向通信、事件接收、取密、业务确认、失败重试和 A/B 切换。

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# 事件与指令

本参考适用于各语言 SDK 和 PAM Agent。常规接入优先使用 SDK 托管回调或 Agent 业务服务；自行实现事件消费者时再使用底层协议。

## 事件职责

| 事件 | 应用职责 |
| --- | --- |
| credential.updated、snapshot 条目 | 获取事件指定的账号、校验版本，完成业务应用后上报结果。SDK 托管回调负责协议处理。 |
| app.scope.updated | 对账范围并清理失效状态。解除轮换绑定不等于撤销账号授权。 |
| `rotation.*`（如 `rotation.started`、`rotation.completed` 等）、`credential.change.*`（如 `credential.change.started`、`credential.change.completed` 等） | 更新进度、日志或告警，不重复应用凭据，也不对这些通知确认业务成功。 |
| credential.switch.requested、app.restart.requested | 管理员针对选定实例的手动指令，须认领、执行并上报指令结果。 |

## 完整事件清单

以下逐项列出 Core 应用事件、手动指令及连接控制消息；兼容项单独标注。这里是完整事件参考，轮换详情只解释其在具体流程中的作用。

### 凭据与范围事件

credential.updated 负责应用凭据；app.scope.updated 和 snapshot 负责范围对账。绑定消失时停止跟随策略并保留仍授权账号的使用状态；实际授权消失时才停用本地取密、清理凭据并通知业务释放连接。

| 事件标识 | 含义 |
| --- | --- |
| `credential.updated` | 当前凭据或发布账号更新，按事件账号取密并应用。 |
| `app.scope.updated` | 账号授权、通知开关或轮换策略绑定变化。按 authorized_account_ids 判断账号是否仍获授权，再通过 snapshot 对账通知项和轮换绑定；解除绑定不应关闭仍获授权账号的连接。 |

### 通用改密事件

通用任务生命周期通知用于进度与告警，不因此再次取密或确认业务成功；通过 credential.updated 或 snapshot 应用凭据。

| 事件标识 | 含义 |
| --- | --- |
| `credential.change.started` | 改密任务进入等待或执行阶段，不代表远端密码已改变。 |
| `credential.change.completed` | 改密任务成功，不代表应用已经采用新凭据。 |
| `credential.change.failed` | 改密失败或结果无法核验，检查任务及远端账号。 |

### 轮换进度事件

这些通知说明轮换进度与结果。应用通过 credential.updated 或 snapshot 切换，不因每条进度通知重复执行。

| 事件标识 | 含义 |
| --- | --- |
| `rotation.verify.started` | 开始验证备用账号能否登录。 |
| `rotation.verify.skipped` | 已按凭据策略跳过备用账号登录验证。 |
| `rotation.verify.failed` | 备用账号验证失败，本次不发布切换。 |
| `rotation.verify.cancelled` | 备用账号验证已取消。 |
| `rotation.started` | 轮换已发布目标账号并进入切换阶段。 |
| `rotation.waiting_for_app` | 等待必需应用实例完成切换并确认。 |
| `rotation.source.waiting` | 发布切换后，观察原主账号的成功取密记录。 |
| `rotation.source.ready` | 原主账号无取密窗口和客户端确认条件达标，可以继续改密。 |
| `rotation.completed` | 原主账号改密已核验，本轮轮换完成。 |
| `rotation.failed` | 轮换进入改密失败或需要核验恢复的状态。 |
| `rotation.force_stopped` | 管理员跳过剩余恢复确认，强制结束周期。 |

### 管理员指令事件

先用 running 认领，仅在 accepted 为 true 时执行，再上报指令结果；这些指令独立于自动轮换进度通知。

| 事件标识 | 含义 |
| --- | --- |
| `credential.switch.requested` | 要求选定实例应用已经发布的账号及版本，不会启动新轮换。 |
| `app.restart.requested` | 执行应用支持的重启动作，并验证健康状态。 |

### 连接与回执消息

SDK 和 Agent 管理保活及接收回执。snapshot、pong 无需 received 回执；快照中的凭据条目单独进行应用处理。

| 事件标识 | 含义 |
| --- | --- |
| `snapshot` | Core 在连接、重连或范围变化时发送当前通知范围及授权账号 ID。 |
| `ping` | 客户端向 Core 发送应用层保活消息。 |
| `pong` | Core 对应用层 ping 的响应。 |
| `received` | 客户端确认已读取事件，不表示业务成功。 |

### 兼容事件

旧事件名和旧准备周期兼容项，不作为新接入的执行流程。当前 Core 不再发送 credential.revoked；新轮换周期验证备用账号后直接发布切换。

| 事件标识 | 含义 |
| --- | --- |
| `credential.revoked` | 已停用：当前 Core 不再发送。由 app.scope.updated 和 snapshot 统一处理；仅保留旧 Core 接入及历史记录兼容。 |
| `configuration.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `rotation.preparation.started` | 旧准备流程开始对齐应用当前账号。 |
| `rotation.accounts.aligned` | 旧准备流程中，应用账号已完成对齐。 |
| `rotation.standby.waiting` | 旧准备流程观察备用账号取密。 |
| `rotation.preparation.ready` | 旧准备流程就绪，不代表已切换或改密。 |
| `rotation.preparation.cancelled` | 旧准备流程已取消。 |
| `rotation.verification.started` | 旧事件名，兼容映射为 `rotation.verify.started`。 |
| `rotation.verification.failed` | 旧事件名，兼容映射为 `rotation.verify.failed`。 |
| `rotation.verification.cancelled` | 旧事件名，兼容映射为 `rotation.verify.cancelled`。 |
| `application.scope.updated` | 旧事件名，兼容映射为 `app.scope.updated`。 |
| `application.restart.requested` | 旧事件名，兼容映射为 `app.restart.requested`。 |
| `rotation.waiting_for_application` | 旧事件名，兼容映射为 `rotation.waiting_for_app`。 |
| `rotation.verification.skipped` | 旧事件名，兼容映射为 `rotation.verify.skipped`。 |

## 事件字段

| 字段 | 含义 |
| --- | --- |
| event_id | 用于接收回执和业务结果的事件标识。snapshot 本身无需回执，其中的凭据条目可有各自的 event_id。 |
| event / occurred_at | 事件类型和发布时间。 |
| account_id / account_revision | 实际目标账号及密码版本；应用前实时获取的账号版本须匹配。 |
| credential_key / revision | A/B 策略的发布元数据；普通账号通知使用账号版本，不混用两种版本。 |
| credential_mode | alternating_rotation 表示 A/B 策略，subscription 表示应用级账号通知，无需订阅策略；应用范围事件可能为 null。 |
| account_switch.account_ids | A/B 切换关联的账号。 |
| operation_id | 关联同一次变更或轮换周期中的事件。 |
| command_id / expires_at | 手动指令标识和执行截止时间，使用独立的指令结果接口。 |

## 快照与授权范围

首次连接、重连及范围变化时，Core 发送 snapshot。credentials 包含应用开启通知后的全部授权账号，以及绑定且仍有授权的有效 A/B 轮换策略。每项按 credential.updated 处理，使用 event_id、account_id 和 account_revision。快照还包含 authorized_account_ids；完整授权账号元数据可通过 get_accounts 查询。解除轮换策略绑定不等于撤销账号授权。

```json
{
  "event": "snapshot",
  "authorized_account_ids": ["<account-id>"],
  "credentials": [{
    "event_id": "<event-id>", "key": "account:<account-id>",
    "credential_mode": "subscription", "account_id": "<account-id>",
    "revision": 1, "account_revision": 1
  }]
}
```

## 接收回执与业务结果

SDK 在交出业务事件前自动尽力发送 received 回执，应用无需额外发送。回执仅表示已读取事件，不表示凭据已经生效，也不能代替确认。应用应处理首次和重连快照；snapshot 与 pong 无需回执。

```json
{"event": "received", "event_id": "<event-id>"}
```

使用 `account_id` 获取事件指定的账号。应用完成连接验证、连接池切换等操作后，通过 `event_id` 上报 `success` 或 `failed`。收到事件或成功取密不代表应用成功。事件和重连快照条目包含 `event_id`、`account_id`、`account_revision`，应用前须检查账号版本。重启和手动切换指令先用 `running` 认领。业务处理须幂等，重连可能重复事件。

## 管理员手动指令

管理员从应用的“发送事件”操作选择目标实例及有效期。

- credential.switch.requested 要求实例应用轮换策略**已经发布**的账号及版本，不会启动新轮换，也不会选择另一个账号。
- app.restart.requested 要求应用执行支持的重启动作并验证健康状态；SDK 应用须实现相应处理函数，Agent 能力由本地服务配置决定。

SDK 托管处理函数负责指令认领和结果回报。底层消费者使用已签名的客户端 API：

1. GET /api/v1/accounts/credential-client/commands/，传入 instance_id，轮询待处理指令；已连接实例也可通过 WebSocket 接收。
2. POST /api/v1/accounts/credential-client/command-result/，提交 instance_id、command_id 和 status: running，仅在 accepted 为 true 时执行。
3. 实际完成业务切换，或重启且健康检查成功后，通过同一指令结果接口报告 status: success；失败报告 failed，并使用安全的 error_code。received 回执或仅调用 confirm_event 不能替代指令结果上报。

过期指令不能再认领，重复投递不得重复执行已认领动作。失败的指令动作不自动重做；重新发送重启前先检查应用状态。

## 连接保活与兼容

事件连接空闲时每 10 秒发送应用层 ping，约 30 秒收不到消息则重连。重连采用 1–30 秒指数退避并重新签名。重连快照恢复当前状态，不重放历史事件。

当前使用 app.scope.updated、app.restart.requested、rotation.verify.started/failed/cancelled 和 rotation.waiting_for_app。旧 application 前缀、rotation.verification 名称及 configuration.updated 兼容映射到新名称，事件 ID 不变。credential.revoked 已停止发送，仅保留旧 Core 接入及历史记录兼容。

`get_credential` 和基于 key 的确认接口保留兼容旧接入；新接入使用 `get_account` 和 `confirm_event`。事件结果流程需要同步更新 Core，仅升级 SDK 1.0.2 不会增加服务端能力。

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Socket API 接入

业务应用通过本机 PAM Agent 接入，无需安装 JumpServer SDK，也无需在业务进程保存应用 AK/SK。Agent 负责 Core 身份认证、授权核对、凭据刷新、A/B 解析和结果重试；业务负责真实连接及生效验证。

## 1. Unix Socket / HTTP / WebSocket

底层是 Unix Domain Socket（AF_UNIX），不是 TCP。连接的是文件系统路径，不开放 IP 和端口。HTTP/JSON 请求与 WebSocket 事件通道共用该 Socket；URL 中的 localhost 仅作为 HTTP Host。Agent 与 Core 之间仍使用 HTTPS/WSS。

## 2. Agent / services

在 Agent 配置中声明 Socket 服务，复用文件交付的账号声明和 allow_account_switch 规则。声明的 A ID 始终是映射键，account_id 表示当前生效的 A/B 账号。Socket 服务不能同时配置 update_config、reload_service、credential_check 或 app_check；restart_supported 显式允许该业务处理重启指令。

```json
{
  "version": 1,
  "endpoint": "https://jumpserver.example.com",
  "app_id": "<application-id>",
  "app_secret": "<application-secret>",
  "org_id": "<org-id>",
  "pull_interval": 300,
  "socket": {
    "path": "/run/jms-pam-agent/agent.sock",
    "owner": "appuser"
  },
  "services": [
    {
      "name": "orders",
      "delivery": "socket",
      "allow_account_switch": true,
      "restart_supported": false,
      "accounts": [
        {
          "id": "<primary-account-id>"
        }
      ]
    }
  ]
}
```

## 3. Events / A/B

建立连接、读取初始快照、获取交付凭据、验证真实新连接、替换业务连接并释放旧会话后，才返回 success。收到通知或取到密码都不代表业务已切换。业务失败返回 failed，并等待 Agent 退避后重试。

每次连接先发送最新快照，用于对齐当前状态，不重放全部历史。delivery 只包含元数据，包括 delivery_id、声明账号键和精确的账号/策略版本。Core 不可用时也会通知明确撤销，revoked_account_ids 要求业务关闭旧会话。凭据刷新尚未完成时，delivery 可以为空。每个独立生效的业务实例使用独立 service_id；同一服务只允许一个 WebSocket 长连接。

```text
GET /v1/events?service_id=orders
Upgrade: websocket
```

```json
{
  "event": "snapshot",
  "service_id": "orders",
  "result": "pending",
  "retry_ready": true,
  "received": false,
  "credentials_read": false,
  "core_status": "pending",
  "delivery": {
    "delivery_id": "<delivery-id>",
    "event": "credential.updated",
    "service_id": "orders",
    "credentials": {
      "<primary-account-id>": {
        "key": "<rotation-key>",
        "account_id": "<active-account-id>",
        "revision": 7,
        "account_revision": 3,
        "event_id": "<core-event-id>"
      }
    },
    "revoked_account_ids": []
  },
  "commands": []
}
```

```json
{"request_id":"1","operation":"credentials","delivery_id":"<delivery-id>"}
```

```json
{"request_id":"2","operation":"result","delivery_id":"<delivery-id>","status":"success"}
```

```json
{"request_id":"3","operation":"result","delivery_id":"<delivery-id>","status":"failed","error_code":"database_probe_failed"}
```

按原 delivery_id 返回 success 或 failed。Agent 先持久化结果，再向 Core 上报；相关业务全部成功后才确认凭据事件。accepted 表示本地已保存，不代表 Core 已接收。成功结果可幂等重报；旧版本返回 HTTP 409。失败后等待 1–30 秒，retry_ready 会变为 true。结果响应丢失时重报结果，不重复已经成功的业务动作。回调必须可重复执行：业务动作和本地 ACK 之间发生崩溃时，无法保证只执行一次。 快照中的 received、credentials_read、result 分别表示业务接收、读取凭据和业务处理结果；core_status 单独表示 Core 上报状态，支持 pending、confirmed、failed、not_required。received 回执通过同名操作提交，取密也会记录接收阶段，二者都不确认业务成功。

## 4. Commands / status

Agent 先认领指令，再交给业务。只有声明 restart_supported=true 的服务会在 commands 中收到重启指令；重启并验证就绪后返回 command.result。成功或失败先持久化，上报 Core 失败时只重试上报。A/B 切换指令复用凭据交付和业务结果确认；过期指令不能确认。

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

业务状态与凭据确认分离，支持 starting、running、degraded、stopping、stopped、error，error_code 仅填写不含敏感数据的短码。账号使用情况只在业务成功后更新，Agent 汇总各服务状态。GET /v1/usage 获取汇总；已有 POST /v1/usage 替换整个 Agent 实例的显式账号声明，不确认事件。

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

| HTTP | API |
| --- | --- |
| GET | `/v1/health`, `/v1/services` |
| GET | `/v1/accounts`, `/v1/usage` |
| GET | `/v1/credential?account_id=<id>&allow_local_fallback=false` |
| GET | `/v1/application`, `/v1/instance`, `/v1/policies?search=...` |
| GET | `/v1/policy?key=<key>` or `/v1/policy?policy_id=<id>` |
| GET | `/v1/services/<service_id>` |
| GET | `/v1/services/<service_id>/credentials?delivery_id=<delivery_id>` |
| POST | `/v1/services/<service_id>/received`, `/v1/services/<service_id>/results` |
| POST | `/v1/services/<service_id>/status` |
| POST | `/v1/services/<service_id>/commands/<command_id>/result` |
| POST | `/v1/usage`, `/v1/confirm` |

WebSocket operations: `ping`, `received`, `accounts`, `credential`, `application`, `instance`, `policies`, `policy`, `credentials`, `result`, `command.result`, `status`, `usage`.
`usage` without `account_ids` reads the aggregate; with `account_ids` (including `[]`) it replaces the instance-wide explicit usage declaration, just like HTTP POST `/v1/usage`.
Every request includes `request_id`; responses contain the same ID and `result` or `error: {code, status}`. Snapshots can arrive between responses. `{"event":"ping"}` is a heartbeat. HTTP result/status bodies use the same fields without `request_id` and `operation`.

```bash
curl --fail --unix-socket /run/jms-pam-agent/agent.sock \
  http://localhost/v1/services/orders
```

Unix Socket 的属主权限是访问边界。service_id 用于区分业务消费者，不是独立安全身份：能打开同一 Socket 的进程共享应用授权。不可信的不同业务应使用独立 Agent 实例和 Socket。事件不携带密码，业务显式取密且不要记录密码。语言环境不支持 Unix Socket WebSocket 时，也可以轮询业务快照并提交结果。

## 6. Python

Python 示例使用 websockets，不依赖 JumpServer SDK。默认仅观察元数据，不会确认业务成功。真实接入时实现 Business 回调，并调用 run(socket_path, service_id, business)。业务进程新启动时，即使旧进程已应用当前交付，也应从当前凭据初始化连接；需要跨崩溃去重时，由业务持久化 delivery_id。

```bash
python3 -m pip install 'websockets>=11'
curl -fL https://jumpserver.example.com/api/v1/accounts/client-source/socket/ -o jms-pam-clients.tar.gz &&
tar -xzf jms-pam-clients.tar.gz
cd ./jms-pam-clients/socket
python3 python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
```

```python
"""Unix Socket WebSocket example, without a JumpServer SDK or application keys.

Observe: python python_example.py --socket /run/jms-pam-agent/agent.sock --service orders
For business processing, subclass Business and call run(socket, service, business).
"""
import argparse
import json
import time
from collections import deque
from urllib.parse import urlencode

from websockets.sync.client import unix_connect


class Business:
    def apply_credentials(self, credentials):
        """Verify new real connections, switch pools, then release old sessions."""
        raise NotImplementedError

    def revoke_accounts(self, account_ids):
        """Close sessions and discard credentials for explicitly revoked accounts."""
        raise NotImplementedError

    def restart(self):
        """Restart the business and verify that it is ready before returning."""
        raise NotImplementedError


class Session:
    def __init__(self, connection):
        self.connection = connection
        self.snapshots = deque(maxlen=1)
        self.sequence = 0

    def call(self, operation, **fields):
        self.sequence += 1
        request_id = str(self.sequence)
        self.connection.send(json.dumps(dict(request_id=request_id, operation=operation, **fields)))
        while True:
            response = json.loads(self.connection.recv())
            if response.get("request_id") == request_id:
                if "error" in response:
                    # Return only the protocol code, never credentials or server payloads.
                    raise RuntimeError(response["error"]["code"])
                return response["result"]
            if response.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(response)

    def snapshot(self):
        while not self.snapshots:
            message = json.loads(self.connection.recv())
            if message.get("event") in ("snapshot", "agent.access_denied"):
                self.snapshots.append(message)
        return self.snapshots.popleft()


def process(session, snapshot, business, completed, commands):
    delivery = snapshot.get("delivery")
    if delivery and snapshot.get("retry_ready"):
        delivery_id = delivery["delivery_id"]
        session.call("received", delivery_id=delivery_id)
        status = completed.get(delivery_id)
        if status is None:
            try:
                # Cleanup also runs when the credential set is empty.
                revoked = delivery.get("revoked_account_ids", [])
                if revoked:
                    business.revoke_accounts(revoked)
                if delivery["credentials"]:
                    credentials = session.call("credentials", delivery_id=delivery_id)
                    business.apply_credentials(credentials)
                status = "success"
                # Remember before sending the result, so a lost response doesn't
                # replay successful work on the next reconnect in this process.
                completed[delivery_id] = status
            except Exception:
                status = "failed"
        session.call("result", delivery_id=delivery_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    for command in snapshot.get("commands", []):
        if command["status"] != "pending":
            continue
        command_id = command["command_id"]
        status = commands.get(command_id)
        if status is None:
            try:
                business.restart()
                status = "success"
            except Exception:
                status = "failed"
            commands[command_id] = status
        session.call("command.result", command_id=command_id, status=status,
                     error_code="application_failed" if status == "failed" else "")
    # Bound process-local de-duplication. Persist delivery IDs in your business
    # transaction if callbacks must survive a crash between effect and ACK.
    for history in (completed, commands):
        while len(history) > 256:
            del history[next(iter(history))]


def run(socket_path, service_id, business=None):
    uri = "ws://localhost/v1/events?" + urlencode({"service_id": service_id})
    completed, commands = {}, {}
    delay = 1
    while True:
        try:
            # AF_UNIX is used here; localhost is the HTTP Host, not a TCP target.
            with unix_connect(socket_path, uri=uri, open_timeout=5, max_size=1024 * 1024) as connection:
                session = Session(connection)
                delay = 1
                initialize = True
                while True:
                    snapshot = session.snapshot()
                    if business is None:
                        delivery = snapshot.get("delivery") or {}
                        print(json.dumps({"service_id": service_id, "event": snapshot["event"],
                                          "delivery_id": delivery.get("delivery_id"),
                                          "result": snapshot.get("result")}), flush=True)
                    else:
                        # A new process must rebuild its own connections even
                        # when a previous process applied this Agent delivery.
                        if initialize and snapshot.get("result") == "success":
                            snapshot["retry_ready"] = True
                        process(session, snapshot, business, completed, commands)
                        initialize = False
        except KeyboardInterrupt:
            return
        except Exception as error:
            # Errors may contain arbitrary business details; print only the type.
            print("Socket connection interrupted:", type(error).__name__, flush=True)
            time.sleep(delay)
            delay = min(delay * 2, 30)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--socket", required=True)
    parser.add_argument("--service", required=True)
    options = parser.parse_args()
    # The executable example observes only. It never claims business success.
    run(options.socket, options.service)
```
<!-- socket-doc:end -->
