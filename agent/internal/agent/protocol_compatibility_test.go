//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	pam "github.com/jumpserver/pam-clients/go"
)

func TestProtocolMismatchPreservesAgentAccessAndExplainsFailure(t *testing.T) {
	config, remote, service := fixture(t)
	ctx := context.Background()
	if err := service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	remote.fault = &pam.PAMError{Code: "client_upgrade_required", StatusCode: 426}
	if service.Sync(ctx) == nil || service.state.Denied || service.Health()["error_code"] != "client_upgrade_required" {
		t.Fatal("protocol mismatch was hidden or treated as identity revocation")
	}
	if _, err := service.LocalCredential("db"); err != nil {
		t.Fatal("protocol mismatch discarded the local credential")
	}
	if report := service.AccountUsageSnapshot(); report.Status != "degraded" || report.ErrorCode != "client_upgrade_required" {
		t.Fatal("usage report did not expose the compatibility failure")
	}
	for _, path := range []string{"/v1/accounts", "/v1/credential?account_id=account"} {
		result := httptest.NewRecorder()
		service.ServeHTTP(result, httptest.NewRequest("GET", path, nil))
		var data map[string]string
		if err := json.Unmarshal(result.Body.Bytes(), &data); err != nil || result.Code != 426 || data["code"] != "client_upgrade_required" {
			t.Fatal("local API misreported compatibility as a permission failure")
		}
	}
	restarted, err := New(config, remote)
	if err != nil || restarted.state.Denied || restarted.state.SyncError != "client_upgrade_required" {
		t.Fatal("restart lost compatibility status or denied access", err)
	}
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	if value, err := restarted.AccountCredential(ctx, "account"); err != nil || value.Source != "local" {
		t.Fatal("later temporary outage lost previously authorized cached access")
	}
	remote.fault = nil
	if err := restarted.Sync(ctx); err != nil || restarted.Health()["error_code"] != "" {
		t.Fatal("successful sync did not clear compatibility status", err)
	}
}

func TestFetchProtocolMismatchNeverConfirmsOrRevivesDeniedAccess(t *testing.T) {
	config, remote, service := fixture(t)
	ctx := context.Background()
	if err := service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	remote.metadata++
	remote.value.Revision++
	remote.credentialFault = &pam.PAMError{Code: "client_upgrade_required", StatusCode: 426}
	if service.Sync(ctx) == nil || service.state.Denied || remote.confirmed != 0 {
		t.Fatal("failed fresh fetch confirmed a rotation or revoked the identity")
	}
	if value, err := service.LocalCredential("db"); err != nil || value.Revision != 2 {
		t.Fatal("failed fresh fetch lost the last known credential")
	}
	remote.credentialFault = nil
	remote.fault = &pam.PAMError{Code: "client_disabled", StatusCode: 401}
	if service.Sync(ctx) == nil || !service.state.Denied {
		t.Fatal("real identity denial was ignored")
	}
	remote.fault = &pam.PAMError{Code: "client_upgrade_required", StatusCode: 426}
	_ = service.Sync(ctx)
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.LocalCredential("db"); err == nil {
		t.Fatal("later compatibility error revived an already denied identity")
	}
}
