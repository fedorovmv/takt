package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

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
	wf := &spec.Workflow{Name: "adapter", Nodes: []spec.Node{{ID: "item", Adapter: &spec.AdapterCallSpec{Name: "tracker", Operation: "item.get", Input: `{"id":"ABC-123"}`}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.adapters = fakeDomainResolver{adapter}
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
	wf := &spec.Workflow{Name: "adapter", Nodes: []spec.Node{{ID: "create", Adapter: &spec.AdapterCallSpec{Name: "scm", Operation: "change.create", Input: `{}`}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.adapters = fakeDomainResolver{adapter}
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
	wf := &spec.Workflow{Name: "adapter", Nodes: []spec.Node{{ID: "create", Adapter: &spec.AdapterCallSpec{Name: "scm", Operation: "change.create", Input: `{"title":"x"}`}, SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.adapters = fakeDomainResolver{adapter}
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
	wf := &spec.Workflow{Name: "adapter", Nodes: []spec.Node{{ID: "ci", Adapter: &spec.AdapterCallSpec{Name: "ci", Operation: "run.start", Input: `{}`}, SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}, Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"internal"}}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", t.TempDir())
	r.adapters = fakeDomainResolver{adapter}
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

func TestDomainAdapterNodeHonorsPauseAtNodeBoundary(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	adapter := &fakeDomainAdapter{declaration: domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "tracker", Capabilities: []string{"item.get", "item.comment"}}}
	adapter.invoke = domainadapter.Result{Status: "completed", Output: json.RawMessage(`{"ok":true}`)}
	resolver := &blockingDomainResolver{adapter: adapter, started: started, release: release}
	wf := &spec.Workflow{Name: "adapter-pause", Nodes: []spec.Node{
		{ID: "first", Adapter: &spec.AdapterCallSpec{Name: "tracker", Operation: "item.get", Input: `{}`}},
		{ID: "second", DependsOn: []string{"first"}, Adapter: &spec.AdapterCallSpec{Name: "tracker", Operation: "item.comment", Input: `{}`}},
	}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", dir)
	r.adapters = resolver
	runID := "adapter-pause"
	type result struct {
		state *store.RunState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, err := r.StartWithOptions(context.Background(), "", StartOptions{RunID: runID})
		done <- result{state, err}
	}()
	<-started
	if err := (store.FS{Workspace: dir}).RequestPause(runID); err != nil {
		t.Fatal(err)
	}
	close(release)
	out := <-done
	if !errors.Is(out.err, ErrPaused) || out.state.Status != store.RunPaused {
		t.Fatalf("expected paused run: state=%+v err=%v", out.state, out.err)
	}
	if adapter.invokes != 1 || out.state.Nodes["second"].Status != store.NodePending {
		t.Fatalf("pause allowed next adapter node: invokes=%d second=%+v", adapter.invokes, out.state.Nodes["second"])
	}
}

func TestDomainAdapterNodeHonorsCancellationWhileInvoking(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{}, 1)
	adapter := &cancelDomainAdapter{started: started}
	wf := &spec.Workflow{Name: "adapter-cancel", Nodes: []spec.Node{{ID: "call", Adapter: &spec.AdapterCallSpec{Name: "tracker", Operation: "item.get", Input: `{}`}}}}
	r := New(wf, &spec.Config{APIVersion: "takt/v1alpha1", Kind: "Config"}, "<test>", "<test>", dir)
	r.adapters = singleDomainResolver{adapter: adapter}
	runID := "adapter-cancel"
	type result struct {
		state *store.RunState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, err := r.StartWithOptions(context.Background(), "", StartOptions{RunID: runID})
		done <- result{state, err}
	}()
	<-started
	if err := (store.FS{Workspace: dir}).RequestCancel(runID); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.state == nil || out.state.Status != store.RunCancelled || !errors.Is(out.err, context.Canceled) {
			t.Fatalf("expected cancelled adapter run: state=%+v err=%v", out.state, out.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("adapter invocation did not observe cancellation")
	}
}

type blockingDomainResolver struct {
	adapter *fakeDomainAdapter
	started chan struct{}
	release chan struct{}
}

func (r *blockingDomainResolver) Resolve(string) (domainadapter.Adapter, error) {
	return &blockingDomainAdapter{base: r.adapter, started: r.started, release: r.release}, nil
}

type blockingDomainAdapter struct {
	base    *fakeDomainAdapter
	started chan struct{}
	release chan struct{}
}

func (a *blockingDomainAdapter) Describe(ctx context.Context) (domainadapter.Declaration, error) {
	return a.base.Describe(ctx)
}
func (a *blockingDomainAdapter) Invoke(ctx context.Context, req domainadapter.InvokeRequest) (domainadapter.Result, error) {
	a.base.invokes++
	if a.base.invokes == 1 {
		a.started <- struct{}{}
		select {
		case <-a.release:
		case <-ctx.Done():
			return domainadapter.Result{}, ctx.Err()
		}
	}
	return a.base.invoke, nil
}
func (a *blockingDomainAdapter) Reconcile(ctx context.Context, req domainadapter.ReconcileRequest) (domainadapter.ReconcileResult, error) {
	return a.base.Reconcile(ctx, req)
}

type singleDomainResolver struct{ adapter domainadapter.Adapter }

func (r singleDomainResolver) Resolve(string) (domainadapter.Adapter, error) { return r.adapter, nil }

type cancelDomainAdapter struct{ started chan struct{} }

func (a *cancelDomainAdapter) Describe(context.Context) (domainadapter.Declaration, error) {
	return domainadapter.Declaration{APIVersion: domainadapter.ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: "tracker", Capabilities: []string{"item.get"}}, nil
}
func (a *cancelDomainAdapter) Invoke(ctx context.Context, _ domainadapter.InvokeRequest) (domainadapter.Result, error) {
	a.started <- struct{}{}
	<-ctx.Done()
	return domainadapter.Result{}, ctx.Err()
}
func (a *cancelDomainAdapter) Reconcile(context.Context, domainadapter.ReconcileRequest) (domainadapter.ReconcileResult, error) {
	return domainadapter.ReconcileResult{}, errors.New("unexpected reconcile")
}
