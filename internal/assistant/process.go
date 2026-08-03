package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
	for k, v := range p.spec.Env {
		cmd.Env = append(cmd.Env, k+"="+renderArg(v, req))
	}
	paramsJSON, _ := json.Marshal(req.Model.Params)
	cmd.Env = append(cmd.Env,
		"TAKT_MODEL_NAME="+req.ModelName,
		"TAKT_MODEL_ID="+req.Model.ID,
		"TAKT_MODEL_PROVIDER="+req.Model.Provider,
		"TAKT_MODEL_PARAMS_JSON="+string(paramsJSON),
		"TAKT_SESSION_MODE="+req.SessionMode,
		"TAKT_SESSION_ID="+req.SessionID,
		"TAKT_WORKSPACE="+req.Workspace,
	)
	if len(req.NativeHooks) > 0 {
		if compact, err := compactJSON(req.NativeHooks); err == nil {
			cmd.Env = append(cmd.Env, "TAKT_NATIVE_HOOKS_JSON="+compact)
		}
	}
	if !hasPrompt {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	budget := &outputBudget{limit: p.spec.MaxOutputBytes}
	stdout := newLimitedBuffer(budget)
	stderr := newLimitedBuffer(budget)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	result := Result{
		Output:    combineOutput(stdout.String(), stderr.String()),
		SessionID: req.SessionID,
		ExitCode:  0,
		Truncated: stdout.Truncated() || stderr.Truncated(),
	}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		result.ExitCode = -1
		return result, &execution.Error{Kind: kind, ExitCode: -1, Op: "assistant process", Err: ctx.Err()}
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
	repl := map[string]string{
		"{{prompt}}":         req.Prompt,
		"{{model.name}}":     req.ModelName,
		"{{model.id}}":       req.Model.ID,
		"{{model.provider}}": req.Model.Provider,
		"{{model.params}}":   string(mustJSON(req.Model.Params)),
		"{{workspace}}":      req.Workspace,
		"{{session.mode}}":   req.SessionMode,
		"{{session.id}}":     req.SessionID,
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
	limit     int
	used      int
	truncated bool
}

type limitedBuffer struct {
	data   []byte
	budget *outputBudget
}

func newLimitedBuffer(budget *outputBudget) *limitedBuffer { return &limitedBuffer{budget: budget} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if b.budget == nil || b.budget.limit <= 0 {
		b.data = append(b.data, p...)
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
	if original > remaining {
		b.budget.truncated = true
	}
	return original, nil
}

func (b *limitedBuffer) String() string  { return string(b.data) }
func (b *limitedBuffer) Truncated() bool { return b.budget != nil && b.budget.truncated }
