package runtime

import (
	"context"
	"strings"
	"testing"

	"takt/internal/execution"
)

func TestRunBashSeparatesStdoutAndStderr(t *testing.T) {
	result, err := runBash(context.Background(), t.TempDir(), `exec /bin/sh -c 'printf "%s\n" stdout-value; printf "%s\n" stderr-value >&2; exit 1'`)
	if execution.KindOf(err) != execution.KindExit {
		t.Fatalf("expected exit error, got result=%+v err=%v", result, err)
	}
	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %+v", result)
	}
	if result.Stdout != "stdout-value\n" {
		t.Fatalf("stdout was not preserved separately: %q", result.Stdout)
	}
	if result.Stderr != "stderr-value\n" {
		t.Fatalf("stderr was not preserved separately: %q", result.Stderr)
	}
	if result.Output != "stdout-value\nstderr-value" {
		t.Fatalf("combined diagnostic output changed: %q", result.Output)
	}
	if !strings.Contains(result.Output, strings.TrimSpace(result.Stdout)) || !strings.Contains(result.Output, strings.TrimSpace(result.Stderr)) {
		t.Fatalf("combined output does not contain both streams: %+v", result)
	}
}
