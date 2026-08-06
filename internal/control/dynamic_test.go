package control

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/blockcatalog"
	"takt/internal/dynamicplan"
	"takt/internal/profile"
	"takt/internal/rolecontract"
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
	for _, status := range []string{"waiting", "running"} {
		t.Run(status, func(t *testing.T) {
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
			record := &dynamicplan.Record{ID: "plan-limit" + status + "12345", Status: status, Profile: "code", ConfigPath: service.ConfigPath, CreatedAt: now, UpdatedAt: now, Results: map[string]string{}, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "initial", CreatedAt: now, Plan: plan}}}
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
		})
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

func TestPlanFallsBackToStableTemplateWhenRouterFails(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(workspace, ".takt", "config.yaml")
	config := `apiVersion: takt/v1alpha1
kind: Config
default_assistant: mock
models:
  routing:
    provider: test
    id: routing
  implementation:
    provider: test
    id: implementation
  review:
    provider: test
    id: review
assistants:
  mock:
    type: mock
`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Plan(context.Background(), PlanRequest{Goal: "Исправить неясный дефект", Profile: "code"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Route == nil || result.Route.Route != "template" {
		t.Fatalf("route = %#v", result.Route)
	}
	foundFallback := false
	for _, signal := range result.Route.Signals {
		if signal == "router_fallback" {
			foundFallback = true
		}
	}
	if !foundFallback || !result.Route.Controls.InspectFirst {
		t.Fatalf("fallback controls = %#v", result.Route)
	}
	if result.Decision != "planned" || !result.RequiresConfirmation {
		t.Fatalf("plan result = %#v", result)
	}
}

func TestSegmentControlsDifferentiateDenyRepairAndWarn(t *testing.T) {
	implementRole := rolecontract.Definition{Paths: rolecontract.PathScope{Expected: []string{"src/**"}, Allowed: []string{"src/**", "docs/**"}, Protected: []string{"src/security/**"}, Forbidden: []string{".takt/**"}}}
	catalog := &blockcatalog.Catalog{Blocks: map[string]blockcatalog.ResolvedBlock{
		"implement": {Name: "implement", Role: "implementer", RoleDefinition: &implementRole},
		"validate":  {Name: "validate", Checks: []rolecontract.Check{{Name: "validate", Path: "passed", Level: rolecontract.CheckRequired, Reaction: rolecontract.ReactionRepair}}},
		"policy":    {Name: "policy", Checks: []rolecontract.Check{{Name: "policy", Path: "allowed", Level: rolecontract.CheckRequired, Reaction: rolecontract.ReactionDeny}}},
		"quality":   {Name: "quality", Checks: []rolecontract.Check{{Name: "quality", Path: "ideal", Level: rolecontract.CheckPreferred, Reaction: rolecontract.ReactionWarn}}},
	}}
	record := &dynamicplan.Record{Results: map[string]string{
		"implement": `{"changed_files":["src/security/auth.go","docs/note.md"],"summary":"done"}`,
		"validate":  `{"passed":false}`,
		"policy":    `{"allowed":false}`,
		"quality":   `{"ideal":false}`,
	}}
	outcome, err := evaluateSegmentControls(record, []dynamicplan.Phase{{ID: "implement", Uses: "implement"}, {ID: "validate", Uses: "validate"}, {ID: "quality", Uses: "quality"}}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.DenyReason != "" || len(outcome.RepairFailures) != 1 || outcome.RepairFailures[0].Check != "validate" {
		t.Fatalf("outcome=%#v", outcome)
	}
	if len(record.Warnings) < 2 {
		t.Fatalf("warnings=%#v", record.Warnings)
	}

	denied, err := evaluateSegmentControls(record, []dynamicplan.Phase{{ID: "policy", Uses: "policy"}}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if denied.DenyReason == "" {
		t.Fatalf("deny result=%#v", denied)
	}

	record.Results["implement"] = `{"changed_files":["../escape"],"summary":"bad"}`
	denied, err = evaluateSegmentControls(record, []dynamicplan.Phase{{ID: "implement", Uses: "implement"}}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	if denied.DenyReason == "" {
		t.Fatal("path escape was not denied")
	}
}

func TestSegmentControlsDenyUndeclaredWorkspaceChange(t *testing.T) {
	role := rolecontract.Definition{Paths: rolecontract.PathScope{Allowed: []string{"src/**"}, Forbidden: []string{".takt/**"}}}
	catalog := &blockcatalog.Catalog{Blocks: map[string]blockcatalog.ResolvedBlock{
		"implement": {Name: "implement", Role: "implementer", RoleDefinition: &role},
	}}
	record := &dynamicplan.Record{Results: map[string]string{
		"implement": `{"changed_files":["src/declared.go"]}`,
	}}
	outcome, err := evaluateSegmentControls(record, []dynamicplan.Phase{{ID: "implement", Uses: "implement"}}, catalog, []string{"src/declared.go", "src/hidden.go"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.DenyReason == "" {
		t.Fatal("actual change omitted from changed_files was not denied")
	}
}

func TestDynamicActualChangesUsesOriginalWorktreeBase(t *testing.T) {
	workspace := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Takt Test", "GIT_AUTHOR_EMAIL=takt@example.invalid", "GIT_COMMITTER_NAME=Takt Test", "GIT_COMMITTER_EMAIL=takt@example.invalid")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	if err := os.MkdirAll(filepath.Join(workspace, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "base.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "base")
	base := strings.TrimSpace(git("rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(workspace, "src", "base.go"), []byte("package src\n// changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "src", "new.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := &dynamicplan.Record{ExecutionWorkspace: workspace, ExecutionBaseCommit: base}
	changes, err := (&Service{Workspace: workspace}).dynamicActualChanges(context.Background(), record)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0] != "src/base.go" || changes[1] != "src/new.go" {
		t.Fatalf("changes=%#v", changes)
	}
}
