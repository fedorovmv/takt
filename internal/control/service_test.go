package control

import (
	"context"
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
	if err := service.savePlanRecord(record); err != nil {
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
