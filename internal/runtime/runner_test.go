package runtime

import (
	"context"
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
)

type adapterFunc func(context.Context, assistant.Request) (assistant.Result, error)

func (f adapterFunc) Run(ctx context.Context, req assistant.Request) (assistant.Result, error) {
	return f(ctx, req)
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
			ID: "agent", Prompt: "hello", Timeout: "2s",
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
