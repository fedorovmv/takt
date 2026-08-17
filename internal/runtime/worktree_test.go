package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "takt/internal/config"
	"takt/internal/gitworktree"
	"takt/internal/profile"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestRunUsesIsolatedWorktreeAndRetainsDirtyResult(t *testing.T) {
	repo, workflowPath, configPath := runtimeWorktreeRepo(t, `name: isolated-change
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: change
    bash: printf 'isolated\n' > generated.txt
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, cfg, workflowPath, configPath, repo)
	state, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Worktree == nil {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.ExecutionWorkspace == repo {
		t.Fatalf("execution was not isolated: %+v", state.Worktree)
	}
	if state.Worktree.Removed || state.Worktree.RetainedReason != "uncommitted_changes" || !state.Worktree.Dirty {
		t.Fatalf("dirty worktree was not retained: %+v", state.Worktree)
	}
	if _, err := os.Stat(filepath.Join(repo, "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("generated file leaked into control workspace: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(state.ExecutionWorkspace, "generated.txt"))
	if err != nil || string(content) != "isolated\n" {
		t.Fatalf("isolated result missing: %q, %v", content, err)
	}
	if err := gitworktree.Remove(context.Background(), state.Worktree.RepositoryRoot, state.Worktree.Path, true); err != nil {
		t.Fatal(err)
	}
}

func TestRunRemapsControlInputPathIntoExecutionWorkspace(t *testing.T) {
	repo, workflowPath, configPath := runtimeWorktreeRepo(t, `name: input-remap
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: inspect
    bash: test -f "$TAKT_WORKSPACE/.takt/eval/input.md"
`)
	inputPath := filepath.Join(repo, ".takt", "eval", "input.md")
	if err := os.MkdirAll(filepath.Dir(inputPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(inputPath, []byte("task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, repo, "add", ".takt/eval/input.md")
	runtimeGit(t, repo, "commit", "-m", "input")

	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	preparedInput, err := profile.PrepareInput(profile.InputSpec{Format: "markdown", PreservePath: true}, inputPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, cfg, workflowPath, configPath, repo).Start(context.Background(), preparedInput)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state.ExecutionWorkspace, ".takt", "eval", "input.md")
	if !strings.Contains(state.Input, "Source file: `"+want+"`") || strings.Contains(state.Input, "Source file: `"+inputPath+"`") {
		t.Fatalf("state input did not remap source header to execution path:\n%s", state.Input)
	}
}

func TestRunRemovesCleanWorktreeOnSuccess(t *testing.T) {
	repo, workflowPath, configPath := runtimeWorktreeRepo(t, `name: clean-run
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: inspect
    bash: git status --short
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, cfg, workflowPath, configPath, repo).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Worktree == nil || !state.Worktree.Removed || state.Worktree.Dirty {
		t.Fatalf("clean worktree was not removed: %+v", state.Worktree)
	}
	if _, err := os.Stat(state.Worktree.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree path still exists: %v", err)
	}
	branch := runtimeGit(t, repo, "branch", "--list", state.Worktree.Branch)
	if strings.Contains(branch, state.Worktree.Branch) || !state.Worktree.BranchRemoved {
		t.Fatalf("empty run branch was not removed: branch=%q state=%+v", branch, state.Worktree)
	}
}

func runtimeWorktreeRepo(t *testing.T, workflowText string) (string, string, string) {
	t.Helper()
	repo := t.TempDir()
	runtimeGit(t, repo, "init")
	runtimeGit(t, repo, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, repo, "config", "user.name", "Takt Test")
	workflowPath := filepath.Join(repo, "workflow.yaml")
	configPath := filepath.Join(repo, "config.yaml")
	if err := os.WriteFile(workflowPath, []byte(workflowText), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, repo, "add", ".")
	runtimeGit(t, repo, "commit", "-m", "test definitions")
	return repo, workflowPath, configPath
}

func runtimeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSubworkflowEnablesWorktreeAtItsGate(t *testing.T) {
	repo := t.TempDir()
	runtimeGit(t, repo, "init")
	runtimeGit(t, repo, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, repo, "config", "user.name", "Takt Test")
	parentPath := filepath.Join(repo, "workflow.yaml")
	childPath := filepath.Join(repo, "child.yaml")
	configPath := filepath.Join(repo, "config.yaml")
	if err := os.WriteFile(parentPath, []byte(`name: router-like-parent
nodes:
  - id: select
    bash: printf selected
  - id: child
    depends_on: [select]
    subworkflow:
      path: child.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(`name: mutating-child
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: change
    bash: printf 'child-worktree\n' > generated.txt
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, repo, "add", ".")
	runtimeGit(t, repo, "commit", "-m", "test definitions")

	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, cfg, parentPath, configPath, repo).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Worktree == nil || state.ExecutionWorkspace == repo {
		t.Fatalf("child gate did not create an isolated worktree: %+v", state.Worktree)
	}
	if state.Worktree.RetainedReason != "uncommitted_changes" {
		t.Fatalf("mutating child worktree should be retained: %+v", state.Worktree)
	}
	if _, err := os.Stat(filepath.Join(repo, "generated.txt")); !os.IsNotExist(err) {
		t.Fatalf("child change leaked into control checkout: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(state.ExecutionWorkspace, "generated.txt"))
	if err != nil || string(content) != "child-worktree\n" {
		t.Fatalf("child did not execute in worktree: %q, %v", content, err)
	}
	if err := gitworktree.Remove(context.Background(), state.Worktree.RepositoryRoot, state.Worktree.Path, true); err != nil {
		t.Fatal(err)
	}
}

func TestSubworkflowWorktreeRespectsPersistedNoWorktreeOverride(t *testing.T) {
	repo := t.TempDir()
	runtimeGit(t, repo, "init")
	runtimeGit(t, repo, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, repo, "config", "user.name", "Takt Test")
	parentPath := filepath.Join(repo, "workflow.yaml")
	childPath := filepath.Join(repo, "child.yaml")
	configPath := filepath.Join(repo, "config.yaml")
	if err := os.WriteFile(parentPath, []byte(`name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(`name: child
worktree:
  enabled: true
nodes:
  - id: inspect
    bash: pwd
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, repo, "add", ".")
	runtimeGit(t, repo, "commit", "-m", "test definitions")
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	state, err := New(wf, cfg, parentPath, configPath, repo).StartWithOptions(context.Background(), "", StartOptions{Worktree: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	if state.Worktree != nil || state.ExecutionWorkspace != repo {
		t.Fatalf("--no-worktree was not applied to child policy: %+v", state)
	}
	if state.RunOptions.WorktreeMode != "disabled" {
		t.Fatalf("override was not persisted: %+v", state.RunOptions)
	}
}

func TestGovernedChildRunCanInheritParentWorktree(t *testing.T) {
	repo := t.TempDir()
	runtimeGit(t, repo, "init")
	runtimeGit(t, repo, "config", "user.email", "takt@example.invalid")
	runtimeGit(t, repo, "config", "user.name", "Takt Test")
	parentPath := filepath.Join(repo, "workflow.yaml")
	childPath := filepath.Join(repo, "child.yaml")
	configPath := filepath.Join(repo, "config.yaml")
	if err := os.WriteFile(parentPath, []byte(`name: parent
worktree:
  enabled: true
  cleanup: on_success
nodes:
  - id: child
    workflow:
      path: child.yaml
      isolation: inherit
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(`name: child
worktree:
  enabled: true
nodes:
  - id: change
    bash: printf child > inherited.txt
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runtimeGit(t, repo, "add", ".")
	runtimeGit(t, repo, "commit", "-m", "definitions")
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := cfgpkg.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, cfg, parentPath, configPath, repo)
	parent, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if parent.Worktree == nil || parent.ExecutionWorkspace == repo || len(parent.ChildRunIDs) != 1 {
		t.Fatalf("unexpected parent isolation: %+v", parent)
	}
	child, err := runner.store.Load(parent.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if child.Worktree != nil || child.ExecutionWorkspace != parent.ExecutionWorkspace {
		t.Fatalf("child did not inherit parent worktree: parent=%+v child=%+v", parent.Worktree, child)
	}
	if _, err := os.Stat(filepath.Join(repo, "inherited.txt")); !os.IsNotExist(err) {
		t.Fatalf("child change leaked into control checkout: %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(parent.ExecutionWorkspace, "inherited.txt")); err != nil || string(content) != "child" {
		t.Fatalf("child did not write in inherited worktree: %q err=%v", content, err)
	}
	if err := gitworktree.Remove(context.Background(), parent.Worktree.RepositoryRoot, parent.Worktree.Path, true); err != nil {
		t.Fatal(err)
	}
}
