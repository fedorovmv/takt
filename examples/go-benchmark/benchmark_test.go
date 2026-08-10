package gobenchmark

import (
	"testing"

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
