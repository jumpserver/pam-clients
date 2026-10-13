package pam

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestStatusReportBestEffortAndCredentialIsolation(t *testing.T) {
	client := contractClient(t, "go-status", "jms-pam")
	ctx := context.Background()
	report := ApplicationStatus{Status: "running", AccountIDs: []string{"account", "backup"}}
	if !client.TryReportStatus(ctx, report) {
		t.Fatal("status report failed")
	}
	info, err := client.GetClientInfo(ctx)
	if err != nil || info.ApplicationStatus == nil || len(info.ApplicationStatus.AccountIDs) != 2 || info.DateStatusReported == nil {
		t.Fatalf("missing self status: %v", err)
	}
	if _, err := client.GetCredential(ctx, CredentialSelector{Key: "cache-503"}); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"fault_401", "fault_403", "fault_404", "fault_503", "fault_invalid", "fault_network"} {
		failure := ApplicationStatus{Status: "running", ErrorCode: code}
		if client.TryReportStatus(ctx, failure) {
			t.Fatalf("failure accepted: %s", code)
		}
		if client.ReportStatus(ctx, failure) == nil {
			t.Fatalf("strict failure missing: %s", code)
		}
	}
	cached, err := client.GetCredential(ctx, CredentialSelector{Key: "cache-503"})
	if err != nil || !cached.FromLocal {
		t.Fatalf("report invalidated credentials: %v", err)
	}
	if !client.TryReportStatus(ctx, ApplicationStatus{Status: "stopped"}) {
		t.Fatal("empty snapshot failed")
	}
	info, err = client.GetClientInfo(ctx)
	if err != nil || len(info.ApplicationStatus.AccountIDs) != 0 {
		t.Fatal("usage was not cleared")
	}
	if client.TryReportStatus(ctx, ApplicationStatus{Status: "unknown"}) {
		t.Fatal("invalid status accepted")
	}
	client.Close()
	if client.TryReportStatus(ctx, report) {
		t.Fatal("closed client reported success")
	}
	timeoutClient := contractClient(t, "go-status-timeout", "jms-pam")
	timeoutClient.http.Timeout = 50 * time.Millisecond
	if timeoutClient.TryReportStatus(ctx, ApplicationStatus{Status: "running", ErrorCode: "fault_timeout"}) {
		t.Fatal("timeout reported success")
	}
}

func TestApplicationHookPanicsAreIsolated(t *testing.T) {
	watcher := &EventWatcher{ctx: context.Background(), pending: make(map[CredentialSelector]pendingCredential)}
	watcher.handlers = EventHandlers{
		OnEvent:             func(context.Context, Event) error { panic("SECRET_PANIC") },
		OnError:             func(context.Context, error, *Event) { panic("SECRET_ERROR_HOOK") },
		OnCredentialRevoked: func(context.Context, Event) error { panic("SECRET_REVOKED") },
	}
	watcher.dispatch(Event{Event: "credential.revoked"})
	watcher.handlers.OnCredentialRevoked = func(context.Context, Event) error { return nil }
	watcher.dispatch(Event{Event: "credential.revoked"})
}

func TestCredentialAndCommandHookPanicsAreIsolated(t *testing.T) {
	client := contractClient(t, "go-panic-hooks", "jms-pam")
	watcher := &EventWatcher{client: client, ctx: context.Background(), pending: make(map[CredentialSelector]pendingCredential)}
	watcher.handlers.OnCredentialChanged = func(context.Context, Credential) error { panic("SECRET_CREDENTIAL_PANIC") }
	watcher.handlers.OnError = func(context.Context, error, *Event) { panic("SECRET_ERROR_HOOK") }
	event := Event{Event: "credential.updated", CredentialMode: "alternating_rotation", CredentialKey: "db", Revision: 2}
	watcher.refresh(event, event, 0)
	if len(watcher.pending) != 1 {
		t.Fatal("panicking credential hook did not schedule retry")
	}
	_, err := client.ExecuteApplicationCommand(context.Background(), Event{CommandID: "panic-command"},
		func(Event) error { panic("SECRET_COMMAND_PANIC") })
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("command panic was not safely reported")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = client.WatchCredentialEvents(ctx, func(Event) error { panic("SECRET_STREAM_PANIC") })
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatal("stream panic was not safely returned")
	}
}
