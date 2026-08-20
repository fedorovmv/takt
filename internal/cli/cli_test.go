package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cfgpkg "takt/internal/config"
	"takt/internal/runtime"
	"takt/internal/store"
	"takt/internal/testsupport/runtimefixture"
	"takt/internal/workflow"
)

func TestAnswerValidatesDefinitionsBeforeConsumingApproval(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	workflowV1 := `name: approval
nodes:
  - id: approve
    approval:
      message: Continue?
      capture_response: true
`
	workflowV2 := `name: approval
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
	runner := runtimefixture.New(wf, cfg, workflowPath, configPath, dir)
	state, err := runner.Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	if err := os.WriteFile(workflowPath, []byte(workflowV2), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := answerCmd(context.Background(), []string{state.ID, "approve", "--value", "yes", "--workspace", dir}); err == nil {
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

func TestValidatePreservesCWDRelativeWorkflowAndConfigPaths(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: cwd-relative\nnodes:\n  - id: ok\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := validateCmd(context.Background(), []string{"workflow.yaml", "--config", "config.yaml", "--workspace", workspace, "--json=false"}); err != nil {
		t.Fatalf("validate cwd-relative path: %v", err)
	}
}

func TestJSONModeDefaults(t *testing.T) {
	if !WantsJSON([]string{"run", "workflow.yaml"}) {
		t.Fatal("run should default to JSON")
	}
	if WantsJSON([]string{"run", "workflow.yaml", "--json=false"}) {
		t.Fatal("--json=false should disable JSON")
	}
	if WantsJSON([]string{"validate", "workflow.yaml"}) {
		t.Fatal("validate should default to text")
	}
	if !WantsJSON([]string{"eval", "report", ".takt/evals/latest"}) {
		t.Fatal("eval should default to JSON")
	}
	if !WantsJSON([]string{"compatibility", "matrix"}) {
		t.Fatal("compatibility should default to JSON")
	}
}

func TestEvalFlowParsesOnlyItsContract(t *testing.T) {
	if err := evalCmd(context.Background(), []string{"flow", "suite.yaml", "--repeat", "0"}); err == nil || !strings.Contains(err.Error(), "repeat must be positive") {
		t.Fatalf("repeat error = %v", err)
	}
	if err := evalCmd(context.Background(), []string{"flow", "init"}); err == nil || !strings.Contains(err.Error(), "usage: takt eval flow init") {
		t.Fatalf("init error = %v", err)
	}
	if err := evalCmd(context.Background(), []string{"flow", "suite.yaml", "--assistant-idle-timeout", "0s"}); err == nil || !strings.Contains(err.Error(), "assistant-idle-timeout must be positive") {
		t.Fatalf("idle timeout error = %v", err)
	}
}

func TestEvalAnalyzeValidatesArguments(t *testing.T) {
	if err := evalCmd(context.Background(), []string{"analyze", ".takt/evals/run", "--repeat", "2"}); err == nil || err.Error() != "repeat requires --case" {
		t.Fatalf("repeat without case error=%v", err)
	}
	if err := evalCmd(context.Background(), []string{"analyze", ".takt/evals/run", "--case", "c", "--repeat", "-1"}); err == nil || err.Error() != "repeat cannot be negative" {
		t.Fatalf("negative repeat error=%v", err)
	}
	if err := evalCmd(context.Background(), []string{"analyze", ".takt/evals/run", "--case", "c", "--repeat", "0"}); err == nil || err.Error() != "repeat must be positive" {
		t.Fatalf("zero repeat error=%v", err)
	}
	if err := evalCmd(context.Background(), []string{"analyze", ".takt/evals/run", "--language", "fr"}); err == nil || err.Error() != `unsupported analysis language "fr" (want en or ru)` {
		t.Fatalf("language error=%v", err)
	}
	if err := evalCmd(context.Background(), []string{"analyze"}); err == nil || err.Error() != "usage: takt eval analyze <evaluation-output-dir> [flags]" {
		t.Fatalf("missing directory error=%v", err)
	}
}

func TestEvalStatsRequiresOneOutputDirectory(t *testing.T) {
	err := evalCmd(context.Background(), []string{"stats"})
	if err == nil || !strings.Contains(err.Error(), "usage: takt eval stats") {
		t.Fatalf("stats error = %v", err)
	}
}

func TestEvalStatusRequiresOneOutputDirectory(t *testing.T) {
	err := evalCmd(context.Background(), []string{"status"})
	if err == nil || !strings.Contains(err.Error(), "usage: takt eval status") {
		t.Fatalf("status error = %v", err)
	}
}

func TestEvalInspectRequiresDirectoryAndValidFilter(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"inspect"}, "usage: takt eval inspect"},
		{[]string{"inspect", "missing", "--repeat", "-1"}, "repeat cannot be negative"},
		{[]string{"inspect", "missing", "--repeat", "1"}, "repeat requires --case"},
	} {
		if err := evalCmd(context.Background(), tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("eval %v error=%v, want %q", tc.args, err, tc.want)
		}
	}
}

func TestEvalTraceWritesElapsedProgress(t *testing.T) {
	var output strings.Builder
	now := time.Unix(10, 0)
	trace := newEvalTrace(&output, func() time.Time { return now })
	now = now.Add(3 * time.Second)
	trace("node.started node=implement")
	if got, want := output.String(), "[3s] node.started node=implement\n"; got != want {
		t.Fatalf("trace=%q want=%q", got, want)
	}
}

func TestEvalFlowInitRequiresSelectorAndOutput(t *testing.T) {
	if err := evalCmd(context.Background(), []string{"flow", "init"}); err == nil || !strings.Contains(err.Error(), "usage: takt eval flow init") {
		t.Fatalf("init error = %v", err)
	}
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := evalCmd(context.Background(), []string{"flow", "init", "code:feature-development", "--output", "flow", "--json=false"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "flow", "suite.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestAnswerAcceptsPublicSubworkflowNodeID(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	childPath := filepath.Join(dir, "child.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(childPath, []byte(`name: child
nodes:
  - id: approve
    approval:
      message: Continue?
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`name: parent
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
	runner := runtimefixture.New(wf, cfg, workflowPath, configPath, dir)
	state, err := runner.Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	if err := answerCmd(context.Background(), []string{state.ID, "child", "--value", "yes", "--workspace", dir, "--json=false"}); err != nil {
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
	workflowText := `name: approval-worktree
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
    bash: printf '%s' $approve.output > answer.txt
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
	state, err := runtimefixture.New(wf, cfg, workflowPath, configPath, repo).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting run, got state=%+v err=%v", state, err)
	}
	worktreePath := state.Worktree.Path
	if err := answerCmd(context.Background(), []string{state.ID, "approve", "--value", "yes", "--workspace", repo, "--json=false"}); err != nil {
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
	if err := worktreeRemoveCmd(context.Background(), []string{state.ID, "--workspace", repo, "--force", "--json=false"}); err != nil {
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
	if err := os.WriteFile(childPath, []byte(`name: child
nodes:
  - id: approve
    approval:
      message: Continue child?
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s' '$approve.output'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      output_node: done
  - id: finish
    depends_on: [child]
    bash: printf 'root:%s' '$child.output'
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
	state, err := runtimefixture.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected governed child waiting, got %v", err)
	}
	if err := answerCmd(context.Background(), []string{state.ID, "child", "--value", "yes", "--workspace", dir, "--json=false"}); err != nil {
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
	if err := childrenCmd(context.Background(), []string{root.ID, "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelWaitingGovernedTree(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	childPath := filepath.Join(dir, "child.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(childPath, []byte(`name: child
nodes:
  - id: approve
    approval:
      message: wait
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflowPath, []byte(`name: parent
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
	state, err := runtimefixture.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if !errors.Is(err, runtime.ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	childID := state.Waiting.ChildRunID
	if err := cancelCmd(context.Background(), []string{state.ID, "--workspace", dir, "--reason", "stop", "--json=false"}); err != nil {
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
	if err := os.WriteFile(workflowPath, []byte(`name: failing
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
	state, err := runtimefixture.New(wf, cfg, workflowPath, configPath, dir).Start(context.Background(), "")
	if err == nil || state.Status != store.RunFailed {
		t.Fatalf("expected failed run: state=%+v err=%v", state, err)
	}
	cancelErr := cancelCmd(context.Background(), []string{state.ID, "--workspace", dir, "--json=false"})
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
	if err := os.WriteFile(workflowPath, []byte(`name: logical-coding-agent
nodes:
  - id: work
    provider: fixture
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
	if err := runCmd(context.Background(), []string{workflowPath, "--config", configPath, "--workspace", dir, "--json=false"}); err != nil {
		t.Fatal(err)
	}
}

func TestCommandCmdSelectsModelPreset(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	commandDir := filepath.Join(dir, ".takt", "commands")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "work.md"), []byte("---\nprovider: fixture\nmodel: implementation\n---\nwork\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`apiVersion: takt/v1alpha1
kind: Config
model_preset: candidate
model_presets:
  candidate:
    implementation: vendor/org/model
    review: vendor/review
    routing: vendor/router
assistants:
  fixture: {type: mock}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := commandCmd(context.Background(), []string{"run", "work", "--workspace", dir, "--config", configPath, "--model-preset", "candidate", "--json=false"}); err != nil {
		t.Fatal(err)
	}
	ids, err := (store.FS{Workspace: dir}).ListRunIDs()
	if err != nil || len(ids) != 1 {
		t.Fatalf("run ids=%v err=%v", ids, err)
	}
	state, err := (store.FS{Workspace: dir}).Load(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	model := state.Nodes["command"].RequestedModel
	if state.RunOptions.ModelPreset != "candidate" || model == nil || model.Provider != "vendor" || model.ID != "org/model" {
		t.Fatalf("options=%+v model=%+v", state.RunOptions, model)
	}
}

func TestRunCmdSelectsModelPreset(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: preset\nprovider: fixture\nmodel: implementation\nnodes:\n  - id: work\n    prompt: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`apiVersion: takt/v1alpha1
kind: Config
model_preset: candidate
model_presets:
  candidate:
    implementation: vendor/org/model
    review: vendor/review
    routing: vendor/router
assistants:
  fixture: {type: mock}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCmd(context.Background(), []string{workflowPath, "--config", configPath, "--workspace", dir, "--model-preset", "candidate", "--model", "implementation=override/org/model", "--json=false"}); err != nil {
		t.Fatal(err)
	}
	ids, err := (store.FS{Workspace: dir}).ListRunIDs()
	if err != nil || len(ids) != 1 {
		t.Fatalf("run ids=%v err=%v", ids, err)
	}
	state, err := (store.FS{Workspace: dir}).Load(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	model := state.Nodes["work"].RequestedModel
	if state.RunOptions.ModelPreset != "candidate" || model == nil || model.Provider != "override" || model.ID != "org/model" {
		t.Fatalf("state=%+v model=%+v", state.RunOptions, model)
	}
}

func TestValidateCmdSelectsModelPreset(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte("name: preset\nprovider: fixture\nmodel: implementation\nnodes:\n  - id: work\n    prompt: work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`apiVersion: takt/v1alpha1
kind: Config
model_preset: candidate
model_presets:
  candidate:
    implementation: vendor/model
    review: vendor/review
    routing: vendor/router
assistants:
  fixture: {type: mock}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateCmd(context.Background(), []string{workflowPath, "--config", configPath, "--workspace", dir, "--model-preset", "candidate", "--json=false"}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskGoalReadsFilesOnlyWithExplicitFileFlag(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goal.txt")
	if err := os.WriteFile(path, []byte("file contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	goal, err := resolveTaskGoal([]string{path}, "")
	if err != nil {
		t.Fatal(err)
	}
	if goal != path {
		t.Fatalf("positional path was treated as file: %q", goal)
	}
	goal, err = resolveTaskGoal(nil, path)
	if err != nil {
		t.Fatal(err)
	}
	if goal != "file contents" {
		t.Fatalf("--file goal=%q", goal)
	}
	if _, err := resolveTaskGoal([]string{"text"}, path); err == nil {
		t.Fatal("text plus --file must fail")
	}
}

func TestRunCmdPerformsCapabilityPreflightBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	configPath := filepath.Join(dir, "config.yaml")
	workflowText := `name: preflight
nodes:
  - id: work
    prompt: do work
    provider: limited
    model: demo
    allowed_tools: [read]
`
	configText := `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  limited:
    type: process
    argv: [cat]
`
	if err := os.WriteFile(workflowPath, []byte(workflowText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(configText), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runCmd(context.Background(), []string{workflowPath, "--workspace", dir, "--config", configPath})
	if err == nil || !strings.Contains(err.Error(), "capability validation") || !strings.Contains(err.Error(), "tool_policy") {
		t.Fatalf("runCmd error = %v, want capability preflight failure", err)
	}
	ids, listErr := (store.FS{Workspace: dir}).ListRunIDs()
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(ids) != 0 {
		t.Fatalf("run was created before capability validation: %v", ids)
	}
}

func TestAdapterListDescribeAndDoctorPaths(t *testing.T) {
	dir := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: process
    argv: ["` + os.Args[0] + `", "-test.run=TestAdapterCLIHelper"]
    env:
      TAKT_ADAPTER_CLI_HELPER: "1"
    operations:
      change.create: change.create
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapterCmd(context.Background(), []string{"list", "--workspace", dir, "--config", path}); err != nil {
		t.Fatal(err)
	}
	if err := adapterCmd(context.Background(), []string{"describe", "scm", "--workspace", dir, "--config", path}); err != nil {
		t.Fatal(err)
	}
	if err := adapterCmd(context.Background(), []string{"doctor", "scm", "--workspace", dir, "--config", path}); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterDoctorReturnsErrorForCapabilityMismatch(t *testing.T) {
	dir := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: process
    argv: ["` + os.Args[0] + `", "-test.run=TestAdapterCLIHelper"]
    env:
      TAKT_ADAPTER_CLI_HELPER: "1"
    operations:
      change.create: change.create
      change.review: change.review
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapterCmd(context.Background(), []string{"doctor", "scm", "--workspace", dir, "--config", path}); err == nil || !strings.Contains(err.Error(), "configuration problems") {
		t.Fatalf("expected non-zero doctor result for capability mismatch, got %v", err)
	}
}

func TestAdapterCLIHelper(t *testing.T) {
	if os.Getenv("TAKT_ADAPTER_CLI_HELPER") == "" {
		return
	}
	_, _ = os.Stdin.Read(make([]byte, 4096))
	_, _ = os.Stdout.WriteString(`{"apiVersion":"takt-domain-adapter/v1alpha1","kind":"DescribeResponse","declaration":{"apiVersion":"takt-domain-adapter/v1alpha1","kind":"AdapterCapabilities","domain":"scm","capabilities":["change.create"],"reconcile":[]}}` + "\n")
	os.Exit(0)
}

func TestPackageCmdProjectLifecycle(t *testing.T) {
	workspace := t.TempDir()
	pkg := filepath.Join(t.TempDir(), "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "workflow.yaml"), []byte(`name: cli-package
nodes:
  - id: done
    prompt: Return JSON summary.
    output_format:
      type: object
      properties:
        summary:
          type: string
      required: [summary]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.yaml"), []byte(`apiVersion: takt/v1alpha1
kind: BlockPackage
metadata:
  name: cli-package
  version: 1.0.0
  scope: project
blocks:
  cli-package:
    workflow: workflow.yaml
    output_paths: [summary]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"install", pkg, "--scope", "project", "--workspace", workspace},
		{"list", "--workspace", workspace},
		{"doctor", "--workspace", workspace},
		{"uninstall", "cli-package", "--scope", "project", "--workspace", workspace},
	} {
		if err := packageCmd(context.Background(), args); err != nil {
			t.Fatalf("packageCmd(context.Background(), %v): %v", args, err)
		}
	}
}

func TestCompatibilityCmdMatrixSchemaAndCheck(t *testing.T) {
	if err := compatibilityCmd(context.Background(), []string{"matrix", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := compatibilityCmd(context.Background(), []string{"fields", "--json"}); err != nil {
		t.Fatal(err)
	}
	if err := compatibilityCmd(context.Background(), []string{"schema", "--json"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  current:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: ["fake"]
    capabilities: [tool_control]
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compatibilityCmd(context.Background(), []string{"check", "--workspace", dir, "--config", path}); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityCmdStrictRejectsLegacyProcess(t *testing.T) {
	dir := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  legacy:
    type: process
    argv: ["fake"]
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compatibilityCmd(context.Background(), []string{"check", "--workspace", dir, "--config", path, "--strict"}); err == nil || !strings.Contains(err.Error(), "status: warning") {
		t.Fatalf("expected strict compatibility warning to fail, got %v", err)
	}
}

func TestCompatibilityCmdLiveDomainAdapterUsesDescribe(t *testing.T) {
	dir := t.TempDir()
	config := `apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: process
    argv: ["` + os.Args[0] + `", "-test.run=TestAdapterCLIHelper"]
    env:
      TAKT_ADAPTER_CLI_HELPER: "1"
    operations:
      change.create: change.create
`
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compatibilityCmd(context.Background(), []string{"check", "--workspace", dir, "--config", path, "--live"}); err != nil {
		t.Fatal(err)
	}
}

func TestPrintResultJSONUsesStableEnvelope(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()
	if err := printResult(true, map[string]any{"value": 7}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK     bool `json:"ok"`
		Result struct {
			Value int `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.Result.Value != 7 {
		t.Fatalf("unexpected CLI envelope: %s", raw)
	}
}

func TestRunAssessmentCmdRequiresRunID(t *testing.T) {
	err := runDispatchCmd(context.Background(), []string{"assessment"})
	if err == nil || !strings.Contains(err.Error(), "usage: takt run assessment <run-id>") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunObservationCommandsRequireRunID(t *testing.T) {
	for _, operation := range []string{"status", "stats", "inspect"} {
		err := runDispatchCmd(context.Background(), []string{operation})
		if err == nil || !strings.Contains(err.Error(), "usage: takt run "+operation+" <run-id>") {
			t.Fatalf("%s error = %v", operation, err)
		}
	}
}

func TestPrintErrorJSONUsesStableEnvelope(t *testing.T) {
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()
	if err := PrintErrorJSON(errors.New("fixture failure")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Retryable bool           `json:"retryable"`
			Details   map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || envelope.Error.Code != "internal_error" || envelope.Error.Message != "fixture failure" || envelope.Error.Details == nil {
		t.Fatalf("unexpected CLI error envelope: %s", raw)
	}
}

func TestUsageSeparatesStableExperimentalAndToolingCommands(t *testing.T) {
	err := usage()
	if err == nil {
		t.Fatal("usage must return the command help error")
	}
	text := err.Error()
	for _, section := range []string{"stable:", "extensions:", "experimental:", "tooling:"} {
		if !strings.Contains(text, section) {
			t.Fatalf("usage is missing %q section: %s", section, text)
		}
	}
	if !strings.Contains(text, "experimental: task plan execute steer host learn") {
		t.Fatalf("dynamic flow must be visibly experimental: %s", text)
	}
}
