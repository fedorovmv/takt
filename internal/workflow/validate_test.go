package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"takt/internal/spec"
	"testing"
)

func TestValidateDetectsCycle(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "x"}, Nodes: []spec.Node{{ID: "a", Bash: "true", DependsOn: []string{"b"}}, {ID: "b", Bash: "true", DependsOn: []string{"a"}}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestRejectsInvalidTimeout(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "bad-timeout"}, Nodes: []spec.Node{{ID: "n", Bash: "true", Timeout: "never"}}}
	if err := Validate(wf); err == nil {
		t.Fatal("expected invalid timeout error")
	}
}

func TestValidateRejectsNestedLoopGroups(t *testing.T) {
	zero := 0
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "nested"}, Nodes: []spec.Node{{
		ID: "outer", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{
			ID: "inner", LoopGroup: &spec.LoopGroupSpec{MaxIterations: 1, Nodes: []spec.Node{{ID: "check", Bash: "true"}}, Until: spec.UntilSpec{Node: "check", ExitCode: &zero}},
		}}, Until: spec.UntilSpec{Node: "inner", ExitCode: &zero}},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "nested loop_group") {
		t.Fatalf("expected nested loop validation error, got %v", err)
	}
}

func TestValidateAcceptsGovernedWorkflowNode(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "parent"}, Nodes: []spec.Node{{
		ID: "child", WorkflowRun: &spec.WorkflowRunSpec{Path: "child.yaml", Input: "${input}", Isolation: "inherit"},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatalf("governed workflow node was rejected: %v", err)
	}
}

func TestValidateRejectsGovernedWorkflowIsolation(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "parent"}, Nodes: []spec.Node{{
		ID: "child", WorkflowRun: &spec.WorkflowRunSpec{Path: "child.yaml", Isolation: "shared"},
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("expected governed workflow isolation error, got %v", err)
	}
}

func TestLoadRejectsGovernedWorkflowRecursion(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.yaml")
	second := filepath.Join(dir, "second.yaml")
	if err := os.WriteFile(first, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: first
nodes:
  - id: child
    workflow:
      path: second.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: second
nodes:
  - id: child
    workflow:
      path: first.yaml
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(first)
	if err == nil || !strings.Contains(err.Error(), "recursive governed child workflow reference") {
		t.Fatalf("governed recursion was not rejected during validation: %v", err)
	}
}

func TestValidateRejectsAssistantPolicyOnBashNode(t *testing.T) {
	allowedTools := []string{"read"}
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "bad-policy"}, Nodes: []spec.Node{{
		ID: "shell", Bash: "true", AllowedTools: &allowedTools,
	}}}
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "command or prompt") {
		t.Fatalf("expected policy placement error, got %v", err)
	}
}

func TestLoadRejectsMissingMCPPolicyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: missing-mcp
nodes:
  - id: agent
    prompt: test
    mcp: missing.json
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "read MCP config") {
		t.Fatalf("missing MCP policy file was not rejected: %v", err)
	}
}

func TestLoadPreservesExplicitEmptyPolicyAllowlists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: empty-policy
nodes:
  - id: classify
    prompt: classify
    allowed_tools: []
    skills: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	node := wf.Nodes[0]
	if node.AllowedTools == nil || len(*node.AllowedTools) != 0 {
		t.Fatalf("explicit empty allowed_tools was lost: %#v", node.AllowedTools)
	}
	if node.Skills == nil || len(*node.Skills) != 0 {
		t.Fatalf("explicit empty skills was lost: %#v", node.Skills)
	}
}
