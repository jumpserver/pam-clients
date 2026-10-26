//go:build !windows

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestRotationAndAllAccountNotificationsCoexist(t *testing.T) {
	config, remote, _ := fixture(t)
	root := filepath.Dir(config.StateFile)
	rotatedFile, ordinaryFile := filepath.Join(root, "db.json"), filepath.Join(root, "cache.json")
	config.Rules = []Rule{
		{Accounts: []AccountSelector{{AccountID: "a", AllowAccountSwitch: true}}, ConfigUpdate: &ConfigUpdate{Files: []File{{Path: rotatedFile}}}},
		{Accounts: []AccountSelector{{AccountID: "cache"}}, ConfigUpdate: &ConfigUpdate{Files: []File{{Path: ordinaryFile}}}},
	}
	remote.eventID = "rotation-event"
	remote.value.AccountSwitch = &pam.AccountSwitch{AccountIDs: []string{"a", "b"}}
	r := &notificationRemote{fakeRemote: remote, notificationRevision: 1, otherRevision: 1}
	service, err := New(config, r)
	if err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"a", "b", "a"} {
		remote.value.Account.ID, remote.value.Account.Revision = id, int64(index+2)
		remote.value.Revision, remote.metadata = int64(index+2), int64(index+2)
		if err := service.Sync(context.Background()); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(rotatedFile)
		if err != nil || !strings.Contains(string(data), `"account_id": "`+id+`"`) {
			t.Fatalf("rotation did not publish %s: %s (%v)", id, data, err)
		}
		if service.state.Authorized["account:a"] || service.state.Delivered["account:a"] != 0 {
			t.Fatal("redundant notification was fetched or counted as a separate delivery")
		}
		// A password notification for the source must not fetch or republish it.
		fetched := remote.fetched
		r.notificationRevision++
		r.otherRevision++
		if err := service.HandleEvent(context.Background(), pam.Event{Event: "credential.updated", CredentialKey: "account:a", AccountID: "a", Revision: r.notificationRevision}); err != nil {
			t.Fatal(err)
		}
		if remote.fetched != fetched {
			t.Fatal("notification fetched an unused rotation account")
		}
		if service.state.Delivered["account:cache"] != r.otherRevision {
			t.Fatal("independent account notification did not update its file")
		}
	}
}

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
