package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/application"
	"takt/internal/store"
	"takt/internal/tooling/evaluation"
)

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
	result, err := (evaluationEngine{}).pollFlowCase(ctx, app, started.RunID, "yes")
	if err != context.Canceled || !result.ContextCancelled || len(result.States) == 0 || result.States[0].Status != store.RunCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
