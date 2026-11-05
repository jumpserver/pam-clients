package agent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

func (c Config) pullTimesPath() string {
	if c.Store == nil {
		return ""
	}
	return filepath.Join(filepath.Dir(c.Store.Root), "last_pull_at.json")
}

// Call with mu held. This is a diagnostic view of the private state, keyed by
// the actual stored account ID; it never contains credentials or policy data.
func (a *Agent) writePullTimesLocked() error {
	path := a.Config.pullTimesPath()
	if path == "" {
		return nil
	}
	times := map[string]*time.Time{}
	if !a.state.Denied {
		for key, value := range a.state.Latest {
			if key == "account:"+value.AccountID && a.state.Authorized[key] &&
				(contains(a.Config.Store.Accounts, "all") || contains(a.Config.Store.Accounts, value.AccountID)) {
				times[value.AccountID] = value.LastPullAt
			}
		}
	}
	raw, err := json.MarshalIndent(times, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err = secureTarget(path); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil && bytes.Equal(existing, raw) {
		return nil
	}
	return atomicWrite(path, raw, 0600, -1, -1)
}
