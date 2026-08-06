package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/store"
)

func TestDetachedStartAcceptsImmediateDurableFailure(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: immediate-failure
nodes:
  - id: fail
    bash: exit 17
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatalf("detached durable failure must be accepted: %v", err)
	}
	if !started.Accepted || started.RunID == "" {
		t.Fatalf("unexpected start result: %#v", started)
	}
	state := waitRunStatus(t, service, started.RunID, store.RunFailed, 3*time.Second)
	if state.Nodes["fail"].Status != store.NodeFailed || state.Error == "" {
		t.Fatalf("terminal failure was not persisted: %#v", state)
	}
}

func TestRunOperationsListAttentionSummaryPauseAndResume(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: operator-view
nodes:
  - id: approve
    approval:
      message: Continue?
`)
	now := time.Now().UTC()
	state := &store.RunState{ID: "run-operations-view", Status: store.RunWaiting, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Input: "input", Nodes: map[string]*store.NodeState{"approve": {Status: store.NodeWaiting}}, Approvals: map[string]string{}, Waiting: &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Continue?"}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := service.ListRuns(RunListRequest{AttentionOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Attention.Reason != "approval" {
		t.Fatalf("attention list = %#v", listed)
	}
	summary, err := service.Summary(state.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != store.RunWaiting || summary.NodesWaiting != 1 || !summary.Attention.Required {
		t.Fatalf("summary = %#v", summary)
	}
	paused, err := service.Pause(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != store.RunWaiting && paused.Status != store.RunPaused {
		t.Fatalf("pause result = %#v", paused)
	}
	loaded, err := st.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != store.RunPaused || loaded.PausedFrom != store.RunWaiting {
		t.Fatalf("paused state = %#v", loaded)
	}
	resumed, err := service.ResumePaused(context.Background(), state.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != store.RunWaiting || resumed.Waiting == nil {
		t.Fatalf("resumed state = %#v", resumed)
	}
}

func TestSafePauseStopsAtNodeBoundary(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: safe-pause
nodes:
  - id: first
    bash: sleep 0.4
  - id: second
    depends_on: [first]
    bash: printf done > second.txt
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := service.Pause(started.RunID); err != nil {
		t.Fatal(err)
	}
	state := waitRunStatus(t, service, started.RunID, store.RunPaused, 4*time.Second)
	if state.Nodes["first"].Status != store.NodeCompleted || state.Nodes["second"].Status != store.NodePending {
		t.Fatalf("pause did not stop at boundary: %#v", state.Nodes)
	}
	if _, err := os.Stat(filepath.Join(workspace, "second.txt")); !os.IsNotExist(err) {
		t.Fatalf("second node ran before resume: %v", err)
	}
	if _, err := service.ResumePaused(context.Background(), started.RunID, false); err != nil {
		t.Fatal(err)
	}
	completed := waitRunStatus(t, service, started.RunID, store.RunCompleted, 4*time.Second)
	if completed.Status != store.RunCompleted {
		t.Fatalf("resume status = %s", completed.Status)
	}
	if _, err := os.Stat(filepath.Join(workspace, "second.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestRetryAndRecoverInterruptedRun(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry-recover
nodes:
  - id: build
    bash: "true"
  - id: verify
    depends_on: [build]
    bash: "true"
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	st := store.FS{Workspace: workspace}
	now := time.Now().UTC()
	failed := &store.RunState{ID: "run-operator-retry", Status: store.RunFailed, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"build": {Status: store.NodeFailed, Attempts: 1, Error: "boom"}, "verify": {Status: store.NodeBlocked}}, Approvals: map[string]string{}, CurrentNode: "build", Error: "boom", CreatedAt: now, UpdatedAt: now}
	if err := st.Save(failed); err != nil {
		t.Fatal(err)
	}
	retried, err := service.Retry(context.Background(), RetryRequest{RunID: failed.ID})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != store.RunCompleted {
		t.Fatalf("retry status = %s error=%s", retried.Status, retried.Error)
	}
	if len(retried.OperatorRetries) != 2 {
		t.Fatalf("retry history = %#v", retried.OperatorRetries)
	}

	lost := &store.RunState{ID: "run-worker-lost", Status: store.RunRunning, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"build": {Status: store.NodeRunning, Attempts: 1}, "verify": {Status: store.NodePending}}, Approvals: map[string]string{}, CurrentNode: "build", CurrentNodes: []string{"build"}, ExecutorPID: 99999999, CreatedAt: now, UpdatedAt: now}
	if err := st.Save(lost); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered.Recovered) != 1 || recovered.Recovered[0] != lost.ID {
		t.Fatalf("recovery = %#v", recovered)
	}
	final := waitRunStatus(t, service, lost.ID, store.RunCompleted, 4*time.Second)
	if final.RecoveryCount != 1 {
		t.Fatalf("recovery count = %d", final.RecoveryCount)
	}
}

func waitRunStatus(t *testing.T, service *Service, runID, status string, timeout time.Duration) *store.RunState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := service.GetRun(runID)
		if err == nil && state.Status == status {
			return state
		}
		time.Sleep(25 * time.Millisecond)
	}
	state, err := service.GetRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("run %s status = %s, want %s; error=%s", runID, state.Status, status, state.Error)
	return nil
}

func writeControlFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicPlanTracksPauseResumeAndAbandon(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: dynamic-operator
nodes:
  - id: approve
    approval:
      message: Continue?
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := &store.RunState{ID: "run-dynamic-operator", Status: store.RunWaiting, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"approve": {Status: store.NodeWaiting}}, Approvals: map[string]string{}, Waiting: &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Continue?"}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(run); err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	record := &dynamicplan.Record{ID: "plan-operator123456", Status: "running", Profile: "code", ConfigPath: configPath, CurrentRunID: run.ID, ExecutionRunIDs: []string{run.ID}, Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	planStore := dynamicplan.Store{Workspace: workspace}
	if err := planStore.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Pause(run.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "paused" {
		t.Fatalf("plan status after pause = %q", loaded.Status)
	}
	if _, err := service.ResumePaused(context.Background(), run.ID, false); err != nil {
		t.Fatal(err)
	}
	loaded, err = planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "waiting" {
		t.Fatalf("plan status after waiting resume = %q", loaded.Status)
	}
	if _, err := service.Abandon(run.ID, "operator stopped campaign"); err != nil {
		t.Fatal(err)
	}
	loaded, err = planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "abandoned" {
		t.Fatalf("plan status after abandon = %q", loaded.Status)
	}
	state, err := service.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunAbandoned {
		t.Fatalf("run status after abandon = %q", state.Status)
	}
}

func TestSafePausePropagatesThroughGovernedChildRun(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	childPath := filepath.Join(workspace, "child.yaml")
	parentPath := filepath.Join(workspace, "parent.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: pause-child
nodes:
  - id: first
    bash: sleep 0.4
  - id: second
    depends_on: [first]
    bash: printf done > child-second.txt
`)
	writeControlFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: pause-parent
nodes:
  - id: delegated
    workflow:
      path: child.yaml
      isolation: inherit
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Start(context.Background(), StartRequest{Selector: parentPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, loadErr := service.GetRun(started.RunID)
		if loadErr == nil && len(state.ChildRunIDs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child run was not linked: %v", loadErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := service.Pause(started.RunID); err != nil {
		t.Fatal(err)
	}
	parent := waitRunStatus(t, service, started.RunID, store.RunPaused, 4*time.Second)
	if len(parent.ChildRunIDs) != 1 {
		t.Fatalf("parent children = %#v", parent.ChildRunIDs)
	}
	child := waitRunStatus(t, service, parent.ChildRunIDs[0], store.RunPaused, 2*time.Second)
	if child.Nodes["second"].Status != store.NodePending {
		t.Fatalf("child did not pause at boundary: %#v", child.Nodes)
	}
	if _, err := service.ResumePaused(context.Background(), parent.ID, false); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, service, parent.ID, store.RunCompleted, 4*time.Second)
	if _, err := os.Stat(filepath.Join(workspace, "child-second.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestOperatorActionsPrearmMarkersForLinkedChildWithoutState(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: prearm-markers
nodes:
  - id: wait
    bash: sleep 1
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	st := store.FS{Workspace: workspace}
	now := time.Now().UTC()
	pauseChild := "run-linked-pause-child"
	pauseRoot := &store.RunState{
		ID:                 "run-linked-pause-root",
		Status:             store.RunRunning,
		WorkflowPath:       workflowPath,
		ConfigPath:         configPath,
		Workspace:          workspace,
		ExecutionWorkspace: workspace,
		Nodes:              map[string]*store.NodeState{"wait": {Status: store.NodeRunning}},
		ChildRunIDs:        []string{pauseChild},
		Approvals:          map[string]string{},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := st.Save(pauseRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Pause(pauseRoot.ID); err != nil {
		t.Fatal(err)
	}
	paused, err := st.PauseRequested(pauseChild)
	if err != nil {
		t.Fatal(err)
	}
	if !paused {
		t.Fatal("pause marker was not pre-armed for linked child without state")
	}

	abandonChild := "run-linked-abandon-child"
	abandonRoot := &store.RunState{
		ID:                 "run-linked-abandon-root",
		Status:             store.RunRunning,
		WorkflowPath:       workflowPath,
		ConfigPath:         configPath,
		Workspace:          workspace,
		ExecutionWorkspace: workspace,
		Nodes:              map[string]*store.NodeState{"wait": {Status: store.NodeRunning}},
		ChildRunIDs:        []string{abandonChild},
		Approvals:          map[string]string{},
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := st.Save(abandonRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Abandon(abandonRoot.ID, "stop campaign"); err != nil {
		t.Fatal(err)
	}
	abandoned, reason, err := st.AbandonRequested(abandonChild)
	if err != nil {
		t.Fatal(err)
	}
	if !abandoned || reason != "stop campaign" {
		t.Fatalf("abandon marker = %v %q", abandoned, reason)
	}
}
