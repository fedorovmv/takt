package runtime

import (
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/command"
	"takt/internal/spec"
)

func TestValidateCapabilitiesFailsBeforeRunCreation(t *testing.T) {
	allowed := []string{"read"}
	workflow := &spec.Workflow{Defaults: spec.Defaults{Assistant: "limited", Model: "demo"}, Nodes: []spec.Node{{ID: "agent", Prompt: "work", AllowedTools: &allowed}}}
	config := &spec.Config{Models: map[string]spec.ModelSpec{"demo": {Provider: "test", ID: "demo"}}, Assistants: map[string]spec.AssistantSpec{"limited": {Type: "process", Argv: []string{"cat"}}}}
	err := ValidateCapabilities(workflow, config, filepath.Join(t.TempDir(), "workflow.yaml"), command.Resolver{})
	if err == nil || !strings.Contains(err.Error(), "tool_policy") {
		t.Fatalf("error = %v", err)
	}
}
