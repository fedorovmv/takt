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

func TestResolveNamedWorkflow(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve("code:assist", root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkflowName != "assist" {
		t.Fatalf("unexpected workflow name %q", resolved.WorkflowName)
	}
	if got := filepath.ToSlash(resolved.WorkflowPath); !strings.HasSuffix(got, "/workflows/assist.yaml") {
		t.Fatalf("unexpected workflow path %q", resolved.WorkflowPath)
	}
	if _, err := Resolve("code:missing", root); err == nil || !strings.Contains(err.Error(), "available:") {
		t.Fatalf("expected unknown named workflow error, got %v", err)
	}
}

func TestUnknownBuiltin(t *testing.T) {
	if _, err := Init("missing", t.TempDir(), false); err == nil {
		t.Fatal("expected unknown profile error")
	}
}

func TestCodeProfileShipsNineteenRoutedWorkflows(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("code", root, false); err != nil {
		t.Fatal(err)
	}
	resolved, err := Resolve("code", root)
	if err != nil {
		t.Fatal(err)
	}
	expected := []string{
		"assist", "fix-github-issue", "create-issue", "issue-review-full", "piv-loop",
		"idea-to-pr", "plan-to-pr", "feature-development", "adversarial-dev", "smart-pr-review",
		"comprehensive-pr-review", "validate-pr", "architect", "refactor-safely", "interactive-prd",
		"ralph-dag", "workflow-builder", "remotion-generate", "resolve-conflicts",
	}
	if len(resolved.Manifest.Workflows) != len(expected) {
		t.Fatalf("expected %d workflows, got %d", len(expected), len(resolved.Manifest.Workflows))
	}
	for _, name := range expected {
		if _, ok := resolved.Manifest.Workflows[name]; !ok {
			t.Fatalf("missing workflow %q", name)
		}
	}
}
