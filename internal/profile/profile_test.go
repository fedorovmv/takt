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

func TestCodeReviewWorkflowsPreserveJSONInput(t *testing.T) {
	workspace := t.TempDir()
	if _, err := Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"comprehensive-pr-review", "architect"} {
		resolved, err := Resolve("code:"+name, workspace)
		if err != nil {
			t.Fatal(err)
		}
		if got := resolved.EffectiveInput(); got.Format != "json" || !got.PreservePath {
			t.Fatalf("%s input=%+v", name, got)
		}
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

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(" code ") || IsBuiltin("missing") || IsBuiltin("") {
		t.Fatal("unexpected built-in profile result")
	}
}

func TestSelectorParts(t *testing.T) {
	name, workflow := SelectorParts(" code : feature-development ")
	if name != "code" || workflow != "feature-development" {
		t.Fatalf("selector parts = %q, %q", name, workflow)
	}
	name, workflow = SelectorParts(" workflows/local.yaml ")
	if name != "workflows/local.yaml" || workflow != "" {
		t.Fatalf("path selector parts = %q, %q", name, workflow)
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

func TestEvaluationProfileInstallsReadOnlyAnalysisWorkflow(t *testing.T) {
	root := t.TempDir()
	if _, err := Init("evaluation", root, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		".takt/profiles/evaluation/profile.yaml",
		".takt/profiles/evaluation/workflows/analyze.yaml",
		".takt/profiles/evaluation/commands/analyze.md",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("missing installed evaluation asset %s: %v", rel, err)
		}
	}
	resolved, err := Resolve("evaluation:analyze", root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.WorkflowName != "analyze" || !strings.HasSuffix(filepath.ToSlash(resolved.WorkflowPath), "/workflows/analyze.yaml") {
		t.Fatalf("unexpected analysis workflow: %+v", resolved)
	}
	if got := resolved.EffectiveInput(); got.Format != "json" || got.PreservePath {
		t.Fatalf("unexpected analysis input: %+v", got)
	}
}
