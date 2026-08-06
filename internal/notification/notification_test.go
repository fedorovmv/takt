package notification

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
)

func TestDispatchDeduplicatesAndAcknowledgesInbox(t *testing.T) {
	workspace := t.TempDir()
	dispatcher := Dispatcher{Workspace: workspace}
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-notify-complete", Status: store.RunRunning, WorkflowPath: filepath.Join(workspace, "workflow.yaml"), ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	baseline, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline) != 0 {
		t.Fatalf("initial dispatch must establish baseline: %#v", baseline)
	}
	state.Status = store.RunCompleted
	if err := st.Commit(state, store.Event{Type: "test.completed"}); err != nil {
		t.Fatal(err)
	}
	first, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Event != "run.completed" {
		t.Fatalf("first dispatch = %#v", first)
	}
	second, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 0 {
		t.Fatalf("duplicate notification = %#v", second)
	}
	unread, err := dispatcher.List(true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 1 || unread[0].ID != first[0].ID {
		t.Fatalf("unread = %#v", unread)
	}
	acked, err := dispatcher.Ack(first[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if acked.AcknowledgedAt == nil {
		t.Fatal("notification was not acknowledged")
	}
	unread, err = dispatcher.List(true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(unread) != 0 {
		t.Fatalf("acknowledged item remains unread: %#v", unread)
	}
}

func TestAttentionNotificationIsEmittedOncePerReason(t *testing.T) {
	workspace := t.TempDir()
	dispatcher := Dispatcher{Workspace: workspace}
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-notify-wait", Status: store.RunRunning, WorkflowPath: filepath.Join(workspace, "workflow.yaml"), ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace, Nodes: map[string]*store.NodeState{"approve": {Status: store.NodePending}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	if items, err := dispatcher.Dispatch(); err != nil || len(items) != 0 {
		t.Fatalf("initial baseline = %#v, %v", items, err)
	}

	state.Status = store.RunWaiting
	state.Nodes["approve"].Status = store.NodeWaiting
	state.Waiting = &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Approve plan"}
	if err := st.Commit(state, store.Event{Type: "approval.requested", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	items, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != "approval.required" || items[0].Reason != "approval" {
		t.Fatalf("attention notification = %#v", items)
	}
	if items, err = dispatcher.Dispatch(); err != nil || len(items) != 0 {
		t.Fatalf("unchanged attention redelivered = %#v, %v", items, err)
	}

	// Resolve and enter the same approval again. Revision is part of the key, so
	// a loop iteration cannot silently reuse the old notification.
	state.Status = store.RunRunning
	state.Waiting = nil
	state.Nodes["approve"].Status = store.NodeRunning
	if err := st.Commit(state, store.Event{Type: "approval.answered", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(); err != nil {
		t.Fatal(err)
	}
	state.Status = store.RunWaiting
	state.Nodes["approve"].Status = store.NodeWaiting
	state.Waiting = &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Approve again"}
	if err := st.Commit(state, store.Event{Type: "approval.requested", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	items, err = dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != "approval.required" {
		t.Fatalf("repeated approval was lost = %#v", items)
	}

	state.Waiting.Kind = "question"
	state.Waiting.Message = "Choose target"
	if err := st.Commit(state, store.Event{Type: "question.requested", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	items, err = dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != "question.required" || items[0].Reason != "question" {
		t.Fatalf("question attention = %#v", items)
	}
}

func TestProcessSinkReceivesJSON(t *testing.T) {
	workspace := t.TempDir()
	out := filepath.Join(workspace, "notification.json")
	config := `apiVersion: takt/v1alpha1
kind: NotificationConfig
events: [run.completed]
sinks:
  - type: process
    command: /bin/sh
    args: [-c, "cat > ` + out + `"]
`
	if err := os.MkdirAll(filepath.Join(workspace, ".takt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".takt", "notifications.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatcher := Dispatcher{Workspace: workspace}
	item, err := dispatcher.Test("hello")
	if err != nil {
		t.Fatal(err)
	}
	if item.Deliveries["process:0"] != "delivered" {
		t.Fatalf("delivery = %#v", item.Deliveries)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryEmitsWorkerLostNotification(t *testing.T) {
	workspace := t.TempDir()
	dispatcher := Dispatcher{Workspace: workspace}
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-notify-recovery", Status: store.RunRunning, WorkflowPath: filepath.Join(workspace, "workflow.yaml"), ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Dispatch(); err != nil {
		t.Fatal(err)
	}
	state.RecoveryCount = 1
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	items, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != "worker.lost" || items[0].Command == "" {
		t.Fatalf("worker lost notification = %#v", items)
	}
}

func TestProcessSinkTimeoutDoesNotHangDispatcher(t *testing.T) {
	workspace := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: NotificationConfig
sinks:
  - type: process
    command: /bin/sh
    args: [-c, "sleep 5"]
    timeout: 1s
`
	if err := os.MkdirAll(filepath.Join(workspace, ".takt"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".takt", "notifications.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	item, err := (Dispatcher{Workspace: workspace}).Test("timeout")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("notification sink blocked for %s", elapsed)
	}
	if got := item.Deliveries["process:0"]; !strings.HasPrefix(got, "failed:") {
		t.Fatalf("delivery = %q", got)
	}
}

func TestPruneInboxPrefersAcknowledgedOldItems(t *testing.T) {
	workspace := t.TempDir()
	d := Dispatcher{Workspace: workspace}
	if err := os.MkdirAll(d.InboxDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour)
	for i := 0; i < 5; i++ {
		item := Item{ID: fmt.Sprintf("notice-prune-%d", i), Event: "test", Message: "x", CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if i == 1 || i == 3 {
			ts := base.Add(time.Duration(i) * time.Minute)
			item.AcknowledgedAt = &ts
		}
		if err := writeJSONAtomic(filepath.Join(d.InboxDir(), item.ID+".json"), item, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.pruneInbox(3); err != nil {
		t.Fatal(err)
	}
	items, err := d.List(false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items=%#v", items)
	}
	for _, item := range items {
		if item.ID == "notice-prune-1" || item.ID == "notice-prune-3" {
			t.Fatalf("acknowledged overflow item survived: %#v", items)
		}
	}
}

func TestConcurrentDispatchUsesSingleLockAndDoesNotDuplicate(t *testing.T) {
	workspace := t.TempDir()
	d := Dispatcher{Workspace: workspace}
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-concurrent-dispatch", Status: store.RunRunning, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Dispatch(); err != nil {
		t.Fatal(err)
	}
	state.Status = store.RunCompleted
	if err := st.Commit(state, store.Event{Type: "done"}); err != nil {
		t.Fatal(err)
	}
	results := make(chan []Item, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { items, err := d.Dispatch(); results <- items; errs <- err }()
	}
	total := 0
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		total += len(<-results)
	}
	if total != 1 {
		t.Fatalf("concurrent dispatch emitted %d notifications", total)
	}
}

func TestDesktopSinkTimeoutIsBounded(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux notify-send contract")
	}
	bin := t.TempDir()
	path := filepath.Join(bin, "notify-send")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	start := time.Now()
	err := deliverDesktop(Sink{Type: "desktop", Timeout: "1s"}, Item{Event: "test", Message: "x"})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("desktop timeout was not bounded: %s", time.Since(start))
	}
}
