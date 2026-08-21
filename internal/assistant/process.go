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
	"takt/internal/redact"
	"takt/internal/spec"
	agentadaptersdk "takt/sdk/agentadapter"
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
		return Result{Adapter: "process"}, &execution.Error{Kind: execution.KindStart, Op: "assistant process", Err: fmt.Errorf("empty process argv")}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	execution.ConfigureCommand(cmd)
	cmd.Dir = req.Workspace
	cmd.Env = append([]string{}, os.Environ()...)
	renderedEnv := make(map[string]string, len(p.spec.Env))
	secretResolver := redact.NewFromEnvironment()
	for k, v := range p.spec.Env {
		rendered := renderArg(v, req)
		resolved, resolveErr := secretResolver.Resolve(rendered)
		if resolveErr != nil {
			return Result{Adapter: "process"}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process secret", Err: resolveErr}
		}
		renderedEnv[k] = resolved
		cmd.Env = append(cmd.Env, k+"="+resolved)
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
		"TAKT_ARTIFACTS_DIR="+req.ArtifactsDir,
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
			return Result{Adapter: "process"}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process", Err: fmt.Errorf("unsupported protocol %q", p.spec.Protocol)}
		}
		protocolRequest = buildProtocolRequest(ctx, req, p.spec, renderedEnv, time.Now())
		encoded, err := encodeProtocolRequest(protocolRequest)
		if err != nil {
			return Result{Adapter: "process"}, &execution.Error{Kind: execution.KindProtocol, Op: "assistant process", Err: err}
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
		Adapter:   "process",
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
	stdin, stdout, stderr, err := startV1Alpha2Process(cmd)
	if err != nil {
		return Result{Adapter: "process"}, err
	}
	budget := &outputBudget{limit: p.spec.MaxOutputBytes}
	rawOut, rawErr := newLimitedBuffer(budget), newLimitedBuffer(budget)
	stderrDone := copyProcessStderr(stderr, rawErr)
	processFinished := false
	defer func() {
		if processFinished {
			return
		}
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		<-stderrDone
		_ = cmd.Wait()
	}()

	protocolRequest := buildProtocolRequest(ctx, req, p.spec, env, time.Now())
	protocolRequest.ProtocolVersion = ProtocolV1Alpha2
	encoder := json.NewEncoder(stdin)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(protocolRequest); err != nil {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2 request", err)
	}

	state, err := p.readV1Alpha2Stream(ctx, stdout, rawOut, req, encoder)
	_ = stdin.Close()
	if err != nil {
		_ = cmd.Process.Kill()
		return Result{Adapter: "process", Stdout: rawOut.String(), Stderr: rawErr.String(), ExitCode: -1, Truncated: rawOut.Truncated() || rawErr.Truncated()}, err
	}

	// Drain stderr before Wait closes the StderrPipe. os/exec documents that
	// calling Wait while reads from StderrPipe are still in flight is incorrect
	// and can surface a spurious "file already closed" under the race detector.
	copyErr := <-stderrDone
	waitErr := cmd.Wait()
	processFinished = true
	if copyErr != nil {
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2 stderr", copyErr)
	}
	return finishV1Alpha2(ctx, protocolRequest, state, waitErr, rawOut, rawErr)
}

type v1Alpha2StreamState struct {
	declarationSeen bool
	declaration     CapabilityDeclaration
	final           *ProtocolResult
}

func startV1Alpha2Process(cmd *exec.Cmd) (io.WriteCloser, io.ReadCloser, io.ReadCloser, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stdin", Err: err}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stdout", Err: err}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2 stderr", Err: err}
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2", Err: err}
	}
	return stdin, stdout, stderr, nil
}

func copyProcessStderr(stderr io.Reader, dst io.Writer) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(dst, stderr)
		done <- err
	}()
	return done
}

func (p Process) readV1Alpha2Stream(ctx context.Context, stdout io.Reader, rawOut io.Writer, req Request, encoder *json.Encoder) (v1Alpha2StreamState, error) {
	state := v1Alpha2StreamState{}
	scanner := bufio.NewScanner(io.TeeReader(stdout, rawOut))
	max := p.spec.MaxOutputBytes
	if max <= 0 || max > 16*1024*1024 {
		max = 16 * 1024 * 1024
	}
	scanner.Buffer(make([]byte, 64*1024), max)
	for scanner.Scan() {
		message, err := decodeV1Alpha2Message(scanner.Text())
		if err != nil {
			return state, err
		}
		if err := p.handleV1Alpha2Message(ctx, req, encoder, &state, message); err != nil {
			return state, err
		}
	}
	if err := scanner.Err(); err != nil {
		return state, protocolError("assistant process v1alpha2 stream", err)
	}
	return state, nil
}

func decodeV1Alpha2Message(line string) (ProtocolStreamMessage, error) {
	var message ProtocolStreamMessage
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&message); err != nil {
		return ProtocolStreamMessage{}, protocolError("assistant process v1alpha2", fmt.Errorf("decode stream record: %w", err))
	}
	if message.ProtocolVersion != ProtocolV1Alpha2 {
		return ProtocolStreamMessage{}, protocolError("assistant process v1alpha2", fmt.Errorf("unsupported stream protocol_version %q", message.ProtocolVersion))
	}
	return message, nil
}

func (p Process) handleV1Alpha2Message(ctx context.Context, req Request, encoder *json.Encoder, state *v1Alpha2StreamState, message ProtocolStreamMessage) error {
	switch message.Type {
	case "capabilities":
		return p.handleV1Alpha2Capabilities(state, message)
	case "event":
		return handleV1Alpha2Event(req, state, message)
	case "tool.request":
		return handleV1Alpha2ToolRequest(ctx, req, encoder, state, message)
	case "result":
		return handleV1Alpha2Result(state, message)
	default:
		return protocolError("assistant process v1alpha2", fmt.Errorf("unsupported stream record type %q", message.Type))
	}
}

func (p Process) handleV1Alpha2Capabilities(state *v1Alpha2StreamState, message ProtocolStreamMessage) error {
	if state.declarationSeen || message.Declaration == nil {
		return protocolError("assistant process v1alpha2", fmt.Errorf("invalid capabilities record"))
	}
	if err := validateStreamDeclaration(*message.Declaration); err != nil {
		return protocolError("assistant process v1alpha2 declaration", err)
	}
	for _, required := range p.spec.Capabilities {
		if !containsString(message.Declaration.Capabilities, required) {
			return protocolError("assistant process v1alpha2 declaration", fmt.Errorf("configured capability %q was not declared by wrapper", required))
		}
	}
	state.declaration = *message.Declaration
	state.declarationSeen = true
	return nil
}

func handleV1Alpha2Event(req Request, state *v1Alpha2StreamState, message ProtocolStreamMessage) error {
	if !state.declarationSeen {
		return protocolError("assistant process v1alpha2", fmt.Errorf("capability declaration must precede event"))
	}
	if message.Event == nil {
		return protocolError("assistant process v1alpha2", fmt.Errorf("event record requires event"))
	}
	if err := ValidateEvent(*message.Event); err != nil {
		return protocolError("assistant process v1alpha2", err)
	}
	if !containsString(state.declaration.EventTypes, message.Event.Type) {
		return protocolError("assistant process v1alpha2", fmt.Errorf("event %q was not declared by wrapper", message.Event.Type))
	}
	Emit(req, *message.Event)
	return nil
}

func handleV1Alpha2ToolRequest(ctx context.Context, req Request, encoder *json.Encoder, state *v1Alpha2StreamState, message ProtocolStreamMessage) error {
	if !state.declarationSeen || !state.declaration.ToolControl {
		return protocolError("assistant process v1alpha2", fmt.Errorf("tool request without declared tool_control"))
	}
	if message.ToolRequest == nil {
		return protocolError("assistant process v1alpha2", fmt.Errorf("tool.request record requires tool_request"))
	}
	decision := policyToolDecision(req.Policy, *message.ToolRequest)
	if req.ToolControl != nil {
		var err error
		decision, err = req.ToolControl.Decide(ctx, *message.ToolRequest)
		if err != nil {
			return protocolError("assistant process v1alpha2 tool decision", err)
		}
	}
	Emit(req, Event{Type: EventToolRequested, Tool: message.ToolRequest.Tool, CallID: message.ToolRequest.CallID, Input: message.ToolRequest.Input, SessionID: message.ToolRequest.SessionID})
	eventType := EventToolAllowed
	if decision.Decision != "allow" {
		eventType = EventToolDenied
	}
	Emit(req, Event{Type: eventType, Tool: message.ToolRequest.Tool, CallID: message.ToolRequest.CallID, Decision: decision.Decision, Reason: decision.Reason, SessionID: message.ToolRequest.SessionID})
	if err := encoder.Encode(ProtocolToolDecisionMessage{ProtocolVersion: ProtocolV1Alpha2, Type: "tool.decision", CallID: message.ToolRequest.CallID, Decision: decision}); err != nil {
		return protocolError("assistant process v1alpha2 tool decision", err)
	}
	return nil
}

func handleV1Alpha2Result(state *v1Alpha2StreamState, message ProtocolStreamMessage) error {
	if !state.declarationSeen {
		return protocolError("assistant process v1alpha2", fmt.Errorf("capability declaration must precede result"))
	}
	if message.Result == nil || state.final != nil {
		return protocolError("assistant process v1alpha2", fmt.Errorf("stream requires exactly one final result"))
	}
	state.final = message.Result
	return nil
}

func finishV1Alpha2(ctx context.Context, protocolRequest ProtocolRequest, state v1Alpha2StreamState, waitErr error, rawOut, rawErr *limitedBuffer) (Result, error) {
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		return Result{Adapter: "process", Stdout: rawOut.String(), Stderr: rawErr.String(), ExitCode: -1}, &execution.Error{Kind: kind, ExitCode: -1, Op: "assistant process v1alpha2", Err: ctx.Err()}
	}
	if !state.declarationSeen {
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2", fmt.Errorf("capability declaration is required"))
	}
	if state.final == nil {
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2", fmt.Errorf("final result is missing"))
	}
	if err := validateProtocolResult(*state.final, protocolRequest.Session, ProtocolV1Alpha2); err != nil {
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2 result", err)
	}
	osExit, err := processExitCode(waitErr)
	if err != nil {
		return Result{Adapter: "process"}, err
	}
	if osExit != *state.final.ExitCode {
		return Result{Adapter: "process"}, protocolError("assistant process v1alpha2", fmt.Errorf("process exit code %d differs from result exit_code %d", osExit, *state.final.ExitCode))
	}
	result := Result{Adapter: "process", Output: state.final.Output, Structured: state.final.Structured, ExitCode: *state.final.ExitCode, Stdout: rawOut.String(), Stderr: rawErr.String(), ResolvedModel: state.final.ResolvedModel, Usage: state.final.Usage, Truncated: rawOut.Truncated() || rawErr.Truncated()}
	if state.final.Session != nil {
		result.SessionID, result.Resumed = state.final.Session.ID, state.final.Session.Resumed
	}
	if result.ExitCode != 0 {
		kind := execution.KindExit
		switch state.final.FailureKind {
		case "timed_out":
			kind = execution.KindTimedOut
		case "cancelled":
			kind = execution.KindCancelled
		case "provider_unavailable":
			kind = execution.KindProviderUnavailable
		}
		executionErr := &execution.Error{Kind: kind, ExitCode: result.ExitCode, Op: "assistant process v1alpha2", Err: waitErr}
		if state.final.RetryAfterMS != nil {
			executionErr.RetryAfter = execution.ProviderRetryAfterMilliseconds(*state.final.RetryAfterMS)
		}
		return result, executionErr
	}
	return result, nil
}

func processExitCode(waitErr error) (int, error) {
	if waitErr == nil {
		return 0, nil
	}
	if ee, ok := waitErr.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 0, &execution.Error{Kind: execution.KindStart, Op: "assistant process v1alpha2", Err: waitErr}
}

func protocolError(op string, err error) error {
	return &execution.Error{Kind: execution.KindProtocol, Op: op, Err: err}
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

// PolicyToolDecision exposes the same fail-closed tool policy used by the
// process protocol so runtime-owned controllers can add enforcement checks
// without bypassing the node policy.
func PolicyToolDecision(policy Policy, request ToolRequest) ToolDecision {
	return policyToolDecision(policy, request)
}

func validateStreamDeclaration(value CapabilityDeclaration) error {
	return agentadaptersdk.ValidateDeclaration(agentadaptersdk.Declaration{
		Protocol: value.Protocol, Capabilities: value.Capabilities, EventTypes: value.EventTypes,
		SessionEvents: value.SessionEvents, ToolEvents: value.ToolEvents, ToolControl: value.ToolControl,
		ArtifactEvents: value.ArtifactEvents, UsageEvents: value.UsageEvents,
	})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func CompactJSON(src json.RawMessage) (string, error) {
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

// RegisterRenderedEnvSecrets registers explicit SecretRefs after request
// templating. This closes the gap where an env value becomes secret://NAME only
// after {{prompt}}, {{session.id}} or another request placeholder is rendered.
func RegisterRenderedEnvSecrets(r *redact.Redactor, s spec.AssistantSpec, req Request) {
	if r == nil {
		return
	}
	for _, value := range s.Env {
		r.RegisterReferences(renderArg(value, req))
	}
}

func RenderArg(s string, req Request) string {
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

type OutputBudget struct {
	mu         sync.Mutex
	limit      int
	used       int
	truncated  bool
	onTruncate func()
}

type LimitedBuffer struct {
	data   []byte
	budget *OutputBudget
}

func NewOutputBudget(limit int, onTruncate func()) *OutputBudget {
	return &OutputBudget{limit: limit, onTruncate: onTruncate}
}

func NewLimitedBuffer(budget *OutputBudget) *LimitedBuffer { return &LimitedBuffer{budget: budget} }

// Internal aliases keep the process adapter source compact while extension adapters
// use the exported helpers across the package boundary.
type outputBudget = OutputBudget
type limitedBuffer = LimitedBuffer

func newLimitedBuffer(budget *OutputBudget) *LimitedBuffer { return NewLimitedBuffer(budget) }
func compactJSON(src json.RawMessage) (string, error)      { return CompactJSON(src) }
func renderArg(s string, req Request) string               { return RenderArg(s, req) }

func (b *LimitedBuffer) Write(p []byte) (int, error) {
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

func (b *LimitedBuffer) String() string {
	if b.budget == nil {
		return string(b.data)
	}
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	return string(b.data)
}
func (b *LimitedBuffer) Truncated() bool {
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
	if p.spec.Protocol != ProtocolV1Alpha2 {
		return value
	}
	// v1alpha2 is a transport capable of carrying these records; it does not
	// guarantee that every external wrapper implements every lifecycle feature.
	// Static preflight therefore stays conservative and follows the explicitly
	// configured capability set. The wrapper's stream declaration remains the
	// authoritative run-time statement.
	value.Protocol = EventProtocolV2
	set := map[string]bool{}
	for _, capability := range value.Capabilities {
		set[capability] = true
	}
	value.SessionEvents = set[CapabilitySessionEvents]
	value.ToolEvents = set[CapabilityToolEvents]
	value.ToolControl = set[CapabilityToolControl]
	value.ArtifactEvents = set[CapabilityArtifactEvents]
	value.UsageEvents = set[CapabilityUsageEvents]
	// The concrete event_types set is known only after the wrapper starts and
	// sends its stream declaration, so static preflight deliberately leaves it
	// empty instead of overclaiming every v2 event.
	return value
}

// CombineOutput joins stdout and stderr for adapters that expose a single
// diagnostic/output field while preserving each stream separately.
func CombineOutput(stdout, stderr string) string { return combineOutput(stdout, stderr) }
