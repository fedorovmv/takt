package profile

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type scopeReport struct {
	Status         string   `json:"status"`
	BaseCommit     string   `json:"base_commit"`
	ChangedFiles   []string `json:"changed_files"`
	OutsideAllowed []string `json:"outside_allowed"`
}

func TestScopeCheckUsesGitPathspecs(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "scope-check")
	build := exec.Command("go", "build", "-o", tool, "./builtin/code/tools/scope-check.go")
	build.Dir = "."
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scope-check: %v\n%s", err, output)
	}

	t.Run("allows tracked file", func(t *testing.T) {
		repo := newScopeRepo(t, map[string]string{"app.txt": "initial\n"})
		writeScopeFile(t, repo, "app.txt", "changed\n")
		report, exit, stderr := runScopeCheck(t, tool, repo, []string{"app.txt"})
		if exit != 0 || report.Status != "ready" || len(report.OutsideAllowed) != 0 {
			t.Fatalf("exit=%d report=%+v stderr=%s", exit, report, stderr)
		}
		requireScopePaths(t, report.ChangedFiles, "app.txt")
	})

	t.Run("reports untracked filename with space", func(t *testing.T) {
		repo := newScopeRepo(t, map[string]string{"app.txt": "initial\n"})
		writeScopeFile(t, repo, "app.txt", "changed\n")
		writeScopeFile(t, repo, "extra file.txt", "outside\n")
		report, exit, _ := runScopeCheck(t, tool, repo, []string{"app.txt"})
		if exit != 3 || report.Status != "failed" {
			t.Fatalf("exit=%d report=%+v", exit, report)
		}
		requireScopePaths(t, report.ChangedFiles, "app.txt", "extra file.txt")
		requireScopePaths(t, report.OutsideAllowed, "extra file.txt")
	})

	t.Run("allows nested Git wildcard", func(t *testing.T) {
		repo := newScopeRepo(t, map[string]string{"docs/nested/readme.md": "initial\n"})
		writeScopeFile(t, repo, "docs/nested/readme.md", "changed\n")
		report, exit, _ := runScopeCheck(t, tool, repo, []string{"docs/**"})
		if exit != 0 || report.Status != "ready" {
			t.Fatalf("exit=%d report=%+v", exit, report)
		}
		requireScopePaths(t, report.ChangedFiles, "docs/nested/readme.md")
	})

	t.Run("checks both sides of rename", func(t *testing.T) {
		repo := newScopeRepo(t, map[string]string{"old.txt": "initial\n"})
		if err := os.Rename(filepath.Join(repo, "old.txt"), filepath.Join(repo, "new.txt")); err != nil {
			t.Fatal(err)
		}
		report, exit, _ := runScopeCheck(t, tool, repo, []string{"new.txt"})
		if exit != 3 {
			t.Fatalf("exit=%d report=%+v", exit, report)
		}
		requireScopePaths(t, report.ChangedFiles, "new.txt", "old.txt")
		requireScopePaths(t, report.OutsideAllowed, "old.txt")
	})
}

func TestScopeCheckRejectsUnsafePathspecs(t *testing.T) {
	tool := filepath.Join(t.TempDir(), "scope-check")
	build := exec.Command("go", "build", "-o", tool, "./builtin/code/tools/scope-check.go")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build scope-check: %v\n%s", err, output)
	}
	repo := newScopeRepo(t, map[string]string{"app.txt": "initial\n"})
	for _, pathspec := range []string{"../escape", ":(exclude)app.txt"} {
		t.Run(pathspec, func(t *testing.T) {
			_, exit, stderr := runScopeCheck(t, tool, repo, []string{pathspec})
			if exit != 2 || !strings.Contains(stderr, "invalid allowed path") {
				t.Fatalf("exit=%d stderr=%q", exit, stderr)
			}
		})
	}
}

func newScopeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	runScopeGit(t, repo, "init", "-q", "-b", "main")
	runScopeGit(t, repo, "config", "user.name", "Takt Fixture")
	runScopeGit(t, repo, "config", "user.email", "takt@example.test")
	for name, content := range files {
		writeScopeFile(t, repo, name, content)
	}
	runScopeGit(t, repo, "add", ".")
	runScopeGit(t, repo, "commit", "-q", "-m", "base")
	return repo
}

func writeScopeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runScopeCheck(t *testing.T, tool, repo string, allowed []string) (scopeReport, int, string) {
	t.Helper()
	input, err := json.Marshal(map[string]any{"base_branch": "main", "allowed_paths": allowed})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(tool)
	cmd.Dir = repo
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	exit := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		exit = exitErr.ExitCode()
	}
	var report scopeReport
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("decode report %q: %v", stdout.String(), err)
		}
	}
	return report, exit, stderr.String()
}

func runScopeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func requireScopePaths(t *testing.T, actual []string, expected ...string) {
	t.Helper()
	sort.Strings(actual)
	sort.Strings(expected)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("paths=%q want=%q", actual, expected)
	}
}
