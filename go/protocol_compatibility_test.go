package pam

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"
)

func TestProtocolMismatchIsExplicitAndDoesNotRevokeCachedCredentials(t *testing.T) {
	instance := "go-compat-cycle"
	client := contractClient(t, instance, "jms-pam")
	ctx := context.Background()
	initial, err := client.GetAccount(ctx, "account")
	if err != nil {
		t.Fatal(err)
	}
	controlBackend(t, instance, url.Values{"fault": {"426"}})
	for _, fetch := range []func() (Account, error){
		func() (Account, error) { return client.GetAccount(ctx, "account") },
		func() (Account, error) { return client.GetAccountFresh(ctx, "account") },
	} {
		_, err := fetch()
		var failure *PAMError
		if !errors.As(err, &failure) || !failure.IsCompatibilityError() {
			t.Fatal("protocol mismatch silently fell back or lost its classification")
		}
	}
	controlBackend(t, instance, url.Values{"fault": {"503"}})
	retained, err := client.GetAccount(ctx, "account")
	if err != nil || !retained.FromLocal || retained.Secret != initial.Secret {
		t.Fatal("protocol mismatch discarded the cached credential")
	}
	controlBackend(t, instance, url.Values{"fault": {"401"}})
	if _, err := client.GetAccount(ctx, "account"); err == nil {
		t.Fatal("identity denial was ignored")
	}
	controlBackend(t, instance, url.Values{"fault": {"503"}})
	if _, err := client.GetAccount(ctx, "account"); err == nil {
		t.Fatal("later outage revived denied credentials")
	}
}

func TestOptionalMetadataAbsenceAndCapabilityExtensions(t *testing.T) {
	ctx := context.Background()
	legacy := contractClient(t, "go-compat-legacy", "jms-pam")
	info, err := legacy.GetClientInfo(ctx)
	if err != nil || info.ServerCapabilities != nil {
		t.Fatal("legacy server response did not leave capabilities unknown")
	}
	future := contractClient(t, "go-compat-capabilities", "jms-pam")
	info, err = future.GetClientInfo(ctx)
	if err != nil || len(info.ServerCapabilities) != 3 || info.ServerCapabilities[2] != "future_optional_capability" {
		t.Fatal("unknown optional capability broke metadata parsing")
	}
	bad := contractClient(t, "go-compat-bad-capabilities", "jms-pam")
	if _, err := bad.GetClientInfo(ctx); err == nil {
		t.Fatal("malformed capability metadata was accepted")
	}
	instance := "go-compat-missing"
	missing := contractClient(t, instance, "jms-pam")
	if _, err := missing.GetAccount(ctx, "account"); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.GetClientInfo(ctx); err == nil {
		t.Fatal("missing metadata endpoint was hidden")
	}
	controlBackend(t, instance, url.Values{"fault": {"503"}})
	if account, err := missing.GetAccount(ctx, "account"); err != nil || !account.FromLocal {
		t.Fatal("missing optional endpoint discarded cached credentials")
	}
}

func TestMalformedCompatibilityResponsesPreserveClassification(t *testing.T) {
	for _, fault := range []string{"426-invalid", "426-timeout"} {
		client := contractClient(t, "go-compat-"+fault, "jms-pam")
		client.http.Timeout = 100 * time.Millisecond
		selector := CredentialSelector{Key: "cache-" + fault}
		if _, err := client.GetCredential(context.Background(), selector); err != nil {
			t.Fatal(err)
		}
		_, err := client.GetCredential(context.Background(), selector)
		var failure *PAMError
		if !errors.As(err, &failure) || !failure.IsCompatibilityError() || len(client.latestCredentials) != 1 {
			t.Fatal("malformed 426 response fell back or discarded cache")
		}
	}
}
