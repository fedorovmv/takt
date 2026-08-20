package authoring

import (
	"strings"
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
)

func TestAnalyzeChecksOutputReferencesAndAuthoringHints(t *testing.T) {
	closed := false
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "produce", Bash: "echo", OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"summary": {Type: "string"}}, Required: []string{"summary"}, AdditionalProperties: &closed}},
		{ID: "consume", DependsOn: []string{"produce"}, Bash: "echo $produce.output.summry", AlwaysRun: true, IdleTimeout: "2m", Timeout: "1m"},
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
		{ID: "consumer", Bash: "echo $source.output"},
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
			{ID: "implement", Prompt: "Use $analyze.output.summary"},
		}}},
	}}
	if diagnostics := Analyze(workflow, command.Resolver{}); HasErrors(diagnostics) {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func TestAnalyzeChecksEveryCompoundWhenReference(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{
		{ID: "source", Bash: "echo"},
		{ID: "consumer", DependsOn: []string{"source"}, Bash: "echo", When: `$source.status == "completed" && $missing.status == "ready"`},
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

func TestAnalyzeRejectsReferencesInInlineScriptSource(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{{
		ID:     "script",
		Script: &spec.ScriptSpec{Runtime: "python", Inline: `print("$ARGUMENTS")`},
	}}}
	diagnostics := Analyze(workflow, command.Resolver{})
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "script.inline_reference" && strings.Contains(diagnostic.Message, "script.args") {
			return
		}
	}
	t.Fatalf("inline reference was accepted: %#v", diagnostics)
}

func TestAnalyzeChecksMatrixBodyTemplates(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{{
		ID: "cases", Matrix: &spec.MatrixSpec{ItemsFrom: "$INPUTS.cases", As: "item", OutputNode: "emit", Nodes: []spec.Node{{
			ID: "emit", Bash: "printf '%s' $MATRIX.item; echo $missing.output",
		}}},
	}}}
	diagnostics := Analyze(workflow, command.Resolver{})
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "template.node_unknown" {
			return
		}
	}
	t.Fatalf("matrix body was not analyzed: %#v", diagnostics)
}

func TestAnalyzeRejectsMatrixReferenceOutsideMatrix(t *testing.T) {
	workflow := &spec.Workflow{Nodes: []spec.Node{{ID: "emit", Bash: "echo $MATRIX.item"}}}
	diagnostics := Analyze(workflow, command.Resolver{})
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "template.matrix_outside_matrix" {
			return
		}
	}
	t.Fatalf("root matrix reference was accepted: %#v", diagnostics)
}
