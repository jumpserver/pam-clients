//go:build !windows

package agent

import (
	"context"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestCurrentServiceUsageAndExplicitReportsPersist(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	applySocket(t, a, "db")
	fetched := remote.fetched
	if err := a.ReportAccounts(ctx, []string{testC}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.AccountUsageSnapshot().AccountIDs, []string{testA, testC}) {
		t.Fatal("explicit report replaced service usage")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.AccountUsageSnapshot().AccountIDs, []string{testA, testC}) {
		t.Fatal("usage did not persist")
	}
	if err := restarted.ReportAccounts(ctx, []string{}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restarted.AccountUsageSnapshot().AccountIDs, []string{testA}) {
		t.Fatal("empty report removed service usage")
	}
	if remote.fetched != fetched {
		t.Fatal("usage reporting fetched a secret")
	}
}

func TestUsageChangesOnlyAfterApplicationSucceeds(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	applySocket(t, a, "db")
	remote.active, remote.rotation = testB, 2
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.AccountUsageSnapshot().AccountIDs, []string{testA}) {
		t.Fatal("fetch or delivery counted as application")
	}
	applySocket(t, a, "db")
	if !reflect.DeepEqual(a.AccountUsageSnapshot().AccountIDs, []string{testB}) {
		t.Fatal("successful application did not replace account usage")
	}
}

func TestUsageRejectsUnauthorizedAccountsAndMalformedReports(t *testing.T) {
	_, _, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	for _, ids := range [][]string{{"unauthorized"}, {testA, testA}, {""}} {
		if err := a.ReportAccounts(ctx, ids); err == nil {
			t.Fatal("invalid usage accepted")
		}
	}
	for _, body := range []string{`{}`, `{"account_ids":null}`, `{"account_ids":["unauthorized"]}`, `{"account_ids":[]} {}`, `{"account_ids":[],"extra":"unused"}`} {
		response := httptest.NewRecorder()
		a.ServeHTTP(response, httptest.NewRequest("POST", "/v1/usage", strings.NewReader(body)))
		if response.Code != 400 {
			t.Fatalf("invalid report accepted: %d", response.Code)
		}
	}
}
