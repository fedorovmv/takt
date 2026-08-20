package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/execution"
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

func TestDynamicChildMatrixSelectsContainedWorkflowAndRepository(t *testing.T) {
	root := t.TempDir()
	for name, output := range map[string]string{"repo-a": "one", "repo-b": "two"} {
		repo := filepath.Join(root, name)
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(repo, "child.yaml"), "name: "+name+"\nnodes:\n  - id: result\n    bash: printf "+output+"\n")
	}
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, parentPath, `name: dynamic-children
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items:
          type: object
          properties:
            repository: {type: string}
            workflow_path: {type: string}
          required: [repository, workflow_path]
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            isolation: none
            keep_worktree: true
      output_node: candidate
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(`{"cases":[{"repository":"repo-a","workflow_path":%q},{"repository":"repo-b","workflow_path":%q}]}`, filepath.Join(root, "repo-a", "child.yaml"), filepath.Join(root, "repo-b", "child.yaml"))
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", root).Start(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if state.Output != `["one","two"]` || len(state.ChildRunIDs) != 2 {
		t.Fatalf("dynamic matrix output=%s children=%v", state.Output, state.ChildRunIDs)
	}
	branches := state.Nodes["cases"].MatrixBranches
	for index, repository := range []string{"repo-a", "repo-b"} {
		childNode := branches[index].Nodes["cases__candidate"]
		child, loadErr := (store.FS{Workspace: root}).Load(childNode.ChildRunID)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		wantRepo, _ := filepath.EvalSymlinks(filepath.Join(root, repository))
		if childNode.ChildControlWorkspace != wantRepo || !child.RunOptions.KeepWorktree {
			t.Fatalf("branch %d child=%+v run_options=%+v", index, childNode, child.RunOptions)
		}
	}
}

func TestMatrixPreflightRejectsInvalidLaterChildBeforeSideEffect(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(repo, "child.yaml"), "name: child\nnodes:\n  - id: effect\n    bash: touch side-effect; printf ok\n")
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, parentPath, dynamicMatrixParentSource())
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	input := fmt.Sprintf(`{"cases":[{"repository":"repo","workflow_path":%q},{"repository":"repo","workflow_path":%q}]}`, filepath.Join(repo, "child.yaml"), filepath.Join(repo, "missing.yaml"))
	_, err = New(wf, &spec.Config{}, parentPath, "<config>", root).Start(context.Background(), input)
	if err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Fatalf("preflight error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(repo, "side-effect")); !os.IsNotExist(statErr) {
		t.Fatalf("first branch executed before later preflight failure: %v", statErr)
	}
}

func TestMatrixPreflightChecksEveryStaticChildInputBeforeSideEffect(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "child.yaml"), `name: child
input:
  format: json
  schema: {type: integer}
nodes:
  - id: effect
    bash: touch side-effect; printf ok
`)
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, parentPath, `name: static-preflight
input:
  format: json
  schema:
    type: object
    properties:
      cases: {type: array, items: {type: object}}
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: child.yaml
            input: $MATRIX.item.value
            isolation: inherit
      output_node: candidate
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New(wf, &spec.Config{}, parentPath, "<config>", root).Start(context.Background(), `{"cases":[{"value":1},{"value":"bad"}]}`)
	if err == nil || !strings.Contains(err.Error(), "workflow input") {
		t.Fatalf("static input preflight error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "side-effect")); !os.IsNotExist(statErr) {
		t.Fatalf("first static child executed before later input failure: %v", statErr)
	}
}

func TestDynamicChildRejectsRepositoryEscapeAndInvalidWorkflow(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	outside := t.TempDir()
	outsideWorkflow := filepath.Join(root, "outside.yaml")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(repo, "child.yaml"), "name: child\nnodes:\n  - id: done\n    bash: true\n")
	mustWriteFile(t, outsideWorkflow, "name: outside\nnodes:\n  - id: done\n    bash: true\n")
	if err := os.Symlink(outside, filepath.Join(root, "escaped-repo")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, parentPath, dynamicMatrixParentSource())
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ repository, childPath string }{
		{"../" + filepath.Base(outside), filepath.Join(repo, "child.yaml")},
		{"escaped-repo", filepath.Join(repo, "child.yaml")},
		{"repo", outsideWorkflow},
		{"repo", filepath.Join(repo, "missing.yaml")},
		{"repo", repo},
	}
	for _, tc := range tests {
		t.Run(tc.repository+tc.childPath, func(t *testing.T) {
			input := fmt.Sprintf(`{"cases":[{"repository":%q,"workflow_path":%q}]}`, tc.repository, tc.childPath)
			if _, err := New(wf, &spec.Config{}, parentPath, "<config>", root).Start(context.Background(), input); err == nil {
				t.Fatalf("dynamic child repository=%q path=%q was accepted", tc.repository, tc.childPath)
			}
		})
	}
}

func TestDynamicChildDefinitionDriftBlocksMatrixResume(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(repo, "child.yaml")
	if err := os.MkdirAll(filepath.Join(repo, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(repo, "commands", "build.md")
	mustWriteFile(t, commandPath, "return one")
	mustWriteFile(t, childPath, "name: child\nprovider: worker\nmodel: demo\nnodes:\n  - id: result\n    command: build\n")
	parentPath := filepath.Join(root, "parent.yaml")
	mustWriteFile(t, parentPath, `name: drift
input:
  format: json
  schema:
    type: object
    properties:
      cases: {type: array, items: {type: object}}
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: approve
          approval: {message: continue}
        - id: candidate
          depends_on: [approve]
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            isolation: none
      output_node: candidate
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	config := &spec.Config{Models: map[string]spec.ModelSpec{"demo": {Provider: "test", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"worker": {Type: "mock"}}}
	runner := New(wf, config, parentPath, "<config>", root)
	state, err := runner.Start(context.Background(), fmt.Sprintf(`{"cases":[{"repository":"repo","workflow_path":%q}]}`, childPath))
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("start error = %v", err)
	}
	mustWriteFile(t, commandPath, "return changed")
	approvalID := state.Waiting.NodeID
	state.Approvals[approvalID] = "yes"
	state.Nodes[approvalID].Status = store.NodePending
	state.Status, state.Waiting = store.RunRunning, nil
	if err := runner.store.Commit(state, store.Event{Type: "approval.answered", NodeID: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Resume(context.Background(), state); err == nil || !strings.Contains(err.Error(), "definition changed") {
		t.Fatalf("resume drift error = %v", err)
	}
}

func dynamicMatrixParentSource() string {
	return `name: dynamic-preflight
input:
  format: json
  schema:
    type: object
    properties:
      cases: {type: array, items: {type: object}}
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            isolation: none
      output_node: candidate
`
}

func TestChildFailureKindPreservesInfrastructureDiagnostics(t *testing.T) {
	for _, code := range []execution.Kind{execution.KindExit, execution.KindProtocol, execution.KindConfiguration, execution.KindTimedOut} {
		t.Run(string(code), func(t *testing.T) {
			child := &store.RunState{ID: "child", Status: store.RunFailed, ErrorCode: string(code), Error: "failed"}
			_, err := (&Runner{}).finishChildRun(&store.RunState{}, "child", &store.NodeState{ChildRunID: child.ID}, &spec.Workflow{}, "", child, &RunFailedError{RunID: child.ID, Code: string(code), Cause: "failed"})
			if got := execution.KindOf(err); got != code {
				t.Fatalf("child error kind = %s, want %s: %v", got, code, err)
			}
		})
	}
}

func TestChildFailureKindPreservesFanOutInfrastructureDiagnostics(t *testing.T) {
	records := []store.ChildRunItemState{
		{Attempt: 1, Status: store.RunFailed, ErrorCode: string(execution.KindExit)},
		{Attempt: 1, Status: store.RunFailed, ErrorCode: string(execution.KindProtocol)},
	}
	if got := fanOutFailureKind(records, 1); got != execution.KindProtocol {
		t.Fatalf("fan-out failure kind = %s", got)
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
