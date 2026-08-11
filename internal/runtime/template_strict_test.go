package runtime

import (
	"strings"
	"testing"

	"takt/internal/store"
)

func TestStrictTemplateRequiredOptionalAndDefault(t *testing.T) {
	state := &store.RunState{Input: "request", Nodes: map[string]*store.NodeState{
		"producer": {Output: `{"summary":"ok","empty":""}`, Status: store.NodeCompleted},
	}, Approvals: map[string]string{}}
	value, err := renderTemplate("$producer.output.summary|$producer.output.missing?|$producer.output.empty:-fallback|$producer.output.missing:-default", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "ok||fallback|default" {
		t.Fatalf("rendered = %q", value)
	}
	_, err = renderTemplate("$producer.output.missing", state, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "unresolved reference") {
		t.Fatalf("expected unresolved error, got %v", err)
	}
}

func TestApprovalOutputFallsBackToDurableAnswer(t *testing.T) {
	state := &store.RunState{Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{"confirm": "yes"}}
	value, err := renderTemplate("$confirm.output", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "yes" {
		t.Fatalf("approval output = %q", value)
	}
}

func TestLoopPreviousIsImplicitlyEmptyOnFirstIteration(t *testing.T) {
	state := &store.RunState{Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
	value, err := renderTemplate("$LOOP_PREV.review.output", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "" {
		t.Fatalf("first iteration loop previous = %q, want empty", value)
	}
}
