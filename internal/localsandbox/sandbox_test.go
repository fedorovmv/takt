package localsandbox

import (
	"context"
	"os"
	"os/exec"
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
	if runtime.GOOS == "windows" {
		t.Skip("unix contract")
	}
	commandPath := filepath.Join(dir, "fixture-command")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	cmd, decision, err := CommandContext(context.Background(), dir, Policy{Enforcement: "optional", Network: "deny"}, commandPath)
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

func TestSandboxExecBuildsStrictProfile(t *testing.T) {
	cmd, err := commandForBackend(context.Background(), "sandbox-exec", "/usr/bin/sandbox-exec", "/workspace", Policy{Enforcement: "required", Filesystem: "read_only", Network: "deny"}, "/bin/sh", "-c", "true")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "(deny network*)") || !strings.Contains(joined, "(deny file-write*)") || !strings.Contains(joined, "/bin/sh -c true") {
		t.Fatalf("sandbox-exec args=%q", joined)
	}
}

func TestSandboxExecEnforcesWhenAvailable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS backend")
	}
	if _, err := exec.LookPath("sandbox-exec"); err != nil {
		t.Skip("sandbox-exec unavailable")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "blocked")
	cmd, decision, err := CommandContext(context.Background(), dir, Policy{Enforcement: "required", Filesystem: "read_only", Network: "deny"}, "/bin/sh", "-c", "printf blocked > "+target)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != "enforced" || decision.Backend != "sandbox-exec" {
		t.Fatalf("decision=%+v", decision)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("sandbox-exec allowed a file write under read_only policy")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("blocked file exists: %v", err)
	}
}
