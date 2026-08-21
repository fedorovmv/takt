package profile

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/workflow"
)

func TestCodeProfileArtifactGateMatrix(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, ".takt", "profiles", "code", "tools", "require-artifacts")
	for _, name := range []string{"implementation.md", "review-fixes.md", "revalidation.md", "pr.md", "pr-url.txt", "summary.md"} {
		for _, kind := range []string{"missing", "empty", "directory"} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), name)
				switch kind {
				case "empty":
					if err := os.WriteFile(path, nil, 0o644); err != nil {
						t.Fatal(err)
					}
				case "directory":
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				_, _, err := runCodeProfileTool(t, tool, path)
				if err == nil {
					t.Fatalf("%s artifact unexpectedly accepted", kind)
				}
			})
		}
		t.Run(name+"/valid", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte("evidence\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, stderr, err := runCodeProfileTool(t, tool, path); err != nil {
				t.Fatalf("valid artifact rejected: %v stderr=%s", err, stderr)
			}
		})
	}
}

func TestCodeProfileVerdictGateMatrix(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, ".takt", "profiles", "code", "tools", "require-verdict")
	cases := []struct {
		name, body, want string
		valid            bool
	}{
		{name: "pass", body: "verdict: PASS\n", want: "PASS", valid: true},
		{name: "repair", body: "# findings\nverdict: REPAIR\n", want: "REPAIR", valid: true},
		{name: "blocked", body: "verdict: BLOCKED\n", want: "BLOCKED", valid: true},
		{name: "extra-evidence", body: "# findings\nnote\nverdict: PASS\n", want: "PASS", valid: true},
		{name: "title-case", body: "Verdict: Pass\n", want: "PASS", valid: true},
		{name: "markdown-heading", body: "## verdict: PASS\n", want: "PASS", valid: true},
		{name: "markdown-heading-case", body: "###### VERDICT: blocked\n", want: "BLOCKED", valid: true},
		{name: "missing", valid: false},
		{name: "empty", body: "", valid: false},
		{name: "directory", valid: false},
		{name: "unknown", body: "verdict: MAYBE\n", valid: false},
		{name: "malformed", body: "verdict: PASS extra\n", valid: false},
		{name: "duplicate", body: "verdict: PASS\nverdict: PASS\n", valid: false},
		{name: "formatted-duplicate", body: "verdict: PASS\n## Verdict: REPAIR\n", valid: false},
		{name: "too-deep-heading", body: "####### verdict: PASS\n", valid: false},
		{name: "formatted-tail", body: "## Verdict: PASS extra\n", valid: false},
		{name: "non-markdown-heading-whitespace", body: "##\fverdict: PASS\n", valid: false},
		{name: "nul", body: "verdict: PASS\x00\n", valid: false},
		{name: "typo", body: "verdict PASS\n", valid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "validation.md")
			switch tc.name {
			case "missing":
			case "directory":
				if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stdout, stderr, err := runCodeProfileTool(t, tool, path, "validation")
			if tc.valid {
				if err != nil || strings.TrimSpace(stdout) != tc.want {
					t.Fatalf("valid verdict rejected: err=%v stdout=%q stderr=%q", err, stdout, stderr)
				}
				return
			}
			if err == nil {
				t.Fatalf("invalid verdict accepted: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func TestCodeFeatureWorkflowUsesGateTools(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve("code:feature-development", root)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		t.Fatal(err)
	}
	byID := func(id string) string {
		for _, node := range wf.Nodes {
			if node.ID == id {
				return node.Bash
			}
		}
		return ""
	}
	for _, id := range []string{"initial-verdict", "revalidation-verdict", "review-acceptance-gate", "pr-effect-gate"} {
		if body := byID(id); !strings.Contains(body, "require-artifacts") && !strings.Contains(body, "require-verdict") {
			t.Fatalf("node %q does not call a shared gate tool: %s", id, body)
		}
	}
	for _, node := range wf.Nodes {
		if node.ID != "implement" && node.ID != "summary" {
			continue
		}
		for _, hook := range node.Hooks.AfterNode {
			if node.ID == "implement" && hook.ID == "validation" {
				if !strings.Contains(hook.Bash, "require-artifacts") || !strings.Contains(hook.Bash, "require-change") {
					t.Fatalf("implementation hook does not enforce artifacts and execution change: %s", hook.Bash)
				}
			}
			if node.ID == "summary" && hook.ID == "summary-artifact" && !strings.Contains(hook.Bash, "require-artifacts") {
				t.Fatalf("summary hook does not call require-artifacts: %s", hook.Bash)
			}
		}
	}
}

func TestCodeProfileChangeGateMatrix(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(root, ".takt", "profiles", "code", "tools", "require-change")
	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, repo string)
		want  bool
	}{
		{name: "clean", setup: func(t *testing.T, repo string) {}, want: false},
		{name: "missing-baseline", setup: func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("pre-existing\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: false},
		{name: "tracked-change", setup: func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("changed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: true},
		{name: "untracked-change", setup: func(t *testing.T, repo string) {
			if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: true},
		{name: "runtime-artifact-only", setup: func(t *testing.T, repo string) {
			if err := os.MkdirAll(filepath.Join(repo, ".takt", "pi-sessions"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, ".takt", "pi-sessions", "session.jsonl"), []byte("runtime\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, want: false},
		{name: "committed-change-from-baseline", setup: func(t *testing.T, repo string) {
			cmd := exec.Command("git", "rev-parse", "HEAD")
			cmd.Dir = repo
			if _, err := cmd.Output(); err != nil {
				t.Fatalf("git rev-parse: %v", err)
			}
			if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("committed\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			cmd = exec.Command("git", "add", "tracked.txt")
			cmd.Dir = repo
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git add: %v\n%s", err, output)
			}
			cmd = exec.Command("git", "commit", "-qm", "implementation")
			cmd.Dir = repo
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git commit: %v\n%s", err, output)
			}
		}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := initGitRepo(t)
			baseOutput, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(t, repo)
			cmd := exec.Command(tool)
			cmd.Dir = repo
			cmd.Env = append(os.Environ(), "TAKT_WORKSPACE="+repo)
			if tc.name != "missing-baseline" {
				cmd.Env = append(cmd.Env, "TAKT_BASE_COMMIT="+strings.TrimSpace(string(baseOutput)))
			}
			err = cmd.Run()
			if (err == nil) != tc.want {
				t.Fatalf("require-change error=%v, want changed=%t", err, tc.want)
			}
		})
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"config", "user.email", "test@example.invalid"}, {"config", "user.name", "test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".takt/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", ".gitignore", "tracked.txt")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, output)
	}
	cmd = exec.Command("git", "commit", "-qm", "base")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v\n%s", err, output)
	}
	return repo
}

func runCodeProfileTool(t *testing.T, tool string, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(tool, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
