package assistant

import (
	"context"
	"encoding/json"

	"takt/internal/spec"
)

type Request struct {
	RunID       string
	NodeID      string
	Attempt     int
	Prompt      string
	Workspace   string
	ModelName   string
	Model       spec.ModelSpec
	SessionMode string
	SessionID   string
	NativeHooks json.RawMessage
	Policy      Policy
	Metadata    map[string]string
	Emit        EventSink
	ToolControl ToolController
}

type Result struct {
	Output           string
	Structured       json.RawMessage
	SessionID        string
	Resumed          bool
	ExitCode         int
	Stdout           string
	Stderr           string
	AssistantVersion string
	ResolvedModel    *ProtocolModel
	Usage            *ProtocolUsage
	Truncated        bool
}

// SessionAdapter is the provider-neutral execution seam used by Takt.
// Codex, Oh My Pi, Qwen CLI and other coding agents can be connected through
// a built-in adapter or a process wrapper that implements takt-assistant/v1alpha2.
type SessionAdapter interface {
	Run(context.Context, Request) (Result, error)
	Capabilities() []string
}

// Adapter is kept as a source-compatible name for existing integrations.
type Adapter = SessionAdapter

type Resolver interface {
	Resolve(string) (Adapter, error)
}

type Factory struct {
	Config *spec.Config
}

func (f Factory) Resolve(name string) (Adapter, error) {
	resolvedName := name
	s, ok := f.Config.Assistants[resolvedName]
	if !ok && name == "coding-agent" {
		resolvedName, s, ok = f.resolveDefaultAssistant()
	}
	if !ok {
		return nil, &UnknownAssistantError{Name: name}
	}
	switch s.Type {
	case "mock":
		return Mock{name: resolvedName}, nil
	case "process":
		return Process{spec: s}, nil
	case "pi":
		return NewPi(s), nil
	case "opencode":
		return NewOpenCode(s), nil
	default:
		return nil, &UnknownAssistantError{Name: name}
	}
}

func (f Factory) resolveDefaultAssistant() (string, spec.AssistantSpec, bool) {
	if f.Config == nil {
		return "", spec.AssistantSpec{}, false
	}
	if name := f.Config.DefaultAssistant; name != "" {
		value, ok := f.Config.Assistants[name]
		return name, value, ok
	}
	// Compatibility for code profiles initialized before logical coding-agent
	// selection was introduced. Existing OpenCode configurations keep working.
	if value, ok := f.Config.Assistants["opencode"]; ok {
		return "opencode", value, true
	}
	if len(f.Config.Assistants) == 1 {
		for name, value := range f.Config.Assistants {
			return name, value, true
		}
	}
	return "", spec.AssistantSpec{}, false
}

type UnknownAssistantError struct{ Name string }

func (e *UnknownAssistantError) Error() string { return "unknown assistant: " + e.Name }
