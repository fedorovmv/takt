package runtime

import (
	"bytes"
	"context"
	"strings"

	"takt/internal/execution"
	"takt/internal/localsandbox"
	"takt/internal/spec"
	"takt/internal/store"
)

func (r *Runner) runBash(ctx context.Context, node spec.Node, script string) (execResult, error) {
	policy := localsandbox.Policy{}
	if node.Sandbox != nil {
		policy = localsandbox.Policy{Enforcement: node.Sandbox.Enforcement, Filesystem: node.Sandbox.Filesystem, Network: node.Sandbox.Network}
	}
	cmd, decision, err := localsandbox.CommandContext(ctx, r.Workspace, policy, "bash", "-lc", script)
	sandboxState := &store.SandboxState{Requested: decision.Requested, Status: decision.Status, Backend: decision.Backend, Reason: decision.Reason}
	if err != nil {
		return execResult{Sandbox: sandboxState}, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "OS sandbox", Err: err}
	}
	execution.ConfigureCommand(cmd)
	cmd.Dir = r.Workspace
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	stdoutText, stderrText := stdout.String(), stderr.String()
	result := execResult{Output: combineBashOutput(stdoutText, stderrText), Stdout: stdoutText, Stderr: stderrText, ExitCode: 0, Sandbox: sandboxState}
	if err == nil {
		return result, nil
	}
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if ctx.Err() == context.DeadlineExceeded {
			kind = execution.KindTimedOut
		}
		result.ExitCode = -1
		return result, &execution.Error{Kind: kind, ExitCode: -1, Op: "bash", Err: ctx.Err()}
	}
	if ee, ok := err.(interface{ ExitCode() int }); ok {
		result.ExitCode = ee.ExitCode()
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "bash", Err: err}
	}
	result.ExitCode = -1
	return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "bash", Err: err}
}

func combineBashOutput(stdout, stderr string) string {
	out := strings.TrimSpace(stdout)
	if value := strings.TrimSpace(stderr); value != "" {
		if out != "" {
			out += "\n"
		}
		out += value
	}
	return out
}
