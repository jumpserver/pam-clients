package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	pam "github.com/jumpserver/pam-clients/go"
)

// Remote is implemented by the signed Go SDK; tests can replace the transport.
type Remote interface {
	ListAuthorizedAccounts(context.Context) ([]pam.AuthorizedAccount, error)
	SyncAgent(context.Context, pam.AgentSyncOptions) (pam.AgentSync, error)
	GetCredentialFresh(context.Context, pam.CredentialSelector) (pam.Credential, error)
	WatchCredentialEvents(context.Context, func(pam.Event) error) error
	ConfirmEvent(context.Context, string, string, string) (pam.CommandResult, error)
	ListApplicationCommands(context.Context) ([]pam.Event, error)
	ReportApplicationCommandResult(context.Context, string, string, string) (pam.CommandResult, error)
	ReportStatus(context.Context, pam.ApplicationStatus) error
}

type Applied struct {
	Key       string `json:"key"`
	Revision  int64  `json:"revision"`
	AccountID string `json:"account_id"`
	Confirmed bool   `json:"confirmed"`
	Result    string `json:"result,omitempty"`
}

type State struct {
	StateVersion            int                            `json:"state_version"`
	Events                  map[string]pam.Event           `json:"pending_events"`
	Identity                string                         `json:"identity"`
	InstanceID              string                         `json:"instance_id,omitempty"`
	LocalDeliveryHash       string                         `json:"local_delivery_hash"`
	Latest                  map[string]Credential          `json:"credentials"`
	Delivered               map[string]int64               `json:"delivered"`
	Applied                 map[string]Applied             `json:"applied"`
	AccountUsage            map[string]AccountUsage        `json:"account_usage"`
	ReportedAccounts        []string                       `json:"reported_accounts,omitempty"`
	Authorized              map[string]bool                `json:"authorized"`
	Wanted                  map[string]int64               `json:"wanted_revisions"`
	Commands                map[string]pam.Event           `json:"pending_commands"`
	Scope                   pam.AgentScope                 `json:"scope"`
	Digest                  string                         `json:"config_digest"`
	Denied                  bool                           `json:"access_denied"`
	Status                  string                         `json:"sync_status"`
	SyncError               string                         `json:"sync_error,omitempty"`
	ServiceVersions         map[string]map[string]string   `json:"service_versions,omitempty"`
	StoreVersions           map[string]string              `json:"store_versions,omitempty"`
	StoreFiles              map[string]string              `json:"store_files,omitempty"`
	ServiceConfigHashes     map[string]string              `json:"service_config_hashes,omitempty"`
	StoreConfigHash         string                         `json:"store_config_hash,omitempty"`
	SocketServices          map[string]*SocketServiceState `json:"socket_services,omitempty"`
	SocketCommands          map[string]map[string]string   `json:"socket_commands,omitempty"`
	CompletedSocketCommands []string                       `json:"completed_socket_commands,omitempty"`
	SocketCommandDeadlines  map[string]time.Time           `json:"socket_command_deadlines,omitempty"`
}

type Agent struct {
	Config          Config
	remote          Remote
	mu              sync.Mutex
	syncMu          sync.Mutex
	state           State
	deliver         func(context.Context, Config, map[string]Credential, map[string]bool) error
	changed         chan struct{}
	socketConsumers map[string]bool
}

func New(config Config, remote Remote) (*Agent, error) {
	if err := config.ensureInstanceID(); err != nil {
		return nil, err
	}
	config = config.normalized()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	state := State{StateVersion: CurrentStateVersion, Events: map[string]pam.Event{}, Latest: map[string]Credential{}, Delivered: map[string]int64{}, Applied: map[string]Applied{}, Authorized: map[string]bool{}, Wanted: map[string]int64{}, Commands: map[string]pam.Event{}}
	identity, _ := json.Marshal([]string{config.Endpoint, config.AppID, config.OrgID, config.InstanceID})
	if config.Local {
		identity = append(identity, []byte("local")...)
	}
	digest := sha256.Sum256(identity)
	expectedIdentity := hex.EncodeToString(digest[:])
	state.Identity = expectedIdentity
	localDelivery, _ := json.Marshal(struct {
		Delivery DeliveryConfiguration
		Rules    []Rule
		Store    *StoreConfiguration
		Services []Rule
	}{config.Delivery, config.Rules, config.Store, config.Services})

	localDigest := sha256.Sum256(localDelivery)
	expectedLocalHash := hex.EncodeToString(localDigest[:])
	state.LocalDeliveryHash = expectedLocalHash
	if _, err := os.Stat(config.StateFile); err == nil {
		if err = privateFile(config.StateFile); err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(config.StateFile)
		if err != nil {
			return nil, err
		}
		if err = decodeState(raw, &state); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if state.Identity != expectedIdentity {
		return nil, errors.New("retained PAM Agent state belongs to another identity or instance")
	}
	state.InstanceID = config.InstanceID
	if state.Latest == nil {
		state.Latest = map[string]Credential{}
	}
	if state.Delivered == nil {
		state.Delivered = map[string]int64{}
	}
	if state.Applied == nil {
		state.Applied = map[string]Applied{}
	}
	if state.Authorized == nil {
		state.Authorized = map[string]bool{}
	}
	if state.Wanted == nil {
		state.Wanted = map[string]int64{}
	}
	if state.Events == nil {
		state.Events = map[string]pam.Event{}
	}
	if state.Commands == nil {
		state.Commands = map[string]pam.Event{}
	}
	if state.LocalDeliveryHash != expectedLocalHash {

		state.LocalDeliveryHash = expectedLocalHash
	}
	if state.ServiceVersions == nil {
		state.ServiceVersions = map[string]map[string]string{}
	}
	if state.StoreVersions == nil {
		state.StoreVersions = map[string]string{}
	}
	if state.StoreFiles == nil {
		state.StoreFiles = map[string]string{}
	}

	prepareServiceVersions(config, &state)

	if err := validateScope(state.Scope); err != nil {
		return nil, err
	}
	service := &Agent{Config: config, remote: remote, state: state, deliver: Deliver, changed: make(chan struct{})}
	service.restoreSocketServices()

	if err := service.pruneServiceState(); err != nil {
		return nil, err
	}

	if err := service.restoreServiceUsage(); err != nil {
		return nil, err
	}
	if config.instanceIDGenerated {
		if err := service.persist(); err != nil {
			return nil, err
		}
	}
	return service, nil
}

// Call with mu held once the Agent is shared. Wake local consumers only after
// the atomic state write succeeds, so a receipt never outruns persistence.
func (a *Agent) persist() error {
	a.revokeSocketServicesLocked()
	if err := writeJSON(a.Config.StateFile, a.state); err != nil {
		return err
	}
	if err := a.writePullTimesLocked(); err != nil {
		return err
	}
	if a.changed != nil {
		close(a.changed)
	}
	a.changed = make(chan struct{})
	return nil
}

func denied(err error) bool {
	var failure *pam.PAMError
	return errors.As(err, &failure) && (failure.StatusCode == 401 || failure.StatusCode == 403 || failure.StatusCode == 404)
}

func (a *Agent) failure(err error, identity bool) error {
	a.mu.Lock()
	a.state.Status = "error"
	a.state.SyncError = "agent_sync_failed"

	if identity && denied(err) {
		a.state.Denied = true
		a.state.SyncError = "agent_access_denied"

		clear(a.state.Latest)
		clear(a.state.Authorized)
		clear(a.state.Wanted)
		clear(a.state.Delivered)
		clear(a.state.Applied)

	}
	persistence := a.persist()
	a.mu.Unlock()
	if persistence != nil {
		return persistence
	}
	if identity && denied(err) {
		return errors.Join(err, a.removeRevokedStoreFiles())
	}
	return err
}

func (a *Agent) LocalCredential(key string) (Credential, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.Denied {
		return Credential{}, errors.New("agent_access_denied")
	}
	if !a.state.Authorized[key] {
		return Credential{}, errors.New("credential_not_authorized")
	}
	value, found := a.state.Latest[key]
	if !found {
		return value, errors.New("credential_not_available")
	}
	return value, nil
}

func (a *Agent) Health() map[string]string {
	a.mu.Lock()
	defer a.mu.Unlock()
	status := "ok"
	if a.state.Denied {
		status = "denied"
	}
	health := map[string]string{"status": status, "sync_status": a.state.Status}
	if a.state.SyncError != "" {
		health["error_code"] = a.state.SyncError
	}
	return health
}

// HandleEvent reduces local authorization before any HTTP work. A failed refresh
// preserves the latest password and its required revision for later retries.
func (a *Agent) HandleEvent(ctx context.Context, event pam.Event) error {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if event.CommandID != "" {
		return errors.Join(a.recordEvent("received", event), a.command(ctx, event))
	}
	switch event.Event {
	case "snapshot", pam.CredentialSecretUpdated, pam.CredentialSwitchPublished, pam.CredentialRefreshRequested, "app.scope.updated":
	default:
		return a.recordEvent("received", event)
	}
	if (event.Event == "snapshot" || event.Event == "app.scope.updated") && event.AuthorizedAccountIDs == nil {
		return errors.New("account authorization is missing from scope event")
	}
	a.mu.Lock()
	if event.Event == "snapshot" || event.Event == "app.scope.updated" {
		a.applyAccountAuthorizationLocked(event.AuthorizedAccountIDs)
	}
	switch event.Event {
	case "snapshot":
		for _, item := range event.Credentials {
			key := eventKey(item)
			if item.Revision > a.state.Wanted[key] {
				a.state.Wanted[key] = item.Revision
			}
		}
	case pam.CredentialSecretUpdated, pam.CredentialSwitchPublished, pam.CredentialRefreshRequested:
		key := eventKey(event)
		if key != "" && event.Revision >= a.state.Wanted[key] {
			a.state.Wanted[key] = event.Revision
		}
	}
	err := a.persist()
	a.mu.Unlock()
	if err != nil {
		return err
	}

	if event.Event == "snapshot" || event.Event == "app.scope.updated" {
		if err := a.removeRevokedStoreFiles(); err != nil {
			return err
		}
	}
	return errors.Join(a.recordEvent("received", event), a.synchronizeServices(ctx, true))

}

// Push membership describes notifications and rotation following. Only the
// explicit account authorization list can revoke an otherwise usable account.
// Apply reductions before HTTP synchronization so an outage cannot retain access.
func (a *Agent) applyAccountAuthorizationLocked(ids []string) {
	allowed := make(map[string]bool, len(ids))
	for _, id := range ids {
		allowed[id] = true
	}
	for key := range a.state.Authorized {
		id := a.state.Latest[key].AccountID
		if id == "" && strings.HasPrefix(key, "account:") {
			id = strings.TrimPrefix(key, "account:")
		}
		if id != "" && !allowed[id] {
			delete(a.state.Authorized, key)
			delete(a.state.Latest, key)
			delete(a.state.Wanted, key)
			delete(a.state.Delivered, key)
			delete(a.state.Applied, key)
			delete(a.state.Events, key)
		}
	}
	for selector, usage := range a.state.AccountUsage {
		if !allowed[usage.AccountID] {
			delete(a.state.AccountUsage, selector)
		}
	}
	reported := a.state.ReportedAccounts[:0]
	for _, id := range a.state.ReportedAccounts {
		if allowed[id] {
			reported = append(reported, id)
		}
	}
	a.state.ReportedAccounts = reported
}

func eventKey(event pam.Event) string {
	key := event.Key
	if event.CredentialKey != "" {
		key = event.CredentialKey
	}
	if event.CredentialMode == "subscription" && event.AccountID != "" && !strings.Contains(key, ":") && key != "" {
		return key + ":" + event.AccountID
	}
	return key
}

func (a *Agent) Sync(ctx context.Context) error {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	return a.synchronize(ctx)
}

func (a *Agent) synchronize(ctx context.Context) error {
	return a.synchronizeServices(ctx, false)
}

func sameSwitch(a, b *pam.AccountSwitch) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return slices.Equal(a.AccountIDs, b.AccountIDs)
}

func validateAccountSwitch(value *pam.AccountSwitch, active string) error {
	if value == nil {
		return nil
	}
	if len(value.AccountIDs) < 2 {
		return errors.New("account switch requires at least two accounts")
	}
	seen := map[string]bool{}
	for _, id := range value.AccountIDs {
		if !validKey(id) || seen[id] {
			return errors.New("invalid account switch account_ids")
		}
		seen[id] = true
	}
	if active != "" && !seen[active] {
		return errors.New("active account is not in account switch")
	}
	return nil
}

// Retire pre-account-key subscription state after the signed scope and new
// delivery have succeeded. Only Agent-created default files can be identified
// safely; custom rule destinations remain under the local administrator's control.

// ConfirmEvent is called by the application after it has actually applied a delivered event.
func (a *Agent) ConfirmEvent(ctx context.Context, eventID string) (Applied, error) {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	a.mu.Lock()
	var target pam.Event
	for _, event := range a.state.Events {
		if event.EventID == eventID {
			target = event
			break
		}
	}
	if target.EventID == "" {
		for _, event := range a.state.Commands {
			if event.EventID == eventID {
				target = event
				break
			}
		}
	}
	a.mu.Unlock()
	if target.EventID == "" {
		return Applied{}, errors.New("unknown application event")
	}
	a.mu.Lock()
	value := a.state.Latest[eventKey(target)]
	matches := value.AccountID == target.AccountID && value.AccountRevision == target.AccountRevision
	a.mu.Unlock()
	if !matches {
		return Applied{}, errors.New("event account version is superseded")
	}
	return a.confirm(ctx, eventKey(target), target.Revision)
}

func (a *Agent) confirm(ctx context.Context, key string, revision int64) (Applied, error) {
	a.mu.Lock()
	value, exists := a.state.Latest[key]
	deliveredRevision, delivered := a.state.Delivered[key]
	if a.state.Denied || !a.state.Authorized[key] || !exists || !delivered || deliveredRevision != revision || value.Revision != revision || a.state.Events[key].EventID == "" {
		a.mu.Unlock()
		return Applied{}, errors.New("confirm an authorized alternating-rotation revision actually applied by the application")
	}
	applied := Applied{Key: key, Revision: revision, AccountID: value.AccountID}
	a.state.Applied[key] = applied
	err := a.persist()
	a.mu.Unlock()
	if err != nil {
		return applied, err
	}
	_ = a.reportPending(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.Applied[key], nil
}

func (a *Agent) reportPending(ctx context.Context) error {
	if err := a.reportSocketFailures(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	var applied []Applied
	for key, value := range a.state.Applied {
		current := a.state.Latest[key]
		if !a.state.Denied && a.state.Authorized[key] && a.state.Events[key].EventID != "" && !value.Confirmed && current.Revision == value.Revision && current.AccountID == value.AccountID {
			applied = append(applied, value)
		}
	}
	a.mu.Unlock()
	for _, value := range applied {
		a.mu.Lock()
		event := a.state.Events[value.Key]
		accountRevision := a.state.Latest[value.Key].AccountRevision
		a.mu.Unlock()
		if event.EventID != "" {
			if event.Revision != value.Revision || event.AccountID != value.AccountID || accountRevision != event.AccountRevision {
				continue
			}
			status := value.Result
			if status == "" {
				status = "success"
			}
			result, err := a.remote.ConfirmEvent(ctx, event.EventID, status, "")
			if err != nil {
				return a.failure(err, true)
			}
			if result.Status != status && result.Status != "success" {
				return errors.New("Core rejected the event application result")
			}
		}

		a.mu.Lock()
		value.Confirmed = true
		a.state.Applied[value.Key] = value
		err := a.persist()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	a.mu.Lock()
	pending := make([]pam.Event, 0, len(a.state.Commands))
	for _, event := range a.state.Commands {
		pending = append(pending, event)
	}
	a.mu.Unlock()
	for _, event := range pending {
		if err := a.finishSwitch(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) finishSwitch(ctx context.Context, event pam.Event) error {
	if event.Event == "app.restart.requested" {
		return a.finishSocketRestart(ctx, event)
	}
	key := eventKey(event)
	a.mu.Lock()
	applied := a.state.Applied[key]
	latest := a.state.Latest[key]
	ready := !a.state.Denied && a.state.Authorized[key] && applied.Revision == event.Revision && applied.AccountID == event.AccountID && latest.Revision == event.Revision && latest.AccountID == event.AccountID
	a.mu.Unlock()
	if !ready {
		return nil
	}
	var err error
	status := applied.Result
	if status == "" {
		status = "success"
	}
	if event.EventID != "" {
		_, err = a.remote.ConfirmEvent(ctx, event.EventID, status, "")
	} else {
		_, err = a.remote.ReportApplicationCommandResult(ctx, event.CommandID, status, "")
	}
	if err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.state.Commands, event.CommandID)
	err = a.persist()
	a.mu.Unlock()
	return err
}

func (a *Agent) command(ctx context.Context, event pam.Event) error {
	claim, err := a.remote.ReportApplicationCommandResult(ctx, event.CommandID, "running", "")
	if err != nil {
		return a.failure(err, true)
	}
	if !claim.Accepted {
		if claim.Status == "running" && event.Event == "app.restart.requested" {
			if queued, err := a.queueSocketRestart(event); queued || err != nil {
				return err
			}
		}
		if claim.Status == "running" && (event.Event == "credential.switch.requested" || event.Event == "app.restart.requested") {
			return a.finishSwitch(ctx, event)
		}
		return nil
	}
	switch event.Event {
	case "app.restart.requested":
		if queued, err := a.queueSocketRestart(event); queued || err != nil {
			return err
		}
		d := a.Config.Delivery
		if d.Mode != "environment" || d.Operation != "restart" {
			err = errors.New("application restart is not locally configured")
			break
		}
		err = execute(ctx, Action{Type: "systemd", Unit: d.Unit, Operation: "restart"}, Payload{})
		if err == nil {
			err = checkService(ctx, d.Unit)
		}
	case "credential.switch.requested":
		key := eventKey(event)
		a.mu.Lock()
		a.state.Wanted[key] = event.Revision
		a.mu.Unlock()

		err = a.synchronizeServices(ctx, true)

		if err != nil {
			break
		}
		a.mu.Lock()
		current, ok := a.state.Latest[key]
		authorized := a.state.Authorized[key]
		a.mu.Unlock()
		if !ok || !authorized || current.Revision != event.Revision || current.AccountID != event.AccountID {
			err = errors.New("requested account version is superseded")
			break
		}
		a.mu.Lock()
		a.state.Commands[event.CommandID] = event
		err = a.persist()
		a.mu.Unlock()
		if err == nil {
			return a.finishSwitch(ctx, event)
		}
	default:
		err = errors.New("unsupported application command")
	}
	if err != nil {
		_, _ = a.remote.ReportApplicationCommandResult(ctx, event.CommandID, "failed", "execution_failed")
		return err
	}
	if event.EventID != "" {
		_, err = a.remote.ConfirmEvent(ctx, event.EventID, "success", "")
	} else {
		_, err = a.remote.ReportApplicationCommandResult(ctx, event.CommandID, "success", "")
	}
	return err
}

func (a *Agent) pollCommands(ctx context.Context) error {
	commands, err := a.remote.ListApplicationCommands(ctx)
	if err != nil {
		return a.failure(err, true)
	}
	var result error
	for _, event := range commands {
		a.syncMu.Lock()
		err = a.command(ctx, event)
		a.syncMu.Unlock()
		if err != nil {
			result = errors.New("one or more application commands failed")
		}
	}
	return result
}

func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	server, err := a.startServer()
	if err != nil {
		return err
	}
	defer server.Close()
	events := make(chan pam.Event, 128)
	readerDone := make(chan error, 1)
	go func() {
		readerDone <- a.remote.WatchCredentialEvents(ctx, func(event pam.Event) error {
			select {
			case events <- event:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	defer func() { cancel(); <-readerDone }()
	interval := time.Duration(a.Config.PullIntervalSeconds) * time.Second
	periodicDeadline := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	delay := time.Second
	schedule := func(err error) {
		next := interval
		if err != nil {
			log.Printf("PAM Agent synchronization or delivery failed: %T", err)
			next = delay
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		} else {
			delay = time.Second
		}

		remaining := time.Until(periodicDeadline)
		if remaining < 0 {
			remaining = 0
		}
		if remaining < next {
			next = remaining
		}

		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(next)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event := <-events:
			schedule(a.HandleEvent(ctx, event))
		case <-timer.C:
			periodic := !time.Now().Before(periodicDeadline)
			var err error
			if periodic {
				periodicDeadline = time.Now().Add(interval)
				err = a.Sync(ctx)
			} else {
				a.syncMu.Lock()
				err = a.synchronizeServices(ctx, true)
				a.syncMu.Unlock()
			}
			if err == nil {
				err = a.pollCommands(ctx)
			}
			schedule(err)
		}
	}
}

func sortedKeys(values map[string]Credential) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
