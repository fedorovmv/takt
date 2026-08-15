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
	Activity    func(string)
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
	Adapter          string // concrete adapter registration: pi, opencode, process, mock
	SessionPath      string // optional local session file supplied by the adapter
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

type ProviderFactory func(spec.AssistantSpec) Adapter

type Factory struct {
	Config    *spec.Config
	Providers Registry
}

func (f Factory) Resolve(name string) (Adapter, error) {
	resolvedName, s, ok := ResolveConfigured(name, f.Config)
	if !ok {
		return nil, &UnknownAssistantError{Name: name}
	}
	switch s.Type {
	case "mock":
		return Mock{name: resolvedName}, nil
	case "process":
		return Process{spec: s}, nil
	default:
		if provider, ok := f.Providers.Resolve(s.Type, s); ok {
			return provider, nil
		}
		return nil, &UnknownAssistantError{Name: name}
	}
}

func ResolveConfigured(name string, cfg *spec.Config) (string, spec.AssistantSpec, bool) {
	if cfg == nil {
		return "", spec.AssistantSpec{}, false
	}
	if value, ok := cfg.Assistants[name]; ok {
		return name, value, true
	}
	if name != "coding-agent" {
		return "", spec.AssistantSpec{}, false
	}
	if configured := cfg.DefaultAssistant; configured != "" {
		value, ok := cfg.Assistants[configured]
		return configured, value, ok
	}
	// Compatibility for code profiles initialized before logical coding-agent
	// selection was introduced. Existing OpenCode configurations keep working.
	if value, ok := cfg.Assistants["opencode"]; ok {
		return "opencode", value, true
	}
	if len(cfg.Assistants) == 1 {
		for configured, value := range cfg.Assistants {
			return configured, value, true
		}
	}
	return "", spec.AssistantSpec{}, false
}

type UnknownAssistantError struct{ Name string }

func (e *UnknownAssistantError) Error() string { return "unknown assistant: " + e.Name }
