package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"takt/internal/execution"
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
	args := make([]string, len(definition.Args))
	for index, value := range definition.Args {
		rendered, err := renderTemplate(value, state, local, feedback, artifactsDir)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render script argument", Err: err}
		}
		args[index] = rendered
	}
	var cmd *exec.Cmd
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
		cmd = exec.CommandContext(ctx, path, args...)
	case "python":
		if definition.Inline != "" {
			inline, renderErr := renderTemplate(definition.Inline, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render python inline script", Err: renderErr}
			}
			cmd = exec.CommandContext(ctx, "python3", append([]string{"-c", inline}, args...)...)
		} else {
			renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render python script path", Err: renderErr}
			}
			path, err := r.resolveExecutionPath(renderedPath)
			if err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve python script", Err: err}
			}
			cmd = exec.CommandContext(ctx, "python3", append([]string{path}, args...)...)
		}
	case "node":
		if definition.Inline != "" {
			inline, renderErr := renderTemplate(definition.Inline, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render node inline script", Err: renderErr}
			}
			cmd = exec.CommandContext(ctx, "node", append([]string{"-e", inline}, args...)...)
		} else {
			renderedPath, renderErr := renderTemplate(definition.Path, state, local, feedback, artifactsDir)
			if renderErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render node script path", Err: renderErr}
			}
			path, err := r.resolveExecutionPath(renderedPath)
			if err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve node script", Err: err}
			}
			cmd = exec.CommandContext(ctx, "node", append([]string{path}, args...)...)
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
		cmd = exec.CommandContext(ctx, "go", append([]string{"run", path}, args...)...)
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
	for key, value := range definition.Env {
		rendered, err := renderTemplate(value, state, local, feedback, artifactsDir)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render script environment", Err: err}
		}
		cmd.Env = append(cmd.Env, key+"="+rendered)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	result := execResult{Output: strings.TrimSpace(stdout.String()), Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: 0}
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
