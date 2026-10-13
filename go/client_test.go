package pam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func contractClient(t *testing.T, instance, source string) *Client {
	t.Helper()
	endpoint := os.Getenv("JMS_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("Run clients/tests/run.py to start the shared protocol fixture")
	}
	client, err := NewClient(Options{Endpoint: endpoint, AppID: "contract-app", AppSecret: "contract-secret", InstanceID: instance, OrgID: "contract-org", Source: source})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}
func TestSignedCredentialOperations(t *testing.T) {
	client := contractClient(t, "go", "jms-pam")
	ctx := context.Background()
	for _, selector := range []CredentialSelector{{AccountID: "account"}, {Key: "db/空 格?=&"}} {
		credential, err := client.GetCredential(ctx, selector)
		if err != nil {
			t.Fatal(err)
		}
		if credential.Account.Secret != "DO_NOT_LOG_SECRET" || credential.Account.SecretType != "password" || credential.Asset.Platform.Type != "postgresql" {
			t.Fatal("Invalid structured response")
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", credential, credential), credential.Account.Secret) {
			t.Fatal("Secret leaked in representation")
		}
	}
	confirmation, err := client.ConfirmCredential(ctx, "db", 2, "account")
	if err != nil || confirmation.Revision != 2 {
		t.Fatalf("Confirmation failed: %v", err)
	}
}

func TestSelfApplicationAndPolicyMetadata(t *testing.T) {
	client := contractClient(t, "go-metadata", "jms-pam")
	ctx := context.Background()
	application, err := client.GetApplication(ctx)
	if err != nil || application.ID != "contract-app" || application.OrgID != "contract-org" || application.AccountsCount != 3 {
		t.Fatalf("Invalid application metadata: %v", err)
	}
	replica, err := client.GetClientInfo(ctx)
	if err != nil || replica.InstanceID != "go-metadata" || replica.Type != "sdk" || replica.ConfigSchemaVersion == nil || *replica.ConfigSchemaVersion != 0 {
		t.Fatalf("Invalid client metadata: %v", err)
	}
	policies, err := client.ListPolicies(ctx, "")
	if err != nil || len(policies) != 2 {
		t.Fatalf("Invalid policy list: %v", err)
	}
	if policies[1].IsActive || policies[1].ActiveAccountID != "" || policies[0].AccountSwitch == nil || len(policies[0].AccountSwitch.AccountIDs) != 2 {
		t.Fatal("Invalid policy metadata")
	}
	for _, selector := range []PolicySelector{{Key: policies[0].Key}, {PolicyID: policies[0].ID}} {
		policy, err := client.GetPolicy(ctx, selector)
		if err != nil || policy.ID != policies[0].ID {
			t.Fatalf("Policy lookup failed: %v", err)
		}
	}
	filtered, err := client.ListPolicies(ctx, "db/空 格?=&")
	if err != nil || len(filtered) != 1 || filtered[0].ID != policies[0].ID {
		t.Fatalf("Policy search failed: %v", err)
	}
	empty, err := client.ListPolicies(ctx, "no-match")
	if err != nil || len(empty) != 0 {
		t.Fatalf("Empty policy list failed: %v", err)
	}
	raw, err := json.Marshal([]any{application, replica, policies})
	if err != nil || strings.Contains(string(raw), "DO_NOT_LOG_SECRET") {
		t.Fatal("Secret leaked in metadata")
	}
}

func TestMetadataValidationAndMissingPolicies(t *testing.T) {
	client := contractClient(t, "go-invalid-metadata", "jms-pam")
	ctx := context.Background()
	_, appErr := client.GetApplication(ctx)
	_, clientErr := client.GetClientInfo(ctx)
	_, policyErr := client.GetPolicy(ctx, PolicySelector{Key: "invalid"})
	for _, err := range []error{appErr, clientErr, policyErr} {
		var failure *PAMError
		if !errors.As(err, &failure) || failure.Code != "ResponseError" {
			t.Fatalf("Malformed metadata accepted: %v", err)
		}
	}
	for _, search := range []string{"fault:invalid-list", "fault:duplicate", "fault:invalid-policy"} {
		if _, err := client.ListPolicies(ctx, search); err == nil {
			t.Fatal("Malformed list accepted")
		}
	}
	for _, selector := range []PolicySelector{{}, {Key: "db", PolicyID: "id"}, {Key: strings.Repeat("x", 65)}} {
		if _, err := client.GetPolicy(ctx, selector); err == nil {
			t.Fatal("Invalid selector accepted")
		}
	}
	if _, err := client.ListPolicies(ctx, strings.Repeat("x", 129)); err == nil {
		t.Fatal("Invalid search accepted")
	}
	_, err := client.GetPolicy(ctx, PolicySelector{Key: "missing"})
	var failure *PAMError
	if !errors.As(err, &failure) || failure.StatusCode != 404 {
		t.Fatalf("Missing policy did not fail: %v", err)
	}
}

func TestAccountListing(t *testing.T) {
	client := contractClient(t, "go-accounts", "jms-pam")
	ctx := context.Background()
	accounts, err := client.ListAccounts(ctx, "")
	if err != nil || len(accounts) != 205 {
		t.Fatalf("Account listing failed: %v, count %d", err, len(accounts))
	}
	if accounts[0].Asset.Platform.Type != "postgresql" || len(accounts[0].Credentials[0].AccountSwitch.AccountIDs) != 2 || accounts[1].Credentials[0].Mode != "subscription" || len(accounts[2].Credentials) != 0 || accounts[2].Username != "" {
		t.Fatal("Invalid account metadata or policy mapping")
	}
	if strings.Contains(fmt.Sprint(accounts), "DO_NOT_LOG_SECRET") {
		t.Fatal("Secret leaked in account metadata")
	}
	page, count, err := client.ListAccountsPage(ctx, 2, 1, "")
	if err != nil || count != 205 || len(page) != 2 || page[0].ID != "account-1" || page[1].ID != "account-2" {
		t.Fatalf("Invalid account page: %v", err)
	}
	for _, search := range []string{"db/空 格?=&", "no-match", "fault:empty"} {
		found, err := client.ListAccounts(ctx, search)
		expected := 0
		if search == "db/空 格?=&" {
			expected = 1
		}
		if err != nil || len(found) != expected {
			t.Fatalf("Search failed for %q: %v", search, err)
		}
	}
	page, _, err = client.ListAccountsPage(ctx, 200, 205, "")
	if err != nil || len(page) != 0 {
		t.Fatalf("Invalid empty page: %v", err)
	}
	legacy, err := client.ListAuthorizedAccounts(ctx)
	if err != nil || len(legacy) != 205 {
		t.Fatalf("Legacy method failed: %v", err)
	}
	response, err := http.Get(strings.TrimSuffix(os.Getenv("JMS_TEST_ENDPOINT"), "/prefix") + "/__stats")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var stats struct {
		Requests []struct {
			Path string
			Data map[string]any
		}
	}
	if err := json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	for _, request := range stats.Requests {
		if request.Data["instance_id"] == "go-accounts" && !strings.HasSuffix(request.Path, "/accounts/") {
			t.Fatal("Enumeration performed a secret fetch")
		}
	}
}

func TestAccountListingFailures(t *testing.T) {
	client := contractClient(t, "go-errors-accounts", "jms-pam")
	ctx := context.Background()
	for _, page := range [][2]int{{0, 0}, {501, 0}, {200, -1}} {
		if _, _, err := client.ListAccountsPage(ctx, page[0], page[1], ""); err == nil {
			t.Fatal("Invalid page accepted")
		}
	}
	if _, err := client.ListAccounts(ctx, strings.Repeat("x", 129)); err == nil {
		t.Fatal("Oversized search accepted")
	}
	for _, fault := range []string{"invalid-count", "null-count", "missing-policy-revision", "missing-username", "null-policies", "invalid-account", "invalid-policy", "invalid-list", "oversized", "duplicate-page", "incomplete", "duplicate", "changing-count", "forbidden"} {
		accounts, err := client.ListAccounts(ctx, "fault:"+fault)
		var failure *PAMError
		code := "ResponseError"
		if fault == "forbidden" {
			code = "credential_not_authorized"
		}
		if !errors.As(err, &failure) || failure.Code != code || accounts != nil {
			t.Fatalf("Expected %s for %s, received %v", code, fault, err)
		}
	}
}
func TestValidationAndErrors(t *testing.T) {
	client := contractClient(t, "go-errors", "jms-pam")
	ctx := context.Background()
	for _, selector := range []CredentialSelector{{}, {Key: "db", AccountID: "account"}} {
		if _, err := client.GetCredential(ctx, selector); err == nil {
			t.Fatal("Invalid selector accepted")
		}
	}
	for _, revision := range []int64{0, -1} {
		if _, err := client.ConfirmCredential(ctx, "db", revision, "account"); err == nil {
			t.Fatal("Invalid revision accepted")
		}
	}
	for _, item := range []struct{ key, code string }{{"forbidden", "credential_not_authorized"}, {"upgrade", "client_upgrade_required"}, {"invalid-json", "ResponseError"}, {"invalid-revision", "ResponseError"}, {"redirect", "HTTPError"}} {
		_, err := client.GetCredential(ctx, CredentialSelector{Key: item.key})
		var failure *PAMError
		if !errors.As(err, &failure) || failure.Code != item.code {
			t.Fatalf("Expected %s, received %v", item.code, err)
		}
	}
}
func TestSynchronization(t *testing.T) {
	client := contractClient(t, "go-agent", "jms-pam-agent")
	options := AgentSyncOptions{Credentials: []KnownRevision{{Key: "db", Revision: 2}}}
	sync, err := client.SyncAgent(context.Background(), options)
	if err != nil || sync.Credentials[0].Changed || len(sync.Scope.Keys) != 1 || sync.Scope.Keys[0] != "db" {
		t.Fatalf("Sync failed: %v", err)
	}
	options.SyncError = "invalid-flag"
	_, err = client.SyncAgent(context.Background(), options)
	var failure *PAMError
	if !errors.As(err, &failure) || failure.Code != "ResponseError" {
		t.Fatal("Malformed boolean accepted")
	}
}
func TestCommandLifecycle(t *testing.T) {
	client := contractClient(t, "go-command", "jms-pam")
	ctx := context.Background()
	commands, err := client.ListApplicationCommands(ctx)
	if err != nil {
		t.Fatal(err)
	}
	event := commands[0]
	executed := 0
	result, err := client.ExecuteApplicationCommand(ctx, event, func(Event) error { executed++; return nil })
	if err != nil || result.Status != "success" || executed != 1 {
		t.Fatal("Command did not complete")
	}
	event.CommandID = "duplicate"
	result, err = client.ExecuteApplicationCommand(ctx, event, func(Event) error { executed++; return nil })
	if err != nil || result.Accepted || executed != 1 {
		t.Fatal("Duplicate command executed")
	}
	sentinel := errors.New("business failure")
	event.CommandID = "report-failure"
	_, err = client.ExecuteApplicationCommand(ctx, event, func(Event) error { return sentinel })
	if err != sentinel {
		t.Fatal("Handler exception was replaced")
	}
}
func TestEventsAndCancellation(t *testing.T) {
	client := contractClient(t, "go-events", "jms-pam")
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	events := []Event{}
	sentinel := errors.New("finished")
	err := client.WatchCredentialEvents(ctx, func(event Event) error {
		events = append(events, event)
		if len(events) == 4 {
			return sentinel
		}
		return nil
	})
	if err != sentinel || len(events) != 4 || events[0].Event != "snapshot" || events[1].Event != "credential.updated" || events[2].Event != "future.notification" || events[3].Credentials[0].Revision != 3 {
		t.Fatalf("Stream failed: %v", err)
	}
	var future map[string]string
	if err := json.Unmarshal(events[2].Data["future_payload"], &future); err != nil || future["notice"] != "preserved" {
		t.Fatal("Future notification fields were lost")
	}
	response, err := http.Get(strings.TrimSuffix(os.Getenv("JMS_TEST_ENDPOINT"), "/prefix") + "/__stats")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var stats struct {
		Receipts []struct {
			Instance string `json:"instance"`
			EventID  string `json:"eventId"`
		}
	}
	if err = json.NewDecoder(response.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	received := map[string]bool{}
	for _, receipt := range stats.Receipts {
		if receipt.Instance == "go-events" {
			received[receipt.EventID] = true
		}
	}
	if !received["updated-1"] || !received["future-1"] {
		t.Fatal("Business receipts missing")
	}
	stopped, stop := context.WithCancel(context.Background())
	stop()
	if err = client.WatchCredentialEvents(stopped, func(Event) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal("Cancellation was ignored")
	}
	clone, err := client.Clone()
	if err != nil {
		t.Fatal(err)
	}
	defer clone.Close()
	client.Close()
	if _, err = clone.GetCredential(context.Background(), CredentialSelector{Key: "db"}); err != nil {
		t.Fatal("Clone was closed with original")
	}
}

func TestAccountAndEventResult(t *testing.T) {
	client := contractClient(t, "go-account-event", "jms-pam")
	account, err := client.GetAccountFresh(context.Background(), "account")
	if err != nil || account.Username != "app" || account.Asset.Address != "127.0.0.1" || account.Revision != 2 || account.FromLocal {
		t.Fatalf("invalid account: %v", err)
	}
	result, err := client.ConfirmEvent(context.Background(), "event-one", "success", "")
	if err != nil || !result.Accepted {
		t.Fatalf("event result: %v", err)
	}
	result, err = client.ConfirmEvent(context.Background(), "event-two", "failed", "application_failed")
	if err != nil || result.Status != "failed" {
		t.Fatalf("failed event result: %v", err)
	}
}
