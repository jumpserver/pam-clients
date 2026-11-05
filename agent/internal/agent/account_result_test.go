//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A second policy uses the same actual account. Both retained copies must
// describe the same successful pull without changing their policy revisions.

func assertAccountJSON(t *testing.T, raw []byte, id, source string, version int64) CredentialResult {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	fields := 1
	if source != "" {
		fields = 3
		if object["source"] == nil || object["last_pull_at"] == nil {
			t.Fatal("query result omitted its source or pull time")
		}
	} else if object["source"] != nil || object["last_pull_at"] != nil {
		t.Fatal("credential file contains operational metadata")
	}
	if len(object) != fields || object["account"] == nil {
		t.Fatal("credential result has unexpected fields")
	}
	var account map[string]json.RawMessage
	if err := json.Unmarshal(object["account"], &account); err != nil {
		t.Fatal(err)
	}
	if len(account) != 7 || account["version"] == nil || account["asset"] == nil {
		t.Fatal("account contains protocol metadata or omits its version/asset")
	}
	var value CredentialResult
	if err := json.Unmarshal(raw, &value); err != nil || value.Account.ID != id || value.Account.Version != version || value.Source != source {
		t.Fatal("incorrect business account result", err)
	}
	return value
}

func readPullTimes(t *testing.T, a *Agent) map[string]*time.Time {
	t.Helper()
	raw, err := os.ReadFile(a.Config.pullTimesPath())
	if err != nil {
		t.Fatal(err)
	}
	var times map[string]*time.Time
	if err = json.Unmarshal(raw, &times); err != nil {
		t.Fatal("invalid pull diagnostic file", err)
	}
	return times
}

func TestBusinessAccountJSONAndNotificationContext(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	// Policy revision and actual account version intentionally differ, including
	// the valid initial account version zero.
	remote.rotation = 7
	credential := remote.values[testA]
	credential.Account.Revision = 0
	remote.values[testA] = credential
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	if view.Delivery.Credentials[testA].EventID == "" || view.Delivery.Credentials[testA].Revision != 7 {
		t.Fatal("notification lost its event and policy context")
	}
	values, err := a.SocketCredentials("db", view.Delivery.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(values[testA])
	if err != nil {
		t.Fatal(err)
	}
	cached := assertAccountJSON(t, raw, testA, "local", 0)
	if cached.LastPullAt == nil {
		t.Fatal("socket delivery omitted its retained pull time")
	}

	for _, path := range []string{"/v1/credential?account_id=" + testA, "/v1/credentials/rotation"} {
		reply := httptest.NewRecorder()
		a.ServeHTTP(reply, httptest.NewRequest(http.MethodGet, path, nil))
		if reply.Code != 200 {
			t.Fatalf("credential HTTP status %d", reply.Code)
		}
		source := "api"
		if strings.HasPrefix(path, "/v1/credentials/") {
			source = "local"
		}
		assertAccountJSON(t, reply.Body.Bytes(), testA, source, 0)
	}

	value := a.state.Latest["rotation"]
	payload := Payload{Event: "credential.secret.updated", Credentials: map[string]Credential{testA: value}}
	raw, err = render(File{Format: "json"}, payload)
	if err != nil {
		t.Fatal(err)
	}
	assertAccountJSON(t, raw, testA, "", 0)
	raw, err = json.Marshal(payload.notification())
	var notification notificationPayload
	if err != nil || json.Unmarshal(raw, &notification) != nil || notification.Credentials[testA].EventID != value.EventID {
		t.Fatal("script notification lost its result-reporting event ID", err)
	}

	// Repeated same-version pulls update the diagnostic file without rewriting
	// the credential file that business processes read.
	storeConfig, storeRemote := servicesFixture(t)
	storeAgent, err := New(storeConfig, storeRemote)
	if err != nil {
		t.Fatal(err)
	}
	if err = storeAgent.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(storeConfig.Store.Root, testA+".json")
	raw, err = os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertAccountJSON(t, raw, testA, "", 1)
	beforeContents := append([]byte(nil), raw...)
	beforeInfo, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	previous := readPullTimes(t, storeAgent)[testA]
	live, err := storeAgent.AccountCredential(ctx, testA)
	if err != nil || previous == nil || !live.LastPullAt.After(*previous) {
		t.Fatal("same-version live pull did not advance time", err)
	}
	if err = storeAgent.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	assertAccountJSON(t, raw, testA, "", 1)
	afterInfo, err := os.Stat(filename)
	if err != nil || !bytes.Equal(raw, beforeContents) || !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatal("same-version pull rewrote the business credential file", err)
	}
	if pulled := readPullTimes(t, storeAgent)[testA]; pulled == nil || !pulled.Equal(*live.LastPullAt) {
		t.Fatal("separate diagnostic file still has the previous pull time")
	}
}

func TestBackgroundPullSharesTimeWithExistingAccountCache(t *testing.T) {
	config, remote := servicesFixture(t)
	config.Services = []Rule{{Name: "app", Delivery: "socket", AllowAccountSwitch: true, Accounts: []AccountSelector{{AccountID: testA}}}}
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	first := a.state.Latest["account:"+testA].LastPullAt
	fetched := remote.fetched
	// A new policy revision requests the same account/version. Its successful
	// fetch also refreshes the timestamp of the unchanged direct-account cache.
	remote.rotation++
	if err = a.Sync(ctx); err != nil || remote.fetched != fetched+1 {
		t.Fatal("expected one fresh pull for the new policy revision", err)
	}
	direct, policy := a.state.Latest["account:"+testA], a.state.Latest["rotation"]
	if direct.LastPullAt == nil || policy.LastPullAt == nil || !direct.LastPullAt.Equal(*policy.LastPullAt) || !direct.LastPullAt.After(*first) {
		t.Fatal("account and policy caches disagree about their latest successful pull")
	}
}

func TestMultiAccountJSONFilesOmitSource(t *testing.T) {
	values := map[string]Credential{"a": {AccountID: "a", AccountRevision: 1}, "b": {AccountID: "b", AccountRevision: 2}}
	raw, err := render(File{Format: "json"}, Payload{Credentials: values})
	if err != nil {
		t.Fatal(err)
	}
	var files map[string]json.RawMessage
	if err = json.Unmarshal(raw, &files); err != nil || len(files) != 2 {
		t.Fatal("multi-account JSON file lost its account keys", err)
	}
	for key, value := range values {
		assertAccountJSON(t, files[key], value.AccountID, "", value.AccountRevision)
	}
}
