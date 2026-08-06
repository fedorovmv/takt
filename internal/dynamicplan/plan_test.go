package dynamicplan

import (
	"os"
	"path/filepath"
	"testing"
)

func validPlan() Plan {
	plan := Plan{
		Decision: "planned",
		Goal:     "audit handlers",
		Reason:   "requires a bounded dynamic audit",
		Budget:   Budget{MaxChildRuns: 12, MaxParallel: 4, MaxIterations: 3, MaxTokens: 10000},
		Phases: []Phase{
			{ID: "inventory", Uses: "discover", Objective: "find handlers", Strategy: "task"},
			{ID: "inspect", Uses: "investigate", Objective: "inspect each handler", Strategy: "map", Source: "phases.inventory.output.items", DependsOn: []string{"inventory"}, MaxParallel: 4, Checkpoint: true},
			{ID: "summary", Uses: "synthesize", Objective: "summarize findings", Strategy: "task", DependsOn: []string{"inspect"}},
		},
	}
	Normalize(&plan)
	return plan
}

func TestValidatePlanAndSegments(t *testing.T) {
	plan := validPlan()
	if err := Validate(plan); err != nil {
		t.Fatal(err)
	}
	segments := Segments(plan.Phases)
	if len(segments) != 2 || len(segments[0]) != 2 || len(segments[1]) != 1 {
		t.Fatalf("unexpected segments: %#v", segments)
	}
}

func TestValidateRejectsFutureDependency(t *testing.T) {
	plan := validPlan()
	plan.Phases[0].DependsOn = []string{"summary"}
	if err := Validate(plan); err == nil {
		t.Fatal("expected future dependency error")
	}
}

func TestCompileUsesNormalGovernedWorkflowNodes(t *testing.T) {
	plan := validPlan()
	dir := t.TempDir()
	blocks := filepath.Join(dir, "blocks")
	if err := os.MkdirAll(blocks, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range AllowedBlocks {
		if err := os.WriteFile(filepath.Join(blocks, name), []byte("apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: block\nnodes:\n  - id: result\n    bash: printf ok\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(dir, "generated.yaml")
	wf, err := Compile(plan.Phases[:2], plan.Budget, CompileOptions{WorkflowName: "dynamic-test", OutputPath: output, BlocksDir: blocks, Goal: plan.Goal})
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Nodes) != 2 || wf.Nodes[0].WorkflowRun == nil || wf.Nodes[1].WorkflowRun == nil || wf.Nodes[1].WorkflowRun.FanOut == nil {
		t.Fatalf("expected ordinary governed workflow nodes: %#v", wf.Nodes)
	}
	if wf.Nodes[1].WorkflowRun.FanOut.MaxItems != plan.Budget.MaxChildRuns-1 {
		t.Fatalf("max_items=%d", wf.Nodes[1].WorkflowRun.FanOut.MaxItems)
	}
	if err := WriteWorkflow(output, wf); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRoundTrip(t *testing.T) {
	store := Store{Workspace: t.TempDir()}
	record := &Record{ID: "plan-0123456789ab", Status: "draft", Profile: "code", Results: map[string]string{}, Revisions: []Revision{{Number: 1, Plan: validPlan()}}}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != record.ID || loaded.Revisions[0].Plan.Goal != "audit handlers" {
		t.Fatalf("unexpected record: %#v", loaded)
	}
	if loaded.Results == nil {
		t.Fatal("empty results map must be restored after omitempty serialization")
	}
}

func TestCompileLimitsParallelTaskPhases(t *testing.T) {
	plan := validPlan()
	plan.Budget.MaxParallel = 2
	plan.Phases = []Phase{
		{ID: "a", Uses: "discover", Objective: "a", Strategy: "task"},
		{ID: "b", Uses: "review", Objective: "b", Strategy: "task"},
		{ID: "c", Uses: "validate", Objective: "c", Strategy: "task"},
		{ID: "d", Uses: "synthesize", Objective: "d", Strategy: "task"},
	}
	wf, err := Compile(plan.Phases, plan.Budget, CompileOptions{WorkflowName: "parallel-limit", BlocksDir: t.TempDir(), Goal: plan.Goal})
	if err != nil {
		t.Fatal(err)
	}
	if !containsDependency(wf.Nodes[2].DependsOn, "a") || !containsDependency(wf.Nodes[3].DependsOn, "b") {
		t.Fatalf("parallel lanes were not added: %#v", wf.Nodes)
	}
}

func TestNormalizeTurnsZeroTokenBudgetIntoBoundedDefault(t *testing.T) {
	plan := validPlan()
	plan.Budget.MaxTokens = 0
	Normalize(&plan)
	if plan.Budget.MaxTokens != 500000 {
		t.Fatalf("max_tokens=%d", plan.Budget.MaxTokens)
	}
	if err := Validate(plan); err != nil {
		t.Fatal(err)
	}
}
