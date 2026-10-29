//go:build !windows

package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	pam "github.com/jumpserver/pam-clients/go"
)

type socketRemote struct {
	*servicesRemote
	results   []string
	restart   bool
	delivered []pam.KnownRevision
}

func (r *socketRemote) SyncAgent(ctx context.Context, options pam.AgentSyncOptions) (pam.AgentSync, error) {
	response, err := r.servicesRemote.SyncAgent(ctx, options)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.restart = options.RestartSupported
	r.delivered = append([]pam.KnownRevision(nil), options.DeliveredCredentials...)
	for i := range response.Credentials {
		item := &response.Credentials[i]
		item.EventID = fmt.Sprintf("event-%s-%d-%d", item.Key, item.Revision, item.AccountRevision)
	}
	return response, err
}
func (r *socketRemote) ConfirmEvent(_ context.Context, id, status, code string) (pam.CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.confirmFault != nil {
		return pam.CommandResult{}, r.confirmFault
	}
	r.results = append(r.results, id+":"+status)
	return pam.CommandResult{Accepted: true, Status: status}, nil
}
func (r *socketRemote) ReportStatus(_ context.Context, value pam.ApplicationStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statusReports = append(r.statusReports, value)
	return nil
}
func (r *socketRemote) ListAuthorizedAccounts(context.Context) ([]pam.AuthorizedAccount, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []pam.AuthorizedAccount{}
	for id := range r.values {
		result = append(result, pam.AuthorizedAccount{ID: id})
	}
	return result, r.fault
}
func (r *socketRemote) ReportApplicationCommandResult(_ context.Context, id, status, code string) (pam.CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if status != "running" {
		if r.confirmFault != nil {
			return pam.CommandResult{}, r.confirmFault
		}
		r.results = append(r.results, "command:"+id+":"+status)
		return pam.CommandResult{Accepted: true, Status: status}, nil
	}
	r.claims++
	return pam.CommandResult{Accepted: r.claims == 1, Status: "running"}, nil
}
func (r *socketRemote) GetApplication(context.Context) (pam.ApplicationInfo, error) {
	return pam.ApplicationInfo{ID: "application", Name: "orders"}, nil
}
func (r *socketRemote) GetClientInfo(context.Context) (pam.ClientInfo, error) {
	return pam.ClientInfo{InstanceID: "agent"}, nil
}
func (r *socketRemote) ListPolicies(context.Context, string) ([]pam.CredentialPolicy, error) {
	return []pam.CredentialPolicy{{Key: "rotation"}}, nil
}
func (r *socketRemote) GetPolicy(_ context.Context, s pam.PolicySelector) (pam.CredentialPolicy, error) {
	return pam.CredentialPolicy{Key: s.Key, ID: s.PolicyID}, nil
}

func socketFixture(t *testing.T, names ...string) (Config, *socketRemote, *Agent) {
	t.Helper()
	config, remote := servicesFixture(t)
	config.Store = nil
	for _, name := range names {
		config.Services = append(config.Services, Rule{Name: name, Delivery: "socket", AllowAccountSwitch: true, RestartSupported: true, Accounts: []AccountSelector{{AccountID: testA}}})
	}
	r := &socketRemote{servicesRemote: remote}
	a, err := New(config, r)
	if err != nil {
		t.Fatal(err)
	}
	return config, r, a
}
func snapshotFor(t *testing.T, a *Agent, name string) socketSnapshot {
	t.Helper()
	raw, _, err := a.socketSnapshot(name)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("secret-")) || bytes.Contains(raw, []byte("app_secret")) {
		t.Fatal("snapshot disclosed secret")
	}
	var value socketSnapshot
	if err = json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func applySocket(t *testing.T, a *Agent, name string) string {
	t.Helper()
	view := snapshotFor(t, a, name)
	if view.Delivery == nil {
		t.Fatal("missing delivery")
	}
	if _, err := a.SocketResult(context.Background(), name, SocketResult{DeliveryID: view.Delivery.ID, Status: "success"}); err != nil {
		t.Fatal(err)
	}
	return view.Delivery.ID
}

func TestSocketServicesRequireExplicitDeliveryAndNoLocalActions(t *testing.T) {
	config, _, _ := socketFixture(t, "db")
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Rule){
		func(r *Rule) { r.Delivery = "tcp" },
		func(r *Rule) { r.ServiceAction = &Action{Type: "script", Path: "/usr/bin/true"} },
		func(r *Rule) { r.Delivery = ""; r.RestartSupported = true },
	} {
		altered := config
		altered.Services = append([]Rule(nil), config.Services...)
		change(&altered.Services[0])
		if altered.Validate() == nil {
			t.Fatal("ambiguous delivery accepted")
		}
	}
	raw, _ := json.Marshal(config)
	decoded, err := decodeConfig(raw)
	if err != nil || decoded.Services[0].Delivery != "socket" || !decoded.restartSupported() {
		t.Fatal("socket settings lost", err)
	}
}

func TestSocketABSwitchWaitsForEveryServiceAndRejectsOldDelivery(t *testing.T) {
	_, remote, a := socketFixture(t, "db", "worker")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	initial := snapshotFor(t, a, "db")
	if initial.Delivery == nil || !initial.RetryReady {
		t.Fatal("initial delivery missing")
	}
	values, err := a.SocketCredentials("db", initial.Delivery.ID)
	if err != nil || values[testA].AccountID != testA {
		t.Fatal("initial effective credential missing", err)
	}
	if len(a.state.Delivered) != 0 || len(remote.results) != 0 || len(a.AccountUsageSnapshot().AccountIDs) != 0 {
		t.Fatal("fetch counted as business use")
	}
	old := applySocket(t, a, "db")
	if len(remote.results) != 0 || len(a.state.Delivered) != 0 {
		t.Fatal("one service confirmed every destination")
	}
	applySocket(t, a, "worker")
	if len(remote.results) != 1 || !a.state.Applied["rotation"].Confirmed {
		t.Fatal("all service success was not confirmed", remote.results)
	}
	applySocket(t, a, "db")
	if len(remote.results) != 1 {
		t.Fatal("duplicate ACK replayed result")
	}
	for i, id := range []string{testB, testA} {
		remote.mu.Lock()
		remote.active = id
		remote.rotation = int64(i + 2)
		remote.mu.Unlock()
		if err := a.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		current := snapshotFor(t, a, "db")
		values, err = a.SocketCredentials("db", current.Delivery.ID)
		if err != nil || values[testA].AccountID != id {
			t.Fatal("declared A did not follow effective account", err)
		}
		if _, err = a.SocketResult(ctx, "db", SocketResult{DeliveryID: old, Status: "success"}); !errors.Is(err, errSocketConflict) {
			t.Fatal("superseded delivery ACK accepted", err)
		}
		applySocket(t, a, "db")
		applySocket(t, a, "worker")
	}
	if len(remote.results) != 3 {
		t.Fatal("A/B cycle did not confirm each applied version", remote.results)
	}
}

func TestSocketBusinessSuccessSurvivesOfflineAckAndAgentRestart(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	id := applySocket(t, a, "db")
	if len(remote.results) != 0 {
		t.Fatal("offline result claimed Core confirmation")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, restarted, "db")
	if view.Delivery.ID != id || view.Result != "success" || view.RetryReady {
		t.Fatal("restart requeued successful business work")
	}
	remote.mu.Lock()
	remote.fault = nil
	remote.mu.Unlock()
	if err = restarted.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(remote.results) != 1 || !restarted.state.Applied["rotation"].Confirmed {
		t.Fatal("pending Core result did not recover")
	}
}

func TestSocketFailureRetryAndRevocationDoNotAdvanceUsage(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	applySocket(t, a, "db")
	remote.mu.Lock()
	remote.active = testB
	remote.rotation = 2
	remote.mu.Unlock()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	_, err := a.SocketResult(ctx, "db", SocketResult{DeliveryID: view.Delivery.ID, Status: "failed", ErrorCode: "connection_failed"})
	if err != nil {
		t.Fatal(err)
	}
	view = snapshotFor(t, a, "db")
	if view.Result != "failed" || view.RetryReady || len(a.AccountUsageSnapshot().AccountIDs) != 1 || a.AccountUsageSnapshot().AccountIDs[0] != testA {
		t.Fatal("failed switch changed usage or retry backoff")
	}
	a.mu.Lock()
	a.state.SocketServices["db"].RetryAt = time.Now().Add(-time.Second)
	a.mu.Unlock()
	if !snapshotFor(t, a, "db").RetryReady {
		t.Fatal("failed delivery never became retryable")
	}
	remote.mu.Lock()
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	err = a.HandleEvent(ctx, pam.Event{Event: "credential.revoked", CredentialKey: "rotation", AccountID: testB})
	if err == nil {
		t.Fatal("expected unavailable backend")
	}
	revoked := snapshotFor(t, a, "db")
	if revoked.Delivery == nil || revoked.Delivery.Event != "credential.revoked" || len(revoked.Delivery.Credentials) != 0 {
		t.Fatal("offline revocation was not delivered", revoked)
	}
	if _, err = a.SocketCredentials("db", view.Delivery.ID); !errors.Is(err, errSocketConflict) {
		t.Fatal("old credential remained available")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotFor(t, restarted, "db").Delivery.ID != revoked.Delivery.ID {
		t.Fatal("restart lost pending cleanup")
	}
	applySocket(t, restarted, "db")
	if len(restarted.AccountUsageSnapshot().AccountIDs) != 0 {
		t.Fatal("revoked service still reports account use")
	}
}

func TestSocketRestartCommandsAggregateResultsAndKeepDuplicateReceipts(t *testing.T) {
	config, remote, a := socketFixture(t, "db", "worker")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if !remote.restart {
		t.Fatal("Core was not told about socket restart support")
	}
	event := pam.Event{Event: "app.restart.requested", CommandID: "restart-1", EventID: "restart-event"}
	if err := a.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	if len(snapshotFor(t, a, "db").Commands) != 1 {
		t.Fatal("restart command was not delegated")
	}
	if _, err := a.SocketCommandResult(ctx, "db", event.CommandID, "success", ""); err != nil {
		t.Fatal(err)
	}
	if len(remote.results) != 0 {
		t.Fatal("one consumer finished every restart")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotFor(t, restarted, "db").Commands[0].Status != "success" {
		t.Fatal("restart replayed completed command work")
	}
	if _, err = restarted.SocketCommandResult(ctx, "worker", event.CommandID, "success", ""); err != nil {
		t.Fatal(err)
	}
	if len(remote.results) != 1 || remote.results[0] != "restart-event:success" {
		t.Fatal("restart result not confirmed", remote.results)
	}
	if _, err = restarted.SocketCommandResult(ctx, "worker", event.CommandID, "success", ""); err != nil {
		t.Fatal("lost response was not idempotent", err)
	}
	if _, err = restarted.SocketCommandResult(ctx, "worker", event.CommandID, "failed", "bad"); err == nil {
		t.Fatal("completed command result regressed")
	}
}

func TestSocketHTTPRoutesAndFreshOnlyQueries(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/services", "/v1/services/db", "/v1/application", "/v1/instance", "/v1/policies", "/v1/policy?key=rotation"} {
		recorder := httptest.NewRecorder()
		a.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		if recorder.Code != 200 {
			t.Fatal(path, recorder.Code, recorder.Body.String())
		}
	}
	for _, body := range []string{`{"delivery_id":"bad","status":"success"}`, `{"status":"success"} {}`, `{"status":"success","unexpected":1}`} {
		recorder := httptest.NewRecorder()
		a.ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/services/db/results", strings.NewReader(body)))
		if recorder.Code == 200 {
			t.Fatal("invalid local result accepted", body)
		}
	}
	remote.mu.Lock()
	remote.credentialFault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	if _, err := a.accountCredential(context.Background(), testA, false); err == nil {
		t.Fatal("fresh-only query fell back")
	}
	if _, err := a.accountCredential(context.Background(), testA, true); err != nil {
		t.Fatal("authorized offline fallback failed", err)
	}
	if err := a.SocketStatus(context.Background(), "db", "degraded", "db_unavailable"); err != nil {
		t.Fatal(err)
	}
	if a.AccountUsageSnapshot().Status != "degraded" {
		t.Fatal("business health was not aggregated")
	}
}

func TestSocketWebSocketUsesUnixTransportAndStopsWithServer(t *testing.T) {
	_, _, a := socketFixture(t, "db")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	server, err := a.startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", a.Config.Delivery.Socket)
	}}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, "ws://localhost/v1/events?service_id=db", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if duplicate, response, err := websocket.Dial(ctx, "ws://localhost/v1/events?service_id=db", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}}); err == nil {
		duplicate.CloseNow()
		t.Fatal("a second active consumer was accepted")
	} else if response == nil || response.StatusCode != 409 {
		t.Fatal("duplicate connection did not return 409", err)
	}
	_, raw, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var initial socketSnapshot
	if err = json.Unmarshal(raw, &initial); err != nil || initial.Delivery == nil {
		t.Fatal("no initial snapshot", err)
	}
	send := func(request socketRequest) map[string]json.RawMessage {
		t.Helper()
		raw, _ := json.Marshal(request)
		if err = connection.Write(ctx, websocket.MessageText, raw); err != nil {
			t.Fatal(err)
		}
		for {
			_, raw, err = connection.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var response map[string]json.RawMessage
			if json.Unmarshal(raw, &response) != nil {
				t.Fatal("invalid server JSON")
			}
			if string(response["request_id"]) == `"`+request.RequestID+`"` {
				return response
			}
		}
	}
	response := send(socketRequest{RequestID: "fetch", Operation: "credentials", DeliveryID: initial.Delivery.ID})
	if !bytes.Contains(response["result"], []byte("secret-1")) {
		t.Fatal("bidirectional secret query failed", string(response["error"]))
	}
	response = send(socketRequest{RequestID: "applied", Operation: "result", DeliveryID: initial.Delivery.ID, Status: "success"})
	if response["error"] != nil {
		t.Fatal("business result rejected", string(response["error"]))
	}
	if snapshotFor(t, a, "db").Result != "success" {
		t.Fatal("business success missing")
	}
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	for {
		_, _, err = connection.Read(ctx)
		if err != nil {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("server shutdown left WebSocket running")
	}
	if _, err = os.Stat(a.Config.Delivery.Socket); !os.IsNotExist(err) {
		t.Fatal("socket path left behind")
	}
}

func TestSocketPythonExampleWithoutSDK(t *testing.T) {
	python := os.Getenv("JMS_SOCKET_PYTHON")
	if python == "" {
		t.Skip("set JMS_SOCKET_PYTHON for Python/websockets interoperability")
	}
	_, _, a := socketFixture(t, "db")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	server, err := a.startServer()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path, err := filepath.Abs("../../../socket/python_example.py")
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, python, path, "--socket", a.Config.Delivery.Socket, "--service", "db")
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = child.Wait() }()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil {
		t.Fatal("Python did not receive a Unix-socket snapshot", err)
	}
	var value map[string]any
	if json.Unmarshal([]byte(line), &value) != nil || value["service_id"] != "db" || value["event"] != "snapshot" || strings.Contains(line, "secret-") {
		t.Fatal("Python reference example did not interoperate", line)
	}
}

func TestSocketResultsDoNotSurviveFailedPersistenceInMemory(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	original := a.Config.StateFile
	// A directory cannot be atomically replaced by the state JSON file.
	a.Config.StateFile = original + "/invalid"
	_, err := a.SocketResult(context.Background(), "db", SocketResult{DeliveryID: view.Delivery.ID, Status: "success"})
	if err == nil {
		t.Fatal("failed persistence was accepted")
	}
	if a.state.SocketServices["db"].Result != "pending" || len(a.state.ServiceVersions["db"]) != 0 || len(a.AccountUsageSnapshot().AccountIDs) != 0 || len(remote.results) != 0 {
		t.Fatal("failed ACK advanced business state")
	}
	event := pam.Event{Event: "app.restart.requested", CommandID: "undurable", EventID: "undurable-event"}
	if _, err = a.queueSocketRestart(event); err == nil {
		t.Fatal("undurable command was exposed")
	}
	if _, ok := a.state.Commands[event.CommandID]; ok {
		t.Fatal("failed enqueue left a command for execution")
	}
	a.Config.StateFile = original
	applySocket(t, a, "db")
}

func TestSocketExpiredRestartRemainsExpiredAfterAgentRestart(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	if err := a.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(50 * time.Millisecond).Format(time.RFC3339Nano)
	raw, _ := json.Marshal(map[string]string{"event": "app.restart.requested", "event_id": "expired-event", "command_id": "expires", "expires_at": deadline})
	var event pam.Event
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if _, err := a.queueSocketRestart(event); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if len(snapshotFor(t, restarted, "db").Commands) != 0 {
		t.Fatal("expired command reappeared after restart")
	}
	if _, err = restarted.SocketCommandResult(context.Background(), "db", event.CommandID, "success", ""); !errors.Is(err, errSocketConflict) {
		t.Fatal("expired result was accepted", err)
	}
}

func TestSocketResultOutboxRetriesWithoutReapplyingAndFreshnessFloorBlocksAck(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	remote.mu.Lock()
	remote.confirmFault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	if _, err := a.SocketResult(ctx, "db", SocketResult{DeliveryID: view.Delivery.ID, Status: "failed", ErrorCode: "probe_failed"}); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.confirmFault = nil
	remote.mu.Unlock()
	if err = restarted.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if len(remote.results) != 1 || !strings.HasSuffix(remote.results[0], ":failed") {
		t.Fatal("failure outbox did not retry", remote.results)
	}
	applySocket(t, restarted, "db")
	if len(remote.results) != 2 || !strings.HasSuffix(remote.results[1], ":success") {
		t.Fatal("business retry did not replace failure")
	}
	old := snapshotFor(t, restarted, "db")
	remote.mu.Lock()
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	_ = restarted.HandleEvent(ctx, pam.Event{Event: "credential.updated", CredentialKey: "rotation", Revision: 2, AccountID: testB})
	if _, err = restarted.SocketResult(ctx, "db", SocketResult{DeliveryID: old.Delivery.ID, Status: "success"}); !errors.Is(err, errSocketConflict) {
		t.Fatal("known newer version allowed an old ACK")
	}
}

func TestSocketFailedManualSwitchReportsCommandFailure(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	event := pam.Event{Event: "credential.switch.requested", CommandID: "switch-1", EventID: "switch-event", CredentialKey: "rotation", AccountID: testA, AccountRevision: 1, Revision: 1}
	if err := a.HandleEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	if _, err := a.SocketReceived("db", view.Delivery.ID); err != nil {
		t.Fatal(err)
	}
	if a.state.Delivered["rotation"] != 0 || len(a.AccountUsageSnapshot().AccountIDs) != 0 {
		t.Fatal("receipt counted as business use")
	}
	if _, err := a.SocketResult(ctx, "db", SocketResult{DeliveryID: view.Delivery.ID, Status: "failed", ErrorCode: "db_failed"}); err != nil {
		t.Fatal(err)
	}
	if _, pending := a.state.Commands[event.CommandID]; pending {
		t.Fatal("failed switch remained running")
	}
	if len(remote.results) != 2 || remote.results[1] != "command:switch-1:failed" {
		t.Fatal("manual switch failure was not reported", remote.results)
	}
}

func TestSocketStatusBeforeInitialSyncAndReceiptStagesRemainIndependent(t *testing.T) {
	config, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.SocketStatus(ctx, "db", "starting", ""); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	if view.Delivery != nil || view.Result != "pending" {
		t.Fatal("runtime status invented a credential delivery")
	}
	restarted, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotFor(t, restarted, "db").Status != "starting" {
		t.Fatal("initial business status was not retained")
	}
	if err = restarted.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	view = snapshotFor(t, restarted, "db")
	if _, err = restarted.SocketReceived("db", view.Delivery.ID); err != nil {
		t.Fatal(err)
	}
	receipt := snapshotFor(t, restarted, "db")
	if !receipt.Received || receipt.CredentialsRead || receipt.Result != "pending" || receipt.CoreStatus != "pending" {
		t.Fatal("receipt advanced a later stage")
	}
	if _, err = restarted.SocketCredentials("db", view.Delivery.ID); err != nil {
		t.Fatal(err)
	}
	fetched := snapshotFor(t, restarted, "db")
	if !fetched.CredentialsRead || fetched.Result != "pending" || len(remote.results) != 0 {
		t.Fatal("fetch was counted as business success")
	}
	applySocket(t, restarted, "db")
	if snapshotFor(t, restarted, "db").CoreStatus != "confirmed" {
		t.Fatal("Core confirmation was not exposed")
	}
}

func TestSocketAccountRevisionFloorAndProtocolMismatchRejectStaleApplication(t *testing.T) {
	_, remote, a := socketFixture(t, "db")
	ctx := context.Background()
	if err := a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	view := snapshotFor(t, a, "db")
	remote.mu.Lock()
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	remote.mu.Unlock()
	_ = a.HandleEvent(ctx, pam.Event{Event: "credential.updated", CredentialKey: "account:" + testA, AccountID: testA, Revision: 2, AccountRevision: 2})
	if _, err := a.SocketCredentials("db", view.Delivery.ID); !errors.Is(err, errSocketConflict) {
		t.Fatal("account version floor did not block an older rotation credential", err)
	}
	_ = a.failure(&pam.PAMError{StatusCode: 426, Code: "client_upgrade_required"}, true)
	if _, err := a.SocketCredentials("db", view.Delivery.ID); !incompatible(err) {
		t.Fatal("protocol mismatch was hidden as an offline value", err)
	}
	if _, err := a.SocketResult(ctx, "db", SocketResult{DeliveryID: view.Delivery.ID, Status: "success"}); !incompatible(err) {
		t.Fatal("unsupported protocol accepted a business confirmation", err)
	}
	if snapshotFor(t, a, "db").ErrorCode != "client_upgrade_required" {
		t.Fatal("upgrade requirement was missing from local events")
	}
}
