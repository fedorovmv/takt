package runtime

import (
	"bytes"
	"context"
	"os/exec"
	"strings"

	"takt/internal/execution"
)

func runBash(ctx context.Context, workspace, script string) (execResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	execution.ConfigureCommand(cmd)
	cmd.Dir = workspace
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	stdoutText, stderrText := stdout.String(), stderr.String()
	result := execResult{
		Output:   combineBashOutput(stdoutText, stderrText),
		Stdout:   stdoutText,
		Stderr:   stderrText,
		ExitCode: 0,
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
		return result, &execution.Error{Kind: kind, ExitCode: -1, Op: "bash", Err: ctx.Err()}
	}
	if ee, ok := err.(*exec.ExitError); ok {
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
