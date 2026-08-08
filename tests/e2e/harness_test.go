package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	repoRoot string
	buildDir string
	buildMu  sync.Mutex
	binaries = map[string]string{}
)

func TestMain(m *testing.M) {
	_, file, _, _ := runtime.Caller(0)
	repoRoot = filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	var err error
	buildDir, err = os.MkdirTemp("", "takt-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	code := m.Run()
	_ = os.RemoveAll(buildDir)
	os.Exit(code)
}

type Result struct {
	Stdout string
	Stderr string
	Err    error
}

func (r Result) RequireSuccess(t *testing.T) Result {
	t.Helper()
	if r.Err != nil {
		t.Fatalf("command failed: %v\nstdout:\n%s\nstderr:\n%s", r.Err, r.Stdout, r.Stderr)
	}
	return r
}

func (r Result) RequireFailure(t *testing.T) Result {
	t.Helper()
	if r.Err == nil {
		t.Fatalf("command unexpectedly succeeded\nstdout:\n%s\nstderr:\n%s", r.Stdout, r.Stderr)
	}
	return r
}

func (r Result) Contains(t *testing.T, needle string) Result {
	t.Helper()
	if !strings.Contains(r.Stdout+r.Stderr, needle) {
		t.Fatalf("output does not contain %q\nstdout:\n%s\nstderr:\n%s", needle, r.Stdout, r.Stderr)
	}
	return r
}

func (r Result) JSON(t *testing.T) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(r.Stdout), &value); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, r.Stdout)
	}
	return value
}

func resultObject(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	got, ok := value["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result object: %#v", value)
	}
	return got
}

func stringField(t *testing.T, value map[string]any, key string) string {
	t.Helper()
	got, ok := value[key].(string)
	if !ok {
		t.Fatalf("%s is not a string: %#v", key, value[key])
	}
	return got
}

func boolField(t *testing.T, value map[string]any, key string) bool {
	t.Helper()
	got, ok := value[key].(bool)
	if !ok {
		t.Fatalf("%s is not a bool: %#v", key, value[key])
	}
	return got
}

func binary(t *testing.T, name string) string {
	t.Helper()
	buildMu.Lock()
	defer buildMu.Unlock()
	if path := binaries[name]; path != "" {
		return path
	}
	path := filepath.Join(buildDir, name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", path, "./cmd/"+name)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	binaries[name] = path
	return path
}

func takt(t *testing.T, env []string, args ...string) Result {
	t.Helper()
	return run(t, repoRoot, env, nil, binary(t, "takt"), args...)
}

func taktInput(t *testing.T, env []string, input string, args ...string) Result {
	t.Helper()
	return run(t, repoRoot, env, strings.NewReader(input), binary(t, "takt"), args...)
}

func run(t *testing.T, dir string, env []string, stdin io.Reader, name string, args ...string) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("command timed out after 90s: %s %s: %w", name, strings.Join(args, " "), ctx.Err())
	}
	return Result{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
}

func writeFile(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func copyFile(t *testing.T, src, dst string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func requireFileContains(t *testing.T, path string, needles ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, needle := range needles {
		if !strings.Contains(text, needle) {
			t.Fatalf("%s does not contain %q", path, needle)
		}
	}
}

func requireEventually(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !fn() {
		t.Fatalf("condition was not satisfied within %s", timeout)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	result := run(t, dir, nil, nil, "git", args...).RequireSuccess(t)
	return strings.TrimSpace(result.Stdout)
}
