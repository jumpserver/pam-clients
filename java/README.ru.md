# JumpServer PAM Java SDK

<!-- sdk-doc:start -->

## 1. Условия и порядок подключения

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Для pull достаточно первого запроса. Для push и A/B реализуйте бизнес-обработчики и слушатель. SDK ведёт протокол и результаты, приложение переключает реальные соединения. Примеры для текущих исходников: нужны совместимые сборки Core и SDK. Опубликованные пакеты 1.0.2 содержат не все новые API.

- JDK 11+ / Maven / Jackson

## 2. Установка SDK и настройка идентификации

Install the SDK below and use the identity configuration downloaded from the application access wizard. Protect AK/SK and assign each replica a stable, unique instance ID.

Установите исходный код клиента из той же версии Core; пакеты 1.0.2 не содержат управляемых обработчиков. Замените jumpserver.example.com адресом Core и сохраните каталог jms-pam-clients, включая локальный путь замены модуля Go.

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

### Альтернатива: локальные исходники

Если Core не предоставляет API загрузки исходников или установка автономная, подготовьте совместимые исходники в `/path/to/pam-clients` и выполните команды из каталога приложения. Замените путь.

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

## 3. Первое получение учётных данных

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

## 4. Обработчики событий

Реализуйте обработчик смены аккаунта и запустите управляемое прослушивание. SDK обрабатывает снимки, получение актуальных данных, проверку аккаунта и версии, повторы, получение права выполнения команды и результаты. Возвращайтесь только после проверки реального подключения, переключения пула и закрытия старых соединений. Python и Node.js используют подклассы, Go — OnAccountChanged, Java — AccountEventListener. Без реализации переключения пример завершается ошибкой.

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

Обработчики выполняются последовательно через ограниченную очередь; обычные обновления повторяются с задержкой 1–30 секунд. Успех сообщается только после завершения обработчика. При сбое отправки результата завершённое действие в том же слушателе не повторяется. Команды требуют подтверждённого права выполнения; неудачные действия команд автоматически не повторяются. Применённые аккаунты сообщаются автоматически, при A/B-переключении текущий аккаунт заменяется. Реализуйте закрытие отозванных соединений. Отчёт об использовании, получение пароля и доставка файла сами не подтверждают успех приложения. Старые API сохраняют явное подтверждение. Обеспечьте идемпотентность после перезапуска.

### Жизненный цикл и завершение

`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`

watchEvents ждёт в вызывающем потоке; startEvents не гарантирует синхронизацию. Один слушатель на клиент. close ждёт обработчик, который может закрыть свой клиент. awaitTermination вызывается извне.

### Последние учётные данные и недоступность сервера

SDK сначала обращается к API и обновляет сохранённое значение без отката версии. Только тайм-аут, сетевая ошибка или HTTP 5xx допускают прежнее значение того же селектора с отметкой локального источника и без временного срока. Клоны и перезапуски SDK начинают с пустого кэша. HTTP 401/403/404 или credential_not_found (400) удаляют значения; 426 и неверные успешные ответы завершаются ошибкой без кэша. Отсутствие необязательного API метаданных кэш не очищает. PAM Agent сохраняет записи, объявленные в store/services; необъявленный разовый запрос автоматически не сохраняется. Явный отзыв применяется до HTTP-обновления даже офлайн. Push-снимки не определяют все права хранения; успешная сверка проверяет текущие права приложения.

- `account.isFromLocal()`
- `getAccount(accountId, false)`

При управляемой подписке snapshot и credential.secret.updated / credential.switch.published / credential.refresh.requested автоматически получают данные, заменяют локальное значение и вызывают обработчик. Ошибка обновления сохраняет прежнее значение и запускает повтор. PAM Agent также получает данные по уведомлениям и сохраняет их при сбоях сервера. Для обновления и ручного переключения используйте вызовы с обязательным запросом API ниже. Сохранённое значение не считается новой полученной версией и не подтверждает ротацию автоматически.




## 5. Основные методы

### Список разрешённых аккаунтов

Методы списка возвращают текущую область pull-доступа приложения: метаданные аккаунтов, активов и push-политик без чтения паролей и записи получения секретов. Включены аккаунты без политик. Полный список читает все страницы; метод страницы возвращает `count` и `accounts` (Go возвращает список и число отдельно). По умолчанию limit 200, offset 0; limit 1–500, search до 128 символов ищет по имени аккаунта, пользователю, имени актива или адресу. Дубликаты, неполные результаты и изменение числа вызывают ошибку; при изменении доступа повторите полный запрос. Для получения секрета используйте ID аккаунта. Примеры требуют исходный код SDK с новыми методами; опубликованная 1.0.2 ещё не содержит их в Python, Java или Node.js.

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


### Информация о приложении, экземпляре и политиках

getApplication возвращает ID, имя, организацию и число разрешённых аккаунтов и видимых политик подписавшего приложения. getClientInfo возвращает регистрацию, тип и версии только текущей реплики. Список возвращает все видимые привязанные политики, включая отключённые; search ищет по имени или ключу. Для одной политики используйте ID либо собственный ключ политики, а не push-ключ аккаунта. Ротация требует доступа к обоим аккаунтам и активному аккаунту; число подписок учитывает только разрешённые аккаунты. Запросы не возвращают секреты, не получают и не подтверждают учётные данные. Первый доступ регистрирует реплику. Нужны обновлённые Core и исходный код SDK; в опубликованной 1.0.2 методов пока нет.

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


### Отчёт о состоянии приложения

Передавайте полный снимок состояния и ID используемых аккаунтов. Состояния: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. Пустой список очищает прежние сведения. Вызывайте при изменении состояния или из heartbeat приложения. Python, Java и Node.js возвращают false при ошибках сети/API; Go предоставляет `TryReportStatus`. Строгие варианты возвращают подробности ошибок. Неверные аргументы отклоняются. Используется настроенный HTTP timeout. Отчёт не получает секреты, не подтверждает ротацию и не очищает кеш. Не передавайте секреты и тексты исключений в `error_code`. ClientInfo возвращает последний отчёт и его время.

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

## 6. Проверка интеграции

1. Получите разрешённый аккаунт и проверьте реальное соединение.
2. Проверьте начальную синхронизацию и обновление; успех — после завершения обработчика.
3. Отдельно проверьте статус/использование и результат события. Получение и отчёт не подтверждают применение.

### Устранение неполадок

Ошибки HTTP, сети, аутентификации и декодирования представлены типом ошибки SDK с кодом и HTTP-статусом. Закрывайте потоки и клиенты после использования; жизненные циклы копий независимы. Повторяйте временные сбои на уровне приложения и не записывайте секреты или заголовки авторизации.

`PAMException`



<!-- sdk-doc:end -->

<!-- overview-doc:start -->

# Application integration overview

## Authorization, notifications and rotation

1. **Account authorization** controls which accounts the application may fetch on demand. Authorize the application before installing a client.
2. **Account credential change notifications** is an application-level switch covering all authorized accounts. Use it when applications should react to password changes; no subscription policy is required.
3. **A/B rotation** is a separately bound policy for switching between two accounts before changing the original password. Both accounts must be authorized. Open the policy’s operation guide for its cycle, waiting conditions, cancellation and recovery.

## Choose an integration

Choose by who applies PAM-managed usernames and passwords to the service. Socket API is an application integration through PAM Agent: the application calls a local API while the Agent handles communication with PAM.

| Method | Integration characteristics | Responsibilities |
| --- | --- | --- |
| PAM Agent | Integrate through configuration, templates or adaptation scripts; suitable for existing services. | The Agent retrieves and refreshes the accounts required by the service. Configure field mappings, templates or adaptation scripts to update service configuration, reload or restart the service, and verify that the credentials are in use. |
| SDK | Use the SDK for the application language and call it directly from business code. | The application embeds a language-specific SDK and connects directly to PAM. The SDK handles PAM communication and event processing; the application implements connection validation, credential changes and release of old connections in business callbacks. |
| Socket API | Cross-language integration without embedding a JumpServer SDK; the Agent can be upgraded independently of the application. | The application uses HTTP / WebSocket over the local Agent's Unix Socket to retrieve credentials, receive notifications and report results. The Agent handles PAM authentication, credential refresh and reporting retries; the application validates and switches its own connections. |

Agent automation follows the configured service adaptation and checks. For every method, successful credential retrieval or delivery alone does not prove that the service has switched successfully.

## Setup and acceptance

1. Create an application, authorize accounts and download its private identity configuration.
2. Choose Agent service adaptation, an embedded SDK or the Agent's Socket API according to who will apply credentials. Install the matching components, fetch an account and verify a real connection.
3. For automatic updates, enable application notifications and configure Agent business actions or implement application connection changes. For A/B switching, also bind a rotation policy and enable following its current account.
4. Verify one account update or A/B switch: the application uses the new credential, old connections are released and the business result is successful. Reception, fetching and file delivery alone are insufficient.

## Where to find the details

Agent integration covers deployment and service adaptation: credential files, configuration updates, reload/restart actions and validation. SDK integration covers language-specific installation and business callbacks. Socket API access covers local API calls, credential retrieval, notifications, business results and A/B switching. Events and commands is the shared reference for event payloads, snapshots, receipts and administrator commands. The operation guide inside each rotation policy explains its own cycle, blockers and recovery.

<!-- overview-doc:end -->


<!-- protocol-doc:start -->

# Events and commands

This reference applies to all SDK languages and PAM Agent. Prefer managed SDK callbacks or configured Agent services for normal integration; use the raw protocol only when implementing an event consumer yourself.

## Event responsibilities

| Event | Responsibility |
| --- | --- |
| credential.secret.updated / credential.switch.published / credential.refresh.requested and snapshot items | Fetch the named account, verify its version, apply it and report the business result. Managed SDK callbacks handle the protocol. |
| app.scope.updated | Reconcile scope and remove invalid state. Removing a rotation binding does not revoke account authorization. |
| `rotation.*` (such as `rotation.started` and `rotation.completed`), `credential.change.*` (such as `credential.change.started` and `credential.change.completed`) | Update progress, logs or alerts. Do not repeat credential application or confirm business success for these notifications. |
| credential.switch.requested and app.restart.requested | Administrator commands for selected instances: claim, execute and report the command result. |

## Complete event catalog

Current application events, rotation operation commands and connection control messages. Rotation details display progress; each event has one execution responsibility.

### Credential and scope events

credential.secret.updated / credential.switch.published / credential.refresh.requested applies credentials. app.scope.updated and snapshot reconcile scope: detach a missing rotation policy while preserving use of authorized accounts. Only actual account authorization removal disables local access, clears retained credentials and asks the application to release connections.

| Event code | Meaning |
| --- | --- |
| `credential.secret.updated` | The account secret version changed. Fetch and apply the named account; this does not change the selected account. |
| `credential.switch.published` | A target account and policy revision were published for switching, including return to the source account. Fetch and apply the named target; publication is not application success. |
| `credential.refresh.requested` | Fetch and apply the currently published credentials for an authorization grant, manual refresh or snapshot reconciliation. This does not mean the password changed or a rotation started. |
| `app.scope.updated` | Account authorization, notification settings or rotation bindings changed. Use authorized_account_ids to determine account access, then snapshot to reconcile notifications and rotation following. Removing a binding must not close an otherwise authorized account connection. |

### General password-change events

General task lifecycle notifications for progress and alerts. Do not fetch again or report application success for these notifications; apply credentials on credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot.

| Event code | Meaning |
| --- | --- |
| `credential.change.started` | A password-change task is queued or running; the remote password may not have changed yet. |
| `credential.change.completed` | The password-change task succeeded; this is not proof that applications applied the credentials. |
| `credential.change.failed` | The password change failed or could not be verified; inspect the task and remote account. |

### Rotation progress events

These notifications explain cycle progress and outcomes. Application switching follows credential.secret.updated / credential.switch.published / credential.refresh.requested or snapshot, not a second action on each progress event.

| Event code | Meaning |
| --- | --- |
| `rotation.verify.started` | Backup-account login verification started. |
| `rotation.verify.skipped` | Backup-account login verification skipped by credential policy. |
| `rotation.verify.failed` | Backup verification failed; no switch is published. |
| `rotation.verify.cancelled` | Backup verification was cancelled. |
| `rotation.started` | The rotation cycle has published the target account and started switching. |
| `rotation.waiting_for_app` | Waiting for required application instances to apply and confirm the target. |
| `rotation.source.waiting` | Observe successful secret fetches for the original account from switch publication onward, in parallel with application confirmation. Password change requires both conditions to be satisfied. |
| `rotation.source.ready` | The original account no-fetch window and required client conditions allow password change. |
| `rotation.completed` | The original account password change was verified and the rotation completed. |
| `rotation.failed` | Rotation entered a password-change failure or recovery-required state. |
| `rotation.force_stopped` | An administrator ended the cycle without waiting for remaining recovery confirmations. |

### Manual account rotation operations

credential.switch.requested asks selected instances to apply the account and revision already published by the rotation policy. Administrators can manually apply or retry the switch; the command does not start a new rotation.

| Event code | Meaning |
| --- | --- |
| `credential.switch.requested` | Apply the already published account and version on selected instances; this does not start a rotation. |

### Application operation commands

app.restart.requested restarts the application and verifies its health. Manual account switching belongs to the account rotation operations above.

| Event code | Meaning |
| --- | --- |
| `app.restart.requested` | Execute the supported application restart action and verify health. |

### Connection and receipt messages

SDK and Agent manage liveness and receipts. snapshot and pong need no received receipt; snapshot credential items are applied separately.

| Event code | Meaning |
| --- | --- |
| `snapshot` | Core sends the current notification scope and authorized account IDs on connection, reconnection or scope changes. |
| `ping` | The client sends an application-level keepalive to Core. |
| `pong` | Core replies to an application-level ping. |
| `received` | The client acknowledges reading an event; it does not report business success. |

## Payload fields

| Field | Meaning |
| --- | --- |
| event_id | Event identity for receipts and application results. snapshot itself has no receipt; credential items can have their own event_id. |
| event / occurred_at | Event type and publication time. |
| account_id / account_revision | Concrete target account and password version. A fresh account revision must match before application. |
| credential_key / revision | Policy publication metadata for A/B rotation. Ordinary account notifications use account revisions. Do not mix the two version domains. |
| credential_mode | alternating_rotation for an A/B policy; subscription for application-level account notifications, without a subscription policy. Application-wide events may use null. |
| account_switch.account_ids | Accounts associated with an A/B switch. |
| operation_id | Correlates events belonging to one change operation or rotation cycle. |
| command_id / expires_at | Administrator command identity and execution deadline. Commands have a separate result API. |

## Snapshots and authorization

Core отправляет snapshot при подключении, переподключении и изменении области. credentials содержит все разрешённые учётные записи при включённых уведомлениях, а также привязанные активные и разрешённые политики A/B. Обрабатывайте каждый элемент как credential.secret.updated / credential.switch.published / credential.refresh.requested с event_id, account_id и account_revision. authorized_account_ids задаёт область доступа; get_accounts возвращает метаданные. Отвязка политики не отзывает доступ к учётной записи.

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

## Receipts and business results

SDK автоматически пытается отправить квитанцию received перед передачей бизнес-события. Приложению не нужно отправлять её повторно. Квитанция означает только чтение события, а не применение учётных данных, и не заменяет подтверждение. Сверяйте начальный снимок и снимки при переподключении; snapshot и pong не требуют квитанций.

```json
{"event": "received", "event_id": "<event-id>"}
```

Получите аккаунт по `account_id`. После проверки подключения и применения изменений передайте `success` или `failed` по `event_id`. Получение события или секрета не означает успешное применение. События и снимки содержат `event_id`, `account_id`, `account_revision`; проверяйте версию. Сначала захватывайте команды со статусом `running`; обработка должна быть идемпотентной.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

При простое ping отправляется каждые 10 секунд. Около 30 секунд без сообщений вызывают переподключение с задержкой 1–30 секунд и новой подписью. snapshot восстанавливает текущее состояние без воспроизведения истории.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Подключение через Socket API

Приложение подключается к локальному PAM Agent без SDK JumpServer и без хранения AK/SK в бизнес-процессе. Используется Unix Domain Socket, а не TCP. Ниже приведён полный справочник протокола на английском языке.

## 1. Unix Socket / HTTP / WebSocket

The transport is a Unix Domain Socket (AF_UNIX), not TCP. Connect to its filesystem path; no IP address or port is opened. HTTP/JSON requests and the WebSocket event channel share this socket. The localhost URL supplies the HTTP Host only. Agent-to-Core traffic still uses HTTPS/WSS.

## 2. Agent / services

Add a socket service to the Agent configuration. Use the same account declarations and allow_account_switch rules as file delivery. The declared A ID remains the map key while account_id identifies the active A/B account. Socket services cannot also configure update_config, reload_service, credential_check or app_check. restart_supported is an explicit opt-in for business restart commands.

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

Connect, read the initial snapshot, retrieve the delivery credentials, verify new real connections, replace business connections and release old sessions, then submit success. Receiving a notification or fetching a password never proves that the business has switched. A failed callback submits failed and is retried after the Agent backoff.

Every connection starts with the latest snapshot; this is current-state reconciliation, not historical replay. delivery contains metadata only, including delivery_id, declared-account keys and exact account/policy revisions. Revocations are delivered even during a Core outage; revoked_account_ids requires closing old sessions. With no complete credentials, a delivery may be absent while refresh is pending. Each independently applied business instance needs its own service_id. Only one WebSocket connection per service is accepted.

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
    "event": "credential.refresh.requested",
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

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution.

## 4. Commands / status

Commands are claimed by the Agent before delegation. Restart commands appear in commands only for services with restart_supported=true. Report command.result after restart and readiness validation; success or failure is persisted and Core reporting is retried without executing the command again. A/B switch commands reuse credential delivery and its business result. Expired commands cannot be acknowledged.

```json
{"request_id":"4","operation":"command.result","command_id":"<command-id>","status":"success"}
```

Business status is separate from credential confirmation. Use starting, running, degraded, stopping, stopped or error and a short non-secret error_code. Account usage is updated only after business success, and the Agent aggregates all service reports. GET /v1/usage returns the aggregate. The existing POST /v1/usage replaces the instance-wide explicit account declaration; it does not acknowledge events.

```json
{"request_id":"5","operation":"status","status":"degraded","error_code":"database_unavailable"}
```

## 5. API

Запросы CLI/Socket и чтение данных сервисами возвращают `account`, `source` и `last_pull_at`. `account.version` — версия учётных данных самой записи; обычные результаты не содержат версию политики, `account_revision` и `event_id`. `source` равен `api` при запросе Core и `local` для кэша. `last_pull_at` — время последнего успешного получения данных этой записи из Core в формате RFC 3339, даже если версия не изменилась. Оно сохраняется с кэшем после перезапуска. Синхронизация метаданных, чтение кэша и ошибки его не обновляют. Старый кэш без известного времени возвращает null. Запрос по требованию сам по себе не включает автономное хранение.

`event_id` остаётся в метаданных уведомлений и оболочках скриптов для отчёта о результате. Снимок повторно использует последнее событие с подходящими записью, версией, политикой и клиентом либо создаёт событие повторной публикации. Скрипты читают `credentials[selector].account`. Имена полей Go-шаблонов, сопоставлений и EnvironmentFile сохраняются. Читатели JSON должны перейти на вложенную структуру account.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  },
  "source": "local",
  "last_pull_at": "2026-10-30T06:30:00Z"
}
```

JSON-файлы учётных данных содержат только `account`, без `source` и `last_pull_at`. Время получения экспортируется по фактическому ID записи в `last_pull_at.json` рядом с каталогом `store.root` и сохраняется в закрытом состоянии. Успешный прямой запрос сразу обновляет диагностику даже при той же версии, не перезаписывая файлы учётных данных. Проверки версий, чтение кэша и ошибки время не меняют. Отозванные записи удаляются из диагностики.

```json
{
  "account": {
    "id": "<active-account-id>",
    "name": "database",
    "username": "db_app",
    "secret_type": "password",
    "secret": "<password>",
    "version": 3,
    "asset": {
      "id": "<asset-id>",
      "name": "database-server",
      "address": "10.0.0.10"
    }
  }
}
```

`last_pull_at.json`:

```json
{
  "<active-account-id>": "2026-10-30T06:30:00Z"
}
```

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

Unix socket ownership is the access boundary. service_id identifies a business consumer, not a separate security identity: processes able to open the same socket share the application authorization. Keep unrelated trust domains on separate Agent instances/sockets. Events omit passwords; retrieve credentials explicitly and never log them. Polling the service snapshot and posting results is supported when a language has no Unix-socket WebSocket client.

## 6. Python

The Python example uses websockets, not a JumpServer SDK. By default it only observes metadata and never acknowledges business success. Implement the Business callbacks and call run(socket_path, service_id, business) for real processing. On a new business process, initialize connections from the current delivery even when it was applied by the previous process. Persist business-side delivery IDs when crash-safe de-duplication is required.

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
