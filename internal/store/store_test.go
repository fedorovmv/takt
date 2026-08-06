package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommitAndLoadRevision(t *testing.T) {
	dir := t.TempDir()
	fs := FS{Workspace: dir}
	state := &RunState{ID: "run-1", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC()}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	if state.Revision != 1 {
		t.Fatalf("revision = %d", state.Revision)
	}
	loaded, err := fs.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 1 {
		t.Fatalf("loaded revision = %d", loaded.Revision)
	}
}

func TestLoadDetectsEventStateMismatch(t *testing.T) {
	dir := t.TempDir()
	fs := FS{Workspace: dir}
	state := &RunState{ID: "run-2", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC()}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fs.RunDir(state.ID), "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an interrupted/externally corrupted state write.
	data = []byte(string(data[:len(data)-2]) + `,"revision":0}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Load(state.ID); err == nil {
		t.Fatal("expected inconsistency error")
	}
}

func TestAcquireLockRejectsConcurrentOwner(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	release, err := fs.AcquireLock("run-3")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := fs.AcquireLock("run-3"); err == nil {
		t.Fatal("expected lock error")
	}
}

func TestUnsafeRunIDRejected(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	if _, err := fs.Load("../escape"); err == nil {
		t.Fatal("expected unsafe id error")
	}
}

func TestRunStateSchemaContainsExecutionIdentity(t *testing.T) {
	path := filepath.Join("..", "..", "schemas", "run-state.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	node := defs["nodeState"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"assistant", "assistant_version", "requested_model", "resolved_model", "executions"} {
		if _, ok := node[field]; !ok {
			t.Fatalf("run-state schema misses %s", field)
		}
	}
}

func TestCancellationMarkerLifecycle(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	if err := fs.RequestCancel("run-1"); err != nil {
		t.Fatal(err)
	}
	requested, err := fs.CancelRequested("run-1")
	if err != nil || !requested {
		t.Fatalf("expected cancellation marker: requested=%v err=%v", requested, err)
	}
	if err := fs.ClearCancel("run-1"); err != nil {
		t.Fatal(err)
	}
	requested, err = fs.CancelRequested("run-1")
	if err != nil || requested {
		t.Fatalf("marker was not cleared: requested=%v err=%v", requested, err)
	}
}

func TestReadEventsUsesRevisionCursorAndLimit(t *testing.T) {
	dir := t.TempDir()
	st := FS{Workspace: dir}
	state := &RunState{ID: "run-events", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := st.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(state, Event{Type: "node.started", NodeID: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(state, Event{Type: "node.completed", NodeID: "one"}); err != nil {
		t.Fatal(err)
	}
	events, err := st.ReadEvents(state.ID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Revision != 2 || events[0].Type != "node.started" {
		t.Fatalf("events = %#v", events)
	}
	events, err = st.ReadEvents(state.ID, 3, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected no events, got %#v", events)
	}
}
