package control

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

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
