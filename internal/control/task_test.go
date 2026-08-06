package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/profile"
	"takt/internal/store"
)

func TestRespondTaskAnswerRoutesPlanReferenceToWaitingRun(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	workflowPath := filepath.Join(workspace, "question.yaml")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: task-answer
nodes:
  - id: question
    approval:
      message: Which target?
      capture_response: true
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if started.State == nil || started.State.Status != store.RunWaiting {
		t.Fatalf("started = %#v", started)
	}
	now := time.Now().UTC()
	plan := dynamicplan.Plan{APIVersion: dynamicplan.APIVersion, Kind: dynamicplan.Kind, Decision: "existing", Goal: "answer the question", ExistingWorkflow: workflowPath, Reason: "test", Budget: dynamicplan.Budget{MaxChildRuns: 4, MaxParallel: 1, MaxIterations: 3, MaxTokens: 1000}}
	record := &dynamicplan.Record{ID: "plan-taskanswer123", Status: "running", Profile: "code", ConfigPath: configPath, CurrentRunID: started.RunID, ExecutionRunIDs: []string{started.RunID}, Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	view, err := service.RespondTask(context.Background(), TaskRespondRequest{Reference: record.ID, Action: "answer", Message: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "completed" || view.NeedsInput {
		t.Fatalf("view = %#v", view)
	}
	run, err := service.GetRun(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Approvals["question"] != "staging" {
		t.Fatalf("answer was not delivered to waiting run: %#v", run.Approvals)
	}
}

func TestStopTaskReconcilesActivePlanWithoutDaemon(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, workflowPath, "apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: stop\nnodes:\n  - id: x\n    bash: true\n")
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := &store.RunState{ID: "run-task-stop", Status: store.RunRunning, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"x": {Status: store.NodeRunning}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(run); err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	record := &dynamicplan.Record{ID: "plan-taskstop1234", Status: "running", Profile: "code", ConfigPath: configPath, CurrentRunID: run.ID, ExecutionRunIDs: []string{run.ID}, Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	view, err := service.StopTask(TaskStopRequest{Reference: record.ID, Reason: "stop test"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "abandoned" {
		t.Fatalf("view = %#v", view)
	}
	loaded, err := (dynamicplan.Store{Workspace: workspace}).Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "abandoned" || !strings.Contains(loaded.LastError, "stop test") {
		t.Fatalf("plan was not reconciled: %#v", loaded)
	}
}

func TestRespondTaskNeverFabricatesApprovalMessage(t *testing.T) {
	service := &Service{Workspace: t.TempDir()}
	_, err := service.RespondTask(context.Background(), TaskRespondRequest{Reference: "run-missing", Action: "answer"})
	if err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Fatalf("error = %v", err)
	}
}

func TestTaskStatusProjectsParkedPlanAsNeedsInput(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	plan := candidateDynamicPlan()
	record := &dynamicplan.Record{ID: "plan-parked1234", Status: "parked", Profile: "code", ConfigPath: configPath, LastError: "owner decision required", Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	parkPlan(record, "OWNER_DECISION_REQUIRED", "choose a safe path", "owner", "reply with steering", false)
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	view, err := service.TaskStatus(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "parked" || !view.NeedsInput || view.Plan == nil || view.Plan.Record.LastError != "choose a safe path" {
		t.Fatalf("view = %#v", view)
	}
	if _, err := service.RespondTask(context.Background(), TaskRespondRequest{Reference: record.ID, Action: "continue"}); err == nil || !strings.Contains(err.Error(), "parked") {
		t.Fatalf("continue on parked plan error = %v", err)
	}
}
