package agent

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCurrentServiceFieldsRoundTrip(t *testing.T) {
	raw := []byte(`{"version":1,"services":[{"name":"app","accounts":[{"id":"account-a"}],"update_config":{"file":"/etc/app.env","fields_map":{"DB_PASSWORD":"secret"}},"reload_service":{"unit":"app.service","operation":"restart"},"app_check":{"path":"/usr/local/bin/check-app","confirm_on_success":true}}]}`)
	config, err := decodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	if config.Services[0].Accounts[0].AccountID != "account-a" || config.Services[0].ConfigUpdate == nil || config.Services[0].ApplicationCheck == nil {
		t.Fatal("current service fields were not decoded")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	again, err := decodeConfig(encoded)
	if err != nil || !reflect.DeepEqual(config, again) {
		t.Fatalf("current config round trip failed: %v", err)
	}
}

func TestOnlyCurrentConfigurationFormatIsAccepted(t *testing.T) {
	for _, raw := range []string{
		`{"services":[]}`,
		`{"version":1,"config_version":1}`,
		`{"version":1,"local_api":{}}`,
		`{"version":1,"socket":{"socket_path":"/tmp/a.sock"}}`,
		`{"version":1,"rules":[]}`,
		`{"version":1,"delivery":{}}`,
		`{"version":1,"services":[{"accounts":[{"account_id":"a"}]}]}`,
		`{"version":1,"services":[{"config_update":{}}]}`,
	} {
		if _, err := decodeConfig([]byte(raw)); err == nil {
			t.Fatalf("unsupported configuration accepted: %s", raw)
		}
	}
}
