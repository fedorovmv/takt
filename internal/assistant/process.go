package assistant

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"takt/internal/execution"
	"takt/internal/spec"
)

type Process struct{ spec spec.AssistantSpec }

func (p Process) Run(ctx context.Context, req Request) (Result, error) {
	argv := make([]string, len(p.spec.Argv))
	hasPrompt := false
	for i, arg := range p.spec.Argv {
		if strings.Contains(arg, "{{prompt}}") {
			hasPrompt = true
		}
		argv[i] = renderArg(arg, req)
	}
	if len(argv) == 0 {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process", Err: fmt.Errorf("empty process argv")}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	execution.ConfigureCommand(cmd)
	cmd.Dir = req.Workspace
	cmd.Env = append([]string{}, os.Environ()...)
	renderedEnv := make(map[string]string, len(p.spec.Env))
	for k, v := range p.spec.Env {
		rendered := renderArg(v, req)
		renderedEnv[k] = rendered
		cmd.Env = append(cmd.Env, k+"="+rendered)
	}
	paramsJSON, _ := json.Marshal(req.Model.Params)
	mode, sessionID := effectiveSession(req.SessionMode, req.SessionID)
	cmd.Env = append(cmd.Env,
		"TAKT_RUN_ID="+req.RunID,
		"TAKT_NODE_ID="+req.NodeID,
		fmt.Sprintf("TAKT_ATTEMPT=%d", req.Attempt),
		"TAKT_MODEL_NAME="+req.ModelName,
		"TAKT_MODEL_ID="+req.Model.ID,
		"TAKT_MODEL_PROVIDER="+req.Model.Provider,
		"TAKT_MODEL_PARAMS_JSON="+string(paramsJSON),
		"TAKT_SESSION_MODE="+mode,
		"TAKT_SESSION_ID="+sessionID,
		"TAKT_WORKSPACE="+req.Workspace,
	)
	if len(req.NativeHooks) > 0 {
		if compact, err := compactJSON(req.NativeHooks); err == nil {
			cmd.Env = append(cmd.Env, "TAKT_NATIVE_HOOKS_JSON="+compact)
		}
	}
	if policyJSON, err := json.Marshal(req.Policy); err == nil {
		cmd.Env = append(cmd.Env, "TAKT_POLICY_JSON="+string(policyJSON))
	}

	protocolRequest := ProtocolRequest{}
	if p.spec.Protocol == ProtocolV1Alpha2 {
		return p.runV1Alpha2(ctx, cmd, req, renderedEnv)
	}
	if p.spec.Protocol != "" {
		if p.spec.Protocol != ProtocolV1Alpha1 {
			return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process", Err: fmt.Errorf("unsupported protocol %q", p.spec.Protocol)}
		}
		protocolRequest = buildProtocolRequest(ctx, req, p.spec, renderedEnv, time.Now())
		encoded, err := encodeProtocolRequest(protocolRequest)
		if err != nil {
			return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process", Err: err}
		}
		cmd.Stdin = strings.NewReader(string(encoded))
	} else if !hasPrompt {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}

	budget := &outputBudget{limit: p.spec.MaxOutputBytes}
	stdout := newLimitedBuffer(budget)
	stderr := newLimitedBuffer(budget)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	rawStdout, rawStderr := stdout.String(), stderr.String()
	result := Result{
		Output:    combineOutput(rawStdout, rawStderr),
		SessionID: sessionID,
		ExitCode:  0,
		Stdout:    rawStdout,
		Stderr:    rawStderr,
		Truncated: stdout.Truncated() || stderr.Truncated(),
	}

	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		result.ExitCode = -1
		return result, &execution.Error{Kind: kind, ExitCode: -1, Op: "assistant process", Err: ctx.Err()}
	}
	osExitCode := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			result.ExitCode = -1
			return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "assistant process", Err: err}
		}
		osExitCode = ee.ExitCode()
	}

	if p.spec.Protocol == ProtocolV1Alpha1 {
		if result.Truncated {
			result.ExitCode = -1
			return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "assistant process", Err: fmt.Errorf("assistant protocol output exceeded max_output_bytes")}
		}
		parsed, parseErr := decodeProtocolResult([]byte(rawStdout), protocolRequest.Session)
		if parseErr != nil {
			result.ExitCode = -1
			return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "assistant process", Err: parseErr}
		}
		result.Output = parsed.Output
		result.Structured = parsed.Structured
		result.ExitCode = *parsed.ExitCode
		result.ResolvedModel = parsed.ResolvedModel
		result.Usage = parsed.Usage
		if parsed.Session != nil {
			result.SessionID = parsed.Session.ID
			result.Resumed = parsed.Session.Resumed
		}
		if osExitCode != result.ExitCode {
			return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: result.ExitCode, Op: "assistant process", Err: fmt.Errorf("process exit code %d differs from result exit_code %d", osExitCode, result.ExitCode)}
		}
		if result.ExitCode != 0 {
			return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "assistant process", Err: err}
		}
		return result, nil
	}

	if err == nil {
		return result, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		result.ExitCode = ee.ExitCode()
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "assistant process", Err: err}
	}
	result.ExitCode = -1
	return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "assistant process", Err: err}
}

func (p Process) runV1Alpha2(ctx context.Context, cmd *exec.Cmd, req Request, env map[string]string) (Result, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stdin", Err: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stdout", Err: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stderr", Err: err}
	}
	if err := cmd.Start(); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2", Err: err}
	}
	budget := &outputBudget{limit: p.spec.MaxOutputBytes}
	rawOut, rawErr := newLimitedBuffer(budget), newLimitedBuffer(budget)
	stderrDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(rawErr, stderr)
		stderrDone <- copyErr
	}()
	processFinished := false
	defer func() {
		if processFinished {
			return
		}
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		<-stderrDone
	}()

	protocolRequest := buildProtocolRequest(ctx, req, p.spec, env, time.Now())
	protocolRequest.ProtocolVersion = ProtocolV1Alpha2
	encoder := json.NewEncoder(stdin)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(protocolRequest); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2 request", Err: err}
	}

	scanner := bufio.NewScanner(io.TeeReader(stdout, rawOut))
	max := p.spec.MaxOutputBytes
	if max <= 0 || max > 16*1024*1024 {
		max = 16 * 1024 * 1024
	}
	scanner.Buffer(make([]byte, 64*1024), max)
	var final *ProtocolResult
	declarationSeen := false
	for scanner.Scan() {
		var message ProtocolStreamMessage
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&message); err != nil {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return Result{Stdout: rawOut.String(), Stderr: rawErr.String(), ExitCode: -1}, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "assistant process v1alpha2", Err: fmt.Errorf("decode stream record: %w", err)}
		}
		if message.ProtocolVersion != ProtocolV1Alpha2 {
			return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("unsupported stream protocol_version %q", message.ProtocolVersion)}
		}
		switch message.Type {
		case "capabilities":
			if message.Declaration == nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("capabilities record requires declaration")}
			}
			declarationSeen = true
		case "event":
			if message.Event == nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("event record requires event")}
			}
			if err := ValidateEvent(*message.Event); err != nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: err}
			}
			Emit(req, *message.Event)
		case "tool.request":
			if message.ToolRequest == nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("tool.request record requires tool_request")}
			}
			decision := policyToolDecision(req.Policy, *message.ToolRequest)
			if req.ToolControl != nil {
				decision, err = req.ToolControl.Decide(ctx, *message.ToolRequest)
				if err != nil {
					return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2 tool decision", Err: err}
				}
			}
			Emit(req, Event{Type: EventToolRequested, Tool: message.ToolRequest.Tool, CallID: message.ToolRequest.CallID, Input: message.ToolRequest.Input, SessionID: message.ToolRequest.SessionID})
			eventType := EventToolAllowed
			if decision.Decision != "allow" {
				eventType = EventToolDenied
			}
			Emit(req, Event{Type: eventType, Tool: message.ToolRequest.Tool, CallID: message.ToolRequest.CallID, Decision: decision.Decision, Reason: decision.Reason, SessionID: message.ToolRequest.SessionID})
			if err := encoder.Encode(ProtocolToolDecisionMessage{ProtocolVersion: ProtocolV1Alpha2, Type: "tool.decision", CallID: message.ToolRequest.CallID, Decision: decision}); err != nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2 tool decision", Err: err}
			}
		case "result":
			if message.Result == nil || final != nil {
				return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("stream requires exactly one final result")}
			}
			final = message.Result
		default:
			return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("unsupported stream record type %q", message.Type)}
		}
	}
	_ = stdin.Close()
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return Result{Stdout: rawOut.String(), Stderr: rawErr.String(), ExitCode: -1, Truncated: rawOut.Truncated() || rawErr.Truncated()}, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "assistant process v1alpha2 stream", Err: err}
	}
	// Drain stderr before Wait closes the StderrPipe. os/exec documents that
	// calling Wait while reads from StderrPipe are still in flight is incorrect
	// and can surface a spurious "file already closed" under the race detector.
	copyErr := <-stderrDone
	waitErr := cmd.Wait()
	processFinished = true
	if copyErr != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2 stderr", Err: copyErr}
	}
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		return Result{Stdout: rawOut.String(), Stderr: rawErr.String(), ExitCode: -1}, &execution.Error{Kind: kind, ExitCode: -1, Op: "assistant process v1alpha2", Err: ctx.Err()}
	}
	if !declarationSeen {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("capability declaration is required")}
	}
	if final == nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("final result is missing")}
	}
	final.ProtocolVersion = ProtocolV1Alpha2
	if err := validateProtocolResult(*final, protocolRequest.Session, ProtocolV1Alpha2); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2 result", Err: err}
	}
	osExit := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			osExit = ee.ExitCode()
		} else {
			return Result{}, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2", Err: waitErr}
		}
	}
	if osExit != *final.ExitCode {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process v1alpha2", Err: fmt.Errorf("process exit code %d differs from result exit_code %d", osExit, *final.ExitCode)}
	}
	result := Result{Output: final.Output, Structured: final.Structured, ExitCode: *final.ExitCode, Stdout: rawOut.String(), Stderr: rawErr.String(), ResolvedModel: final.ResolvedModel, Usage: final.Usage, Truncated: rawOut.Truncated() || rawErr.Truncated()}
	if final.Session != nil {
		result.SessionID, result.Resumed = final.Session.ID, final.Session.Resumed
	}
	if result.ExitCode != 0 {
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "assistant process v1alpha2", Err: waitErr}
	}
	return result, nil
}

func policyToolDecision(policy Policy, request ToolRequest) ToolDecision {
	for _, denied := range policy.DeniedTools {
		if denied == request.Tool {
			return ToolDecision{Decision: "deny", Reason: "tool is denied by node policy"}
		}
	}
	if policy.ToolsRestricted {
		allowed := false
		for _, candidate := range policy.AllowedTools {
			if candidate == request.Tool {
				allowed = true
				break
			}
		}
		if !allowed {
			return ToolDecision{Decision: "deny", Reason: "tool is outside allowed_tools"}
		}
	}
	if policy.Filesystem == "read_only" {
		switch request.Tool {
		case "write", "edit", "patch", "bash", "shell", "task":
			return ToolDecision{Decision: "deny", Reason: "tool conflicts with read_only filesystem policy"}
		}
	}
	if policy.Network == "deny" {
		switch request.Tool {
		case "web", "http", "fetch", "network", "browser":
			return ToolDecision{Decision: "deny", Reason: "tool conflicts with network deny policy"}
		}
	}
	return ToolDecision{Decision: "allow", Reason: "allowed by node policy"}
}

func compactJSON(src json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(src, &value); err != nil {
		return "", err
	}
	b, err := json.Marshal(value)
	return string(b), err
}

func combineOutput(stdout, stderr string) string {
	out := strings.TrimSpace(stdout)
	if s := strings.TrimSpace(stderr); s != "" {
		if out != "" {
			out += "\n"
		}
		out += s
	}
	return out
}

func renderArg(s string, req Request) string {
	mode, sessionID := effectiveSession(req.SessionMode, req.SessionID)
	repl := map[string]string{
		"{{prompt}}":         req.Prompt,
		"{{run.id}}":         req.RunID,
		"{{node.id}}":        req.NodeID,
		"{{attempt}}":        fmt.Sprintf("%d", req.Attempt),
		"{{model.name}}":     req.ModelName,
		"{{model.id}}":       req.Model.ID,
		"{{model.provider}}": req.Model.Provider,
		"{{model.params}}":   string(mustJSON(req.Model.Params)),
		"{{workspace}}":      req.Workspace,
		"{{session.mode}}":   mode,
		"{{session.id}}":     sessionID,
	}
	for k, v := range repl {
		s = strings.ReplaceAll(s, k, v)
	}
	return s
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

type outputBudget struct {
	mu         sync.Mutex
	limit      int
	used       int
	truncated  bool
	onTruncate func()
}

type limitedBuffer struct {
	data   []byte
	budget *outputBudget
}

func newLimitedBuffer(budget *outputBudget) *limitedBuffer { return &limitedBuffer{budget: budget} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.budget == nil {
		b.data = append(b.data, p...)
		return original, nil
	}
	b.budget.mu.Lock()
	if b.budget.limit <= 0 {
		b.data = append(b.data, p...)
		b.budget.mu.Unlock()
		return original, nil
	}
	remaining := b.budget.limit - b.budget.used
	if remaining > 0 {
		take := len(p)
		if take > remaining {
			take = remaining
		}
		b.data = append(b.data, p[:take]...)
		b.budget.used += take
	}
	notify := false
	if original > remaining && !b.budget.truncated {
		b.budget.truncated = true
		notify = true
	}
	callback := b.budget.onTruncate
	b.budget.mu.Unlock()
	if notify && callback != nil {
		callback()
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	if b.budget == nil {
		return string(b.data)
	}
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	return string(b.data)
}
func (b *limitedBuffer) Truncated() bool {
	if b.budget == nil {
		return false
	}
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	return b.budget.truncated
}
func (p Process) Capabilities() []string { return mergeCapabilities(nil, p.spec.Capabilities) }

func (p Process) CapabilityDeclaration() CapabilityDeclaration {
	value := CapabilityDeclaration{Capabilities: p.Capabilities()}
	if p.spec.Protocol == ProtocolV1Alpha2 {
		value.EventTypes = EventTypes()
		value.SessionEvents = true
		value.ToolEvents = true
		value.ArtifactEvents = true
		value.UsageEvents = true
		value.ToolControl = true
	}
	return value
}
