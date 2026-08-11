package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
)

func TestAlwaysRunExecutesCleanupAfterDependencyFailure(t *testing.T) {
	dir := t.TempDir()
	workflow := &spec.Workflow{Name: "always-run", Nodes: []spec.Node{
		{ID: "fail", Bash: "exit 7"},
		{ID: "cleanup", DependsOn: []string{"fail"}, AlwaysRun: true, Bash: "printf cleaned > cleanup.txt"},
	}}
	runner := New(workflow, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	state, err := runner.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected failed Run")
	}
	if state.Nodes["cleanup"].Status != store.NodeCompleted {
		t.Fatalf("cleanup = %#v", state.Nodes["cleanup"])
	}
}

func TestAssistantIdleTimeoutUsesEventActivity(t *testing.T) {
	dir := t.TempDir()
	workflow := &spec.Workflow{Name: "idle", Provider: "demo", Model: "model", Nodes: []spec.Node{{ID: "agent", Prompt: "work", IdleTimeout: "50ms", Timeout: "2s"}}}
	config := &spec.Config{Models: map[string]spec.ModelSpec{"model": {Provider: "demo", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	runner := New(workflow, config, filepath.Join(dir, "workflow.yaml"), filepath.Join(dir, "config.yaml"), dir)
	runner.assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(ctx context.Context, request assistant.Request) (assistant.Result, error) {
			<-ctx.Done()
			return assistant.Result{}, context.Cause(ctx)
		}), nil
	})
	state, err := runner.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected idle timeout")
	}
	var failed *RunFailedError
	if !errors.As(err, &failed) || state.ErrorCode != string(execution.KindTimedOut) {
		t.Fatalf("state=%#v err=%v", state, err)
	}
}

func TestIdleMonitorResetsOnActivity(t *testing.T) {
	monitor, err := newIdleMonitor(context.Background(), "60ms")
	if err != nil {
		t.Fatal(err)
	}
	defer monitor.Close()
	for range 3 {
		time.Sleep(35 * time.Millisecond)
		monitor.Touch()
	}
	select {
	case <-monitor.Context().Done():
		t.Fatalf("monitor expired despite activity: %v", context.Cause(monitor.Context()))
	default:
	}
}
