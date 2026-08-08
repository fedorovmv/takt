package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestGovernedChildFanOutAggregatesOrderedResults(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: fanout-child
nodes:
  - id: result
    bash: printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: fanout-parent
nodes:
  - id: discover
    bash: printf '%s' '{"tasks":[{"name":"alpha"},{"name":"beta"},{"name":"gamma"}]}'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${task.name}:${fanout.index}/${fanout.total}'
      output_node: result
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output.tasks
        as: task
        max_parallel: 2
        join: all_success
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
	if state.Status != store.RunCompleted || len(state.Nodes["execute"].ChildRuns) != 3 || len(state.ChildRunIDs) != 3 {
		t.Fatalf("unexpected fan-out state: %+v", state.Nodes["execute"])
	}
	var records []struct {
		Index  int    `json:"index"`
		RunID  string `json:"run_id"`
		Status string `json:"status"`
		Output string `json:"output"`
	}
	if err := json.Unmarshal([]byte(state.Nodes["execute"].Output), &records); err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha:0/3", "beta:1/3", "gamma:2/3"}
	for index, record := range records {
		if record.Index != index || record.Status != store.RunCompleted || record.Output != want[index] || record.RunID == "" {
			t.Fatalf("unexpected ordered record %d: %+v", index, record)
		}
	}
}

func TestGovernedChildFanOutRunsWithBoundedParallelism(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parallel-child
nodes:
  - id: barrier
    bash: |
      touch "ready-${input}"
      i=0
      while [ "$i" -lt 80 ]; do
        if [ -f ready-0 ] && [ -f ready-1 ]; then
          printf 'done-%s' '${input}'
          exit 0
        fi
        i=$((i + 1))
        sleep 0.025
      done
      echo barrier-timeout >&2
      exit 9
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parallel-parent
nodes:
  - id: discover
    bash: printf '[0,1]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      output_node: barrier
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 2
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("parallel fan-out did not pass barrier: state=%+v err=%v", state, err)
	}
}

func TestGovernedChildFanOutResumesOnlyWaitingChildren(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval-child
nodes:
  - id: approve
    approval:
      message: approve ${input}
      capture_response: true
  - id: done
    depends_on: [approve]
    bash: printf '%s:%s' '${input}' '${nodes.approve.output}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: approval-parent
nodes:
  - id: discover
    bash: printf '["first","second"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      output_node: done
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 2
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	parent, err := runner.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) || parent.Waiting == nil || len(parent.Waiting.ChildRunIDs) != 2 {
		t.Fatalf("expected two waiting children: state=%+v err=%v", parent.Waiting, err)
	}
	originalIDs := append([]string(nil), parent.Nodes["execute"].ChildRunIDs...)
	approveFanOutChild(t, runner, childPath, originalIDs[0], "yes")
	parent, err = runner.Resume(context.Background(), parent)
	if !errors.Is(err, ErrWaiting) || parent.Waiting == nil || len(parent.Waiting.ChildRunIDs) != 1 || parent.Waiting.ChildRunIDs[0] != originalIDs[1] {
		t.Fatalf("expected only second child to remain waiting: state=%+v err=%v", parent.Waiting, err)
	}
	if got := parent.Nodes["execute"].ChildRunIDs; len(got) != 2 || got[0] != originalIDs[0] || got[1] != originalIDs[1] {
		t.Fatalf("fan-out child IDs changed across resume: %v -> %v", originalIDs, got)
	}
	approveFanOutChild(t, runner, childPath, originalIDs[1], "ok")
	parent, err = runner.Resume(context.Background(), parent)
	if err != nil || parent.Status != store.RunCompleted {
		t.Fatalf("parent did not complete after both approvals: state=%+v err=%v", parent, err)
	}
	var records []map[string]any
	if err := json.Unmarshal([]byte(parent.Nodes["execute"].Output), &records); err != nil {
		t.Fatal(err)
	}
	if records[0]["output"] != "first:yes" || records[1]["output"] != "second:ok" {
		t.Fatalf("unexpected resumed fan-out output: %+v", records)
	}
}

func TestGovernedChildFanOutAllDonePreservesFailures(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: fallible-child
nodes:
  - id: run
    bash: |
      if [ '${input}' = bad ]; then
        echo rejected >&2
        exit 7
      fi
      printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: all-done-parent
nodes:
  - id: discover
    bash: printf '["good","bad"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 2
        join: all_done
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("all_done should complete parent: state=%+v err=%v", state, err)
	}
	var records []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(state.Nodes["execute"].Output), &records); err != nil {
		t.Fatal(err)
	}
	if records[0].Status != store.RunCompleted || records[1].Status != store.RunFailed {
		t.Fatalf("all_done lost child statuses: %+v", records)
	}
}

func approveFanOutChild(t *testing.T, parentRunner *Runner, childPath, childID, answer string) {
	t.Helper()
	child, err := parentRunner.store.Load(childID)
	if err != nil {
		t.Fatal(err)
	}
	childWorkflow, err := workflow.Load(childPath)
	if err != nil {
		t.Fatal(err)
	}
	childRunner := New(childWorkflow, &spec.Config{}, childPath, "<config>", parentRunner.controlWorkspace)
	childRunner.store = parentRunner.store
	child.Approvals["approve"] = answer
	child.Nodes["approve"].Status = store.NodePending
	child.Status = store.RunRunning
	child.Waiting = nil
	if err := childRunner.store.Commit(child, store.Event{Type: "approval.answered", NodeID: "approve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := childRunner.Resume(context.Background(), child); err != nil {
		t.Fatal(err)
	}
}

func TestGovernedChildFanOutPauseStopsBeforeNextBatch(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: pausable-fanout-child
nodes:
  - id: run
    bash: |
      touch "started-${input}"
      if [ '${input}' = 0 ]; then
        i=0
        while [ ! -f release-first ] && [ "$i" -lt 200 ]; do
          i=$((i + 1))
          sleep 0.01
        done
      fi
      printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: pausable-fanout-parent
nodes:
  - id: discover
    bash: printf '[0,1,2]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      output_node: run
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 1
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	type result struct {
		state *store.RunState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, runErr := runner.Start(context.Background(), "")
		done <- result{state: state, err: runErr}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, statErr := filepath.Glob(filepath.Join(dir, "started-0")); statErr != nil {
			t.Fatal(statErr)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "started-0")); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("first fan-out child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	fs := store.FS{Workspace: dir}
	ids, err := fs.ListRunIDs()
	if err != nil {
		t.Fatal(err)
	}
	var parentID string
	for _, id := range ids {
		state, loadErr := fs.Load(id)
		if loadErr == nil && state.ParentRunID == "" {
			parentID = id
			break
		}
	}
	if parentID == "" {
		t.Fatal("parent run was not persisted")
	}
	if err := fs.RequestPause(parentID); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(dir, "release-first"), "ok")

	paused := <-done
	if !errors.Is(paused.err, ErrPaused) || paused.state == nil || paused.state.Status != store.RunPaused {
		t.Fatalf("expected paused parent after first batch: state=%+v err=%v", paused.state, paused.err)
	}
	if _, err := os.Stat(filepath.Join(dir, "started-1")); !os.IsNotExist(err) {
		t.Fatalf("second fan-out batch started after pause request: err=%v", err)
	}

	resumed, err := runner.Resume(context.Background(), paused.state)
	if err != nil || resumed.Status != store.RunCompleted {
		t.Fatalf("fan-out did not resume remaining batches: state=%+v err=%v", resumed, err)
	}
	for _, name := range []string{"started-1", "started-2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing resumed fan-out marker %s: %v", name, err)
		}
	}
}

func TestGovernedChildFanOutRejectsEmptyItemsByDefault(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: empty-child
nodes:
  - id: run
    bash: printf done
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: empty-parent
nodes:
  - id: discover
    bash: printf '[]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "fan-out source") || state.Status != store.RunFailed {
		t.Fatalf("expected empty-source failure, state=%+v err=%v", state, err)
	}
}

func TestGovernedChildFanOutAllowsEmptyItems(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: empty-child
nodes:
  - id: run
    bash: printf done
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: empty-parent
nodes:
  - id: discover
    bash: printf '[]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        allow_empty: true
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("expected empty fan-out completion, state=%+v err=%v", state, err)
	}
	if got := state.Nodes["execute"].Output; got != "[]" {
		t.Fatalf("unexpected empty fan-out output: %q", got)
	}
}

func TestGovernedChildFanOutRejectsChangedItemsOnResume(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: waiting-child
nodes:
  - id: approve
    approval:
      message: wait
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: changing-parent
nodes:
  - id: discover
    bash: printf '["one"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	state, err := runner.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected wait, got %v", err)
	}
	state.Nodes["discover"].Output = `["two"]`
	_, err = runner.Resume(context.Background(), state)
	if err == nil || !strings.Contains(err.Error(), "fan-out items changed") {
		t.Fatalf("expected changed-items protection, got %v", err)
	}
}

func TestGovernedChildFanOutRetryCreatesFreshGroup(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry-fanout-child
nodes:
  - id: run
    bash: |
      marker="attempt-${input}"
      if [ ! -f "$marker" ]; then
        touch "$marker"
        exit 8
      fi
      printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: retry-fanout-parent
nodes:
  - id: discover
    bash: printf '["a","b"]'
  - id: execute
    depends_on: [discover]
    attempts:
      max: 2
      retry_on: [exit]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 2
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("retry fan-out did not complete: state=%+v err=%v", state, err)
	}
	node := state.Nodes["execute"]
	if node.Attempts != 2 || len(node.ChildRunIDs) != 4 || len(node.ChildRuns) != 4 {
		t.Fatalf("retry did not preserve two child groups: %+v", node)
	}
	if node.ChildRuns[0].Attempt != 1 || node.ChildRuns[2].Attempt != 2 || node.ChildRuns[0].RunID == node.ChildRuns[2].RunID {
		t.Fatalf("retry reused child IDs: %+v", node.ChildRuns)
	}
}

func TestGovernedChildFanOutRejectsDuplicateItemsByDefault(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: duplicate-child
nodes:
  - id: run
    bash: printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: duplicate-parent
nodes:
  - id: discover
    bash: printf '["code","code"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "duplicate items") || state.Status != store.RunFailed {
		t.Fatalf("expected duplicate-source failure, state=%+v err=%v", state, err)
	}
}

func TestGovernedChildFanOutCanExplicitlyAllowDuplicateItems(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: duplicate-child
nodes:
  - id: run
    bash: printf '%s' '${input}'
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: duplicate-parent
nodes:
  - id: discover
    bash: printf '["code","code"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        allow_duplicates: true
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted || len(state.Nodes["execute"].ChildRuns) != 2 {
		t.Fatalf("explicit duplicate fan-out failed: state=%+v err=%v", state, err)
	}
}

func TestGovernedChildFanOutOneSuccessCancelsUnneededChildren(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: early-success-child
nodes:
  - id: run
    bash: |
      if [ '${input}' = fast ]; then
        sleep 0.05
        printf success
        exit 0
      fi
      sleep 10
      printf slow
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: early-success-parent
nodes:
  - id: discover
    bash: printf '["fast","slow-a","slow-b"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 3
        join: one_success
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("one_success did not terminate early: %s", time.Since(started))
	}
	completed, cancelled := 0, 0
	for _, child := range state.Nodes["execute"].ChildRuns {
		switch child.Status {
		case store.RunCompleted:
			completed++
		case store.RunCancelled:
			cancelled++
			if child.CancelReason != "fanout_result_decided" {
				t.Fatalf("cancel reason=%q want fanout_result_decided", child.CancelReason)
			}
		}
	}
	if completed < 1 || cancelled < 1 {
		t.Fatalf("children=%+v", state.Nodes["execute"].ChildRuns)
	}
}

func TestGovernedChildFanOutAllSuccessCancelsAfterFirstFailure(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: early-failure-child
nodes:
  - id: run
    bash: |
      if [ '${input}' = fail ]; then
        sleep 0.05
        exit 7
      fi
      sleep 10
`)
	mustWriteFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: early-failure-parent
nodes:
  - id: discover
    bash: printf '["fail","slow-a","slow-b"]'
  - id: execute
    depends_on: [discover]
    workflow:
      path: child.yaml
      input: '${fanout.item}'
      isolation: inherit
      fan_out:
        items_from: nodes.discover.output
        max_parallel: 3
        join: all_success
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err == nil || state.Status != store.RunFailed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("all_success did not terminate early: %s", time.Since(started))
	}
	cancelled := 0
	for _, child := range state.Nodes["execute"].ChildRuns {
		if child.Status == store.RunCancelled {
			cancelled++
			if child.CancelReason != "fanout_result_decided" {
				t.Fatalf("cancel reason=%q want fanout_result_decided", child.CancelReason)
			}
		}
	}
	if cancelled < 1 {
		t.Fatalf("children=%+v", state.Nodes["execute"].ChildRuns)
	}
}
