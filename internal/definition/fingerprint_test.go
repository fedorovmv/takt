package definition

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestCommandFingerprintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	cmdDir := filepath.Join(dir, "commands")
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cmdDir, "build.md")
	if err := os.WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "test", Nodes: []spec.Node{{ID: "build", Command: "build"}}}
	cfg := &spec.Config{}
	resolver := command.Resolver{Dirs: []string{cmdDir}}
	before, err := Compute(wf, cfg, "<workflow>", "<config>", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Compute(wf, cfg, "<workflow>", "<config>", resolver)
	if err != nil {
		t.Fatal(err)
	}
	if before.Commands == after.Commands {
		t.Fatal("command fingerprint did not change")
	}
	if err := Verify(before, after); err == nil {
		t.Fatal("expected definition change")
	}
}

func TestGovernedChildWorkflowChangesParentFingerprint(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	if err := os.WriteFile(childPath, []byte("name: child\nnodes:\n  - id: run\n    bash: echo one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parentPath, []byte("name: parent\nnodes:\n  - id: child\n    workflow:\n      path: child.yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	first, err := Compute(wf, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte("name: child\nnodes:\n  - id: run\n    bash: echo two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err = workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compute(wf, &spec.Config{}, parentPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Workflow == second.Workflow {
		t.Fatal("child workflow change did not affect parent fingerprint")
	}
}

func TestScriptFingerprintChangesWithSourceAndDependency(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "script.py")
	depPath := filepath.Join(dir, "schema.json")
	if err := os.WriteFile(scriptPath, []byte("print('one')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(depPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(dir, "workflow.yaml")
	wf := &spec.Workflow{Name: "script", Nodes: []spec.Node{{
		ID: "run", Script: &spec.ScriptSpec{Runtime: "python", Path: "script.py", Dependencies: []string{"schema.json"}},
	}}}
	first, err := Compute(wf, &spec.Config{}, workflowPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(depPath, []byte("{\"changed\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Compute(wf, &spec.Config{}, workflowPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Workflow == second.Workflow {
		t.Fatal("script dependency did not affect workflow fingerprint")
	}
	if err := os.WriteFile(depPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scriptPath, []byte("print('two')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := Compute(wf, &spec.Config{}, workflowPath, "<config>", command.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Workflow == third.Workflow {
		t.Fatal("script source did not affect workflow fingerprint")
	}
}
