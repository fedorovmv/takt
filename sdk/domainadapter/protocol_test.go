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

func TestNormalizeDeclarationSortsAndDeduplicates(t *testing.T) {
	got := NormalizeDeclaration(Declaration{Domain: " SCM ", Capabilities: []string{"change.get", " change.create ", "change.get"}, Reconcile: []string{" change.create ", "change.create"}})
	if got.APIVersion != ProtocolV1Alpha1 || got.Kind != "AdapterCapabilities" || got.Domain != DomainSCM {
		t.Fatalf("normalized=%#v", got)
	}
	if len(got.Capabilities) != 2 || got.Capabilities[0] != "change.create" || got.Capabilities[1] != "change.get" {
		t.Fatalf("capabilities=%v", got.Capabilities)
	}
	if len(got.Reconcile) != 1 || got.Reconcile[0] != "change.create" {
		t.Fatalf("reconcile=%v", got.Reconcile)
	}
}

func TestValidateOperationRejectsInvalidNames(t *testing.T) {
	for _, value := range []string{"", "Change.get", "change..get", "change/get", ".change"} {
		if err := ValidateOperation(value); err == nil {
			t.Fatalf("expected %q to fail", value)
		}
	}
	for _, value := range []string{"change.get", "item.transition", "custom-v2.operation_1"} {
		if err := ValidateOperation(value); err != nil {
			t.Fatalf("%q: %v", value, err)
		}
	}
}

func TestValidateDeclarationRejectsInvalidContracts(t *testing.T) {
	cases := []Declaration{
		{APIVersion: "wrong", Kind: "AdapterCapabilities", Domain: DomainSCM, Capabilities: []string{"change.get"}},
		{Domain: "unknown", Capabilities: []string{"change.get"}},
		{Domain: DomainSCM},
		{Domain: DomainSCM, Capabilities: []string{"Change.get"}},
		{Domain: DomainSCM, Capabilities: []string{"change.get"}, Reconcile: []string{"change.create"}},
	}
	for i, value := range cases {
		if err := ValidateDeclaration(value); err == nil {
			t.Fatalf("case %d unexpectedly valid: %#v", i, value)
		}
	}
}

func TestValidateResultContracts(t *testing.T) {
	if err := ValidateResult(Result{Status: "completed"}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []Result{
		{Status: "completed", Error: "bad"},
		{Status: "failed"},
		{Status: "other"},
	} {
		if err := ValidateResult(value); err == nil {
			t.Fatalf("expected invalid result: %#v", value)
		}
	}
	for _, value := range []ReconcileResult{{Outcome: "bad"}, {Outcome: "applied"}} {
		if err := ValidateReconcileResult(value); err == nil {
			t.Fatalf("expected invalid reconcile result: %#v", value)
		}
	}
	if err := ValidateReconcileResult(ReconcileResult{Outcome: "applied", Receipt: "r1"}); err != nil {
		t.Fatal(err)
	}
}

func TestValidateInvokeAndReconcileRequests(t *testing.T) {
	valid := InvokeRequest{RunID: "r", NodeID: "n", Attempt: 1, Workspace: "/tmp/work", Domain: DomainSCM, Operation: SCMRepositoryGet}
	if err := ValidateInvokeRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, value := range []InvokeRequest{
		{NodeID: "n", Attempt: 1, Domain: DomainSCM, Operation: SCMRepositoryGet},
		{RunID: "r", NodeID: "n", Attempt: 0, Domain: DomainSCM, Operation: SCMRepositoryGet},
		{RunID: "r", NodeID: "n", Attempt: 1, Domain: "bad", Operation: SCMRepositoryGet},
		{RunID: "r", NodeID: "n", Attempt: 1, Domain: DomainSCM, Operation: SCMChangeCreate, SideEffectMode: "reconcile"},
	} {
		if err := ValidateInvokeRequest(value); err == nil {
			t.Fatalf("expected invalid invoke: %#v", value)
		}
	}
	if err := ValidateReconcileRequest(ReconcileRequest{RunID: "r", NodeID: "n", Workspace: "/tmp/work", Domain: DomainSCM, Operation: SCMChangeCreate, IdempotencyKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReconcileRequest(ReconcileRequest{RunID: "r", NodeID: "n", Domain: DomainSCM, Operation: SCMChangeCreate}); err == nil {
		t.Fatal("missing reconcile idempotency key accepted")
	}
}
