package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestAnswerValidatesDefinitionsBeforeConsumingApproval(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	workflowV1 := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval
nodes:
  - id: approve
    approval:
      message: Continue?
      capture_response: true
`
	workflowV2 := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval
nodes:
  - id: approve
    approval:
      message: Changed definition
      capture_response: true
`
	config := `apiVersion: takt/v1alpha1
kind: Config
models: {}
assistants: {}
`
	if err := os.WriteFile(workflowPath, []byte(workflowV1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := runtime.New(wf, cfg, workflowPath, configPath, dir)
	state, err := runner.Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	if err := os.WriteFile(workflowPath, []byte(workflowV2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := answerCmd([]string{state.ID, "approve", "--value", "yes", "--workspace", dir}); err == nil {
		t.Fatal("expected definition change error")
	}
	loaded, err := (store.FS{Workspace: dir}).Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != store.RunWaiting || loaded.Waiting == nil {
		t.Fatalf("approval was consumed: %+v", loaded)
	}
	if _, ok := loaded.Approvals["approve"]; ok {
		t.Fatalf("approval answer was persisted despite failed validation: %+v", loaded.Approvals)
	}
}

func TestJSONModeDefaults(t *testing.T) {
	if !wantsJSON([]string{"run", "workflow.yaml"}) {
		t.Fatal("run should default to JSON")
	}
	if wantsJSON([]string{"run", "workflow.yaml", "--json=false"}) {
		t.Fatal("--json=false should disable JSON")
	}
	if wantsJSON([]string{"validate", "workflow.yaml"}) {
		t.Fatal("validate should default to text")
	}
	if !wantsJSON([]string{"eval", "report", ".takt/evals/latest"}) {
		t.Fatal("eval should default to JSON")
	}
}
