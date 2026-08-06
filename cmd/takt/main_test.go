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

func TestAnswerForGovernedChildResumesRootRun(t *testing.T) {
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
      message: Continue child?
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s' '${nodes.approve.output}'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      output_node: done
  - id: finish
    depends_on: [child]
    bash: printf 'root:%s' '${nodes.child.output}'
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
	state, err := runtime.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected governed child waiting, got %v", err)
	}
	if err := answerCmd([]string{state.ID, "child", "--value", "yes", "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
	st := store.FS{Workspace: dir}
	root, err := st.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if root.Status != store.RunCompleted || root.Output != "root:yes" {
		t.Fatalf("root was not resumed to completion: %+v", root)
	}
	if len(root.ChildRunIDs) != 1 {
		t.Fatalf("unexpected child links: %+v", root.ChildRunIDs)
	}
	child, err := st.Load(root.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != store.RunCompleted || child.Approvals["approve"] != "yes" {
		t.Fatalf("child approval was not consumed: %+v", child)
	}
	if err := childrenCmd([]string{root.ID, "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelWaitingGovernedTree(t *testing.T) {
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
      message: wait
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, _ := workflow.Load(workflowPath)
	cfg, _ := cfgpkg.Load(configPath)
	state, err := runtime.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	childID := state.Waiting.ChildRunID
	if err := cancelCmd([]string{state.ID, "--workspace", dir, "--reason", "stop", "--json=false"}); err != nil {
		t.Fatal(err)
	}
	st := store.FS{Workspace: dir}
	root, err := st.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	if root.Status != store.RunCancelled {
		t.Fatalf("root was not cancelled: %+v", root)
	}
	child, err := st.Load(childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != store.RunCancelled {
		t.Fatalf("child was not cancelled: %+v", child)
	}
}

func TestCancelCommandRejectsFailedRun(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: failing
nodes:
  - id: fail
    bash: exit 7
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, _ := workflow.Load(workflowPath)
	cfg, _ := cfgpkg.Load(configPath)
	state, err := runtime.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if err == nil || state.Status != store.RunFailed {
		t.Fatalf("expected failed run: state=%+v err=%v", state, err)
	}
	cancelErr := cancelCmd([]string{state.ID, "--workspace", dir, "--json=false"})
	if cancelErr == nil || !strings.Contains(cancelErr.Error(), "cannot cancel terminal run") {
		t.Fatalf("terminal cancel was not rejected: %v", cancelErr)
	}
	loaded, loadErr := (store.FS{Workspace: dir}).Load(state.ID)
	if loadErr != nil || loaded.Status != store.RunFailed {
		t.Fatalf("failed status changed: state=%+v err=%v", loaded, loadErr)
	}
}

func TestRunCmdResolvesLogicalCodingAgentThroughDefaultAssistant(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: logical-coding-agent
nodes:
  - id: work
    assistant: coding-agent
    model: demo
    prompt: complete the fixture
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`apiVersion: takt/v1alpha1
kind: Config
default_assistant: fixture
models:
  demo:
    provider: fixture
    id: demo
assistants:
  fixture:
    type: mock
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd([]string{workflowPath, "--config", configPath, "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
}
