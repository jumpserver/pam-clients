package pam

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRevocationRetrySurvivesIdleAndCancelsOnRegrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"accepted":true}`)) }))
	defer server.Close()
	client, err := NewClient(Options{Endpoint: server.URL, AppID: "app", AppSecret: "secret", InstanceID: "instance"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	a, b := CredentialSelector{AccountID: "a"}, CredentialSelector{AccountID: "b"}
	count := map[string]int{}
	w := &EventWatcher{client: client, ctx: context.Background(), pending: map[CredentialSelector]pendingCredential{},
		usedAccounts: map[CredentialSelector]string{a: "a", b: "b"}, appliedEvents: map[string]accountEventResult{},
		handlers: EventHandlers{OnAccountRevoked: func(_ context.Context, id string) error {
			count[id]++
			if id == "a" && count[id] == 1 {
				return errors.New("temporary cleanup failure")
			}
			return nil
		}, OnError: func(context.Context, error, *Event) {}},
	}
	w.revokeAccounts(map[CredentialSelector]bool{}, Event{Event: "snapshot"})
	if count["a"] != 1 || count["b"] != 1 || len(w.pendingRevocations) != 1 {
		t.Fatal("one cleanup failure blocked another account")
	}
	item := w.pendingRevocations[a]
	item.due = time.Now().Add(-time.Second)
	w.pendingRevocations[a] = item
	w.retry()
	if count["a"] != 2 || len(w.pendingRevocations) != 0 || len(w.usedAccounts) != 0 {
		t.Fatal("idle cleanup did not converge")
	}
	count["a"] = 0
	w.usedAccounts[a] = "a"
	w.revokeAccounts(map[CredentialSelector]bool{}, Event{Event: "snapshot"})
	w.revokeAccounts(map[CredentialSelector]bool{a: true}, Event{Event: "snapshot"})
	w.retry()
	if count["a"] != 1 || len(w.pendingRevocations) != 0 || w.usedAccounts[a] != "a" {
		t.Fatal("regrant did not cancel cleanup")
	}
}
