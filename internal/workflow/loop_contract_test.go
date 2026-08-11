package workflow

import (
	"os"
	"path/filepath"
	"strings"
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

func TestArchonSharedContextChoosesNearestCompatibleAncestor(t *testing.T) {
	wf := &spec.Workflow{Name: "nearest", Nodes: []spec.Node{
		{ID: "a", Prompt: "a", Provider: "p", Model: "m"},
		{ID: "b", DependsOn: []string{"a"}, Prompt: "b", Provider: "p", Model: "m"},
		{ID: "c", DependsOn: []string{"b"}, Prompt: "c", Provider: "p", Model: "m", Context: "shared"},
	}}
	if err := Validate(wf); err != nil {
		t.Fatalf("nearest shared ancestor rejected: %v", err)
	}
}

func TestArchonSharedContextRejectsConcurrentConsumers(t *testing.T) {
	wf := &spec.Workflow{Name: "concurrent", Nodes: []spec.Node{
		{ID: "source", Prompt: "source", Provider: "p", Model: "m"},
		{ID: "left", DependsOn: []string{"source"}, Prompt: "left", Provider: "p", Model: "m", Context: "shared"},
		{ID: "right", DependsOn: []string{"source"}, Prompt: "right", Provider: "p", Model: "m", Context: "shared"},
	}}
	if err := Validate(wf); err == nil {
		t.Fatal("parallel shared consumers accepted")
	}
}

func TestArchonSharedContextAllowsSequentialReuse(t *testing.T) {
	wf := &spec.Workflow{Name: "sequential", Nodes: []spec.Node{
		{ID: "source", Prompt: "source", Provider: "p", Model: "m"},
		{ID: "first", DependsOn: []string{"source"}, Prompt: "first", Provider: "p", Model: "m", Context: "shared"},
		{ID: "second", DependsOn: []string{"first"}, Prompt: "second", Provider: "p", Model: "m", Context: "shared"},
	}}
	if err := Validate(wf); err != nil {
		t.Fatalf("sequential shared reuse rejected: %v", err)
	}
}

func TestArchonLoopChildAllowsCancelAndSharedContext(t *testing.T) {
	wf := &spec.Workflow{Name: "loop-fields", Nodes: []spec.Node{{
		ID: "repair",
		LoopGroup: &spec.LoopGroupSpec{
			MaxIterations: 2,
			Nodes: []spec.Node{
				{ID: "source", Prompt: "source", Provider: "p", Model: "m"},
				{ID: "stop", DependsOn: []string{"source"}, Cancel: "stop", Context: "shared", Provider: "p", Model: "m"},
			},
			Until: spec.UntilSpec{Node: "source", OutputContains: "done"},
		},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatalf("loop child cancel/shared rejected: %v", err)
	}
}

func TestArchonRejectsContainerOnlyFieldsOnOrdinaryNodes(t *testing.T) {
	dir := t.TempDir()
	for _, field := range []string{"fresh_context: false", "fresh_context: true", "until_bash: true"} {
		path := filepath.Join(dir, strings.NewReplacer(" ", "", ":", "-").Replace(field)+".yaml")
		raw := "name: placement\nnodes:\n  - id: node\n    bash: \"true\"\n    " + field + "\n"
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("unsupported field placement accepted for %q", field)
		}
	}
}
