package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"takt/internal/control"
	"takt/internal/store"
)

func TestDaemonRunsInBackgroundServesMCPAndStreamsEvents(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeDaemonFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeDaemonFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: daemon-background
nodes:
  - id: wait
    bash: |
      sleep 0.15
      printf done
`)
	server, err := New(Options{Workspace: workspace, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	client, err := NewClient(workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	metadata, err := WaitForHealth(waitCtx, client, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.PID == 0 || metadata.API != APIRevision || metadata.Socket == "" {
		t.Fatalf("metadata = %#v", metadata)
	}

	// A second independent client uses the same daemon and filesystem Store.
	second, err := NewClient(workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Health(context.Background()); err != nil {
		t.Fatal(err)
	}

	var started control.StartResult
	if err := client.Call(context.Background(), "run.start", control.StartRequest{Selector: workflowPath, ConfigPath: configPath}, &started); err != nil {
		t.Fatal(err)
	}
	if !started.Accepted || started.RunID == "" {
		t.Fatalf("started = %#v", started)
	}

	var eventsMu sync.Mutex
	var events []store.Event
	subscribeCtx, subscribeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer subscribeCancel()
	if err := second.Subscribe(subscribeCtx, started.RunID, 0, 200, func(event store.Event) error {
		eventsMu.Lock()
		defer eventsMu.Unlock()
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var state store.RunState
	if err := client.Call(context.Background(), "run.get", map[string]any{"run_id": started.RunID}, &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Output != "done" {
		t.Fatalf("state = %#v", state)
	}
	if len(events) == 0 {
		t.Fatal("subscription returned no events")
	}

	request := []byte(`{"jsonrpc":"2.0","id":"daemon-mcp","method":"tools/list","params":{}}`)
	payload, respond, err := client.MCP(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !respond {
		t.Fatal("MCP request did not return a response")
	}
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	result := envelope["result"].(map[string]any)
	if got := len(result["tools"].([]any)); got != 22 {
		t.Fatalf("MCP tools = %d", got)
	}

	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
	if _, err := os.Stat(server.Paths().Socket); !os.IsNotExist(err) {
		t.Fatalf("socket remains after shutdown: %v", err)
	}
}

func writeDaemonFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDaemonExpiresIdleExternalWorker(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	workflowPath := filepath.Join(workspace, "external.yaml")
	writeDaemonFile(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	writeDaemonFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: daemon-external-idle
defaults:
  assistant: worker
  model: demo
nodes:
  - id: delegated
    prompt: wait
    executor: external
    idle_timeout: 100ms
`)
	server, err := New(Options{Workspace: workspace, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()
	client, _ := NewClient(workspace, "")
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	if _, err := WaitForHealth(waitCtx, client, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var started control.StartResult
	if err := client.Call(context.Background(), "run.start", control.StartRequest{Selector: workflowPath, ConfigPath: configPath}, &started); err != nil {
		t.Fatal(err)
	}
	claimDeadline := time.Now().Add(2 * time.Second)
	for {
		tasks, pendingErr := server.service.PendingExternal(started.RunID, false)
		if pendingErr == nil && len(tasks) == 1 {
			break
		}
		if time.Now().After(claimDeadline) {
			t.Fatalf("external node did not become claimable: %v", pendingErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := server.service.ClaimExternal(control.ExternalClaimRequest{RunID: started.RunID, NodeID: "delegated", WorkerID: "idle-worker"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		state, loadErr := server.service.GetRun(started.RunID)
		if loadErr == nil && state.Status == store.RunFailed {
			if state.ErrorCode != "timed_out" {
				t.Fatalf("state = %#v", state)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("idle node did not expire: %v", loadErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := client.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not stop")
	}
}
