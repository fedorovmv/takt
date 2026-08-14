package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
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
	for _, field := range []string{"assistant", "assistant_version", "requested_model", "resolved_model", "executions", "path", "diagnostic", "retry", "sandbox", "provider_attempts"} {
		if _, ok := node[field]; !ok {
			t.Fatalf("run-state schema misses %s", field)
		}
	}
	retry := defs["retryState"].(map[string]any)["properties"].(map[string]any)
	for _, field := range []string{"scope", "provider_attempt", "attempt_deadline"} {
		if _, ok := retry[field]; !ok {
			t.Fatalf("run-state retry schema misses %s", field)
		}
	}
	execution := defs["executionState"].(map[string]any)["properties"].(map[string]any)
	if _, ok := execution["provider_attempt"]; !ok {
		t.Fatal("run-state execution schema misses provider_attempt")
	}
}

func TestProviderRetryStateRoundTrip(t *testing.T) {
	deadline := time.Now().UTC().Add(time.Minute)
	want := NodeState{
		ProviderAttempts: 2,
		Retry:            &RetryState{Scope: "provider", ProviderAttempt: 2, NextAttempt: 1, NotBefore: time.Now().UTC(), Delay: "2s", AttemptDeadline: &deadline},
		Executions:       []ExecutionState{{Attempt: 1, ProviderAttempt: 2, Status: NodeErrored}},
	}
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got NodeState
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if got.ProviderAttempts != 2 || got.Retry == nil || got.Retry.Scope != "provider" || got.Retry.ProviderAttempt != 2 || got.Retry.AttemptDeadline == nil || !got.Retry.AttemptDeadline.Equal(deadline) || len(got.Executions) != 1 || got.Executions[0].ProviderAttempt != 2 {
		t.Fatalf("provider retry state round-trip = %+v", got)
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

func TestLoadRetriesTransientEventStateMismatch(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	state := &RunState{ID: "run-transient", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}

	// Reproduce the Commit window: the event journal and index have advanced,
	// while state.json still exposes the previous revision.
	eventsPath := filepath.Join(fs.RunDir(state.ID), "events.jsonl")
	events, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	event, _ := json.Marshal(Event{RunID: state.ID, Revision: 2, Type: "node.started", Time: time.Now().UTC()})
	events = append(events, event...)
	events = append(events, '\n')
	if err := os.WriteFile(eventsPath, events, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fs.RunDir(state.ID), eventIndexFile), buildEventIndex(events), 0o644); err != nil {
		t.Fatal(err)
	}

	updated := *state
	updated.Revision = 2
	go func() {
		time.Sleep(2 * time.Millisecond)
		_ = fs.writeStateAtomic(&updated)
	}()
	loaded, err := fs.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != 2 {
		t.Fatalf("revision = %d", loaded.Revision)
	}
}

func TestConcurrentLoadDuringCommitDoesNotExposeTransientMismatch(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	state := &RunState{ID: "run-concurrent", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}

	const commits = 24
	const readers = 3
	stop := make(chan struct{})
	errs := make(chan error, readers)
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if _, err := fs.Load(state.ID); err != nil {
						var inconsistent *InconsistentError
						if errors.As(err, &inconsistent) {
							errs <- err
							return
						}
						errs <- err
						return
					}
				}
			}
		}()
	}
	for i := 0; i < commits; i++ {
		if err := fs.Commit(state, Event{Type: "node.progress", Data: map[string]any{"n": i}}); err != nil {
			close(stop)
			wg.Wait()
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

func TestConcurrentCommitSerializesRevisionAndPreservesEvents(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	base := &RunState{ID: "run-concurrent-commit", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := fs.Commit(base, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	left, right := *base, *base
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, pair := range []struct {
		state *RunState
		event Event
	}{
		{&left, Event{Type: "left"}},
		{&right, Event{Type: "right"}},
	} {
		go func(state *RunState, event Event) {
			<-start
			errs <- fs.Commit(state, event)
		}(pair.state, pair.event)
	}
	close(start)
	var successes int
	for i := 0; i < 2; i++ {
		if err := <-errs; err == nil {
			successes++
		} else {
			var inconsistent *InconsistentError
			if !errors.As(err, &inconsistent) {
				t.Fatalf("concurrent commit error = %v", err)
			}
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent commits = %d, want 1", successes)
	}
	events, err := fs.ReadEvents(base.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("event count = %d, want 2", len(events))
	}
	if events[0].Revision != 1 || events[1].Revision != 2 {
		t.Fatalf("event revisions = %d,%d", events[0].Revision, events[1].Revision)
	}
}

func TestSaveRejectsStateOlderThanEventJournal(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	state := &RunState{ID: "run-stale-save", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	stale := *state
	if err := fs.Commit(state, Event{Type: "node.started"}); err != nil {
		t.Fatal(err)
	}
	if err := fs.Save(&stale); err == nil {
		t.Fatal("stale Save overwrote a newer committed revision")
	} else {
		var inconsistent *InconsistentError
		if !errors.As(err, &inconsistent) {
			t.Fatalf("stale Save error = %v", err)
		}
	}
}

func TestReadEventsWaitsForCommitLock(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	state := &RunState{ID: "run-events-lock", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	if err := fs.Commit(state, Event{Type: "run.started"}); err != nil {
		t.Fatal(err)
	}
	release, err := acquireCommitLock(fs.RunDir(state.ID))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, readErr := fs.ReadEvents(state.ID, 0, 0)
		done <- readErr
	}()
	select {
	case err := <-done:
		release()
		t.Fatalf("ReadEvents bypassed commit lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadEvents remained blocked after commit lock release")
	}
}

func TestLocalCommitLockSerializesGoroutines(t *testing.T) {
	release := acquireLocalCommitLock("same-run")
	done := make(chan struct{})
	go func() {
		secondRelease := acquireLocalCommitLock("same-run")
		secondRelease()
		close(done)
	}()
	select {
	case <-done:
		release()
		t.Fatal("local commit lock admitted a concurrent owner")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("local commit lock remained blocked after release")
	}
}

func TestReadEventsCreatesAndUsesOffsetIndex(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	state := &RunState{ID: "run-index", Status: RunRunning, Nodes: map[string]*NodeState{}, Approvals: map[string]string{}}
	for i := 0; i < 5; i++ {
		if err := fs.Commit(state, Event{Type: "event"}); err != nil {
			t.Fatal(err)
		}
	}
	index, err := os.ReadFile(filepath.Join(fs.RunDir(state.ID), eventIndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 5*8 {
		t.Fatalf("index size = %d", len(index))
	}
	events, err := fs.ReadEvents(state.ID, 4, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Revision != 5 {
		t.Fatalf("events = %#v", events)
	}
}

func TestClearOperatorMarkersReturnFilesystemErrors(t *testing.T) {
	fs := FS{Workspace: t.TempDir()}
	const runID = "run-marker-errors"
	if err := os.MkdirAll(fs.RunDir(runID), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"pause.requested", "cancel.requested"} {
		path := filepath.Join(fs.RunDir(runID), marker)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "child"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := fs.ClearPause(runID); err == nil {
		t.Fatal("ClearPause swallowed persistence error")
	}
	if err := fs.ClearCancel(runID); err == nil {
		t.Fatal("ClearCancel swallowed persistence error")
	}
}
