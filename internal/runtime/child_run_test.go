package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: approve
    approval:
      message: Approve child
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s' '${nodes.approve.output}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      input: ${input}
      output_node: done
      isolation: inherit
  - id: after
    depends_on: [child]
    bash: printf 'parent:%s' '${nodes.child.output}'
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
	child, err := runner.Store.Load(childID)
	if err != nil {
		t.Fatal(err)
	}
	if child.ParentRunID != parent.ID || child.ParentNodeID != "child" || child.Status != store.RunWaiting {
		t.Fatalf("unexpected child linkage: %+v", child)
	}
	if filepath.Clean(runner.Store.ArtifactsDir(child.ID)) == filepath.Clean(runner.Store.ArtifactsDir(parent.ID)) {
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
	if err := childRunner.Store.Commit(child, store.Event{Type: "approval.answered", NodeID: "approve"}); err != nil {
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
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: fail
    bash: echo child-failed >&2; exit 7
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
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
	child, loadErr := runner.Store.Load(parent.Nodes["child"].ChildRunID)
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
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: approve
    approval:
      message: wait
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
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
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "cancel-running"}, Nodes: []spec.Node{{ID: "slow", Bash: "sleep 30"}}}
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
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry-child
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
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry-parent
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
	first, err := runner.Store.Load(node.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := runner.Store.Load(node.ChildRunIDs[1])
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
