package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"sort"
	"time"

	pam "github.com/jumpserver/pam-clients/go"
)

var errSocketService = errors.New("socket_service_not_found")
var errSocketConflict = errors.New("socket_delivery_superseded")
var socketErrorCode = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,128}$`)

// SocketVersion deliberately contains no passwords or arbitrary Core payloads.
// The map key remains the declared account ID across A/B switches.
type SocketVersion struct {
	Key             string `json:"key"`
	AccountID       string `json:"account_id"`
	Revision        int64  `json:"revision"`
	AccountRevision int64  `json:"account_revision"`
	EventID         string `json:"event_id,omitempty"`
}

type SocketDelivery struct {
	ID                string                   `json:"delivery_id"`
	Event             string                   `json:"event"`
	ServiceID         string                   `json:"service_id"`
	Credentials       map[string]SocketVersion `json:"credentials"`
	RevokedAccountIDs []string                 `json:"revoked_account_ids"`
}

type SocketServiceState struct {
	Delivery        SocketDelivery `json:"delivery"`
	Received        bool           `json:"received"`
	CredentialsRead bool           `json:"credentials_read"`
	Result          string         `json:"result"`
	ErrorCode       string         `json:"error_code,omitempty"`
	Attempts        int            `json:"attempts,omitempty"`
	RetryAt         time.Time      `json:"retry_at,omitempty"`
	FailureReported bool           `json:"failure_reported,omitempty"`
	Status          string         `json:"status,omitempty"`
	StatusError     string         `json:"status_error,omitempty"`
}

type SocketResult struct {
	DeliveryID string `json:"delivery_id"`
	Status     string `json:"status"`
	ErrorCode  string `json:"error_code,omitempty"`
}

func (c Config) restartSupported() bool {
	if c.Delivery.Mode == "environment" && c.Delivery.Operation == "restart" {
		return true
	}
	for _, service := range c.Services {
		if service.Delivery == "socket" && service.RestartSupported {
			return true
		}
	}
	return false
}

func (a *Agent) socketService(name string) (Rule, bool) {
	for _, service := range a.Config.Services {
		if service.Name == name && service.Delivery == "socket" {
			return service, true
		}
	}
	return Rule{}, false
}

func (a *Agent) restoreSocketServices() {
	if a.state.SocketServices == nil {
		a.state.SocketServices = map[string]*SocketServiceState{}
	}
	if a.state.SocketCommands == nil {
		a.state.SocketCommands = map[string]map[string]string{}
	}
	if a.state.SocketCommandDeadlines == nil {
		a.state.SocketCommandDeadlines = map[string]time.Time{}
	}
	for name, state := range a.state.SocketServices {
		if _, ok := a.socketService(name); !ok || state == nil || state.Delivery.ID != "" && state.Delivery.ID != socketDelivery(name, state.Delivery.Event, state.Delivery.Credentials, state.Delivery.RevokedAccountIDs, a.state.ServiceConfigHashes[name]).ID {
			delete(a.state.SocketServices, name)
		}
	}
}

func socketDelivery(name, event string, credentials map[string]SocketVersion, revoked []string, configHash string) SocketDelivery {
	if revoked == nil {
		revoked = []string{}
	}
	sort.Strings(revoked)
	value := SocketDelivery{Event: event, ServiceID: name, Credentials: credentials, RevokedAccountIDs: revoked}
	raw, _ := json.Marshal(struct {
		Delivery SocketDelivery
		Config   string
	}{value, configHash})
	sum := sha256.Sum256(raw)
	value.ID = hex.EncodeToString(sum[:])
	return value
}

// Called under mu, using the same resolved payload as file/script delivery.
func (a *Agent) queueSocketServiceLocked(service Rule, payload Payload) error {
	versions := map[string]SocketVersion{}
	for selector, value := range payload.Credentials {
		versions[selector] = SocketVersion{value.Key, value.AccountID, value.Revision, value.AccountRevision, value.EventID}
	}
	delivery := socketDelivery(service.Name, "credential.updated", versions, nil, a.state.ServiceConfigHashes[service.Name])
	previous := a.state.SocketServices[service.Name]
	if previous != nil && previous.Delivery.ID == delivery.ID {
		return nil
	}
	next := &SocketServiceState{Delivery: delivery, Result: "pending", Status: "starting"}
	if previous != nil {
		next.Status, next.StatusError = previous.Status, previous.StatusError
	}
	// Matching versions were already applied by this service. A new Core event
	// for that exact value needs only another result report, not another callback.
	applied := len(versions) > 0
	for _, value := range versions {
		current := a.state.Latest[value.Key]
		if a.state.ServiceVersions[service.Name][value.Key] != credentialVersion(current) {
			applied = false
		}
	}
	if applied {
		next.Result = "success"
	}
	a.state.SocketServices[service.Name] = next
	if err := a.persist(); err != nil {
		if previous == nil {
			delete(a.state.SocketServices, service.Name)
		} else {
			a.state.SocketServices[service.Name] = previous
		}
		return err
	}
	return nil
}

// Authorization reductions wake consumers even if the subsequent HTTP refresh
// fails. Keep revocation metadata across restart until business cleanup succeeds.
func (a *Agent) revokeSocketServicesLocked() {
	for name, state := range a.state.SocketServices {
		if state == nil {
			continue
		}
		versions := map[string]SocketVersion{}
		revoked := map[string]bool{}
		for _, id := range state.Delivery.RevokedAccountIDs {
			revoked[id] = true
		}
		changed := false
		for selector, value := range state.Delivery.Credentials {
			if a.state.Denied || !a.state.Authorized[value.Key] {
				revoked[value.AccountID] = true
				if usage := a.state.AccountUsage["service:"+name+":"+selector]; usage.AccountID != "" {
					revoked[usage.AccountID] = true
				}
				delete(a.state.AccountUsage, "service:"+name+":"+selector)
				changed = true
			} else {
				versions[selector] = value
			}
		}
		if !changed {
			continue
		}
		ids := []string{}
		for id := range revoked {
			ids = append(ids, id)
		}
		next := &SocketServiceState{Delivery: socketDelivery(name, "credential.revoked", versions, ids, a.state.ServiceConfigHashes[name]), Result: "pending", Status: "degraded", StatusError: "credential_revoked"}
		a.state.SocketServices[name] = next
	}
}

func (a *Agent) socketDeliveryCurrentLocked(state *SocketServiceState) bool {
	if a.state.SyncError == "client_upgrade_required" {
		return false
	}
	for _, expected := range state.Delivery.Credentials {
		value, exists := a.state.Latest[expected.Key]
		if a.state.Denied || !a.state.Authorized[expected.Key] || !exists || value.AccountID != expected.AccountID || value.Revision != expected.Revision || value.AccountRevision != expected.AccountRevision || a.state.Wanted[expected.Key] > value.Revision || a.state.Wanted["account:"+value.AccountID] > value.AccountRevision {
			return false
		}
	}
	return true
}

// SocketCredentials never returns an earlier A/B value for a superseded local
// delivery. Fetching does not advance usage, delivery, or confirmation state.
func (a *Agent) SocketCredentials(name, id string) (map[string]Credential, error) {
	if _, ok := a.socketService(name); !ok {
		return nil, errSocketService
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.SyncError == "client_upgrade_required" {
		return nil, &pam.PAMError{StatusCode: 426, Code: "client_upgrade_required"}
	}
	state := a.state.SocketServices[name]
	if state == nil || id == "" || id != state.Delivery.ID || !a.socketDeliveryCurrentLocked(state) {
		return nil, errSocketConflict
	}
	result := map[string]Credential{}
	for selector, expected := range state.Delivery.Credentials {
		result[selector] = a.state.Latest[expected.Key]
	}
	if !state.CredentialsRead || !state.Received {
		beforeRead, beforeReceived := state.CredentialsRead, state.Received
		state.CredentialsRead, state.Received = true, true
		if err := a.persist(); err != nil {
			state.CredentialsRead, state.Received = beforeRead, beforeReceived
			return nil, err
		}
	}
	return result, nil
}

func (a *Agent) SocketReceived(name, id string) (map[string]bool, error) {
	if _, ok := a.socketService(name); !ok {
		return nil, errSocketService
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.state.SocketServices[name]
	if state == nil || id == "" || state.Delivery.ID != id || !a.socketDeliveryCurrentLocked(state) {
		return nil, errSocketConflict
	}
	if !state.Received {
		state.Received = true
		if err := a.persist(); err != nil {
			state.Received = false
			return nil, err
		}
	}
	return map[string]bool{"accepted": true}, nil
}

func validSocketResult(status, code string) bool {
	return (status == "success" || status == "failed") && socketErrorCode.MatchString(code) && (status != "success" || code == "")
}

// Record business success before any network work. Repeated acknowledgements
// retry reporting only; service versions prevent re-executing successful work.
func (a *Agent) SocketResult(ctx context.Context, name string, result SocketResult) (map[string]string, error) {
	if _, ok := a.socketService(name); !ok {
		return nil, errSocketService
	}
	if !validSocketResult(result.Status, result.ErrorCode) {
		return nil, errors.New("invalid_socket_result")
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	a.mu.Lock()
	if a.state.SyncError == "client_upgrade_required" {
		a.mu.Unlock()
		return nil, &pam.PAMError{StatusCode: 426, Code: "client_upgrade_required"}
	}
	state := a.state.SocketServices[name]
	if state == nil || result.DeliveryID == "" || result.DeliveryID != state.Delivery.ID || !a.socketDeliveryCurrentLocked(state) {
		a.mu.Unlock()
		return nil, errSocketConflict
	}
	if state.Result == "success" {
		if result.Status != "success" {
			a.mu.Unlock()
			return nil, errSocketConflict
		}
		a.mu.Unlock()
		// Reconcile pending Core results without reapplying the business callback.
		_ = a.synchronizeServices(ctx, true)
		return map[string]string{"status": "accepted"}, nil
	}
	previous := *state
	previousVersions := a.state.ServiceVersions[name]
	previousUsage := maps.Clone(a.state.AccountUsage)
	state.Result, state.ErrorCode = result.Status, result.ErrorCode
	state.FailureReported = false
	if result.Status == "success" {
		state.Status, state.StatusError = "running", ""
		versions := map[string]string{}
		for selector, expected := range state.Delivery.Credentials {
			value := a.state.Latest[expected.Key]
			versions[expected.Key] = credentialVersion(value)
			a.state.AccountUsage["service:"+name+":"+selector] = usageFromCredential(value)
		}
		a.state.ServiceVersions[name] = versions
	} else {
		state.Status, state.StatusError = "degraded", result.ErrorCode
		state.Attempts++
		delay := time.Second * time.Duration(1<<min(state.Attempts-1, 5))
		state.RetryAt = time.Now().Add(min(delay, 30*time.Second))
	}
	err := a.persist()
	if err != nil {
		a.state.SocketServices[name] = &previous
		if previousVersions == nil {
			delete(a.state.ServiceVersions, name)
		} else {
			a.state.ServiceVersions[name] = previousVersions
		}
		a.state.AccountUsage = previousUsage
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	// Existing synchronization computes all-destination readiness and writes its
	// durable confirmation outbox. It also retries after an offline acceptance.
	if result.Status == "success" {
		_ = a.synchronizeServices(ctx, true)
	} else {
		_ = a.reportPending(ctx)
	}
	a.reportAccountUsage(ctx)
	return map[string]string{"status": "accepted"}, nil
}

func (a *Agent) reportSocketFailures(ctx context.Context) error {
	a.mu.Lock()
	pending := map[string]SocketServiceState{}
	for name, state := range a.state.SocketServices {
		if state.Result == "failed" && !state.FailureReported && a.socketDeliveryCurrentLocked(state) {
			pending[name] = *state
		}
	}
	a.mu.Unlock()
	for name, state := range pending {
		ids := map[string]bool{}
		for _, value := range state.Delivery.Credentials {
			if value.EventID != "" {
				ids[value.EventID] = true
			}
		}
		for id := range ids {
			result, err := a.remote.ConfirmEvent(ctx, id, "failed", "application_failed")
			if err != nil {
				return a.failure(err, true)
			}
			if result.Status != "failed" && result.Status != "success" {
				return errors.New("Core rejected the business failure result")
			}
		}
		// A failed application attempt also completes any matching manual
		// switch command as failed. Retrying the desired credential afterwards
		// remains independent of re-executing a terminal command.
		a.mu.Lock()
		commands := []pam.Event{}
		for _, command := range a.state.Commands {
			if command.Event != "credential.switch.requested" {
				continue
			}
			for _, value := range state.Delivery.Credentials {
				if value.Key == eventKey(command) && value.Revision == command.Revision && value.AccountID == command.AccountID {
					commands = append(commands, command)
					break
				}
			}
		}
		a.mu.Unlock()
		for _, command := range commands {
			result, err := a.remote.ReportApplicationCommandResult(ctx, command.CommandID, "failed", "application_failed")
			if err != nil {
				return a.failure(err, true)
			}
			if result.Status != "failed" && result.Status != "success" {
				return errors.New("Core rejected the switch command result")
			}
			a.mu.Lock()
			delete(a.state.Commands, command.CommandID)
			err = a.persist()
			a.mu.Unlock()
			if err != nil {
				return err
			}
		}
		a.mu.Lock()
		if current := a.state.SocketServices[name]; current != nil && current.Delivery.ID == state.Delivery.ID && current.Result == "failed" {
			current.FailureReported = true
		}
		err := a.persist()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// Restart commands share the existing Agent command claim and retry outbox.
// Targets are frozen before local delivery; one service cannot ACK another.
func (a *Agent) queueSocketRestart(event pam.Event) (bool, error) {
	targets := map[string]string{}
	for _, service := range a.Config.Services {
		if service.Delivery == "socket" && service.RestartSupported {
			targets[service.Name] = "pending"
		}
	}
	if len(targets) == 0 {
		return false, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	previousEvent, hadEvent := a.state.Commands[event.CommandID]
	previousTargets, hadTargets := a.state.SocketCommands[event.CommandID]
	previousDeadline, hadDeadline := a.state.SocketCommandDeadlines[event.CommandID]
	if _, exists := a.state.SocketCommands[event.CommandID]; !exists {
		a.state.SocketCommands[event.CommandID] = targets
	}
	a.state.Commands[event.CommandID] = event
	var expires string
	if json.Unmarshal(event.Data["expires_at"], &expires) == nil {
		if deadline, err := time.Parse(time.RFC3339Nano, expires); err == nil {
			a.state.SocketCommandDeadlines[event.CommandID] = deadline
		}
	}
	err := a.persist()
	if err != nil {
		if hadEvent {
			a.state.Commands[event.CommandID] = previousEvent
		} else {
			delete(a.state.Commands, event.CommandID)
		}
		if hadTargets {
			a.state.SocketCommands[event.CommandID] = previousTargets
		} else {
			delete(a.state.SocketCommands, event.CommandID)
		}
		if hadDeadline {
			a.state.SocketCommandDeadlines[event.CommandID] = previousDeadline
		} else {
			delete(a.state.SocketCommandDeadlines, event.CommandID)
		}
	}
	return true, err
}

func commandExpired(event pam.Event) bool {
	var value string
	if raw := event.Data["expires_at"]; raw != nil && json.Unmarshal(raw, &value) == nil {
		if deadline, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return !time.Now().Before(deadline)
		}
	}
	return false
}

func (a *Agent) socketCommandExpiredLocked(event pam.Event) bool {
	deadline, known := a.state.SocketCommandDeadlines[event.CommandID]
	return commandExpired(event) || known && !time.Now().Before(deadline)
}

func (a *Agent) finishSocketRestart(ctx context.Context, event pam.Event) error {
	a.mu.Lock()
	targets := a.state.SocketCommands[event.CommandID]
	if a.socketCommandExpiredLocked(event) {
		delete(a.state.Commands, event.CommandID)
		delete(a.state.SocketCommands, event.CommandID)
		delete(a.state.SocketCommandDeadlines, event.CommandID)
		err := a.persist()
		a.mu.Unlock()
		return err
	}
	status := "success"
	for _, result := range targets {
		if result == "pending" {
			a.mu.Unlock()
			return nil
		}
		if result == "failed" {
			status = "failed"
		}
	}
	if len(targets) == 0 {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()
	code := ""
	if status == "failed" {
		code = "application_failed"
	}
	var response pam.CommandResult
	var err error
	if event.EventID != "" {
		response, err = a.remote.ConfirmEvent(ctx, event.EventID, status, code)
	} else {
		response, err = a.remote.ReportApplicationCommandResult(ctx, event.CommandID, status, code)
	}
	if err != nil {
		return err
	}
	if response.Status != status {
		return errors.New("Core rejected the command result")
	}
	a.mu.Lock()
	delete(a.state.Commands, event.CommandID)
	a.state.CompletedSocketCommands = append(a.state.CompletedSocketCommands, event.CommandID)
	if len(a.state.CompletedSocketCommands) > 128 {
		delete(a.state.SocketCommands, a.state.CompletedSocketCommands[0])
		delete(a.state.SocketCommandDeadlines, a.state.CompletedSocketCommands[0])
		a.state.CompletedSocketCommands = a.state.CompletedSocketCommands[1:]
	}
	err = a.persist()
	a.mu.Unlock()
	return err
}

func (a *Agent) SocketCommandResult(ctx context.Context, name, id, status, code string) (map[string]string, error) {
	if _, ok := a.socketService(name); !ok {
		return nil, errSocketService
	}
	if !validSocketResult(status, code) {
		return nil, errors.New("invalid_command_result")
	}
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	a.mu.Lock()
	event, exists := a.state.Commands[id]
	targets := a.state.SocketCommands[id]
	previous, target := targets[name]
	if !exists && target && previous == status {
		a.mu.Unlock()
		return map[string]string{"status": "accepted"}, nil
	}
	if !exists || !target || a.socketCommandExpiredLocked(event) || a.state.Denied || (previous != "pending" && previous != status) {
		a.mu.Unlock()
		return nil, errSocketConflict
	}
	targets[name] = status
	err := a.persist()
	if err != nil {
		targets[name] = previous
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	_ = a.finishSocketRestart(ctx, event)
	return map[string]string{"status": "accepted"}, nil
}

func (a *Agent) SocketStatus(ctx context.Context, name, status, code string) error {
	if _, ok := a.socketService(name); !ok {
		return errSocketService
	}
	switch status {
	case "starting", "running", "degraded", "stopping", "stopped", "error":
	default:
		return errors.New("invalid_application_status")
	}
	if !socketErrorCode.MatchString(code) {
		return errors.New("invalid_error_code")
	}
	a.mu.Lock()
	state := a.state.SocketServices[name]
	existed := state != nil
	if state == nil {
		state = &SocketServiceState{Delivery: SocketDelivery{ServiceID: name}, Status: "starting"}
		a.state.SocketServices[name] = state
	}
	beforeStatus, beforeCode := state.Status, state.StatusError
	state.Status, state.StatusError = status, code
	err := a.persist()
	if err != nil {
		state.Status, state.StatusError = beforeStatus, beforeCode
		if !existed {
			delete(a.state.SocketServices, name)
		}
	}
	a.mu.Unlock()
	if err == nil {
		a.reportAccountUsage(ctx)
	}
	return err
}
