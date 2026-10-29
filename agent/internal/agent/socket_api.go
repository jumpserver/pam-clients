package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/coder/websocket"
	pam "github.com/jumpserver/pam-clients/go"
)

type socketCommand struct {
	Event     string `json:"event"`
	EventID   string `json:"event_id,omitempty"`
	CommandID string `json:"command_id"`
	Status    string `json:"status"`
}

type socketSnapshot struct {
	Event           string          `json:"event"`
	ServiceID       string          `json:"service_id"`
	Delivery        *SocketDelivery `json:"delivery,omitempty"`
	Result          string          `json:"result"`
	Received        bool            `json:"received"`
	CredentialsRead bool            `json:"credentials_read"`
	CoreStatus      string          `json:"core_status"`
	ErrorCode       string          `json:"error_code,omitempty"`
	RetryReady      bool            `json:"retry_ready"`
	Status          string          `json:"status"`
	SyncStatus      string          `json:"sync_status"`
	Commands        []socketCommand `json:"commands"`
}

func (a *Agent) socketSnapshot(name string) ([]byte, <-chan struct{}, error) {
	if _, ok := a.socketService(name); !ok {
		return nil, nil, errSocketService
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	value := socketSnapshot{Event: "snapshot", ServiceID: name, Result: "pending", Status: "starting", CoreStatus: "pending", SyncStatus: a.state.Status, Commands: []socketCommand{}}
	if a.state.Denied {
		value.Event = "agent.access_denied"
	}
	if state := a.state.SocketServices[name]; state != nil {
		if state.Result != "" {
			value.Result = state.Result
		}
		value.Status, value.ErrorCode = state.Status, state.ErrorCode
		value.Received, value.CredentialsRead = state.Received, state.CredentialsRead
		value.CoreStatus = a.socketCoreStatusLocked(state)
		if state.Delivery.ID != "" && a.socketDeliveryCurrentLocked(state) {
			value.Delivery = &state.Delivery
			value.RetryReady = state.Result == "pending" || state.Result == "failed" && !time.Now().Before(state.RetryAt)
		}
	}
	if a.state.SyncError != "" && value.ErrorCode == "" {
		value.ErrorCode = a.state.SyncError
	}
	for id, targets := range a.state.SocketCommands {
		if status, ok := targets[name]; ok && !a.state.Denied {
			if event, exists := a.state.Commands[id]; exists && !a.socketCommandExpiredLocked(event) {
				value.Commands = append(value.Commands, socketCommand{event.Event, event.EventID, id, status})
			}
		}
	}
	sort.Slice(value.Commands, func(i, j int) bool { return value.Commands[i].CommandID < value.Commands[j].CommandID })
	// Encoding under the lock avoids publishing mutable state maps to writers.
	raw, err := json.Marshal(value)
	return raw, a.changed, err
}

func (a *Agent) socketCoreStatusLocked(state *SocketServiceState) string {
	if state.Result == "failed" && state.FailureReported {
		return "failed"
	}
	if state.Result != "success" {
		return "pending"
	}
	required := false
	for _, version := range state.Delivery.Credentials {
		if version.EventID != "" || contains(a.state.Scope.ConfirmationKeys, version.Key) {
			required = true
			applied := a.state.Applied[version.Key]
			if !applied.Confirmed || applied.Revision != version.Revision || applied.AccountID != version.AccountID {
				return "pending"
			}
		}
	}
	if required {
		return "confirmed"
	}
	return "not_required"
}

type socketRequest struct {
	RequestID          string   `json:"request_id"`
	Operation          string   `json:"operation"`
	DeliveryID         string   `json:"delivery_id,omitempty"`
	CommandID          string   `json:"command_id,omitempty"`
	Status             string   `json:"status,omitempty"`
	ErrorCode          string   `json:"error_code,omitempty"`
	AccountID          string   `json:"account_id,omitempty"`
	AccountIDs         []string `json:"account_ids,omitempty"`
	AllowLocalFallback *bool    `json:"allow_local_fallback,omitempty"`
	Search             string   `json:"search,omitempty"`
	Key                string   `json:"key,omitempty"`
	PolicyID           string   `json:"policy_id,omitempty"`
}

func (a *Agent) socketOperation(ctx context.Context, name string, request socketRequest) (any, error) {
	switch request.Operation {
	case "ping":
		return map[string]string{"event": "pong"}, nil
	case "credentials":
		return a.SocketCredentials(name, request.DeliveryID)
	case "received":
		return a.SocketReceived(name, request.DeliveryID)
	case "result":
		return a.SocketResult(ctx, name, SocketResult{request.DeliveryID, request.Status, request.ErrorCode})
	case "command.result":
		return a.SocketCommandResult(ctx, name, request.CommandID, request.Status, request.ErrorCode)
	case "status":
		err := a.SocketStatus(ctx, name, request.Status, request.ErrorCode)
		return map[string]bool{"accepted": err == nil}, err
	case "accounts":
		return a.Accounts(ctx)
	case "usage":
		if request.AccountIDs == nil {
			return a.AccountUsageSnapshot(), nil
		}
		err := a.ReportAccounts(ctx, request.AccountIDs)
		return map[string]bool{"accepted": err == nil}, err
	case "credential":
		return a.accountCredential(ctx, request.AccountID, request.AllowLocalFallback == nil || *request.AllowLocalFallback)
	case "application", "instance", "policies", "policy":
		return a.localMetadata(ctx, request.Operation, request.Search, request.Key, request.PolicyID)
	default:
		return nil, errors.New("unknown_operation")
	}
}

func localError(err error) (int, string) {
	switch {
	case errors.Is(err, errSocketService):
		return http.StatusNotFound, "socket_service_not_found"
	case errors.Is(err, errSocketConflict):
		return http.StatusConflict, "socket_delivery_superseded"
	case incompatible(err):
		return http.StatusUpgradeRequired, "client_upgrade_required"
	}
	var remote *pam.PAMError
	if errors.As(err, &remote) {
		switch remote.StatusCode {
		case 401, 403:
			return http.StatusForbidden, "access_denied"
		case 404:
			return http.StatusNotFound, "not_found"
		}
		return http.StatusServiceUnavailable, "backend_unavailable"
	}
	return http.StatusBadRequest, "request_rejected"
}

// Each connection has one reader and one writer. Bounded response backpressure
// affects only that local consumer, never the Core event reader or other services.
func (a *Agent) socketEvents(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("service_id")
	if _, ok := a.socketService(name); !ok {
		http.Error(w, `{"code":"socket_service_not_found"}`, http.StatusNotFound)
		return
	}
	a.mu.Lock()
	if a.socketConsumers == nil {
		a.socketConsumers = map[string]bool{}
	}
	if a.socketConsumers[name] {
		a.mu.Unlock()
		http.Error(w, `{"code":"socket_service_already_connected"}`, http.StatusConflict)
		return
	}
	a.socketConsumers[name] = true
	a.mu.Unlock()
	defer func() { a.mu.Lock(); delete(a.socketConsumers, name); a.mu.Unlock() }()
	// HTTP request deadlines must not limit an upgraded, long-lived connection.
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Time{})
	_ = controller.SetWriteDeadline(time.Time{})
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	connection.SetReadLimit(32768)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer connection.CloseNow()
	replies := make(chan []byte, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		for {
			_, raw, err := connection.Read(ctx)
			if err != nil {
				return
			}
			var request socketRequest
			if strictJSON(strings.NewReader(string(raw)), &request) != nil || request.RequestID == "" || len(request.RequestID) > 128 {
				connection.Close(websocket.StatusPolicyViolation, "invalid request")
				return
			}
			bounded, stop := context.WithTimeout(ctx, 20*time.Second)
			value, err := a.socketOperation(bounded, name, request)
			stop()
			response := map[string]any{"request_id": request.RequestID, "result": value}
			if err != nil {
				status, code := localError(err)
				response = map[string]any{"request_id": request.RequestID, "error": map[string]any{"code": code, "status": status}}
			}
			encoded, _ := json.Marshal(response)
			select {
			case replies <- encoded:
			case <-ctx.Done():
				return
			}
		}
	}()
	defer func() { cancel(); connection.CloseNow(); <-done }()
	write := func(raw []byte) error {
		bounded, stop := context.WithTimeout(ctx, 5*time.Second)
		defer stop()
		return connection.Write(bounded, websocket.MessageText, raw)
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	heartbeat := time.NewTicker(10 * time.Second)
	defer heartbeat.Stop()
	previous := ""
	for {
		snapshot, changed, err := a.socketSnapshot(name)
		if err != nil {
			return
		}
		if string(snapshot) != previous {
			if write(snapshot) != nil {
				return
			}
			previous = string(snapshot)
		}
		select {
		case <-ctx.Done():
			return
		case response := <-replies:
			if write(response) != nil {
				return
			}
		case <-changed:
		case <-ticker.C:
		case <-heartbeat.C:
			if write([]byte(`{"event":"ping"}`)) != nil {
				return
			}
		}
	}
}

func strictJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

func (a *Agent) serveSocketService(w http.ResponseWriter, r *http.Request) {
	reply := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/services/"), "/")
	name := parts[0]
	if _, ok := a.socketService(name); !ok {
		reply(404, map[string]string{"code": "socket_service_not_found"})
		return
	}
	var value any
	var err error
	switch {
	case r.Method == http.MethodGet && len(parts) == 1:
		var raw []byte
		raw, _, err = a.socketSnapshot(name)
		value = json.RawMessage(raw)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[1] == "credentials":
		value, err = a.SocketCredentials(name, r.URL.Query().Get("delivery_id"))
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "results":
		var request SocketResult
		err = strictJSON(http.MaxBytesReader(w, r.Body, 32768), &request)
		if err == nil {
			value, err = a.SocketResult(r.Context(), name, request)
		}
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "received":
		var request struct {
			DeliveryID string `json:"delivery_id"`
		}
		err = strictJSON(http.MaxBytesReader(w, r.Body, 4096), &request)
		if err == nil {
			value, err = a.SocketReceived(name, request.DeliveryID)
		}
	case r.Method == http.MethodPost && len(parts) == 2 && parts[1] == "status":
		var request struct {
			Status    string `json:"status"`
			ErrorCode string `json:"error_code"`
		}
		err = strictJSON(http.MaxBytesReader(w, r.Body, 4096), &request)
		if err == nil {
			err = a.SocketStatus(r.Context(), name, request.Status, request.ErrorCode)
			value = map[string]bool{"accepted": err == nil}
		}
	case r.Method == http.MethodPost && len(parts) == 4 && parts[1] == "commands" && parts[3] == "result":
		var request struct {
			Status    string `json:"status"`
			ErrorCode string `json:"error_code"`
		}
		err = strictJSON(http.MaxBytesReader(w, r.Body, 4096), &request)
		if err == nil {
			value, err = a.SocketCommandResult(r.Context(), name, parts[2], request.Status, request.ErrorCode)
		}
	default:
		reply(404, map[string]string{"code": "not_found"})
		return
	}
	if err != nil {
		status, code := localError(err)
		reply(status, map[string]string{"code": code})
		return
	}
	reply(200, value)
}

// The production Remote is the Go SDK. Keep optional metadata capabilities
// separate from the Agent's synchronization contract and its test transports.
type metadataRemote interface {
	GetApplication(context.Context) (pam.ApplicationInfo, error)
	GetClientInfo(context.Context) (pam.ClientInfo, error)
	ListPolicies(context.Context, string) ([]pam.CredentialPolicy, error)
	GetPolicy(context.Context, pam.PolicySelector) (pam.CredentialPolicy, error)
}

func (a *Agent) localMetadata(ctx context.Context, operation, search, key, id string) (value any, err error) {
	defer func() {
		var failure *pam.PAMError
		if errors.As(err, &failure) && (failure.StatusCode == 401 || failure.StatusCode == 403) {
			err = a.failure(err, true)
		}
	}()
	remote, ok := a.remote.(metadataRemote)
	if !ok {
		return nil, errors.New("metadata_not_supported")
	}
	switch operation {
	case "application":
		return remote.GetApplication(ctx)
	case "instance":
		return remote.GetClientInfo(ctx)
	case "policies":
		return remote.ListPolicies(ctx, search)
	case "policy":
		return remote.GetPolicy(ctx, pam.PolicySelector{Key: key, PolicyID: id})
	}
	return nil, errors.New("unknown_metadata_operation")
}

func (a *Agent) serveMetadata(w http.ResponseWriter, r *http.Request) {
	operation := strings.TrimPrefix(r.URL.Path, "/v1/")
	query := r.URL.Query()
	key, id := query.Get("key"), query.Get("policy_id")
	value, err := a.localMetadata(r.Context(), operation, query.Get("search"), key, id)
	if err != nil {
		status, code := localError(err)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}
