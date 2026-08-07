package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"takt/internal/domainadapter"
	"takt/internal/spec"
	"takt/internal/store"
)

type fakeDomainAdapter struct {
	declaration  domainadapter.Declaration
	invoke       domainadapter.Result
	invokeErr    error
	reconcile    domainadapter.ReconcileResult
	reconcileErr error
	invokes      int
	reconciles   int
}

func (f *fakeDomainAdapter) Describe(context.Context) (domainadapter.Declaration, error) {
	return f.declaration, nil
}
func (f *fakeDomainAdapter) Invoke(context.Context, domainadapter.InvokeRequest) (domainadapter.Result, error) {
	f.invokes++
	return f.invoke, f.invokeErr
}
func (f *fakeDomainAdapter) Reconcile(context.Context, domainadapter.ReconcileRequest) (domainadapter.ReconcileResult, error) {
	f.reconciles++
	return f.reconcile, f.reconcileErr
}

type fakeDomainResolver struct{ adapter domainadapter.Adapter }

func (f fakeDomainResolver) Resolve(string) (domainadapter.Adapter, error) {
	if f.adapter == nil {
		return nil, errors.New("missing")
	}
	return f.adapter, nil
}

func TestDomainAdapterNodeRunsNeutralOperation(t *testing.T) {
	adapter := &fakeDomainAdapter{declaration: domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "tracker", Capabilities: []string{"item.get"}}, invoke: domainadapter.Result{Status: "completed", Output: json.RawMessage(`{"id":"ABC-123","title":"bug"}`)}}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "adapter"}, Nodes: []spec.Node{{ID: "item", Adapter: &spec.AdapterCallSpec{Name: "tracker", Operation: "item.get", Input: `{"id":"ABC-123"}`}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.Adapters = fakeDomainResolver{adapter}
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Nodes["item"].Output != `{"id":"ABC-123","title":"bug"}` {
		t.Fatalf("state=%#v", state.Nodes["item"])
	}
	op := state.Nodes["item"].DomainOperation
	if op == nil || op.Domain != "tracker" || op.Operation != "item.get" {
		t.Fatalf("operation=%#v", op)
	}
}

func TestDomainAdapterPreflightRejectsMissingCapabilityBeforeInvoke(t *testing.T) {
	adapter := &fakeDomainAdapter{declaration: domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "scm", Capabilities: []string{"change.get"}}}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "adapter"}, Nodes: []spec.Node{{ID: "create", Adapter: &spec.AdapterCallSpec{Name: "scm", Operation: "change.create", Input: `{}`}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.Adapters = fakeDomainResolver{adapter}
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected preflight failure")
	}
	if adapter.invokes != 0 {
		t.Fatalf("invoke count=%d", adapter.invokes)
	}
	if state.Nodes["create"].ErrorCode != "protocol" {
		t.Fatalf("node=%#v", state.Nodes["create"])
	}
}

func TestDomainAdapterUnknownSideEffectReconcilesToApplied(t *testing.T) {
	adapter := &fakeDomainAdapter{declaration: domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "scm", Capabilities: []string{"change.create"}, Reconcile: []string{"change.create"}}, invoke: domainadapter.Result{Status: "unknown", Receipt: "r1"}, reconcile: domainadapter.ReconcileResult{Outcome: "applied", Receipt: "r1", Output: json.RawMessage(`{"change":"42"}`)}}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "adapter"}, Nodes: []spec.Node{{ID: "create", Adapter: &spec.AdapterCallSpec{Name: "scm", Operation: "change.create", Input: `{"title":"x"}`}, SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.Adapters = fakeDomainResolver{adapter}
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	op := state.Nodes["create"].DomainOperation
	if adapter.invokes != 1 || adapter.reconciles != 1 || op == nil || op.ReconcileStatus != "applied" || op.IdempotencyKey == "" || op.Receipt != "r1" {
		t.Fatalf("invokes=%d reconciles=%d op=%#v", adapter.invokes, adapter.reconciles, op)
	}
}

func TestDomainAdapterUnknownReconcileBlocksRetry(t *testing.T) {
	adapter := &fakeDomainAdapter{declaration: domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "ci", Capabilities: []string{"run.start"}, Reconcile: []string{"run.start"}}, invoke: domainadapter.Result{Status: "unknown"}, reconcile: domainadapter.ReconcileResult{Outcome: "unknown"}}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "adapter"}, Nodes: []spec.Node{{ID: "ci", Adapter: &spec.AdapterCallSpec{Name: "ci", Operation: "run.start", Input: `{}`}, SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}, Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"internal"}}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.Adapters = fakeDomainResolver{adapter}
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected unknown-state failure")
	}
	if adapter.invokes != 1 || adapter.reconciles != 1 {
		t.Fatalf("blind retry occurred: invokes=%d reconciles=%d", adapter.invokes, adapter.reconciles)
	}
	if state.Nodes["ci"].ErrorCode != "external_state_unknown" {
		t.Fatalf("node=%#v", state.Nodes["ci"])
	}
}
