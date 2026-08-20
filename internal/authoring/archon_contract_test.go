package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/workflow"
)

func TestArchonProviderModelPreflight(t *testing.T) {
	cases := []struct {
		name string
		cfg  *spec.Config
		want string
	}{
		{name: "provider", cfg: &spec.Config{Models: map[string]spec.ModelSpec{"large": {}}}, want: "unknown assistant"},
		{name: "model", cfg: &spec.Config{Assistants: map[string]spec.AssistantSpec{"pi": {Type: "pi"}}}, want: "unknown model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "workflow.yaml")
			source := "name: preflight\nprovider: pi\nmodel: large\nnodes:\n  - id: decide\n    prompt: decide\n"
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			wf, err := workflow.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			err = workflow.ValidateReferences(wf, tc.cfg, command.Resolver{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateReferences() error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestArchonReservedNodeIDsAreRejected(t *testing.T) {
	for _, id := range []string{"ARGUMENTS", "ARTIFACTS_DIR", "BASE_BRANCH", "INPUTS", "LOOP_PREV", "FEEDBACK", "FANOUT", "MATRIX"} {
		wf := &spec.Workflow{Nodes: []spec.Node{{ID: id, Bash: "true"}}}
		if diagnostics := Analyze(wf, command.Resolver{}); !hasArchonDiagnostic(diagnostics, "node.id_reserved") {
			t.Errorf("node %q diagnostics = %#v", id, diagnostics)
		}
	}
}

func TestArchonNestedOutputPathIsChecked(t *testing.T) {
	closed := false
	wf := &spec.Workflow{Nodes: []spec.Node{
		{ID: "produce", Bash: "true", OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{
			"result": {Type: "object", Properties: map[string]spec.OutputFormat{"status": {Type: "string"}}, AdditionalProperties: &closed},
		}, AdditionalProperties: &closed}},
		{ID: "consume", DependsOn: []string{"produce"}, Prompt: "$produce.output.result.missing"},
	}}
	if diagnostics := Analyze(wf, command.Resolver{}); !hasArchonDiagnostic(diagnostics, "template.output_path_unknown") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestArchonLegacyReferencesAreRejected(t *testing.T) {
	wf := &spec.Workflow{Nodes: []spec.Node{
		{ID: "produce", Bash: "true"},
		{ID: "consume", DependsOn: []string{"produce"}, Prompt: "${nodes.produce.output}"},
	}}
	if diagnostics := Analyze(wf, command.Resolver{}); !hasArchonDiagnostic(diagnostics, "template.reference_legacy") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func hasArchonDiagnostic(values []Diagnostic, code string) bool {
	for _, value := range values {
		if value.Code == code && value.Severity == "error" {
			return true
		}
	}
	return false
}
