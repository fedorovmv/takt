package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func runBash(ctx context.Context, workspace, script string) (execResult, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", script)
	cmd.Dir = workspace
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
	}
	output := strings.TrimSpace(stdout.String())
	if strings.TrimSpace(stderr.String()) != "" {
		if output != "" {
			output += "\n"
		}
		output += strings.TrimSpace(stderr.String())
	}
	if err != nil {
		return execResult{Output: output, ExitCode: exitCode}, fmt.Errorf("bash exited with code %d: %s", exitCode, output)
	}
	return execResult{Output: output, ExitCode: exitCode}, nil
}
