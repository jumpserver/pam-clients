package agent

import (
	"context"
	"errors"
	"log"
	"sort"
	"time"

	pam "github.com/jumpserver/pam-clients/go"
)

// AccountUsage retains only metadata. The map key identifies the administrator's
// selector, so a configured A can retain current B independently of fetched secrets.
type AccountUsage struct {
	AccountID     string             `json:"account_id"`
	Key           string             `json:"credential_key,omitempty"`
	Revision      int64              `json:"revision,omitempty"`
	AccountSwitch *pam.AccountSwitch `json:"account_switch,omitempty"`
}

func (a *Agent) restoreAccountUsage() error {
	if a.Config.modern() {
		return a.restoreServiceUsage()
	}
	seen := make(map[string]bool)
	if len(a.state.ReportedAccounts) > 200 {
		return errors.New("invalid reported account usage")
	}
	for _, id := range a.state.ReportedAccounts {
		if !validKey(id) || seen[id] {
			return errors.New("invalid reported account usage")
		}
		seen[id] = true
	}
	retained := a.state.AccountUsage
	a.state.AccountUsage = make(map[string]AccountUsage)
	for _, rule := range a.Config.Rules {
		for _, account := range rule.Accounts {
			selector := "account:" + account.AccountID
			usage := AccountUsage{AccountID: account.AccountID}
			if previous, ok := retained[selector]; ok && account.AllowAccountSwitch {
				if !validKey(previous.AccountID) || previous.Revision < 0 || validateAccountSwitch(previous.AccountSwitch, previous.AccountID) != nil {
					return errors.New("invalid retained account usage")
				}
				if previous.AccountID == account.AccountID || (previous.AccountSwitch != nil && contains(previous.AccountSwitch.AccountIDs, account.AccountID)) {
					usage = previous
				}
			}
			a.state.AccountUsage[selector] = usage
		}
		for _, key := range rule.Keys {
			if previous, ok := retained["key:"+key]; ok && previous.Key == key {
				if !validKey(previous.AccountID) || previous.Revision < 0 {
					return errors.New("invalid retained account usage")
				}
				a.state.AccountUsage["key:"+key] = previous
			}
		}
	}
	// Older state files have no usage map. Recover only successfully delivered
	// credentials; a newly fetched rotation target must not replace current use.
	delivered := make(map[string]Credential)
	for key, value := range a.state.Latest {
		if revision, ok := a.state.Delivered[key]; ok && revision == value.Revision && a.state.Authorized[key] {
			delivered[key] = value
		}
	}
	a.recordAccountUsage(delivered)
	return nil
}

// Called with a.mu held after the complete local delivery workflow succeeds.
func (a *Agent) recordAccountUsage(delivered map[string]Credential) {
	for _, rule := range a.Config.Rules {
		payload, _, complete, err := resolveRule(rule, delivered)
		if err != nil || !complete {
			continue
		}
		for _, account := range rule.Accounts {
			if value, ok := payload.Credentials[account.AccountID]; ok && value.AccountID != "" {
				a.state.AccountUsage["account:"+account.AccountID] = usageFromCredential(value)
			}
		}
		for _, key := range rule.Keys {
			if value, ok := payload.Credentials[key]; ok && value.AccountID != "" {
				a.state.AccountUsage["key:"+key] = usageFromCredential(value)
			}
		}
	}
}

func usageFromCredential(value Credential) AccountUsage {
	result := AccountUsage{AccountID: value.AccountID, Key: value.Key, Revision: value.Revision}
	if value.AccountSwitch != nil {
		result.AccountSwitch = &pam.AccountSwitch{AccountIDs: append([]string(nil), value.AccountSwitch.AccountIDs...)}
	}
	return result
}

// AccountUsageSnapshot reports declarations and the last successfully delivered
// rotation account. It never treats an arbitrary password fetch as account use.
func (a *Agent) AccountUsageSnapshot() pam.ApplicationStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	report := pam.ApplicationStatus{Status: "starting", AccountIDs: []string{}}
	if a.state.Status == "success" {
		report.Status = "running"
	}
	if a.state.Status == "error" {
		report.Status, report.ErrorCode = "degraded", "agent_sync_failed"
		if a.state.SyncError != "" {
			report.ErrorCode = a.state.SyncError
		}
	}
	used := make(map[string]bool)
	for _, id := range a.state.ReportedAccounts {
		used[id] = true
	}
	for _, usage := range a.state.AccountUsage {
		if usage.AccountID != "" {
			used[usage.AccountID] = true
		}
	}
	for id := range used {
		report.AccountIDs = append(report.AccountIDs, id)
	}
	sort.Strings(report.AccountIDs)
	return report
}

// ReportAccounts replaces the application's explicit usage declaration. Rules
// remain administrator-owned and their account use is reported independently.
// This records no secrets and never confirms an event or expands delivery scope.
func (a *Agent) ReportAccounts(ctx context.Context, accountIDs []string) error {
	if len(accountIDs) > 200 {
		return errors.New("too many reported accounts")
	}
	seen := make(map[string]bool)
	for _, id := range accountIDs {
		if !validKey(id) || seen[id] {
			return errors.New("invalid reported accounts")
		}
		seen[id] = true
	}
	accounts, err := a.remote.ListAuthorizedAccounts(ctx)
	if err != nil {
		return errors.New("cannot verify account authorization")
	}
	authorized := make(map[string]bool)
	for _, account := range accounts {
		authorized[account.ID] = true
	}
	for _, id := range accountIDs {
		if !authorized[id] {
			return errors.New("reported account is not authorized")
		}
	}
	a.mu.Lock()
	if a.state.Denied {
		a.mu.Unlock()
		return errors.New("agent access denied")
	}
	previous := a.state.ReportedAccounts
	a.state.ReportedAccounts = append([]string(nil), accountIDs...)
	sort.Strings(a.state.ReportedAccounts)
	err = a.persist()
	if err != nil {
		a.state.ReportedAccounts = previous
	}
	a.mu.Unlock()
	if err == nil {
		a.reportAccountUsage(ctx)
	}
	return err
}

func (a *Agent) reportAccountUsage(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.remote.ReportStatus(bounded, a.AccountUsageSnapshot()); err != nil {
		// Old Core versions and telemetry failures do not deny local access or
		// retry delivery actions. The next synchronization retries reporting.
		log.Printf("PAM Agent account usage report failed: %T", err)
	}
}
