package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"takt/internal/appapi"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/externalworker"
	"takt/internal/maintenance"
	"takt/internal/version"
)

const (
	Protocol2026 = "2026-07-28"
	Protocol2025 = "2025-11-25"
)

type Server struct {
	api         *appapi.Registry
	plans       *dynamicflow.PlanService
	external    *externalworker.Service
	maintenance *maintenance.Service
	in          io.Reader
	out         io.Writer
	errOut      io.Writer
	surface     Surface

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

type Dependencies struct {
	API         *appapi.Registry
	Plans       *dynamicflow.PlanService
	External    *externalworker.Service
	Maintenance *maintenance.Service
}

func New(deps Dependencies, in io.Reader, out, errOut io.Writer) *Server {
	return NewWithDependencies(deps, in, out, errOut, SurfaceAll)
}

func NewWithSurface(deps Dependencies, in io.Reader, out, errOut io.Writer, surface Surface) *Server {
	return NewWithDependencies(deps, in, out, errOut, surface)
}

func NewWithDependencies(deps Dependencies, in io.Reader, out, errOut io.Writer, surface Surface) *Server {
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}
	if surface == "" {
		surface = SurfaceAgent
	}
	return &Server{
		api: deps.API, plans: deps.Plans, external: deps.External, maintenance: deps.Maintenance,
		in: in, out: out, errOut: errOut, surface: surface, calls: map[string]context.CancelFunc{},
	}
}

// HandleJSON handles one JSON-RPC request without binding the MCP server to a
// transport. It is used by the local daemon HTTP-over-Unix-socket endpoint and
// preserves the same request cancellation registry as stdio.
func (s *Server) HandleJSON(ctx context.Context, line []byte) ([]byte, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		payload, _ := json.Marshal(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error", Data: "empty request"}})
		return payload, true
	}
	if len(line) > 0 && line[0] == '[' {
		payload, _ := json.Marshal(response{JSONRPC: "2.0", Error: &rpcError{Code: -32600, Message: "JSON-RPC batches are not supported"}})
		return payload, true
	}
	var req request
	if err := unmarshalEnvelope(line, &req); err != nil {
		payload, _ := json.Marshal(response{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error", Data: err.Error()}})
		return payload, true
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		payload, _ := json.Marshal(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32600, Message: "invalid JSON-RPC request"}})
		return payload, true
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		s.handleNotification(req)
		return nil, false
	}
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
	payload, _ := json.Marshal(response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr})
	return payload, true
}

// ServeStdio serves newline-delimited JSON-RPC over stdin/stdout. Runtime and
// diagnostics never write to stdout, preserving the MCP transport contract.
func (s *Server) ServeStdio(ctx context.Context) error {
	if s.api == nil || s.plans == nil || s.external == nil || s.maintenance == nil {
		return fmt.Errorf("MCP control service is required")
	}
	monitorCtx, stopMonitor := context.WithCancel(ctx)
	var monitor sync.WaitGroup
	monitor.Add(1)
	go func() {
		defer monitor.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				if _, err := s.maintenance.Tick(monitorCtx, time.Now().UTC()); err != nil {
					fmt.Fprintln(s.errOut, "MCP maintenance:", err)
				}
			}
		}
	}()
	defer func() {
		stopMonitor()
		monitor.Wait()
	}()
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
		return map[string]any{"tools": tools(s.surface), "ttlMs": 5000, "cacheScope": "private"}, nil
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
		"instructions":    instructions(s.surface),
	}, nil
}

func (s *Server) discover() map[string]any {
	return map[string]any{
		"protocolVersion": Protocol2026,
		"capabilities":    serverCapabilities(),
		"instructions":    instructions(s.surface),
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

func instructions(surface Surface) string { return surfaceInstructions(surface) }

func (s *Server) callTool(ctx context.Context, params callParams) map[string]any {
	if !surfaceAllows(s.surface, params.Name) {
		return toolError(fmt.Errorf("tool %q is not available on MCP surface %q", params.Name, s.surface))
	}
	value, err := s.executeTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return toolError(err)
	}
	return toolSuccess(value)
}

func canonicalOperation(name string) (string, bool) {
	return appapi.CanonicalOperationForMCP(name)
}

func (s *Server) executeTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if operation, ok := canonicalOperation(name); ok {
		return s.api.CallMap(ctx, operation, args)
	}
	handler, ok := s.specialToolHandler(name)
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	return handler(ctx, args)
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

func canonicalTools() []tool {
	descriptors := appapi.CanonicalOperations()
	result := make([]tool, 0, len(descriptors))
	for _, descriptor := range descriptors {
		if descriptor.MCPTool == "" {
			continue
		}
		result = append(result, tool{
			Name: descriptor.MCPTool, Title: descriptor.Title, Description: descriptor.Description,
			InputSchema: descriptor.InputSchema, Annotations: descriptor.Annotations,
		})
	}
	return result
}

func allTools() []tool {
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
	result := canonicalTools()
	result = append(result,
		tool{Name: "takt.node.pending", Title: "List external Takt nodes", Description: "List pending or expired-lease external command/prompt nodes. Omit run_id to inspect all local Runs.", InputSchema: object(map[string]any{
			"run_id": stringProp("Optional root Run ID"), "recursive": boolProp("Include descendant Runs"),
		}), Annotations: readOnly},
		tool{Name: "takt.node.claim", Title: "Claim external Takt node", Description: "Claim one durable external node with a worker identity, explicit agent-event capability declaration and bounded lease.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "worker_id": stringProp("Stable worker identity"),
			"capabilities":           stringArray("Compatibility shorthand for policy capabilities"),
			"capability_declaration": capabilityDeclaration,
			"lease_ms":               integerProp("Claim lease in milliseconds; defaults to 900000", 1, 3600000),
		}, "run_id", "node_id", "worker_id"), Annotations: mutating},
		tool{Name: "takt.node.reconcile", Title: "Reconcile external side effect", Description: "Resolve an expired external claim before retrying a non-idempotent side effect. applied requires a receipt and result; not_applied permits a fresh claim; unknown remains blocked.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "outcome": map[string]any{"type": "string", "enum": []string{"applied", "not_applied", "unknown"}},
			"receipt": stringProp("External receipt or durable operation identifier"), "output": stringProp("Recovered result when outcome is applied"), "structured": map[string]any{},
		}, "run_id", "node_id", "outcome"), Annotations: mutating},
		tool{Name: "takt.node.event", Title: "Append external node event", Description: "Append one provider-neutral assistant or tool event under the active claim.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque token returned by takt.node.claim"),
			"event": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"type"}, "properties": map[string]any{
				"type":    map[string]any{"enum": []string{"session.started", "session.resumed", "message", "usage", "diagnostic", "completed", "failed"}},
				"message": stringProp("Message or diagnostic"), "tool": stringProp("Tool name"), "call_id": stringProp("Provider tool-call ID"),
				"input": map[string]any{}, "output": map[string]any{}, "provider": stringProp("Provider ID"), "session_id": stringProp("Provider session ID"),
				"usage": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"input_tokens": integerProp("Input tokens", 0, int(^uint32(0))), "output_tokens": integerProp("Output tokens", 0, int(^uint32(0))), "cost": map[string]any{"type": "number", "minimum": 0}}},
				"data":  map[string]any{"type": "object"},
			}},
		}, "run_id", "node_id", "claim_token", "event"), Annotations: mutating},
		tool{Name: "takt.node.tool.request", Title: "Request external tool call", Description: "Register a tool call, enforce node policy and block for human approval when configured. Returns allowed, denied, cancelled, or waiting_approval.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"),
			"call_id": stringProp("Stable provider tool-call ID"), "tool": stringProp("Tool name"), "input": map[string]any{},
			"session_id": stringProp("Provider session ID"), "wait_ms": integerProp("Wait for a decision, 0 to 30000 milliseconds", 0, 30000),
		}, "run_id", "node_id", "claim_token", "call_id", "tool"), Annotations: mutating},
		tool{Name: "takt.node.tool.decide", Title: "Decide external tool call", Description: "Allow or deny a tool call waiting for blocking approval.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"),
			"decision": map[string]any{"type": "string", "enum": []string{"allow", "deny"}}, "reason": stringProp("Decision reason"),
		}, "run_id", "node_id", "call_id", "decision"), Annotations: mutating},
		tool{Name: "takt.node.tool.start", Title: "Start external tool call", Description: "Mark an allowed tool call as running before execution.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
		}, "run_id", "node_id", "claim_token", "call_id"), Annotations: mutating},
		tool{Name: "takt.node.tool.complete", Title: "Complete external tool call", Description: "Persist tool output and terminal status under the active claim.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
			"output": map[string]any{}, "failed": boolProp("Mark the tool call failed"), "reason": stringProp("Failure or completion reason"),
		}, "run_id", "node_id", "claim_token", "call_id"), Annotations: mutating},
		tool{Name: "takt.node.tool.get", Title: "Get external tool call", Description: "Read durable tool-call status, including cancellation requests.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"),
		}, "run_id", "node_id", "call_id"), Annotations: readOnly},
		tool{Name: "takt.node.tool.cancel", Title: "Cancel external tool call", Description: "Cancel a pending tool call or request cooperative cancellation of a running call.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "call_id": stringProp("Tool-call ID"), "reason": stringProp("Cancellation reason"),
		}, "run_id", "node_id", "call_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}},
		tool{Name: "takt.node.artifact.declare", Title: "Declare external tool artifact", Description: "Copy a tool-produced file into the Run artifact store and link it to its call_id.", InputSchema: object(map[string]any{
			"run_id": stringProp("Run ID"), "node_id": stringProp("External node ID"), "claim_token": stringProp("Opaque active claim token"), "call_id": stringProp("Tool-call ID"),
			"type": stringProp("Semantic artifact type"), "mime": stringProp("Artifact MIME"), "path": stringProp("Path inside execution workspace or Run artifact root"),
		}, "run_id", "node_id", "claim_token", "call_id", "type", "path"), Annotations: mutating},
		tool{Name: "takt.node.complete", Title: "Complete external Takt node", Description: "Submit a successful external result and continue the normal Takt retry, output, hook, artifact and parent/child lifecycle.", InputSchema: externalSubmissionSchema(object, stringProp, integerProp), Annotations: mutating},
		tool{Name: "takt.node.fail", Title: "Fail external Takt node", Description: "Submit an external failure and continue normal retry and failure handling.", InputSchema: externalSubmissionSchema(object, stringProp, integerProp), Annotations: mutating},
	)
	for i := range result {
		if experimentalTool(result[i].Name) {
			result[i].Description = "Experimental: " + result[i].Description
		}
	}
	return result
}

func experimentalTool(name string) bool {
	return strings.HasPrefix(name, "takt.task.") ||
		strings.HasPrefix(name, "takt.host.") ||
		name == "takt.plan" || strings.HasPrefix(name, "takt.plan.") ||
		name == "takt.execute" || name == "takt.run.steer"
}

func tools(surface Surface) []tool {
	all := allTools()
	if surface == SurfaceAll {
		return all
	}
	out := make([]tool, 0, len(all))
	for _, item := range all {
		if surfaceAllows(surface, item.Name) {
			out = append(out, item)
		}
	}
	return out
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
