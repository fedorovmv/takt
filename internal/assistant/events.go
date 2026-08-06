package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	EventStarted       = "started"
	EventMessage       = "message"
	EventToolStarted   = "tool.started"
	EventToolCompleted = "tool.completed"
	EventUsage         = "usage"
	EventDiagnostic    = "diagnostic"
	EventCompleted     = "completed"
	EventFailed        = "failed"
)

// Event is the provider-neutral observation contract persisted by Takt. It is
// deliberately smaller than any one provider stream and can also be emitted by
// an external node executor through MCP.
type Event struct {
	Type      string          `json:"type"`
	Time      time.Time       `json:"time,omitempty"`
	Message   string          `json:"message,omitempty"`
	Tool      string          `json:"tool,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`
	Usage     *ProtocolUsage  `json:"usage,omitempty"`
	Data      map[string]any  `json:"data,omitempty"`
	Provider  string          `json:"provider,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
}

type EventSink func(Event)

func ValidEventType(value string) bool {
	switch value {
	case EventStarted, EventMessage, EventToolStarted, EventToolCompleted, EventUsage, EventDiagnostic, EventCompleted, EventFailed:
		return true
	default:
		return false
	}
}

func ValidateEvent(event Event) error {
	if !ValidEventType(event.Type) {
		return fmt.Errorf("unsupported assistant event type %q", event.Type)
	}
	if strings.HasPrefix(event.Type, "tool.") && strings.TrimSpace(event.Tool) == "" {
		return fmt.Errorf("assistant event %s requires tool", event.Type)
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
