//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pam "github.com/jumpserver/pam-clients/go"
)

type fakeRemote struct {
	eventID                    string
	accountSelectors           []string
	value                      pam.Credential
	metadata                   int64
	fault                      error
	credentialFault            error
	unsubscribed               bool
	confirmFault               error
	fetched, confirmed, claims int
	eventResults               []string
	eventErrorCodes            []string
	commandResults             []string
	statusReports              []pam.ApplicationStatus
	statusFault                error
}

func (f *fakeRemote) ReportStatus(_ context.Context, report pam.ApplicationStatus) error {
	f.statusReports = append(f.statusReports, report)
	return f.statusFault
}

func (f *fakeRemote) SyncAgent(context.Context, pam.AgentSyncOptions) (pam.AgentSync, error) {
	if f.unsubscribed {
		return pam.AgentSync{ConfigDigest: "digest", Scope: pam.AgentScope{Keys: []string{}, ConfirmationKeys: []string{}}, Credentials: []pam.CredentialRevision{}}, f.fault
	}
	return pam.AgentSync{ConfigDigest: "digest", Scope: pam.AgentScope{Keys: []string{f.value.Key}, ConfirmationKeys: []string{f.value.Key}}, Credentials: []pam.CredentialRevision{{Key: f.value.Key, Revision: f.metadata, Available: true, AccountSwitch: f.value.AccountSwitch, EventID: f.eventID, AccountID: func() string {
		if f.eventID != "" {
			return f.value.Account.ID
		}
		return ""
	}(), AccountRevision: f.value.Account.Revision}}}, f.fault
}
func (f *fakeRemote) ListAuthorizedAccounts(context.Context) ([]pam.AuthorizedAccount, error) {
	return []pam.AuthorizedAccount{{ID: f.value.Account.ID, Name: f.value.Account.Name, Username: f.value.Account.Username, SecretType: f.value.Account.SecretType, Asset: f.value.Asset, Credentials: []pam.AccountPolicy{{Key: f.value.Key, Revision: f.metadata, Mode: "alternating_rotation"}}}}, f.fault
}
func (f *fakeRemote) GetCredentialFresh(_ context.Context, selector pam.CredentialSelector) (pam.Credential, error) {
	f.fetched++
	f.accountSelectors = append(f.accountSelectors, selector.AccountID)
	if f.credentialFault != nil {
		return f.value, f.credentialFault
	}
	if selector.AccountID != "" {
		if selector.AccountID != f.value.Account.ID {
			return pam.Credential{}, &pam.PAMError{Code: "credential_not_authorized", StatusCode: 403}
		}
		value := f.value
		value.Key = "account:" + selector.AccountID
		return value, f.fault
	}
	return f.value, f.fault
}
func (f *fakeRemote) WatchCredentialEvents(ctx context.Context, _ func(pam.Event) error) error {
	<-ctx.Done()
	return ctx.Err()
}
func (f *fakeRemote) ConfirmEvent(_ context.Context, _ string, status string, code string) (pam.CommandResult, error) {
	f.eventResults = append(f.eventResults, status)
	f.eventErrorCodes = append(f.eventErrorCodes, code)
	if status == "success" || status == "skip" {
		f.confirmed++
	}
	return pam.CommandResult{Accepted: true, Status: status}, f.confirmFault
}
func (f *fakeRemote) ConfirmCredential(context.Context, string, int64, string) (pam.CredentialConfirmation, error) {
	f.confirmed++
	return pam.CredentialConfirmation{}, f.confirmFault
}
func (f *fakeRemote) ListApplicationCommands(context.Context) ([]pam.Event, error) { return nil, nil }
func (f *fakeRemote) ReportApplicationCommandResult(_ context.Context, _ string, status, _ string) (pam.CommandResult, error) {
	f.commandResults = append(f.commandResults, status)
	if status == "running" {
		f.claims++
	}
	return pam.CommandResult{Accepted: f.claims == 1, Status: "running"}, nil
}

// Use a short private directory under the home folder: /tmp has writable
// ancestors, and macOS Unix sockets cannot use long checkout paths.
// No root/systemd mutation is tested.
func fixture(t *testing.T) (Config, *fakeRemote, *Agent) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(home, ".pam-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	root, err := filepath.Abs(directory)
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	config := Config{Endpoint: "https://example.com", AppID: "app", AppSecret: "secret", InstanceID: "instance", StateFile: filepath.Join(root, "state.json"), PullIntervalSeconds: 300, Delivery: DeliveryConfiguration{Mode: "socket", Root: filepath.Join(root, "output"), Socket: filepath.Join(root, "run", "agent.sock"), User: account.Username}}
	config.ConfigVersion = 1
	config.Socket = &SocketConfiguration{Socket: config.Delivery.Socket, Owner: config.Delivery.User}
	config.Store = &StoreConfiguration{Root: config.Delivery.Root, Owner: config.Delivery.User, Accounts: []string{"all"}}
	config.Services = []Rule{}
	remote := &fakeRemote{metadata: 2, value: pam.Credential{Key: "db", Revision: 2, Account: pam.Account{ID: "account", Revision: 2, Secret: "latest-2"}}}
	service, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	return config, remote, service
}

func TestUnknownConfigurationFieldIsRejected(t *testing.T) {
	config, _, _ := fixture(t)
	path := filepath.Join(filepath.Dir(config.StateFile), "agent.json")
	raw, _ := json.Marshal(config)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	value["unexpected"] = "value"
	raw, _ = json.Marshal(value)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("unknown field was silently accepted")
	}
}

func TestPullOnlyCredentialIsNotPersistedForOfflineUse(t *testing.T) {
	_, remote, service := fixture(t)
	value, err := service.AccountCredential(context.Background(), "account")
	if err != nil || value.Source != "api" || len(service.state.Latest) != 0 {
		t.Fatal("pull-only access was not served on demand", err)
	}
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	if _, err = service.AccountCredential(context.Background(), "account"); err == nil {
		t.Fatal("pull-only credential was available offline")
	}
}

// A fixed trusted executable reads secrets from stdin, never arguments.

// A newly installed Agent can start at B without seeing the earlier A event.

func TestFixedServiceAndPrivateConfig(t *testing.T) {
	config, _, _ := fixture(t)
	path := filepath.Join(filepath.Dir(config.StateFile), "agent.json")
	if err := writeJSON(path, config); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(path, 0644)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("public identity file accepted")
	}
	unit := Unit("/usr/local/bin/pam-agent", DefaultConfig)
	if ServiceName != "pam-agent.service" || !strings.Contains(unit, "/etc/jms-pam-agent/agent.json") {
		t.Fatal("service name or path is not fixed")
	}
}

func TestRelativeConfigPathRetainsPrivateFileChecks(t *testing.T) {
	config, _, _ := fixture(t)
	path := filepath.Join(filepath.Dir(config.StateFile), "agent.json")
	if err := writeJSON(path, config); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadConfig(relative)
	if err != nil || loaded.InstanceID != config.InstanceID {
		t.Fatal("relative configuration could not be loaded", err)
	}
	link := filepath.Join(filepath.Dir(path), "linked.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	relativeLink, _ := filepath.Rel(cwd, link)
	if _, err = LoadConfig(relativeLink); err == nil {
		t.Fatal("relative path bypassed the symlink restriction")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadConfig(relative); err == nil {
		t.Fatal("relative path bypassed private file permissions")
	}
}

func TestNativeAgentAutomaticallyRefreshesAndRestartsOffline(t *testing.T) {
	endpoint := os.Getenv("JMS_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("Use clients/tests/run.py for the native protocol fixture")
	}
	config, _, _ := fixture(t)
	config.Endpoint = endpoint
	config.AppID = "contract-app"
	config.AppSecret = "contract-secret"
	config.OrgID = "contract-org"
	config.InstanceID = "agent-native-go"
	accountID := "11111111-1111-4111-8111-111111111111"
	config.EventFile = filepath.Join(filepath.Dir(config.StateFile), "events.jsonl")
	control := func(values url.Values) {
		t.Helper()
		values.Set("instance", config.InstanceID)
		response, err := http.Get(strings.TrimSuffix(endpoint, "/prefix") + "/__control?" + values.Encode())
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("fixture control failed")
		}
	}
	control(url.Values{"delivery_root": {config.Delivery.Root}, "socket_path": {config.Delivery.Socket}, "app_user": {config.Delivery.User}, "account_id": {accountID}})
	remote, err := config.Client()
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	service, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Sync(context.Background()); err != nil {
		t.Fatal("initial service synchronization failed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("native PAM Agent failed to stop")
		}
	}()
	waitRevision := func(revision int64) {
		t.Helper()
		for i := 0; i < 300; i++ {
			value, err := service.LocalCredential("account:" + accountID)
			if err == nil && value.Revision == revision {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("automatic refresh did not reach revision %d", revision)
	}
	waitRevision(2)
	// Wait for the event connection, then notify without calling Sync or fetching.
	time.Sleep(50 * time.Millisecond)
	control(url.Values{"revision": {"3"}, "event": {"credential.secret.updated"}})
	waitRevision(3)
	// LocalCredential becomes available before delivery completes; queries wait
	// for the serialized update and therefore verify both native query and files.
	queried, err := service.AccountCredential(context.Background(), accountID)
	if err != nil || queried.Source != "api" || queried.Account.Version != 3 {
		t.Fatal("native account query failed", err)
	}
	raw, err := os.ReadFile(filepath.Join(config.Delivery.Root, accountID+".json"))
	var file CredentialResult
	if err != nil || json.Unmarshal(raw, &file) != nil || file.Account.Version != 3 || file.Account.Secret != queried.Account.Secret {
		t.Fatalf("native notification delivery mismatch: read=%v file_revision=%d queried_revision=%d same_secret=%t", err, file.Account.Version, queried.Account.Version, file.Account.Secret == queried.Account.Secret)
	}
	control(url.Values{"fault": {"503"}})
	if service.Sync(context.Background()) == nil {
		t.Fatal("backend outage was ignored")
	}
	queried, err = service.AccountCredential(context.Background(), accountID)
	if err != nil || queried.Source != "local" || queried.Account.Version != 3 {
		t.Fatal("native account query did not fall back during HTTP 503", err)
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := restarted.LocalCredential("account:" + accountID)
	if err != nil || retained.Secret != "LATEST_SECRET_3" {
		t.Fatal("native offline restart lost the latest credential", err)
	}
}

// A lagging metadata response still claims revision 2 is available. Requiring
// a new live fetch prevents that response from restoring the revoked password.

func TestFailedDeliveryDoesNotConfirmEvent(t *testing.T) {
	_, remote, service := fixture(t)
	remote.eventID = "event-failed"
	remote.metadata, remote.value.Revision, remote.value.Account.Revision = 0, 0, 0
	service.deliver = func(context.Context, Config, map[string]Credential, map[string]bool) error {
		return errors.New("application failed")
	}
	if err := service.Sync(context.Background()); err == nil {
		t.Fatal("expected delivery failure")
	}
	if remote.confirmed != 0 {
		t.Fatal("failed application confirmed")
	}
	if _, err := service.ConfirmEvent(context.Background(), "event-failed"); err == nil {
		t.Fatal("undelivered event confirmed")
	}
}
