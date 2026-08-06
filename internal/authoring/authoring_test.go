package authoring

import (
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
)

func TestAnalyzeChecksOutputReferencesAndAuthoringHints(t *testing.T) {
	closed := false
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "produce", Bash: "echo", OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"summary": {Type: "string"}}, Required: []string{"summary"}, AdditionalProperties: &closed}},
		{ID: "consume", DependsOn: []string{"produce"}, Bash: "echo ${nodes.produce.output.summry}", AlwaysRun: true, IdleTimeout: "2m", Timeout: "1m"},
	}}
	diagnostics := Analyze(workflow, command.Resolver{})
	codes := map[string]string{}
	for _, diagnostic := range diagnostics {
		codes[diagnostic.Code] = diagnostic.Severity
	}
	if codes["template.output_path_unknown"] != "error" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if codes["idle_timeout.ineffective"] != "warning" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestAnalyzeRejectsNonUpstreamReference(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "source", Bash: "echo"},
		{ID: "consumer", Bash: "echo ${nodes.source.output}"},
	}}
	if diagnostics := Analyze(workflow, command.Resolver{}); !HasErrors(diagnostics) {
		t.Fatalf("expected error, got %#v", diagnostics)
	}
}

func TestAnalyzeAllowsLoopNodeToReferenceContainerUpstream(t *testing.T) {
	closed := false
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "analyze", Bash: "echo", OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"summary": {Type: "string"}}, Required: []string{"summary"}, AdditionalProperties: &closed}},
		{ID: "cycle", DependsOn: []string{"analyze"}, LoopGroup: &spec.LoopGroupSpec{MaxIterations: 2, Nodes: []spec.Node{
			{ID: "implement", Prompt: "Use ${nodes.analyze.output.summary}"},
		}}},
	}}
	if diagnostics := Analyze(workflow, command.Resolver{}); HasErrors(diagnostics) {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func TestAnalyzeChecksEveryCompoundWhenReference(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "source", Bash: "echo"},
		{ID: "consumer", DependsOn: []string{"source"}, Bash: "echo", When: `nodes.source.status == "completed" && nodes.missing.status == "ready"`},
	}}
	diagnostics := Analyze(workflow, command.Resolver{})
	found := false
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "when.node_unknown" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unknown node in compound when, got %#v", diagnostics)
	}
}
