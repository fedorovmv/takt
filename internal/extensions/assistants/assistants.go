package assistants

import (
	"context"
	"fmt"

	core "takt/internal/assistant"
	"takt/internal/extensions/assistants/opencode"
	"takt/internal/extensions/assistants/pi"
	"takt/internal/spec"
)

// Factories returns the optional bundled coding-agent adapters. Stable core
// understands only the provider-neutral assistant contract; concrete CLIs are
// wired from the extensions layer.
func Factories() map[string]core.ProviderFactory {
	return map[string]core.ProviderFactory{
		"pi":       func(s spec.AssistantSpec) core.Adapter { return pi.NewPi(s) },
		"opencode": func(s spec.AssistantSpec) core.Adapter { return opencode.NewOpenCode(s) },
	}
}

func ProbeVersion(ctx context.Context, assistantSpec spec.AssistantSpec, workspace string) (string, error) {
	switch assistantSpec.Type {
	case "pi":
		return pi.ProbeVersion(ctx, assistantSpec, workspace)
	case "opencode":
		return opencode.ProbeVersion(ctx, assistantSpec, workspace)
	default:
		return "", fmt.Errorf("assistant type %q has no bundled version probe", assistantSpec.Type)
	}
}
