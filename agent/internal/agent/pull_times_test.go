//go:build !windows

package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	pam "github.com/jumpserver/pam-clients/go"
)

func TestPullTimeFileIsSeparateStableAndDropsRevokedAccounts(t *testing.T) {
	config, remote := servicesFixture(t)
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = a.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(config.Store.Root), "last_pull_at.json")
	if a.Config.pullTimesPath() != path {
		t.Fatal("pull times must be beside the credentials directory")
	}
	times := readPullTimes(t, a)
	if len(times) != 3 {
		t.Fatal("diagnostic file does not describe every stored account")
	}
	for _, value := range a.state.Latest {
		if times[value.AccountID] == nil || !times[value.AccountID].Equal(*value.LastPullAt) {
			t.Fatal("diagnostic file differs from the retained pull time")
		}
	}
	before, err := os.Stat(path)
	if err != nil || before.Mode().Perm() != 0600 {
		t.Fatal("diagnostic file is not private", err)
	}
	files, err := os.ReadDir(config.Store.Root)
	if err != nil || len(files) != 3 {
		t.Fatal("diagnostic file was added to the credential directory", err)
	}
	fetched := remote.fetched
	if err = a.Sync(ctx); err != nil || remote.fetched != fetched {
		t.Fatal("metadata reconciliation fetched unchanged credentials", err)
	}
	after, err := os.Stat(path)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("metadata reconciliation rewrote unchanged diagnostic data", err)
	}
	// Live queries publish their new pull time immediately, independently of
	// the next store synchronization, without adding operational file fields.
	live, err := a.AccountCredential(ctx, testA)
	if err != nil {
		t.Fatal(err)
	}
	if pulled := readPullTimes(t, a)[testA]; pulled == nil || !pulled.Equal(*live.LastPullAt) {
		t.Fatal("live secret query did not update the diagnostic file")
	}
	remote.fault = &pam.PAMError{Code: "NetworkError"}
	if a.HandleEvent(ctx, pam.Event{Event: "app.scope.updated", AuthorizedAccountIDs: []string{testB, testC}}) == nil {
		t.Fatal("expected backend outage during revocation reconciliation")
	}
	if times = readPullTimes(t, a); len(times) != 2 || times[testA] != nil {
		t.Fatal("revoked account remained in pull diagnostics")
	}
	remote.fault = &pam.PAMError{Code: "client_disabled", StatusCode: 403}
	if a.Sync(ctx) == nil {
		t.Fatal("disabled client was accepted")
	}
	if len(readPullTimes(t, a)) != 0 {
		t.Fatal("disabled client retained diagnostic account entries")
	}
}

func TestPullTimeFileCannotOverwritePrivateOrBusinessFiles(t *testing.T) {
	config, _ := servicesFixture(t)
	path := config.pullTimesPath()
	for _, change := range []func(*Config){
		func(c *Config) { c.StateFile = path },
		func(c *Config) { c.EventFile = path },
		func(c *Config) { c.Socket = &SocketConfiguration{Socket: path, Owner: config.Socket.Owner} },
		func(c *Config) { c.Store = &StoreConfiguration{Root: path, Accounts: []string{"all"}} },
		func(c *Config) {
			c.Services = []Rule{{Name: "app", Accounts: []AccountSelector{{AccountID: testA}}, ConfigUpdate: &ConfigUpdate{Files: []File{{Path: path, Format: "json"}}}}}
		},
	} {
		altered := config
		change(&altered)
		if altered.Validate() == nil {
			t.Fatal("pull diagnostic path collision was accepted")
		}
	}
}

func TestPullTimeFileRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	config, remote := servicesFixture(t)
	target := filepath.Join(filepath.Dir(config.StateFile), "unrelated.json")
	original := []byte("leave this file unchanged\n")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, config.pullTimesPath()); err != nil {
		t.Fatal(err)
	}
	a, err := New(config, remote)
	if err != nil {
		t.Fatal(err)
	}
	if a.Sync(context.Background()) == nil {
		t.Fatal("diagnostic file followed a symbolic link")
	}
	raw, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(raw, original) {
		t.Fatal("diagnostic export modified an unrelated target", err)
	}
}
