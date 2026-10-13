package pam

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
)

// ApplicationStatus is a complete usage snapshot, independent of rotation confirmation.
// AccountIDs may be empty when no accounts are in use. ErrorCode must never contain secrets.
type ApplicationStatus struct {
	Status     string   `json:"status"`
	AccountIDs []string `json:"account_ids"`
	ErrorCode  string   `json:"error_code"`
}

var statusErrorCode = regexp.MustCompile(`^[A-Za-z0-9_.:-]*$`)

func (report ApplicationStatus) validate() error {
	switch report.Status {
	case "starting", "running", "degraded", "stopping", "stopped", "error":
	default:
		return fmt.Errorf("invalid application status")
	}
	if len(report.AccountIDs) > 200 || len(report.ErrorCode) > 128 || !statusErrorCode.MatchString(report.ErrorCode) {
		return fmt.Errorf("invalid status report fields")
	}
	seen := make(map[string]bool)
	for _, id := range report.AccountIDs {
		if id == "" || seen[id] {
			return fmt.Errorf("account IDs must be unique and non-empty")
		}
		seen[id] = true
	}
	return nil
}

// ReportStatus uses the configured HTTP timeout and returns errors to the caller.
// A rejected report does not invalidate retained credentials or confirm a rotation.
func (c *Client) ReportStatus(ctx context.Context, report ApplicationStatus) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if err := report.validate(); err != nil {
		return err
	}
	accounts := report.AccountIDs
	if accounts == nil {
		accounts = []string{}
	}
	var result struct {
		Accepted bool `json:"accepted"`
	}
	err := c.requestInternal(ctx, http.MethodPost, "/status/", map[string]any{
		"status": report.Status, "account_ids": accounts, "error_code": report.ErrorCode,
	}, &result, func(data map[string]json.RawMessage) error {
		return metadataFields(data, nil, []string{"accepted"}, nil)
	}, false)
	if err != nil {
		return err
	}
	if !result.Accepted {
		return fmt.Errorf("status report was not accepted")
	}
	return nil
}

// TryReportStatus is a best-effort convenience for optional application telemetry.
func (c *Client) TryReportStatus(ctx context.Context, report ApplicationStatus) bool {
	return c.ReportStatus(ctx, report) == nil
}
