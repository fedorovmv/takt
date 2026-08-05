package gitworktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCreatesIsolatedBranchAndMappedWorkspace(t *testing.T) {
	repo := initRepo(t)
	subdir := filepath.Join(repo, "service")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "value.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", "add service")

	info, err := Prepare(context.Background(), subdir, "run-123", "Feature Development", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info.Branch, "feature-development/run-123") {
		t.Fatalf("unexpected branch %q", info.Branch)
	}
	if got := filepath.Clean(info.ExecutionWorkspace); got != filepath.Join(info.Path, "service") {
		t.Fatalf("mapped workspace = %q", got)
	}
	if err := os.WriteFile(filepath.Join(info.ExecutionWorkspace, "value.txt"), []byte("isolated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(subdir, "value.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(base) != "base\n" {
		t.Fatalf("base workspace changed: %q", base)
	}
	status, err := Inspect(context.Background(), info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty {
		t.Fatal("expected dirty isolated worktree")
	}
	if err := Remove(context.Background(), info.RepositoryRoot, info.Path, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func TestPrepareRejectsDirtyBaseByDefault(t *testing.T) {
	repo := initRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "uncommitted.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(context.Background(), repo, "run-dirty", "test", Options{})
	if err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("expected dirty workspace error, got %v", err)
	}
	info, err := Prepare(context.Background(), repo, "run-allowed", "test", Options{AllowDirty: true})
	if err != nil {
		t.Fatal(err)
	}
	defer Remove(context.Background(), info.RepositoryRoot, info.Path, true) //nolint:errcheck
	if !info.BaseDirty {
		t.Fatal("base dirty flag was not recorded")
	}
	if _, err := os.Stat(filepath.Join(info.Path, "uncommitted.txt")); !os.IsNotExist(err) {
		t.Fatalf("uncommitted file leaked into worktree: %v", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	git(t, repo, "init")
	git(t, repo, "config", "user.email", "takt@example.invalid")
	git(t, repo, "config", "user.name", "Takt Test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", "README.md")
	git(t, repo, "commit", "-m", "init")
	return repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
