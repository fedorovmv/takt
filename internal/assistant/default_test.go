package assistant

import (
	"testing"

	"takt/internal/spec"
)

func TestCodingAgentResolvesConfiguredDefault(t *testing.T) {
	factory := Factory{Config: &spec.Config{
		DefaultAssistant: "codex",
		Assistants: map[string]spec.AssistantSpec{
			"codex": {Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"codex-takt-adapter"}},
			"qwen":  {Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"qwen-takt-adapter"}},
		},
	}}
	adapter, err := factory.Resolve("coding-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(Process); !ok {
		t.Fatalf("adapter type = %T", adapter)
	}
}

func TestCodingAgentFallsBackToLegacyOpenCode(t *testing.T) {
	factory := Factory{Config: &spec.Config{Assistants: map[string]spec.AssistantSpec{
		"opencode": {Type: "opencode", Binary: "opencode"},
		"pi":       {Type: "pi", Binary: "pi"},
	}}}
	adapter, err := factory.Resolve("coding-agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := adapter.(OpenCode); !ok {
		t.Fatalf("adapter type = %T", adapter)
	}
}

func TestCodingAgentRejectsAmbiguousConfiguration(t *testing.T) {
	factory := Factory{Config: &spec.Config{Assistants: map[string]spec.AssistantSpec{
		"codex": {Type: "process", Argv: []string{"codex"}},
		"qwen":  {Type: "process", Argv: []string{"qwen"}},
	}}}
	if _, err := factory.Resolve("coding-agent"); err == nil {
		t.Fatal("expected ambiguous coding-agent resolution to fail")
	}
}
