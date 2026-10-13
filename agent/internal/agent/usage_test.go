//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	pam "github.com/jumpserver/pam-clients/go"
)

func usageFixture(t *testing.T) (Config, *fakeRemote, *Agent) {
	t.Helper()
	config, remote, _ := fixture(t)
	config.Rules = []Rule{{
		Accounts:     []AccountSelector{{AccountID: "account-a", AllowAccountSwitch: true}},
		ConfigUpdate: &ConfigUpdate{Files: []File{{Path: filepath.Join(filepath.Dir(config.StateFile), "business.json")}}},
	}}
	remote.value.Account.ID = "account-a"
	remote.value.AccountSwitch = &pam.AccountSwitch{AccountIDs: []string{"account-a", "account-b"}}
	service, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	return config, remote, service
}

func TestUsageDeclarationAndRotationSurviveRestart(t *testing.T) {
	config, remote, service := usageFixture(t)
	if snapshot := service.AccountUsageSnapshot(); !reflect.DeepEqual(snapshot.AccountIDs, []string{"account-a"}) {
		t.Fatal("configured account is not represented before any password fetch")
	}
	for index, id := range []string{"account-a", "account-b", "account-a"} {
		remote.value.Account.ID = id
		remote.value.Revision = int64(index + 2)
		remote.metadata = remote.value.Revision
		if err := service.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
		report := remote.statusReports[len(remote.statusReports)-1]
		if report.Status != "running" || !reflect.DeepEqual(report.AccountIDs, []string{id}) {
			t.Fatalf("current usage did not follow rotation: %v", report.AccountIDs)
		}
		if remote.confirmed != 0 {
			t.Fatal("usage reporting confirmed a rotation")
		}
		raw, err := os.ReadFile(config.StateFile)
		if err != nil {
			t.Fatal(err)
		}
		var state map[string]json.RawMessage
		if err := json.Unmarshal(raw, &state); err != nil {
			t.Fatal(err)
		}
		var usage map[string]map[string]any
		if err := json.Unmarshal(state["account_usage"], &usage); err != nil {
			t.Fatal(err)
		}
		if _, found := usage["account:account-a"]["secret"]; found {
			t.Fatal("usage metadata contains a secret")
		}
		service, err = New(config, remote)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(service.AccountUsageSnapshot().AccountIDs, []string{id}) {
			t.Fatal("restart lost current rotation account")
		}
	}
	if err := privateFile(config.StateFile); err != nil {
		t.Fatal(err)
	}
}

func TestFailedSwitchPreservesUsageDespiteFetchingNewAccount(t *testing.T) {
	config, remote, service := usageFixture(t)
	if err := service.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote.value.Account.ID = "account-b"
	remote.value.Revision++
	remote.metadata = remote.value.Revision
	service.deliver = func(context.Context, Config, map[string]Credential, map[string]bool) error {
		return errors.New("delivery_failed")
	}
	if err := service.Sync(context.Background()); err == nil {
		t.Fatal("failed delivery returned success")
	}
	report := remote.statusReports[len(remote.statusReports)-1]
	if report.Status != "degraded" || !reflect.DeepEqual(report.AccountIDs, []string{"account-a"}) {
		t.Fatal("fetching target B changed current usage before successful delivery")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.AccountUsageSnapshot().AccountIDs, []string{"account-a"}) {
		t.Fatal("restart inferred usage from fetched B")
	}
	if err = restarted.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.AccountUsageSnapshot().AccountIDs, []string{"account-b"}) {
		t.Fatal("retry did not update usage")
	}
}

func TestUsageReportingFailuresDoNotAffectDeliveryOrConfirmation(t *testing.T) {
	_, remote, service := usageFixture(t)
	for _, code := range []int{403, 404, 503} {
		remote.statusFault = &pam.PAMError{Code: "report_unavailable", StatusCode: code}
		if err := service.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
		if service.state.Denied || service.state.Status != "success" {
			t.Fatal("reporting failure blocked credential access")
		}
		if _, err := service.LocalCredential("db"); err != nil {
			t.Fatal(err)
		}
	}
	if remote.confirmed != 0 {
		t.Fatal("usage reporting was treated as application confirmation")
	}
}

func TestUsageConfigurationRemovalAndLocalQuery(t *testing.T) {
	config, remote, service := usageFixture(t)
	if err := service.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	config.Rules = nil
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.AccountUsageSnapshot().AccountIDs) != 0 {
		t.Fatal("removed declaration is still in use")
	}
	request := httptest.NewRequest("GET", "/v1/usage", nil)
	response := httptest.NewRecorder()
	restarted.ServeHTTP(response, request)
	var report pam.ApplicationStatus
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || len(report.AccountIDs) != 0 {
		t.Fatal("local usage endpoint inferred use from credentials")
	}
}

func TestInitialSnapshotAtBAndLegacyStateUsageRecovery(t *testing.T) {
	config, remote, service := usageFixture(t)
	remote.value.Account.ID = "account-b"
	if err := service.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(service.AccountUsageSnapshot().AccountIDs, []string{"account-b"}) {
		t.Fatal("first snapshot did not resolve current B")
	}
	service.state.AccountUsage = nil
	if err := service.persist(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.AccountUsageSnapshot().AccountIDs, []string{"account-b"}) {
		t.Fatal("legacy delivered state was not restored")
	}
}
