package dynamicflow

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"takt/internal/experimental/dynamicplan"
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
	service, err := newTestServices(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath})
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
	view, err := service.TaskService.RespondTask(context.Background(), TaskRespondRequest{Reference: record.ID, Action: "answer", Message: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "completed" || view.NeedsInput {
		t.Fatalf("view = %#v", view)
	}
	run, err := service.RunService.GetRun(started.RunID)
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
	service, err := newTestServices(workspace, configPath)
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
	view, err := service.TaskService.StopTask(context.Background(), TaskStopRequest{Reference: record.ID, Reason: "stop test"})
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
	service := &TaskService{}
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
	service, err := newTestServices(workspace, configPath)
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
	view, err := service.TaskService.TaskStatus(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "parked" || !view.NeedsInput || view.Plan == nil || view.Plan.Record.LastError != "choose a safe path" {
		t.Fatalf("view = %#v", view)
	}
	if _, err := service.TaskService.RespondTask(context.Background(), TaskRespondRequest{Reference: record.ID, Action: "continue"}); err == nil || !strings.Contains(err.Error(), "parked") {
		t.Fatalf("continue on parked plan error = %v", err)
	}
}

func TestResolveTaskSourceAtControlBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process fixture uses POSIX shell")
	}
	workspace := t.TempDir()
	adapterPath := filepath.Join(workspace, "fixture-task-source")
	if err := os.WriteFile(adapterPath, []byte(`#!/bin/sh
read req
printf '%s\n' '{"apiVersion":"takt-task-source/v1alpha1","kind":"ResolveResponse","task":{"apiVersion":"takt-task-source/v1alpha1","kind":"Task","id":"issue-42","title":"Fix the issue","goal":"Repair route handling","description":"Keep structured context","acceptance":["route passes"],"source":{"adapter":"untrusted-adapter-name","kind":"fixture.issue","reference":"ACME-42","revision":"sha256:immutable"}}}'
`), 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\ntask_sources:\n  fixture:\n    transport: process\n    argv: ["+adapterPath+"]\n")
	service, err := newTestServices(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	goal, task, err := service.TaskService.resolveTaskStart(context.Background(), TaskStartRequest{Source: "fixture", SourceRef: "ACME-42"})
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.Source.Adapter != "fixture" || task.Source.Reference != "ACME-42" || task.Source.Revision != "sha256:immutable" {
		t.Fatalf("task source boundary lost provenance: %#v", task)
	}
	if !strings.Contains(goal, "Repair route handling") || !strings.Contains(goal, "route passes") || !strings.Contains(goal, "sha256:immutable") {
		t.Fatalf("normalized goal lost structured task fields: %q", goal)
	}
}
