package pam

const (
	CredentialSecretUpdated    = "credential.secret.updated"
	CredentialSwitchPublished  = "credential.switch.published"
	CredentialRefreshRequested = "credential.refresh.requested"
)

func IsCredentialApplicationEvent(value string) bool {
	switch value {
	case CredentialSecretUpdated, CredentialSwitchPublished, CredentialRefreshRequested:
		return true
	default:
		return false
	}
}
