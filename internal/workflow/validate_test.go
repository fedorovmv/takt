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

func TestValidateGovernedFanOutRequiresUpstreamArraySource(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "fanout"}, Nodes: []spec.Node{
		{ID: "discover", Bash: "printf '[]'"},
		{ID: "run", DependsOn: []string{"discover"}, WorkflowRun: &spec.WorkflowRunSpec{Path: "/tmp/child.yaml", FanOut: &spec.WorkflowFanOutSpec{ItemsFrom: "nodes.discover.output.items", MaxParallel: 4, Join: "all_done"}}},
	}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
	wf.Nodes[1].DependsOn = nil
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "must be an upstream dependency") {
		t.Fatalf("expected upstream dependency error, got %v", err)
	}
	wf.Nodes[1].DependsOn = []string{"discover"}
	wf.Nodes[1].WorkflowRun.FanOut.ItemsFrom = "discover.items"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "items_from") {
		t.Fatalf("expected items_from path error, got %v", err)
	}
}

func TestValidateScriptAndTypedArtifactContracts(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "script"}, Nodes: []spec.Node{{
		ID: "run", Script: &spec.ScriptSpec{Runtime: "python", Inline: "print('ok')"}, OutputType: "result", OutputMIME: "text/plain",
	}}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
	wf.Nodes[0].Script.Runtime = "ruby"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Fatalf("unsupported script runtime was accepted: %v", err)
	}
	wf.Nodes[0].Script.Runtime = "python"
	wf.Nodes[0].Script.Path = "script.py"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("path+inline script was accepted: %v", err)
	}
	wf.Nodes[0].Script.Path = ""
	wf.Nodes[0].OutputType = "bad/type"
	if err := Validate(wf); err == nil || !strings.Contains(err.Error(), "output_type") {
		t.Fatalf("invalid artifact type was accepted: %v", err)
	}
}

func TestValidateAllowsOutputFormatForScript(t *testing.T) {
	wf := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "script-json"}, Nodes: []spec.Node{{
		ID: "run", Script: &spec.ScriptSpec{Runtime: "python", Inline: "print('{}')"}, OutputFormat: &spec.OutputFormat{Type: "object"},
	}}}
	if err := Validate(wf); err != nil {
		t.Fatal(err)
	}
}

func TestCommandDirsForDefinitionIncludesProfileAndTaktRoots(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".takt", "profiles", "code", "workflows", "blocks", "review.yaml")
	dirs := CommandDirsForDefinition(path)
	wantProfile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "commands")
	wantTakt := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(path))))), "commands")
	contains := func(value string) bool {
		for _, dir := range dirs {
			if dir == value {
				return true
			}
		}
		return false
	}
	if !contains(wantProfile) || !contains(wantTakt) {
		t.Fatalf("command dirs %v do not include profile=%s and takt=%s", dirs, wantProfile, wantTakt)
	}
}

func TestValidateExternalSideEffectContract(t *testing.T) {
	valid := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "side-effect"}, Nodes: []spec.Node{{ID: "publish", Prompt: "publish", Executor: "external", SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	if err := Validate(valid); err != nil {
		t.Fatalf("valid reconcile side effect rejected: %v", err)
	}
	invalidMode := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "side-effect"}, Nodes: []spec.Node{{ID: "publish", Prompt: "publish", Executor: "external", SideEffect: &spec.SideEffectSpec{Mode: "maybe"}}}}
	if err := Validate(invalidMode); err == nil || !strings.Contains(err.Error(), "side_effect.mode") {
		t.Fatalf("invalid mode error = %v", err)
	}
	local := &spec.Workflow{APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "side-effect"}, Nodes: []spec.Node{{ID: "publish", Bash: "true", SideEffect: &spec.SideEffectSpec{Mode: "reconcile"}}}}
	if err := Validate(local); err == nil || !strings.Contains(err.Error(), "executor: external") {
		t.Fatalf("local side effect error = %v", err)
	}
}
