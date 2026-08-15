package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
	assistantpi "takt/internal/extensions/assistants/pi"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

type adapterFunc func(context.Context, assistant.Request) (assistant.Result, error)

func (f adapterFunc) Run(ctx context.Context, req assistant.Request) (assistant.Result, error) {
	return f(ctx, req)
}

func (f adapterFunc) Capabilities() []string {
	return []string{assistant.CapabilityToolPolicy, assistant.CapabilitySkills, assistant.CapabilityMCP, assistant.CapabilitySandboxFilesystem, assistant.CapabilitySandboxNetwork}
}

type resolverFunc func(string) (assistant.Adapter, error)

func (f resolverFunc) Resolve(name string) (assistant.Adapter, error) {
	return f(name)
}

type providerRetryCaptureStore struct {
	store.Repository
	states map[string]*store.RunState
	events []store.Event
}

func (s *providerRetryCaptureStore) Commit(state *store.RunState, event store.Event) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	var snapshot store.RunState
	if err := json.Unmarshal(encoded, &snapshot); err != nil {
		return err
	}
	if s.states == nil {
		s.states = map[string]*store.RunState{}
	}
	s.states[event.Type] = &snapshot
	s.events = append(s.events, event)
	return s.Repository.Commit(state, event)
}

func TestProviderRetryResumesSameSessionWithoutWorkflowRetry(t *testing.T) {
	dir := t.TempDir()
	var requests []assistant.Request
	adapter := adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
		requests = append(requests, req)
		result := assistant.Result{Output: "ok", Stdout: "ok", ExitCode: 0, SessionID: "provider-session", Usage: &assistant.ProtocolUsage{InputTokens: 1, OutputTokens: 2}}
		if len(requests) < 3 {
			return result, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("temporarily unavailable")}
		}
		return result, nil
	})
	wf := &spec.Workflow{Name: "provider-retry", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Attempts: spec.AttemptsSpec{Max: 1}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["work"]
	if node.Attempts != 1 || node.ProviderAttempts != 3 || len(node.Executions) != 3 {
		t.Fatalf("provider retry accounting = %+v", node)
	}
	for index, executionState := range node.Executions {
		if executionState.Attempt != 1 || executionState.ProviderAttempt != index+1 {
			t.Fatalf("execution %d = %+v", index, executionState)
		}
	}
	if node.Usage == nil || node.Usage.InputTokens != 3 || node.Usage.OutputTokens != 6 {
		t.Fatalf("usage = %+v", node.Usage)
	}
	if len(requests) != 3 || requests[0].SessionMode != "fresh" || requests[1].SessionMode != "resume" || requests[1].SessionID != "provider-session" || requests[2].SessionMode != "resume" || requests[2].SessionID != "provider-session" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestProviderRetrySessionMismatchIsProtocolAndNotRetried(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	adapter := adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
		calls++
		if calls == 1 {
			return assistant.Result{ExitCode: 1, SessionID: "expected-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
		}
		if req.SessionMode != "resume" || req.SessionID != "expected-session" {
			t.Fatalf("retry request = %+v", req)
		}
		return assistant.Result{ExitCode: 1, SessionID: "wrong-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
	})
	wf := &spec.Workflow{Name: "provider-mismatch", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected protocol failure")
	}
	if calls != 2 || state.Nodes["work"].ErrorCode != string(execution.KindProtocol) || state.Nodes["work"].ProviderAttempts != 2 {
		t.Fatalf("mismatch state = %+v calls=%d", state.Nodes["work"], calls)
	}
}

func TestExternalProviderFailureGetsProviderOrdinal(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "external-provider", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Executor: "external"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	state := &store.RunState{ID: "external-provider", Nodes: map[string]*store.NodeState{"work": {Status: store.NodeRunning, Attempts: 1, External: &store.ExternalExecutionState{Status: "failed", Attempt: 1, Result: &store.ExternalResultState{SessionID: "session", ErrorCode: string(execution.KindProviderUnavailable), Error: "unavailable"}}}}, Approvals: map[string]string{}}
	result, err := r.executeAssistantAction(context.Background(), state, wf.Nodes[0], r.actionContext(state, wf.Nodes[0], nil))
	if execution.KindOf(err) != execution.KindProviderUnavailable || result.ProviderAttempt != 1 {
		t.Fatalf("external provider result = %+v err=%v", result, err)
	}
}

func TestExternalProviderRetryRequestsFreshClaimAndCompletes(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "external-provider-retry", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Executor: "external"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	state, err := r.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("initial external request = state=%+v err=%v", state, err)
	}
	state, err = r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := state.Nodes["work"].External
	first.Status = "failed"
	first.Result = &store.ExternalResultState{ExitCode: 1, ErrorCode: string(execution.KindProviderUnavailable), Error: "unavailable", SessionID: "external-session"}
	state.Nodes["work"].Status = store.NodePending
	state.Status, state.Waiting = store.RunRunning, nil
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("provider retry handoff = state=%+v err=%v", state, err)
	}
	state, err = r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["work"]
	if node.Attempts != 1 || node.Retry == nil || node.Retry.Scope != "provider" || node.Retry.ProviderAttempt != 2 || node.External == first || node.External.Status != "pending" || node.External.Attempt != 2 || node.External.SessionID != "external-session" || node.External.SessionMode != "resume" {
		t.Fatalf("fresh provider handoff = %+v", node)
	}
	node.External.Status = "completed"
	node.External.Result = &store.ExternalResultState{Output: "ok", Stdout: "ok", SessionID: "external-session", Resumed: true}
	node.Status = store.NodePending
	state.Nodes["work"] = node
	state.Status, state.Waiting = store.RunRunning, nil
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	node = state.Nodes["work"]
	if node.Status != store.NodeCompleted || node.Attempts != 1 || node.ProviderAttempts != 2 || len(node.Executions) != 2 || node.Executions[0].ProviderAttempt != 1 || node.Executions[1].ProviderAttempt != 2 {
		t.Fatalf("external provider completion = %+v", node)
	}
	events, err := r.store.(store.FS).ReadEvents(state.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	for _, event := range events {
		if event.Type == "external_node.requested" {
			requests++
		}
	}
	if requests != 2 {
		t.Fatalf("external requests = %d events=%+v", requests, events)
	}
}

func TestProviderRetryDelay(t *testing.T) {
	for _, test := range []struct {
		attempt    int
		retryAfter time.Duration
		want       time.Duration
	}{{1, 0, 2 * time.Second}, {2, 0, 4 * time.Second}, {1, time.Second, time.Second}, {1, 30 * time.Second, 30 * time.Second}, {2, 90 * time.Second, time.Minute}} {
		if got := providerRetryDelay(test.attempt, test.retryAfter); got != test.want {
			t.Fatalf("providerRetryDelay(%d, %s) = %s, want %s", test.attempt, test.retryAfter, got, test.want)
		}
	}
}

func TestProviderRetryKeepsOriginalNodeDeadline(t *testing.T) {
	dir := t.TempDir()
	var deadlines []time.Time
	adapter := adapterFunc(func(ctx context.Context, _ assistant.Request) (assistant.Result, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("provider execution has no node deadline")
		}
		deadlines = append(deadlines, deadline)
		if len(deadlines) == 1 {
			return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
		}
		return assistant.Result{Output: "ok", SessionID: "provider-session", Resumed: true}, nil
	})
	wf := &spec.Workflow{Name: "provider-deadline", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Timeout: "1s"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	if _, err := r.Start(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(deadlines) != 2 || !deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("provider deadlines = %v", deadlines)
	}
}

func TestProviderRetryBackoffConsumesNodeTimeout(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		calls++
		if calls == 1 {
			return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: 700 * time.Millisecond, Err: errors.New("unavailable")}
		}
		return assistant.Result{Output: "late", SessionID: "provider-session", Resumed: true}, nil
	})
	wf := &spec.Workflow{Name: "provider-timeout", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Timeout: "300ms"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected node timeout during provider backoff")
	}
	if calls != 1 || state.Nodes["work"].ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("calls=%d node=%+v err=%v", calls, state.Nodes["work"], err)
	}
}

func TestProviderRetryTerminalExecutionKeepsDiagnostic(t *testing.T) {
	dir := t.TempDir()
	messages := []string{"apple outage", "banana outage", "carrot outage"}
	calls := 0
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		message := messages[calls]
		calls++
		return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New(message)}
	})
	wf := &spec.Workflow{Name: "provider-terminal-diagnostic", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected provider exhaustion")
	}
	node := state.Nodes["work"]
	if len(node.Executions) != 3 || node.Executions[2].Diagnostic == nil || !strings.Contains(node.Executions[2].Diagnostic.Message, "carrot outage") || node.Executions[2].Diagnostic.Retryable {
		t.Fatalf("terminal provider execution = %+v", node.Executions)
	}
	events, readErr := r.store.(store.FS).ReadEvents(state.ID, 0, 100)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, event := range events {
		if event.Type == "provider.retry.exhausted" && event.Data["fingerprint"] != node.Executions[2].Diagnostic.Fingerprint {
			t.Fatalf("exhausted fingerprint=%v terminal=%s", event.Data["fingerprint"], node.Executions[2].Diagnostic.Fingerprint)
		}
	}
}

func TestProviderRetryFailureHookUsesOriginalNodeDeadline(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		calls++
		if calls == 1 {
			return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
		}
		return assistant.Result{ExitCode: 7, SessionID: "provider-session", Resumed: true}, &execution.Error{Kind: execution.KindExit, ExitCode: 7, Op: "provider", Err: errors.New("ordinary failure")}
	})
	wf := &spec.Workflow{Name: "provider-hook-timeout", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Timeout: "300ms", Hooks: spec.HookSet{OnFailure: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}}}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected node timeout in provider failure hook")
	}
	if state.Nodes["work"].ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("node=%+v err=%v", state.Nodes["work"], err)
	}
}

func TestExternalProviderFailureWithoutSessionIsProtocol(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "external-provider-session", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", Executor: "external"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	state := &store.RunState{ID: "external-provider-session", Nodes: map[string]*store.NodeState{"work": {Status: store.NodeRunning, Attempts: 1, External: &store.ExternalExecutionState{Status: "failed", Attempt: 1, Result: &store.ExternalResultState{ErrorCode: string(execution.KindProviderUnavailable), Error: "unavailable"}}}}, Approvals: map[string]string{}}
	_, err := r.executeAssistantAction(context.Background(), state, wf.Nodes[0], r.actionContext(state, wf.Nodes[0], nil))
	if execution.KindOf(err) != execution.KindProtocol {
		t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
	}
}

func TestRetryScopeDefaultsOldStateToWorkflow(t *testing.T) {
	if got := retryScope(nil); got != "workflow" {
		t.Fatalf("nil retry scope = %q", got)
	}
	if got := retryScope(&store.RetryState{}); got != "workflow" {
		t.Fatalf("legacy retry scope = %q", got)
	}
}

func TestProviderRetryExhaustionIgnoresAllowFailure(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		calls++
		return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
	})
	wf := &spec.Workflow{Name: "provider-exhausted", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m", AllowFailure: true, Attempts: spec.AttemptsSpec{Max: 1}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected provider retry exhaustion to fail")
	}
	node := state.Nodes["work"]
	if calls != 3 || state.Status != store.RunFailed || node.Status != store.NodeFailed || node.ErrorCode != string(execution.KindProviderUnavailable) || node.Attempts != 1 || node.ProviderAttempts != 3 {
		t.Fatalf("provider exhaustion state = run=%s node=%+v calls=%d", state.Status, node, calls)
	}
}

func TestProviderRetryOutOfBudgetFailsClosedWithoutAdapterCall(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	adapter := adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
		calls++
		return assistant.Result{SessionID: "unexpected"}, nil
	})
	wf := &spec.Workflow{Name: "provider-invalid-marker", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state := &store.RunState{ID: "provider-invalid-marker", Status: store.RunRunning, Nodes: map[string]*store.NodeState{"work": {Status: store.NodePending, Attempts: 1, Retry: &store.RetryState{Scope: "provider", ProviderAttempt: providerRetryMax + 1, NextAttempt: 1}, SessionID: "provider-session"}}, Approvals: map[string]string{}}
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := r.runNode(context.Background(), state, wf.Nodes[0], nil); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("invalid provider marker invoked adapter %d times", calls)
	}
	if state.Nodes["work"].ErrorCode != string(execution.KindProtocol) || state.Nodes["work"].Status != store.NodeErrored {
		t.Fatalf("invalid provider marker state = %+v", state.Nodes["work"])
	}
}

func TestProviderRetryEmitsDurableLifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
	})
	wf := &spec.Workflow{Name: "provider-events", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected provider exhaustion")
	}
	events, readErr := r.store.(store.FS).ReadEvents(state.ID, 0, 100)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var lifecycle []store.Event
	for _, event := range events {
		if strings.HasPrefix(event.Type, "provider.retry.") {
			lifecycle = append(lifecycle, event)
		}
	}
	if got, want := []string{lifecycle[0].Type, lifecycle[1].Type, lifecycle[2].Type, lifecycle[3].Type, lifecycle[4].Type}, []string{"provider.retry.scheduled", "provider.retry.ready", "provider.retry.scheduled", "provider.retry.ready", "provider.retry.exhausted"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("lifecycle types=%v want=%v", got, want)
	}
	for index, wantAttempt := range []int{1, 2} {
		event := lifecycle[index*2]
		if event.Data["scope"] != "provider" || event.Data["provider_attempt"] != float64(wantAttempt) || event.Data["max_provider_attempts"] != float64(3) || event.Data["kind"] != string(execution.KindProviderUnavailable) || event.Data["delay"] == "" || event.Data["not_before"] == nil || event.Data["fingerprint"] == "" {
			t.Fatalf("scheduled event %d=%+v", index, event.Data)
		}
	}
	for index, wantAttempt := range []int{2, 3} {
		event := lifecycle[index*2+1]
		if event.Data["scope"] != "provider" || event.Data["provider_attempt"] != float64(wantAttempt) || event.Data["max_provider_attempts"] != float64(3) {
			t.Fatalf("ready event %d=%+v", index, event.Data)
		}
	}
	exhausted := lifecycle[len(lifecycle)-1]
	if exhausted.Data["scope"] != "provider" || exhausted.Data["provider_attempts"] != float64(3) || exhausted.Data["max_provider_attempts"] != float64(3) || exhausted.Data["kind"] != string(execution.KindProviderUnavailable) || exhausted.Data["fingerprint"] == "" {
		t.Fatalf("exhausted event=%+v", exhausted.Data)
	}
}

func TestProviderRetryLeavesParallelSiblingCompleted(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	requests := map[string][]assistant.Request{}
	adapter := adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
		mu.Lock()
		requests[req.NodeID] = append(requests[req.NodeID], req)
		count := len(requests[req.NodeID])
		mu.Unlock()
		if req.NodeID == "retry" && count == 1 {
			return assistant.Result{ExitCode: 1, SessionID: "retry-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Millisecond, Err: errors.New("unavailable")}
		}
		return assistant.Result{Output: req.NodeID, ExitCode: 0, SessionID: req.NodeID + "-session"}, nil
	})
	wf := &spec.Workflow{Name: "provider-parallel", Nodes: []spec.Node{{ID: "retry", Prompt: "retry", Provider: "demo", Model: "m"}, {ID: "sibling", Prompt: "sibling", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	capture := &providerRetryCaptureStore{Repository: store.FS{Workspace: dir}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: capture, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Nodes["sibling"].Status != store.NodeCompleted || state.Nodes["retry"].Status != store.NodeCompleted || state.Nodes["retry"].ProviderAttempts != 2 {
		t.Fatalf("parallel provider state = retry=%+v sibling=%+v", state.Nodes["retry"], state.Nodes["sibling"])
	}
	mu.Lock()
	retryRequests := append([]assistant.Request(nil), requests["retry"]...)
	mu.Unlock()
	if len(retryRequests) != 2 || retryRequests[1].SessionMode != "resume" || retryRequests[1].SessionID != "retry-session" {
		t.Fatalf("retry requests = %+v", retryRequests)
	}
	scheduled := capture.states["provider.retry.scheduled"]
	if scheduled == nil || len(scheduled.CurrentNodes) != 1 || scheduled.CurrentNodes[0] != "sibling" || scheduled.CurrentNode != "" {
		t.Fatalf("parallel provider schedule ownership = %+v", scheduled)
	}
}

func TestProviderRetryParallelExhaustionEmitsDiagnosticFingerprint(t *testing.T) {
	dir := t.TempDir()
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", Err: errors.New("unavailable")}
	})
	wf := &spec.Workflow{Name: "provider-parallel-exhaustion", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	capture := &providerRetryCaptureStore{Repository: store.FS{Workspace: dir}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: capture, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	state := &store.RunState{ID: "provider-parallel-exhaustion", Status: store.RunRunning, Nodes: map[string]*store.NodeState{"work": {Status: store.NodePending, Attempts: 1, Retry: &store.RetryState{Scope: "provider", ProviderAttempt: 3, NextAttempt: 1}, SessionID: "provider-session", Resumed: true}}, Approvals: map[string]string{}}
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := r.runParallelWave(context.Background(), state, wf.Nodes, nil); err != nil {
		t.Fatal(err)
	}
	var exhausted store.Event
	for _, event := range capture.events {
		if event.Type == "provider.retry.exhausted" {
			exhausted = event
			break
		}
	}
	if exhausted.Type == "" {
		t.Fatal("provider.retry.exhausted was not emitted")
	}
	if exhausted.Data["scope"] != "provider" || exhausted.Data["provider_attempts"] != 3 || exhausted.Data["max_provider_attempts"] != 3 || exhausted.Data["kind"] != string(execution.KindProviderUnavailable) || exhausted.Data["fingerprint"] == "" {
		t.Fatalf("exhausted event=%+v", exhausted.Data)
	}
}

func TestProviderRetryCancellationWinsDuringBackoff(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{}, 1)
	calls := 0
	adapter := adapterFunc(func(_ context.Context, _ assistant.Request) (assistant.Result, error) {
		calls++
		started <- struct{}{}
		return assistant.Result{ExitCode: 1, SessionID: "provider-session"}, &execution.Error{Kind: execution.KindProviderUnavailable, Op: "provider", RetryAfter: time.Second, Err: errors.New("unavailable")}
	})
	wf := &spec.Workflow{Name: "provider-cancel", Nodes: []spec.Node{{ID: "work", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg)})
	result := make(chan error, 1)
	go func() {
		_, err := r.StartWithOptions(context.Background(), "", StartOptions{RunID: "provider-cancel"})
		result <- err
	}()
	<-started
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state, err := r.store.Load("provider-cancel")
		if err == nil && state.Nodes["work"].Retry != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := r.store.(store.FS).RequestCancel("provider-cancel"); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation result = %v", err)
	}
	if calls != 1 {
		t.Fatalf("provider retry called adapter %d times after cancellation", calls)
	}
}

func TestApprovalResume(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "do.md"), []byte("---\nprovider: demo\nmodel: large\n---\nHello $ARGUMENTS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "test", Nodes: []spec.Node{{ID: "do", Command: "do"}, {ID: "approve", DependsOn: []string{"do"}, Approval: &spec.ApprovalSpec{Message: "OK?", CaptureResponse: true}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"large": {Provider: "demo", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.commands.Dirs = []string{cmdDir}
	state, err := r.Start(context.Background(), "world")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	state, err = r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Waiting == nil || state.Waiting.Kind != "question" || state.Waiting.NodeID != "approve" {
		t.Fatalf("capture_response must publish a real question wait state: %#v", state.Waiting)
	}
	state.Approvals["approve"] = "yes"
	state.Nodes["approve"].Status = "pending"
	state.Status = "running"
	state.Waiting = nil
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" || state.Nodes["approve"].Output != "yes" {
		t.Fatalf("unexpected state: %+v", state)
	}
}

func TestHookRetry(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "retry", Nodes: []spec.Node{{ID: "n", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`, Attempts: spec.AttemptsSpec{Max: 3}, Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{ID: "check", Bash: `test $(cat c) -ge 2 || { echo too-small; exit 1; }`, OnFailure: spec.HookDecision{Action: "retry"}}}}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{}, Assistants: map[string]spec.AssistantSpec{}}
	r := New(wf, cfg, "wf", "cfg", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Nodes["n"].Attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", state.Nodes["n"].Attempts)
	}
}

func TestSharedContextResumesTransitiveAncestorSession(t *testing.T) {
	dir := t.TempDir()
	var requests []assistant.Request
	adapter := adapterFunc(func(_ context.Context, request assistant.Request) (assistant.Result, error) {
		requests = append(requests, request)
		return assistant.Result{Output: request.NodeID, Stdout: request.NodeID, ExitCode: 0, SessionID: "source-session"}, nil
	})
	wf := &spec.Workflow{Name: "shared-transitive", Nodes: []spec.Node{
		{ID: "source", Prompt: "source", Provider: "demo", Model: "m"},
		{ID: "bridge", DependsOn: []string{"source"}, Bash: "true"},
		{ID: "target", DependsOn: []string{"bridge"}, Prompt: "target", Provider: "demo", Model: "m", Context: "shared"},
	}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{
		Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir}, Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg),
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("run status = %s", state.Status)
	}
	if len(requests) != 2 || requests[1].NodeID != "target" {
		t.Fatalf("assistant requests = %#v", requests)
	}
	if requests[1].SessionMode != "resume" || requests[1].SessionID != "source-session" {
		t.Fatalf("target did not resume transitive source: mode=%q id=%q", requests[1].SessionMode, requests[1].SessionID)
	}
}

func TestLoopGroup(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop", Nodes: []spec.Node{{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 3, Nodes: []spec.Node{{ID: "inc", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`}, {ID: "check", DependsOn: []string{"inc"}, Bash: `test $(cat c) -ge 2`, AllowFailure: true}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}}}
	cfg := &spec.Config{}
	r := New(wf, cfg, "wf", "cfg", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" {
		t.Fatalf("unexpected: %+v", state)
	}
	loop := state.Nodes["loop"]
	if len(loop.LoopIterations) != 2 {
		t.Fatalf("expected two durable iterations, got %+v", loop.LoopIterations)
	}
	if loop.LoopIterations[0].Iteration != 1 || loop.LoopIterations[0].Nodes["check"].ExitCode != 1 || loop.LoopIterations[0].Satisfied {
		t.Fatalf("unexpected first iteration: %+v", loop.LoopIterations[0])
	}
	if loop.LoopIterations[1].Iteration != 2 || loop.LoopIterations[1].Nodes["check"].ExitCode != 0 || !loop.LoopIterations[1].Satisfied {
		t.Fatalf("unexpected second iteration: %+v", loop.LoopIterations[1])
	}
	if loop.LoopPrevious["check"].ExitCode != loop.LoopIterations[1].Nodes["check"].ExitCode {
		t.Fatalf("loop_previous no longer matches latest iteration: previous=%+v history=%+v", loop.LoopPrevious, loop.LoopIterations)
	}
	persisted, err := r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Nodes["loop"].LoopIterations) != 2 {
		t.Fatalf("iteration history was not durable: %+v", persisted.Nodes["loop"])
	}
}

func TestLoopFreshContextOverridesSharedChild(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	var requests []assistant.Request
	wf := &spec.Workflow{Name: "fresh-shared", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{
			MaxIterations: 2,
			FreshContext:  true,
			Nodes: []spec.Node{
				{ID: "source", Prompt: "source", Provider: "demo", Model: "m"},
				{ID: "target", DependsOn: []string{"source"}, Prompt: "target", Provider: "demo", Model: "m", Context: "shared"},
				{ID: "check", DependsOn: []string{"target"}, Bash: `n=0; test -f count && n=$(cat count); n=$((n+1)); echo -n $n > count; test $n -ge 2`, AllowFailure: true},
			},
			Until: spec.UntilSpec{Node: "check", ExitCode: &zero},
		},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, request assistant.Request) (assistant.Result, error) {
			requests = append(requests, request)
			sessionID := request.NodeID + "-session"
			if request.SessionID != "" {
				sessionID = request.SessionID
			}
			return assistant.Result{Output: request.NodeID, SessionID: sessionID, ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || len(requests) != 4 {
		t.Fatalf("unexpected loop result: status=%s requests=%d", state.Status, len(requests))
	}
	var targetRequests []assistant.Request
	for _, request := range requests {
		if request.NodeID == "target" {
			targetRequests = append(targetRequests, request)
		}
	}
	if len(targetRequests) != 2 {
		t.Fatalf("target requests = %+v", targetRequests)
	}
	if targetRequests[0].SessionMode != "resume" || targetRequests[0].SessionID != "source-session" {
		t.Fatalf("iteration 1 did not preserve shared context: %+v", targetRequests[0])
	}
	if targetRequests[1].SessionMode != "fresh" || targetRequests[1].SessionID != "" {
		t.Fatalf("fresh_context did not apply to iteration 2: %+v", targetRequests[1])
	}
}

func TestLoopFreshContextDoesNotOverrideRetrySession(t *testing.T) {
	zero := 0
	wf := &spec.Workflow{Name: "fresh-retry", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{
			MaxIterations: 2,
			FreshContext:  true,
			Nodes: []spec.Node{
				{ID: "source", Prompt: "source", Provider: "demo", Model: "m"},
				{ID: "target", DependsOn: []string{"source"}, Prompt: "target", Provider: "demo", Model: "m", Context: "shared", Executor: "external", Attempts: spec.AttemptsSpec{Max: 2, RetrySession: "reuse"}},
			},
			Until: spec.UntilSpec{Node: "target", ExitCode: &zero},
		},
	}}}
	r := New(wf, &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}}, "wf", "cfg", t.TempDir())
	state := &store.RunState{Nodes: map[string]*store.NodeState{
		"loop":   {LoopIteration: 2},
		"source": {SessionID: "source-session", Status: store.NodeCompleted},
		"target": {Attempts: 2, SessionID: "iteration-session", Status: store.NodeRunning},
	}}
	resolved, err := r.resolveAssistantNode(state, wf.Nodes[0].LoopGroup.Nodes[1], nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.SessionMode != "resume" || resolved.SessionID != "iteration-session" {
		t.Fatalf("retry session was overridden by fresh_context: mode=%q id=%q", resolved.SessionMode, resolved.SessionID)
	}
}

type failLoopStartStore struct {
	store.Repository
	crashed bool
}

type crashAfterSatisfiedCommitStore struct {
	store.Repository
	crashed bool
}

func (s *crashAfterSatisfiedCommitStore) Commit(state *store.RunState, event store.Event) error {
	if s.crashed {
		return errors.New("simulated process loss after satisfied commit")
	}
	if event.Type == "loop.iteration.completed" {
		satisfied, _ := event.Data["satisfied"].(bool)
		if satisfied {
			if err := s.Repository.Commit(state, event); err != nil {
				return err
			}
			s.crashed = true
			return errors.New("simulated process loss after satisfied commit")
		}
	}
	return s.Repository.Commit(state, event)
}

func (s *failLoopStartStore) Commit(state *store.RunState, event store.Event) error {
	if s.crashed {
		return errors.New("simulated crash between iterations")
	}
	if event.Type == "loop.iteration.started" {
		if iteration, ok := event.Data["iteration"].(int); ok && iteration == 2 {
			s.crashed = true
			return errors.New("simulated crash between iterations")
		}
	}
	return s.Repository.Commit(state, event)
}

func TestLoopGroupCrashBetweenIterationsResumesAfterDurableHistory(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	parent := spec.Node{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 3, Nodes: []spec.Node{{ID: "inc", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`}, {ID: "check", DependsOn: []string{"inc"}, Bash: `test $(cat c) -ge 2`, AllowFailure: true}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}
	wf := &spec.Workflow{Name: "loop-crash", Nodes: []spec.Node{parent}}
	r := New(wf, &spec.Config{}, "wf", "cfg", dir)
	base := r.store
	state := &store.RunState{ID: "run-loop-crash", Status: store.RunRunning, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: dir, ExecutionWorkspace: dir, Nodes: map[string]*store.NodeState{"loop": {Status: store.NodeRunning, Path: "/loop"}}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := base.Save(state); err != nil {
		t.Fatal(err)
	}
	r.store = &failLoopStartStore{Repository: base}
	if _, err := r.runLoopGroup(context.Background(), state, parent); err == nil || !strings.Contains(err.Error(), "simulated crash") {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	persisted, err := base.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	loop := persisted.Nodes["loop"]
	if loop.LoopIteration != 0 || len(loop.LoopIterations) != 1 || loop.LoopIterations[0].Iteration != 1 {
		t.Fatalf("unexpected durable boundary state: %+v", loop)
	}
	if got := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "c"))); got != "1" {
		t.Fatalf("first iteration side effect=%q", got)
	}
	r2 := New(wf, &spec.Config{}, "wf", "cfg", dir)
	result, err := r2.runLoopGroup(context.Background(), persisted, parent)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
	if got := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "c"))); got != "2" {
		t.Fatalf("iteration 1 was replayed after crash, counter=%q", got)
	}
	if len(persisted.Nodes["loop"].LoopIterations) != 2 || persisted.Nodes["loop"].LoopIterations[1].Iteration != 2 {
		t.Fatalf("history was duplicated or misnumbered: %+v", persisted.Nodes["loop"].LoopIterations)
	}
}

func TestLoopGroupResumeAfterSatisfiedCommitDoesNotReplaySideEffectsOrExhaust(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	parent := spec.Node{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{
		{ID: "effect", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`},
		{ID: "check", DependsOn: []string{"effect"}, Bash: `test $(cat c) -eq 1`, AllowFailure: true},
	}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}
	wf := &spec.Workflow{Name: "loop-satisfied-crash", Nodes: []spec.Node{parent}}
	r := New(wf, &spec.Config{}, "wf", "cfg", dir)
	base := r.store
	state := &store.RunState{ID: "run-loop-satisfied-crash", Status: store.RunRunning, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: dir, ExecutionWorkspace: dir, Nodes: map[string]*store.NodeState{"loop": {Status: store.NodeRunning, Path: "/loop"}}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := base.Save(state); err != nil {
		t.Fatal(err)
	}
	r.store = &crashAfterSatisfiedCommitStore{Repository: base}
	if _, err := r.runLoopGroup(context.Background(), state, parent); err == nil || !strings.Contains(err.Error(), "simulated process loss") {
		t.Fatalf("expected simulated crash, got %v", err)
	}
	persisted, err := base.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Nodes["loop"].LoopIterations) != 1 || !persisted.Nodes["loop"].LoopIterations[0].Satisfied {
		t.Fatalf("satisfied iteration was not durable: %+v", persisted.Nodes["loop"])
	}
	r2 := New(wf, &spec.Config{}, "wf", "cfg", dir)
	result, err := r2.runLoopGroup(context.Background(), persisted, parent)
	if err != nil {
		t.Fatalf("resume after satisfied commit must complete, got %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "c"))); got != "1" {
		t.Fatalf("satisfied iteration replayed side effects, counter=%q", got)
	}
	if len(persisted.Nodes["loop"].LoopIterations) != 1 {
		t.Fatalf("resume appended history after satisfied iteration: %+v", persisted.Nodes["loop"].LoopIterations)
	}
}

func TestLoopGroupRetryAfterExhaustionDoesNotAppendHistory(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop-exhaust", Nodes: []spec.Node{{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 2, Nodes: []spec.Node{{ID: "inc", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`}, {ID: "check", DependsOn: []string{"inc"}, Bash: `exit 1`, AllowFailure: true}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}}}
	r := New(wf, &spec.Config{}, "wf", "cfg", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected exhausted loop")
	}
	if len(state.Nodes["loop"].LoopIterations) != 2 {
		t.Fatalf("history=%+v", state.Nodes["loop"].LoopIterations)
	}
	before := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "c")))
	state.Nodes["loop"].Status = store.NodePending
	state.Nodes["loop"].Error = ""
	state.Status = store.RunRunning
	state.Error = ""
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	resumed, err := r.Resume(context.Background(), state)
	if err == nil {
		t.Fatal("expected exhaustion to remain terminal for the loop node")
	}
	if len(resumed.Nodes["loop"].LoopIterations) != 2 {
		t.Fatalf("retry appended history beyond max_iterations: %+v", resumed.Nodes["loop"].LoopIterations)
	}
	after := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "c")))
	if after != before {
		t.Fatalf("retry replayed loop side effects: before=%q after=%q", before, after)
	}
}

func TestLoopHistoryBackwardCompatibleWithoutLoopIterations(t *testing.T) {
	raw := `{"id":"run-old-loop","status":"running","nodes":{"loop":{"status":"pending","loop_previous":{"check":{"status":"completed","exit_code":1}}}}}`
	var state store.RunState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Nodes["loop"].LoopIterations) != 0 || state.Nodes["loop"].LoopPrevious["check"].ExitCode != 1 {
		t.Fatalf("legacy state did not decode: %+v", state.Nodes["loop"])
	}
}

func TestLoopHistoryBackwardCompatibleStateResumesWithoutLoopIterations(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "legacy-loop-resume", Nodes: []spec.Node{{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "check", Bash: `echo resumed`}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	raw := `{"id":"run-old-loop-resume","status":"running","workflow_path":"<workflow>","config_path":"<config>","workspace":"` + dir + `","execution_workspace":"` + dir + `","nodes":{"loop":{"status":"pending","loop_previous":{"check":{"status":"completed","exit_code":1}}}},"approvals":{}}`
	var state store.RunState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	if err := (store.FS{Workspace: dir}).Save(&state); err != nil {
		t.Fatal(err)
	}
	resumed, err := r.Resume(context.Background(), &state)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != store.RunCompleted || len(resumed.Nodes["loop"].LoopIterations) != 1 || !resumed.Nodes["loop"].LoopIterations[0].Satisfied {
		t.Fatalf("legacy loop did not resume into durable history: %+v", resumed.Nodes["loop"])
	}
	if got := resumed.Nodes["loop"].LoopIterations[0].Nodes["check"].Output; !strings.Contains(got, "resumed") {
		t.Fatalf("resume did not execute the next legacy iteration: %q", got)
	}
}

func TestLoopPreviousDoesNotAliasHistorySnapshot(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop-alias", Nodes: []spec.Node{{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "check", Bash: `echo ok`}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}}}
	state, err := New(wf, &spec.Config{}, "wf", "cfg", dir).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	loop := state.Nodes["loop"]
	prev := loop.LoopPrevious["check"]
	prev.Output = "mutated"
	loop.LoopPrevious["check"] = prev
	if loop.LoopIterations[0].Nodes["check"].Output == "mutated" {
		t.Fatal("loop_previous aliases immutable history snapshot")
	}
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAllowFailureOnlyAllowsNonZeroExit(t *testing.T) {
	t.Run("non-zero exit is data", func(t *testing.T) {
		dir := t.TempDir()
		wf := &spec.Workflow{Name: "allow-exit", Nodes: []spec.Node{{ID: "check", Bash: "exit 7", AllowFailure: true}}}
		r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
		state, err := r.Start(context.Background(), "")
		if err != nil {
			t.Fatal(err)
		}
		if state.Nodes["check"].Status != "completed" || state.Nodes["check"].ExitCode != 7 {
			t.Fatalf("unexpected node state: %+v", state.Nodes["check"])
		}
	})

	t.Run("start error remains fatal", func(t *testing.T) {
		dir := t.TempDir()
		wf := &spec.Workflow{Name: "allow-start", Provider: "broken", Model: "m", Nodes: []spec.Node{{ID: "agent", Prompt: "hello", AllowFailure: true}}}
		cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"broken": {Type: "process", Argv: []string{"definitely-missing-takt-binary"}}}}
		r := New(wf, cfg, "<workflow>", "<config>", dir)
		state, err := r.Start(context.Background(), "")
		if err == nil {
			t.Fatal("expected run failure")
		}
		if state.Nodes["agent"].Status != "errored" || state.Nodes["agent"].ErrorCode != "start" {
			t.Fatalf("unexpected node state: %+v", state.Nodes["agent"])
		}
	})
}

func TestAllDoneRunsAfterFailedDependency(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "all-done", Nodes: []spec.Node{
		{ID: "build", Bash: "exit 7"},
		{ID: "cleanup", DependsOn: []string{"build"}, TriggerRule: "all_done", Bash: "echo cleaned > cleanup.txt"},
		{ID: "publish", DependsOn: []string{"build"}, Bash: "echo published > publish.txt"},
	}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected failed run")
	}
	if state.Nodes["build"].Status != "failed" {
		t.Fatalf("build status: %+v", state.Nodes["build"])
	}
	if state.Nodes["cleanup"].Status != "completed" {
		t.Fatalf("cleanup status: %+v", state.Nodes["cleanup"])
	}
	if state.Nodes["publish"].Status != "skipped" {
		t.Fatalf("publish status: %+v", state.Nodes["publish"])
	}
	if _, statErr := os.Stat(filepath.Join(dir, "cleanup.txt")); statErr != nil {
		t.Fatalf("cleanup did not execute: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "publish.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("publish unexpectedly executed: %v", statErr)
	}
}

func TestLoopGroupUsesWhenAndTriggerRules(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop-semantics", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{
			{ID: "side-effect", When: `$INPUTS.input == "run"`, Bash: "echo touched > touched.txt"},
			{ID: "check", DependsOn: []string{"side-effect"}, TriggerRule: "all_done", Bash: "test ! -f touched.txt"},
		}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "skip")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" {
		t.Fatalf("unexpected state: %+v", state)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "touched.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("when=false child executed: %v", statErr)
	}
	previous := state.Nodes["loop"].LoopPrevious
	if previous["side-effect"].Status != "skipped" || previous["check"].Status != "completed" {
		t.Fatalf("unexpected loop states: %+v", previous)
	}
}

func TestNodeTimeoutAndAllDoneCleanup(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "timeout", Nodes: []spec.Node{
		{ID: "slow", Bash: "sleep 2", Timeout: "20ms"},
		{ID: "cleanup", DependsOn: []string{"slow"}, TriggerRule: "all_done", Bash: "echo done > cleanup.txt"},
	}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected timeout failure")
	}
	if state.Nodes["slow"].Status != "timed_out" {
		t.Fatalf("slow status: %+v", state.Nodes["slow"])
	}
	if state.Nodes["cleanup"].Status != "completed" {
		t.Fatalf("cleanup status: %+v", state.Nodes["cleanup"])
	}
}

type failingRepository struct {
	store.Repository
	failOn int
	count  int
}

func (f *failingRepository) Commit(state *store.RunState, event store.Event) error {
	f.count++
	if f.count == f.failOn {
		return fmt.Errorf("injected persistence failure")
	}
	return f.Repository.Commit(state, event)
}

func TestPersistenceErrorsAreReturned(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "persistence", Nodes: []spec.Node{{ID: "node", Bash: "true"}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	r.store = &failingRepository{Repository: store.FS{Workspace: dir}, failOn: 2}
	if _, err := r.Start(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "injected persistence failure") {
		t.Fatalf("expected persistence error, got %v", err)
	}
}

func TestNodeTimeoutCoversHookPhases(t *testing.T) {
	tests := []struct {
		name string
		node spec.Node
	}{
		{
			name: "before_node",
			node: spec.Node{ID: "n", Bash: "true", Timeout: "40ms", Hooks: spec.HookSet{
				BeforeNode: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}},
			}},
		},
		{
			name: "after_node",
			node: spec.Node{ID: "n", Bash: "true", Timeout: "40ms", Hooks: spec.HookSet{
				AfterNode: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}},
			}},
		},
		{
			name: "before_complete",
			node: spec.Node{ID: "n", Bash: "true", Timeout: "40ms", Hooks: spec.HookSet{
				BeforeComplete: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}},
			}},
		},
		{
			name: "on_failure",
			node: spec.Node{ID: "n", Bash: "exit 7", Timeout: "40ms", Hooks: spec.HookSet{
				OnFailure: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wf := &spec.Workflow{Name: "hook-timeout", Nodes: []spec.Node{tt.node}}
			r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
			started := time.Now()
			state, err := r.Start(context.Background(), "")
			if err == nil {
				t.Fatal("expected timeout failure")
			}
			if state.Nodes["n"].Status != store.NodeTimedOut || state.Nodes["n"].ErrorCode != string(execution.KindTimedOut) {
				t.Fatalf("unexpected node state: %+v", state.Nodes["n"])
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("node timeout did not bound hook phase: %s", elapsed)
			}
		})
	}
}

func TestCancellationDuringHookCancelsRun(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "hook-cancel", Nodes: []spec.Node{{
		ID: "n", Bash: "true", Hooks: spec.HookSet{BeforeNode: []spec.HookSpec{{ID: "slow", Bash: "sleep 1"}}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	state, err := r.Start(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	if state.Status != store.RunCancelled || state.Nodes["n"].Status != store.NodeCancelled {
		t.Fatalf("unexpected cancellation state: run=%s node=%+v", state.Status, state.Nodes["n"])
	}
}

func TestNestedLoopGroupIsRejectedAtRuntimeWithoutCorruptingOuterState(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "nested-loop", Nodes: []spec.Node{
		{ID: "victim", Bash: `n=0; test -f count && n=$(cat count); n=$((n+1)); echo -n $n > count`},
		{ID: "outer", DependsOn: []string{"victim"}, LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{
			{ID: "inner", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "victim", Bash: "true"}}, Until: spec.UntilSpec{Node: "victim", ExitCode: &zero}}},
		}, Until: spec.UntilSpec{Node: "inner", ExitCode: &zero}}},
	}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected nested loop failure")
	}
	data, readErr := os.ReadFile(filepath.Join(dir, "count"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "1" {
		t.Fatalf("top-level victim executed more than once: %q", data)
	}
	if state.Nodes["victim"] == nil || state.Nodes["victim"].Status != store.NodeCompleted {
		t.Fatalf("top-level state was corrupted: %+v", state.Nodes["victim"])
	}
}

func TestUntilRequiresCompletedNode(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "until-status", Nodes: []spec.Node{{
		ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "check", When: `$INPUTS.input == "run"`, Bash: "true",
		}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "skip")
	if err == nil {
		t.Fatal("expected loop exhaustion because until node was skipped")
	}
	if state.Nodes["loop"].Status != store.NodeFailed {
		t.Fatalf("unexpected loop state: %+v", state.Nodes["loop"])
	}
	if state.Nodes["loop"].LoopPrevious["check"].Status != store.NodeSkipped {
		t.Fatalf("unexpected until node state: %+v", state.Nodes["loop"].LoopPrevious["check"])
	}
}

func TestUntilDoesNotAcceptFailedNode(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "until-failed", Nodes: []spec.Node{{
		ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "check", Bash: "exit 7",
		}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected loop exhaustion because until node failed")
	}
	check := state.Nodes["loop"].LoopPrevious["check"]
	if check.Status != store.NodeFailed || check.ExitCode != 7 {
		t.Fatalf("unexpected until node state: %+v", check)
	}
}

func TestLoopBodyFailureStopsBeforeNextIterationAndSnapshotsState(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop-body-failure", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 3, Nodes: []spec.Node{
			{ID: "check", Bash: "echo attempt >> attempts; exit 7"},
		}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected failed loop")
	}
	if got := strings.TrimSpace(readFileForTest(t, filepath.Join(dir, "attempts"))); got != "attempt" {
		t.Fatalf("failure-like body was retried: %q", got)
	}
	loop := state.Nodes["loop"]
	if len(loop.LoopIterations) != 1 || loop.LoopIteration != 0 {
		t.Fatalf("active iteration was not durably snapshotted: %+v", loop)
	}
	if loop.LoopIterations[0].Nodes["check"].Status != store.NodeFailed {
		t.Fatalf("snapshot lost failed child: %+v", loop.LoopIterations[0])
	}
}

func TestCancelInsideLoopPersistsCanonicalPathAndIteration(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "loop-cancel", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 2, Nodes: []spec.Node{
			{ID: "stop", Cancel: "operator stop"},
		}, Until: spec.UntilSpec{Node: "stop", ExitCode: &zero}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected cancellation")
	}
	if state.Status != store.RunCancelled || state.CancelNodePath != "/loop/stop" || state.CancelIteration != 1 || state.CancelReason != "operator stop" {
		t.Fatalf("cancel metadata = %#v", state)
	}
}

func TestExpandedLoopChildUsesSingleCanonicalParentPath(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	raw := `name: expanded-loop-path
nodes:
  - id: loop
    loop_group:
      max_iterations: 1
      nodes:
        - id: stop
          cancel: stop
      until:
        node: stop
        exit_code: 0
`
	if err := os.WriteFile(workflowPath, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected cancel")
	}
	if state.CancelNodePath != "/loop/stop" {
		t.Fatalf("expanded loop child path = %q, want /loop/stop", state.CancelNodePath)
	}
}

func TestPredicateTruncationFailsBeforeNodeCompleted(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "truncated-predicate", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "review", Prompt: "review", Provider: "demo", Model: "m",
		}}, Until: spec.UntilSpec{Node: "review", Signal: "BUILD-CLEAN"}},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "<workflow>", "<config>", dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
			return assistant.Result{Output: "<promise>BUILD-CLEAN</promise>", Stdout: "<promise>BUILD-CLEAN</promise>", ExitCode: 0, SessionID: "s", Truncated: true}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("truncated predicate output was accepted")
	}
	loop := state.Nodes["loop"]
	if len(loop.LoopIterations) != 1 || loop.LoopIterations[0].Nodes["review"].Status != store.NodeErrored {
		t.Fatalf("truncated predicate snapshot = %#v", loop)
	}
	events, err := (store.FS{Workspace: dir}).ReadEvents(state.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == "node.completed" && event.NodeID == "review" {
			t.Fatal("predicate node completed before truncation failure")
		}
	}
}

func TestBaseBranchIsResolvedForShellAndUntilBashSurfaces(t *testing.T) {
	dir := t.TempDir()
	r := New(&spec.Workflow{Name: "base-branch", Nodes: []spec.Node{{ID: "bash", Bash: `test "$BASE_BRANCH" = base`}}}, &spec.Config{}, "<workflow>", "<config>", dir)
	state := &store.RunState{ID: "base-branch-run", Input: "", Nodes: map[string]*store.NodeState{"bash": {Status: store.NodePending}}, Approvals: map[string]string{}, Worktree: &store.WorktreeState{BaseRef: "base"}}
	if _, err := r.executeBashAction(context.Background(), state, spec.Node{ID: "bash", Bash: `test "$BASE_BRANCH" = base`}, actionContext{}); err != nil {
		t.Fatalf("durable base branch was not passed to bash: %v", err)
	}
	state.Worktree = nil
	if _, err := r.executeBashAction(context.Background(), state, spec.Node{ID: "bash", Bash: `test "$BASE_BRANCH" = base`}, actionContext{}); err == nil {
		t.Fatal("missing durable base branch was accepted")
	}
}

func TestUntilRequiresRejectsFailedTerminalEvidence(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "requires-failed", Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{
			{ID: "review", Bash: "true"},
			{ID: "validate", Bash: "exit 9"},
		}, Until: spec.UntilSpec{Node: "review", ExitCode: &zero, Requires: []spec.UntilRequirement{{Node: "validate", ExitCode: &zero}}}},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("failed required evidence was accepted")
	}
	if state.Nodes["loop"].LoopIterations[0].Nodes["validate"].Status != store.NodeFailed {
		t.Fatalf("required failure missing from snapshot: %+v", state.Nodes["loop"].LoopIterations[0])
	}
}

func TestParentLoopGroupTimeoutPreservesClassification(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{
		Name: "parent-loop-timeout", Nodes: []spec.Node{{
			ID:      "loop",
			Timeout: "40ms",
			LoopGroup: &spec.LoopGroupSpec{
				MaxIterations: 2,
				Nodes: []spec.Node{{
					ID:   "check",
					Bash: "sleep 1",
				}},
				Until: spec.UntilSpec{Node: "check", ExitCode: &zero},
			},
		}},
	}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected parent loop timeout")
	}
	parent := state.Nodes["loop"]
	if parent.Status != store.NodeTimedOut || parent.ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("parent loop lost timeout classification: %+v", parent)
	}
	if state.Status != store.RunFailed || state.ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("unexpected run state: status=%s code=%s error=%s", state.Status, state.ErrorCode, state.Error)
	}
}

func TestParentLoopGroupCancellationPreservesClassification(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{
		Name: "parent-loop-cancel", Nodes: []spec.Node{{
			ID: "loop",
			LoopGroup: &spec.LoopGroupSpec{
				MaxIterations: 2,
				Nodes: []spec.Node{{
					ID:   "check",
					Bash: "sleep 1",
				}},
				Until: spec.UntilSpec{Node: "check", ExitCode: &zero},
			},
		}},
	}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(40 * time.Millisecond)
		cancel()
	}()
	state, err := r.Start(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	parent := state.Nodes["loop"]
	if parent.Status != store.NodeCancelled || parent.ErrorCode != string(execution.KindCancelled) {
		t.Fatalf("parent loop lost cancellation classification: %+v", parent)
	}
	if state.Status != store.RunCancelled || state.ErrorCode != string(execution.KindCancelled) {
		t.Fatalf("unexpected run state: status=%s code=%s error=%s", state.Status, state.ErrorCode, state.Error)
	}
}

func TestProtocolAssistantResumesSessionAcrossRetry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-assistant")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-assistant")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake assistant: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "assistant-session", Provider: "fake", Model: "m", Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
			Context:  "resume",
			Attempts: spec.AttemptsSpec{Max: 2},
			Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
				ID:        "retry-once",
				Bash:      `test -f retried || { touch retried; echo retry; exit 1; }`,
				OnFailure: spec.HookDecision{Action: "retry"},
			}}},
		}},
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "model"}},
		Assistants: map[string]spec.AssistantSpec{"fake": {
			Type:           "process",
			Protocol:       assistant.ProtocolV1Alpha1,
			Argv:           []string{binary, "--case", "session-cycle"},
			MaxOutputBytes: 32 * 1024,
		}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["agent"]
	if node.Status != store.NodeCompleted || node.Attempts != 2 || node.SessionID != "cycle-session" {
		t.Fatalf("unexpected node state: %+v", node)
	}
}

func TestPiOverflowContextStateIntegration(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-pi")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v: %s", err, output)
	}

	tests := []struct {
		name       string
		caseName   string
		wantStatus string
		wantKind   execution.Kind
		context    func() (context.Context, context.CancelFunc, func())
	}{
		{
			name:       "timeout plus overflow",
			caseName:   "timeout-overflow",
			wantStatus: store.NodeTimedOut,
			wantKind:   execution.KindTimedOut,
			context: func() (context.Context, context.CancelFunc, func()) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				return ctx, cancel, func() { <-ctx.Done() }
			},
		},
		{
			name:       "cancel plus overflow",
			caseName:   "cancel-overflow",
			wantStatus: store.NodeCancelled,
			wantKind:   execution.KindCancelled,
			context: func() (context.Context, context.CancelFunc, func()) {
				ctx, cancel := context.WithCancel(context.Background())
				return ctx, cancel, cancel
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel, onTruncate := tt.context()
			defer cancel()
			dir := t.TempDir()
			wf := &spec.Workflow{
				Name: "pi-context-overflow", Provider: "pi", Model: "m", Nodes: []spec.Node{{ID: "agent", Prompt: "run"}},
			}
			cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model"}}}
			adapter := assistantpi.NewPi(spec.AssistantSpec{
				Type: "pi", Binary: binary, Args: []string{"--fake-case", tt.caseName},
				ProjectTrust: "approve", MaxOutputBytes: 1024,
			}).WithOutputTruncatedObserver(onTruncate)
			r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
			r.assistants = resolverFunc(func(name string) (assistant.Adapter, error) {
				if name != "pi" {
					return nil, fmt.Errorf("unexpected assistant %q", name)
				}
				return adapter, nil
			})

			state, runErr := r.Start(ctx, "")
			var failed *RunFailedError
			if !errors.Is(runErr, ctx.Err()) && !(errors.As(runErr, &failed) && failed.Code == string(tt.wantKind)) {
				t.Fatalf("unexpected run error: err=%v", runErr)
			}
			node := state.Nodes["agent"]
			if node.Status != tt.wantStatus || node.ErrorCode != string(tt.wantKind) || !node.OutputTruncated {
				t.Fatalf("unexpected node state: %+v", node)
			}
			if ctx.Done() == nil || ctx.Err() == nil {
				t.Fatalf("parent context did not complete correctly: done=%v err=%v", ctx.Done(), ctx.Err())
			}
		})
	}
}

func TestPiActivityControlsAssistantIdleTimeout(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-pi")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v: %s", err, output)
	}
	run := func(t *testing.T, caseName string, idle time.Duration) (*store.RunState, error, []string) {
		t.Helper()
		dir := t.TempDir()
		wf := &spec.Workflow{Name: "pi-activity", Provider: "pi", Model: "m", Nodes: []spec.Node{{ID: "agent", Prompt: "run"}}}
		cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model"}}}
		adapter := assistantpi.NewPi(spec.AssistantSpec{Type: "pi", Binary: binary, Args: []string{"--fake-case", caseName}, ProjectTrust: "approve", MaxOutputBytes: 64 * 1024})
		var activity []string
		r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: filepath.Join(dir, "workflow.yaml"), ConfigPath: filepath.Join(dir, "config.yaml"), ControlWorkspace: dir}, Dependencies{
			Store: store.FS{Workspace: dir}, Redactor: redact.NewFromConfig(cfg), AssistantIdleTimeout: idle,
			Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }),
			AssistantEvents: func(_, _ string, event assistant.Event) {
				activity = append(activity, event.Type+":"+event.Tool+":"+event.Message)
			},
			AssistantActivity: func(_, _, kind string) { activity = append(activity, "activity:"+kind) },
		})
		state, runErr := r.Start(context.Background(), "")
		return state, runErr, activity
	}
	t.Run("streaming progress resets timeout without durable partial events", func(t *testing.T) {
		state, runErr, activity := run(t, "streaming-progress", time.Second)
		if runErr != nil || state.Nodes["agent"].Status != store.NodeCompleted {
			t.Fatalf("state=%#v err=%v activity=%v", state, runErr, activity)
		}
		if got := strings.Join(activity, "\n"); !strings.Contains(got, "activity:provider.streaming") {
			t.Fatalf("streaming activity missing:\n%s", got)
		}
		if strings.Contains(state.Nodes["agent"].Stdout, "message_update") {
			t.Fatalf("transient streaming update became durable stdout: %q", state.Nodes["agent"].Stdout)
		}
	})
	t.Run("completed tool followed by provider stall times out", func(t *testing.T) {
		state, runErr, observed := run(t, "tool-then-hang", 500*time.Millisecond)
		if runErr == nil || state.Nodes["agent"].Status != store.NodeTimedOut || state.Nodes["agent"].ErrorCode != string(execution.KindTimedOut) {
			t.Fatalf("state=%#v err=%v", state, runErr)
		}
		joined := strings.Join(observed, "\n")
		for _, want := range []string{"tool.started:bash:", "tool.completed:bash:", "failed::assistant idle timeout"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("observer missing %q:\n%s", want, joined)
			}
		}
	})
}

func TestPiAssistantResumesSessionAcrossRetry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-pi")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "pi-session", Provider: "pi", Model: "m", Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
			Context:  "resume",
			Attempts: spec.AttemptsSpec{Max: 2},
			Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
				ID:        "retry-once",
				Bash:      `test -f retried || { touch retried; echo retry; exit 1; }`,
				OnFailure: spec.HookDecision{Action: "retry"},
			}}},
		}},
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model", Params: map[string]any{"reasoning_effort": "high"}}},
		Assistants: map[string]spec.AssistantSpec{"pi": {
			Type:           "pi",
			Binary:         binary,
			Args:           []string{"--fake-case", "success"},
			ProjectTrust:   "deny",
			MaxOutputBytes: 64 * 1024,
		}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["agent"]
	if node.Status != store.NodeCompleted || node.Attempts != 2 || node.SessionID != "fake-pi-session-1" {
		t.Fatalf("unexpected Pi node state: %+v", node)
	}
	if node.Usage == nil || node.Usage.InputTokens != 222 || node.Usage.OutputTokens != 44 || math.Abs(node.Usage.Cost-0.025) > 1e-9 {
		t.Fatalf("attempt usage was not accumulated: %+v", node.Usage)
	}
	if node.Assistant != "pi" || node.RequestedModel == nil || node.RequestedModel.Name != "m" || node.RequestedModel.Provider != "openai" || node.RequestedModel.ID != "fake-model" {
		t.Fatalf("requested execution identity was not preserved: %+v", node)
	}
	if node.ResolvedModel == nil || node.ResolvedModel.Provider != "openai" || node.ResolvedModel.ID != "fake-model" {
		t.Fatalf("resolved execution identity was not preserved: %+v", node.ResolvedModel)
	}
	if !strings.Contains(node.AssistantVersion, "0.83.0") {
		t.Fatalf("assistant version was not preserved: %q", node.AssistantVersion)
	}
	if len(node.Executions) != 2 {
		t.Fatalf("per-attempt executions were not preserved: %+v", node.Executions)
	}
	for index, executionRecord := range node.Executions {
		if executionRecord.Attempt != index+1 || executionRecord.Status != store.NodeCompleted || executionRecord.Usage == nil || executionRecord.Usage.InputTokens != 111 || executionRecord.Usage.OutputTokens != 22 {
			t.Fatalf("unexpected execution record %d: %+v", index, executionRecord)
		}
	}
}

func TestOpenCodeAssistantResumesSessionAcrossRetry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-opencode")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-opencode")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake OpenCode: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "opencode-session", Provider: "opencode", Model: "m", Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
			Context:  "resume",
			Attempts: spec.AttemptsSpec{Max: 2},
			Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
				ID:        "retry-once",
				Bash:      `test -f retried || { touch retried; echo retry; exit 1; }`,
				OnFailure: spec.HookDecision{Action: "retry"},
			}}},
		}},
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model", Params: map[string]any{"variant": "high"}}},
		Assistants: map[string]spec.AssistantSpec{"opencode": {
			Type:           "opencode",
			Binary:         binary,
			Args:           []string{"--fake-case", "success"},
			Agent:          "build",
			MaxOutputBytes: 64 * 1024,
		}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["agent"]
	if node.Status != store.NodeCompleted || node.Attempts != 2 || node.SessionID != "ses-opencode-1" || !node.Resumed {
		t.Fatalf("unexpected OpenCode node state: %+v", node)
	}
	if node.Usage == nil || node.Usage.InputTokens != 202 || node.Usage.OutputTokens != 34 || math.Abs(node.Usage.Cost-0.0084) > 1e-9 {
		t.Fatalf("attempt usage was not accumulated: %+v", node.Usage)
	}
	if node.Assistant != "opencode" || node.RequestedModel == nil || node.RequestedModel.Name != "m" || node.RequestedModel.Provider != "openai" || node.RequestedModel.ID != "fake-model" {
		t.Fatalf("requested execution identity was not preserved: %+v", node)
	}
	if node.ResolvedModel == nil || node.ResolvedModel.Provider != "openai" || node.ResolvedModel.ID != "fake-model" {
		t.Fatalf("resolved execution identity was not preserved: %+v", node.ResolvedModel)
	}
	if !strings.Contains(node.AssistantVersion, "1.2.3-test") {
		t.Fatalf("assistant version was not preserved: %q", node.AssistantVersion)
	}
	if len(node.Executions) != 2 {
		t.Fatalf("per-attempt executions were not preserved: %+v", node.Executions)
	}
	for index, executionRecord := range node.Executions {
		if executionRecord.Attempt != index+1 || executionRecord.Status != store.NodeCompleted || executionRecord.Usage == nil || executionRecord.Usage.InputTokens != 101 || executionRecord.Usage.OutputTokens != 17 {
			t.Fatalf("unexpected execution record %d: %+v", index, executionRecord)
		}
	}
}

func TestOpenCodeTimeoutPreservesProviderDiagnostics(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-opencode")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-opencode")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake OpenCode: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "opencode-provider-timeout", Provider: "opencode", Model: "m", Nodes: []spec.Node{{
			ID: "agent", Prompt: "hello", Timeout: "5s",
		}},
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model"}},
		Assistants: map[string]spec.AssistantSpec{"opencode": {
			Type: "opencode", Binary: binary, Args: []string{"--fake-case", "provider-timeout"}, MaxOutputBytes: 64 * 1024,
		}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	var runErr *RunFailedError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunFailedError, got %v", err)
	}
	node := state.Nodes["agent"]
	if node.Status != store.NodeTimedOut || node.ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("timeout classification changed: %+v", node)
	}
	for _, fragment := range []string{"retrying request 2/3", "connection refused"} {
		if !strings.Contains(node.Error, fragment) {
			t.Fatalf("diagnostic %q missing from node error: %+v", fragment, node)
		}
		if !strings.Contains(node.Output, fragment) {
			t.Fatalf("diagnostic %q missing from node output: %+v", fragment, node)
		}
	}
	if !strings.Contains(node.Stderr, "provider endpoint unavailable") || !strings.Contains(node.Stdout, `"type":"error"`) {
		t.Fatalf("raw OpenCode streams were not preserved: %+v", node)
	}
	if len(node.Executions) != 1 || node.Executions[0].Status != store.NodeTimedOut || !strings.Contains(node.Executions[0].Error, "connection refused") {
		t.Fatalf("execution diagnostic was not preserved: %+v", node.Executions)
	}
}

func TestRetryPreservesPerExecutionModelIdentityAndUsage(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "mixed-model-retry", Provider: "dynamic", Model: "logical", Nodes: []spec.Node{{
			ID: "agent", Prompt: "generate", Attempts: spec.AttemptsSpec{Max: 2},
			Context: "resume",
			Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
				ID: "retry-once", Bash: `test -f retried || { touch retried; echo retry; exit 1; }`,
				OnFailure: spec.HookDecision{Action: "retry"},
			}}},
		}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"logical": {Provider: "router", ID: "requested"}}}
	calls := 0
	adapter := adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
		calls++
		resolved := "model-a"
		version := "adapter-1"
		usage := &assistant.ProtocolUsage{InputTokens: 10, OutputTokens: 1, Cost: 0.1}
		if calls == 2 {
			resolved = "model-b"
			version = "adapter-2"
			usage = &assistant.ProtocolUsage{InputTokens: 20, OutputTokens: 2, Cost: 0.2}
		}
		return assistant.Result{
			Output: "ok", ExitCode: 0, SessionID: "session", Resumed: req.SessionID != "",
			AssistantVersion: version,
			ResolvedModel:    &assistant.ProtocolModel{Provider: "router", ID: resolved},
			Usage:            usage,
		}, nil
	})
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["agent"]
	if node.Status != store.NodeCompleted || node.Attempts != 2 || len(node.Executions) != 2 {
		t.Fatalf("unexpected node state: %+v", node)
	}
	first, second := node.Executions[0], node.Executions[1]
	if first.ResolvedModel == nil || first.ResolvedModel.ID != "model-a" || first.AssistantVersion != "adapter-1" || first.Usage == nil || first.Usage.InputTokens != 10 {
		t.Fatalf("first execution identity was overwritten: %+v", first)
	}
	if second.ResolvedModel == nil || second.ResolvedModel.ID != "model-b" || second.AssistantVersion != "adapter-2" || second.Usage == nil || second.Usage.InputTokens != 20 {
		t.Fatalf("second execution identity missing: %+v", second)
	}
	if node.ResolvedModel == nil || node.ResolvedModel.ID != "model-b" || node.Usage == nil || node.Usage.InputTokens != 30 {
		t.Fatalf("aggregate compatibility fields are incorrect: %+v", node)
	}
}

func TestExhaustedHookRetriesPreserveLastExecution(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "exhausted-resume", Provider: "demo", Model: "m", Nodes: []spec.Node{{
			ID: "agent", Prompt: "generate", Attempts: spec.AttemptsSpec{Max: 2},
			Context: "resume",
			Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{
				ID: "reject", Bash: `echo retry; exit 1`, OnFailure: spec.HookDecision{Action: "retry"},
			}}},
		}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "model"}}}
	calls := 0
	adapter := adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
		calls++
		return assistant.Result{Output: fmt.Sprintf("attempt-%d", calls), SessionID: "session", Resumed: req.SessionID != ""}, nil
	})
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	var runErr *RunFailedError
	if !errors.As(err, &runErr) {
		t.Fatalf("expected RunFailedError, got %v", err)
	}
	node := state.Nodes["agent"]
	if node.SessionID != "session" || !node.Resumed || node.Output != "attempt-2" {
		t.Fatalf("last execution was cleared on exhaustion: %+v", node)
	}
}

func TestIndependentNodesRunInParallel(t *testing.T) {
	dir := t.TempDir()
	waitForPeer := func(self, peer string) string {
		return fmt.Sprintf(`touch "$ARTIFACTS_DIR/%s.ready"
i=0
while [ ! -f "$ARTIFACTS_DIR/%s.ready" ] && [ "$i" -lt 200 ]; do
  i=$((i + 1))
  sleep 0.01
done
test -f "$ARTIFACTS_DIR/%s.ready"
printf '%s'`, self, peer, peer, self)
	}
	wf := &spec.Workflow{Name: "parallel", Nodes: []spec.Node{
		{ID: "a", Bash: waitForPeer("a", "b")},
		{ID: "b", Bash: waitForPeer("b", "a")},
	}}
	state, err := New(wf, &spec.Config{}, "<workflow>", "<config>", dir).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Nodes["a"].Output != "a" || state.Nodes["b"].Output != "b" {
		t.Fatalf("independent nodes did not cross the concurrency barrier: %+v", state)
	}
}

func TestApprovalInsideLoopGroupResumesAndPromptsEachIteration(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{Name: "interactive-loop", Nodes: []spec.Node{{
		ID: "explore",
		LoopGroup: &spec.LoopGroupSpec{
			MaxIterations: 3,
			Nodes: []spec.Node{
				{ID: "feedback", Approval: &spec.ApprovalSpec{Message: "Continue or ready?", CaptureResponse: true}},
				{ID: "check", DependsOn: []string{"feedback"}, Bash: `test $feedback.output = "ready"`, AllowFailure: true},
			},
			Until: spec.UntilSpec{Node: "check", ExitCode: &zero},
		},
	}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected first wait, got %v", err)
	}
	waiting := state.Waiting.NodeID
	state.Approvals[waiting] = "continue"
	state.Nodes[waiting].Status = store.NodePending
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected second wait, got %v", err)
	}
	if state.Nodes["explore"].LoopIteration != 2 {
		t.Fatalf("unexpected active iteration: %+v", state.Nodes["explore"])
	}
	waiting = state.Waiting.NodeID
	state.Approvals[waiting] = "ready"
	state.Nodes[waiting].Status = store.NodePending
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := r.store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("unexpected final state: %+v", state)
	}
	previous := state.Nodes["explore"].LoopPrevious
	if previous["feedback"].Output != "ready" {
		t.Fatalf("latest loop feedback was not preserved: %+v", previous)
	}
}

func TestParallelWavePublishesAllCurrentNodes(t *testing.T) {
	workspace := t.TempDir()
	wf := &spec.Workflow{
		Name: "parallel-status", Nodes: []spec.Node{
			{ID: "left", Bash: `while [ ! -f release ]; do sleep 0.02; done`},
			{ID: "right", Bash: `while [ ! -f release ]; do sleep 0.02; done`},
		},
	}
	cfg := &spec.Config{}
	r := New(wf, cfg, "<workflow>", "<config>", workspace)
	type result struct {
		state *store.RunState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, err := r.Start(context.Background(), "")
		done <- result{state: state, err: err}
	}()
	var runID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(filepath.Join(workspace, ".takt", "runs"))
		if len(entries) > 0 {
			runID = entries[0].Name()
			state, err := (store.FS{Workspace: workspace}).Load(runID)
			if err == nil && len(state.CurrentNodes) == 2 {
				if state.CurrentNodes[0] != "left" || state.CurrentNodes[1] != "right" {
					t.Fatalf("unexpected current nodes: %v", state.CurrentNodes)
				}
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("run state was not created")
	}
	state, err := (store.FS{Workspace: workspace}).Load(runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.CurrentNodes) != 2 {
		t.Fatalf("parallel current nodes were not published: %+v", state)
	}
	if err := os.WriteFile(filepath.Join(workspace, "release"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if out.err != nil || out.state.Status != store.RunCompleted || len(out.state.CurrentNodes) != 0 {
		t.Fatalf("unexpected final result state=%+v err=%v", out.state, out.err)
	}
}

type policyAdapter struct {
	caps []string
	seen *assistant.Request
}

func (p *policyAdapter) Run(_ context.Context, req assistant.Request) (assistant.Result, error) {
	copy := req
	p.seen = &copy
	return assistant.Result{Output: "ok", Stdout: "raw", ExitCode: 0}, nil
}

func (p *policyAdapter) Capabilities() []string { return append([]string(nil), p.caps...) }

func TestNodePolicyRejectsUnsupportedAssistantCapability(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "policy", Nodes: []spec.Node{{
		ID: "agent", Prompt: "test", Provider: "demo", Model: "model", DeniedTools: []string{"write"},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "test", ID: "model"}}}
	adapter := &policyAdapter{}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	if err == nil || state.Status != store.RunFailed || !strings.Contains(err.Error(), assistant.CapabilityToolPolicy) {
		t.Fatalf("unsupported policy was not rejected: state=%+v err=%v", state, err)
	}
	if adapter.seen != nil {
		t.Fatal("adapter was invoked before capability validation")
	}
}

func TestNodePolicyIsResolvedPassedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Review"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"search":{"type":"remote","url":"https://example.invalid/mcp"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	allowedTools := []string{"read", "grep"}
	skills := []string{"skills/review"}
	wf := &spec.Workflow{Name: "policy", Nodes: []spec.Node{{
		ID: "agent", Prompt: "test", Provider: "demo", Model: "model",
		AllowedTools: &allowedTools, DeniedTools: []string{"write"}, Skills: &skills, MCP: "mcp.json",
		Sandbox: &spec.SandboxSpec{Filesystem: "read_only"}, Requires: []string{"custom"},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "test", ID: "model"}}}
	adapter := &policyAdapter{caps: []string{assistant.CapabilityToolPolicy, assistant.CapabilitySkills, assistant.CapabilityMCP, assistant.CapabilitySandboxFilesystem, "custom"}}
	r := New(wf, cfg, workflowPath, filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if adapter.seen == nil || adapter.seen.Policy.MCPPath != filepath.Join(dir, "mcp.json") || len(adapter.seen.Policy.MCPConfig) == 0 {
		t.Fatalf("policy resources were not passed: %+v", adapter.seen)
	}
	if got := adapter.seen.Policy.Skills; len(got) != 1 || got[0] != skillDir {
		t.Fatalf("skill path was not resolved: %+v", got)
	}
	stored := state.Nodes["agent"].Policy
	if stored == nil || stored.MCPPath == "" || stored.Filesystem != "read_only" || !stored.ToolsRestricted || len(stored.Capabilities) != 5 {
		t.Fatalf("policy was not persisted: %+v", stored)
	}
}

func TestGovernedChildPolicyRestrictsChildNode(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	if err := os.WriteFile(childPath, []byte(`name: child
nodes:
  - id: agent
    prompt: child
    provider: demo
    model: model
    allowed_tools: [read, write]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentPath, []byte(`name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      policy:
        allowed_tools: [read, grep]
        denied_tools: [write]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "test", ID: "model"}}}
	adapter := &policyAdapter{caps: []string{assistant.CapabilityToolPolicy}}
	r := New(wf, cfg, parentPath, filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if adapter.seen == nil || len(adapter.seen.Policy.AllowedTools) != 1 || adapter.seen.Policy.AllowedTools[0] != "read" || len(adapter.seen.Policy.DeniedTools) != 1 {
		t.Fatalf("child policy was not inherited as an upper bound: %+v", adapter.seen)
	}
	child, err := r.store.Load(state.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if child.InheritedPolicy == nil || !child.InheritedPolicy.ToolsRestricted {
		t.Fatalf("inherited policy was not persisted on child run: %+v", child.InheritedPolicy)
	}
}

func TestAssistantEventsAreNormalizedAndPersisted(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{
		Name: "assistant-events", Provider: "demo", Model: "large", Nodes: []spec.Node{{ID: "agent", Prompt: "review"}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"large": {Provider: "provider-x", ID: "model-x"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
			assistant.Emit(req, assistant.Event{Type: assistant.EventToolStarted, Tool: "read", CallID: "call-1", Input: []byte(`{"path":"main.go"}`)})
			assistant.Emit(req, assistant.Event{Type: assistant.EventToolCompleted, Tool: "read", CallID: "call-1", Output: []byte(`{"bytes":12}`)})
			return assistant.Result{Output: "done", Stdout: "raw", ExitCode: 0, SessionID: "session-1", Usage: &assistant.ProtocolUsage{InputTokens: 7, OutputTokens: 2, Cost: 0.01}}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	events, err := (store.FS{Workspace: dir}).ReadEvents(state.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, event := range events {
		if strings.HasPrefix(event.Type, "assistant.") {
			types = append(types, event.Type)
		}
	}
	want := []string{"assistant.session.started", "assistant.tool.started", "assistant.tool.completed", "assistant.message", "assistant.usage", "assistant.completed"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("assistant event types = %#v, want %#v", types, want)
	}
}

func TestAssistantEventObserverReceivesProgressBeforeAdapterReturns(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "assistant-live-events", Provider: "demo", Model: "large", Nodes: []spec.Node{{ID: "agent", Prompt: "review"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"large": {Provider: "provider-x", ID: "model-x"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	observed := make(chan assistant.Event, 16)
	release := make(chan struct{})
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: filepath.Join(dir, "workflow.yaml"), ConfigPath: filepath.Join(dir, "config.yaml"), ControlWorkspace: dir}, Dependencies{
		Commands: NewCommandResolver(filepath.Join(dir, "workflow.yaml"), dir, dir), Store: store.FS{Workspace: dir}, Redactor: redact.NewFromConfig(cfg),
		Assistants: resolverFunc(func(string) (assistant.Adapter, error) {
			return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
				assistant.Emit(req, assistant.Event{Type: assistant.EventMessage, Message: "working"})
				<-release
				return assistant.Result{Output: "done"}, nil
			}), nil
		}),
		AssistantEvents: func(runID, nodeID string, event assistant.Event) { observed <- event },
	})
	done := make(chan error, 1)
	go func() { _, err := r.Start(context.Background(), ""); done <- err }()
	deadline := time.After(time.Second)
	for {
		select {
		case event := <-observed:
			if event.Message == "working" {
				goto observed
			}
		case <-deadline:
			t.Fatal("live event was not observed before adapter returned")
		}
	}
observed:
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestPreStartCancellationMarkerIsHonored(t *testing.T) {
	dir := t.TempDir()
	runID := "pre-cancelled-run"
	st := store.FS{Workspace: dir}
	if err := st.RequestCancel(runID); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "pre-cancel", Nodes: []spec.Node{{ID: "work", Bash: "touch should-not-exist"}}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: runID})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, state=%+v err=%v", state, err)
	}
	loaded, loadErr := st.Load(runID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.Status != store.RunCancelled {
		t.Fatalf("status = %s", loaded.Status)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "should-not-exist")); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled run executed work: %v", statErr)
	}
}

func TestPauseIsRecheckedBeforeRetryAttempt(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "do.md"), []byte("---\nprovider: demo\nmodel: m\n---\nretry me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "pause-retry", Nodes: []spec.Node{{ID: "do", Command: "do", Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"exit"}}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	r.commands.Dirs = []string{cmdDir}
	runID := make(chan string, 1)
	release := make(chan struct{})
	calls := 0
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(ctx context.Context, req assistant.Request) (assistant.Result, error) {
			calls++
			if calls == 1 {
				runID <- req.RunID
				<-release
				return assistant.Result{ExitCode: 7}, &execution.Error{Kind: execution.KindExit, ExitCode: 7, Op: "test", Err: errors.New("retryable")}
			}
			return assistant.Result{Output: "unexpected second attempt"}, nil
		}), nil
	})
	resultCh := make(chan struct {
		state *store.RunState
		err   error
	}, 1)
	go func() {
		state, err := r.Start(context.Background(), "")
		resultCh <- struct {
			state *store.RunState
			err   error
		}{state, err}
	}()
	id := <-runID
	if err := (store.FS{Workspace: dir}).RequestPause(id); err != nil {
		t.Fatal(err)
	}
	close(release)
	result := <-resultCh
	if !errors.Is(result.err, ErrPaused) {
		t.Fatalf("expected ErrPaused, got %v", result.err)
	}
	if calls != 1 {
		t.Fatalf("pause boundary allowed %d attempts", calls)
	}
	state, err := r.store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunPaused {
		t.Fatalf("status=%s", state.Status)
	}
}

func TestRetryBackoffPersistsDeadlineAndDiagnosticFingerprint(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "backoff", Nodes: []spec.Node{{
		ID:       "work",
		Bash:     `n=0; test -f count && n=$(cat count); n=$((n+1)); printf %s "$n" > count; if test "$n" -lt 3; then echo transient >&2; exit 7; fi; echo done`,
		Attempts: spec.AttemptsSpec{Max: 3, RetryOn: []string{"exit"}, Backoff: &spec.BackoffSpec{Initial: "80ms", Multiplier: 2, Max: "120ms"}},
	}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	runID := "retry-backoff-durable"
	resultCh := make(chan struct {
		state *store.RunState
		err   error
	}, 1)
	started := time.Now()
	go func() {
		state, err := r.StartWithOptions(context.Background(), "", StartOptions{RunID: runID})
		resultCh <- struct {
			state *store.RunState
			err   error
		}{state, err}
	}()

	var observed *store.RetryState
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := r.store.Load(runID)
		if err == nil && loaded.Nodes["work"] != nil && loaded.Nodes["work"].Retry != nil {
			copy := *loaded.Nodes["work"].Retry
			observed = &copy
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if observed == nil {
		t.Fatal("retry deadline was not persisted while backoff was active")
	}
	if observed.NotBefore.IsZero() || observed.Delay == "" || observed.Fingerprint == "" {
		t.Fatalf("incomplete durable retry state: %+v", observed)
	}

	result := <-resultCh
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got := result.state.Nodes["work"].Attempts; got != 3 {
		t.Fatalf("attempts=%d want 3", got)
	}
	if elapsed := time.Since(started); elapsed < 160*time.Millisecond {
		t.Fatalf("backoff did not delay retries: elapsed=%v", elapsed)
	}
	executions := result.state.Nodes["work"].Executions
	if len(executions) < 3 || executions[0].Diagnostic == nil || executions[1].Diagnostic == nil {
		t.Fatalf("missing execution diagnostics: %+v", executions)
	}
	if executions[0].Diagnostic.Fingerprint == "" || executions[0].Diagnostic.Fingerprint != executions[1].Diagnostic.Fingerprint {
		t.Fatalf("equivalent retry failures should share a fingerprint: %+v %+v", executions[0].Diagnostic, executions[1].Diagnostic)
	}
	if executions[0].Diagnostic.Retryable != true {
		t.Fatalf("first diagnostic should be retryable: %+v", executions[0].Diagnostic)
	}
}

func TestSecretRefIsRedactedFromDurableStateEventsAndTextArtifact(t *testing.T) {
	dir := t.TempDir()
	secret := "takt-super-secret-044"
	t.Setenv("TAKT_TEST_SECRET_TOKEN", secret)
	scriptPath := filepath.Join(dir, "emit.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s' \"$TOKEN\"\nprintf '%s' \"$TOKEN\" >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "secret", Nodes: []spec.Node{{
		ID: "emit", Script: &spec.ScriptSpec{Runtime: "command", Path: "emit.sh", Env: map[string]string{"TOKEN": "secret://TAKT_TEST_SECRET_TOKEN"}},
		OutputType: "secret-output", OutputMIME: "text/plain",
	}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(state.Nodes["emit"].Output, secret) {
		t.Fatal("execution did not receive resolved secret ref")
	}
	persisted, err := r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(persisted)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("secret leaked into durable state: %s", raw)
	}
	events, err := (store.FS{Workspace: dir}).ReadEvents(state.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	eventRaw, _ := json.Marshal(events)
	if strings.Contains(string(eventRaw), secret) {
		t.Fatalf("secret leaked into durable events: %s", eventRaw)
	}
	if len(persisted.Artifacts) != 1 {
		t.Fatalf("artifacts=%d want 1", len(persisted.Artifacts))
	}
	artifactData, err := os.ReadFile(persisted.Artifacts[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(artifactData), secret) || !strings.Contains(string(artifactData), "<redacted>") {
		t.Fatalf("text artifact was not redacted: %q", artifactData)
	}
}

func TestKnownSecretCannotBePersistedInBinaryArtifact(t *testing.T) {
	dir := t.TempDir()
	secret := "takt-binary-secret-044"
	t.Setenv("TAKT_TEST_BINARY_SECRET", secret)
	scriptPath := filepath.Join(dir, "emit.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '%s' \"$TOKEN\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "secret-binary", Nodes: []spec.Node{{
		ID: "emit", Script: &spec.ScriptSpec{Runtime: "command", Path: "emit.sh", Env: map[string]string{"TOKEN": "secret://TAKT_TEST_BINARY_SECRET"}},
		OutputType: "binary-output", OutputMIME: "application/octet-stream",
	}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected binary secret artifact to fail closed")
	}
	persisted, loadErr := r.store.Load(state.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	raw, _ := json.Marshal(persisted)
	if strings.Contains(string(raw), secret) {
		t.Fatalf("secret leaked into failed run state: %s", raw)
	}
}

func TestCanonicalNodePathUsesStructuredNamespace(t *testing.T) {
	cases := map[string]string{
		"build":                  "/build",
		"batch__001__append":     "/batch[1]/append",
		"outer__002__inner__003": "/outer[2]/inner[3]",
	}
	for id, want := range cases {
		if got := canonicalNodePath(id); got != want {
			t.Fatalf("canonicalNodePath(%q)=%q want %q", id, got, want)
		}
	}
}

func TestValidationScriptCannotBypassRequiredOSSandbox(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	wf := &spec.Workflow{Name: "validation-sandbox", Nodes: []spec.Node{{
		ID: "validate", Script: &spec.ScriptSpec{Runtime: "validation"}, Sandbox: &spec.SandboxSpec{Enforcement: "required", Network: "deny"},
	}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), `{"validation_commands":["true"]}`)
	if err == nil {
		t.Fatal("required OS sandbox should fail closed when no backend is available")
	}
	if state == nil || state.Nodes["validate"] == nil || state.Nodes["validate"].Sandbox == nil {
		t.Fatalf("sandbox decision was not persisted: %+v", state)
	}
	if state.Nodes["validate"].Sandbox.Status != "degraded" || !strings.Contains(state.Nodes["validate"].Error, "sandbox") {
		t.Fatalf("unexpected sandbox failure state: %+v", state.Nodes["validate"])
	}
	persisted, loadErr := r.store.Load(state.ID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if persisted.Nodes["validate"].Sandbox == nil || persisted.Nodes["validate"].Sandbox.Status != "degraded" {
		t.Fatalf("degraded sandbox decision was not durable: %+v", persisted.Nodes["validate"])
	}
}

func TestAfterHookCannotBypassRequiredOSSandbox(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	wf := &spec.Workflow{Name: "hook-sandbox", Nodes: []spec.Node{{
		ID: "worker", Prompt: "complete fixture", Provider: "demo", Model: "m", Sandbox: &spec.SandboxSpec{Enforcement: "required", Network: "deny"},
		Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{ID: "verify", Bash: "true", OnFailure: spec.HookDecision{Action: "fail"}}}},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "fixture", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("required OS sandbox on hook should fail closed when backend is unavailable")
	}
	if state == nil || state.Nodes["worker"] == nil || !strings.Contains(state.Nodes["worker"].Error, "sandbox") {
		t.Fatalf("unexpected hook sandbox state: %+v err=%v", state, err)
	}
}

func TestRetryBackoffDeadlineSurvivesPauseAndNewRunnerResume(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "backoff-resume", Nodes: []spec.Node{{
		ID:       "work",
		Bash:     `n=0; test -f count && n=$(cat count); n=$((n+1)); printf %s "$n" > count; if test "$n" -lt 2; then echo transient >&2; exit 7; fi; echo done`,
		Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"exit"}, Backoff: &spec.BackoffSpec{Initial: "500ms"}},
	}}}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	runID := "retry-backoff-resume"
	first := New(wf, &spec.Config{}, workflowPath, configPath, dir)
	resultCh := make(chan error, 1)
	go func() {
		_, err := first.StartWithOptions(context.Background(), "", StartOptions{RunID: runID})
		resultCh <- err
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		loaded, err := first.store.Load(runID)
		if err == nil && loaded.Nodes["work"] != nil && loaded.Nodes["work"].Retry != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := (store.FS{Workspace: dir}).RequestPause(runID); err != nil {
		t.Fatal(err)
	}
	if err := <-resultCh; !errors.Is(err, ErrPaused) {
		t.Fatalf("start error=%v want ErrPaused", err)
	}
	persisted, err := first.store.Load(runID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Nodes["work"] == nil || persisted.Nodes["work"].Retry == nil {
		t.Fatalf("retry deadline lost across pause: %+v", persisted.Nodes["work"])
	}

	second := New(wf, &spec.Config{}, workflowPath, configPath, dir)
	remaining := time.Until(persisted.Nodes["work"].Retry.NotBefore)
	if remaining <= 100*time.Millisecond {
		t.Fatalf("insufficient persisted backoff remaining for restart test: %v", remaining)
	}
	started := time.Now()
	resumed, err := second.Resume(context.Background(), persisted)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed+30*time.Millisecond < remaining {
		t.Fatalf("resume recomputed or skipped persisted deadline: elapsed=%v remaining=%v", elapsed, remaining)
	}
	if resumed.Status != store.RunCompleted || resumed.Nodes["work"].Attempts != 2 {
		t.Fatalf("resumed state=%+v", resumed.Nodes["work"])
	}
}

func TestCanonicalNodePathIsPersistedInStateAndEvents(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "node-path", Nodes: []spec.Node{{ID: "batch__1__append", Bash: "true"}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := r.store.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Nodes["batch__1__append"].Path; got != "/batch[1]/append" {
		t.Fatalf("node path=%q", got)
	}
	events, err := (store.FS{Workspace: dir}).ReadEvents(state.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, event := range events {
		if event.NodeID != "batch__1__append" {
			continue
		}
		if got, _ := event.Data["node_path"].(string); got != "/batch[1]/append" {
			t.Fatalf("event %s node_path=%q", event.Type, got)
		}
		seen = true
	}
	if !seen {
		t.Fatal("no node events observed")
	}
}

func TestAssistantAdapterSessionMetadataPersistsInExecutionAndNode(t *testing.T) {
	dir := t.TempDir()
	adapter := adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
		return assistant.Result{Output: "ok", Stdout: "ok", ExitCode: 0, Adapter: "fake", SessionPath: "/tmp/session.jsonl"}, nil
	})
	wf := &spec.Workflow{Name: "metadata", Nodes: []spec.Node{{ID: "agent", Prompt: "work", Provider: "demo", Model: "m"}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "demo", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := NewWithDependencies(Definition{Workflow: wf, Config: cfg, WorkflowPath: "wf", ConfigPath: "cfg", ControlWorkspace: dir}, Dependencies{
		Commands: NewCommandResolver("wf", dir, dir), Store: store.FS{Workspace: dir},
		Assistants: resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil }), Redactor: redact.NewFromConfig(cfg),
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["agent"]
	if node.Adapter != "fake" || node.SessionPath != "/tmp/session.jsonl" {
		t.Fatalf("latest node metadata = adapter %q path %q", node.Adapter, node.SessionPath)
	}
	if len(node.Executions) != 1 || node.Executions[0].Adapter != "fake" || node.Executions[0].SessionPath != "/tmp/session.jsonl" {
		t.Fatalf("execution metadata = %+v", node.Executions)
	}
}
