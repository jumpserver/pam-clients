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
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGeneratedInstanceIDFormat(t *testing.T) {
	for _, hostname := range []string{"app-pod-123", strings.Repeat("a", 200), strings.Repeat("主", 60)} {
		id := instanceIDForHost(hostname, [2]byte{0x1a, 0x2b})
		if len(id) > 128 || !utf8.ValidString(id) || !strings.HasSuffix(id, "-1a2b") {
			t.Fatal("generated instance ID has invalid length or suffix")
		}
	}
	if got := instanceIDForHost("app-pod-123", [2]byte{0x1a, 0x2b}); got != "app-pod-123-1a2b" {
		t.Fatal("instance ID did not include the hostname")
	}
}

func TestAutomaticInstanceIDSurvivesRestartWithoutRewritingInput(t *testing.T) {
	config, remote, _ := fixture(t)
	config.InstanceID = ""
	path := filepath.Join(filepath.Dir(config.StateFile), "agent.json")
	if err := writeJSON(path, config); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	hostname, _ := os.Hostname()
	if !strings.HasPrefix(loaded.InstanceID, strings.TrimSpace(hostname)+"-") || !regexp.MustCompile(`-[0-9a-f]{4}$`).MatchString(loaded.InstanceID) {
		t.Fatal("missing instance ID did not generate hostname plus four characters")
	}
	service, err := New(loaded, remote)
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(config.StateFile)
	if err != nil || service.state.InstanceID != loaded.InstanceID {
		t.Fatal("generated instance ID was not persisted before connecting", err)
	}
	reloaded, err := LoadConfig(path)
	if err != nil || reloaded.InstanceID != loaded.InstanceID {
		t.Fatal("restart changed the generated instance ID", err)
	}
	if _, err := New(reloaded, remote); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("automatic identity rewrote the input configuration")
	}
	reloaded.AppID = "another-app"
	if _, err := New(reloaded, remote); err == nil {
		t.Fatal("automatic identity allowed another application to reuse state")
	}
	if after, _ := os.ReadFile(config.StateFile); !bytes.Equal(stateBefore, after) {
		t.Fatal("restart or rejected identity rewrote retained state")
	}
}

func TestAutomaticInstanceIDPreservesUnidentifiedLegacyState(t *testing.T) {
	config, _, service := fixture(t)
	service.state.InstanceID = ""
	if err := service.persist(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config.StateFile)
	config.InstanceID = ""
	if err := config.ensureInstanceID(); err == nil {
		t.Fatal("old state without a recoverable ID was silently replaced")
	}
	if after, _ := os.ReadFile(config.StateFile); !bytes.Equal(before, after) {
		t.Fatal("failed identity recovery modified the old state")
	}
	config.InstanceID = "instance"
	if err := config.ensureInstanceID(); err != nil {
		t.Fatal("explicit ID was not preserved", err)
	}
}

func TestInstallationAndLocalPreparationAllowOmittedInstanceID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			InstanceID string `json:"instance_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.InstanceID == "" {
			t.Error("access verification did not use the generated ID")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"config_digest":"identity","scope":{"credential_keys":[],"confirmation_keys":[]},"credentials":[],"removed_keys":[],"date_last_synced":"2026-10-29T00:00:00Z"}`))
	}))
	defer server.Close()
	config, _, _ := fixture(t)
	config.Endpoint, config.InstanceID = server.URL, ""
	root := filepath.Dir(config.StateFile)
	bootstrap := filepath.Join(root, "bootstrap.json")
	if err := writeJSON(bootstrap, config); err != nil {
		t.Fatal(err)
	}
	options := InstallOptions{Bootstrap: bootstrap, ConfigFile: filepath.Join(root, "installed.json")}
	installed, err := Prepare(context.Background(), options)
	if err != nil || installed.InstanceID == "" {
		t.Fatal("installation required an explicit ID", err)
	}
	again, err := Prepare(context.Background(), options)
	if err != nil || again.InstanceID != installed.InstanceID {
		t.Fatal("repeated installation changed the instance ID", err)
	}
	local, err := PrepareLocal(context.Background(), bootstrap, filepath.Join(root, "local"), "")
	if err != nil {
		t.Fatal("local preparation required an explicit ID", err)
	}
	loaded, err := LoadConfig(local)
	if err != nil || loaded.InstanceID == "" {
		t.Fatal("local preparation did not save the generated ID", err)
	}
	options.ConfigFile = bootstrap
	if _, err := Prepare(context.Background(), options); err != nil {
		t.Fatal("an existing unconfigured installation could not generate its ID", err)
	}
	loaded, err = LoadConfig(bootstrap)
	if err != nil || loaded.InstanceID == "" {
		t.Fatal("existing installation did not save its generated ID", err)
	}
}
