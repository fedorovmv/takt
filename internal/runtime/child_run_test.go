package runtime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestGovernedChildRunWaitsResumesAndReturnsOutput(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: child
nodes:
  - id: approve
    approval:
      message: Approve child
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s' $approve.output
`)
	mustWriteFile(t, parentPath, `name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      input: $ARGUMENTS
      output_node: done
      isolation: inherit
  - id: after
    depends_on: [child]
    bash: printf 'parent:%s' $child.output
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "request")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected child wait, got state=%+v err=%v", parent, err)
	}
	if parent.Waiting == nil || parent.Waiting.Kind != "child_run" || parent.Waiting.ChildRunID == "" {
		t.Fatalf("unexpected parent waiting state: %+v", parent.Waiting)
	}
	childID := parent.Waiting.ChildRunID
	child, err := runner.store.Load(childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentRunID != parent.ID || child.ParentNodeID != "child" || child.Status != store.RunWaiting {
		t.Fatalf("unexpected child linkage: %+v", child)
	}
	if filepath.Clean(runner.store.ArtifactsDir(child.ID)) == filepath.Clean(runner.store.ArtifactsDir(parent.ID)) {
		t.Fatal("child and parent share artifacts directory")
	}

	childWorkflow, err := workflow.Load(childPath)
	if err != nil {
		t.Fatal(err)
	}
	childRunner := New(childWorkflow, &spec.Config{}, childPath, "<config>", dir)
	child.Approvals["approve"] = "yes"
	child.Nodes["approve"].Status = store.NodePending
	child.Status = store.RunRunning
	child.Waiting = nil
	if err := childRunner.store.Commit(child, store.Event{Type: "approval.answered", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	child, err = childRunner.Resume(context.Background(), child)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != store.RunCompleted || child.Output != "yes" {
		t.Fatalf("unexpected completed child: %+v", child)
	}

	parent, err = runner.Resume(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != store.RunCompleted {
		t.Fatalf("parent did not complete: %+v", parent)
	}
	if parent.Nodes["child"].ChildRunID != child.ID || parent.Nodes["child"].Output != "yes" {
		t.Fatalf("child node did not expose child result: %+v", parent.Nodes["child"])
	}
	if parent.Nodes["after"].Output != "parent:yes" || parent.Output != "parent:yes" {
		t.Fatalf("unexpected parent output: node=%q run=%q", parent.Nodes["after"].Output, parent.Output)
	}
}

func TestGovernedChildRunFailureFailsParentNode(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: child
nodes:
  - id: fail
    bash: echo child-failed >&2; exit 7
`)
	mustWriteFile(t, parentPath, `name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected parent failure")
	}
	if parent.Status != store.RunFailed || parent.Nodes["child"].Status != store.NodeFailed {
		t.Fatalf("unexpected parent failure state: %+v", parent)
	}
	child, loadErr := runner.store.Load(parent.Nodes["child"].ChildRunID)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if child.Status != store.RunFailed || child.ParentRunID != parent.ID {
		t.Fatalf("unexpected child failure state: %+v", child)
	}
}

func TestCancelParentRequestsCancellationForChild(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: child
nodes:
  - id: approve
    approval:
      message: wait
`)
	mustWriteFile(t, parentPath, `name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected waiting, got %v", err)
	}
	childID := parent.Waiting.ChildRunID
	parent, err = runner.Cancel(parent, "stop tree")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if parent.Status != store.RunCancelled {
		t.Fatalf("unexpected parent status: %s", parent.Status)
	}
	fs := store.FS{Workspace: dir}
	requested, err := fs.CancelRequested(childID)
	if err != nil || !requested {
		t.Fatalf("child cancellation was not requested: requested=%v err=%v", requested, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCancellationMarkerStopsRunningNode(t *testing.T) {
	dir := t.TempDir()
	wf := &spec.Workflow{Name: "cancel-running", Nodes: []spec.Node{{ID: "slow", Bash: "sleep 30"}}}
	runner := New(wf, &spec.Config{}, "<workflow>", "<config>", dir)
	type result struct {
		state *store.RunState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, err := runner.Start(context.Background(), "")
		done <- result{state: state, err: err}
	}()
	var runID string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir(filepath.Join(dir, ".takt", "runs"))
		if len(entries) > 0 {
			runID = entries[0].Name()
			state, err := (store.FS{Workspace: dir}).Load(runID)
			if err == nil && state.Nodes["slow"].Status == store.NodeRunning {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if runID == "" {
		t.Fatal("run did not start")
	}
	if err := (store.FS{Workspace: dir}).RequestCancel(runID); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-done:
		if out.state == nil || out.state.Status != store.RunCancelled {
			t.Fatalf("running node was not cancelled: state=%+v err=%v", out.state, out.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("running node did not observe cancellation marker")
	}
}

func TestGovernedChildRetryCreatesNewChildRun(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: retry-child
nodes:
  - id: run
    bash: |
      if [ ! -f retry-marker ]; then
        touch retry-marker
        echo first-child-failed >&2
        exit 9
      fi
      printf child-succeeded
`)
	mustWriteFile(t, parentPath, `name: retry-parent
nodes:
  - id: child
    attempts:
      max: 2
      retry_on: [exit]
    workflow:
      path: child.yaml
      isolation: inherit
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != store.RunCompleted || parent.Output != "child-succeeded" {
		t.Fatalf("unexpected parent result: %+v", parent)
	}
	node := parent.Nodes["child"]
	if node.Attempts != 2 || len(node.ChildRunIDs) != 2 || len(parent.ChildRunIDs) != 2 {
		t.Fatalf("retry did not create two governed children: node=%+v parent=%+v", node, parent.ChildRunIDs)
	}
	first, err := runner.store.Load(node.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.store.Load(node.ChildRunIDs[1])
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != store.RunFailed || second.Status != store.RunCompleted {
		t.Fatalf("unexpected child attempt statuses: first=%s second=%s", first.Status, second.Status)
	}
	if node.ChildRunID != second.ID {
		t.Fatalf("current child link does not point to the successful attempt: %q != %q", node.ChildRunID, second.ID)
	}
}

func TestAggregateRunUsageIncludesHiddenCompositionNodes(t *testing.T) {
	usage := aggregateRunUsage(map[string]*store.NodeState{
		"public": {Status: store.NodeCompleted, Usage: &store.Usage{InputTokens: 3, OutputTokens: 4, Cost: 0.1}},
		"hidden": {Status: store.NodeCompleted, Hidden: true, Usage: &store.Usage{InputTokens: 10, OutputTokens: 20, Cost: 0.5}},
	})
	if usage == nil || usage.InputTokens != 13 || usage.OutputTokens != 24 || usage.Cost != 0.6 {
		t.Fatalf("hidden usage was lost: %+v", usage)
	}
}

func TestCancelRejectsFailedRun(t *testing.T) {
	state := &store.RunState{ID: "failed-run", Status: store.RunFailed}
	got, err := (&Runner{}).Cancel(state, "late cancel")
	if err == nil || got.Status != store.RunFailed || !strings.Contains(err.Error(), "cannot cancel terminal run") {
		t.Fatalf("failed run status was not preserved: state=%+v err=%v", got, err)
	}
}

func TestGovernedChildRunCanOwnRepositoryWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "init", "-q")
	runGitTest(t, repo, "config", "user.email", "test@example.com")
	runGitTest(t, repo, "config", "user.name", "Test")
	mustWriteFile(t, filepath.Join(repo, "base.txt"), "base\n")
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-qm", "base")

	childPath := filepath.Join(root, "child.yaml")
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, childPath, `name: child-repo
nodes:
  - id: change
    bash: printf changed > changed.txt; printf '{"summary":"changed"}'
`)
	mustWriteFile(t, parentPath, `name: parent-repo
nodes:
  - id: repo-change
    workflow:
      path: child.yaml
      repository: repo
      isolation: worktree
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", root)
	state, err := runner.Start(context.Background(), "change repo")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["repo-change"]
	resolvedRepo, resolveErr := filepath.EvalSymlinks(repo)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if node == nil || node.ChildRunID == "" || node.ChildControlWorkspace != resolvedRepo || node.ChildExecutionWorkspace == "" || node.ChildExecutionWorkspace == resolvedRepo || node.ChildBranch == "" || node.ChildBaseCommit == "" {
		t.Fatalf("child metadata not captured: %+v", node)
	}
	if _, err := os.Stat(filepath.Join(node.ChildExecutionWorkspace, "changed.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "changed.txt")); !os.IsNotExist(err) {
		t.Fatalf("base checkout was modified: %v", err)
	}
}

func TestChildRepositoryRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "repo")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	r := &Runner{controlWorkspace: root}
	if _, err := r.resolveChildControlWorkspace("repo"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("expected escape rejection, got %v", err)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestGovernedChildRetryReusesCompletedChildRun(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: completed-child
nodes:
  - id: run
    bash: |
      n=0
      test -f child-count && n=$(cat child-count)
      n=$((n+1))
      printf %s "$n" > child-count
      printf child-ok
`)
	mustWriteFile(t, parentPath, `name: retry-parent-postcheck
nodes:
  - id: child
    attempts:
      max: 2
    hooks:
      after_node:
        - id: transient-parent-check
          bash: |
            if [ ! -f parent-check-marker ]; then
              touch parent-check-marker
              exit 1
            fi
          on_failure:
            action: retry
    workflow:
      path: child.yaml
      isolation: inherit
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := parent.Nodes["child"]
	if node.Attempts != 2 {
		t.Fatalf("parent attempts=%d want 2", node.Attempts)
	}
	if len(node.ChildRunIDs) != 1 || len(parent.ChildRunIDs) != 1 {
		t.Fatalf("completed child should be reused, node=%v parent=%v", node.ChildRunIDs, parent.ChildRunIDs)
	}
	count, err := os.ReadFile(filepath.Join(dir, "child-count"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("completed child executed %s times", strings.TrimSpace(string(count)))
	}
	child, err := runner.store.Load(node.ChildRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != store.RunCompleted {
		t.Fatalf("child status=%s", child.Status)
	}
}

func TestLoopGroupRunsGovernedWorkflowChild(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child-loop.yaml")
	parentPath := filepath.Join(dir, "parent-loop.yaml")
	mustWriteFile(t, childPath, `name: child-loop
nodes:
  - id: result
    bash: printf child-done
`)
	mustWriteFile(t, parentPath, `name: parent-loop
nodes:
  - id: retry
    loop_group:
      max_iterations: 1
      nodes:
        - id: child
          workflow:
            path: child-loop.yaml
            output_node: result
            isolation: inherit
      until:
        node: child
        output_contains: child-done
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	state, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Nodes["retry"].Output != "child-done" {
		t.Fatalf("unexpected loop state: %+v", state.Nodes["retry"])
	}
	if len(state.Nodes["retry"].LoopIterations) != 1 {
		t.Fatalf("missing history: %+v", state.Nodes["retry"].LoopIterations)
	}
	loopChild := state.Nodes["retry"].LoopIterations[0].Nodes["retry__child"]
	if loopChild.Status != store.NodeCompleted || loopChild.ChildRunID == "" {
		t.Fatalf("governed child missing from loop history: %+v", loopChild)
	}
	child, err := runner.store.Load(loopChild.ChildRunID)
	if err != nil {
		t.Fatal(err)
	}
	if child.Status != store.RunCompleted || child.ParentRunID != state.ID {
		t.Fatalf("unexpected governed child: %+v", child)
	}
}
