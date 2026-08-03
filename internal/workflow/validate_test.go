package workflow

import (
	"strings"
	"takt/internal/spec"
	"testing"
)

func TestValidateDetectsCycle(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "x"}, Nodes: []spec.Node{{ID: "a", Bash: "true", DependsOn: []string{"b"}}, {ID: "b", Bash: "true", DependsOn: []string{"a"}}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestRejectsInvalidTimeout(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "bad-timeout"}, Nodes: []spec.Node{{ID: "n", Bash: "true", Timeout: "never"}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestValidateRejectsNestedLoopGroups(t *testing.T) {
	zero := 0
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "nested"}, Nodes: []spec.Node{{
		ID: "outer", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "inner", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "check", Bash: "true"}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
		}}, Until: spec.UntilSpec{Node: "inner", ExitCode: &zero}},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "nested loop_group") {
		t.Fatalf("expected nested loop validation error, got %v", err)
	}
}
