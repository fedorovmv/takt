package assistant

import (
	"context"
	"fmt"
	"strings"
)

type Mock struct{ name string }

func (m Mock) Run(_ context.Context, req Request) (Result, error) {
	preview := strings.TrimSpace(req.Prompt)
	runes := []rune(preview)
	if len(runes) > 160 {
		preview = string(runes[:160]) + "..."
	}
	return Result{
		Output:    fmt.Sprintf("mock assistant %s completed: %s", m.name, preview),
		Adapter:   "mock",
		SessionID: req.SessionID,
		ExitCode:  0,
	}, nil
}
func (m Mock) Capabilities() []string {
	return []string{CapabilityToolPolicy, CapabilitySkills, CapabilityMCP, CapabilitySandboxFilesystem, CapabilitySandboxNetwork}
}

func (m Mock) CapabilityDeclaration() CapabilityDeclaration {
	return CapabilityDeclaration{
		Capabilities: m.Capabilities(), EventTypes: []string{EventSessionStarted, EventSessionResumed, EventMessage, EventUsage, EventCompleted, EventFailed},
		SessionEvents: true, UsageEvents: true,
	}
}
