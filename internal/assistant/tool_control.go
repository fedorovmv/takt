package assistant

import (
	"context"
	"encoding/json"
)

type ToolRequest struct {
	Tool      string          `json:"tool"`
	CallID    string          `json:"call_id"`
	Input     json.RawMessage `json:"input,omitempty"`
	Message   string          `json:"message,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
}

type ToolDecision struct {
	Decision string `json:"decision"` // allow, deny, cancel
	Reason   string `json:"reason,omitempty"`
}

type ToolController interface {
	Decide(context.Context, ToolRequest) (ToolDecision, error)
}
