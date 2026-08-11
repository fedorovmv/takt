package workflow

import (
	"os"
	"path/filepath"
	"testing"

	"takt/internal/spec"
)

func TestArchonScalarUntilNormalizesToLoopGroup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loop.yaml")
	source := `name: repair
nodes:
  - id: repair
    loop:
      prompt: fix
      until: BUILD-CLEAN
      max_iterations: 3
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	wf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if wf.Nodes[0].LoopGroup == nil || wf.Nodes[0].LoopGroup.Until.Signal != "BUILD-CLEAN" {
		t.Fatalf("normalized loop = %#v", wf.Nodes[0])
	}
}

func TestArchonSharedContextNeedsUniqueAncestor(t *testing.T) {
	valid := &spec.Workflow{Name: "shared", Nodes: []spec.Node{
		{ID: "first", Prompt: "one", Provider: "p", Model: "m"},
		{ID: "second", DependsOn: []string{"first"}, Prompt: "two", Provider: "p", Model: "m", Context: "shared"},
	}}
	if err := Validate(valid); err != nil {
		t.Fatal(err)
	}
	invalid := &spec.Workflow{Name: "invalid", Nodes: []spec.Node{{ID: "shared", Prompt: "two", Context: "shared"}}}
	if err := Validate(invalid); err == nil {
		t.Fatal("shared context without ancestor accepted")
	}
}
