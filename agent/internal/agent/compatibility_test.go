package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestLocalFormatVersionValidation(t *testing.T) {
	for _, format := range []struct{ kind, field string }{
		{"configuration", "config_version"}, {"state", "state_version"},
	} {
		for _, test := range []struct {
			name, value, wantError string
		}{
			{"omitted", "", ""}, {"v1", "1", ""},
			{"future", "2", "unsupported"},
			{"zero", "0", "positive integer"}, {"negative", "-1", "positive integer"},
			{"null", "null", "positive integer"}, {"bool", "true", "positive integer"},
			{"fraction", "1.5", "positive integer"}, {"decimal", "1.0", "positive integer"},
			{"string", `"DO_NOT_LOG_SECRET"`, "positive integer"},
			{"object", `{}`, "positive integer"}, {"array", `[]`, "positive integer"},
			{"overflow", "99999999999999999999999999", "positive integer"},
		} {
			t.Run(format.kind+"/"+test.name, func(t *testing.T) {
				raw := `{}`
				if test.value != "" {
					raw = `{"` + format.field + `":` + test.value + `}`
				}
				version, err := readFormatVersion([]byte(raw), format.kind, format.field, 1)
				if test.wantError == "" {
					if err != nil || version != 1 {
						t.Fatalf("v1 format was not accepted: %v", err)
					}
					return
				}
				if err == nil || !strings.Contains(err.Error(), test.wantError) || !strings.Contains(err.Error(), format.field) {
					t.Fatalf("missing actionable version error: %v", err)
				}
				if strings.Contains(err.Error(), "DO_NOT_LOG_SECRET") {
					t.Fatal("version error exposed a configuration value")
				}
			})
		}
		for _, raw := range []string{"null", "[]", `"DO_NOT_LOG_SECRET"`, "{} {}", "{} trailing", "{"} {
			if _, err := readFormatVersion([]byte(raw), format.kind, format.field, 1); err == nil || strings.Contains(err.Error(), "DO_NOT_LOG_SECRET") {
				t.Fatalf("invalid JSON was accepted or leaked a value: %v", err)
			}
		}
		if _, err := readFormatVersion([]byte(`{"`+strings.ToUpper(format.field)+`":2}`), format.kind, format.field, 1); err == nil {
			t.Fatal("case-insensitive JSON decoding bypassed format version validation")
		}
	}
}

func TestConfigCompatibilityDiagnostics(t *testing.T) {
	for _, test := range []struct{ raw, location string }{
		{`{"rules":[{"config_update":{"fiels_map":{}}}]}`, "fiels_map"},
		{`{"rules":[{"config_update":{"file":123}}]}`, "rules.config_update.file"},
		{`{"app_secret":"DO_NOT_LOG_SECRET" "rules":[]}`, "byte"},
	} {
		_, err := decodeConfig([]byte(test.raw))
		if err == nil || !strings.Contains(err.Error(), test.location) || strings.Contains(err.Error(), "DO_NOT_LOG_SECRET") {
			t.Fatalf("missing field diagnostic or secret redaction: %v", err)
		}
	}
	config, err := decodeConfig([]byte(`{"instance_id":"stable-instance"}`))
	if err != nil || config.ConfigVersion != CurrentConfigVersion {
		t.Fatalf("missing config_version was not normalized in memory: %v", err)
	}
	// Programmatic legacy configurations are still valid as version zero, but
	// an explicit JSON zero is covered by the version validation above.
	if config.compactDefaults().ConfigVersion != CurrentConfigVersion {
		t.Fatal("new configuration did not retain its version")
	}
	config.ConfigVersion = 0
	if config.compactDefaults().ConfigVersion != CurrentConfigVersion {
		t.Fatal("newly written legacy configuration has no format version")
	}
	config.ConfigVersion = 2
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "config_version") {
		t.Fatal("programmatic unsupported configuration version was accepted")
	}
}

func TestRejectedStateVersionDoesNotChangeRuntimeState(t *testing.T) {
	state := State{StateVersion: 1, Identity: "stable-identity", Denied: true, Delivered: map[string]int64{"db": 3}}
	before, _ := json.Marshal(state)
	if err := decodeState([]byte(`{"state_version":2,"access_denied":false}`), &state); err == nil {
		t.Fatal("future state format was accepted")
	}
	after, _ := json.Marshal(state)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("rejected format changed authorization or delivery state")
	}
}
