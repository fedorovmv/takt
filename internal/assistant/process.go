package assistant

import (
	"context"
	"encoding/json"
	"fmt"
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

	protocolRequest := ProtocolRequest{}
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
