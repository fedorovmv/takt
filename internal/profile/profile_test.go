package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitResolveAndPrepareMarkdownInput(t *testing.T) {
	root := t.TempDir()
	installed, err := Init("code", root, false)
	if err != nil {
		t.Fatal(err)
	}
	if installed != filepath.Join(root, ".takt", "profiles", "code") {
		t.Fatalf("unexpected path %q", installed)
	}
	resolved, err := Resolve("code", root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Manifest.Input.Format != "markdown" || !resolved.Manifest.Input.PreservePath {
		t.Fatalf("unexpected input spec: %+v", resolved.Manifest.Input)
	}
	plan := filepath.Join(root, "PLAN.md")
	if err := os.WriteFile(plan, []byte("# Plan\n\n- [ ] task\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	input, err := PrepareInput(resolved.Manifest.Input, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(input, plan) || !strings.Contains(input, "- [ ] task") {
		t.Fatalf("prepared input missing path/content: %s", input)
	}
	if _, err := Init("code", root, false); err == nil {
		t.Fatal("expected duplicate init error")
	}
}

func TestUnknownBuiltin(t *testing.T) {
	if _, err := Init("missing", t.TempDir(), false); err == nil {
		t.Fatal("expected unknown profile error")
	}
}
