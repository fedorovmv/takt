package assistant

import "sort"

const (
	CapabilityAgentEventsV2  = "agent_events_v2"
	CapabilitySessionEvents  = "session_events"
	CapabilityToolEvents     = "tool_events"
	CapabilityToolControl    = "tool_control"
	CapabilityArtifactEvents = "artifact_events"
	CapabilityUsageEvents    = "usage_events"
)

// CapabilityDeclaration describes what an adapter or external worker can
// actually guarantee. Capabilities remains the policy-preflight set; the
// explicit booleans describe event/control semantics and are surfaced through
// MCP so callers do not infer guarantees from adapter names.
type CapabilityDeclaration struct {
	Protocol       string   `json:"protocol"`
	Capabilities   []string `json:"capabilities,omitempty"`
	EventTypes     []string `json:"event_types,omitempty"`
	SessionEvents  bool     `json:"session_events,omitempty"`
	ToolEvents     bool     `json:"tool_events,omitempty"`
	ToolControl    bool     `json:"tool_control,omitempty"`
	ArtifactEvents bool     `json:"artifact_events,omitempty"`
	UsageEvents    bool     `json:"usage_events,omitempty"`
}

func NormalizeDeclaration(value CapabilityDeclaration) CapabilityDeclaration {
	if value.Protocol == "" {
		value.Protocol = EventProtocolV2
	}
	value.Capabilities = uniqueSorted(value.Capabilities)
	value.EventTypes = uniqueSorted(value.EventTypes)
	return value
}

func DeclarationFor(adapter Adapter) CapabilityDeclaration {
	if declarer, ok := adapter.(interface{ CapabilityDeclaration() CapabilityDeclaration }); ok {
		value := declarer.CapabilityDeclaration()
		if len(value.Capabilities) == 0 {
			value.Capabilities = adapter.Capabilities()
		}
		return NormalizeDeclaration(value)
	}
	return NormalizeDeclaration(CapabilityDeclaration{Capabilities: adapter.Capabilities()})
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
