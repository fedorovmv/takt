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
	"strconv"
	"strings"
	"sync"
	"time"

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
		if err := strictUnmarshal(line, &req); err != nil {
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
	return "Use takt.workflow.* to discover workflows and takt.run.* to start and govern local Runs. Start is detached by default; poll takt.run.get or takt.run.events with the returned run_id. Approval, cancellation, child Runs and artifacts remain explicit durable handles."
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
	}
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
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	switch typed := value.(type) {
	case string:
		return "s:" + typed
	case float64:
		return "n:" + strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return string(raw)
	}
}

func (s *Server) write(value response) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	encoder := json.NewEncoder(s.out)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
