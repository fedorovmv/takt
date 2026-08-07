package domainadapter

import "testing"

func TestCoreContractsAndDeclaration(t *testing.T) {
	for _, domain := range []string{DomainSCM, DomainTracker, DomainCI} {
		ops := CoreOperations(domain)
		if len(ops) == 0 {
			t.Fatalf("no core operations for %s", domain)
		}
		decl := Declaration{Domain: domain, Capabilities: ops}
		if err := ValidateDeclaration(decl); err != nil {
			t.Fatalf("%s: %v", domain, err)
		}
	}
}

func TestReconcileRequiresReceiptWhenApplied(t *testing.T) {
	if err := ValidateReconcileResult(ReconcileResult{Outcome: "applied"}); err == nil {
		t.Fatal("expected applied outcome without receipt to fail")
	}
}
