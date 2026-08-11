package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestScriptCommandStructuredOutputCreatesTypedArtifact(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "emit.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '{\"value\": 7, \"name\": \"ok\"}\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "script-artifact"},
		Nodes: []spec.Node{{
			ID:     "emit",
			Script: &spec.ScriptSpec{Runtime: "command", Path: "emit.sh"},
			OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{
				"value": {Type: "integer"}, "name": {Type: "string"},
			}, Required: []string{"value", "name"}},
			OutputType: "analysis", OutputMIME: "application/json",
		}},
	}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), "<config>", dir)
	state, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["emit"]
	if node.Output != `{"name":"ok","value":7}` {
		t.Fatalf("structured output was not normalized: %q", node.Output)
	}
	if !strings.Contains(node.Stdout, `"value": 7`) {
		t.Fatalf("raw stdout was not preserved: %q", node.Stdout)
	}
	if len(node.Artifacts) != 1 || len(state.Artifacts) != 1 {
		t.Fatalf("artifact was not registered: node=%+v run=%+v", node.Artifacts, state.Artifacts)
	}
	artifact := node.Artifacts[0]
	if artifact.Type != "analysis" || artifact.MIME != "application/json" || artifact.ProducerRunID != state.ID || artifact.ProducerNodeID != "emit" {
		t.Fatalf("unexpected artifact metadata: %+v", artifact)
	}
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != node.Output {
		t.Fatalf("artifact content mismatch: %q", raw)
	}
	sum := sha256.Sum256(raw)
	if artifact.SHA256 != hex.EncodeToString(sum[:]) || artifact.Size != int64(len(raw)) {
		t.Fatalf("artifact checksum/size mismatch: %+v", artifact)
	}
	events, err := (store.FS{Workspace: dir}).ReadEvents(state.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundArtifactEvent := false
	for _, event := range events {
		if event.Type == "assistant.artifact.declared" && event.NodeID == "emit" {
			foundArtifactEvent = true
			break
		}
	}
	if !foundArtifactEvent {
		t.Fatalf("assistant.artifact.declared event is missing: %+v", events)
	}
}

func TestScriptCanRegisterFileArtifact(t *testing.T) {
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "report.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\nprintf '# Report\\nhello\\n' > report.md\nprintf 'done\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "file-artifact"}, Nodes: []spec.Node{{
		ID: "report", Script: &spec.ScriptSpec{Runtime: "command", Path: "report.sh"},
		OutputType: "report", OutputMIME: "text/markdown", OutputPath: "report.md",
	}}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), "<config>", dir)
	state, err := runner.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	artifact := state.Nodes["report"].Artifacts[0]
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# Report\nhello\n" {
		t.Fatalf("unexpected copied artifact: %q", raw)
	}
	if filepath.Clean(artifact.Path) == filepath.Join(dir, "report.md") {
		t.Fatalf("artifact was not copied into run storage: %s", artifact.Path)
	}
}

func TestGovernedChildArtifactIsAvailableToParentTemplate(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.yaml")
	parentPath := filepath.Join(dir, "parent.yaml")
	mustWriteFile(t, childPath, `name: child
nodes:
  - id: plan
    bash: printf '# Plan\nchild\n'
    output_type: plan
    output_mime: text/markdown
`)
	mustWriteFile(t, parentPath, `name: parent
nodes:
  - id: child
    workflow:
      path: child.yaml
      isolation: inherit
  - id: consume
    depends_on: [child]
    bash: cat $child.artifacts.plan.path
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
	childNode := state.Nodes["child"]
	if len(childNode.Artifacts) != 1 || childNode.Artifacts[0].ProducerRunID == state.ID {
		t.Fatalf("child artifacts were not propagated as references: %+v", childNode.Artifacts)
	}
	if state.Nodes["consume"].Output != "# Plan\nchild" {
		t.Fatalf("parent could not consume child artifact: %q", state.Nodes["consume"].Output)
	}
}

func TestArtifactPublicViewPreservesMetadata(t *testing.T) {
	state := &store.RunState{Artifacts: []store.ArtifactRef{{ID: "a", Type: "plan", Path: "/tmp/plan"}}, Nodes: map[string]*store.NodeState{
		"n": {Status: store.NodeCompleted, Artifacts: []store.ArtifactRef{{ID: "a", Type: "plan", Path: "/tmp/plan"}}},
	}}
	public := state.PublicView()
	if len(public.Artifacts) != 1 || len(public.Nodes["n"].Artifacts) != 1 {
		t.Fatalf("artifact metadata lost in public view: %+v", public)
	}
}

func TestScriptLanguageRuntimes(t *testing.T) {
	dir := t.TempDir()
	goPath := filepath.Join(dir, "emit.go")
	if err := os.WriteFile(goPath, []byte("package main\nimport \"fmt\"\nfunc main(){fmt.Print(\"go-ok\")}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		binary   string
		script   *spec.ScriptSpec
		expected string
	}{
		{name: "python-inline", binary: "python3", script: &spec.ScriptSpec{Runtime: "python", Inline: `print("python-ok", end="")`}, expected: "python-ok"},
		{name: "node-inline", binary: "node", script: &spec.ScriptSpec{Runtime: "node", Inline: `process.stdout.write("node-ok")`}, expected: "node-ok"},
		{name: "go-file", binary: "go", script: &spec.ScriptSpec{Runtime: "go", Path: "emit.go"}, expected: "go-ok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := exec.LookPath(tc.binary); err != nil {
				t.Skipf("%s is not installed", tc.binary)
			}
			wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: tc.name}, Nodes: []spec.Node{{ID: "run", Script: tc.script}}}
			runner := New(wf, &spec.Config{}, filepath.Join(dir, "workflow.yaml"), "<config>", dir)
			state, err := runner.Start(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			if got := state.Nodes["run"].Output; got != tc.expected {
				t.Fatalf("output=%q want=%q", got, tc.expected)
			}
		})
	}
}

func TestResolveArtifactSourcePathUsesExistingSymlinkPrefix(t *testing.T) {
	realRoot := t.TempDir()
	aliasParent := t.TempDir()
	alias := filepath.Join(aliasParent, "workspace")
	if err := os.Symlink(realRoot, alias); err != nil {
		t.Fatal(err)
	}
	runner := &Runner{workspace: alias}
	artifactsDir := filepath.Join(alias, ".takt", "runs", "run-test", "artifacts")
	resolved, err := runner.resolveArtifactSourcePath(filepath.Join(alias, "future", "validation.json"), artifactsDir)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wantRoot, "future", "validation.json")
	if resolved != want {
		t.Fatalf("resolved=%q want=%q", resolved, want)
	}
}
