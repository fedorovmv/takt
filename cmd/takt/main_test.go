package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestAnswerAcceptsPublicSubworkflowNodeID(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	childPath := filepath.Join(dir, "child.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(childPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: approve
    approval:
      message: Continue?
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
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
	if err := answerCmd([]string{state.ID, "child", "--value", "yes", "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := (store.FS{Workspace: dir}).Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != store.RunCompleted || loaded.Approvals["child__approve"] != "yes" {
		t.Fatalf("public approval alias was not resolved: %+v", loaded)
	}
}

func TestAnswerResumesManagedWorktreeAndManualRemoveCleansIt(t *testing.T) {
	repo := t.TempDir()
	mainGit(t, repo, "init")
	mainGit(t, repo, "config", "user.email", "takt@example.invalid")
	mainGit(t, repo, "config", "user.name", "Takt Test")
	workflowPath := filepath.Join(repo, "workflow.yaml")
	configPath := filepath.Join(repo, "config.yaml")
	workflowText := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval-worktree
worktree:
  enabled: true
  cleanup: manual
nodes:
  - id: approve
    approval:
      message: Continue?
      capture_response: true
  - id: write
    depends_on: [approve]
    bash: printf '%s' "${nodes.approve.output}" > answer.txt
`
	if err := os.WriteFile(workflowPath, []byte(workflowText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mainGit(t, repo, "add", ".")
	mainGit(t, repo, "commit", "-m", "definitions")
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := runtime.New(wf, cfg, workflowPath, configPath, repo).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting run, got state=%+v err=%v", state, err)
	}
	worktreePath := state.Worktree.Path
	if err := answerCmd([]string{state.ID, "approve", "--value", "yes", "--workspace", repo, "--json=false"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := (store.FS{Workspace: repo}).Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != store.RunCompleted || loaded.Worktree == nil || loaded.Worktree.Removed {
		t.Fatalf("worktree run was not retained after resume: %+v", loaded)
	}
	answer, err := os.ReadFile(filepath.Join(loaded.ExecutionWorkspace, "answer.txt"))
	if err != nil || string(answer) != "yes" {
		t.Fatalf("answer was not written in worktree: %q err=%v", answer, err)
	}
	if err := worktreeRemoveCmd([]string{state.ID, "--workspace", repo, "--force", "--json=false"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = (store.FS{Workspace: repo}).Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Worktree.Removed {
		t.Fatalf("worktree was not marked removed: %+v", loaded.Worktree)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
}

func mainGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
