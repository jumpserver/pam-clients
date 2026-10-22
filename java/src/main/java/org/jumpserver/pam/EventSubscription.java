package org.jumpserver.pam;

import java.util.HashMap;
import java.util.Map;
import java.util.Set;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.ArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import org.jumpserver.pam.Models.Credential;
import org.jumpserver.pam.Models.Account;
import org.jumpserver.pam.Models.Event;

/** A managed listener. close() stops and waits; hooks may close their own subscription. */
public final class EventSubscription implements AutoCloseable {
  private final Client client;
  private final CredentialEventListener listener;
  private final EventStream stream;
  private final AtomicBoolean stopped = new AtomicBoolean();
  private final CountDownLatch finished = new CountDownLatch(1);
  private final Map<String, PendingCredential> pending = new HashMap<>();
  private final Map<String, String> appliedEvents = new LinkedHashMap<>();
  private final Map<String, String> usedAccounts = new HashMap<>();
  private Set<String> accountScope;
  private final Thread worker;

  private static final class PendingCredential {
    final Event update, event;
    final long delay, due;

    PendingCredential(Event update, Event event, long delay) {
      this.update = update;
      this.event = event;
      this.delay = delay;
      this.due = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(delay);
    }
  }

  EventSubscription(Client client, CredentialEventListener listener) {
    this.client = client;
    this.listener = java.util.Objects.requireNonNull(listener, "listener");
    this.stream = client.watchCredentialEvents();
    this.worker = new Thread(this::run, "jms-pam-event-handler");
    this.worker.setDaemon(true);
  }

  void start() {
    try {
      worker.start();
    } catch (RuntimeException | Error error) {
      stop();
      finished.countDown();
      throw error;
    }
  }

  public boolean isRunning() {
    return finished.getCount() != 0;
  }

  public void stop() {
    stopped.set(true);
    stream.close();
  }

  public void awaitTermination() throws InterruptedException {
    if (Thread.currentThread() == worker)
      throw new IllegalStateException("A listener cannot await its own termination");
    finished.await();
  }

  @Override
  public void close() {
    stop();
    if (Thread.currentThread() == worker) return;
    boolean interrupted = false;
    while (true) {
      try {
        finished.await();
        break;
      } catch (InterruptedException error) {
        interrupted = true;
      }
    }
    if (interrupted) Thread.currentThread().interrupt();
  }

  private void report(Exception error, Event event) {
    if (stopped.get()) return;
    try {
      listener.onEventError(error, event);
    } catch (RuntimeException hookError) {
      CredentialEventListener.logError(hookError);
    }
  }

  private static String selector(Event update) {
    if (update.getCredentialMode().equals("subscription") && !update.getAccountId().isEmpty()
        && !update.getKey().isEmpty()) {
      String key = update.getKey();
      if (!key.endsWith(":" + update.getAccountId())) key += ":" + update.getAccountId();
      return "key:" + key;
    }
    if (update.getCredentialMode().equals("alternating_rotation") && !update.getKey().isEmpty())
      return "key:" + update.getKey();
    return null;
  }

  private void refresh(Event update, Event event, long delay) {
    if (listener instanceof AccountEventListener) {
      refreshAccount(update, event, delay);
      return;
    }
    String target = selector(update);
    if (target == null || stopped.get()) return;
    try {
      PendingCredential previous = pending.get(target);
      if (previous != null
          && previous.update.getData().has("revision")
          && update.getData().has("revision")
          && previous.update.getRevision() > update.getRevision()) return;
      Credential credential = client.getCredential(target.substring(4), false);
      if (update.getData().has("revision") && credential.getRevision() < update.getRevision())
        throw new IllegalStateException("Credential API revision is behind the event");
      if (stopped.get()) return;
      listener.onCredentialChanged(credential);
      pending.remove(target);
    } catch (Exception error) {
      if (stopped.get()) return;
      report(error, event);
      pending.put(
          target, new PendingCredential(update, event, Math.min(Math.max(1000, delay * 2), 30000)));
    }
  }

  private static String accountSelector(Event event) {
    if (!event.getCommandId().isEmpty()) {
      String name = event.getEvent();
      return name.equals("credential.updated") || name.equals("credential.switch.requested")
              || name.equals("application.restart.requested")
          ? "command:" + event.getCommandId() : null;
    }
    return usageSelector(event);
  }

  private static String usageSelector(Event event) {
    if (event.getCredentialMode().equals("alternating_rotation") && !event.getKey().isEmpty())
      return "policy:" + event.getKey();
    if (event.getCredentialMode().equals("subscription") && !event.getAccountId().isEmpty())
      return "account:" + event.getAccountId();
    return null;
  }

  private void rememberApplication(String id, String result) {
    appliedEvents.put(id, result);
    if (appliedEvents.size() > 256) appliedEvents.remove(appliedEvents.keySet().iterator().next());
  }

  private void reportAccountUsage() {
    try { client.reportAccounts(new ArrayList<>(new java.util.TreeSet<>(usedAccounts.values()))); }
    catch (RuntimeException ignored) { /* Telemetry cannot repeat a business action. */ }
  }

  private void refreshAccount(Event update, Event event, long delay) {
    String target = accountSelector(update);
    if (target == null || update.getEventId().isEmpty() || stopped.get()) return;
    if (!update.getEvent().equals("application.restart.requested")
        && accountScope != null && !accountScope.contains(usageSelector(update))) return;
    PendingCredential previous = pending.get(target);
    if (previous != null && previous.update.getRevision() > update.getRevision()) return;
    try {
      String result = appliedEvents.get(update.getEventId());
      if (result == null) {
        if (!update.getCommandId().isEmpty()
            && !client.reportApplicationCommandResult(update.getCommandId(), "running", null).isAccepted()) {
          pending.remove(target);
          return;
        }
        try {
          AccountEventListener accounts = (AccountEventListener) listener;
          if (update.getEvent().equals("application.restart.requested")) accounts.onApplicationRestart();
          else {
            if (!update.getCredentialMode().equals("subscription")
                && !update.getCredentialMode().equals("alternating_rotation"))
              throw new IllegalArgumentException("Unsupported credential mode");
            Account account = client.getAccount(update.getAccountId(), false);
            if (!account.getId().equals(update.getAccountId()) || update.getAccountRevision() < 0 || account.getRevision() != update.getAccountRevision())
              throw new IllegalStateException("Event account version is superseded");
            if (stopped.get()) return;
            accounts.onAccountChanged(account);
            String usage = update.getCredentialMode().equals("alternating_rotation")
                ? "policy:" + update.getKey() : "account:" + account.getId();
            usedAccounts.put(usage, account.getId());
          }
        } catch (Exception error) {
          if (!update.getCommandId().isEmpty()) rememberApplication(update.getEventId(), "failed");
          else {
            try { client.confirmEvent(update.getEventId(), "failed", "application_failed"); }
            catch (RuntimeException ignored) { /* Preserve the business error. */ }
          }
          throw error;
        }
        result = "success";
        rememberApplication(update.getEventId(), result);
      }
      if (stopped.get()) return;
      client.confirmEvent(update.getEventId(), result, result.equals("success") ? "" : "application_failed");
      pending.remove(target);
      reportAccountUsage();
    } catch (Exception error) {
      if (stopped.get()) return;
      report(error, event);
      pending.put(target, new PendingCredential(update, event, Math.min(Math.max(1000, delay * 2), 30000)));
    }
  }

  private void revokeAccounts(Set<String> retained, Event event) {
    for (String target : new ArrayList<>(usedAccounts.keySet())) {
      if (retained.contains(target)) continue;
      try {
        ((AccountEventListener) listener).onAccountRevoked(usedAccounts.get(target));
        usedAccounts.remove(target);
        appliedEvents.clear();
      } catch (Exception error) { report(error, event); }
    }
    reportAccountUsage();
  }

  private void dispatch(Event event) {
    String name = event.getEvent();
    if (name.equals("snapshot")
        || name.equals("credential.revoked")
        || name.equals("application.scope.updated")) pending.clear();
    try {
      listener.onEvent(event);
    } catch (Exception error) {
      report(error, event);
    }
    if (stopped.get()) return;
    try {
      if (name.equals("snapshot")) {
        if (listener instanceof AccountEventListener) {
          Set<String> retained = new HashSet<>();
          for (Event update : event.getCredentials()) retained.add(accountSelector(update));
          accountScope = retained;
          revokeAccounts(retained, event);
        }
        for (Event update : event.getCredentials()) refresh(update, event, 0);
      } else if (listener instanceof AccountEventListener && !event.getCommandId().isEmpty()) {
        refresh(event, event, 0);
      } else if (name.equals("credential.updated") && event.getCommandId().isEmpty()) {
        refresh(event, event, 0);
      } else if (name.equals("credential.revoked")) {
        if (listener instanceof AccountEventListener) {
          if (accountScope != null) {
            accountScope.remove("policy:" + event.getKey());
            accountScope.remove("account:" + event.getAccountId());
          }
          Set<String> retained = new HashSet<>();
          for (Map.Entry<String, String> item : usedAccounts.entrySet())
            if (!item.getValue().equals(event.getAccountId()) && !item.getKey().equals("policy:" + event.getKey()))
              retained.add(item.getKey());
          revokeAccounts(retained, event);
        }
        listener.onCredentialRevoked(event);
      }
    } catch (Exception error) {
      report(error, event);
    }
  }

  private void retry() {
    PendingCredential earliest = null;
    for (PendingCredential item : pending.values())
      if (earliest == null || item.due < earliest.due) earliest = item;
    if (earliest != null && earliest.due <= System.nanoTime())
      refresh(earliest.update, earliest.event, earliest.delay);
  }

  private void run() {
    try {
      while (!stopped.get() && !stream.isClosed()) {
        Event event = stream.poll(100);
        if (event != null) dispatch(event);
        retry();
      }
    } catch (InterruptedException error) {
      Thread.currentThread().interrupt();
    } catch (Exception error) {
      report(error, null);
    } finally {
      stop();
      pending.clear();
      finished.countDown();
    }
  }
}
