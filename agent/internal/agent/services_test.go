//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	pam "github.com/jumpserver/pam-clients/go"
)

const testA = "00000000-0000-0000-0000-000000000001"
const testB = "00000000-0000-0000-0000-000000000002"
const testC = "00000000-0000-0000-0000-000000000003"

type servicesRemote struct {
	mu sync.Mutex
	fakeRemote
	values    map[string]pam.Credential
	active    string
	rotation  int64
	lastScope *pam.DeliveryScope
}

func (f *servicesRemote) SyncAgent(_ context.Context, options pam.AgentSyncOptions) (pam.AgentSync, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastScope = options.DeliveryScope
	result := pam.AgentSync{ConfigSchemaVersion: 1, ServerCapabilities: []string{"agent_store_services"}, ConfigDigest: "services", Scope: pam.AgentScope{Keys: []string{}, ConfirmationKeys: []string{}}}
	if f.fault != nil {
		return result, f.fault
	}
	add := func(key, id string, revision int64, pair *pam.AccountSwitch) {
		if contains(result.Scope.Keys, key) {
			return
		}
		value, ok := f.values[id]
		if !ok {
			return
		}
		result.Scope.Keys = append(result.Scope.Keys, key)
		result.Credentials = append(result.Credentials, pam.CredentialRevision{Key: key, AccountID: id, AccountRevision: value.Account.Revision, Revision: revision, Available: true, AccountSwitch: pair})
	}
	for id, value := range f.values {
		if options.DeliveryScope.AllAuthorized || contains(options.DeliveryScope.AccountIDs, id) {
			add("account:"+id, id, value.Account.Revision, nil)
		}
	}
	for _, id := range options.DeliveryScope.SwitchAccountIDs {
		if id == testA || id == testB {
			add("rotation", f.active, f.rotation, &pam.AccountSwitch{AccountIDs: []string{testA, testB}})
			result.Scope.ConfirmationKeys = append(result.Scope.ConfirmationKeys, "rotation")
		} else {
			add("account:"+id, id, f.values[id].Account.Revision, nil)
		}
	}
	return result, nil
}

func (f *servicesRemote) GetCredentialFresh(_ context.Context, selector pam.CredentialSelector) (pam.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetched++
	if f.credentialFault != nil {
		return pam.Credential{}, f.credentialFault
	}
	value, ok := f.values[selector.AccountID]
	if !ok {
		return value, &pam.PAMError{StatusCode: 403, Code: "credential_not_authorized"}
	}
	return value, nil
}

func servicesFixture(t *testing.T) (Config, *servicesRemote) {
	t.Helper()
	base, _, _ := fixture(t)
	config := Config{ConfigVersion: 1, Endpoint: base.Endpoint, AppID: base.AppID, AppSecret: base.AppSecret, InstanceID: base.InstanceID, StateFile: base.StateFile, PullIntervalSeconds: 300, Socket: &SocketConfiguration{Socket: base.Delivery.Socket, Owner: base.Delivery.User}, Store: &StoreConfiguration{Root: base.Delivery.Root, Owner: base.Delivery.User, Accounts: []string{"all"}}}
	remote := &servicesRemote{active: testA, rotation: 1, values: map[string]pam.Credential{}}
	for _, id := range []string{testA, testB, testC} {
		remote.values[id] = pam.Credential{Key: "account:" + id, Revision: 1, Account: pam.Account{ID: id, Revision: 1, Username: "user-" + id[len(id)-1:], Secret: "secret-" + id[len(id)-1:]}}
	}
	return config, remote
}

func TestServicesConfigurationAndExplicitAccountScope(t *testing.T) {
	config, _ := servicesFixture(t)
	config.Store.Accounts = []string{testA}
	config.Services = []Rule{{Name: "db", AllowAccountSwitch: true, Accounts: []AccountSelector{{AccountID: testB}}, ServiceAction: &Action{Path: "/usr/bin/true"}}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	scope := config.DeliveryScope()
	if !scope.IncludeAuthorized || scope.AllAuthorized || !reflect.DeepEqual(scope.AccountIDs, []string{testA}) || !reflect.DeepEqual(scope.SwitchAccountIDs, []string{testB}) {
		t.Fatal("storage and switching declarations were conflated", scope)
	}
	raw, err := json.Marshal(config.normalized())
	if err != nil || strings.Contains(string(raw), `"delivery"`) || strings.Contains(string(raw), `"rules"`) {
		t.Fatal("service export contains legacy configuration", err)
	}
	loaded, err := decodeConfig(raw)
	if err != nil || loaded.ConfigVersion != 1 || loaded.Services[0].Name != "db" {
		t.Fatal("service configuration did not round trip", err)
	}
	for _, accounts := range [][]string{nil, {"all", testA}, {testA, testA}, {"invalid"}} {
		config.Store.Accounts = accounts
		if config.Validate() == nil {
			t.Fatal("invalid store declaration accepted", accounts)
		}
	}
}

func TestStoreAllPersistsUnsubscribedAccountsAndRemovesRevocations(t *testing.T) {
	config, remote := servicesFixture(t)
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{testA, testB, testC} {
		if _, err = os.Stat(filepath.Join(config.Store.Root, id+".json")); err != nil {
			t.Fatal("authorized account missing from store", err)
		}
	}
	if remote.fetched != 3 || remote.confirmed != 0 {
		t.Fatal("storage was treated as application use")
	}
	if err = a.Sync(ctx); err != nil || remote.fetched != 3 {
		t.Fatal("unchanged background synchronization fetched passwords again", err)
	}
	delete(remote.values, testC)
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testC+".json")); !os.IsNotExist(err) {
		t.Fatal("revoked store file survived")
	}
	if _, err = a.retainedAccount(testC); err == nil {
		t.Fatal("revoked account survived offline")
	}
	remote.fault = &pam.PAMError{StatusCode: 403, Code: "client_disabled"}
	if a.Sync(ctx) == nil {
		t.Fatal("client disable ignored")
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testA+".json")); !os.IsNotExist(err) {
		t.Fatal("disabled application retained store files")
	}
}

func TestServicesShareFetchButTrackNotificationAndRotationSeparately(t *testing.T) {
	config, remote := servicesFixture(t)
	root := filepath.Dir(config.StateFile)
	path := filepath.Join(root, "business.yml")
	if err := os.WriteFile(path, []byte("DB_USER: old\nDB_PASSWORD: old\nCACHE_PASSWORD: old\nOTHER: keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	deferredPath := filepath.Join(root, "deferred.json")
	no := false
	check := &ApplicationCheck{Action: Action{Path: "/usr/bin/true"}, ConfirmOnSuccess: true}
	config.Services = []Rule{
		{Name: "immediate", AllowAccountSwitch: true, Accounts: []AccountSelector{{AccountID: testA, Alias: "database", Username: "DISPLAY_ONLY"}, {AccountID: testC, Alias: "cache"}}, ConfigUpdate: &ConfigUpdate{File: path, Format: "yaml", FieldsMap: map[string]string{"DB_USER": "database.username", "DB_PASSWORD": "database.secret", "CACHE_PASSWORD": "cache.secret"}}, ApplicationCheck: check},
		{Name: "deferred", AllowAccountSwitch: true, UpdateOnNotification: &no, Accounts: []AccountSelector{{AccountID: testA}}, ConfigUpdate: &ConfigUpdate{Files: []File{{Path: deferredPath, Format: "json"}}}, ApplicationCheck: check},
	}
	config.Services[0].ServiceAction = &Action{Path: "/usr/bin/true"}
	// Exercise all three renamed JSON blocks through the real update and
	// rotation-confirmation lifecycle, including restart deduplication below.
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	config, err = decodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if remote.fetched != 3 || remote.confirmed != 1 {
		t.Fatal("duplicate fetch or missing initial confirmation", remote.fetched, remote.confirmed)
	}
	remote.active, remote.rotation = testB, 2
	if err = a.HandleEvent(ctx, pam.Event{Event: "credential.updated", CredentialKey: "rotation", Revision: 2}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `DB_USER: "user-2"`) || strings.Contains(string(data), "DISPLAY_ONLY") || !strings.Contains(string(data), `CACHE_PASSWORD: "secret-3"`) || !strings.Contains(string(data), "OTHER: keep") {
		t.Fatal("alias mapping did not follow active account without overwriting other fields")
	}
	data, _ = os.ReadFile(deferredPath)
	if !strings.Contains(string(data), testA) || remote.confirmed != 1 {
		t.Fatal("deferred business updated or rotation confirmed before all services applied")
	}
	if err = a.HandleEvent(ctx, pam.Event{Event: "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if _, err = a.retainedAccount(testC); err != nil {
		t.Fatal("push snapshot revoked a pull-only stored account")
	}
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(deferredPath)
	if !strings.Contains(string(data), testB) || remote.confirmed != 2 {
		t.Fatal("periodic synchronization failed to apply deferred account switch")
	}
	for _, versions := range a.state.ServiceVersions {
		if versions["rotation"] != credentialVersion(a.state.Latest["rotation"]) {
			t.Fatal("service version ledger is stale")
		}
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	restarted.deliver = func(context.Context, Config, map[string]Credential, map[string]bool) error {
		return errors.New("unchanged business must not execute after restart")
	}
	if err = restarted.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	config.Store.Accounts = []string{testA, testB}
	config.Services[0].Accounts[0].Username = "A DIFFERENT DISPLAY HINT"
	reconfigured, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	reconfigured.deliver = restarted.deliver
	if err = reconfigured.Sync(ctx); err != nil {
		t.Fatal("storage or display changes re-executed an unchanged service", err)
	}
}

func TestMultiAccountMappingsRejectAmbiguousSourcesBeforeMutation(t *testing.T) {
	config, _ := servicesFixture(t)
	path := filepath.Join(filepath.Dir(config.StateFile), "business.yml")
	service := Rule{Name: "two", Accounts: []AccountSelector{{AccountID: testA, Alias: "db"}, {AccountID: testC, Alias: "cache"}}, ConfigUpdate: &ConfigUpdate{File: path, Format: "yaml", FieldsMap: map[string]string{"DB_USER": "username"}}}
	config.Services = []Rule{service}
	if config.Validate() == nil {
		t.Fatal("multi-account mapping accepted an unqualified field")
	}
	config.Services[0].ConfigUpdate.FieldsMap["DB_USER"] = "missing.username"
	if config.Validate() == nil {
		t.Fatal("unknown alias accepted")
	}
	config.Services[0].ConfigUpdate.FieldsMap["DB_USER"] = "db.username"
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Services[0].Accounts[1].Alias = "db"
	if config.Validate() == nil {
		t.Fatal("duplicate alias accepted")
	}
}

func TestRevokedStoreFileIsDeletedBeforeOfflineRefresh(t *testing.T) {
	config, remote := servicesFixture(t)
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote.fault = &pam.PAMError{StatusCode: 503, Code: "NetworkError"}
	if a.HandleEvent(context.Background(), pam.Event{Event: "credential.revoked", AccountID: testA}) == nil {
		t.Fatal("outage ignored")
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testA+".json")); !os.IsNotExist(err) {
		t.Fatal("revoked file survived failed refresh")
	}
	if raw, readErr := os.ReadFile(config.StateFile); readErr != nil || strings.Contains(string(raw), "secret-1") {
		t.Fatal("revoked password survived in private state", readErr)
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testC+".json")); err != nil {
		t.Fatal("unrelated stored account was removed", err)
	}
	config.Store.Accounts = []string{testB}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.retainedAccount(testC); err == nil {
		t.Fatal("removed local declaration retained offline access")
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testC+".json")); !os.IsNotExist(err) {
		t.Fatal("removed local declaration retained a stored file")
	}
}

type busyServicesRemote struct {
	*servicesRemote
	ready chan struct{}
}

func (f *busyServicesRemote) ReportStatus(ctx context.Context, status pam.ApplicationStatus) error {
	select {
	case f.ready <- struct{}{}:
	default:
	}
	return nil
}
func (f *busyServicesRemote) WatchCredentialEvents(ctx context.Context, handler func(pam.Event) error) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := handler(pam.Event{Event: "snapshot"}); err != nil {
				return err
			}
		}
	}
}

func TestNotificationsDoNotPostponeDeferredServiceReconciliation(t *testing.T) {
	config, remote := servicesFixture(t)
	config.PullIntervalSeconds = 1
	config.Store = nil
	no := false
	output := filepath.Join(filepath.Dir(config.StateFile), "deferred.json")
	config.Services = []Rule{{Name: "deferred", UpdateOnNotification: &no, Accounts: []AccountSelector{{AccountID: testC}}, ConfigUpdate: &ConfigUpdate{Files: []File{{Path: output, Format: "json"}}}}}
	busy := &busyServicesRemote{servicesRemote: remote, ready: make(chan struct{}, 1)}
	a, err := New(config, busy)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-busy.ready:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("initial service did not start")
	}
	remote.mu.Lock()
	value := remote.values[testC]
	value.Account.Revision = 2
	value.Account.Secret = "updated"
	value.Revision = 2
	remote.values[testC] = value
	remote.mu.Unlock()
	deadline := time.Now().Add(2500 * time.Millisecond)
	applied := false
	for time.Now().Before(deadline) {
		a.mu.Lock()
		applied = a.state.ServiceVersions["deferred"]["account:"+testC] == testC+":2:2"
		a.mu.Unlock()
		if applied {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("continuous notifications postponed periodic business update")
	}
}

type overbroadServicesRemote struct{ *servicesRemote }

func (f overbroadServicesRemote) SyncAgent(ctx context.Context, options pam.AgentSyncOptions) (pam.AgentSync, error) {
	result, err := f.servicesRemote.SyncAgent(ctx, options)
	result.Scope.Keys = append(result.Scope.Keys, "account:"+testC)
	result.Credentials = append(result.Credentials, pam.CredentialRevision{Key: "account:" + testC, AccountID: testC, Revision: 1, AccountRevision: 1, Available: true})
	return result, err
}

func TestServerCannotExpandLocalAccountDeclarations(t *testing.T) {
	config, remote := servicesFixture(t)
	config.Store.Accounts = []string{testA}
	a, err := New(config, overbroadServicesRemote{remote})
	if err != nil {
		t.Fatal(err)
	}
	if a.Sync(context.Background()) == nil || remote.fetched != 0 {
		t.Fatal("overbroad response fetched an undeclared account")
	}
}

func TestIdentityDeniedDuringFetchClearsAllStoredCredentials(t *testing.T) {
	config, remote := servicesFixture(t)
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	value := remote.values[testB]
	value.Account.Revision++
	remote.values[testB] = value
	remote.credentialFault = &pam.PAMError{StatusCode: 403, Code: "client_disabled"}
	if a.Sync(context.Background()) == nil {
		t.Fatal("identity rejection was ignored")
	}
	if !a.state.Denied || len(a.state.Latest) != 0 || len(a.state.StoreFiles) != 0 {
		t.Fatal("global identity denial retained credentials or files")
	}
}

func TestScopeAuthorizationReductionPersistsBeforeFailedHTTPRefresh(t *testing.T) {
	config, remote := servicesFixture(t)
	service, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	event := pam.Event{Event: "app.scope.updated", AuthorizedAccountIDs: []string{testB, testC}}
	if service.HandleEvent(ctx, event) == nil {
		t.Fatal("expected synchronization outage")
	}
	if _, err = service.LocalCredential("account:" + testA); err == nil {
		t.Fatal("revoked account remained readable")
	}
	if _, err = os.Stat(filepath.Join(config.Store.Root, testA+".json")); !os.IsNotExist(err) {
		t.Fatal("revoked standard file survived", err)
	}
	if _, err = service.LocalCredential("account:" + testB); err != nil {
		t.Fatal("authorized account was discarded", err)
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.LocalCredential("account:" + testA); err == nil {
		t.Fatal("restart restored revoked access")
	}
	if _, err = restarted.LocalCredential("account:" + testB); err != nil {
		t.Fatal("restart lost authorized credentials", err)
	}
}

func TestPushScopeRemovalPreservesAuthorizedAgentStoreDuringOutage(t *testing.T) {
	config, remote := servicesFixture(t)
	service, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = service.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	event := pam.Event{Event: "snapshot", AuthorizedAccountIDs: []string{testA, testB, testC}}
	if service.HandleEvent(ctx, event) == nil {
		t.Fatal("expected synchronization outage")
	}
	for _, id := range []string{testA, testB, testC} {
		if _, err = service.LocalCredential("account:" + id); err != nil {
			t.Fatal("empty notification scope revoked an authorized account", err)
		}
		if _, err = os.Stat(filepath.Join(config.Store.Root, id+".json")); err != nil {
			t.Fatal("authorized standard file disappeared", err)
		}
	}
}
