package pam

import (
	"context"
	"errors"
	"sort"
	"time"
)

type accountEventResult struct{ status, errorCode string }

func accountEventSelector(event Event) (CredentialSelector, bool) {
	if event.CommandID != "" {
		switch event.Event {
		case "credential.updated", "credential.switch.requested", "application.restart.requested":
			return CredentialSelector{Key: "command:" + event.CommandID}, true
		default:
			return CredentialSelector{}, false
		}
	}
	if event.CredentialMode == "alternating_rotation" {
		key := event.CredentialKey
		if key == "" {
			key = event.Key
		}
		return CredentialSelector{Key: "policy:" + key}, key != ""
	}
	return CredentialSelector{AccountID: event.AccountID}, event.CredentialMode == "subscription" && event.AccountID != ""
}

func (w *EventWatcher) rememberApplication(id string, result accountEventResult) {
	if _, exists := w.appliedEvents[id]; !exists {
		w.appliedOrder = append(w.appliedOrder, id)
	}
	w.appliedEvents[id] = result
	if len(w.appliedOrder) > 256 {
		delete(w.appliedEvents, w.appliedOrder[0])
		w.appliedOrder = w.appliedOrder[1:]
	}
}

func (w *EventWatcher) reportAccountUsage() {
	used := make(map[string]bool)
	for _, id := range w.usedAccounts {
		used[id] = true
	}
	ids := make([]string, 0, len(used))
	for id := range used {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
	defer cancel()
	_ = w.client.ReportAccounts(ctx, ids)
}

func (w *EventWatcher) applyAccountEvent(update Event) error {
	if update.Event == "application.restart.requested" {
		if w.handlers.OnApplicationRestart == nil {
			return errors.New("application restart is not configured")
		}
		return invokeEventHook(func() error { return w.handlers.OnApplicationRestart(w.ctx) })
	}
	if update.CredentialMode != "subscription" && update.CredentialMode != "alternating_rotation" {
		return errors.New("unsupported credential mode")
	}
	account, err := w.client.GetAccountFresh(w.ctx, update.AccountID)
	if err != nil {
		return err
	}
	if account.ID != update.AccountID || update.AccountRevision < 0 || account.Revision != update.AccountRevision {
		return errors.New("event account version is superseded")
	}
	if err = w.ctx.Err(); err != nil {
		return err
	}
	if err = invokeEventHook(func() error { return w.handlers.OnAccountChanged(w.ctx, account) }); err != nil {
		return err
	}
	usage := update
	usage.CommandID = ""
	if selector, valid := accountEventSelector(usage); valid {
		w.usedAccounts[selector] = account.ID
	}
	return nil
}

func (w *EventWatcher) refreshAccount(update, event Event, delay time.Duration) {
	selector, valid := accountEventSelector(update)
	if !valid || update.EventID == "" || w.ctx.Err() != nil {
		return
	}
	usage := update
	usage.CommandID = ""
	usageSelector, _ := accountEventSelector(usage)
	if update.Event != "application.restart.requested" && w.accountScope != nil && !w.accountScope[usageSelector] {
		return
	}
	if previous, exists := w.pending[selector]; exists && previous.update.Revision > update.Revision {
		return
	}
	result, applied := w.appliedEvents[update.EventID]
	var err error
	if !applied {
		if update.CommandID != "" {
			var claim CommandResult
			claim, err = w.client.ReportApplicationCommandResult(w.ctx, update.CommandID, "running", "")
			if err == nil && !claim.Accepted {
				delete(w.pending, selector)
				return
			}
		}
		if err == nil {
			err = w.applyAccountEvent(update)
			if err != nil {
				if update.CommandID != "" {
					w.rememberApplication(update.EventID, accountEventResult{"failed", "application_failed"})
				} else {
					_, _ = w.client.ConfirmEvent(w.ctx, update.EventID, "failed", "application_failed")
				}
			} else {
				result = accountEventResult{"success", ""}
				w.rememberApplication(update.EventID, result)
			}
		}
	}
	if err == nil && w.ctx.Err() == nil {
		_, err = w.client.ConfirmEvent(w.ctx, update.EventID, result.status, result.errorCode)
	}
	if w.ctx.Err() != nil {
		return
	}
	if err != nil {
		w.report(err, &event)
		delay = min(max(time.Second, delay*2), 30*time.Second)
		w.pending[selector] = pendingCredential{update, event, delay, time.Now().Add(delay)}
	} else {
		delete(w.pending, selector)
		w.reportAccountUsage()
	}
}

func (w *EventWatcher) revokeAccount(selector CredentialSelector, id string, event Event, delay time.Duration) {
	if w.ctx.Err() != nil {
		return
	}
	err := errors.New("revoked account cleanup is not configured")
	if w.handlers.OnAccountRevoked != nil {
		err = invokeEventHook(func() error { return w.handlers.OnAccountRevoked(w.ctx, id) })
	}
	if err != nil {
		w.report(err, &event)
		if w.pendingRevocations == nil {
			w.pendingRevocations = make(map[CredentialSelector]pendingRevocation)
		}
		delay = min(max(time.Second, delay*2), 30*time.Second)
		w.pendingRevocations[selector] = pendingRevocation{id, event, delay, time.Now().Add(delay)}
		return
	}
	delete(w.pendingRevocations, selector)
	delete(w.usedAccounts, selector)
	clear(w.appliedEvents)
	w.appliedOrder = nil
	w.reportAccountUsage()
}

func (w *EventWatcher) revokeAccounts(retained map[CredentialSelector]bool, event Event) {
	for selector := range w.pendingRevocations {
		if retained[selector] {
			delete(w.pendingRevocations, selector)
		}
	}
	for selector, id := range w.usedAccounts {
		if retained[selector] {
			continue
		}
		if _, pending := w.pendingRevocations[selector]; pending {
			continue
		}
		w.revokeAccount(selector, id, event, 0)
	}
	w.reportAccountUsage()
}
