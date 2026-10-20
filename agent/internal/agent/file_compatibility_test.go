//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pam "github.com/jumpserver/pam-clients/go"
)

func TestV1ConfigurationFixturesKeepDefaultsAndOriginalFiles(t *testing.T) {
	paths, err := filepath.Glob("testdata/compatibility/v1/config-*.json")
	if err != nil || len(paths) != 4 {
		t.Fatal("missing frozen v1 configuration samples")
	}
	for _, sample := range paths {
		t.Run(filepath.Base(sample), func(t *testing.T) {
			fixtureConfig, _, _ := fixture(t)
			path := filepath.Join(filepath.Dir(fixtureConfig.StateFile), "agent.json")
			raw, err := os.ReadFile(sample)
			if err != nil {
				t.Fatal(err)
			}
			if err = atomicWrite(path, raw, 0600, -1, -1); err != nil {
				t.Fatal(err)
			}
			legacy, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if legacy.ConfigVersion != 1 || legacy.StateFile != DefaultState || legacy.EventFile != DefaultEvent || legacy.Delivery.Socket != DefaultSocket || legacy.InstanceID != "orders-node-1" {
				t.Fatal("loading v1 changed its identity or defaults")
			}
			interval := 300
			if filepath.Base(sample) == "config-yaml.json" {
				interval = 60
			}
			if legacy.ReconcileSeconds != interval {
				t.Fatal("v1 reconciliation default changed")
			}
			if data, err := os.ReadFile(path); err != nil || !bytes.Equal(raw, data) {
				t.Fatal("loading rewrote a user's configuration")
			}
			var versioned map[string]json.RawMessage
			if err := json.Unmarshal(raw, &versioned); err != nil {
				t.Fatal(err)
			}
			versioned["config_version"] = json.RawMessage("1")
			if err := writeJSON(path, versioned); err != nil {
				t.Fatal(err)
			}
			explicit, err := LoadConfig(path)
			if err != nil || !reflect.DeepEqual(legacy, explicit) {
				t.Fatal("adding config_version changed delivery behavior", err)
			}
		})
	}
}

func legacyPendingState(t *testing.T, service *Agent) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/compatibility/v1/state-pending.json")
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.ReplaceAll(raw, []byte("$IDENTITY"), []byte(service.state.Identity))
	return bytes.ReplaceAll(raw, []byte("$DELIVERY_HASH"), []byte(service.state.LocalDeliveryHash))
}

func TestV1StateUpgradePreservesOfflineProgressAndRevocation(t *testing.T) {
	for _, mode := range []string{"authorized", "revoked", "denied"} {
		t.Run(mode, func(t *testing.T) {
			config, remote, original := fixture(t)
			raw := legacyPendingState(t, original)
			if mode == "revoked" {
				raw = bytes.Replace(raw, []byte(`"authorized": {"db": true}`), []byte(`"authorized": {}`), 1)
			}
			if mode == "denied" {
				raw = bytes.Replace(raw, []byte(`"access_denied": false`), []byte(`"access_denied": true`), 1)
			}
			if err := atomicWrite(config.StateFile, raw, 0600, -1, -1); err != nil {
				t.Fatal(err)
			}
			// Adding the configuration version must not alter identity or the
			// delivery hash and trigger another application action.
			config.ConfigVersion = 1
			remote.fault = &pam.PAMError{Code: "NetworkError"}
			restored, err := New(config, remote)
			if err != nil {
				t.Fatal(err)
			}
			if data, err := os.ReadFile(config.StateFile); err != nil || !bytes.Equal(raw, data) {
				t.Fatal("startup rewrote the old state file")
			}
			state := restored.state
			if state.StateVersion != 1 || state.Latest["db"].Revision != 3 || state.Delivered["db"] != 3 || state.Wanted["db"] != 4 || state.Applied["db"].Confirmed || state.Applied["db"].Revision != 3 || state.Events["db"].EventID != "pending-event-1" || state.Commands["command-1"].CommandID != "command-1" || state.Digest != "retained-snapshot-digest" || len(state.Scope.ConfirmationKeys) != 1 {
				t.Fatal("upgrade lost retained credentials or pending progress")
			}
			value, err := restored.LocalCredential("db")
			if mode == "authorized" {
				if err != nil || value.Secret != "synthetic-retained-password" {
					t.Fatal("upgrade lost offline credentials")
				}
			} else if err == nil {
				t.Fatal("upgrade revived revoked credential access")
			}
			if restored.Sync(context.Background()) == nil {
				t.Fatal("offline synchronization unexpectedly succeeded")
			}
			if remote.fetched != 0 || remote.confirmed != 0 {
				t.Fatal("upgrade fetched or confirmed credentials while offline")
			}
			if err := privateFile(config.StateFile); err != nil {
				t.Fatal(err)
			}
			saved, err := os.ReadFile(config.StateFile)
			if err != nil || !bytes.Contains(saved, []byte(`"state_version": 1`)) {
				t.Fatal("normal persistence did not add the state version")
			}
			restarted, err := New(config, remote)
			if err != nil {
				t.Fatal(err)
			}
			// Event.Data is parsed response metadata; the serialized fields
			// define the state file's compatibility contract.
			before, _ := json.Marshal(restored.state)
			after, _ := json.Marshal(restarted.state)
			if !bytes.Equal(before, after) {
				t.Fatal("versioned state did not preserve offline progress on restart", err)
			}
		})
	}
}

func TestPreparationVersionsNewFilesAndKeepsExistingConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/accounts/credential-client/agent/sync/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config_digest":"compatibility","scope":{"credential_keys":[],"confirmation_keys":[]},"credentials":[],"removed_keys":[],"date_last_synced":"2026-10-13T00:00:00Z"}`))
	}))
	defer server.Close()
	config, _, _ := fixture(t)
	config.Endpoint = server.URL
	root := filepath.Dir(config.StateFile)
	bootstrap := filepath.Join(root, "bootstrap.json")
	if err := writeJSON(bootstrap, config); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(root, "installed.json")
	options := InstallOptions{Bootstrap: bootstrap, ConfigFile: installed, InstanceID: config.InstanceID}
	if _, err := Prepare(context.Background(), options); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(installed)
	if err != nil || !bytes.Contains(raw, []byte(`"config_version": 1`)) {
		t.Fatal("new service configuration did not record its format version")
	}
	local, err := PrepareLocal(context.Background(), bootstrap, filepath.Join(root, "local"), config.InstanceID)
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(local); err != nil || !bytes.Contains(raw, []byte(`"config_version": 1`)) {
		t.Fatal("new foreground configuration did not record its format version")
	}
	// An existing unversioned configuration stays byte-for-byte identical,
	// including formatting and omitted defaults, during repeated preparation.
	old, err := os.ReadFile(bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicWrite(installed, old, 0600, -1, -1); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		loaded, err := Prepare(context.Background(), options)
		if err != nil || loaded.ConfigVersion != 1 {
			t.Fatal("existing configuration could not be reused", err)
		}
		if current, err := os.ReadFile(installed); err != nil || !bytes.Equal(current, old) {
			t.Fatal("preparation rewrote existing configuration")
		}
	}
}

func TestUnsupportedFileVersionsAreRejectedWithoutOverwrite(t *testing.T) {
	config, remote, original := fixture(t)
	configPath := filepath.Join(filepath.Dir(config.StateFile), "agent.json")
	configRaw, _ := json.Marshal(config)
	var object map[string]json.RawMessage
	if err := json.Unmarshal(configRaw, &object); err != nil {
		t.Fatal(err)
	}
	object["config_version"] = json.RawMessage("2")
	configRaw, _ = json.Marshal(object)
	if err := atomicWrite(configPath, configRaw, 0600, -1, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(configPath); err == nil || !strings.Contains(err.Error(), "config_version 2") {
		t.Fatal("unsupported configuration lacked a clear error")
	}
	if actual, _ := os.ReadFile(configPath); !bytes.Equal(configRaw, actual) {
		t.Fatal("unsupported configuration was overwritten")
	}
	stateRaw := append([]byte(`{"state_version":2,`), legacyPendingState(t, original)[1:]...)
	if err := atomicWrite(config.StateFile, stateRaw, 0600, -1, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config, remote); err == nil || !strings.Contains(err.Error(), "state_version 2") {
		t.Fatal("unsupported state lacked a clear error")
	}
	if actual, _ := os.ReadFile(config.StateFile); !bytes.Equal(stateRaw, actual) {
		t.Fatal("unsupported state was overwritten")
	}
}
