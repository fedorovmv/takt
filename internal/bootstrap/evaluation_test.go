package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/store"
	"takt/internal/tooling/evaluation"
)

func TestFlowEvaluationTraceReportsDurableEvents(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-trace\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var trace []string
	_, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{
		Workspace: workspace, Selector: workflow, ConfigPath: config,
		Trace: func(line string) { trace = append(trace, line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(trace, "\n")
	for _, want := range []string{"run.accepted", "run.started", "node.started node=done", "node.completed node=done", "run.completed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestTraceFlowRunningNodesReportsCurrentDurableStatus(t *testing.T) {
	state := &store.RunState{Nodes: map[string]*store.NodeState{
		"pending": {Status: store.NodePending},
		"active":  {Status: store.NodeRunning},
	}}
	var trace []string
	traceFlowRunningNodes(func(line string) { trace = append(trace, line) }, state)
	state.Nodes["active"].Attempts = 2
	traceFlowRunningNodes(func(line string) { trace = append(trace, line) }, state)
	if got, want := strings.Join(trace, "\n"), "node.active node=active attempt=0 awaiting=assistant_progress_or_completion\nnode.active node=active attempt=2 awaiting=assistant_progress_or_completion"; got != want {
		t.Fatalf("trace=%q want=%q", got, want)
	}
}

func TestTraceFlowChildFailuresFromSnapshot(t *testing.T) {
	states := []*store.RunState{
		{ID: "root", Status: store.RunFailed},
		{ID: "child-b", ParentRunID: "root", Status: store.RunCancelled, ErrorCode: "cancelled", Nodes: map[string]*store.NodeState{"review": {Status: store.NodeCancelled, ErrorCode: "cancelled"}}},
		{ID: "child-a", ParentRunID: "root", Status: store.RunFailed, ErrorCode: "protocol", Nodes: map[string]*store.NodeState{"review": {Status: store.NodeErrored, ErrorCode: "protocol"}}},
	}
	var trace []string
	traceFlowChildSnapshot(func(line string) { trace = append(trace, line) }, states)
	joined := strings.Join(trace, "\n")
	for _, want := range []string{
		"child_run.failed run=child-a parent=root code=protocol",
		"child_node.errored run=child-a node=review code=protocol",
		"child_run.cancelled run=child-b parent=root code=cancelled",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestTraceEvaluationAssistantEventShowsSafeProgress(t *testing.T) {
	var trace []string
	write := func(line string) { trace = append(trace, line) }
	traceEvaluationAssistantEvent(write, "child-1", "implement", assistant.Event{Type: assistant.EventToolStarted, Tool: "read", Input: json.RawMessage(`{"path":"internal/du/du.go"}`)})
	traceEvaluationAssistantEvent(write, "child-1", "implement", assistant.Event{Type: assistant.EventMessage, Message: strings.Repeat("result ", 40)})
	joined := strings.Join(trace, "\n")
	for _, want := range []string{`assistant.tool.started run=child-1 node=implement tool=read model= session= path="internal/du/du.go"`, `assistant.message run=child-1 node=implement model= session= text="result result`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
	if len(trace[1]) > 260 {
		t.Fatalf("message preview is too large: %d", len(trace[1]))
	}
}

func TestTraceEvaluationAssistantFailureShowsIdleTimeout(t *testing.T) {
	var trace []string
	traceEvaluationAssistantEvent(func(line string) { trace = append(trace, line) }, "run-1", "review", assistant.Event{
		Type: assistant.EventFailed, Message: "assistant idle timeout: node idle timeout exceeded", SessionID: "session-1",
	})
	if got := strings.Join(trace, "\n"); !strings.Contains(got, `assistant.failed run=run-1 node=review session=session-1 error="assistant idle timeout: node idle timeout exceeded"`) {
		t.Fatalf("trace=%q", got)
	}
}

func TestFlowEvaluationCaseReturnsDetachedSnapshotAndDefersCleanup(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-case\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{Workspace: workspace, Selector: workflow, ConfigPath: config})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.States) != 1 || result.States[0].Status != "completed" || len(result.Events) == 0 || result.Cleanup == nil {
		t.Fatalf("result=%#v", result)
	}
	if _, err := result.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFlowEvaluationCaseAnswersWaitingApproval(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-approval\nnodes:\n  - id: approve\n    approval:\n      message: Continue?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{Workspace: workspace, Selector: workflow, ConfigPath: config, ApprovalAnswer: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.States) != 1 || result.States[0].Status != "completed" || result.States[0].Approvals["approve"] != "yes" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowEvaluationCancellationDuringAnswerReturnsSnapshot(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-cancel\nnodes:\n  - id: approve\n    approval:\n      message: Continue?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := New(workspace, config)
	if err != nil {
		t.Fatal(err)
	}
	started, err := app.Core.RunService.Start(context.Background(), application.StartRequest{Selector: workflow, ConfigPath: config, Detached: true, KeepWorktree: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		state, loadErr := app.Core.RunService.GetRun(started.RunID)
		if loadErr == nil && state.Status == store.RunWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not reach waiting: %v", loadErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (evaluationEngine{}).pollFlowCase(ctx, app, started.RunID, "yes", nil)
	if err != context.Canceled || !result.ContextCancelled || len(result.States) == 0 || result.States[0].Status != store.RunCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
