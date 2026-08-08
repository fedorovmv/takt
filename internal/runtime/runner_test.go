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
	"runtime"
	"strings"
	"testing"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
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

func TestApprovalResume(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cmdDir, "do.md"), []byte("---\nassistant: demo\nmodel: large\n---\nHello $USER_MESSAGE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "test"}, Nodes: []spec.Node{{ID: "do", Command: "do"}, {ID: "approve", DependsOn: []string{"do"}, Approval: &spec.ApprovalSpec{Message: "OK?", CaptureResponse: true}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"large": {Provider: "demo", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.Commands.Dirs = []string{cmdDir}
	state, err := r.Start(context.Background(), "world")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	state, err = r.Store.Load(state.ID)
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
	if err := r.Store.Save(state); err != nil {
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "retry"}, Nodes: []spec.Node{{ID: "n", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`, Attempts: spec.AttemptsSpec{Max: 3}, Hooks: spec.HookSet{AfterNode: []spec.HookSpec{{ID: "check", Bash: `test $(cat c) -ge 2 || { echo too-small; exit 1; }`, OnFailure: spec.HookDecision{Action: "retry"}}}}}}}
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

func TestLoopGroup(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "loop"}, Nodes: []spec.Node{{ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 3, Nodes: []spec.Node{{ID: "inc", Bash: `n=0; test -f c && n=$(cat c); n=$((n+1)); echo -n $n > c`}, {ID: "check", DependsOn: []string{"inc"}, Bash: `test $(cat c) -ge 2`, AllowFailure: true}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}}}}}
	cfg := &spec.Config{}
	r := New(wf, cfg, "wf", "cfg", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "completed" {
		t.Fatalf("unexpected: %+v", state)
	}
}

func TestAllowFailureOnlyAllowsNonZeroExit(t *testing.T) {
	t.Run("non-zero exit is data", func(t *testing.T) {
		dir := t.TempDir()
		wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "allow-exit"}, Nodes: []spec.Node{{ID: "check", Bash: "exit 7", AllowFailure: true}}}
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
		wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "allow-start"}, Defaults: spec.Defaults{Assistant: "broken", Model: "m"}, Nodes: []spec.Node{{ID: "agent", Prompt: "hello", AllowFailure: true}}}
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "all-done"}, Nodes: []spec.Node{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "loop-semantics"}, Nodes: []spec.Node{{
		ID: "loop",
		LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{
			{ID: "side-effect", When: `inputs.input == "run"`, Bash: "echo touched > touched.txt"},
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "timeout"}, Nodes: []spec.Node{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "persistence"}, Nodes: []spec.Node{{ID: "node", Bash: "true"}}}
	r := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	r.Store = &failingRepository{Repository: store.FS{Workspace: dir}, failOn: 2}
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
			wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "hook-timeout"}, Nodes: []spec.Node{tt.node}}
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "hook-cancel"}, Nodes: []spec.Node{{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "nested-loop"}, Nodes: []spec.Node{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "until-status"}, Nodes: []spec.Node{{
		ID: "loop", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "check", When: `inputs.input == "run"`, Bash: "true",
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "until-failed"}, Nodes: []spec.Node{{
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

func TestParentLoopGroupTimeoutPreservesClassification(t *testing.T) {
	dir := t.TempDir()
	zero := 0
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "parent-loop-timeout"},
		Nodes: []spec.Node{{
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
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "parent-loop-cancel"},
		Nodes: []spec.Node{{
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
	build := exec.Command("go", "build", "-o", binary, "./cmd/takt-fake-assistant")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake assistant: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "assistant-session"},
		Defaults:   spec.Defaults{Assistant: "fake", Model: "m", Session: "resume"},
		Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
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
	build := exec.Command("go", "build", "-o", binary, "./cmd/takt-fake-pi")
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
				APIVersion: "takt/v1alpha1",
				Kind:       "Workflow",
				Metadata:   spec.Metadata{Name: "pi-context-overflow"},
				Defaults:   spec.Defaults{Assistant: "pi", Model: "m"},
				Nodes:      []spec.Node{{ID: "agent", Prompt: "run"}},
			}
			cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "openai", ID: "fake-model"}}}
			adapter := assistant.NewPi(spec.AssistantSpec{
				Type: "pi", Binary: binary, Args: []string{"--fake-case", tt.caseName},
				ProjectTrust: "approve", MaxOutputBytes: 1024,
			}).WithOutputTruncatedObserver(onTruncate)
			r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
			r.Assistants = resolverFunc(func(name string) (assistant.Adapter, error) {
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

func TestPiAssistantResumesSessionAcrossRetry(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "takt-fake-pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/takt-fake-pi")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "pi-session"},
		Defaults:   spec.Defaults{Assistant: "pi", Model: "m", Session: "resume"},
		Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
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
	build := exec.Command("go", "build", "-o", binary, "./cmd/takt-fake-opencode")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake OpenCode: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "opencode-session"},
		Defaults:   spec.Defaults{Assistant: "opencode", Model: "m", Session: "resume"},
		Nodes: []spec.Node{{
			ID:       "agent",
			Prompt:   "hello",
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
	build := exec.Command("go", "build", "-o", binary, "./cmd/takt-fake-opencode")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake OpenCode: %v: %s", err, output)
	}

	dir := t.TempDir()
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "opencode-provider-timeout"},
		Defaults:   spec.Defaults{Assistant: "opencode", Model: "m"},
		Nodes: []spec.Node{{
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
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "mixed-model-retry"},
		Defaults: spec.Defaults{Assistant: "dynamic", Model: "logical", Session: "resume"},
		Nodes: []spec.Node{{
			ID: "agent", Prompt: "generate", Attempts: spec.AttemptsSpec{Max: 2},
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
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "parallel"}, Nodes: []spec.Node{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "interactive-loop"}, Nodes: []spec.Node{{
		ID: "explore",
		LoopGroup: &spec.LoopGroupSpec{
			MaxIterations: 3,
			Nodes: []spec.Node{
				{ID: "feedback", Approval: &spec.ApprovalSpec{Message: "Continue or ready?", CaptureResponse: true}},
				{ID: "check", DependsOn: []string{"feedback"}, Bash: `test "${nodes.feedback.output}" = "ready"`, AllowFailure: true},
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
	if err := r.Store.Save(state); err != nil {
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
	if err := r.Store.Save(state); err != nil {
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
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "parallel-status"},
		Nodes: []spec.Node{
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "policy"}, Nodes: []spec.Node{{
		ID: "agent", Prompt: "test", Assistant: "demo", Model: "model", DeniedTools: []string{"write"},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "test", ID: "model"}}}
	adapter := &policyAdapter{}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "policy"}, Nodes: []spec.Node{{
		ID: "agent", Prompt: "test", Assistant: "demo", Model: "model",
		AllowedTools: &allowedTools, DeniedTools: []string{"write"}, Skills: &skills, MCP: "mcp.json",
		Sandbox: &spec.SandboxSpec{Filesystem: "read_only"}, Requires: []string{"custom"},
	}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "test", ID: "model"}}}
	adapter := &policyAdapter{caps: []string{assistant.CapabilityToolPolicy, assistant.CapabilitySkills, assistant.CapabilityMCP, assistant.CapabilitySandboxFilesystem, "custom"}}
	r := New(wf, cfg, workflowPath, filepath.Join(dir, "config.yaml"), dir)
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
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
	if err := os.WriteFile(childPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: agent
    prompt: child
    assistant: demo
    model: model
    allowed_tools: [read, write]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
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
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) { return adapter, nil })
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if adapter.seen == nil || len(adapter.seen.Policy.AllowedTools) != 1 || adapter.seen.Policy.AllowedTools[0] != "read" || len(adapter.seen.Policy.DeniedTools) != 1 {
		t.Fatalf("child policy was not inherited as an upper bound: %+v", adapter.seen)
	}
	child, err := r.Store.Load(state.ChildRunIDs[0])
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
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "assistant-events"},
		Defaults: spec.Defaults{Assistant: "demo", Model: "large"},
		Nodes:    []spec.Node{{ID: "agent", Prompt: "review"}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"large": {Provider: "provider-x", ID: "model-x"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
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

func TestPreStartCancellationMarkerIsHonored(t *testing.T) {
	dir := t.TempDir()
	runID := "pre-cancelled-run"
	st := store.FS{Workspace: dir}
	if err := st.RequestCancel(runID); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "pre-cancel"}, Nodes: []spec.Node{{ID: "work", Bash: "touch should-not-exist"}}}
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
	if err := os.WriteFile(filepath.Join(cmdDir, "do.md"), []byte("---\nassistant: demo\nmodel: m\n---\nretry me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "pause-retry"}, Nodes: []spec.Node{{ID: "do", Command: "do", Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"exit"}}}}}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "wf", "cfg", dir)
	r.Commands.Dirs = []string{cmdDir}
	runID := make(chan string, 1)
	release := make(chan struct{})
	calls := 0
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
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
	state, err := r.Store.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunPaused {
		t.Fatalf("status=%s", state.Status)
	}
}

func TestRetryBackoffPersistsDeadlineAndDiagnosticFingerprint(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "backoff"}, Nodes: []spec.Node{{
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
		loaded, err := r.Store.Load(runID)
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "secret"}, Nodes: []spec.Node{{
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
	persisted, err := r.Store.Load(state.ID)
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "secret-binary"}, Nodes: []spec.Node{{
		ID: "emit", Script: &spec.ScriptSpec{Runtime: "command", Path: "emit.sh", Env: map[string]string{"TOKEN": "secret://TAKT_TEST_BINARY_SECRET"}},
		OutputType: "binary-output", OutputMIME: "application/octet-stream",
	}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected binary secret artifact to fail closed")
	}
	persisted, loadErr := r.Store.Load(state.ID)
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "validation-sandbox"}, Nodes: []spec.Node{{
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
	persisted, loadErr := r.Store.Load(state.ID)
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "hook-sandbox"}, Nodes: []spec.Node{{
		ID: "worker", Prompt: "complete fixture", Assistant: "demo", Model: "m", Sandbox: &spec.SandboxSpec{Enforcement: "required", Network: "deny"},
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "backoff-resume"}, Nodes: []spec.Node{{
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
		loaded, err := first.Store.Load(runID)
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
	persisted, err := first.Store.Load(runID)
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "node-path"}, Nodes: []spec.Node{{ID: "batch__1__append", Bash: "true"}}}
	r := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := r.Store.Load(state.ID)
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
