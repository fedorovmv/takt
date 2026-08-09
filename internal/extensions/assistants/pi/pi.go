package pi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
)

// Pi integrates the earendil-works/pi coding-agent CLI through its RPC mode.
// The adapter owns one short-lived Pi RPC process per Takt node attempt. Pi
// remains responsible for its internal tool loop, files, shell, skills, and
// session persistence; Takt only supplies the prompt and normalizes the result.
type Pi struct {
	spec              spec.AssistantSpec
	onOutputTruncated func()
}

func NewPi(assistantSpec spec.AssistantSpec) Pi {
	return Pi{spec: assistantSpec}
}

// WithOutputTruncatedObserver returns a copy that invokes observer when the
// shared stdout/stderr budget is exceeded. The callback does not replace the
// adapter's cancellation behavior and is primarily useful for integration
// instrumentation and deterministic contract tests.
func (p Pi) WithOutputTruncatedObserver(observer func()) Pi {
	p.onOutputTruncated = observer
	return p
}

const defaultPiOutputLimit = 10 * 1024 * 1024

func (p Pi) Run(ctx context.Context, req Request) (Result, error) {
	binary := strings.TrimSpace(p.spec.Binary)
	if binary == "" {
		binary = "pi"
	}
	if err := validatePiArgs(p.spec.Args); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "pi adapter", Err: err}
	}
	if err := validatePiPolicy(req.Policy); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "pi policy", Err: err}
	}

	env := piEnvironment(p.spec, req)
	version, err := probePiVersion(ctx, binary, req.Workspace, env)
	if err != nil {
		return Result{}, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := piArgs(p.spec, req)
	cmd := exec.CommandContext(runCtx, binary, args...)
	execution.ConfigureCommand(cmd)
	cmd.Dir = req.Workspace
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "pi rpc stdin", Err: err}
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "pi rpc stdout", Err: err}
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, Op: "pi rpc stderr", Err: err}
	}

	limit := p.spec.MaxOutputBytes
	if limit == 0 {
		limit = defaultPiOutputLimit
	}
	var limitOnce sync.Once
	budget := core.NewOutputBudget(limit, func() {
		if p.onOutputTruncated != nil {
			p.onOutputTruncated()
		}
		limitOnce.Do(cancel)
	})
	stdout := newLimitedBuffer(budget)
	stderr := newLimitedBuffer(budget)

	if err := cmd.Start(); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "pi rpc", Err: err}
	}

	records := make(chan piRPCRecord, 64)
	streamErr := make(chan error, 2)
	go readPiRPC(stdoutPipe, stdout, records, streamErr)
	go func() {
		_, copyErr := io.Copy(stderr, stderrPipe)
		if copyErr != nil && !errors.Is(copyErr, os.ErrClosed) {
			streamErr <- fmt.Errorf("read pi stderr: %w", copyErr)
		}
	}()
	processWait := newPiProcessWait(cmd)

	client := &piRPCClient{stdin: stdin, records: records, streamErr: streamErr, process: processWait}
	finish := func(result Result, runErr error) (Result, error) {
		result.AssistantVersion = version
		_ = stdin.Close()
		waitErr := client.waitProcess(ctx)
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
		result.Truncated = stdout.Truncated() || stderr.Truncated()
		// The caller context is authoritative. A deadline or cancellation that
		// coincides with output overflow must retain timed_out/cancelled semantics
		// instead of being reclassified as a protocol failure.
		if priorityErr := piPriorityError(ctx, result.Truncated); priorityErr != nil {
			return result, priorityErr
		}
		if runErr != nil {
			return result, runErr
		}
		if waitErr != nil {
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				result.ExitCode = exitErr.ExitCode()
				return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "pi rpc", Err: waitErr}
			}
			return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "pi rpc", Err: waitErr}
		}
		return result, nil
	}

	stateBeforeRaw, err := client.call(ctx, "state-before", map[string]any{"type": "get_state"})
	if err != nil {
		cancel()
		return finish(Result{ExitCode: -1}, err)
	}
	stateBefore, err := decodePiState(stateBeforeRaw)
	if err != nil {
		cancel()
		return finish(Result{ExitCode: -1}, protocolPiError("decode initial state", err))
	}
	statsBeforeRaw, err := client.call(ctx, "stats-before", map[string]any{"type": "get_session_stats"})
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}
	mode, requestedSessionID := effectiveSession(req.SessionMode, req.SessionID)
	if mode == "resume" && stateBefore.SessionID != requestedSessionID {
		cancel()
		return finish(Result{ExitCode: -1}, protocolPiError("resume session", fmt.Errorf("pi opened session %q instead of %q", stateBefore.SessionID, requestedSessionID)))
	}
	if mode == "fresh" && stateBefore.SessionID == "" {
		cancel()
		return finish(Result{ExitCode: -1}, protocolPiError("fresh session", fmt.Errorf("pi did not report a session id")))
	}

	if _, err := client.call(ctx, "prompt", map[string]any{"type": "prompt", "message": req.Prompt}); err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: 1}, err)
	}
	settled, err := client.waitAgentSettled(ctx)
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}

	messagesRaw, err := client.call(ctx, "messages-after", map[string]any{"type": "get_messages"})
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}
	failure := piAgentFailure(messagesRaw)
	if failure == "" && len(settled.LastAgentEnd.Raw) > 0 {
		failure = piAgentFailure(settled.LastAgentEnd.Raw)
	}

	textRaw, err := client.call(ctx, "last-text", map[string]any{"type": "get_last_assistant_text"})
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}
	text, err := decodePiText(textRaw)
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, protocolPiError("decode assistant text", err))
	}
	statsAfterRaw, err := client.call(ctx, "stats-after", map[string]any{"type": "get_session_stats"})
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}
	usage, err := deltaPiUsage(statsBeforeRaw, statsAfterRaw)
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, protocolPiError("calculate attempt usage", err))
	}
	stateAfterRaw, err := client.call(ctx, "state-after", map[string]any{"type": "get_state"})
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, err)
	}
	stateAfter, err := decodePiState(stateAfterRaw)
	if err != nil {
		cancel()
		return finish(Result{SessionID: stateBefore.SessionID, ExitCode: -1}, protocolPiError("decode final state", err))
	}
	if stateAfter.SessionID == "" {
		stateAfter.SessionID = stateBefore.SessionID
	}
	if mode == "resume" && stateAfter.SessionID != requestedSessionID {
		cancel()
		return finish(Result{SessionID: stateAfter.SessionID, ExitCode: -1}, protocolPiError("verify resumed session", fmt.Errorf("pi finished in session %q instead of %q", stateAfter.SessionID, requestedSessionID)))
	}

	structured, _ := json.Marshal(map[string]any{
		"adapter":           "pi",
		"pi_version":        version,
		"session_file":      stateAfter.SessionFile,
		"thinking_level":    stateAfter.ThinkingLevel,
		"usage_semantics":   "attempt_delta",
		"stats_before":      json.RawMessage(statsBeforeRaw),
		"stats_after":       json.RawMessage(statsAfterRaw),
		"low_level_runs":    settled.LowLevelRuns,
		"automatic_retries": settled.AutomaticRetries,
	})
	result := Result{
		Output:     text,
		Structured: structured,
		SessionID:  stateAfter.SessionID,
		Resumed:    mode == "resume",
		ExitCode:   0,
		Usage:      usage,
	}
	result.ResolvedModel = piResolvedModel(messagesRaw, req.ModelName, stateAfter.Model)
	if failure != "" {
		result.ExitCode = 1
		if result.Output == "" {
			result.Output = failure
		}
		return finish(result, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "pi agent", Err: errors.New(failure)})
	}
	return finish(result, nil)
}

func piPriorityError(ctx context.Context, truncated bool) error {
	if ctx != nil && ctx.Err() != nil {
		kind := execution.KindCancelled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = execution.KindTimedOut
		}
		return &execution.Error{Kind: kind, ExitCode: -1, Op: "pi rpc", Err: ctx.Err()}
	}
	if truncated {
		return &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "pi rpc", Err: fmt.Errorf("pi output exceeded max_output_bytes")}
	}
	return nil
}

func piArgs(s spec.AssistantSpec, req Request) []string {
	args := []string{"--mode", "rpc"}
	if req.Model.Provider != "" {
		args = append(args, "--provider", req.Model.Provider)
	}
	if req.Model.ID != "" {
		args = append(args, "--model", req.Model.ID)
	}
	if thinking := piThinking(req.Model.Params); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	if s.SessionDir != "" {
		args = append(args, "--session-dir", s.SessionDir)
	}
	switch s.ProjectTrust {
	case "approve":
		args = append(args, "--approve")
	case "deny":
		args = append(args, "--no-approve")
	}
	mode, id := effectiveSession(req.SessionMode, req.SessionID)
	if mode == "resume" {
		args = append(args, "--session", id)
	}
	if tools := effectivePiTools(req.Policy); len(tools) > 0 {
		args = append(args, "--tools", strings.Join(tools, ","))
	} else if req.Policy.ToolsRestricted {
		args = append(args, "--no-tools")
	}
	if req.Policy.SkillsRestricted || len(req.Policy.Skills) > 0 {
		args = append(args, "--no-skills")
		for _, skill := range req.Policy.Skills {
			args = append(args, "--skill", skill)
		}
	}
	args = append(args, s.Args...)
	return args
}

func validatePiArgs(args []string) error {
	reserved := map[string]struct{}{
		"--mode": {}, "--provider": {}, "--model": {}, "--thinking": {}, "--session": {}, "--session-id": {}, "--session-dir": {},
		"--no-session": {}, "--continue": {}, "-c": {}, "--resume": {}, "-r": {}, "--fork": {},
		"--print": {}, "-p": {}, "--version": {}, "-v": {}, "--help": {}, "-h": {},
		"--approve": {}, "-a": {}, "--no-approve": {}, "-na": {},
		"--tools": {}, "-t": {}, "--no-tools": {}, "-nt": {}, "--no-builtin-tools": {}, "-nbt": {},
		"--skill": {}, "--no-skills": {},
	}
	for _, arg := range args {
		key := arg
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		if _, found := reserved[key]; found {
			return fmt.Errorf("pi args cannot override reserved option %q", key)
		}
	}
	return nil
}

func validatePiPolicy(policy Policy) error {
	if policy.Filesystem == "read_only" {
		writable := map[string]bool{"bash": true, "edit": true, "write": true}
		for _, tool := range policy.AllowedTools {
			if writable[tool] {
				return fmt.Errorf("read_only sandbox cannot allow Pi tool %q", tool)
			}
		}
	}
	for _, skill := range policy.Skills {
		if strings.TrimSpace(skill) == "" {
			return fmt.Errorf("skill path must not be empty")
		}
		if _, err := os.Stat(skill); err != nil {
			return fmt.Errorf("resolve Pi skill %q: %w", skill, err)
		}
	}
	return nil
}

func effectivePiTools(policy Policy) []string {
	base := []string{"read", "bash", "edit", "write", "grep", "find", "ls"}
	denied := map[string]bool{}
	for _, value := range policy.DeniedTools {
		denied[value] = true
	}
	if policy.Filesystem == "read_only" {
		denied["bash"], denied["edit"], denied["write"] = true, true, true
	}
	values := policy.AllowedTools
	if len(values) == 0 && !policy.ToolsRestricted && (len(denied) > 0 || policy.Filesystem == "read_only") {
		values = base
	}
	if len(values) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || denied[value] || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func piThinking(params map[string]any) string {
	for _, key := range []string{"thinking", "reasoning_effort"} {
		if raw, ok := params[key]; ok {
			if value, ok := raw.(string); ok {
				switch value {
				case "off", "minimal", "low", "medium", "high", "xhigh", "max":
					return value
				}
			}
		}
	}
	return ""
}

func piEnvironment(s spec.AssistantSpec, req Request) []string {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	if _, found := values["PI_SKIP_VERSION_CHECK"]; !found {
		values["PI_SKIP_VERSION_CHECK"] = "1"
	}
	for key, value := range s.Env {
		values[key] = renderArg(value, req)
	}
	paramsJSON, _ := json.Marshal(req.Model.Params)
	metadataJSON, _ := json.Marshal(req.Metadata)
	mode, sessionID := effectiveSession(req.SessionMode, req.SessionID)
	values["TAKT_RUN_ID"] = req.RunID
	values["TAKT_NODE_ID"] = req.NodeID
	values["TAKT_ATTEMPT"] = strconv.Itoa(req.Attempt)
	values["TAKT_WORKSPACE"] = req.Workspace
	values["TAKT_MODEL_NAME"] = req.ModelName
	values["TAKT_MODEL_PROVIDER"] = req.Model.Provider
	values["TAKT_MODEL_ID"] = req.Model.ID
	values["TAKT_MODEL_PARAMS_JSON"] = string(paramsJSON)
	values["TAKT_SESSION_MODE"] = mode
	values["TAKT_SESSION_ID"] = sessionID
	values["TAKT_METADATA_JSON"] = string(metadataJSON)
	values["TAKT_NATIVE_HOOKS_JSON"] = ""
	policyJSON, _ := json.Marshal(req.Policy)
	values["TAKT_POLICY_JSON"] = string(policyJSON)
	if len(req.NativeHooks) > 0 {
		if compact, err := compactJSON(req.NativeHooks); err == nil {
			values["TAKT_NATIVE_HOOKS_JSON"] = compact
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}

func probePiVersion(ctx context.Context, binary, workspace string, env []string) (string, error) {
	return probePiVersionWithTimeout(ctx, binary, workspace, env, 10*time.Second)
}

func probePiVersionWithTimeout(ctx context.Context, binary, workspace string, env []string, timeout time.Duration) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, "--version")
	execution.ConfigureCommand(cmd)
	cmd.Dir = workspace
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = execution.KindTimedOut
		}
		return "", &execution.Error{Kind: kind, ExitCode: -1, Op: "pi version", Err: ctx.Err()}
	}
	if probeCtx.Err() != nil {
		return "", &execution.Error{Kind: execution.KindTimedOut, ExitCode: -1, Op: "pi version", Err: probeCtx.Err()}
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", &execution.Error{Kind: execution.KindProtocol, ExitCode: exitErr.ExitCode(), Op: "pi version", Err: fmt.Errorf("version probe exited with code %d", exitErr.ExitCode())}
		}
		return "", &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "pi version", Err: err}
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", protocolPiError("pi version", fmt.Errorf("empty version output"))
	}
	return version, nil
}

type piRPCRecord struct {
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	Command string          `json:"command,omitempty"`
	Success *bool           `json:"success,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

type piRPCClient struct {
	stdin     io.WriteCloser
	records   <-chan piRPCRecord
	streamErr <-chan error
	process   *piProcessWait
	backlog   []piRPCRecord
}

type piProcessWait struct {
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func newPiProcessWait(cmd *exec.Cmd) *piProcessWait {
	w := &piProcessWait{done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.done)
	}()
	return w
}

func (w *piProcessWait) wait(ctx context.Context) error {
	select {
	case <-w.done:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *piProcessWait) result() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (c *piRPCClient) send(id string, command map[string]any) error {
	command["id"] = id
	encoded, err := json.Marshal(command)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	_, err = c.stdin.Write(encoded)
	return err
}

func (c *piRPCClient) call(ctx context.Context, id string, command map[string]any) (json.RawMessage, error) {
	if err := c.send(id, command); err != nil {
		return nil, protocolPiError("send rpc command", err)
	}
	record, err := c.next(ctx, func(r piRPCRecord) bool { return r.Type == "response" && r.ID == id })
	if err != nil {
		return nil, err
	}
	if record.Success == nil {
		return nil, protocolPiError("decode rpc response", fmt.Errorf("response %q has no success field", id))
	}
	if !*record.Success {
		message := record.Error
		if message == "" {
			message = "Pi rejected RPC command"
		}
		return nil, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "pi " + record.Command, Err: errors.New(message)}
	}
	return record.Data, nil
}

func (c *piRPCClient) waitEvent(ctx context.Context, eventType string) (piRPCRecord, error) {
	return c.next(ctx, func(r piRPCRecord) bool { return r.Type == eventType })
}

type piSettledResult struct {
	LastAgentEnd     piRPCRecord
	LowLevelRuns     int
	AutomaticRetries int
}

func (c *piRPCClient) waitAgentSettled(ctx context.Context) (piSettledResult, error) {
	if _, err := c.waitEvent(ctx, "agent_settled"); err != nil {
		return piSettledResult{}, err
	}
	var result piSettledResult
	for _, record := range c.backlog {
		switch record.Type {
		case "agent_end":
			result.LastAgentEnd = record
			result.LowLevelRuns++
		case "auto_retry_start":
			result.AutomaticRetries++
		}
	}
	return result, nil
}

func (c *piRPCClient) next(ctx context.Context, match func(piRPCRecord) bool) (piRPCRecord, error) {
	for i, record := range c.backlog {
		if match(record) {
			c.backlog = append(c.backlog[:i], c.backlog[i+1:]...)
			return record, nil
		}
	}
	for {
		select {
		case <-ctx.Done():
			kind := execution.KindCancelled
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				kind = execution.KindTimedOut
			}
			return piRPCRecord{}, &execution.Error{Kind: kind, ExitCode: -1, Op: "pi rpc", Err: ctx.Err()}
		case err := <-c.streamErr:
			if err != nil {
				return piRPCRecord{}, protocolPiError("read rpc stream", err)
			}
		case <-c.process.done:
			waitErr := c.process.result()
			if waitErr == nil {
				return piRPCRecord{}, protocolPiError("read rpc stream", io.ErrUnexpectedEOF)
			}
			var exitErr *exec.ExitError
			if errors.As(waitErr, &exitErr) {
				return piRPCRecord{}, &execution.Error{Kind: execution.KindExit, ExitCode: exitErr.ExitCode(), Op: "pi rpc", Err: waitErr}
			}
			return piRPCRecord{}, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "pi rpc", Err: waitErr}
		case record, ok := <-c.records:
			if !ok {
				select {
				case <-ctx.Done():
					kind := execution.KindCancelled
					if errors.Is(ctx.Err(), context.DeadlineExceeded) {
						kind = execution.KindTimedOut
					}
					return piRPCRecord{}, &execution.Error{Kind: kind, ExitCode: -1, Op: "pi rpc", Err: ctx.Err()}
				case <-c.process.done:
					waitErr := c.process.result()
					if waitErr == nil {
						return piRPCRecord{}, protocolPiError("read rpc stream", io.ErrUnexpectedEOF)
					}
					var exitErr *exec.ExitError
					if errors.As(waitErr, &exitErr) {
						return piRPCRecord{}, &execution.Error{Kind: execution.KindExit, ExitCode: exitErr.ExitCode(), Op: "pi rpc", Err: waitErr}
					}
					return piRPCRecord{}, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "pi rpc", Err: waitErr}
				case <-time.After(50 * time.Millisecond):
					return piRPCRecord{}, protocolPiError("read rpc stream", io.ErrUnexpectedEOF)
				}
			}
			if record.Type == "extension_ui_request" {
				var ui struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(record.Raw, &ui)
				allowed := map[string]struct{}{
					"notify": {}, "setStatus": {}, "setWidget": {}, "setTitle": {}, "set_editor_text": {},
				}
				if _, ok := allowed[ui.Method]; !ok {
					return piRPCRecord{}, protocolPiError("extension UI", fmt.Errorf("interactive Pi extension request %q is unsupported", ui.Method))
				}
			}
			if match(record) {
				return record, nil
			}
			c.backlog = append(c.backlog, record)
		}
	}
}

func (c *piRPCClient) waitProcess(ctx context.Context) error {
	err := c.process.wait(ctx)
	if err == nil || !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	grace, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if waitErr := c.process.wait(grace); waitErr != nil && !errors.Is(waitErr, context.DeadlineExceeded) {
		return waitErr
	}
	return err
}

func readPiRPC(reader io.Reader, capture *limitedBuffer, records chan<- piRPCRecord, errs chan<- error) {
	defer close(records)
	buffered := bufio.NewReaderSize(reader, 64*1024)
	var line bytes.Buffer
	for {
		fragment, err := buffered.ReadSlice('\n')
		if len(fragment) > 0 {
			_, _ = capture.Write(fragment)
			if capture.Truncated() {
				errs <- fmt.Errorf("pi stdout exceeded max_output_bytes")
				return
			}
			_, _ = line.Write(fragment)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if line.Len() > 0 {
			trimmed := bytes.TrimSpace(line.Bytes())
			if len(trimmed) > 0 {
				var record piRPCRecord
				if decodeErr := json.Unmarshal(trimmed, &record); decodeErr != nil {
					errs <- fmt.Errorf("decode pi RPC JSON: %w", decodeErr)
					return
				}
				if record.Type == "" {
					errs <- fmt.Errorf("Pi RPC record has no type")
					return
				}
				record.Raw = append(record.Raw[:0], trimmed...)
				records <- record
			}
			line.Reset()
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				errs <- fmt.Errorf("read pi RPC JSONL: %w", err)
			}
			return
		}
	}
}

type piState struct {
	SessionID     string   `json:"sessionId"`
	SessionFile   string   `json:"sessionFile"`
	ThinkingLevel string   `json:"thinkingLevel"`
	Model         *piModel `json:"model"`
}

type piModel struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

func decodePiState(raw json.RawMessage) (piState, error) {
	var state piState
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return state, fmt.Errorf("empty state response")
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	return state, nil
}

func decodePiText(raw json.RawMessage) (string, error) {
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	return result.Text, nil
}

func decodePiUsage(raw json.RawMessage) *ProtocolUsage {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	input, inputOK := findJSONNumber(value, "inputTokens", "input_tokens", "input")
	output, outputOK := findJSONNumber(value, "outputTokens", "output_tokens", "output")
	cost, costOK := findJSONNumber(value, "cost", "totalCost", "total_cost")
	if !inputOK && !outputOK && !costOK {
		return nil
	}
	return &ProtocolUsage{InputTokens: int(input), OutputTokens: int(output), Cost: cost}
}

func deltaPiUsage(beforeRaw, afterRaw json.RawMessage) (*ProtocolUsage, error) {
	before := decodePiUsage(beforeRaw)
	after := decodePiUsage(afterRaw)
	if after == nil {
		if before != nil {
			return nil, fmt.Errorf("Pi session statistics lost cumulative usage: before=%+v after=<missing>", *before)
		}
		return nil, nil
	}
	if before == nil {
		before = &ProtocolUsage{}
	}
	input := after.InputTokens - before.InputTokens
	output := after.OutputTokens - before.OutputTokens
	cost := after.Cost - before.Cost
	const epsilon = 1e-9
	if input < 0 || output < 0 || cost < -epsilon {
		return nil, fmt.Errorf("Pi session statistics decreased: before=%+v after=%+v", *before, *after)
	}
	if cost < 0 {
		cost = 0
	}
	return &ProtocolUsage{InputTokens: input, OutputTokens: output, Cost: cost}, nil
}

func findJSONNumber(value any, keys ...string) (float64, bool) {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if raw, ok := typed[key]; ok {
				switch number := raw.(type) {
				case float64:
					return number, true
				case json.Number:
					v, err := number.Float64()
					return v, err == nil
				}
			}
		}
		for _, child := range typed {
			if number, ok := findJSONNumber(child, keys...); ok {
				return number, true
			}
		}
	case []any:
		for _, child := range typed {
			if number, ok := findJSONNumber(child, keys...); ok {
				return number, true
			}
		}
	}
	return 0, false
}

func piResolvedModel(raw json.RawMessage, logicalName string, fallback *piModel) *ProtocolModel {
	var payload struct {
		Messages []struct {
			Role          string `json:"role"`
			Provider      string `json:"provider"`
			Model         string `json:"model"`
			ResponseModel string `json:"responseModel"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &payload) == nil {
		for i := len(payload.Messages) - 1; i >= 0; i-- {
			message := payload.Messages[i]
			if message.Role != "assistant" {
				continue
			}
			provider := message.Provider
			id := message.ResponseModel
			if id == "" {
				id = message.Model
			}
			if fallback != nil {
				if provider == "" {
					provider = fallback.Provider
				}
				if id == "" {
					id = fallback.ID
				}
			}
			if provider != "" || id != "" {
				return &ProtocolModel{Name: logicalName, Provider: provider, ID: id}
			}
			break
		}
	}
	if fallback == nil {
		return nil
	}
	return &ProtocolModel{Name: logicalName, Provider: fallback.Provider, ID: fallback.ID}
}

func piAgentFailure(raw json.RawMessage) string {
	var event struct {
		Messages []struct {
			Role         string `json:"role"`
			StopReason   string `json:"stopReason"`
			ErrorMessage string `json:"errorMessage"`
		} `json:"messages"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return ""
	}
	for i := len(event.Messages) - 1; i >= 0; i-- {
		message := event.Messages[i]
		if message.Role != "assistant" {
			continue
		}
		if message.ErrorMessage != "" {
			return message.ErrorMessage
		}
		if message.StopReason == "error" || message.StopReason == "aborted" {
			return "Pi agent stopped with reason " + message.StopReason
		}
		return ""
	}
	return ""
}

func protocolPiError(op string, err error) error {
	return &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "pi " + op, Err: err}
}
func (p Pi) Capabilities() []string {
	return mergeCapabilities([]string{CapabilityToolPolicy, CapabilitySkills, CapabilitySandboxFilesystem}, p.spec.Capabilities)
}

func (p Pi) CapabilityDeclaration() CapabilityDeclaration {
	return CapabilityDeclaration{
		Capabilities: p.Capabilities(), EventTypes: []string{EventSessionStarted, EventSessionResumed, EventMessage, EventUsage, EventDiagnostic, EventCompleted, EventFailed},
		SessionEvents: true, UsageEvents: true,
	}
}

// ProbeVersion checks the configured Pi CLI without starting an agent session.
func ProbeVersion(ctx context.Context, assistantSpec spec.AssistantSpec, workspace string) (string, error) {
	binary := strings.TrimSpace(assistantSpec.Binary)
	if binary == "" {
		binary = "pi"
	}
	return probePiVersion(ctx, binary, workspace, piEnvironment(assistantSpec, Request{Workspace: workspace, Attempt: 1}))
}
