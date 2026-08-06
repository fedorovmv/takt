package assistant

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	EventSessionStarted   = "session.started"
	EventSessionResumed   = "session.resumed"
	EventMessage          = "message"
	EventToolRequested    = "tool.requested"
	EventToolAllowed      = "tool.allowed"
	EventToolDenied       = "tool.denied"
	EventToolStarted      = "tool.started"
	EventToolCompleted    = "tool.completed"
	EventArtifactDeclared = "artifact.declared"
	EventUsage            = "usage"
	EventDiagnostic       = "diagnostic"
	EventCompleted        = "completed"
	EventFailed           = "failed"

	// EventStarted is retained as a source-compatible alias. New durable events
	// use session.started/session.resumed so session lifecycle is explicit.
	EventStarted = EventSessionStarted
)

const EventProtocolV2 = "takt-agent-events/v2"

// ArtifactDeclaration is the provider-neutral declaration of an artifact
// observed or produced by an assistant tool call. Runtime-owned artifacts are
// subsequently represented by store.ArtifactRef; this contract intentionally
// avoids depending on the store package.
type ArtifactDeclaration struct {
	ID       string         `json:"id,omitempty"`
	Type     string         `json:"type"`
	MIME     string         `json:"mime,omitempty"`
	Path     string         `json:"path,omitempty"`
	SHA256   string         `json:"sha256,omitempty"`
	Size     int64          `json:"size,omitempty"`
	CallID   string         `json:"call_id,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Event is the provider-neutral observation and control contract persisted by
// Takt. Tool lifecycle events always carry a stable call_id.
type Event struct {
	Type      string               `json:"type"`
	Time      time.Time            `json:"time,omitempty"`
	Message   string               `json:"message,omitempty"`
	Tool      string               `json:"tool,omitempty"`
	CallID    string               `json:"call_id,omitempty"`
	Input     json.RawMessage      `json:"input,omitempty"`
	Output    json.RawMessage      `json:"output,omitempty"`
	Usage     *ProtocolUsage       `json:"usage,omitempty"`
	Artifact  *ArtifactDeclaration `json:"artifact,omitempty"`
	Decision  string               `json:"decision,omitempty"`
	Reason    string               `json:"reason,omitempty"`
	Data      map[string]any       `json:"data,omitempty"`
	Provider  string               `json:"provider,omitempty"`
	SessionID string               `json:"session_id,omitempty"`
}

type EventSink func(Event)

func EventTypes() []string {
	out := []string{
		EventSessionStarted, EventSessionResumed, EventMessage,
		EventToolRequested, EventToolAllowed, EventToolDenied,
		EventToolStarted, EventToolCompleted, EventArtifactDeclared,
		EventUsage, EventDiagnostic, EventCompleted, EventFailed,
	}
	sort.Strings(out)
	return out
}

func ValidEventType(value string) bool {
	switch value {
	case EventSessionStarted, EventSessionResumed, EventMessage,
		EventToolRequested, EventToolAllowed, EventToolDenied,
		EventToolStarted, EventToolCompleted, EventArtifactDeclared,
		EventUsage, EventDiagnostic, EventCompleted, EventFailed:
		return true
	default:
		return false
	}
}

func ValidateEvent(event Event) error {
	if !ValidEventType(event.Type) {
		return fmt.Errorf("unsupported assistant event type %q", event.Type)
	}
	if strings.HasPrefix(event.Type, "tool.") {
		if strings.TrimSpace(event.Tool) == "" {
			return fmt.Errorf("assistant event %s requires tool", event.Type)
		}
		if strings.TrimSpace(event.CallID) == "" {
			return fmt.Errorf("assistant event %s requires call_id", event.Type)
		}
	}
	if event.Type == EventToolAllowed && event.Decision != "" && event.Decision != "allow" {
		return fmt.Errorf("assistant event %s decision must be allow", event.Type)
	}
	if event.Type == EventToolDenied && event.Decision != "" && event.Decision != "deny" && event.Decision != "cancel" {
		return fmt.Errorf("assistant event %s decision must be deny or cancel", event.Type)
	}
	if event.Type == EventArtifactDeclared {
		if event.Artifact == nil {
			return fmt.Errorf("assistant event %s requires artifact", event.Type)
		}
		if strings.TrimSpace(event.Artifact.Type) == "" {
			return fmt.Errorf("assistant artifact declaration requires type")
		}
		if event.Artifact.Size < 0 {
			return fmt.Errorf("assistant artifact size cannot be negative")
		}
		if event.CallID != "" && event.Artifact.CallID != "" && event.CallID != event.Artifact.CallID {
			return fmt.Errorf("assistant artifact call_id differs from event call_id")
		}
	}
	if event.Usage != nil {
		if event.Usage.InputTokens < 0 || event.Usage.OutputTokens < 0 || event.Usage.Cost < 0 {
			return fmt.Errorf("assistant event usage cannot be negative")
		}
	}
	return nil
}

// EventData converts an event into its durable store representation without
// coupling the assistant package to the store package.
func EventData(event Event) map[string]any {
	data := map[string]any{}
	if event.Message != "" {
		data["message"] = event.Message
	}
	if event.Tool != "" {
		data["tool"] = event.Tool
	}
	if event.CallID != "" {
		data["call_id"] = event.CallID
	}
	if len(event.Input) > 0 {
		var value any
		if json.Unmarshal(event.Input, &value) == nil {
			data["input"] = value
		}
	}
	if len(event.Output) > 0 {
		var value any
		if json.Unmarshal(event.Output, &value) == nil {
			data["output"] = value
		}
	}
	if event.Usage != nil {
		data["usage"] = event.Usage
	}
	if event.Artifact != nil {
		data["artifact"] = event.Artifact
	}
	if event.Decision != "" {
		data["decision"] = event.Decision
	}
	if event.Reason != "" {
		data["reason"] = event.Reason
	}
	if event.Provider != "" {
		data["provider"] = event.Provider
	}
	if event.SessionID != "" {
		data["session_id"] = event.SessionID
	}
	for key, value := range event.Data {
		if _, exists := data[key]; !exists {
			data[key] = value
		}
	}
	return data
}

func Emit(req Request, event Event) {
	if req.Emit == nil {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	req.Emit(event)
}
