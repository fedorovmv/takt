package runtime

import (
	"context"
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
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
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
	if state.Nodes["assist"].Status != store.NodeCompleted {
		t.Fatalf("assist route did not complete: %+v", state.Nodes["assist"])
	}
	for _, name := range []string{"fix-github-issue", "create-issue", "piv-loop", "smart-pr-review", "ralph-dag"} {
		if state.Nodes[name].Status != store.NodeSkipped {
			t.Fatalf("route %s was not skipped: %+v", name, state.Nodes[name])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0] != "route" || calls[1] != "assist__assist" {
		t.Fatalf("unexpected assistant calls: %v", calls)
	}
}
