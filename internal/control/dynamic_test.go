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

func TestSteerRejectsPlanAtRevisionLimit(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	plan.Budget.MaxIterations = 1
	now := time.Now().UTC()
	record := &dynamicplan.Record{ID: "plan-limit0123456789", Status: "waiting", Profile: "code", ConfigPath: service.ConfigPath, CreatedAt: now, UpdatedAt: now, Results: map[string]string{}, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "initial", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Steer(context.Background(), SteerRequest{PlanID: record.ID, Message: "continue"}); err == nil {
		t.Fatal("expected revision limit error")
	}
	loaded, err := (dynamicplan.Store{Workspace: workspace}).Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Steering) != 0 {
		t.Fatalf("rejected steering was persisted: %#v", loaded.Steering)
	}
}

func TestPromoteRefusesSilentOverwrite(t *testing.T) {
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
	resolved, err := profile.Resolve("code", workspace)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := catalogForResolved(resolved)
	if err != nil {
		t.Fatal(err)
	}
	record := &dynamicplan.Record{ID: "plan-overwrite12345", Status: "completed", Profile: "code", ConfigPath: service.ConfigPath, BlockPackagePaths: resolved.BlockPackagePaths, BlockCatalogFingerprint: catalog.Fingerprint, CreatedAt: now, UpdatedAt: now, Results: map[string]string{}, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromotePlan(record.ID, "same-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PromotePlan(record.ID, "same-name"); err == nil {
		t.Fatal("expected overwrite refusal")
	}
	if _, err := service.PromotePlanWithOptions(record.ID, "same-name", PromotePlanOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanRejectsMapSourceOutsideTrustedBlockOutputs(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	plan.Phases = []dynamicplan.Phase{
		{ID: "inventory", Uses: "discover", Objective: "find", Strategy: "task"},
		{ID: "inspect", Uses: "investigate", Objective: "inspect", Strategy: "map", Source: "phases.inventory.output.not_declared", DependsOn: []string{"inventory"}, MaxParallel: 2},
	}
	if _, err := service.Plan(context.Background(), PlanRequest{Goal: plan.Goal, Profile: "code", Candidate: &plan}); err == nil {
		t.Fatal("expected trusted output path error")
	}
}

func TestExecuteRejectsChangedTrustedBlockPackage(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	result, err := service.Plan(context.Background(), PlanRequest{Goal: plan.Goal, Profile: "code", Candidate: &plan})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Record.BlockPackagePaths) == 0 {
		t.Fatal("plan did not persist trusted block packages")
	}
	path := result.Record.BlockPackagePaths[0]
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\n# changed after planning\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ExecutePlan(context.Background(), ExecutePlanRequest{PlanID: result.PlanID, Confirm: true}); err == nil {
		t.Fatal("expected trusted catalog fingerprint mismatch")
	}
}
