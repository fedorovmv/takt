package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

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
		return Result{}, fmt.Errorf("empty process argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.Workspace
	cmd.Env = append([]string{}, os.Environ()...)
	for k, v := range p.spec.Env {
		cmd.Env = append(cmd.Env, k+"="+renderArg(v, req))
	}
	paramsJSON, _ := json.Marshal(req.Model.Params)
	modelEnv := []string{
		"TAKT_MODEL_NAME=" + req.ModelName,
		"TAKT_MODEL_ID=" + req.Model.ID,
		"TAKT_MODEL_PROVIDER=" + req.Model.Provider,
		"TAKT_MODEL_PARAMS_JSON=" + string(paramsJSON),
		"TAKT_SESSION_MODE=" + req.SessionMode,
		"TAKT_SESSION_ID=" + req.SessionID,
		"TAKT_WORKSPACE=" + req.Workspace,
		// Deprecated compatibility variables. Remove after the alpha migration window.
		"HARNESS_MODEL_NAME=" + req.ModelName,
		"HARNESS_MODEL_ID=" + req.Model.ID,
		"HARNESS_MODEL_PROVIDER=" + req.Model.Provider,
		"HARNESS_MODEL_PARAMS_JSON=" + string(paramsJSON),
		"HARNESS_SESSION_MODE=" + req.SessionMode,
		"HARNESS_SESSION_ID=" + req.SessionID,
		"HARNESS_WORKSPACE=" + req.Workspace,
	}
	cmd.Env = append(cmd.Env, modelEnv...)
	if len(req.NativeHooks) > 0 {
		var compact bytes.Buffer
		if err := json.Compact(&compact, req.NativeHooks); err == nil {
			cmd.Env = append(cmd.Env,
				"TAKT_NATIVE_HOOKS_JSON="+compact.String(),
				"HARNESS_NATIVE_HOOKS_JSON="+compact.String(),
			)
		}
	}
	if !hasPrompt {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
		return Result{Output: stdout.String() + stderr.String(), ExitCode: exitCode}, fmt.Errorf("assistant process: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return Result{Output: strings.TrimSpace(stdout.String()), SessionID: req.SessionID, ExitCode: exitCode}, nil
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
