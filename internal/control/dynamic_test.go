package control

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/profile"
)

func candidateDynamicPlan() dynamicplan.Plan {
	plan := dynamicplan.Plan{
		Decision: "planned",
		Goal:     "audit handlers",
		Reason:   "requires inventory and independent review",
		Budget:   dynamicplan.Budget{MaxChildRuns: 12, MaxParallel: 4, MaxIterations: 3, MaxTokens: 10000},
		Phases: []dynamicplan.Phase{
			{ID: "inventory", Uses: "discover", Objective: "find handlers", Strategy: "task", Checkpoint: true},
			{ID: "summary", Uses: "synthesize", Objective: "summarize findings", Strategy: "task", DependsOn: []string{"inventory"}},
		},
	}
	dynamicplan.Normalize(&plan)
	return plan
}

func TestPlanCandidateProducesPreviewAndRequiresConfirmation(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateDynamicPlan()
	result, err := service.Plan(context.Background(), PlanRequest{Goal: candidate.Goal, Profile: "code", Candidate: &candidate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != "planned" || !result.RequiresConfirmation || result.PlanID == "" || result.Preview == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if _, err := service.ExecutePlan(context.Background(), ExecutePlanRequest{PlanID: result.PlanID}); err == nil {
		t.Fatal("expected confirmation error")
	}
	view, err := service.GetPlan(result.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Phases) != 2 || view.Phases[0].Status != "pending" {
		t.Fatalf("unexpected plan view: %#v", view)
	}
}

func TestPromoteCompletedPlanCreatesProjectWorkflow(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	now := time.Now().UTC()
	record := &dynamicplan.Record{ID: "plan-0123456789ab", Status: "completed", Profile: "code", ConfigPath: service.ConfigPath, CreatedAt: now, UpdatedAt: now, Results: map[string]string{}, CompletedPhases: []string{"inventory", "summary"}, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	promoted, err := service.PromotePlan(record.ID, "Audit Handler Auth")
	if err != nil {
		t.Fatal(err)
	}
	if promoted.PromotedPath == "" {
		t.Fatal("promoted path is empty")
	}
	if _, err := os.Stat(promoted.PromotedPath); err != nil {
		t.Fatal(err)
	}
}
