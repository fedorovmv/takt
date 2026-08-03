package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/spec"
	"takt/internal/store"
)

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
