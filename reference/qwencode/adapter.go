package qwencode

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	sdk "takt/sdk/agentadapter"
)

const DefaultBinary = "qwen"

// Adapter translates Qwen Code headless stream-json into takt-assistant/v1alpha2.
// It intentionally does not declare tool_control, skills, MCP or network sandbox
// capabilities. The reference wrapper runs Qwen in safe mode so unconfigured
// project extensions cannot silently widen the Takt execution contract.
type Adapter struct {
	Binary string
}

type event struct {
	Type      string         `json:"type"`
	Message   string         `json:"message,omitempty"`
	Usage     *sdk.Usage     `json:"usage,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

type qwenMessage map[string]any

func (a Adapter) Serve(ctx context.Context, in io.Reader, out, diagnostic io.Writer) int {
	dec := json.NewDecoder(in)
	dec.DisallowUnknownFields()
	var req sdk.Request
	if err := dec.Decode(&req); err != nil {
		fmt.Fprintf(diagnostic, "decode takt request: %v\n", err)
		return 2
	}
	if err := sdk.ValidateRequest(req); err != nil {
		fmt.Fprintf(diagnostic, "validate takt request: %v\n", err)
		return 2
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	declaration := sdk.NormalizeDeclaration(sdk.Declaration{
		Protocol:      sdk.EventProtocolV2,
		Capabilities:  []string{"agent_events_v2", "session_events", "usage_events"},
		EventTypes:    []string{"session.started", "session.resumed", "message", "usage", "diagnostic", "completed", "failed"},
		SessionEvents: true,
		UsageEvents:   true,
	})
	if err := sdk.ValidateDeclaration(declaration); err != nil {
		fmt.Fprintf(diagnostic, "invalid adapter declaration: %v\n", err)
		return 2
	}
	if err := enc.Encode(sdk.TranscriptRecord{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "capabilities", Declaration: &declaration}); err != nil {
		fmt.Fprintf(diagnostic, "write declaration: %v\n", err)
		return 2
	}
	if reason := unsupportedRequest(req); reason != "" {
		_ = emitEvent(enc, event{Type: "diagnostic", Message: reason, Provider: "qwen-code"})
		return emitFailure(enc, req, "", nil, reason, 1, 0)
	}

	binary := strings.TrimSpace(a.Binary)
	if binary == "" {
		binary = strings.TrimSpace(os.Getenv("QWEN_TAKT_QWEN_BINARY"))
	}
	if binary == "" {
		binary = DefaultBinary
	}
	args := []string{"--prompt", req.Prompt, "--output-format", "stream-json", "--safe-mode", "--approval-mode", "yolo"}
	if req.Model.ID != "" {
		args = append(args, "--model", req.Model.ID)
	}
	if req.Session.Mode == "resume" {
		args = append(args, "--resume", req.Session.ID)
	}
	if req.Limits.TimeoutMS > 0 {
		args = append(args, "--max-wall-time", strconv.FormatInt(req.Limits.TimeoutMS, 10)+"ms")
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = req.Workspace
	cmd.Env = append([]string{}, os.Environ()...)
	keys := make([]string, 0, len(req.Environment))
	for key := range req.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cmd.Env = append(cmd.Env, key+"="+req.Environment[key])
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return emitFailure(enc, req, "", nil, err.Error(), 1, 0)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return emitFailure(enc, req, "", nil, err.Error(), 1, 0)
	}
	if err := cmd.Start(); err != nil {
		return emitFailure(enc, req, "", nil, err.Error(), 1, 0)
	}
	stderrDone := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(stderr); stderrDone <- b }()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	sessionID := ""
	modelID := req.Model.ID
	lastText := ""
	var final qwenMessage
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var msg qwenMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			_ = cmd.Process.Kill()
			<-stderrDone
			_ = cmd.Wait()
			return emitFailure(enc, req, sessionID, nil, "decode qwen stream-json: "+err.Error(), 1, 0)
		}
		if id := stringValue(msg["session_id"]); id != "" {
			sessionID = id
		}
		typ := stringValue(msg["type"])
		switch typ {
		case "system":
			if stringValue(msg["subtype"]) == "session_start" {
				if m := stringValue(msg["model"]); m != "" {
					modelID = m
				}
				kind := "session.started"
				resumed := req.Session.Mode == "resume"
				if resumed {
					kind = "session.resumed"
				}
				_ = emitEvent(enc, event{Type: kind, Provider: "qwen-code", SessionID: sessionID, Data: map[string]any{"model": modelID}})
			}
		case "assistant":
			if text := assistantText(msg); text != "" {
				lastText = text
				_ = emitEvent(enc, event{Type: "message", Message: text, Provider: "qwen-code", SessionID: sessionID})
			}
		case "result":
			final = msg
		}
	}
	scanErr := scanner.Err()
	stderrBytes := <-stderrDone
	waitErr := cmd.Wait()
	if len(stderrBytes) > 0 {
		text := strings.TrimSpace(string(stderrBytes))
		if text != "" {
			_ = emitEvent(enc, event{Type: "diagnostic", Message: text, Provider: "qwen-code", SessionID: sessionID})
		}
	}
	if ctx.Err() != nil {
		return emitFailure(enc, req, sessionID, nil, ctx.Err().Error(), 1, exitCode(waitErr))
	}
	if scanErr != nil {
		return emitFailure(enc, req, sessionID, nil, scanErr.Error(), 1, exitCode(waitErr))
	}
	if final == nil {
		return emitFailure(enc, req, sessionID, nil, "qwen stream ended without result record", 1, exitCode(waitErr))
	}
	if req.Session.Mode == "resume" && sessionID != req.Session.ID {
		return emitFailure(enc, req, sessionID, nil, fmt.Sprintf("qwen resumed session %q instead of %q", sessionID, req.Session.ID), 1, exitCode(waitErr))
	}
	output := stringValue(final["result"])
	if output == "" {
		output = lastText
	}
	usage := usageFrom(final["usage"])
	if usage != nil {
		_ = emitEvent(enc, event{Type: "usage", Usage: usage, Provider: "qwen-code", SessionID: sessionID})
	}
	childExit := exitCode(waitErr)
	failed := boolValue(final["is_error"]) || stringValue(final["subtype"]) != "success" || childExit != 0
	code := 0
	status := "completed"
	terminalEvent := "completed"
	if failed {
		code, status, terminalEvent = 1, "failed", "failed"
	}
	_ = emitEvent(enc, event{Type: terminalEvent, Provider: "qwen-code", SessionID: sessionID})
	resumed := req.Session.Mode == "resume"
	resolved := sdk.Model{Name: req.Model.Name, Provider: req.Model.Provider, ID: modelID, Params: req.Model.Params}
	structured, _ := json.Marshal(map[string]any{"adapter": "qwen-code", "qwen_exit_code": childExit, "safe_mode": true, "tool_control": false})
	result := sdk.Result{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "result", Status: status, Output: output, Structured: structured, Session: &sdk.SessionResult{ID: sessionID, Resumed: resumed}, ExitCode: &code, ResolvedModel: &resolved, Usage: usage}
	if err := sdk.ValidateResult(result, func() string {
		if resumed {
			return req.Session.ID
		}
		return ""
	}()); err != nil {
		fmt.Fprintf(diagnostic, "validate terminal result: %v\n", err)
		return 2
	}
	if err := enc.Encode(sdk.TranscriptRecord{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "result", Result: &result}); err != nil {
		return 2
	}
	return code
}

func unsupportedRequest(req sdk.Request) string {
	if len(req.NativeHooks) > 0 && string(req.NativeHooks) != "null" && string(req.NativeHooks) != "{}" {
		return "Qwen reference wrapper does not support native_hooks"
	}
	if req.Policy == nil {
		return ""
	}
	p := req.Policy
	if p.ToolsRestricted || len(p.AllowedTools) > 0 || len(p.DeniedTools) > 0 {
		return "Qwen reference wrapper does not yet claim Takt tool_policy; use an unrestricted node or a host-specific adapter"
	}
	if p.SkillsRestricted || len(p.Skills) > 0 {
		return "Qwen reference wrapper does not support selected Takt skills"
	}
	if p.MCPPath != "" || len(p.MCPConfig) > 0 {
		return "Qwen reference wrapper does not support Takt MCP projection"
	}
	if p.Filesystem != "" || p.Network != "" {
		return "Qwen reference wrapper does not claim Takt sandbox capabilities"
	}
	if len(p.Requires) > 0 {
		return "Qwen reference wrapper cannot satisfy requested capabilities: " + strings.Join(p.Requires, ",")
	}
	return ""
}

func emitEvent(enc *json.Encoder, value event) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return enc.Encode(sdk.TranscriptRecord{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "event", Event: raw})
}

func emitFailure(enc *json.Encoder, req sdk.Request, sessionID string, usage *sdk.Usage, message string, code, childExit int) int {
	_ = emitEvent(enc, event{Type: "failed", Message: message, Provider: "qwen-code", SessionID: sessionID})
	structured, _ := json.Marshal(map[string]any{"adapter": "qwen-code", "qwen_exit_code": childExit, "error": message})
	resumed := req.Session.Mode == "resume"
	result := sdk.Result{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "result", Status: "failed", Output: message, Structured: structured, Session: &sdk.SessionResult{ID: sessionID, Resumed: resumed}, ExitCode: &code, Usage: usage}
	_ = enc.Encode(sdk.TranscriptRecord{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "result", Result: &result})
	return code
}

func assistantText(msg qwenMessage) string {
	message, _ := msg["message"].(map[string]any)
	content, _ := message["content"].([]any)
	var parts []string
	for _, item := range content {
		object, _ := item.(map[string]any)
		if stringValue(object["type"]) == "text" {
			if text := stringValue(object["text"]); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

func usageFrom(value any) *sdk.Usage {
	object, _ := value.(map[string]any)
	if object == nil {
		return nil
	}
	in := intValue(first(object, "input_tokens", "inputTokens", "prompt_tokens"))
	out := intValue(first(object, "output_tokens", "outputTokens", "completion_tokens"))
	cost := floatValue(first(object, "cost", "total_cost_usd", "totalCostUsd"))
	if in == 0 && out == 0 && cost == 0 {
		return nil
	}
	return &sdk.Usage{InputTokens: in, OutputTokens: out, Cost: cost}
}
func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return v
		}
	}
	return nil
}
func stringValue(v any) string { s, _ := v.(string); return s }
func boolValue(v any) bool     { b, _ := v.(bool); return b }
func intValue(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case int:
		return x
	}
	return 0
}
func floatValue(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case json.Number:
		n, _ := x.Float64()
		return n
	case int:
		return float64(x)
	}
	return 0
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
