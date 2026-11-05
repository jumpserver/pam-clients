//go:build !windows

package agent

import (
	"context"
	"testing"

	pam "github.com/jumpserver/pam-clients/go"
)

type notificationRemote struct {
	*fakeRemote
	notificationRevision int64
	otherRevision        int64
}

func (r *notificationRemote) SyncAgent(ctx context.Context, options pam.AgentSyncOptions) (pam.AgentSync, error) {
	response, err := r.fakeRemote.SyncAgent(ctx, options)
	response.Scope.Keys = append(response.Scope.Keys, "account:a", "account:cache")
	response.Credentials = append(response.Credentials,
		pam.CredentialRevision{Key: "account:a", AccountID: "a", Revision: r.notificationRevision, AccountRevision: r.notificationRevision, Available: true, Changed: true},
		pam.CredentialRevision{Key: "account:cache", AccountID: "cache", Revision: r.otherRevision, AccountRevision: r.otherRevision, Available: true, Changed: true},
	)
	return response, err
}

func (r *notificationRemote) GetCredentialFresh(ctx context.Context, selector pam.CredentialSelector) (pam.Credential, error) {
	if selector.AccountID == "cache" {
		return pam.Credential{Key: "account:cache", Revision: r.otherRevision, Account: pam.Account{ID: "cache", Revision: r.otherRevision, Secret: "cache-password"}}, nil
	}
	return r.fakeRemote.GetCredentialFresh(ctx, selector)
}

// A password notification for the source must not fetch or republish it.

func TestExplicitAccountDestinationIsPreservedAlongsideRotation(t *testing.T) {
	values := map[string]Credential{
		"rotation":  {Key: "rotation", AccountID: "a", AccountSwitch: &pam.AccountSwitch{AccountIDs: []string{"a", "b"}}},
		"account:a": {Key: "account:a", AccountID: "a"},
	}
	rules := []Rule{{Accounts: []AccountSelector{{AccountID: "a", AllowAccountSwitch: true}}}, {Accounts: []AccountSelector{{AccountID: "a"}}}}
	if removed := redundantAccountNotifications(rules, values); len(removed) != 0 {
		t.Fatalf("explicit account destination was removed: %v", removed)
	}
	_, matched, complete, err := resolveRule(rules[1], values)
	if err != nil || !complete || len(matched) != 1 || matched[0] != "account:a" {
		t.Fatalf("ordinary account rule is ambiguous: %v, %v", matched, err)
	}
}
