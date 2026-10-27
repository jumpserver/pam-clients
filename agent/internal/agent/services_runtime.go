package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	pam "github.com/jumpserver/pam-clients/go"
)

func credentialVersion(value Credential) string {
	return fmt.Sprintf("%s:%d:%d", value.AccountID, value.Revision, value.AccountRevision)
}

// A service permits switching wherever a unique current rotation exists; an
// account without a rotation continues to mean the explicitly selected account.
func effectiveService(service Rule, latest map[string]Credential) Rule {
	service.Accounts = append([]AccountSelector(nil), service.Accounts...)
	for index := range service.Accounts {
		account := &service.Accounts[index]
		account.AllowAccountSwitch = false
		if service.AllowAccountSwitch {
			for _, value := range latest {
				if value.AccountSwitch != nil && contains(value.AccountSwitch.AccountIDs, account.AccountID) {
					account.AllowAccountSwitch = true
				}
			}
		}
	}
	return service
}

func (a *Agent) restoreServiceUsage() error {
	previous := a.state.AccountUsage
	a.state.AccountUsage = map[string]AccountUsage{}
	for _, service := range a.Config.Services {
		for _, account := range service.Accounts {
			key := "service:" + service.Name + ":" + account.AccountID
			usage := AccountUsage{AccountID: account.AccountID}
			if old, ok := previous[key]; ok && service.AllowAccountSwitch {
				if !validKey(old.AccountID) || old.Revision < 0 || validateAccountSwitch(old.AccountSwitch, old.AccountID) != nil {
					return errors.New("invalid retained service account usage")
				}
				if old.AccountID == account.AccountID || old.AccountSwitch != nil && contains(old.AccountSwitch.AccountIDs, account.AccountID) {
					usage = old
				}
			}
			a.state.AccountUsage[key] = usage
		}
	}
	seen := map[string]bool{}
	for _, id := range a.state.ReportedAccounts {
		if !validKey(id) || seen[id] {
			return errors.New("invalid reported account usage")
		}
		seen[id] = true
	}
	return nil
}

func (a *Agent) removeStoreFiles(wanted map[string]string) error {
	a.mu.Lock()
	files := map[string]string{}
	for id, path := range a.state.StoreFiles {
		files[id] = path
	}
	a.mu.Unlock()
	for id, path := range files {
		if wanted[id] == path {
			continue
		}
		base := filepath.Base(path)
		if !accountIDPattern.MatchString(id) || !absolute(path) || (base != id+".json" && base != id+".env") {
			return errors.New("invalid managed store path")
		}
		if _, err := os.Lstat(path); err == nil {
			if err = secureTarget(path); err != nil {
				return err
			}
			if err = os.Remove(path); err != nil {
				return err
			}
		} else if !os.IsNotExist(err) {
			return err
		}
		a.mu.Lock()
		delete(a.state.StoreFiles, id)
		delete(a.state.StoreVersions, id)
		err := a.persist()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) removeRevokedStoreFiles() error {
	wanted := map[string]string{}
	a.mu.Lock()
	if !a.state.Denied {
		for id, path := range a.state.StoreFiles {
			if a.state.Authorized["account:"+id] {
				wanted[id] = path
			}
		}
	}
	a.mu.Unlock()
	return a.removeStoreFiles(wanted)
}

func (a *Agent) pruneServiceState() error {
	a.mu.Lock()
	wanted := map[string]string{}
	for key, value := range a.state.Latest {
		keep := false
		if store := a.Config.Store; store != nil && key == "account:"+value.AccountID && (contains(store.Accounts, "all") || contains(store.Accounts, value.AccountID)) {
			keep = true
			suffix := "json"
			if store.Format == "environment" {
				suffix = "env"
			}
			if a.state.Authorized[key] && !a.state.Denied {
				wanted[value.AccountID] = filepath.Join(store.Root, value.AccountID+"."+suffix)
			}
		}
		for _, service := range a.Config.Services {
			for _, account := range service.Accounts {
				if key == "account:"+account.AccountID || service.AllowAccountSwitch && value.AccountSwitch != nil && contains(value.AccountSwitch.AccountIDs, account.AccountID) {
					keep = true
				}
			}
		}
		if !keep {
			delete(a.state.Latest, key)
			delete(a.state.Authorized, key)
			delete(a.state.Delivered, key)
			delete(a.state.Applied, key)
			delete(a.state.Wanted, key)
			delete(a.state.Events, key)
		}
	}
	a.mu.Unlock()
	return a.removeStoreFiles(wanted)
}

func (a *Agent) synchronizeServices(ctx context.Context, notification bool) (result error) {
	defer func() {
		if result != nil && ctx.Err() == nil {
			a.mu.Lock()
			a.state.Status = "error"
			a.mu.Unlock()
		}
		a.reportAccountUsage(ctx)
	}()
	a.mu.Lock()
	options := pam.AgentSyncOptions{ConfigDigest: a.state.Digest, SyncStatus: a.state.Status, SyncError: a.state.SyncError, DeliveryScope: a.Config.serviceScope()}
	for key, value := range a.state.Latest {
		options.Credentials = append(options.Credentials, pam.KnownRevision{Key: key, Revision: value.Revision})
	}
	for key, revision := range a.state.Delivered {
		if a.state.Authorized[key] {
			options.DeliveredCredentials = append(options.DeliveredCredentials, pam.KnownRevision{Key: key, Revision: revision})
		}
	}
	a.mu.Unlock()
	response, err := a.remote.SyncAgent(ctx, options)
	if err != nil {
		failure := a.failure(err, true)
		if denied(err) {
			return errors.Join(failure, a.removeStoreFiles(nil))
		}
		return failure
	}
	if err = validateScope(response.Scope); err != nil {
		return a.failure(err, false)
	}
	if response.ConfigSchemaVersion != 2 {
		return a.failure(errors.New("Core must support Agent store/services configuration version 2"), false)
	}
	metadata := map[string]pam.CredentialRevision{}
	for _, item := range response.Credentials {
		if !a.Config.requestsMetadata(item) {
			return a.failure(errors.New("synchronization exceeds local account declarations"), false)
		}
		if !validKey(item.Key) || !contains(response.Scope.Keys, item.Key) || item.AccountID == "" || item.Revision < 0 || item.AccountRevision < 0 || validateAccountSwitch(item.AccountSwitch, item.AccountID) != nil {
			return a.failure(errors.New("invalid service synchronization metadata"), false)
		}
		if _, duplicate := metadata[item.Key]; duplicate {
			return a.failure(errors.New("duplicate service synchronization metadata"), false)
		}
		metadata[item.Key] = item
	}
	if len(metadata) != len(response.Scope.Keys) {
		return a.failure(errors.New("incomplete service synchronization scope"), false)
	}
	wantedFiles := map[string]string{}
	if store := a.Config.Store; store != nil {
		suffix := "json"
		if store.Format == "environment" {
			suffix = "env"
		}
		for key, item := range metadata {
			if item.Available && key == "account:"+item.AccountID && (contains(store.Accounts, "all") || contains(store.Accounts, item.AccountID)) {
				wantedFiles[item.AccountID] = filepath.Join(store.Root, item.AccountID+"."+suffix)
			}
		}
	}
	// Scope reductions are applied before any secret fetch, file write or action.
	a.mu.Lock()
	a.state.Scope = response.Scope
	a.state.Denied = false
	for key := range a.state.Latest {
		item, exists := metadata[key]
		if !exists || !item.Available {
			delete(a.state.Latest, key)
			delete(a.state.Authorized, key)
			delete(a.state.Wanted, key)
			delete(a.state.Delivered, key)
			delete(a.state.Applied, key)
			delete(a.state.Events, key)
		}
	}
	err = a.persist()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if err = a.removeStoreFiles(wantedFiles); err != nil {
		return a.failure(err, false)
	}
	var problems []error
	fetched := map[string]pam.Credential{}
	for key, item := range metadata {
		if !item.Available {
			continue
		}
		a.mu.Lock()
		previous, exists := a.state.Latest[key]
		valid := exists && a.state.Authorized[key] && previous.AccountID == item.AccountID && previous.Revision == item.Revision && previous.AccountRevision == item.AccountRevision && sameSwitch(previous.AccountSwitch, item.AccountSwitch)
		a.mu.Unlock()
		if !valid {
			value, cached := fetched[item.AccountID]
			if !cached {
				value, err = a.remote.GetCredentialFresh(ctx, pam.CredentialSelector{AccountID: item.AccountID})
				if err != nil {
					var failure *pam.PAMError
					if errors.As(err, &failure) {
						revoked := failure.Code == "credential_not_found" || failure.Code == "credential_not_selected" || failure.Code == "credential_not_authorized" || failure.Code == "credential_not_bound"
						if failure.StatusCode == 401 || failure.StatusCode == 403 && !revoked || failure.IsCompatibilityError() {
							return a.failure(err, true)
						}
					}
					if denied(err) {
						a.mu.Lock()
						delete(a.state.Authorized, key)
						delete(a.state.Latest, key)
						a.mu.Unlock()
						if cleanup := a.removeStoreFiles(withoutStoreFile(wantedFiles, item.AccountID)); cleanup != nil {
							problems = append(problems, cleanup)
						}
					}
					problems = append(problems, errors.New("service credential refresh failed"))
					continue
				}
				fetched[item.AccountID] = value
			}
			if value.Account.ID != item.AccountID || value.Account.Revision != item.AccountRevision || value.Key != "account:"+item.AccountID {
				problems = append(problems, errors.New("service credential version changed during synchronization"))
				continue
			}
			value.Key, value.Revision, value.AccountSwitch = key, item.Revision, item.AccountSwitch
			previous = flatten(value)
			if err = a.recordEvent("saved", pam.Event{Event: "credential.fetched", CredentialKey: key, Revision: item.Revision, AccountID: item.AccountID}); err != nil {
				return err
			}
		}
		previous.EventID = item.EventID
		a.mu.Lock()
		a.state.Latest[key] = previous
		a.state.Authorized[key] = true
		delete(a.state.Wanted, key)
		if item.EventID != "" {
			if a.state.Events[key].EventID != item.EventID {
				applied := a.state.Applied[key]
				applied.Confirmed = false
				a.state.Applied[key] = applied
			}
			a.state.Events[key] = pam.Event{Event: "credential.updated", EventID: item.EventID, CredentialKey: key, AccountID: item.AccountID, Revision: item.Revision, AccountRevision: item.AccountRevision}
		}
		a.mu.Unlock()
	}
	a.mu.Lock()
	latest := map[string]Credential{}
	for key, item := range metadata {
		value, exists := a.state.Latest[key]
		if item.Available && exists && a.state.Authorized[key] && value.AccountID == item.AccountID && value.Revision == item.Revision && value.AccountRevision == item.AccountRevision {
			latest[key] = value
		}
	}
	err = a.persist()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	storeReady := map[string]bool{}
	if store := a.Config.Store; store != nil {
		for id, path := range wantedFiles {
			value, exists := latest["account:"+id]
			if !exists {
				continue
			}
			version := credentialVersion(value)
			a.mu.Lock()
			saved := a.state.StoreVersions[id] == version && a.state.StoreFiles[id] == path
			a.mu.Unlock()
			if saved {
				if _, statErr := os.Lstat(path); statErr != nil {
					saved = false
				}
			}
			if !saved {
				a.mu.Lock()
				a.state.StoreFiles[id] = path
				err = a.persist()
				a.mu.Unlock()
				if err != nil {
					return err
				}
				if err = a.writeStore(path, value, *store); err != nil {
					problems = append(problems, err)
					continue
				}
				a.mu.Lock()
				a.state.StoreVersions[id] = version
				a.state.StoreFiles[id] = path
				err = a.persist()
				a.mu.Unlock()
				if err != nil {
					return err
				}
			}
			storeReady["account:"+id] = true
		}
	}
	matchedServices := map[string][]Rule{}
	completeServices := map[string]bool{}
	for _, declared := range a.Config.Services {
		service := effectiveService(declared, latest)
		blocked := false
		if service.AllowAccountSwitch {
			for _, account := range service.Accounts {
				if contains(response.UnavailableSwitchAccountIDs, account.AccountID) {
					blocked = true
				}
			}
		}
		payload, matched, complete, resolveErr := resolveRule(service, latest)
		if resolveErr != nil {
			problems = append(problems, resolveErr)
			for key, value := range latest {
				for _, account := range service.Accounts {
					if value.AccountID == account.AccountID || service.AllowAccountSwitch && value.AccountSwitch != nil && contains(value.AccountSwitch.AccountIDs, account.AccountID) {
						matchedServices[key] = append(matchedServices[key], service)
						break
					}
				}
			}
			continue
		}
		completeServices[service.Name] = complete
		for _, key := range matched {
			matchedServices[key] = append(matchedServices[key], service)
		}
		if blocked {
			completeServices[service.Name] = false
			problems = append(problems, errors.New("service account switch is unauthorized or ambiguous"))
			continue
		}
		if !complete {
			problems = append(problems, errors.New("service references an unavailable credential"))
			continue
		}
		if notification && service.UpdateOnNotification != nil && !*service.UpdateOnNotification {
			continue
		}
		changed := map[string]bool{}
		a.mu.Lock()
		for _, key := range matched {
			if a.state.ServiceVersions[service.Name][key] != credentialVersion(latest[key]) {
				changed[key] = true
			}
		}
		a.mu.Unlock()
		if len(changed) == 0 {
			continue
		}
		config := a.Config
		config.Rules = []Rule{service}
		if err = a.deliver(ctx, config, latest, changed); err != nil {
			completeServices[service.Name] = false
			problems = append(problems, fmt.Errorf("service %s update failed: %w", service.Name, err))
			continue
		}
		a.mu.Lock()
		a.state.ServiceVersions[service.Name] = map[string]string{}
		for _, key := range matched {
			a.state.ServiceVersions[service.Name][key] = credentialVersion(latest[key])
		}
		for _, account := range service.Accounts {
			if value, ok := payload.Credentials[account.AccountID]; ok {
				a.state.AccountUsage["service:"+service.Name+":"+account.AccountID] = usageFromCredential(value)
			}
		}
		err = a.persist()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	var deliveredEvents []pam.Event
	a.mu.Lock()
	for key, value := range latest {
		ready, verified, seen := true, true, false
		if path := wantedFiles[value.AccountID]; path != "" && key == "account:"+value.AccountID {
			seen = true
			ready = storeReady[key]
		}
		for _, service := range matchedServices[key] {
			seen = true
			if !completeServices[service.Name] || a.state.ServiceVersions[service.Name][key] != credentialVersion(value) {
				ready = false
			}
			_, required, complete, resolveErr := resolveRule(service, latest)
			if !complete || resolveErr != nil {
				ready = false
			}
			for _, requiredKey := range required {
				if a.state.ServiceVersions[service.Name][requiredKey] != credentialVersion(latest[requiredKey]) {
					ready = false
				}
			}
			if service.ApplicationCheck == nil || !service.ApplicationCheck.ConfirmOnSuccess {
				verified = false
			}
		}
		if !seen || !ready {
			delete(a.state.Delivered, key)
			delete(a.state.Applied, key)
			continue
		}
		if revision, ok := a.state.Delivered[key]; !ok || revision != value.Revision {
			deliveredEvents = append(deliveredEvents, pam.Event{Event: "credential.delivered", CredentialKey: key, Revision: value.Revision, AccountID: value.AccountID})
		}
		a.state.Delivered[key] = value.Revision
		if len(matchedServices[key]) > 0 && verified && (contains(response.Scope.ConfirmationKeys, key) || a.state.Events[key].EventID != "") {
			if applied := a.state.Applied[key]; applied.AccountID != value.AccountID || applied.Revision != value.Revision {
				a.state.Applied[key] = Applied{Key: key, Revision: value.Revision, AccountID: value.AccountID}
			}
		}
	}
	a.state.Digest = response.ConfigDigest
	a.state.Status = "success"
	a.state.SyncError = ""
	if len(problems) > 0 {
		a.state.Status = "error"
		a.state.SyncError = "agent_sync_failed"
	}
	err = a.persist()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	for _, event := range deliveredEvents {
		if err = a.recordEvent("delivered", event); err != nil {
			return err
		}
	}
	if err = a.reportPending(ctx); err != nil {
		problems = append(problems, err)
	}
	return errors.Join(problems...)
}

func withoutStoreFile(files map[string]string, removed string) map[string]string {
	result := map[string]string{}
	for id, path := range files {
		if id != removed {
			result[id] = path
		}
	}
	return result
}

func (a *Agent) writeStore(path string, value Credential, store StoreConfiguration) error {
	if err := secureTarget(path); err != nil {
		return err
	}
	if err := preparePrivateDirectory(filepath.Dir(path), 0711); err != nil {
		return err
	}
	uid, gid, err := ownerIDs(store.Owner)
	if err != nil {
		return err
	}
	var raw []byte
	if store.Format == "environment" {
		raw, err = environment(value)
	} else {
		raw, err = json.MarshalIndent(value, "", "  ")
		raw = append(raw, '\n')
	}
	if err != nil {
		return err
	}
	if !strings.HasPrefix(filepath.Base(path), value.AccountID+".") {
		return errors.New("invalid store account path")
	}
	return atomicWrite(path, raw, 0600, uid, gid)
}
