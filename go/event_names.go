package pam

func canonicalEventName(value string) string {
	switch value {
	case "rotation.verification.skipped":
		return "rotation.verify.skipped"
	case "rotation.verification.started":
		return "rotation.verify.started"
	case "rotation.verification.failed":
		return "rotation.verify.failed"
	case "rotation.verification.cancelled":
		return "rotation.verify.cancelled"
	case "application.scope.updated":
		return "app.scope.updated"
	case "application.restart.requested":
		return "app.restart.requested"
	case "rotation.waiting_for_application":
		return "rotation.waiting_for_app"
	case "configuration.updated":
		return "app.scope.updated"
	default:
		return value
	}
}
