package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"takt/internal/spec"
)

const ProtocolV1Alpha1 = "takt-assistant/v1alpha1"

type ProtocolModel struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	ID       string         `json:"id"`
	Params   map[string]any `json:"params,omitempty"`
}

type ProtocolSessionRequest struct {
	Mode string `json:"mode"`
	ID   string `json:"id,omitempty"`
}

type ProtocolLimits struct {
	TimeoutMS      int64 `json:"timeout_ms,omitempty"`
	MaxOutputBytes int   `json:"max_output_bytes,omitempty"`
}

type ProtocolRequest struct {
	ProtocolVersion string                 `json:"protocol_version"`
	Type            string                 `json:"type"`
	RunID           string                 `json:"run_id"`
	NodeID          string                 `json:"node_id"`
	Attempt         int                    `json:"attempt"`
	Prompt          string                 `json:"prompt"`
	Workspace       string                 `json:"workspace"`
	Model           ProtocolModel          `json:"model"`
	Session         ProtocolSessionRequest `json:"session"`
	NativeHooks     json.RawMessage        `json:"native_hooks,omitempty"`
	Environment     map[string]string      `json:"environment,omitempty"`
	Metadata        map[string]string      `json:"metadata,omitempty"`
	Limits          ProtocolLimits         `json:"limits,omitempty"`
}

type ProtocolSessionResult struct {
	ID      string `json:"id,omitempty"`
	Resumed bool   `json:"resumed,omitempty"`
}

type ProtocolUsage struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
}

type ProtocolResult struct {
	ProtocolVersion string                 `json:"protocol_version"`
	Type            string                 `json:"type"`
	Status          string                 `json:"status"`
	Output          string                 `json:"output,omitempty"`
	Structured      json.RawMessage        `json:"structured,omitempty"`
	Session         *ProtocolSessionResult `json:"session,omitempty"`
	ExitCode        *int                   `json:"exit_code"`
	ResolvedModel   *ProtocolModel         `json:"resolved_model,omitempty"`
	Usage           *ProtocolUsage         `json:"usage,omitempty"`
}

func buildProtocolRequest(ctx context.Context, req Request, assistantSpec spec.AssistantSpec, env map[string]string, now time.Time) ProtocolRequest {
	mode, id := effectiveSession(req.SessionMode, req.SessionID)
	limits := ProtocolLimits{MaxOutputBytes: assistantSpec.MaxOutputBytes}
	if deadline, ok := ctx.Deadline(); ok {
		remaining := deadline.Sub(now)
		if remaining < 0 {
			remaining = 0
		}
		limits.TimeoutMS = remaining.Milliseconds()
		if limits.TimeoutMS == 0 && remaining > 0 {
			limits.TimeoutMS = 1
		}
	}
	return ProtocolRequest{
		ProtocolVersion: ProtocolV1Alpha1,
		Type:            "request",
		RunID:           req.RunID,
		NodeID:          req.NodeID,
		Attempt:         req.Attempt,
		Prompt:          req.Prompt,
		Workspace:       req.Workspace,
		Model: ProtocolModel{
			Name:     req.ModelName,
			Provider: req.Model.Provider,
			ID:       req.Model.ID,
			Params:   req.Model.Params,
		},
		Session:     ProtocolSessionRequest{Mode: mode, ID: id},
		NativeHooks: req.NativeHooks,
		Environment: env,
		Metadata:    req.Metadata,
		Limits:      limits,
	}
}

func effectiveSession(mode, id string) (string, string) {
	if mode == "" {
		mode = "fresh"
	}
	if mode == "fresh" || id == "" {
		return "fresh", ""
	}
	return mode, id
}

func encodeProtocolRequest(req ProtocolRequest) ([]byte, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("encode assistant request: %w", err)
	}
	return append(b, '\n'), nil
}

func decodeProtocolResult(src []byte, requestedSession ProtocolSessionRequest) (ProtocolResult, error) {
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.DisallowUnknownFields()
	var result ProtocolResult
	if err := dec.Decode(&result); err != nil {
		return ProtocolResult{}, fmt.Errorf("decode assistant result: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return ProtocolResult{}, fmt.Errorf("decode assistant result: multiple JSON values")
		}
		return ProtocolResult{}, fmt.Errorf("decode assistant result trailing data: %w", err)
	}
	if result.ProtocolVersion != ProtocolV1Alpha1 {
		return ProtocolResult{}, fmt.Errorf("unsupported protocol_version %q", result.ProtocolVersion)
	}
	if result.Type != "result" {
		return ProtocolResult{}, fmt.Errorf("assistant result type must be %q", "result")
	}
	if result.Status != "completed" && result.Status != "failed" {
		return ProtocolResult{}, fmt.Errorf("assistant result status must be completed or failed")
	}
	if result.ExitCode == nil {
		return ProtocolResult{}, fmt.Errorf("assistant result requires exit_code")
	}
	if result.Status == "completed" && *result.ExitCode != 0 {
		return ProtocolResult{}, fmt.Errorf("completed assistant result cannot have exit_code %d", *result.ExitCode)
	}
	if result.Status == "failed" && *result.ExitCode == 0 {
		return ProtocolResult{}, fmt.Errorf("failed assistant result cannot have exit_code 0")
	}
	if result.Usage != nil {
		if result.Usage.InputTokens < 0 {
			return ProtocolResult{}, fmt.Errorf("assistant result usage.input_tokens cannot be negative")
		}
		if result.Usage.OutputTokens < 0 {
			return ProtocolResult{}, fmt.Errorf("assistant result usage.output_tokens cannot be negative")
		}
		if result.Usage.Cost < 0 {
			return ProtocolResult{}, fmt.Errorf("assistant result usage.cost cannot be negative")
		}
	}
	if requestedSession.Mode == "resume" {
		if result.Session == nil || !result.Session.Resumed {
			return ProtocolResult{}, fmt.Errorf("assistant did not resume requested session %q", requestedSession.ID)
		}
		if requestedSession.ID != "" && result.Session.ID != requestedSession.ID {
			return ProtocolResult{}, fmt.Errorf("assistant resumed unexpected session %q instead of %q", result.Session.ID, requestedSession.ID)
		}
	}
	if requestedSession.Mode == "fresh" && result.Session != nil && result.Session.Resumed {
		return ProtocolResult{}, fmt.Errorf("assistant reported resumed=true for a fresh session")
	}
	return result, nil
}
