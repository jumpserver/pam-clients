# JumpServer PAM Java SDK

<!-- sdk-doc:start -->

## 1. Prerequisites and workflow

1. Create an application and authorize the accounts it may retrieve.
2. Enable Account credential change notifications in the application when immediate updates are needed. This covers all authorized accounts. A/B switching uses a separate rotation policy.
3. Download the application access materials and protect its AK/SK. Give each replica a stable, unique instance ID.

Use only the retrieval section for pull-only access. For pushed updates or A/B rotation, implement the managed business callbacks and start listening. The SDK handles protocol and event results; the application validates and switches real connections. These examples reflect the current source: install a matching Core and SDK build before using new APIs; released 1.0.2 packages do not include all of them.

- JDK 11+ / Maven / Jackson

## 2. Installation and identity

Install the SDK below and use the identity configuration downloaded from the application access wizard. Protect AK/SK and assign each replica a stable, unique instance ID.

Install the client source shipped by the same Core using the commands below; released v1.0.2 packages do not include managed callbacks. Replace jumpserver.example.com with your Core address. Keep the extracted jms-pam-clients directory with your application, including the Go module replacement path.

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

### Local source installation (alternative)

If this Core does not provide the source download endpoint, or for offline deployment, obtain its matching client source at `/path/to/pam-clients` and run the following from the application directory. Replace the path with your actual checkout.

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

## 3. Fetch the first account

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

## 4. Listen for updates and switch connections

Implement the account-change callback below, then start the managed listener. The SDK handles snapshots, fresh account lookup, account/version validation, retry, command claims and event results. Return only after validating the real connection, switching the application pool and releasing old connections. Python and Node.js use subclass hooks, Go uses OnAccountChanged, and Java uses AccountEventListener. The placeholder fails until business switching is implemented.

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

The account callback runs serially behind a bounded event queue. Failed account updates retry with 1–30 second backoff. Success is reported only after the business callback returns; a failed result upload retries the upload without repeating a completed action in the same listener. Commands execute only after an accepted claim; failed command actions are not automatically repeated. Applied accounts are reported as a complete usage snapshot; A/B switches replace the current account. Implement revoked-account cleanup for scope removals. Usage reporting, fetches and file delivery never themselves confirm application success. Business handlers must remain idempotent across restarts.

### Lifecycle and shutdown

`watchEvents(listener)` / `startEvents(listener)`; `stop()` / `close()` / `awaitTermination()`

watchEvents waits in the calling thread. startEvents returns before initial synchronization is complete. One managed listener is allowed per client. EventSubscription.close and Client.close stop and wait for active handlers; a handler may close its own subscription or client. awaitTermination must be called externally.

### Latest credentials and backend outages

SDK credential getters request the API first. A successful fetch replaces the retained value without allowing revisions to move backwards. Only a timeout, network error or HTTP 5xx permits fallback to a previously fetched value for the same selector, marked as local; retained values have no time expiry. SDK caches are client-local memory: clones and restarts start empty. HTTP 401/403/404 or credential_not_found (HTTP 400) clear retained credentials and raise. HTTP 426 and malformed successful responses raise without using fallback; a missing optional metadata endpoint does not clear credentials. PAM Agent persists accounts declared by store/services in protected state; an undeclared on-demand query is not automatically retained. Explicit revocations take effect before HTTP refresh, including while offline. Push snapshots do not define the full authorization scope of stored accounts; successful reconciliation checks the current application authorization.

- `account.isFromLocal()`
- `getAccount(accountId, false)`

With managed event listening enabled, snapshot and credential.secret.updated / credential.switch.published / credential.refresh.requested automatically fetch the current credential, replace the retained value and then invoke the business hook. A failed refresh leaves the previous value in place and retries. The PAM Agent also refetches on update notifications and retains its previous credentials during backend outages. Refresh and manual switching use the live-only calls below; a retained password must not be treated as a newly fetched revision or automatically confirmed.




## 5. Common methods

### List authorized accounts

Use the account listing methods to discover the application’s current authorized pull scope. Listings return account and asset metadata plus related push policies, without retrieving passwords or recording credential fetches. Accounts without policies are included. The full-list method follows all pages; the page method returns `count` and `accounts` (Go returns the slice and total separately). Defaults are limit 200 and offset 0; limit is 1–500 and search is at most 128 characters, matching account name, username, asset name or address. Duplicate accounts, incomplete results and changing totals raise an error; retry the whole listing if authorization changes. Use an account ID with the account-fetch method when its secret is needed. Examples require SDK source containing these new methods; published version 1.0.2 does not yet provide them in Python, Java or Node.js.

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


### Application, replica and policy information

Read the authenticated application ID, name, organization and authorized account/policy counts with getApplication; getClientInfo returns only the current replica’s registration, type and versions. Policy listing returns the entire visible bound list, including disabled policies; search matches policy names or keys. Fetch a policy using exactly one of its ID or base policy key, rather than an account push key. Rotation policies require authorization for both accounts and the active account. Subscription counts include only accounts currently authorized to this application. These calls return metadata without secrets and do not fetch credentials or confirm application changes. As with account queries, the replica is registered on first access. They require updated Core and SDK source; the published 1.0.2 does not yet contain these methods.

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


### Application status reporting

Report a complete snapshot of the application state and the account IDs currently in use. States: `starting`, `running`, `degraded`, `stopping`, `stopped`, `error`. An empty account list clears previous usage. Call on state changes or from your own heartbeat; no background reporting is started. Strict variants expose failure details. Invalid arguments are rejected. Requests use the configured HTTP timeout; keep reporting off latency-sensitive paths. Reporting never fetches secrets, confirms rotation or invalidates retained credentials. Use machine-readable `error_code` only, without secrets or exception messages. `get_client_info` / `GetClientInfo` / `getClientInfo` return the last reported snapshot and its timestamp.

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

## 6. Verify the integration

1. Replace the account placeholder with an authorized account ID and verify a real connection without logging the password.
2. If listening for updates, verify initial synchronization and an authorized-account update or A/B switch; complete the business callback before expecting a successful event result.
3. Check the instance usage/status and event result separately. A fetch or usage report does not confirm credential application.

### Troubleshooting

HTTP, network, authentication and response decoding failures use the SDK exception/error type with code and HTTP status. Close streams and clients when finished. Clones have independent lifecycles. Retry transient failures at the application boundary and never log secrets or authentication headers.

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
| `rotation.source.waiting` | Source-account observation starts only after all participants confirm or skip application checks and an administrator has approved any failures. |
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

## Activity log and protocol event reference

Activity logs record audit actions; event logs show protocol notifications and commands with delivery and processing status. Audit actions and protocol notifications are different catalogs. One action may produce several records or notifications; related codes below are possible associations, not proof of delivery. Inspect the record details for the actual event and result. Receipt, retrieval, credential application, skip and administrator approval are separate steps. credential_fetched appears in the separate credential retrieval log.

| Activity type | Activity code | Related notifications / messages | Meaning |
| --- | --- | --- | --- |
| Send application command | `command_requested` | `credential.switch.requested`<br>`app.restart.requested` | An administrator asks selected instances to apply published credentials or restart the application; this does not start a rotation. |
| Application processing result | `command_result` | `credential.secret.updated`<br>`credential.switch.published`<br>`credential.refresh.requested`<br>`credential.switch.requested`<br>`app.restart.requested` | A client reports credential application or command progress/results. A rotation failure still needs administrator review after a successful retry. |
| Create integration or rotation configuration | `configuration_created` | `app.scope.updated` | An application or rotation policy was created; scope changes may cause app.scope.updated. |
| Update integration or rotation configuration | `configuration_updated` | `app.scope.updated` | Application or rotation settings changed; scope changes may cause app.scope.updated. |
| Delete integration or rotation configuration | `configuration_deleted` | No fixed protocol event | An application or rotation policy was deleted; this is not a credential application result. |
| Add credential authorization or binding | `authorization_granted` | `credential.refresh.requested` | Credential authorization or a rotation binding was added and a refresh requested; a binding does not expand account access. |
| Remove credential authorization or binding | `authorization_revoked` | `app.scope.updated` | Authorization or a rotation binding was removed. Release business connections only when account authorization is actually lost. |
| Fetch credentials | `credential_fetched` | No fixed protocol event | Records a credential retrieval in the separate retrieval log; fetching does not prove business use. |
| Account secret version updated | `credential_secret_updated` | `credential.secret.updated` | The account password version changed. Applications must fetch and apply it; the selected A/B account does not change. |
| Publish account switch | `credential_switch_published` | `credential.switch.published` | Publishes the A/B target or return account and policy revision; publication does not prove an application switched. |
| Request current credential application | `credential_refresh_requested` | `credential.refresh.requested` | Requests fetching and applying the published version; no password change or new rotation is implied. |
| Republish credential notification | `credential_republished` | `credential.refresh.requested`<br>`snapshot` | Manual refresh or snapshot reconciliation requests applying the current credentials again; the password version may be unchanged. |
| Confirm new credentials applied | `credential_confirmed` | No fixed protocol event | A client confirms successful application of the current account/policy version, separately from receipt and retrieval. |
| Skip application check | `credential_confirmation_skipped` | No fixed protocol event | Other delivery steps succeeded and the client reported skip for the optional application check; this is not a passed check. |
| Administrator approved continuation | `application_failure_reviewed` | No fixed protocol event | An administrator records a reason and releases the instance failure gate; the original failure is retained. |
| Credential event stream connected | `credential_stream_connected` | `snapshot` | A client connected to the notification stream; snapshot reconciles scope. Connection does not prove application success. |
| Credential event stream disconnected | `credential_stream_disconnected` | No fixed protocol event | A client left the notification stream; this does not by itself prove a business connection failed. |
| Rotation stage changed | `rotation_step` | `rotation.verify.started`<br>`rotation.verify.skipped`<br>`rotation.verify.failed`<br>`rotation.verify.cancelled`<br>`rotation.waiting_for_app`<br>`rotation.source.waiting`<br>`rotation.source.ready`<br>`rotation.completed`<br>`rotation.failed` | Records verification, application waiting, source-account observation and cycle outcomes; consult details for the actual stage. |
| Start account rotation | `rotation_started` | `rotation.started` | The target account was published and application switching began; the rotation is not yet complete. |
| Cancel or force-stop rotation | `rotation_cancelled` | `credential.switch.published`<br>`rotation.force_stopped` | Cancellation publishes the source account and waits for return confirmation; force-stop ends remaining recovery waits. |
| Account password change started | `secret_change_started` | `credential.change.started` | A password-change task was queued or started; the remote password may not have changed yet. |
| Account password change completed | `secret_change_completed` | `credential.change.completed` | The password-change task succeeded; this does not prove applications applied the credentials. |
| Account password change failed | `secret_change_failed` | `credential.change.failed` | The task failed or its outcome is uncertain; inspect the task and remote account. |
| Register client instance | `client_registered` | No fixed protocol event | An SDK or Agent instance registered; registration does not prove credential application. |
| Enable client instance | `client_enabled` | No fixed protocol event | The instance can resume access within its existing authorization; no new account authorization is granted. |
| Disable client instance | `client_disabled` | No fixed protocol event | The instance is disabled and subsequent access is rejected; account passwords are not changed by this action. |
| Application notification delivery | `notification` | No fixed protocol event | Records webhook delivery and retries. HTTP delivery does not prove credential application; see delivery details for the actual event code. |
| Reset application access secret | `application_secret_reset` | No fixed protocol event | Resets the application's authentication secret for JumpServer; this is separate from business account password rotation. |

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

Core sends snapshot on connection, reconnection and scope changes. Its credentials include all authorized accounts when application notifications are enabled, plus active authorized A/B rotation policies bound to the application. Process each item like credential.secret.updated / credential.switch.published / credential.refresh.requested using event_id, account_id and account_revision. The snapshot also includes authorized_account_ids; get_accounts lists authorized account metadata. Removing a rotation policy binding does not revoke account authorization.

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

The SDK automatically sends a best-effort received receipt before yielding a business event. No additional receipt is required from the application. A receipt means only that the event was read, not that credentials were applied; it does not replace confirmation. Reconcile the initial and reconnect snapshots; snapshot and pong require no receipt.

Results are success, failed or skip. A local service without app_check reports skip after its other stages succeed; this is not application confirmation. A failed result blocks the next stage, even after a successful or skipped retry. An administrator must approve continuation with a reason; the original failure is retained.

Administrator approval uses a user session with accounts.change_applicationcredential. Submit the current failure_id and a reason; superseded failures or policy revisions are rejected. Approval is audited and preserves the client result.

```http
POST /api/v1/accounts/application-credentials/<policy-id>/review-application-failure/
Content-Type: application/json

{"client_id": "<client-id>", "failure_id": "<current-failure-id>", "reason": "<review-reason>"}
```

```json
{"event": "received", "event_id": "<event-id>"}
```

Fetch the target account with `account_id`. Apply and verify the application change, then report `success` or `failed` using `event_id`. A delivery receipt or successful fetch does not mean the application succeeded. Events and reconnect snapshot items include `event_id`, `account_id` and `account_revision`. Verify the account version before applying it. Restart and manual switch commands must first be claimed with `running`. Make application handlers idempotent; a reconnect may repeat an event.

## Command execution and results

Administrators select target instances and an expiry time. Use credential.switch.requested to apply the published rotation target, and app.restart.requested for application operations.

1. Poll GET /api/v1/accounts/credential-client/commands/ or receive commands through WebSocket.
2. Claim through POST /api/v1/accounts/credential-client/command-result/ with status: running. Execute only when accepted is true.
3. Report success after actual switching or a successful restart health check; report failed with a safe error code on failure.

Managed SDK callbacks claim and report automatically. received means receipt only. Automatic rotation progress events update progress without repeating command execution.

## Connection liveness

Event connections send application ping messages at 10-second intervals while idle and reconnect after approximately 30 seconds without messages. Reconnect uses 1–30 second exponential backoff and fresh signatures. Reconnect snapshots restore current state; past events are not replayed.

<!-- protocol-doc:end -->


<!-- socket-doc:start -->
# Socket API access

Use a local PAM Agent from any application without installing a JumpServer SDK or storing application AK/SK in the business process. The Agent handles Core authentication, authorization, credential refresh, A/B resolution and result retries. The application owns real connections and business validation.

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

Return the exact delivery_id with success or failed. The Agent persists accepted results before reporting to Core and waits for every relevant service before confirming a credential event. accepted means locally saved, not that Core has already accepted it. Duplicate successful results are idempotent; a superseded version returns HTTP 409. After failed, retry_ready becomes true after 1–30 seconds. If a result response is lost, retry the result rather than repeating successful business work. Callbacks must be idempotent: a crash between a business effect and its local ACK cannot guarantee exactly-once execution. Snapshot fields received, credentials_read and result track distinct local stages. core_status separately tracks pending, confirmed, failed or not_required. Send the received operation to record a receipt; credential retrieval also records receipt. Neither operation confirms business success.

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

CLI/Socket secret queries and service credential reads return `account`, `source` and `last_pull_at`. `account.version` is the actual account credential version; ordinary results omit policy revisions, `account_revision` and `event_id`. `source` is `api` for a live Core fetch and `local` for retained data. `last_pull_at` is the RFC 3339 time of the last successful Core credential fetch for that account, even when its version is unchanged. It is retained with cached credentials across restarts; metadata sync, cache reads and failed pulls do not advance it. Older caches without a known pull time trigger one real credential fetch on the next online sync, even if the version is unchanged. Failed fetches leave the time unknown (null) and are retried; after success, unchanged metadata syncs do not fetch again. Pull-only queries do not enable offline credential retention.

`event_id` stays in notification metadata and script notification envelopes for reporting application success/failure; it is not an account field or a global latest-event pointer. Snapshots reuse the latest matching account/version/policy/client event, or create a republished event if none exists. Script readers use `credentials[selector].account`; existing Go templates, field mappings and EnvironmentFile variables keep their existing field names. Existing JSON readers must adopt the nested account structure.

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

Credential JSON files contain only `account`, with no `source` or `last_pull_at`. Pull times are exported separately to `last_pull_at.json` beside the directory configured by `store.root`, keyed by the actual account ID. The private state retains the same times. A successful live credential fetch updates the diagnostic file immediately, even for the same version, without rewriting credential files. Periodic version checks, cache reads and failed pulls do not advance it. Revoked accounts are removed from the diagnostic file.

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
