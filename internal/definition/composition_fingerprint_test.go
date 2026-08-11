package definition_test

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/command"
	"takt/internal/definition"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestSubworkflowLocalCommandChangesWorkflowFingerprint(t *testing.T) {
	dir := t.TempDir()
	childDir := filepath.Join(dir, "child")
	if err := os.MkdirAll(filepath.Join(childDir, "commands"), 0o755); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(dir, "workflow.yaml")
	childPath := filepath.Join(childDir, "workflow.yaml")
	commandPath := filepath.Join(childDir, "commands", "run.md")
	mustWrite(t, parentPath, `name: parent
nodes:
  - id: child
    subworkflow:
      path: child/workflow.yaml
`)
	mustWrite(t, childPath, `name: child
nodes:
  - id: run
    command: run
`)
	mustWrite(t, commandPath, "first")
	beforeWorkflow, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := definition.Compute(beforeWorkflow, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, commandPath, "second")
	afterWorkflow, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := definition.Compute(afterWorkflow, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Workflow == after.Workflow {
		t.Fatal("subworkflow-local command did not change workflow fingerprint")
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestForeachItemsFileChangesWorkflowFingerprint(t *testing.T) {
	dir := t.TempDir()
	itemsPath := filepath.Join(dir, "items.json")
	if err := os.WriteFile(itemsPath, []byte(`["one"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	childPath := filepath.Join(dir, "child.yaml")
	if err := os.WriteFile(childPath, []byte(`name: child
nodes:
  - id: result
    bash: echo '$INPUTS.value'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(parentPath, []byte(`name: parent
nodes:
  - id: batch
    foreach:
      items_from:
        path: items.json
      subworkflow:
        path: child.yaml
        inputs:
          value: $INPUTS.item
`), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeWorkflow, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := definition.Compute(beforeWorkflow, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(itemsPath, []byte(`["one","two"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	afterWorkflow, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := definition.Compute(afterWorkflow, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if before.Workflow == after.Workflow {
		t.Fatal("foreach items source did not change workflow fingerprint")
	}
}
