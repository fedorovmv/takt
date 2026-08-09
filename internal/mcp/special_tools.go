package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/store"
)

type specialToolHandler func(context.Context, map[string]any) (any, error)

func (s *Server) specialToolHandler(name string) (specialToolHandler, bool) {
	switch name {
	case "takt.execute":
		return s.executePlanTool, true
	case "takt.node.pending":
		return s.pendingExternalTool, true
	case "takt.node.claim":
		return s.claimExternalTool, true
	case "takt.node.reconcile":
		return s.reconcileExternalTool, true
	case "takt.node.event":
		return s.appendExternalEventTool, true
	case "takt.node.tool.request":
		return s.requestExternalTool, true
	case "takt.node.tool.decide":
		return s.decideExternalTool, true
	case "takt.node.tool.start":
		return s.startExternalTool, true
	case "takt.node.tool.complete":
		return s.completeExternalTool, true
	case "takt.node.tool.get":
		return s.getExternalTool, true
	case "takt.node.tool.cancel":
		return s.cancelExternalTool, true
	case "takt.node.artifact.declare":
		return s.declareExternalArtifactTool, true
	case "takt.node.complete":
		return s.completeExternalNodeTool, true
	case "takt.node.fail":
		return s.failExternalNodeTool, true
	default:
		return nil, false
	}
}

func (s *Server) executePlanTool(ctx context.Context, args map[string]any) (any, error) {
	var in dynamicflow.ExecutePlanRequest
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.plans.ExecutePlan(ctx, in)
}

func (s *Server) pendingExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID     string `json:"run_id,omitempty"`
		Recursive bool   `json:"recursive,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	tasks, err := s.external.PendingExternal(in.RunID, in.Recursive)
	if err != nil {
		return nil, err
	}
	return map[string]any{"tasks": tasks}, nil
}

func (s *Server) claimExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID        string                          `json:"run_id"`
		NodeID       string                          `json:"node_id"`
		WorkerID     string                          `json:"worker_id"`
		Capabilities []string                        `json:"capabilities,omitempty"`
		Declaration  assistant.CapabilityDeclaration `json:"capability_declaration,omitempty"`
		LeaseMS      int                             `json:"lease_ms,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	if in.LeaseMS < 0 || in.LeaseMS > int(time.Hour/time.Millisecond) {
		return nil, fmt.Errorf("lease_ms must be between 0 and 3600000")
	}
	return s.external.ClaimExternal(application.ExternalClaimRequest{RunID: in.RunID, NodeID: in.NodeID, WorkerID: in.WorkerID, Capabilities: in.Capabilities, Declaration: in.Declaration, Lease: time.Duration(in.LeaseMS) * time.Millisecond})
}

func (s *Server) reconcileExternalTool(ctx context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string          `json:"run_id"`
		NodeID     string          `json:"node_id"`
		Outcome    string          `json:"outcome"`
		Receipt    string          `json:"receipt,omitempty"`
		Output     string          `json:"output,omitempty"`
		Structured json.RawMessage `json:"structured,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.ReconcileExternal(ctx, application.ExternalReconcileRequest{RunID: in.RunID, NodeID: in.NodeID, Outcome: in.Outcome, Receipt: in.Receipt, Submission: application.ExternalSubmission{Output: in.Output, Structured: in.Structured}})
}

func (s *Server) appendExternalEventTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string          `json:"run_id"`
		NodeID     string          `json:"node_id"`
		ClaimToken string          `json:"claim_token"`
		Event      assistant.Event `json:"event"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	sequence, err := s.external.AppendExternalEvent(in.RunID, in.NodeID, in.ClaimToken, in.Event)
	if err != nil {
		return nil, err
	}
	return map[string]any{"sequence": sequence}, nil
}

func (s *Server) requestExternalTool(ctx context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string          `json:"run_id"`
		NodeID     string          `json:"node_id"`
		ClaimToken string          `json:"claim_token"`
		CallID     string          `json:"call_id"`
		Tool       string          `json:"tool"`
		Input      json.RawMessage `json:"input,omitempty"`
		Message    string          `json:"message,omitempty"`
		WaitMS     int             `json:"wait_ms,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	if in.WaitMS < 0 || in.WaitMS > 30000 {
		return nil, fmt.Errorf("wait_ms must be between 0 and 30000")
	}
	return s.external.RequestExternalTool(ctx, application.ExternalToolRequest{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Tool: in.Tool, Input: in.Input, Message: in.Message, Wait: time.Duration(in.WaitMS) * time.Millisecond})
}

func (s *Server) decideExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID    string `json:"run_id"`
		NodeID   string `json:"node_id"`
		CallID   string `json:"call_id"`
		Decision string `json:"decision"`
		Reason   string `json:"reason,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.DecideExternalTool(application.ExternalToolDecisionRequest{RunID: in.RunID, NodeID: in.NodeID, CallID: in.CallID, Decision: in.Decision, Reason: in.Reason})
}

func (s *Server) startExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string `json:"run_id"`
		NodeID     string `json:"node_id"`
		ClaimToken string `json:"claim_token"`
		CallID     string `json:"call_id"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.StartExternalTool(application.ExternalToolUpdate{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID})
}

func (s *Server) completeExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string          `json:"run_id"`
		NodeID     string          `json:"node_id"`
		ClaimToken string          `json:"claim_token"`
		CallID     string          `json:"call_id"`
		Output     json.RawMessage `json:"output,omitempty"`
		Failed     bool            `json:"failed,omitempty"`
		Reason     string          `json:"reason,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.CompleteExternalTool(application.ExternalToolUpdate{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Output: in.Output, Failed: in.Failed, Reason: in.Reason})
}

func (s *Server) getExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID  string `json:"run_id"`
		NodeID string `json:"node_id"`
		CallID string `json:"call_id"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.GetExternalTool(in.RunID, in.NodeID, in.CallID)
}

func (s *Server) cancelExternalTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID  string `json:"run_id"`
		NodeID string `json:"node_id"`
		CallID string `json:"call_id"`
		Reason string `json:"reason,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.CancelExternalTool(in.RunID, in.NodeID, in.CallID, in.Reason)
}

func (s *Server) declareExternalArtifactTool(_ context.Context, args map[string]any) (any, error) {
	var in struct {
		RunID      string `json:"run_id"`
		NodeID     string `json:"node_id"`
		ClaimToken string `json:"claim_token"`
		CallID     string `json:"call_id"`
		Type       string `json:"type"`
		MIME       string `json:"mime,omitempty"`
		Path       string `json:"path"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	return s.external.DeclareExternalArtifact(application.ExternalArtifactRequest{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Type: in.Type, MIME: in.MIME, Path: in.Path})
}

func (s *Server) completeExternalNodeTool(ctx context.Context, args map[string]any) (any, error) {
	return s.finishExternalNodeTool(ctx, args, false)
}
func (s *Server) failExternalNodeTool(ctx context.Context, args map[string]any) (any, error) {
	return s.finishExternalNodeTool(ctx, args, true)
}

func (s *Server) finishExternalNodeTool(ctx context.Context, args map[string]any, failed bool) (any, error) {
	var in struct {
		RunID            string          `json:"run_id"`
		NodeID           string          `json:"node_id"`
		ClaimToken       string          `json:"claim_token"`
		Output           string          `json:"output,omitempty"`
		Structured       json.RawMessage `json:"structured,omitempty"`
		Stdout           string          `json:"stdout,omitempty"`
		Stderr           string          `json:"stderr,omitempty"`
		ExitCode         int             `json:"exit_code,omitempty"`
		SessionID        string          `json:"session_id,omitempty"`
		Resumed          bool            `json:"resumed,omitempty"`
		AssistantVersion string          `json:"assistant_version,omitempty"`
		ResolvedModel    *store.ModelRef `json:"resolved_model,omitempty"`
		Usage            *store.Usage    `json:"usage,omitempty"`
		ErrorCode        string          `json:"error_code,omitempty"`
		Error            string          `json:"error,omitempty"`
	}
	if err := decodeArguments(args, &in); err != nil {
		return nil, err
	}
	submission := application.ExternalSubmission{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, Output: in.Output, Structured: in.Structured, Stdout: in.Stdout, Stderr: in.Stderr, ExitCode: in.ExitCode, SessionID: in.SessionID, Resumed: in.Resumed, AssistantVersion: in.AssistantVersion, ResolvedModel: in.ResolvedModel, Usage: in.Usage, ErrorCode: in.ErrorCode, Error: in.Error}
	if failed {
		return s.external.FailExternal(ctx, submission)
	}
	return s.external.CompleteExternal(ctx, submission)
}
