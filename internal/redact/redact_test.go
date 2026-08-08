package redact

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"takt/internal/store"
)

func TestSecretRefAndRedaction(t *testing.T) {
	t.Setenv("TAKT_TEST_SECRET", "super-secret-value")
	r := &Redactor{}
	value, err := r.Resolve("secret://TAKT_TEST_SECRET")
	if err != nil || value != "super-secret-value" {
		t.Fatalf("value=%q err=%v", value, err)
	}
	got := r.String("prefix super-secret-value suffix")
	if strings.Contains(got, "super-secret-value") || !strings.Contains(got, "<redacted>") {
		t.Fatalf("got=%q", got)
	}
	_ = os.Getenv("TAKT_TEST_SECRET")
}

func TestMissingSecretFailsClosed(t *testing.T) {
	r := &Redactor{}
	if _, err := r.Resolve("secret://TAKT_MISSING_SECRET_FOR_TEST"); err == nil {
		t.Fatal("missing secret accepted")
	}
}

func TestExplicitSecretRefRedactsShortValue(t *testing.T) {
	t.Setenv("TAKT_SHORT_SECRET", "abc")
	r := NewFromEnvironment()
	resolved, err := r.Resolve("secret://TAKT_SHORT_SECRET")
	if err != nil || resolved != "abc" {
		t.Fatalf("resolve=%q err=%v", resolved, err)
	}
	if got := r.String("value=abc"); got != "value=<redacted>" {
		t.Fatalf("explicit short secret was not redacted: %q", got)
	}
}

func TestRunStateRedactionCoversDomainReceiptAndApproval(t *testing.T) {
	secret := "receipt-secret-046"
	t.Setenv("TAKT_RECEIPT_SECRET", secret)
	r := NewFromEnvironment()
	r.RegisterReferences("secret://TAKT_RECEIPT_SECRET")
	state := &store.RunState{Approvals: map[string]string{"approve": secret}, Nodes: map[string]*store.NodeState{
		"domain": {DomainOperation: &store.DomainOperationState{Receipt: "receipt=" + secret}},
	}}
	RedactRunState(r, state)
	raw, _ := json.Marshal(state)
	if strings.Contains(string(raw), secret) || state.Approvals["approve"] != "<redacted>" || state.Nodes["domain"].DomainOperation.Receipt != "receipt=<redacted>" {
		t.Fatalf("secret remained in state: %s", raw)
	}
}
