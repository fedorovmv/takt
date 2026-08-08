package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"takt/internal/execution"
	"takt/internal/localsandbox"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
)

func (r *Runner) runScript(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifactsDir string) (execResult, error) {
	definition := node.Script
	if definition == nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "script", Err: fmt.Errorf("missing script definition")}
	}
	workingDir := r.Workspace
	if definition.WorkingDir != "" {
		rendered, err := renderTemplate(definition.WorkingDir, state, local, feedback, artifactsDir)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render script working_directory", Err: err}
		}
		resolved, err := r.resolveExecutionPath(rendered)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve script working_directory", Err: err}
		}
		workingDir = resolved
	}
	policy := localsandbox.Policy{}
	if node.Sandbox != nil {
		policy = localsandbox.Policy{Enforcement: node.Sandbox.Enforcement, Filesystem: node.Sandbox.Filesystem, Network: node.Sandbox.Network}
	}
	var sandboxState *store.SandboxState
	newCommand := func(name string, args ...string) (*exec.Cmd, error) {
		cmd, decision, err := localsandbox.CommandContext(ctx, workingDir, policy, name, args...)
		sandboxState = &store.SandboxState{Requested: decision.Requested, Status: decision.Status, Backend: decision.Backend, Reason: decision.Reason}
		if err != nil {
			return nil, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "OS sandbox", Err: err}
		}
		return cmd, nil
	}

	args := make([]string, len(definition.Args))
	for index, value := range definition.Args {
		rendered, err := renderTemplate(value, state, local, feedback, artifactsDir)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render script argument", Err: err}
		}
		args[index] = rendered
	}
	var cmd *exec.Cmd
	var err error
	runtimeName := strings.TrimSpace(definition.Runtime)
	switch runtimeName {
	case "command":
		renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
		if renderErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render command script path", Err: renderErr}
		}
		path, err := r.resolveExecutionPath(renderedPath)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve command script", Err: err}
		}
		cmd, err = newCommand(path, args...)
		if err != nil {
			return execResult{Sandbox: sandboxState}, err
		}
	case "python":
		if definition.Inline != "" {
			inline, renderErr := renderTemplate(definition.Inline, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render python inline script", Err: renderErr}
			}
			cmd, err = newCommand("python3", append([]string{"-c", inline}, args...)...)
			if err != nil {
				return execResult{Sandbox: sandboxState}, err
			}
		} else {
			renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render python script path", Err: renderErr}
			}
			path, err := r.resolveExecutionPath(renderedPath)
			if err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve python script", Err: err}
			}
			cmd, err = newCommand("python3", append([]string{path}, args...)...)
			if err != nil {
				return execResult{Sandbox: sandboxState}, err
			}
		}
	case "node":
		if definition.Inline != "" {
			inline, renderErr := renderTemplate(definition.Inline, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render node inline script", Err: renderErr}
			}
			cmd, err = newCommand("node", append([]string{"-e", inline}, args...)...)
			if err != nil {
				return execResult{Sandbox: sandboxState}, err
			}
		} else {
			renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render node script path", Err: renderErr}
			}
			path, err := r.resolveExecutionPath(renderedPath)
			if err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve node script", Err: err}
			}
			cmd, err = newCommand("node", append([]string{path}, args...)...)
			if err != nil {
				return execResult{Sandbox: sandboxState}, err
			}
		}
	case "go":
		renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
		if renderErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render go script path", Err: renderErr}
		}
		path, err := r.resolveExecutionPath(renderedPath)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve go script", Err: err}
		}
		cmd, err = newCommand("go", append([]string{"run", path}, args...)...)
		if err != nil {
			return execResult{Sandbox: sandboxState}, err
		}
	case "validation":
		return r.runValidationCommands(ctx, state, node, local, feedback, artifactsDir, workingDir)
	default:
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "script", Err: fmt.Errorf("unsupported script runtime %q", runtimeName)}
	}
	execution.ConfigureCommand(cmd)
	cmd.Dir = workingDir
	cmd.Env = append([]string(nil), os.Environ()...)
	cmd.Env = append(cmd.Env,
		"TAKT_RUN_ID="+state.ID,
		"TAKT_NODE_ID="+node.ID,
		fmt.Sprintf("TAKT_ATTEMPT=%d", state.Nodes[node.ID].Attempts),
		"TAKT_WORKSPACE="+r.Workspace,
		"TAKT_ARTIFACTS_DIR="+artifactsDir,
	)
	if r.redactor == nil {
		r.redactor = redact.NewFromEnvironment()
	}
	for key, value := range definition.Env {
		rendered, err := renderTemplate(value, state, local, feedback, artifactsDir)
		if err != nil {
			return execResult{Sandbox: sandboxState}, &execution.Error{Kind: execution.KindInternal, Op: "render script environment", Err: err}
		}
		resolved, err := r.redactor.Resolve(rendered)
		if err != nil {
			return execResult{Sandbox: sandboxState}, &execution.Error{Kind: execution.KindProtocol, Op: "resolve script secret", Err: err}
		}
		cmd.Env = append(cmd.Env, key+"="+resolved)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	result := execResult{Output: strings.TrimSpace(stdout.String()), Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0, Sandbox: sandboxState}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		result.ExitCode = -1
		return result, &execution.Error{Kind: kind, ExitCode: -1, Op: "script", Err: ctx.Err()}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "script", Err: err}
	}
	result.ExitCode = -1
	return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "script", Err: err}
}

type validationCommandResult struct {
	Command  string `json:"command"`
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

func (r *Runner) runValidationCommands(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifactsDir, workingDir string) (execResult, error) {
	var input struct {
		ValidationCommands []string `json:"validation_commands"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(state.Input)), &input); err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validation commands input", Err: err}
	}
	if len(input.ValidationCommands) == 0 {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validation commands input", Err: fmt.Errorf("validation_commands must contain at least one command")}
	}
	report := struct {
		Status  string                    `json:"status"`
		Results []validationCommandResult `json:"results"`
	}{Status: "ready"}
	policy := localsandbox.Policy{}
	if node.Sandbox != nil {
		policy = localsandbox.Policy{Enforcement: node.Sandbox.Enforcement, Filesystem: node.Sandbox.Filesystem, Network: node.Sandbox.Network}
	}
	var sandboxState *store.SandboxState
	var combinedOut, combinedErr strings.Builder
	firstFailure := 0
	for _, raw := range input.ValidationCommands {
		commandText := strings.TrimSpace(raw)
		if commandText == "" {
			return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validation commands input", Err: fmt.Errorf("validation_commands contains an empty command")}
		}
		cmd, decision, sandboxErr := localsandbox.CommandContext(ctx, workingDir, policy, "sh", "-lc", commandText)
		sandboxState = &store.SandboxState{Requested: decision.Requested, Status: decision.Status, Backend: decision.Backend, Reason: decision.Reason}
		if sandboxErr != nil {
			return execResult{Sandbox: sandboxState}, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "OS sandbox", Err: sandboxErr}
		}
		execution.ConfigureCommand(cmd)
		cmd.Dir = workingDir
		cmd.Env = append([]string(nil), os.Environ()...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			if ctx.Err() != nil {
				kind := execution.KindCancelled
				if ctx.Err() == context.DeadlineExceeded {
					kind = execution.KindTimedOut
				}
				return execResult{Sandbox: sandboxState}, &execution.Error{Kind: kind, ExitCode: -1, Op: "validation command", Err: ctx.Err()}
			}
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
			if firstFailure == 0 {
				firstFailure = exitCode
			}
			report.Status = "failed"
		}
		report.Results = append(report.Results, validationCommandResult{Command: commandText, ExitCode: exitCode, Stdout: strings.TrimSpace(stdout.String()), Stderr: strings.TrimSpace(stderr.String())})
		combinedOut.WriteString(stdout.String())
		combinedErr.WriteString(stderr.String())
	}
	rawReport, err := json.Marshal(report)
	if err != nil {
		return execResult{}, err
	}
	if strings.TrimSpace(node.OutputPath) != "" {
		rendered, renderErr := renderTemplate(node.OutputPath, state, local, feedback, artifactsDir)
		if renderErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render validation artifact path", Err: renderErr}
		}
		resolved, resolveErr := r.resolveArtifactSourcePath(rendered, artifactsDir)
		if resolveErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve validation artifact path", Err: resolveErr}
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(resolved), 0o755); mkdirErr != nil {
			return execResult{}, mkdirErr
		}
		persistedReport, _ := r.redactor.Bytes(rawReport)
		if writeErr := os.WriteFile(resolved, append(persistedReport, '\n'), 0o644); writeErr != nil {
			return execResult{}, writeErr
		}
	}
	result := execResult{Output: string(rawReport), Stdout: combinedOut.String(), Stderr: combinedErr.String(), ExitCode: firstFailure, Sandbox: sandboxState}
	if firstFailure != 0 {
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: firstFailure, Op: "validation commands", Err: fmt.Errorf("one or more validation commands failed")}
	}
	return result, nil
}

// resolveExecutionPath maps a definition-relative control-checkout path into
// the current execution worktree when possible. External absolute paths remain
// external and are used as-is.
func (r *Runner) resolveExecutionPath(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("path is empty")
	}
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(filepath.Dir(r.WorkflowPath), candidate)
	}
	controlPath, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	controlPath = filepath.Clean(controlPath)
	if evaluated, evalErr := filepath.EvalSymlinks(controlPath); evalErr == nil {
		controlPath = evaluated
	}
	controlRoot, err := filepath.Abs(r.ControlWorkspace)
	if err != nil {
		return "", err
	}
	controlRoot = filepath.Clean(controlRoot)
	if evaluated, evalErr := filepath.EvalSymlinks(controlRoot); evalErr == nil {
		controlRoot = evaluated
	}
	rel, relErr := filepath.Rel(controlRoot, controlPath)
	if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(r.Workspace, rel), nil
	}
	return controlPath, nil
}
