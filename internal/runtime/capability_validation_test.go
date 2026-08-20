package runtime

import (
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/assistant"

	"takt/internal/command"
	"takt/internal/spec"
)

func TestValidateCapabilitiesFailsBeforeRunCreation(t *testing.T) {
	allowed := []string{"read"}
	workflow := &spec.Workflow{Provider: "limited", Model: "demo", Nodes: []spec.Node{{ID: "agent", Prompt: "work", AllowedTools: &allowed}}}
	config := &spec.Config{Models: map[string]spec.ModelSpec{"demo": {Provider: "test", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"limited": {Type: "process", Argv: []string{"cat"}}}}
	err := ValidateCapabilities(workflow, config, filepath.Join(t.TempDir(), "workflow.yaml"), command.Resolver{}, assistant.Factory{Config: config})
	if err == nil || !strings.Contains(err.Error(), "tool_policy") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateCapabilitiesChecksMatrixBody(t *testing.T) {
	allowed := []string{"read"}
	wf := &spec.Workflow{Provider: "limited", Model: "demo", Nodes: []spec.Node{{
		ID: "cases", Matrix: &spec.MatrixSpec{ItemsFrom: "$INPUTS.cases", As: "item", OutputNode: "agent", Nodes: []spec.Node{{
			ID: "agent", Prompt: "work", AllowedTools: &allowed,
		}}},
	}}}
	config := &spec.Config{Models: map[string]spec.ModelSpec{"demo": {Provider: "test", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"limited": {Type: "process", Argv: []string{"cat"}}}}
	err := ValidateCapabilities(wf, config, filepath.Join(t.TempDir(), "workflow.yaml"), command.Resolver{}, assistant.Factory{Config: config})
	if err == nil || !strings.Contains(err.Error(), "tool_policy") {
		t.Fatalf("matrix capability error = %v", err)
	}
}
