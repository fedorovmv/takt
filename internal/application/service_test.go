package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/dynamicplan"
	"takt/internal/store"
)

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
	mustWriteControlTest(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: answer-redaction
defaults:
  assistant: worker
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
	started, err := service.Start(context.Background(), StartRequest{Selector: workflowPath})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.Answer(context.Background(), started.RunID, "approve", secret)
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

func TestSavePlanRecordRedactsRunConfigSecrets(t *testing.T) {
	workspace := t.TempDir()
	defaultConfig := filepath.Join(workspace, "default.yaml")
	runConfig := filepath.Join(workspace, "run.yaml")
	const envName = "TAKT_PLAN_REDACTION_VALUE"
	const secret = "plan-secret-42"
	t.Setenv(envName, secret)
	mustWriteControlTest(t, defaultConfig, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, runConfig, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
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
	record := &dynamicplan.Record{
		ID:         "plan-redaction-test",
		Status:     "running",
		Profile:    "code",
		ConfigPath: runConfig,
		Results:    map[string]string{"inspect": `{"summary":"` + secret + `"}`},
		LastError:  "failed with " + secret,
		Steering:   []dynamicplan.Steering{{Message: "keep " + secret}},
		Revisions:  []dynamicplan.Revision{},
	}
	if err := service.PlanService.savePlanRecord(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := (dynamicplan.Store{Workspace: workspace}).Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(mustJSONTest(t, loaded))
	if strings.Contains(raw, secret) {
		t.Fatalf("plan record leaked secret: %s", raw)
	}
	if loaded.LastError != "failed with <redacted>" || loaded.Steering[0].Message != "keep <redacted>" || !strings.Contains(loaded.Results["inspect"], "<redacted>") {
		t.Fatalf("plan record was not redacted: %#v", loaded)
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
	if err := service.RunService.commitRedacted(store.FS{Workspace: workspace}, state, store.Event{Type: "test", Data: map[string]any{"value": secret}}); err != nil {
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
	err = service.RunService.commitRedacted(store.FS{Workspace: workspace}, state, store.Event{Type: "test"})
	if err == nil || !strings.Contains(err.Error(), "load persistence redaction config") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(workspace, ".takt", "runs", state.ID, "state.json")); !os.IsNotExist(statErr) {
		t.Fatalf("state persisted despite missing redaction config: %v", statErr)
	}
}
