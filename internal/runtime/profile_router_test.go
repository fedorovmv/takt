package runtime

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"takt/internal/assistant"
	"takt/internal/profile"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestCodeProfileSmartRouterExecutesExactlyOneWorkflow(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := profile.Resolve("code", workspace)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{
			"routing":        {Provider: "test", ID: "routing"},
			"implementation": {Provider: "test", ID: "implementation"},
			"review":         {Provider: "test", ID: "review"},
		},
		Assistants: map[string]spec.AssistantSpec{"opencode": {Type: "mock"}},
	}
	r := New(wf, cfg, resolved.WorkflowPath, filepath.Join(workspace, ".takt", "config.yaml"), workspace)
	var mu sync.Mutex
	var calls []string
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
			mu.Lock()
			calls = append(calls, req.NodeID)
			mu.Unlock()
			if req.NodeID == "route" {
				return assistant.Result{Output: `{"workflow":"assist","reason":"general help"}`, ExitCode: 0}, nil
			}
			return assistant.Result{Output: "assist completed", ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "explain this repository")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("unexpected run state: %+v", state)
	}
	if state.Worktree != nil {
		t.Fatalf("assist route unexpectedly created a worktree: %+v", state.Worktree)
	}
	if state.Nodes["assist"].Status != store.NodeCompleted {
		t.Fatalf("assist route did not complete: %+v", state.Nodes["assist"])
	}
	if state.Output != "assist completed" {
		t.Fatalf("router did not expose the selected terminal child output: %q", state.Output)
	}
	for _, name := range []string{"fix-github-issue", "create-issue", "piv-loop", "smart-pr-review", "ralph-dag"} {
		if state.Nodes[name].Status != store.NodeSkipped {
			t.Fatalf("route %s was not skipped: %+v", name, state.Nodes[name])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != "route" || calls[1] != "assist" {
		t.Fatalf("unexpected assistant calls: %v", calls)
	}
}

func TestCodeProfileRouterCreatesWorktreeForSelectedMutatingWorkflow(t *testing.T) {
	t.Setenv("TAKT_VALIDATE_COMMAND", "true")
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, workspace, "init")
	runtimeGit(t, workspace, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, workspace, "config", "user.name", "Takt Test")
	runtimeGit(t, workspace, "add", ".")
	runtimeGit(t, workspace, "commit", "-m", "profile")
	resolved, err := profile.Resolve("code", workspace)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{
			"routing":        {Provider: "test", ID: "routing"},
			"implementation": {Provider: "test", ID: "implementation"},
			"review":         {Provider: "test", ID: "review"},
		},
		Assistants: map[string]spec.AssistantSpec{"opencode": {Type: "mock"}},
	}
	r := New(wf, cfg, resolved.WorkflowPath, filepath.Join(workspace, ".takt", "config.yaml"), workspace)
	var mu sync.Mutex
	workspaces := map[string]string{}
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
			mu.Lock()
			workspaces[req.NodeID] = req.Workspace
			mu.Unlock()
			if req.NodeID == "route" {
				return assistant.Result{Output: `{"workflow":"feature-development","reason":"implement requested feature"}`, ExitCode: 0}, nil
			}
			if req.NodeID == "implement" {
				artifacts := r.store.ArtifactsDir(req.RunID)
				if err := os.MkdirAll(artifacts, 0o755); err != nil {
					return assistant.Result{}, err
				}
				if err := os.WriteFile(filepath.Join(artifacts, "implementation.md"), []byte("implemented\n"), 0o644); err != nil {
					return assistant.Result{}, err
				}
			}
			return assistant.Result{Output: "completed", ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "implement the existing plan")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Worktree != nil {
		t.Fatalf("router should complete in the control checkout: %+v", state)
	}
	if len(state.ChildRunIDs) != 1 {
		t.Fatalf("router did not create exactly one governed child: %+v", state.ChildRunIDs)
	}
	child, loadErr := r.store.Load(state.ChildRunIDs[0])
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if child.Status != store.RunCompleted || child.Worktree == nil {
		t.Fatalf("selected workflow did not complete in a managed worktree: %+v", child)
	}
	if !child.Worktree.Removed || child.Worktree.Path == "" || child.Worktree.Branch == "" {
		t.Fatalf("clean selected-workflow worktree was not finalized: %+v", child.Worktree)
	}
	mu.Lock()
	defer mu.Unlock()
	if workspaces["route"] != workspace {
		t.Fatalf("router should run in control checkout, got %q", workspaces["route"])
	}
	for _, id := range []string{"implement", "create-pr", "summary"} {
		if workspaces[id] == "" || workspaces[id] == workspace {
			t.Fatalf("selected child node %s did not run in worktree: %q", id, workspaces[id])
		}
	}
}

func TestCreateIssueReportsStructuredReproductionFailure(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, workspace, "init")
	runtimeGit(t, workspace, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, workspace, "config", "user.name", "Takt Test")
	runtimeGit(t, workspace, "add", ".")
	runtimeGit(t, workspace, "commit", "-m", "profile")
	resolved, err := profile.Resolve("code:create-issue", workspace)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{
			"routing":        {Provider: "test", ID: "routing"},
			"implementation": {Provider: "test", ID: "implementation"},
			"review":         {Provider: "test", ID: "review"},
		},
		Assistants: map[string]spec.AssistantSpec{"opencode": {Type: "mock"}},
	}
	r := New(wf, cfg, resolved.WorkflowPath, filepath.Join(workspace, ".takt", "config.yaml"), workspace)
	r.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
			switch req.NodeID {
			case "classify":
				return assistant.Result{Output: `{"issue_type":"bug","title":"Broken behavior"}`, ExitCode: 0}, nil
			case "reproduce":
				return assistant.Result{Output: `{"reproduced":"maybe"}`, Stdout: "provider-stream-diagnostics", Stderr: "schema mismatch", ExitCode: 0}, nil
			default:
				return assistant.Result{Output: "reported", ExitCode: 0}, nil
			}
		}), nil
	})
	state, err := r.Start(context.Background(), "report the broken behavior")
	if err == nil {
		t.Fatal("expected run to retain the reproduction protocol failure")
	}
	if state.Nodes["reproduce"].Status != store.NodeErrored || state.Nodes["reproduce"].ErrorCode != "protocol" {
		t.Fatalf("unexpected reproduce state: %+v", state.Nodes["reproduce"])
	}
	if state.Nodes["reproduction-error"].Status != store.NodeCompleted {
		t.Fatalf("reproduction failure was not reported: %+v", state.Nodes["reproduction-error"])
	}
	if state.Nodes["summary"].Status != store.NodeCompleted {
		t.Fatalf("summary did not run after reproduction failure: %+v", state.Nodes["summary"])
	}
	if state.Nodes["create"].Status != store.NodeSkipped || state.Nodes["cannot-reproduce"].Status != store.NodeSkipped {
		t.Fatalf("wrong issue creation branch ran: create=%+v cannot=%+v", state.Nodes["create"], state.Nodes["cannot-reproduce"])
	}
}
