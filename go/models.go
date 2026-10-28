// Package pam provides the JumpServer credential policy SDK.
package pam

import (
	"encoding/json"
	"fmt"
)

type Platform struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Type     string `json:"type"`
}
type Asset struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Address  string   `json:"address"`
	Platform Platform `json:"platform"`
}
type Account struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Username   string `json:"username"`
	SecretType string `json:"secret_type"`
	Secret     string `json:"secret"`
	Revision   int64  `json:"revision,omitempty"`
	Asset      Asset  `json:"-"`
	FromLocal  bool   `json:"-"`
}

type AccountSwitch struct {
	AccountIDs []string `json:"account_ids"`
}

func (a Account) String() string {
	return fmt.Sprintf("Account{ID:%q Username:%q Secret:[REDACTED]}", a.ID, a.Username)
}
func (a Account) GoString() string { return a.String() }

type Credential struct {
	Key           string         `json:"key"`
	Revision      int64          `json:"revision"`
	Asset         Asset          `json:"asset"`
	Account       Account        `json:"account"`
	AccountSwitch *AccountSwitch `json:"account_switch,omitempty"`
	FromLocal     bool           `json:"-"`
}

func (c Credential) String() string {
	return fmt.Sprintf("Credential{Key:%q Revision:%d Account:%s}", c.Key, c.Revision, c.Account)
}
func (c Credential) GoString() string { return c.String() }

type CredentialSelector struct {
	Key       string
	AccountID string
}

// AuthorizedAccount contains metadata for an application's pull scope.
// Credentials lists push policies, and may be empty for pull-only accounts.
type AuthorizedAccount struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Username    string          `json:"username"`
	SecretType  string          `json:"secret_type"`
	Asset       Asset           `json:"asset"`
	Credentials []AccountPolicy `json:"credentials"`
}

type AccountPolicy struct {
	Key           string         `json:"key"`
	Mode          string         `json:"mode"`
	Revision      int64          `json:"revision"`
	AccountSwitch *AccountSwitch `json:"account_switch,omitempty"`
}

type ApplicationInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OrgID         string `json:"org_id"`
	IsActive      bool   `json:"is_active"`
	AccountsCount int    `json:"accounts_count"`
	PoliciesCount int    `json:"policies_count"`
}
type ClientInfo struct {
	ID                  string             `json:"id"`
	InstanceID          string             `json:"instance_id"`
	Type                string             `json:"type"`
	IsActive            bool               `json:"is_active"`
	ClientVersion       string             `json:"client_version"`
	ProtocolVersion     int                `json:"protocol_version"`
	ConfigSchemaVersion *int               `json:"config_schema_version"`
	ApplicationStatus   *ApplicationStatus `json:"application_status"`
	DateStatusReported  *string            `json:"date_status_reported"`
	ServerCapabilities  []string           `json:"server_capabilities"`
}
type CredentialPolicy struct {
	ID                        string         `json:"id"`
	Key                       string         `json:"key"`
	Name                      string         `json:"name"`
	Mode                      string         `json:"mode"`
	IsActive                  bool           `json:"is_active"`
	Status                    string         `json:"status"`
	Revision                  int64          `json:"revision"`
	AccountsCount             int            `json:"accounts_count"`
	SubscriptionAllAuthorized bool           `json:"subscription_all_authorized"`
	ActiveAccountID           string         `json:"active_account_id"`
	AccountSwitch             *AccountSwitch `json:"account_switch"`
}
type PolicySelector struct{ Key, PolicyID string }

func metadataFields(data map[string]json.RawMessage, stringsFields, bools, counts []string) error {
	for _, name := range stringsFields {
		if err := required(data, name); err != nil {
			return err
		}
		var value string
		if err := json.Unmarshal(data[name], &value); err != nil {
			return err
		}
	}
	for _, name := range bools {
		if err := required(data, name); err != nil {
			return err
		}
		var value bool
		if err := json.Unmarshal(data[name], &value); err != nil {
			return err
		}
	}
	for _, name := range counts {
		if err := required(data, name); err != nil {
			return err
		}
		var value int64
		if err := json.Unmarshal(data[name], &value); err != nil || value < 0 {
			return fmt.Errorf("invalid %s", name)
		}
	}
	return nil
}

func validApplicationInfo(data map[string]json.RawMessage) error {
	return metadataFields(data, []string{"id", "name", "org_id"}, []string{"is_active"}, []string{"accounts_count", "policies_count"})
}
func validClientInfo(data map[string]json.RawMessage) error {
	if err := metadataFields(data, []string{"id", "instance_id", "type", "client_version"}, []string{"is_active"}, []string{"protocol_version"}); err != nil {
		return err
	}
	var clientType string
	json.Unmarshal(data["type"], &clientType)
	if clientType != "sdk" && clientType != "agent" {
		return fmt.Errorf("invalid client type")
	}
	if raw, ok := data["server_capabilities"]; ok && string(raw) != "null" {
		var capabilities []string
		if err := json.Unmarshal(raw, &capabilities); err != nil {
			return fmt.Errorf("invalid server capabilities")
		}
		seen := map[string]bool{}
		for _, capability := range capabilities {
			if capability == "" || seen[capability] {
				return fmt.Errorf("invalid server capabilities")
			}
			seen[capability] = true
		}
	}
	if raw, ok := data["config_schema_version"]; ok && string(raw) != "null" {
		if err := metadataFields(data, nil, nil, []string{"config_schema_version"}); err != nil {
			return err
		}
	}
	if raw, ok := data["application_status"]; ok && string(raw) != "null" {
		fields, err := object(raw)
		if err != nil {
			return err
		}
		var report ApplicationStatus
		if err := json.Unmarshal(raw, &report); err != nil {
			return err
		}
		if err := report.validate(); err != nil {
			return err
		}
		if _, ok := fields["account_ids"]; !ok || report.AccountIDs == nil {
			return fmt.Errorf("invalid account IDs")
		}
		var errorCode string
		rawCode, ok := fields["error_code"]
		if !ok || string(rawCode) == "null" {
			return fmt.Errorf("invalid status error code")
		}
		if err := json.Unmarshal(rawCode, &errorCode); err != nil {
			return err
		}
	}
	if raw, ok := data["date_status_reported"]; ok && string(raw) != "null" {
		var date string
		if err := json.Unmarshal(raw, &date); err != nil || date == "" {
			return fmt.Errorf("invalid report timestamp")
		}
	}
	return nil
}
func validPolicy(data map[string]json.RawMessage) error {
	if err := metadataFields(data, []string{"id", "key", "name", "mode", "status"}, []string{"is_active", "subscription_all_authorized"}, []string{"revision", "accounts_count"}); err != nil {
		return err
	}
	var value CredentialPolicy
	raw, _ := json.Marshal(data)
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if value.Mode != "subscription" && value.Mode != "alternating_rotation" {
		return fmt.Errorf("invalid policy mode")
	}
	if raw, ok := data["active_account_id"]; ok && string(raw) == `""` {
		return fmt.Errorf("invalid active account ID")
	}
	if value.AccountSwitch != nil {
		if value.AccountSwitch.AccountIDs == nil {
			return fmt.Errorf("invalid account switch")
		}
		for _, id := range value.AccountSwitch.AccountIDs {
			if id == "" {
				return fmt.Errorf("invalid account switch ID")
			}
		}
	}
	return nil
}

type CredentialConfirmation struct {
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
}
type KnownRevision struct {
	Key      string `json:"key"`
	Revision int64  `json:"revision"`
}
type CredentialRevision struct {
	AccountID       string         `json:"account_id"`
	AccountRevision int64          `json:"account_revision"`
	EventID         string         `json:"event_id"`
	Key             string         `json:"key"`
	Revision        int64          `json:"revision"`
	Available       bool           `json:"available"`
	Changed         bool           `json:"changed"`
	AccountSwitch   *AccountSwitch `json:"account_switch,omitempty"`
}
type DeliveryScope struct {
	Keys              []string `json:"keys"`
	AccountIDs        []string `json:"account_ids"`
	IncludeAuthorized bool     `json:"include_authorized,omitempty"`
	AllAuthorized     bool     `json:"all_authorized,omitempty"`
	SwitchAccountIDs  []string `json:"switch_account_ids,omitempty"`
}
type AgentScope struct {
	Keys             []string `json:"credential_keys"`
	ConfirmationKeys []string `json:"confirmation_keys"`
}
type AgentSync struct {
	ConfigSchemaVersion         int                  `json:"config_schema_version,omitempty"`
	ServerCapabilities          []string             `json:"server_capabilities,omitempty"`
	UnavailableSwitchAccountIDs []string             `json:"unavailable_switch_account_ids,omitempty"`
	ConfigDigest                string               `json:"config_digest"`
	Credentials                 []CredentialRevision `json:"credentials"`
	RemovedKeys                 []string             `json:"removed_keys"`
	DateLastSynced              string               `json:"date_last_synced"`
	Scope                       AgentScope           `json:"scope"`
}
type AgentSyncOptions struct {
	Credentials          []KnownRevision
	DeliveredCredentials []KnownRevision
	DeliveryScope        *DeliveryScope
	ConfigDigest         string
	SyncStatus           string
	SyncError            string
	RestartSupported     bool
}
type CommandResult struct {
	Accepted bool   `json:"accepted"`
	Status   string `json:"status"`
}

// Event exposes protocol metadata; Credentials contains initial/reconnect snapshot items.
// Data preserves all protocol fields, including future notification payloads.
type Event struct {
	AccountRevision int64                      `json:"account_revision"`
	Event           string                     `json:"event"`
	EventID         string                     `json:"event_id"`
	CommandID       string                     `json:"command_id"`
	Key             string                     `json:"key"`
	CredentialKey   string                     `json:"credential_key"`
	CredentialMode  string                     `json:"credential_mode"`
	AccountID       string                     `json:"account_id"`
	AccountSwitch   *AccountSwitch             `json:"account_switch,omitempty"`
	Revision        int64                      `json:"revision"`
	Credentials     []Event                    `json:"credentials"`
	Data            map[string]json.RawMessage `json:"-"`
}

func (e *Event) UnmarshalJSON(raw []byte) error {
	type fields Event
	var value fields
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	data, err := object(raw)
	if err != nil {
		return err
	}
	*e = Event(value)
	e.Data = data
	return nil
}

type CommandHandler func(Event) error

type PAMError struct {
	Code       string
	StatusCode int
	Detail     string
	RequestID  string
	Err        error
}

func (e *PAMError) Error() string { return fmt.Sprintf("[%s] %s", e.Code, e.Detail) }
func (e *PAMError) Unwrap() error { return e.Err }

// IsCompatibilityError separates unsupported versions from permission denial.
// Compatibility errors are explicit failures, but do not revoke retained values.
func (e *PAMError) IsCompatibilityError() bool {
	if e == nil || e.StatusCode == 401 || e.StatusCode == 403 || e.StatusCode == 404 {
		return false
	}
	return e.StatusCode == 426 || e.Code == "client_upgrade_required"
}

func required(data map[string]json.RawMessage, names ...string) error {
	for _, name := range names {
		value, ok := data[name]
		if !ok || string(value) == "null" || string(value) == `""` {
			return fmt.Errorf("missing %s", name)
		}
	}
	return nil
}
func object(data []byte) (map[string]json.RawMessage, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	if values == nil {
		return nil, fmt.Errorf("response must be an object")
	}
	return values, nil
}

func validAuthorizedAccount(data map[string]json.RawMessage) error {
	if err := required(data, "id", "name", "secret_type", "asset", "credentials"); err != nil {
		return err
	}
	var username string
	if raw, ok := data["username"]; !ok || string(raw) == "null" {
		return fmt.Errorf("missing username")
	} else if err := json.Unmarshal(raw, &username); err != nil {
		return err
	}
	asset, err := object(data["asset"])
	if err != nil {
		return err
	}
	if err := required(asset, "id", "name", "address", "platform"); err != nil {
		return err
	}
	platform, err := object(asset["platform"])
	if err != nil {
		return err
	}
	if err := required(platform, "id", "name", "category", "type"); err != nil {
		return err
	}
	var policies []map[string]json.RawMessage
	if err := json.Unmarshal(data["credentials"], &policies); err != nil {
		return err
	}
	for _, policy := range policies {
		if err := required(policy, "key", "mode", "revision"); err != nil {
			return err
		}
		if raw, ok := policy["account_switch"]; ok && string(raw) != "null" {
			switchData, err := object(raw)
			if err != nil {
				return err
			}
			if err := required(switchData, "account_ids"); err != nil {
				return err
			}
			var ids []string
			if err := json.Unmarshal(switchData["account_ids"], &ids); err != nil {
				return err
			}
			for _, id := range ids {
				if id == "" {
					return fmt.Errorf("invalid account switch ID")
				}
			}
		}
	}
	return nil
}

func validCredential(data map[string]json.RawMessage) error {
	if err := required(data, "key", "revision", "asset", "account"); err != nil {
		return err
	}
	var c Credential
	bytes, _ := json.Marshal(data)
	if err := json.Unmarshal(bytes, &c); err != nil {
		return err
	}
	if c.Key == "" || c.Revision < 0 {
		return fmt.Errorf("invalid credential revision")
	}
	for _, item := range []struct {
		data   json.RawMessage
		fields []string
	}{
		{data["asset"], []string{"id", "name", "address", "platform"}},
		{data["account"], []string{"id", "name", "username", "secret_type", "secret"}},
	} {
		nested, err := object(item.data)
		if err != nil {
			return err
		}
		if err = required(nested, item.fields...); err != nil {
			return err
		}
	}
	asset, _ := object(data["asset"])
	platform, err := object(asset["platform"])
	if err != nil {
		return err
	}
	return required(platform, "id", "name", "category", "type")
}
