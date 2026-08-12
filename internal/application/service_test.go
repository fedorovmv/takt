package application

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/runcontrol"
	"takt/internal/store"
)

func TestEvaluationSnapshotIncludesDurableRunTreeWithoutSharingStoreState(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	now := time.Now().UTC()
	child := &store.RunState{ID: "snapshot-child", Status: store.RunCompleted, ConfigPath: configPath, Workspace: workspace, CreatedAt: now, UpdatedAt: now, Nodes: map[string]*store.NodeState{"compiled": {Status: store.NodeCompleted, External: &store.ExternalExecutionState{ClaimToken: "secret-claim"}}}, Approvals: map[string]string{}, Artifacts: []store.ArtifactRef{{ID: "child-artifact", ProducerRunID: "snapshot-child"}}}
	root := &store.RunState{ID: "snapshot-root", Status: store.RunCompleted, ConfigPath: configPath, Workspace: workspace, CreatedAt: now, UpdatedAt: now, Nodes: map[string]*store.NodeState{"visible": {Status: store.NodeCompleted}}, Approvals: map[string]string{}, ChildRunIDs: []string{child.ID}, Artifacts: []store.ArtifactRef{{ID: "root-artifact", ProducerRunID: "snapshot-root"}}}
	fs := store.FS{Workspace: workspace}
	if err := fs.Save(child); err != nil {
		t.Fatal(err)
	}
	if err := fs.Save(root); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1001; i++ {
		if err := fs.Commit(root, store.Event{Type: "snapshot", RunID: root.ID}); err != nil {
			t.Fatal(err)
		}
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	public, err := service.RunService.GetRun(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(public.ChildRunIDs) != 1 {
		t.Fatalf("public child ids = %#v", public.ChildRunIDs)
	}
	snapshot, err := service.RunService.EvaluationSnapshot(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Root != snapshot.States[0] || len(snapshot.States) != 2 || len(snapshot.Events) != 1001 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if snapshot.States[1].Nodes["compiled"].External.ClaimToken != "" {
		t.Fatalf("claim token leaked: %#v", snapshot.States[1].Nodes["compiled"].External)
	}
	if got := snapshot.Artifacts[0].ID; got != "child-artifact" {
		t.Fatalf("artifacts not sorted: %#v", snapshot.Artifacts)
	}
	if snapshot.ArtifactDirs[root.ID] != fs.ArtifactsDir(root.ID) || snapshot.ArtifactDirs[child.ID] != fs.ArtifactsDir(child.ID) {
		t.Fatalf("artifact dirs = %#v", snapshot.ArtifactDirs)
	}
	snapshot.States[0].Nodes["visible"].Status = store.NodeFailed
	snapshot.Events[0].Data = map[string]any{"changed": true}
	loaded, err := fs.Load(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Nodes["visible"].Status != store.NodeCompleted {
		t.Fatalf("snapshot aliases stored state")
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerRedactsKnownSecretBeforePersistence(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	const envName = "TAKT_APPROVAL_REDACTION_VALUE"
	const secret = "approval-secret-42"
	t.Setenv(envName, secret)
	mustWriteControlTest(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
    env:
      TOKEN: secret://`+envName+`
`)
	mustWriteControlTest(t, workflowPath, `name: answer-redaction
provider: worker
model: demo
nodes:
  - id: approve
    approval:
      message: enter value
`)
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.RunService.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.RunService.Answer(context.Background(), started.RunID, "approve", secret)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mustJSONTest(t, final)), secret) {
		t.Fatalf("answer leaked in public state: %s", mustJSONTest(t, final))
	}
	fs := store.FS{Workspace: workspace}
	persisted, err := fs.Load(started.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if got := persisted.Approvals["approve"]; got != "<redacted>" {
		t.Fatalf("persisted approval=%q", got)
	}
}

func TestCommitRedactedUsesRunSpecificConfig(t *testing.T) {
	workspace := t.TempDir()
	defaultConfig := filepath.Join(workspace, "default.yaml")
	runConfig := filepath.Join(workspace, "run.yaml")
	const envName = "TAKT_RUN_SPECIFIC_REDACTION_VALUE"
	const secret = "run-specific-secret-50"
	t.Setenv(envName, secret)
	mustWriteControlTest(t, defaultConfig, `apiVersion: takt/v1alpha1
kind: Config
models: {}
assistants: {}
`)
	mustWriteControlTest(t, runConfig, `apiVersion: takt/v1alpha1
kind: Config
models: {}
assistants:
  worker:
    type: mock
    env:
      VALUE: secret://`+envName+`
`)
	service, err := New(workspace, defaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	state := &store.RunState{ID: "run-specific-config", Status: store.RunRunning, ConfigPath: runConfig, Workspace: workspace, ExecutionWorkspace: workspace, Output: secret, Nodes: map[string]*store.NodeState{"n": {Status: store.NodeCompleted, Output: secret}}, Approvals: map[string]string{}}
	if err := runcontrol.CommitRedacted(service.ConfigPath, service.RunService.store, state, store.Event{Type: "test", Data: map[string]any{"value": secret}}); err != nil {
		t.Fatal(err)
	}
	persisted, err := (store.FS{Workspace: workspace}).Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := (store.FS{Workspace: workspace}).ReadEvents(state.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(mustJSONTest(t, struct {
		State  *store.RunState
		Events []store.Event
	}{persisted, events}))
	if strings.Contains(raw, secret) {
		t.Fatalf("run-specific config secret leaked: %s", raw)
	}
}

func TestCommitRedactedFailsClosedWhenRunConfigCannotLoad(t *testing.T) {
	workspace := t.TempDir()
	service, err := New(workspace, filepath.Join(workspace, "default.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	state := &store.RunState{ID: "run-redact-missing-config", Status: store.RunRunning, ConfigPath: filepath.Join(workspace, "missing.yaml"), Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
	err = runcontrol.CommitRedacted(service.ConfigPath, service.RunService.store, state, store.Event{Type: "test"})
	if err == nil || !strings.Contains(err.Error(), "load persistence redaction config") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".takt", "runs", state.ID, "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("state persisted despite missing redaction config: %v", statErr)
	}
}
