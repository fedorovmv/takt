package localsandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRequiredFailsWhenBackendUnavailable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, decision, err := CommandContext(context.Background(), t.TempDir(), Policy{Enforcement: "required", Filesystem: "read_only"}, "true")
	if err == nil || decision.Status != "degraded" {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
}

func TestOptionalDegradesWhenBackendUnavailable(t *testing.T) {
	dir := t.TempDir()
	// Provide the actual command by absolute path so only the sandbox backend is absent.
	truePath := "/bin/true"
	if runtime.GOOS == "windows" {
		t.Skip("unix contract")
	}
	if _, err := os.Stat(truePath); err != nil {
		t.Skip("/bin/true unavailable")
	}
	t.Setenv("PATH", dir)
	cmd, decision, err := CommandContext(context.Background(), dir, Policy{Enforcement: "optional", Network: "deny"}, truePath)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != "degraded" {
		t.Fatalf("decision=%+v", decision)
	}
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestFakeBubblewrapBuildsStrictArguments(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux backend")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "bwrap")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	cmd, decision, err := CommandContext(context.Background(), "/workspace", Policy{Enforcement: "required", Filesystem: "read_only", Network: "deny"}, "/bin/sh", "-c", "true")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if decision.Backend != "bubblewrap" || !strings.Contains(joined, "--ro-bind / /") || !strings.Contains(joined, "--unshare-net") || !strings.Contains(joined, "--chdir /workspace") {
		t.Fatalf("decision=%+v args=%q", decision, joined)
	}
}
