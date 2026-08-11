package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
)

func TestDetachedStartAcceptsImmediateDurableFailure(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `name: immediate-failure
nodes:
  - id: fail
    bash: exit 17
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Detached: true})
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
	writeControlFile(t, workflowPath, `name: operator-view
nodes:
  - id: approve
    approval:
      message: Continue?
`)
	now := time.Now().UTC()
	state := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-operations-view", Status: store.RunWaiting, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Input: "input", Nodes: map[string]*store.NodeState{"approve": {Status: store.NodeWaiting}}, Approvals: map[string]string{}, Waiting: &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Continue?"}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := service.RunService.ListRuns(RunListRequest{AttentionOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Attention.Reason != "approval" {
		t.Fatalf("attention list = %#v", listed)
	}
	summary, err := service.RunService.Summary(state.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != store.RunWaiting || summary.NodesWaiting != 1 || !summary.Attention.Required {
		t.Fatalf("summary = %#v", summary)
	}
	paused, err := service.RunService.Pause(context.Background(), state.ID)
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
	resumed, err := service.RunService.ResumePaused(context.Background(), state.ID, false)
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
	writeControlFile(t, workflowPath, `name: safe-pause
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
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := service.RunService.Pause(context.Background(), started.RunID); err != nil {
		t.Fatal(err)
	}
	state := waitRunStatus(t, service, started.RunID, store.RunPaused, 4*time.Second)
	if state.Nodes["first"].Status != store.NodeCompleted || state.Nodes["second"].Status != store.NodePending {
		t.Fatalf("pause did not stop at boundary: %#v", state.Nodes)
	}
	if _, err := os.Stat(filepath.Join(workspace, "second.txt")); !os.IsNotExist(err) {
		t.Fatalf("second node ran before resume: %v", err)
	}
	if _, err := service.RunService.ResumePaused(context.Background(), started.RunID, false); err != nil {
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
	writeControlFile(t, workflowPath, `name: retry-recover
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
	failed := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-operator-retry", Status: store.RunFailed, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"build": {Status: store.NodeFailed, Attempts: 1, Error: "boom"}, "verify": {Status: store.NodeBlocked}}, Approvals: map[string]string{}, CurrentNode: "build", Error: "boom", CreatedAt: now, UpdatedAt: now}
	if err := st.Save(failed); err != nil {
		t.Fatal(err)
	}
	retried, err := service.RunService.Retry(context.Background(), RetryRequest{RunID: failed.ID})
	if err != nil {
		t.Fatal(err)
	}
	if retried.Status != store.RunCompleted {
		t.Fatalf("retry status = %s error=%s", retried.Status, retried.Error)
	}
	if len(retried.OperatorRetries) != 2 {
		t.Fatalf("retry history = %#v", retried.OperatorRetries)
	}

	lost := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-worker-lost", Status: store.RunRunning, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"build": {Status: store.NodeRunning, Attempts: 1}, "verify": {Status: store.NodePending}}, Approvals: map[string]string{}, CurrentNode: "build", CurrentNodes: []string{"build"}, ExecutorPID: 99999999, CreatedAt: now, UpdatedAt: now}
	if err := st.Save(lost); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.RunService.RecoverInterruptedRuns(context.Background())
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

func waitRunStatus(t *testing.T, service *Services, runID, status string, timeout time.Duration) *store.RunState {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := service.RunService.GetRun(runID)
		if err == nil && state.Status == status {
			return state
		}
		time.Sleep(25 * time.Millisecond)
	}
	state, err := service.RunService.GetRun(runID)
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

func TestSafePausePropagatesThroughGovernedChildRun(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	childPath := filepath.Join(workspace, "child.yaml")
	parentPath := filepath.Join(workspace, "parent.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, childPath, `name: pause-child
nodes:
  - id: first
    bash: sleep 0.4
  - id: second
    depends_on: [first]
    bash: printf done > child-second.txt
`)
	writeControlFile(t, parentPath, `name: pause-parent
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
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: parentPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, loadErr := service.RunService.GetRun(started.RunID)
		if loadErr == nil && len(state.ChildRunIDs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child run was not linked: %v", loadErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := service.RunService.Pause(context.Background(), started.RunID); err != nil {
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
	if _, err := service.RunService.ResumePaused(context.Background(), parent.ID, false); err != nil {
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
	writeControlFile(t, workflowPath, `name: prearm-markers
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
	pauseRoot := &store.RunState{WorkflowContract: store.CurrentWorkflowContract,
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
	if _, err := service.RunService.Pause(context.Background(), pauseRoot.ID); err != nil {
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
	abandonRoot := &store.RunState{WorkflowContract: store.CurrentWorkflowContract,
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
	if _, err := service.RunService.Abandon(context.Background(), abandonRoot.ID, "stop campaign"); err != nil {
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

func TestResumePausedRejectsNonPausedWithoutClearingMarker(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, "name: marker-preserve\nnodes:\n  - id: x\n    bash: sleep 1\n")
	now := time.Now().UTC()
	state := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-marker-preserve", Status: store.RunRunning, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"x": {Status: store.NodeRunning}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := st.RequestPause(state.ID); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunService.ResumePaused(context.Background(), state.ID, false); err == nil {
		t.Fatal("resume of a non-paused run unexpectedly succeeded")
	}
	requested, err := st.PauseRequested(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !requested {
		t.Fatal("mistaken resume destroyed durable pause marker")
	}
}

func TestPausedParentWaitingForChildStaysPausedAfterChildAnswer(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	childPath := filepath.Join(workspace, "child.yaml")
	parentPath := filepath.Join(workspace, "parent.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, childPath, `name: approval-child
nodes:
  - id: approve
    approval:
      message: Continue child?
`)
	writeControlFile(t, parentPath, `name: approval-parent
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
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: parentPath, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := service.RunService.GetRun(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != store.RunWaiting || parent.Waiting == nil || parent.Waiting.Kind != "child_run" || len(parent.ChildRunIDs) != 1 {
		t.Fatalf("unexpected parent waiting state: %#v", parent)
	}
	childID := parent.ChildRunIDs[0]
	child, err := service.RunService.GetRun(childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Waiting == nil {
		t.Fatalf("child is not waiting: %#v", child)
	}
	if _, err := service.RunService.Pause(context.Background(), parent.ID); err != nil {
		t.Fatal(err)
	}
	paused := waitRunStatus(t, service, parent.ID, store.RunPaused, 2*time.Second)
	if paused.PausedFrom != store.RunWaiting {
		t.Fatalf("paused_from=%q", paused.PausedFrom)
	}
	if _, err := service.RunService.Answer(context.Background(), childID, "approve", "yes"); err != nil {
		t.Fatal(err)
	}
	stillPaused, err := service.RunService.GetRun(parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stillPaused.Status != store.RunPaused || stillPaused.Waiting == nil || stillPaused.Waiting.Kind != "child_run" {
		t.Fatalf("answer implicitly resumed paused parent: %#v", stillPaused)
	}
	if _, err := service.RunService.ResumePaused(context.Background(), parent.ID, false); err != nil {
		t.Fatal(err)
	}
	waitRunStatus(t, service, parent.ID, store.RunCompleted, 2*time.Second)
}

func TestPauseRecheckedBeforeSequentialExternalNodeInSameWave(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	writeControlFile(t, workflowPath, `name: pause-same-wave
nodes:
  - id: first
    bash: sleep 0.35
    attempts:
      max: 2
  - id: delegated
    prompt: do not publish after pause
    executor: external
    provider: worker
    model: demo
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Detached: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := service.RunService.Pause(context.Background(), started.RunID); err != nil {
		t.Fatal(err)
	}
	state := waitRunStatus(t, service, started.RunID, store.RunPaused, 3*time.Second)
	if state.Nodes["first"].Status != store.NodeCompleted {
		t.Fatalf("first=%#v", state.Nodes["first"])
	}
	delegated := state.Nodes["delegated"]
	if delegated == nil || delegated.Status != store.NodePending || delegated.External != nil {
		t.Fatalf("external node became claimable after pause: %#v", delegated)
	}
}

func TestForegroundRecoveryPreservesOperatorPause(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `name: recover-pausing
nodes:
  - id: build
    bash: "true"
`)
	now := time.Now().UTC()
	state := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-recover-pausing", Status: store.RunPausing, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"build": {Status: store.NodeRunning, Attempts: 1}}, Approvals: map[string]string{}, CurrentNode: "build", CurrentNodes: []string{"build"}, ExecutorPID: 99999999, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	if err := st.RequestPause(state.ID); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunService.RecoverInterruptedRunsForeground(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Recovered) != 1 || result.Recovered[0] != state.ID {
		t.Fatalf("result=%#v", result)
	}
	paused, err := service.RunService.GetRun(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != store.RunPaused {
		t.Fatalf("recovery lost pause: %#v", paused)
	}
}

func TestRetryCancelledStartsAtFirstIncompleteAndPreservesExecutionHistory(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `name: retry-cancelled
nodes:
  - id: done
    bash: "true"
  - id: pending
    depends_on: [done]
    bash: "true"
`)
	now := time.Now().UTC()
	oldExec := store.ExecutionState{Attempt: 1, Status: store.NodeFailed, Error: "old failure", ErrorCode: "old"}
	state := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-retry-cancelled", Status: store.RunCancelled, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"done": {Status: store.NodeCompleted, Attempts: 1, Output: "kept"}, "pending": {Status: store.NodeCancelled, Attempts: 1, Error: "cancelled", Executions: []store.ExecutionState{oldExec}}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	st := store.FS{Workspace: workspace}
	if err := st.Save(state); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.RunService.Retry(context.Background(), RetryRequest{RunID: state.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != store.RunCompleted {
		t.Fatalf("result=%#v", result)
	}
	if result.Nodes["done"].Attempts != 1 || result.Nodes["done"].Output != "kept" {
		t.Fatalf("completed branch reran: %#v", result.Nodes["done"])
	}
	if len(result.Nodes["pending"].Executions) < 2 || result.Nodes["pending"].Executions[0] != oldExec {
		t.Fatalf("execution history was lost: %#v", result.Nodes["pending"].Executions)
	}
}

func TestForkPersistsSourceFingerprintAndProvenance(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, "name: fork-source\nnodes:\n  - id: x\n    bash: \"true\"\n")
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath, Input: "source"})
	if err != nil {
		t.Fatal(err)
	}
	forked, err := service.RunService.ForkRun(context.Background(), RunForkRequest{RunID: started.RunID, Input: "fork input"})
	if err != nil {
		t.Fatal(err)
	}
	if forked == nil || forked.RunID == started.RunID {
		t.Fatalf("fork=%#v", forked)
	}
	state, err := service.RunService.GetRun(forked.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ForkedFromRunID != started.RunID || state.ForkSourceFingerprint == "" || state.Input != "fork input" {
		t.Fatalf("fork provenance missing: %#v", state)
	}
}

func TestRecursiveSummaryToleratesLinkedChildBeforeStatePublication(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, "name: summary-race\nnodes:\n  - id: x\n    bash: true\n")
	now := time.Now().UTC()
	state := &store.RunState{WorkflowContract: store.CurrentWorkflowContract, ID: "run-summary-race", Status: store.RunRunning, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"x": {Status: store.NodeCompleted}}, Approvals: map[string]string{}, ChildRunIDs: []string{"run-child-not-published"}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(state); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := service.RunService.Summary(state.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DescendantRuns != 0 || summary.ChildRuns != 1 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestForegroundStartReturnsDurableRedactedState(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	scriptPath := filepath.Join(workspace, "emit-secret.sh")
	const envName = "TAKT_TEST_CONTROL_PUBLIC_SECRET"
	const secret = "control-boundary-secret-441"
	t.Setenv(envName, secret)
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, scriptPath, "#!/bin/sh\nprintf '%s' \"$TOKEN\"\n")
	if err := os.Chmod(scriptPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeControlFile(t, workflowPath, `name: public-redaction
nodes:
  - id: emit
    script:
      runtime: command
      path: emit-secret.sh
      env:
        TOKEN: secret://`+envName+`
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath, ConfigPath: configPath})
	if err != nil {
		t.Fatal(err)
	}
	if started.State == nil || started.State.Nodes["emit"] == nil {
		t.Fatalf("missing public state: %#v", started)
	}
	if got := started.State.Nodes["emit"].Output; got != "<redacted>" {
		t.Fatalf("foreground public output leaked secret: %q", got)
	}
	if strings.Contains(fmt.Sprintf("%#v", started.State), secret) {
		t.Fatal("foreground public state contains secret")
	}
}
