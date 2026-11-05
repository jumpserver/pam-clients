package agent

import (
	"strconv"
	"time"
)

// Account is the business-facing credential. Policy and event metadata belong
// to notifications, independently of the account's credential version.
type Account struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Username   string       `json:"username"`
	SecretType string       `json:"secret_type"`
	Secret     string       `json:"secret"`
	Version    int64        `json:"version"`
	Asset      AccountAsset `json:"asset"`
}

type AccountAsset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

type CredentialResult struct {
	Account    Account    `json:"account"`
	Source     string     `json:"source"`
	LastPullAt *time.Time `json:"last_pull_at"`
}

// Credential files contain only business account data. Operational metadata
// is retained in state and exported separately beside the credential store.
type credentialFile struct {
	Account Account `json:"account"`
}

func (a Account) String() string {
	return "Account{ID:" + strconv.Quote(a.ID) + " Secret:[REDACTED]}"
}
func (a Account) GoString() string          { return a.String() }
func (c CredentialResult) String() string   { return c.Account.String() }
func (c CredentialResult) GoString() string { return c.String() }
func (c credentialFile) String() string     { return c.Account.String() }
func (c credentialFile) GoString() string   { return c.String() }

func (c Credential) result(source string) CredentialResult {
	return CredentialResult{
		Account: Account{
			ID: c.AccountID, Name: c.Account, Username: c.Username,
			SecretType: c.SecretType, Secret: c.Secret, Version: c.AccountRevision,
			Asset: AccountAsset{ID: c.AssetID, Name: c.Asset, Address: c.Address},
		},
		Source: source, LastPullAt: c.LastPullAt,
	}
}

func (c Credential) fileResult() credentialFile {
	result := c.result("")
	return credentialFile{Account: result.Account}
}

// Call with mu held after accepting a fresh Core value. One account may be
// retained under both an account notification and one or more policy keys.
func (a *Agent) recordAccountPullLocked(pulled Credential) {
	for key, retained := range a.state.Latest {
		if retained.AccountID == pulled.AccountID && retained.AccountRevision == pulled.AccountRevision &&
			retained.SecretType == pulled.SecretType && retained.Secret == pulled.Secret {
			retained.LastPullAt = pulled.LastPullAt
			a.state.Latest[key] = retained
		}
	}
}

// Scripts receive notification context alongside the same account structure
// used by ordinary queries and JSON files.
type credentialNotification struct {
	CredentialResult
	EventID string `json:"event_id,omitempty"`
}

type notificationPayload struct {
	Event       string                            `json:"event"`
	Credentials map[string]credentialNotification `json:"credentials"`
}

func (p Payload) notification() notificationPayload {
	credentials := make(map[string]credentialNotification, len(p.Credentials))
	for selector, value := range p.Credentials {
		credentials[selector] = credentialNotification{value.result("local"), value.EventID}
	}
	return notificationPayload{p.Event, credentials}
}
