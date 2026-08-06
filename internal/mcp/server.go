package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"takt/internal/assistant"
	"takt/internal/control"
	"takt/internal/store"
	"takt/internal/version"
)

const (
	Protocol2026 = "2026-07-28"
	Protocol2025 = "2025-11-25"
)

type Server struct {
	control *control.Service
	in      io.Reader
	out     io.Writer
	errOut  io.Writer

	writeMu sync.Mutex
	callsMu sync.Mutex
	calls   map[string]context.CancelFunc
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type tool struct {
	Name        string         `json:"name"`
	Title       string         `json:"title,omitempty"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

func New(service *control.Service, in io.Reader, out, errOut io.Writer) *Server {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	return &Server{control: service, in: in, out: out, errOut: errOut, calls: map[string]context.CancelFunc{}}
}

// ServeStdio serves newline-delimited JSON-RPC over stdin/stdout. Runtime and
// diagnostics never write to stdout, preserving the MCP transport contract.
func (s *Server) ServeStdio(ctx context.Context) error {
	if s.control == nil {
		return fmt.Errorf("MCP control service is required")
	}
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var workers sync.WaitGroup
	sem := make(chan struct{}, 64)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if len(line) > 0 && line[0] == '[' {
			_ = s.write(response{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "JSON-RPC batches are not supported"}})
			continue
		}
		if err := unmarshalEnvelope(line, &req); err != nil {
			_ = s.write(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error", Data: err.Error()}})
			continue
		}
		if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
			_ = s.write(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "invalid JSON-RPC request"}})
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			s.handleNotification(req)
			continue
		}
		sem <- struct{}{}
		workers.Add(1)
		go func(req request) {
			defer workers.Done()
			defer func() { <-sem }()
			requestCtx, cancel := context.WithCancel(ctx)
			key := requestIDKey(req.ID)
			s.callsMu.Lock()
			s.calls[key] = cancel
			s.callsMu.Unlock()
			defer func() {
				cancel()
				s.callsMu.Lock()
				delete(s.calls, key)
				s.callsMu.Unlock()
			}()
			result, rpcErr := s.handleRequest(requestCtx, req)
			if rpcErr == nil {
				result = withServerMeta(result)
			}
			if err := s.write(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}); err != nil {
				fmt.Fprintln(s.errOut, "MCP response write failed:", err)
			}
		}(req)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	workers.Wait()
	return nil
}

func (s *Server) handleNotification(req request) {
	if req.Method != "notifications/cancelled" {
		return
	}
	var params struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := strictUnmarshal(req.Params, &params); err != nil {
		return
	}
	key := requestIDKey(params.RequestID)
	s.callsMu.Lock()
	cancel := s.calls[key]
	s.callsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *Server) handleRequest(ctx context.Context, req request) (any, *rpcError) {
	switch req.Method {
	case "ping":
		return map[string]any{}, nil
	case "initialize":
		return s.initialize(req.Params)
	case "server/discover":
		return s.discover(), nil
	case "tools/list":
		return map[string]any{"tools": tools(), "ttlMs": 5000, "cacheScope": "private"}, nil
	case "tools/call":
		var params callParams
		if err := strictUnmarshal(req.Params, &params); err != nil {
			return nil, invalidParams(err)
		}
		if strings.TrimSpace(params.Name) == "" {
			return nil, invalidParams(fmt.Errorf("tool name is required"))
		}
		return s.callTool(ctx, params), nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found", Data: req.Method}
	}
}

func (s *Server) initialize(raw json.RawMessage) (any, *rpcError) {
	var params struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		ClientInfo      map[string]any `json:"clientInfo"`
		Meta            map[string]any `json:"_meta,omitempty"`
	}
	if err := strictUnmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	selected := params.ProtocolVersion
	switch selected {
	case "2025-03-26", "2025-06-18", Protocol2025:
	default:
		selected = Protocol2025
	}
	return map[string]any{
		"protocolVersion": selected,
		"capabilities":    serverCapabilities(),
		"serverInfo":      serverInfo(),
		"instructions":    instructions(),
	}, nil
}

func (s *Server) discover() map[string]any {
	return map[string]any{
		"protocolVersion": Protocol2026,
		"capabilities":    serverCapabilities(),
		"instructions":    instructions(),
		"_meta": map[string]any{
			"io.modelcontextprotocol/serverInfo": serverInfo(),
		},
	}
}

func withServerMeta(value any) any {
	object, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if _, exists := object["_meta"]; !exists {
		object["_meta"] = map[string]any{"io.modelcontextprotocol/serverInfo": serverInfo()}
	}
	return object
}

func serverCapabilities() map[string]any {
	return map[string]any{"tools": map[string]any{"listChanged": false}}
}

func serverInfo() map[string]any {
	return map[string]any{"name": "takt", "title": "Takt Local Workflow Runtime", "version": version.Value}
}

func instructions() string {
	return "Use takt.workflow.* to discover workflows, takt.run.* to govern local Runs, and takt.node.* to execute durable external command/prompt nodes. Start is detached by default; poll takt.run.get or takt.run.events with the returned run_id. External workers claim a node with capabilities, stream normalized events, then complete or fail it with the claim token."
}

func (s *Server) callTool(ctx context.Context, params callParams) map[string]any {
	value, err := s.executeTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return toolError(err)
	}
	return toolSuccess(value)
}

func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "takt.workflow.list":
		var in struct {
			Profile string `json:"profile"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		entries, err := s.control.ListWorkflows(in.Profile)
		if err != nil {
			return nil, err
		}
		return map[string]any{"profile": in.Profile, "workflows": entries}, nil
	case "takt.workflow.describe":
		var in struct {
			Selector string `json:"selector"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.DescribeWorkflow(in.Selector)
	case "takt.run.start":
		var in struct {
			Selector     string `json:"selector"`
			Input        string `json:"input,omitempty"`
			ConfigPath   string `json:"config_path,omitempty"`
			Worktree     *bool  `json:"worktree,omitempty"`
			WorktreeBase string `json:"worktree_base,omitempty"`
			KeepWorktree bool   `json:"keep_worktree,omitempty"`
			AllowDirty   bool   `json:"allow_dirty_worktree,omitempty"`
			Detached     *bool  `json:"detached,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		detached := true
		if in.Detached != nil {
			detached = *in.Detached
		}
		return s.control.Start(ctx, control.StartRequest{
			Selector: in.Selector, Input: in.Input, ConfigPath: in.ConfigPath,
			Worktree: in.Worktree, WorktreeBase: in.WorktreeBase,
			KeepWorktree: in.KeepWorktree, AllowDirty: in.AllowDirty, Detached: detached,
		})
	case "takt.run.get":
		var in runIDArguments
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.GetRun(in.RunID)
	case "takt.run.resume":
		var in runIDArguments
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.Resume(ctx, in.RunID)
	case "takt.run.answer":
		var in struct {
			RunID  string `json:"run_id"`
			NodeID string `json:"node_id"`
			Value  string `json:"value"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.Answer(ctx, in.RunID, in.NodeID, in.Value)
	case "takt.run.cancel":
		var in struct {
			RunID  string `json:"run_id"`
			Reason string `json:"reason,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.Cancel(in.RunID, in.Reason)
	case "takt.run.children":
		var in runIDArguments
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.Children(in.RunID)
	case "takt.run.artifacts":
		var in struct {
			RunID          string `json:"run_id"`
			NodeID         string `json:"node_id,omitempty"`
			Type           string `json:"type,omitempty"`
			Recursive      bool   `json:"recursive,omitempty"`
			IncludeContent bool   `json:"include_content,omitempty"`
			MaxBytes       int    `json:"max_bytes,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		result, err := s.control.Artifacts(in.RunID, control.ArtifactQuery{NodeID: in.NodeID, Type: in.Type, Recursive: in.Recursive})
		if err != nil {
			return nil, err
		}
		if in.IncludeContent {
			if err := attachArtifactContent(result, in.MaxBytes); err != nil {
				return nil, err
			}
		}
		return result, nil
	case "takt.run.events":
		var in struct {
			RunID         string `json:"run_id"`
			AfterRevision uint64 `json:"after_revision,omitempty"`
			Limit         int    `json:"limit,omitempty"`
			WaitMS        int    `json:"wait_ms,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		if in.WaitMS < 0 || in.WaitMS > 30000 {
			return nil, fmt.Errorf("wait_ms must be between 0 and 30000")
		}
		return s.control.Events(ctx, in.RunID, in.AfterRevision, in.Limit, time.Duration(in.WaitMS)*time.Millisecond)
	case "takt.node.pending":
		var in struct {
			RunID     string `json:"run_id,omitempty"`
			Recursive bool   `json:"recursive,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		tasks, err := s.control.PendingExternal(in.RunID, in.Recursive)
		if err != nil {
			return nil, err
		}
		return map[string]any{"tasks": tasks}, nil
	case "takt.node.claim":
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
		return s.control.ClaimExternal(control.ExternalClaimRequest{RunID: in.RunID, NodeID: in.NodeID, WorkerID: in.WorkerID, Capabilities: in.Capabilities, Declaration: in.Declaration, Lease: time.Duration(in.LeaseMS) * time.Millisecond})
	case "takt.node.event":
		var in struct {
			RunID      string          `json:"run_id"`
			NodeID     string          `json:"node_id"`
			ClaimToken string          `json:"claim_token"`
			Event      assistant.Event `json:"event"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		sequence, err := s.control.AppendExternalEvent(in.RunID, in.NodeID, in.ClaimToken, in.Event)
		if err != nil {
			return nil, err
		}
		return map[string]any{"sequence": sequence}, nil
	case "takt.node.tool.request":
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
		return s.control.RequestExternalTool(ctx, control.ExternalToolRequest{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Tool: in.Tool, Input: in.Input, Message: in.Message, Wait: time.Duration(in.WaitMS) * time.Millisecond})
	case "takt.node.tool.decide":
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
		return s.control.DecideExternalTool(control.ExternalToolDecisionRequest{RunID: in.RunID, NodeID: in.NodeID, CallID: in.CallID, Decision: in.Decision, Reason: in.Reason})
	case "takt.node.tool.start":
		var in struct {
			RunID      string `json:"run_id"`
			NodeID     string `json:"node_id"`
			ClaimToken string `json:"claim_token"`
			CallID     string `json:"call_id"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.StartExternalTool(control.ExternalToolUpdate{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID})
	case "takt.node.tool.complete":
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
		return s.control.CompleteExternalTool(control.ExternalToolUpdate{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Output: in.Output, Failed: in.Failed, Reason: in.Reason})
	case "takt.node.tool.get":
		var in struct {
			RunID  string `json:"run_id"`
			NodeID string `json:"node_id"`
			CallID string `json:"call_id"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.GetExternalTool(in.RunID, in.NodeID, in.CallID)
	case "takt.node.tool.cancel":
		var in struct {
			RunID  string `json:"run_id"`
			NodeID string `json:"node_id"`
			CallID string `json:"call_id"`
			Reason string `json:"reason,omitempty"`
		}
		if err := decodeArguments(args, &in); err != nil {
			return nil, err
		}
		return s.control.CancelExternalTool(in.RunID, in.NodeID, in.CallID, in.Reason)
	case "takt.node.artifact.declare":
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
		return s.control.DeclareExternalArtifact(control.ExternalArtifactRequest{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, CallID: in.CallID, Type: in.Type, MIME: in.MIME, Path: in.Path})
	case "takt.node.complete", "takt.node.fail":
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
		submission := control.ExternalSubmission{RunID: in.RunID, NodeID: in.NodeID, ClaimToken: in.ClaimToken, Output: in.Output, Structured: in.Structured, Stdout: in.Stdout, Stderr: in.Stderr, ExitCode: in.ExitCode, SessionID: in.SessionID, Resumed: in.Resumed, AssistantVersion: in.AssistantVersion, ResolvedModel: in.ResolvedModel, Usage: in.Usage, ErrorCode: in.ErrorCode, Error: in.Error}
		if name == "takt.node.fail" {
			return s.control.FailExternal(ctx, submission)
		}
		return s.control.CompleteExternal(ctx, submission)
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

type runIDArguments struct {
	RunID string `json:"run_id"`
}

func toolSuccess(value any) map[string]any {
	serialized, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{
		"resultType":        "complete",
		"content":           []map[string]any{{"type": "text", "text": string(serialized)}},
		"structuredContent": value,
		"isError":           false,
	}
}

func toolError(err error) map[string]any {
	value := map[string]any{"ok": false, "error": err.Error()}
	serialized, _ := json.MarshalIndent(value, "", "  ")
	return map[string]any{
		"resultType":        "complete",
		"content":           []map[string]any{{"type": "text", "text": string(serialized)}},
		"structuredContent": value,
		"isError":           true,
	}
}

func tools() []tool {
	object := func(properties map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	stringProp := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	boolProp := func(description string) map[string]any {
		return map[string]any{"type": "boolean", "description": description}
	}
	integerProp := func(description string, min, max int) map[string]any {
		return map[string]any{"type": "integer", "description": description, "minimum": min, "maximum": max}
	}
	stringArray := func(description string) map[string]any {
		return map[string]any{"type": "array", "description": description, "items": map[string]any{"type": "string"}, "uniqueItems": true}
	}
	capabilityDeclaration := object(map[string]any{
		"protocol":        stringProp("Agent event protocol, normally takt-agent-events/v2"),
		"capabilities":    stringArray("Policy and execution capabilities guaranteed by the worker"),
		"event_types":     stringArray("Normalized event types emitted by the worker"),
		"session_events":  boolProp("Worker emits session.started/session.resumed"),
		"tool_events":     boolProp("Worker emits normalized tool lifecycle events"),
		"tool_control":    boolProp("Worker blocks tool execution until Takt returns allow or deny"),
		"artifact_events": boolProp("Worker declares tool-produced artifacts"),
		"usage_events":    boolProp("Worker emits incremental usage updates"),
	})
	readOnly := map[string]any{"readOnlyHint": true, "destructiveHint": false}
	mutating := map[string]any{"readOnlyHint": false}
	return []tool{
		{Name: "takt.workflow.list", Title: "List Takt workflows", Description: "List deterministic workflow selectors published by an installed Takt profile.", InputSchema: object(map[string]any{"profile": stringProp("Installed profile name, for example code")}, "profile"), Annotations: readOnly},
		{Name: "takt.workflow.describe", Title: "Describe a Takt workflow", Description: "Describe the public DAG of a profile selector before starting it.", InputSchema: object(map[string]any{"selector": stringProp("Profile selector such as code:plan-to-pr")}, "selector"), Annotations: readOnly},
		{Name: "takt.run.start", Title: "Start a Takt Run", Description: "Validate definitions and start a local Takt Run. Detached mode is the default and returns a durable run_id for polling.", InputSchema: object(map[string]any{
			"selector": stringProp("Profile selector or workflow file path"), "input": stringProp("Input text or a readable input file path"),
			"config_path": stringProp("Optional config override"), "worktree": boolProp("Force or disable managed Git worktree isolation"),
			"worktree_base": stringProp("Optional Git base revision"), "keep_worktree": boolProp("Keep a successful worktree"),
			"allow_dirty_worktree": boolProp("Allow a dirty control checkout and start from committed state"), "detached": boolProp("Return after the Run is durably started; defaults to true"),
		}, "selector"), Annotations: mutating},
		{Name: "takt.run.get", Title: "Get Takt Run", Description: "Read the current public Run state, including waiting approval, nodes, usage and durable child links.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID")}, "run_id"), Annotations: readOnly},
		{Name: "takt.run.resume", Title: "Resume Takt Run", Description: "Resume a failed or otherwise resumable Run after external correction. Definitions and fingerprints are verified first.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID")}, "run_id"), Annotations: mutating},
		{Name: "takt.run.answer", Title: "Answer Takt approval", Description: "Submit an approval response and continue the waiting child and parent Run chain.", InputSchema: object(map[string]any{"run_id": stringProp("Root or direct child Run ID"), "node_id": stringProp("Public approval node ID"), "value": stringProp("Approval response")}, "run_id", "node_id", "value"), Annotations: mutating},
		{Name: "takt.run.cancel", Title: "Cancel Takt Run", Description: "Request durable cancellation of a Run and its active child tree.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID"), "reason": stringProp("Cancellation reason")}, "run_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}},
		{Name: "takt.run.children", Title: "List child Runs", Description: "List direct governed child Runs and fan-out item metadata.", InputSchema: object(map[string]any{"run_id": stringProp("Parent Run ID")}, "run_id"), Annotations: readOnly},
		{Name: "takt.run.artifacts", Title: "List Takt artifacts", Description: "List typed artifacts with checksum and provenance; optionally include bounded local file content.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("Optional producer node filter"), "type": stringProp("Optional semantic type filter"),
			"recursive": boolProp("Include descendant Runs"), "include_content": boolProp("Include bounded artifact content"), "max_bytes": integerProp("Maximum bytes per included artifact; defaults to 65536", 1, 1048576),
		}, "run_id"), Annotations: readOnly},
		{Name: "takt.run.events", Title: "Read Takt Run events", Description: "Read events after a durable revision cursor. wait_ms enables bounded long polling for incremental monitoring.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "after_revision": integerProp("Return events with a greater revision", 0, int(^uint32(0))),
			"limit": integerProp("Maximum events, defaults to 200", 1, 1000), "wait_ms": integerProp("Long-poll wait, 0 to 30000 milliseconds", 0, 30000),
		}, "run_id"), Annotations: readOnly},
		{Name: "takt.node.pending", Title: "List external Takt nodes", Description: "List pending or expired-lease external command/prompt nodes. Omit run_id to inspect all local Runs.", InputSchema: object(map[string]any{
			"run_id": stringProp("Optional root Run ID"), "recursive": boolProp("Include descendant Runs"),
		}), Annotations: readOnly},
		{Name: "takt.node.claim", Title: "Claim external Takt node", Description: "Claim one durable external node with a worker identity, explicit agent-event capability declaration and bounded lease.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "worker_id": stringProp("Stable worker identity"),
			"capabilities":           stringArray("Compatibility shorthand for policy capabilities"),
			"capability_declaration": capabilityDeclaration,
			"lease_ms":               integerProp("Claim lease in milliseconds; defaults to 900000", 1, 3600000),
		}, "run_id", "node_id", "worker_id"), Annotations: mutating},
		{Name: "takt.node.event", Title: "Append external node event", Description: "Append one provider-neutral assistant or tool event under the active claim.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque token returned by takt.node.claim"),
			"event": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"type"}, "properties": map[string]any{
				"type":    map[string]any{"enum": []string{"session.started", "session.resumed", "message", "usage", "diagnostic", "completed", "failed"}},
				"message": stringProp("Message or diagnostic"), "tool": stringProp("Tool name"), "call_id": stringProp("Provider tool-call ID"),
				"input": map[string]any{}, "output": map[string]any{}, "provider": stringProp("Provider ID"), "session_id": stringProp("Provider session ID"),
				"usage": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"input_tokens": integerProp("Input tokens", 0, int(^uint32(0))), "output_tokens": integerProp("Output tokens", 0, int(^uint32(0))), "cost": map[string]any{"type": "number", "minimum": 0}}},
				"data":  map[string]any{"type": "object"},
			}},
		}, "run_id", "node_id", "claim_token", "event"), Annotations: mutating},
		{Name: "takt.node.tool.request", Title: "Request external tool call", Description: "Register a tool call, enforce node policy and block for human approval when configured. Returns allowed, denied, cancelled, or waiting_approval.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"),
			"call_id": stringProp("Stable provider tool-call ID"), "tool": stringProp("Tool name"), "input": map[string]any{},
			"session_id": stringProp("Provider session ID"), "wait_ms": integerProp("Wait for a decision, 0 to 30000 milliseconds", 0, 30000),
		}, "run_id", "node_id", "claim_token", "call_id", "tool"), Annotations: mutating},
		{Name: "takt.node.tool.decide", Title: "Decide external tool call", Description: "Allow or deny a tool call waiting for blocking approval.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"),
			"decision": map[string]any{"type": "string", "enum": []string{"allow", "deny"}}, "reason": stringProp("Decision reason"),
		}, "run_id", "node_id", "call_id", "decision"), Annotations: mutating},
		{Name: "takt.node.tool.start", Title: "Start external tool call", Description: "Mark an allowed tool call as running before execution.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
		}, "run_id", "node_id", "claim_token", "call_id"), Annotations: mutating},
		{Name: "takt.node.tool.complete", Title: "Complete external tool call", Description: "Persist tool output and terminal status under the active claim.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
			"output": map[string]any{}, "failed": boolProp("Mark the tool call failed"), "reason": stringProp("Failure or completion reason"),
		}, "run_id", "node_id", "claim_token", "call_id"), Annotations: mutating},
		{Name: "takt.node.tool.get", Title: "Get external tool call", Description: "Read durable tool-call status, including cancellation requests.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"),
		}, "run_id", "node_id", "call_id"), Annotations: readOnly},
		{Name: "takt.node.tool.cancel", Title: "Cancel external tool call", Description: "Cancel a pending tool call or request cooperative cancellation of a running call.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"), "reason": stringProp("Cancellation reason"),
		}, "run_id", "node_id", "call_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}},
		{Name: "takt.node.artifact.declare", Title: "Declare external tool artifact", Description: "Copy a tool-produced file into the Run artifact store and link it to its call_id.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
			"type": stringProp("Semantic artifact type"), "mime": stringProp("Artifact MIME"), "path": stringProp("Path inside execution workspace or Run artifact root"),
		}, "run_id", "node_id", "claim_token", "call_id", "type", "path"), Annotations: mutating},
		{Name: "takt.node.complete", Title: "Complete external Takt node", Description: "Submit a successful external result and continue the normal Takt retry, output, hook, artifact and parent/child lifecycle.", InputSchema: externalSubmissionSchema(object, stringProp, integerProp), Annotations: mutating},
		{Name: "takt.node.fail", Title: "Fail external Takt node", Description: "Submit an external failure and continue normal retry and failure handling.", InputSchema: externalSubmissionSchema(object, stringProp, integerProp), Annotations: mutating},
	}
}

func externalSubmissionSchema(object func(map[string]any, ...string) map[string]any, stringProp func(string) map[string]any, integerProp func(string, int, int) map[string]any) map[string]any {
	return object(map[string]any{
		"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"),
		"output": stringProp("Normalized final output"), "structured": map[string]any{}, "stdout": stringProp("Raw provider stdout"), "stderr": stringProp("Raw provider stderr"),
		"exit_code": integerProp("Provider exit code", -1, 255), "session_id": stringProp("Provider session ID"), "resumed": map[string]any{"type": "boolean"},
		"assistant_version": stringProp("Executor/provider version"),
		"resolved_model":    map[string]any{"type": "object", "additionalProperties": true},
		"usage":             map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"input_tokens": integerProp("Input tokens", 0, int(^uint32(0))), "output_tokens": integerProp("Output tokens", 0, int(^uint32(0))), "cost": map[string]any{"type": "number", "minimum": 0}}},
		"error_code":        stringProp("Takt execution error kind"), "error": stringProp("Failure message"),
	}, "run_id", "node_id", "claim_token")
}

func attachArtifactContent(result map[string]any, maxBytes int) error {
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	if maxBytes > 1024*1024 {
		return fmt.Errorf("max_bytes must not exceed 1048576")
	}
	artifacts, ok := result["artifacts"].([]store.ArtifactRef)
	if !ok {
		return fmt.Errorf("unexpected artifact result")
	}
	values := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		raw, err := os.ReadFile(artifact.Path)
		if err != nil {
			return fmt.Errorf("read artifact %s: %w", artifact.ID, err)
		}
		truncated := len(raw) > maxBytes
		if truncated {
			raw = raw[:maxBytes]
		}
		encoded, _ := json.Marshal(artifact)
		var value map[string]any
		_ = json.Unmarshal(encoded, &value)
		if isTextMIME(artifact.MIME) {
			value["content"] = string(raw)
			value["content_encoding"] = "utf-8"
		} else {
			value["content"] = base64.StdEncoding.EncodeToString(raw)
			value["content_encoding"] = "base64"
		}
		if truncated {
			value["content_truncated"] = true
		}
		values = append(values, value)
	}
	result["artifacts"] = values
	return nil
}

func isTextMIME(value string) bool {
	value = strings.ToLower(value)
	return strings.HasPrefix(value, "text/") || strings.Contains(value, "json") || strings.Contains(value, "yaml") || strings.Contains(value, "xml") || value == ""
}

func decodeArguments(args map[string]any, value any) error {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return err
	}
	if err := strictUnmarshal(raw, value); err != nil {
		return fmt.Errorf("invalid tool arguments: %w", err)
	}
	return validateRequiredStrings(value)
}

func validateRequiredStrings(value any) error {
	raw, _ := json.Marshal(value)
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	for _, key := range []string{"profile", "selector", "run_id", "node_id"} {
		if current, ok := fields[key]; ok {
			if text, ok := current.(string); ok && strings.TrimSpace(text) == "" {
				return fmt.Errorf("%s is required", key)
			}
		}
	}
	return nil
}

func unmarshalEnvelope(raw []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func strictUnmarshal(raw []byte, value any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("multiple JSON values")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func invalidParams(err error) *rpcError {
	return &rpcError{Code: -32602, Message: "invalid params", Data: err.Error()}
}

func requestIDKey(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return ""
	}
	if trimmed[0] == '"' {
		var value string
		if json.Unmarshal(trimmed, &value) == nil {
			return "s:" + value
		}
	}
	// Preserve the exact JSON number representation. Decoding through float64
	// would collapse distinct identifiers above 2^53 and could cancel another
	// in-flight request.
	return "j:" + string(trimmed)
}

func (s *Server) write(value response) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	encoder := json.NewEncoder(s.out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
