package notification

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/store"
)

func TestDispatchDeduplicatesAndAcknowledgesInbox(t *testing.T) {
	workspace := t.TempDir()
	dispatcher := Dispatcher{Workspace: workspace}
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-notify-complete", Status: store.RunCompleted, WorkflowPath: filepath.Join(workspace, "workflow.yaml"), ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(state); err != nil {
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
	state := &store.RunState{ID: "run-notify-wait", Status: store.RunWaiting, WorkflowPath: filepath.Join(workspace, "workflow.yaml"), ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace, Nodes: map[string]*store.NodeState{"approve": {Status: store.NodeWaiting}}, Approvals: map[string]string{}, Waiting: &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Approve plan"}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	items, err := dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Event != "approval.required" || items[0].Reason != "approval" {
		t.Fatalf("attention notification = %#v", items)
	}
	state.Waiting.Kind = "question"
	state.Waiting.Message = "Choose target"
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	items, err = dispatcher.Dispatch()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Reason != "question" {
		t.Fatalf("changed attention reason = %#v", items)
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
