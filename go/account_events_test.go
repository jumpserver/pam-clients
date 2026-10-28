package pam

import (
	"context"
	"errors"
	"net/url"
	"reflect"
	"testing"
)

func managedWatcher(t *testing.T, instance string, handler func(context.Context, Account) error) *EventWatcher {
	t.Helper()
	client := contractClient(t, instance, "jms-pam")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &EventWatcher{client: client, ctx: ctx, cancel: cancel,
		handlers: EventHandlers{OnAccountChanged: handler, OnError: func(context.Context, error, *Event) {}},
		pending:  map[CredentialSelector]pendingCredential{}, appliedEvents: map[string]accountEventResult{},
		usedAccounts: map[CredentialSelector]string{}}
}

func managedUpdate() Event {
	return Event{Event: "credential.updated", EventID: "managed-event", AccountID: "account", AccountRevision: 2,
		Revision: 2, Key: "database", CredentialMode: "alternating_rotation"}
}

func TestManagedResultRetryDoesNotRepeatBusinessApplication(t *testing.T) {
	count := 0
	w := managedWatcher(t, "managed-go-result", func(ctx context.Context, account Account) error {
		count++
		if account.FromLocal {
			t.Error("managed event used a retained credential")
		}
		return nil
	})
	controlBackend(t, "managed-go-result", url.Values{"result_failures": {"1"}})
	update := managedUpdate()
	w.dispatch(update)
	if count != 1 || len(w.pending) != 1 {
		t.Fatal("failed acknowledgment was not retained")
	}
	w.refresh(update, update, 0)
	if count != 1 || len(w.pending) != 0 {
		t.Fatal("acknowledgment retried business application")
	}
	info, err := w.client.GetClientInfo(w.ctx)
	if err != nil || info.ApplicationStatus == nil || !reflect.DeepEqual(info.ApplicationStatus.AccountIDs, []string{"account"}) {
		t.Fatal("applied account usage was not reported", err)
	}
}

func TestManagedVersionMismatchAndTemporaryOutageNeverApply(t *testing.T) {
	count := 0
	w := managedWatcher(t, "managed-go-version", func(context.Context, Account) error { count++; return nil })
	update := managedUpdate()
	update.AccountRevision = 3
	w.dispatch(update)
	if count != 0 {
		t.Fatal("superseded account version reached the application")
	}
	controlBackend(t, "managed-go-version", url.Values{"fault": {"503"}})
	update.AccountRevision = 2
	w.dispatch(update)
	if count != 0 {
		t.Fatal("managed event used cached credentials during an outage")
	}
}

func TestManagedBusinessFailureRetriesAndRotationUsageReplacesAccount(t *testing.T) {
	count := 0
	w := managedWatcher(t, "managed-go-retry", func(context.Context, Account) error {
		count++
		if count == 1 {
			return errors.New("business connection failed")
		}
		return nil
	})
	update := managedUpdate()
	w.dispatch(update)
	if len(w.pending) != 1 {
		t.Fatal("business failure was not retained")
	}
	w.refresh(update, update, 0)
	if count != 2 || len(w.pending) != 0 {
		t.Fatal("business failure was not retried")
	}
	controlBackend(t, "managed-go-retry", url.Values{"revision": {"3"}})
	update.EventID, update.AccountID, update.AccountRevision, update.Revision = "event-b", "backup", 3, 3
	w.dispatch(update)
	info, err := w.client.GetClientInfo(w.ctx)
	if err != nil || !reflect.DeepEqual(info.ApplicationStatus.AccountIDs, []string{"backup"}) {
		t.Fatal("rotation did not replace the current account usage", err)
	}
}

func TestManagedSnapshotRevocationAndRegrant(t *testing.T) {
	count, revoked := 0, 0
	w := managedWatcher(t, "managed-go-revocation", func(context.Context, Account) error { count++; return nil })
	w.handlers.OnAccountRevoked = func(context.Context, string) error { revoked++; return nil }
	update := managedUpdate()
	w.dispatch(Event{Event: "snapshot", Credentials: []Event{update}})
	w.dispatch(Event{Event: "snapshot", Credentials: []Event{update}})
	if count != 1 {
		t.Fatal("reconnect applied the same event again")
	}
	w.dispatch(Event{Event: "snapshot"})
	if revoked != 1 || len(w.usedAccounts) != 0 {
		t.Fatal("scope removal did not release the account")
	}
	w.dispatch(update)
	if count != 1 {
		t.Fatal("an old queued event restored a revoked account")
	}
	w.dispatch(Event{Event: "snapshot", Credentials: []Event{update}})
	if count != 2 {
		t.Fatal("regranted account was not applied")
	}
}

func TestNotificationDisablePreservesAuthorizedAccount(t *testing.T) {
	applied, revoked := 0, 0
	w := managedWatcher(t, "managed-go-notification-off", func(context.Context, Account) error { applied++; return nil })
	w.handlers.OnAccountRevoked = func(context.Context, string) error { revoked++; return nil }
	update := managedUpdate()
	update.CredentialMode, update.Key = "subscription", "account:account"
	w.dispatch(Event{Event: "snapshot", Credentials: []Event{update}, AuthorizedAccountIDs: []string{"account"}})
	w.dispatch(Event{Event: "snapshot", AuthorizedAccountIDs: []string{"account"}})
	w.dispatch(update)
	if applied != 1 || revoked != 0 {
		t.Fatal("notification disable changed an authorized connection", applied, revoked)
	}
	w.dispatch(Event{Event: "snapshot", AuthorizedAccountIDs: []string{}})
	if revoked != 1 {
		t.Fatal("actual authorization revocation did not release the account")
	}
}
