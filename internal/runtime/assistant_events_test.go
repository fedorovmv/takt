package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"takt/internal/assistant"
)

func TestAssistantEventCollectorRejectsWriteOutsideExecutionWorkspace(t *testing.T) {
	control := t.TempDir()
	execution := filepath.Join(t.TempDir(), "execution")
	collector := newAssistantEventCollector(execution, filepath.Join(control, ".takt", "runs", "run-1", "artifacts"))
	violated := false
	collector.onViolation = func() { violated = true }
	collector.Emit(assistant.Event{Type: assistant.EventToolStarted, Tool: "write", CallID: "call-1", Input: json.RawMessage(`{"path":"` + filepath.Join(control, "internal", "main.go") + `"}`)})
	if _, err := collector.Result(); err == nil {
		t.Fatal("control-checkout write was accepted")
	}
	if !violated {
		t.Fatal("workspace violation did not trigger cancellation")
	}
}

func TestWorkspaceToolControllerDeniesBeforeProcessExecution(t *testing.T) {
	control := t.TempDir()
	execution := filepath.Join(t.TempDir(), "execution")
	artifacts := filepath.Join(control, ".takt", "runs", "run-1", "artifacts")
	controller := workspaceToolController{policy: assistant.Policy{}, workspace: execution, artifacts: artifacts}
	decision, err := controller.Decide(context.Background(), assistant.ToolRequest{Tool: "write", Input: json.RawMessage(`{"path":"` + filepath.Join(control, "main.go") + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestAssistantEventCollectorRejectsToolRequestWithoutPath(t *testing.T) {
	collector := newAssistantEventCollector(t.TempDir(), t.TempDir())
	collector.Emit(assistant.Event{Type: assistant.EventToolRequested, Tool: "write", CallID: "call-1", Input: json.RawMessage(`{"content":"x"}`)})
	if _, err := collector.Result(); err == nil {
		t.Fatal("write request without path was accepted")
	}
}

func TestAssistantEventCollectorAllowsArtifactWriteOutsideExecutionWorkspace(t *testing.T) {
	control := t.TempDir()
	execution := filepath.Join(t.TempDir(), "execution")
	artifacts := filepath.Join(control, ".takt", "runs", "run-1", "artifacts")
	collector := newAssistantEventCollector(execution, artifacts)
	collector.Emit(assistant.Event{Type: assistant.EventToolStarted, Tool: "write", CallID: "call-1", Input: json.RawMessage(`{"path":"` + filepath.Join(artifacts, "implementation.md") + `"}`)})
	if _, err := collector.Result(); err != nil {
		t.Fatalf("artifact write rejected: %v", err)
	}
}

func TestAssistantEventCollectorRejectsSymlinkEscape(t *testing.T) {
	control := t.TempDir()
	execution := filepath.Join(t.TempDir(), "execution")
	if err := os.MkdirAll(execution, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(control, filepath.Join(execution, "escape")); err != nil {
		t.Fatal(err)
	}
	collector := newAssistantEventCollector(execution, filepath.Join(control, ".takt", "runs", "run-1", "artifacts"))
	collector.Emit(assistant.Event{Type: assistant.EventToolStarted, Tool: "edit", CallID: "call-1", Input: json.RawMessage(`{"path":"escape/main.go"}`)})
	if _, err := collector.Result(); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}
