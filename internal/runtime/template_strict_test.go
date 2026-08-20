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

func TestExecutionWorkspaceTemplateReference(t *testing.T) {
	state := &store.RunState{Workspace: "/control", ExecutionWorkspace: "/execution", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
	value, err := renderTemplate("$TAKT_WORKSPACE", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "/execution" {
		t.Fatalf("workspace = %q, want /execution", value)
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

func TestJSONInputReferencesRequireJSONContract(t *testing.T) {
	jsonState := &store.RunState{Input: `{"cases":[{"name":"alpha"}]}`, InputFormat: "json", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
	value, err := renderTemplate("$INPUTS.cases.0.name", jsonState, nil, "", "")
	if err != nil || value != "alpha" {
		t.Fatalf("JSON input value=%q err=%v", value, err)
	}
	textState := &store.RunState{Input: jsonState.Input, InputFormat: "text", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
	if _, err := renderTemplate("$INPUTS.cases.0.name", textState, nil, "", ""); err == nil {
		t.Fatal("nested input reference was accepted for text input")
	}
}
