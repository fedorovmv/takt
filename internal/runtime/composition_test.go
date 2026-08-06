package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestSubworkflowRunsOnParentSchedulerAndResumesApproval(t *testing.T) {
	dir := t.TempDir()
	writeCompositionFile(t, filepath.Join(dir, "child.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: prepare
    bash: |
      printf '%s' '${inputs.value}' > value.txt
  - id: approve
    depends_on: [prepare]
    approval:
      message: Approve ${inputs.value}?
      capture_response: true
  - id: result
    depends_on: [approve]
    bash: cat value.txt
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
      inputs:
        value: ${input}
  - id: final
    depends_on: [child]
    bash: |
      test "${nodes.child.output}" = "hello"
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "hello")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected approval wait, got %v", err)
	}
	if state.Waiting == nil || state.Waiting.NodeID != "child__approve" {
		t.Fatalf("unexpected waiting state: %+v", state.Waiting)
	}
	state.Approvals[state.Waiting.NodeID] = "approved"
	state.Nodes[state.Waiting.NodeID].Status = store.NodePending
	state.Waiting = nil
	state.Status = store.RunRunning
	if err := r.Store.Save(state); err != nil {
		t.Fatal(err)
	}
	state, err = r.Resume(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Nodes["child"].Output != "hello" || state.Nodes["final"].Status != store.NodeCompleted {
		t.Fatalf("unexpected completed state: %+v", state)
	}
}

func TestForeachRunsItemsSequentiallyAndCollectsOutputs(t *testing.T) {
	dir := t.TempDir()
	writeCompositionFile(t, filepath.Join(dir, "item.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: item
nodes:
  - id: append
    bash: |
      printf '%s\n' '${inputs.value}' >> order.txt
  - id: result
    depends_on: [append]
    bash: printf '%s' '${inputs.value}'
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: foreach
nodes:
  - id: batch
    foreach:
      items:
        - one
        - two
        - three
      subworkflow:
        path: item.yaml
        inputs:
          value: ${item}
  - id: verify
    depends_on: [batch]
    bash: |
      test "$(tr '\n' ',' < order.txt)" = "one,two,three,"
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("unexpected run state: %+v", state)
	}
	if state.Nodes["batch"].Output != `["one","two","three"]` {
		t.Fatalf("unexpected foreach output %q", state.Nodes["batch"].Output)
	}
	if state.Nodes["batch__001"].Status != store.NodeCompleted || state.Nodes["batch__002"].Status != store.NodeCompleted || state.Nodes["batch__003"].Status != store.NodeCompleted {
		t.Fatalf("iteration states were not completed: %+v", state.Nodes)
	}
}

func writeCompositionFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubworkflowDefinitionChangeBlocksResume(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	writeCompositionFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: wait
    approval:
      message: Continue?
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected waiting run, got %v", err)
	}
	writeCompositionFile(t, childPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: wait
    approval:
      message: Changed?
`)
	changed, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r2 := New(changed, &spec.Config{}, workflowPath, "<config>", dir)
	if _, err := r2.Resume(context.Background(), state); err == nil || !strings.Contains(err.Error(), "workflow definition changed") {
		t.Fatalf("expected workflow fingerprint error, got %v", err)
	}
}

func TestLoopGroupRunsComposedChild(t *testing.T) {
	dir := t.TempDir()
	writeCompositionFile(t, filepath.Join(dir, "step.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: step
nodes:
  - id: result
    bash: echo done
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: loop-composition
nodes:
  - id: retry
    loop_group:
      max_iterations: 2
      nodes:
        - id: child
          subworkflow:
            path: step.yaml
      until:
        node: child
        output_contains: done
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || state.Nodes["retry"].Output != "done" {
		t.Fatalf("unexpected loop state: %+v", state)
	}
	previous := state.Nodes["retry"].LoopPrevious
	if previous["retry__child"].Status != store.NodeCompleted {
		t.Fatalf("public composed child is missing from loop state: %+v", previous)
	}
	if !previous["retry__child__result"].Hidden {
		t.Fatalf("expanded child state is not marked internal: %+v", previous["retry__child__result"])
	}
}

func TestPublicRunStateHidesExpandedNodesAndAliasesApproval(t *testing.T) {
	dir := t.TempDir()
	writeCompositionFile(t, filepath.Join(dir, "child.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: child
nodes:
  - id: approve
    approval:
      message: Continue?
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent
nodes:
  - id: child
    subworkflow:
      path: child.yaml
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	r := New(wf, &spec.Config{}, workflowPath, "<config>", dir)
	state, err := r.Start(context.Background(), "")
	if !errors.Is(err, ErrWaiting) {
		t.Fatalf("expected waiting state, got %v", err)
	}
	public := state.PublicView()
	if public.Waiting == nil || public.Waiting.NodeID != "child" {
		t.Fatalf("approval alias was not exposed: %+v", public.Waiting)
	}
	if len(public.Nodes) != 1 || public.Nodes["child"] == nil {
		t.Fatalf("expanded nodes leaked into public state: %+v", public.Nodes)
	}
}

func TestPublicRunStateUsesLocalLoopChildIDs(t *testing.T) {
	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: loop-public-view
nodes:
  - id: retry
    loop_group:
      max_iterations: 1
      nodes:
        - id: result
          bash: echo done
      until:
        node: result
        output_contains: done
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, workflowPath, "<config>", dir).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	public := state.PublicView()
	previous := public.Nodes["retry"].LoopPrevious
	if _, ok := previous["result"]; !ok {
		t.Fatalf("public loop child ID is missing: %+v", previous)
	}
	for id := range previous {
		if strings.Contains(id, "__") {
			t.Fatalf("public loop state exposes expanded ID %q", id)
		}
	}
}

func TestForeachParallelRunsIterationsConcurrentlyAndCollectsInInputOrder(t *testing.T) {
	dir := t.TempDir()
	writeCompositionFile(t, filepath.Join(dir, "item.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: item
nodes:
  - id: result
    bash: |
      touch "$ARTIFACTS_DIR/${inputs.value}.ready"
      i=0
      while { [ ! -f "$ARTIFACTS_DIR/one.ready" ] || [ ! -f "$ARTIFACTS_DIR/two.ready" ]; } && [ "$i" -lt 200 ]; do
        i=$((i + 1))
        sleep 0.01
      done
      test -f "$ARTIFACTS_DIR/one.ready"
      test -f "$ARTIFACTS_DIR/two.ready"
      printf '%s' '${inputs.value}'
`)
	workflowPath := filepath.Join(dir, "workflow.yaml")
	writeCompositionFile(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parallel-foreach
nodes:
  - id: batch
    foreach:
      parallel: true
      items: [one, two]
      subworkflow:
        path: item.yaml
        inputs:
          value: ${item}
`)
	wf, err := workflow.Load(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, workflowPath, "<config>", dir).Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Nodes["batch"].Output != `["one","two"]` {
		t.Fatalf("parallel foreach did not cross the shared barrier in input order: %+v", state.Nodes["batch"])
	}
}

func TestSubworkflowRebasesScriptPathAndDependencies(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "nested")
	if err := os.MkdirAll(filepath.Join(childDir, "tools"), 0o755); err != nil {
		t.Fatal(err)
	}
	toolPath := filepath.Join(childDir, "tools", "emit.sh")
	writeCompositionFile(t, toolPath, "#!/bin/sh\nprintf nested-ok\n")
	if err := os.Chmod(toolPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCompositionFile(t, filepath.Join(childDir, "tools", "value.txt"), "nested-ok")
	writeCompositionFile(t, filepath.Join(childDir, "child.yaml"), `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: nested-script
nodes:
  - id: run
    script:
      runtime: command
      path: tools/emit.sh
      dependencies: [tools/value.txt]
`)
	parentPath := filepath.Join(dir, "parent.yaml")
	writeCompositionFile(t, parentPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: parent-script
nodes:
  - id: nested
    subworkflow:
      path: nested/child.yaml
  - id: verify
    depends_on: [nested]
    bash: test '${nodes.nested.output}' = nested-ok
`)
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, parentPath, "<config>", dir).Start(context.Background(), "")
	if err != nil || state.Status != store.RunCompleted {
		t.Fatalf("nested script composition failed: state=%+v err=%v", state, err)
	}
}
