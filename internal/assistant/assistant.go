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

type Adapter interface {
	Run(context.Context, Request) (Result, error)
	Capabilities() []string
}

type Resolver interface {
	Resolve(string) (Adapter, error)
}

type Factory struct {
	Config *spec.Config
}

func (f Factory) Resolve(name string) (Adapter, error) {
	s, ok := f.Config.Assistants[name]
	if !ok {
		return nil, &UnknownAssistantError{Name: name}
	}
	switch s.Type {
	case "mock":
		return Mock{name: name}, nil
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

type UnknownAssistantError struct{ Name string }

func (e *UnknownAssistantError) Error() string { return "unknown assistant: " + e.Name }
