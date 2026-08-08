package domainadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"takt/internal/spec"
)

type MCP struct{ Spec spec.DomainAdapterSpec }

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type mcpSession struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	next    int
	cancel  context.CancelFunc
}

func (m *MCP) Describe(ctx context.Context) (Declaration, error) {
	session, err := m.start(ctx, "")
	if err != nil {
		return Declaration{}, err
	}
	defer session.close()
	tools, err := session.listTools()
	if err != nil {
		return Declaration{}, err
	}
	available := map[string]bool{}
	for _, t := range tools {
		available[t] = true
	}
	var caps, reconcile []string
	if len(m.Spec.Operations) == 0 {
		prefix := m.Spec.Domain + "."
		for tool := range available {
			if !strings.HasPrefix(tool, prefix) {
				continue
			}
			operation := strings.TrimPrefix(tool, prefix)
			if strings.HasSuffix(operation, ".reconcile") {
				base := strings.TrimSuffix(operation, ".reconcile")
				if available[prefix+base] {
					reconcile = append(reconcile, base)
				}
				continue
			}
			caps = append(caps, operation)
		}
	} else {
		for operation, tool := range m.Spec.Operations {
			if available[tool] {
				caps = append(caps, operation)
				if available[tool+".reconcile"] {
					reconcile = append(reconcile, operation)
				}
			}
		}
	}
	for operation, tool := range m.Spec.ReconcileOperations {
		if available[tool] {
			reconcile = append(reconcile, operation)
		}
	}
	value := NormalizeDeclaration(Declaration{APIVersion: ProtocolV1Alpha1, Kind: "AdapterCapabilities", Domain: m.Spec.Domain, Capabilities: caps, Reconcile: reconcile})
	if err := ValidateDeclaration(value); err != nil {
		return Declaration{}, err
	}
	return value, nil
}

func (m *MCP) Invoke(ctx context.Context, request InvokeRequest) (Result, error) {
	if err := ValidateInvokeRequest(request); err != nil {
		return Result{}, err
	}
	session, err := m.start(ctx, request.Workspace)
	if err != nil {
		return Result{}, err
	}
	defer session.close()
	tool := m.toolFor(request.Operation)
	input, err := objectInput(request.Input)
	if err != nil {
		return Result{}, err
	}
	if request.IdempotencyKey != "" {
		input["idempotency_key"] = request.IdempotencyKey
	}
	raw, isErr, err := session.callTool(tool, input)
	if err != nil {
		return Result{}, err
	}
	result, err := normalizeMCPResult(raw, isErr)
	if err != nil {
		return Result{}, err
	}
	return result, validateResult(result)
}

func (m *MCP) Reconcile(ctx context.Context, request ReconcileRequest) (ReconcileResult, error) {
	if err := ValidateReconcileRequest(request); err != nil {
		return ReconcileResult{}, err
	}
	tool := m.Spec.ReconcileOperations[request.Operation]
	if tool == "" {
		return ReconcileResult{}, fmt.Errorf("MCP domain adapter does not provide reconcile for %s", request.Operation)
	}
	session, err := m.start(ctx, request.Workspace)
	if err != nil {
		return ReconcileResult{}, err
	}
	defer session.close()
	input, err := objectInput(request.Input)
	if err != nil {
		return ReconcileResult{}, err
	}
	input["idempotency_key"] = request.IdempotencyKey
	if request.Receipt != "" {
		input["receipt"] = request.Receipt
	}
	raw, isErr, err := session.callTool(tool, input)
	if err != nil {
		return ReconcileResult{}, err
	}
	if isErr {
		return ReconcileResult{Outcome: "unknown", Error: string(raw)}, nil
	}
	var value ReconcileResult
	if err := json.Unmarshal(raw, &value); err != nil {
		return ReconcileResult{}, fmt.Errorf("decode MCP reconcile result: %w", err)
	}
	if err := validateReconcile(value); err != nil {
		return ReconcileResult{}, err
	}
	return value, nil
}

func (m *MCP) toolFor(operation string) string {
	if value := m.Spec.Operations[operation]; value != "" {
		return value
	}
	return m.Spec.Domain + "." + operation
}

func (m *MCP) start(ctx context.Context, workspace string) (*mcpSession, error) {
	if len(m.Spec.Argv) == 0 {
		return nil, fmt.Errorf("MCP domain adapter requires argv")
	}
	var cancel context.CancelFunc
	if m.Spec.Timeout != "" {
		timeout, err := time.ParseDuration(m.Spec.Timeout)
		if err != nil {
			return nil, err
		}
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	cmd := exec.CommandContext(ctx, m.Spec.Argv[0], m.Spec.Argv[1:]...)
	if workspace != "" {
		cmd.Dir = workspace
	}
	cmd.Env = os.Environ()
	for k, v := range m.Spec.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	session := &mcpSession{cmd: cmd, stdin: stdin, scanner: bufio.NewScanner(stdout), next: 1, cancel: cancel}
	session.scanner.Buffer(make([]byte, 64<<10), int(outputLimit(m.Spec.MaxOutputBytes)))
	cmd.Stderr = &session.stderr
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	if _, err := session.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "takt", "version": "0.1.42-alpha"}}); err != nil {
		session.close()
		return nil, err
	}
	if err := session.notify("notifications/initialized", map[string]any{}); err != nil {
		session.close()
		return nil, err
	}
	return session, nil
}

func (s *mcpSession) close() {
	_ = s.stdin.Close()
	if s.cancel != nil {
		s.cancel()
	}
	_ = s.cmd.Wait()
}
func (s *mcpSession) notify(method string, params any) error {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, err := s.stdin.Write(append(raw, '\n'))
	return err
}
func (s *mcpSession) request(method string, params any) (json.RawMessage, error) {
	id := s.next
	s.next++
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if _, err := s.stdin.Write(append(raw, '\n')); err != nil {
		return nil, err
	}
	for s.scanner.Scan() {
		var resp rpcResponse
		if json.Unmarshal(s.scanner.Bytes(), &resp) != nil {
			continue
		}
		var rid int
		if json.Unmarshal(resp.ID, &rid) != nil || rid != id {
			continue
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
	if err := s.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("MCP %s ended without response: %s", method, strings.TrimSpace(s.stderr.String()))
}
func (s *mcpSession) listTools() ([]string, error) {
	raw, err := s.request("tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var value struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(value.Tools))
	for _, t := range value.Tools {
		out = append(out, t.Name)
	}
	return out, nil
}
func (s *mcpSession) callTool(name string, args map[string]any) (json.RawMessage, bool, error) {
	raw, err := s.request("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, false, err
	}
	var value struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false, err
	}
	if len(value.Structured) > 0 && string(value.Structured) != "null" {
		return value.Structured, value.IsError, nil
	}
	var texts []string
	for _, item := range value.Content {
		if item.Type == "text" {
			texts = append(texts, item.Text)
		}
	}
	text := strings.Join(texts, "\n")
	var candidate json.RawMessage
	if json.Valid([]byte(text)) {
		candidate = json.RawMessage(text)
	} else {
		candidate, _ = json.Marshal(map[string]any{"output": text})
	}
	return candidate, value.IsError, nil
}

func objectInput(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("domain adapter input must be a JSON object: %w", err)
	}
	return value, nil
}
func normalizeMCPResult(raw json.RawMessage, isErr bool) (Result, error) {
	var envelope struct {
		Status    string          `json:"status"`
		Output    json.RawMessage `json:"output"`
		Receipt   string          `json:"receipt"`
		ErrorCode string          `json:"error_code"`
		Error     string          `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Status != "" {
		return Result{Status: envelope.Status, Output: envelope.Output, Receipt: envelope.Receipt, ErrorCode: envelope.ErrorCode, Error: envelope.Error}, nil
	}
	if isErr {
		return Result{Status: "failed", Error: string(raw)}, nil
	}
	return Result{Status: "completed", Output: raw}, nil
}
