package assistants

import (
	core "takt/internal/assistant"
	"takt/internal/extensions/assistants/opencode"
	"takt/internal/extensions/assistants/pi"
	"takt/internal/spec"
)

// Registrations declares the bundled coding-agent integrations. The returned
// slice is data only; production assembles the immutable registry in bootstrap.
func Registrations() []core.ProviderRegistration {
	return []core.ProviderRegistration{
		{
			ID: "pi", DisplayName: "Pi coding agent", Stage: core.ProviderStageExtension,
			Factory:      func(s spec.AssistantSpec) core.Adapter { return pi.NewPi(s) },
			ProbeVersion: pi.ProbeVersion,
		},
		{
			ID: "opencode", DisplayName: "OpenCode", Stage: core.ProviderStageExtension,
			Factory:      func(s spec.AssistantSpec) core.Adapter { return opencode.NewOpenCode(s) },
			ProbeVersion: opencode.ProbeVersion,
		},
	}
}
