package gobenchmark

import (
	"slices"
	"testing"

	"takt/internal/config"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestStrategiesUseIdenticalImplementationPrompt(t *testing.T) {
	baseline, err := workflow.Load("strategies/baseline-direct.yaml")
	if err != nil {
		t.Fatal(err)
	}
	repair, err := workflow.Load("strategies/feedback-repair.yaml")
	if err != nil {
		t.Fatal(err)
	}

	baselineNode := nodeByID(t, baseline.Nodes, "implement")
	repairNode := nodeByID(t, repair.Nodes, "implement")
	if baselineNode.Prompt != repairNode.Prompt {
		t.Fatal("direct and repair must use the same first-attempt prompt")
	}
}

func TestOpenCodeBenchmarkRunsPureWithoutSkills(t *testing.T) {
	cfg, err := config.Load("config.opencode.yaml")
	if err != nil {
		t.Fatal(err)
	}
	assistant := cfg.Assistants["opencode"]
	if !slices.Equal(assistant.Args, []string{"--pure"}) {
		t.Fatalf("OpenCode args = %v, want [--pure]", assistant.Args)
	}
	for _, path := range []string{"strategies/baseline-direct.yaml", "strategies/feedback-repair.yaml"} {
		wf, err := workflow.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		implement := nodeByID(t, wf.Nodes, "implement")
		if implement.Skills == nil || len(*implement.Skills) != 0 {
			t.Fatalf("%s implement skills = %#v, want explicit empty allowlist", path, implement.Skills)
		}
	}
}

func nodeByID(t *testing.T, nodes []spec.Node, id string) spec.Node {
	t.Helper()
	for _, node := range nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("node %q not found", id)
	return spec.Node{}
}
