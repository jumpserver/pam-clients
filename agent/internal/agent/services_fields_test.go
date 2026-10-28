package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestServiceFieldNamesRoundTrip(t *testing.T) {
	current := `{"config_version":1,"services":[{"name":"app","accounts":[{"id":"account-a"}],"update_config":{"file":"/etc/app.env","fields_map":{"DB_PASSWORD":"secret"}},"reload_service":{"unit":"app.service","operation":"restart"},"app_check":{"path":"/usr/local/bin/check-app","confirm_on_success":true}}]}`
	// The internal representation keeps existing persisted fingerprints stable,
	// while the configuration file accepts and exports only the new field names.
	oldConfig := Config{ConfigVersion: 1, Services: []Rule{{
		Name: "app", Accounts: []AccountSelector{{AccountID: "account-a"}},
		ConfigUpdate:     &ConfigUpdate{File: "/etc/app.env", FieldsMap: map[string]string{"DB_PASSWORD": "secret"}},
		ServiceAction:    &Action{Unit: "app.service", Operation: "restart"},
		ApplicationCheck: &ApplicationCheck{Action: Action{Path: "/usr/local/bin/check-app"}, ConfirmOnSuccess: true},
	}}}
	newConfig, err := decodeConfig([]byte(current))
	if err != nil || !reflect.DeepEqual(oldConfig, newConfig) {
		t.Fatalf("new field names changed the configured actions: %v", err)
	}
	for _, config := range []Config{oldConfig, newConfig} {
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		for _, names := range serviceFields {
			if strings.Contains(string(raw), `"`+names[0]+`"`) || !strings.Contains(string(raw), `"`+names[1]+`"`) {
				t.Fatalf("export did not use the canonical service field %s", names[1])
			}
		}
		loaded, err := decodeConfig(raw)
		if err != nil || !reflect.DeepEqual(config, loaded) {
			t.Fatalf("canonical export changed configured actions: %v", err)
		}
	}
	// Changing the spelling must not reset persisted versions and replay service
	// restarts or confirmations on an otherwise unchanged configuration.
	state := State{}
	prepareServiceVersions(oldConfig, &state)
	state.ServiceVersions = map[string]map[string]string{"app": {"account-a": "1"}}
	before, _ := json.Marshal(state)
	prepareServiceVersions(newConfig, &state)
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Fatal("renaming service fields invalidated persisted versions")
	}
}

func TestEmptyServicesRemainV1AfterExport(t *testing.T) {
	config := Config{ConfigVersion: 1, Services: []Rule{}}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeConfig(raw)
	if err != nil || loaded.ConfigVersion != 1 || !loaded.modern() {
		t.Fatalf("empty services lost the v1 service configuration: %v", err)
	}
	if _, err = decodeConfig([]byte(`{"config_version":2,"services":[]}`)); err == nil {
		t.Fatal("an undeclared configuration version was accepted")
	}
}

func TestServiceAccountsUseOnlyID(t *testing.T) {
	for _, account := range []string{
		`{"account_id":"account-a"}`, `{"account_id":null}`,
		`{"id":"account-a","account_id":"account-a"}`, `{"ACCOUNT_ID":"account-a"}`,
	} {
		if _, err := decodeConfig([]byte(`{"services":[{"accounts":[` + account + `]}]}`)); err == nil {
			t.Fatal("a service account accepted account_id")
		}
	}
	_, err := decodeConfig([]byte(`{"services":[{"accounts":[{"id":123}]}]}`))
	if err == nil || !strings.Contains(err.Error(), "services.accounts.id") {
		t.Fatalf("missing id field diagnostic: %v", err)
	}
}

func TestServiceFieldsRejectOldNamesAndInvalidActions(t *testing.T) {
	for _, names := range serviceFields {
		for _, oldValue := range []string{`{}`, `null`} {
			for _, extra := range []string{"", `,"` + names[1] + `":{}`} {
				raw := `{"services":[{"` + names[0] + `":` + oldValue + extra + `}]}`
				_, err := decodeConfig([]byte(raw))
				if err == nil || !strings.Contains(err.Error(), names[0]) || !strings.Contains(err.Error(), names[1]) {
					t.Fatalf("old service field was accepted or missing replacement hint: %v", err)
				}
			}
		}
		for _, field := range []string{names[0], names[1], strings.ToUpper(names[0]), strings.ToUpper(names[1])} {
			_, err := decodeConfig([]byte(`{"services":[{"` + field + `":{"unexpected_field":"DO_NOT_LOG_SECRET"}}]}`))
			if err == nil || strings.Contains(err.Error(), "DO_NOT_LOG_SECRET") {
				t.Fatalf("invalid action accepted or leaked a value: %v", err)
			}
		}
	}
	for _, test := range []struct{ raw, location string }{
		{`{"services":[{"update_config":{"file":123}}]}`, "services.update_config.file"},
		{`{"services":[{"reload_service":{"operation":123}}]}`, "services.reload_service.operation"},
		{`{"services":[{"app_check":{"confirm_on_success":123}}]}`, "services.app_check.confirm_on_success"},
		{`{"services":[{"update_confg":{}}]}`, "update_confg"},
		{`{"Services":[{"app_check":{},"application_check":{}}]}`, "services"},
	} {
		_, err := decodeConfig([]byte(test.raw))
		if err == nil || !strings.Contains(err.Error(), test.location) {
			t.Fatalf("missing service field diagnostic for %s: %v", test.location, err)
		}
	}
}

func TestLegacyRuleExportRetainsOriginalFields(t *testing.T) {
	raw := []byte(`{"rules":[{"config_update":{"file":"/etc/app.env"},"service_action":{"operation":"reload"},"application_check":{"confirm_on_success":true}}]}`)
	config, err := decodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range serviceFields {
		if !strings.Contains(string(encoded), `"`+names[0]+`"`) || strings.Contains(string(encoded), `"`+names[1]+`"`) {
			t.Fatal("service field changes affected the legacy rules format")
		}
	}
}
