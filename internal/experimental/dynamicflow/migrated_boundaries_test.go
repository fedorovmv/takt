package dynamicflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/experimental/dynamicplan"
	"takt/internal/profile"
	"takt/internal/store"
	tasksource "takt/sdk/tasksource"
)

func mustWriteControlTest(t testing.TB, path, value string) { writeControlFile(t, path, value) }
func mustJSONTest(t testing.TB, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDynamicPlanTracksPauseResumeAndAbandon(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	workflowPath := filepath.Join(workspace, "workflow.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	writeControlFile(t, workflowPath, `name: dynamic-operator
nodes:
  - id: approve
    approval:
      message: Continue?
`)
	service, err := newTestServices(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	run := &store.RunState{ID: "run-dynamic-operator", Status: store.RunWaiting, WorkflowPath: workflowPath, ConfigPath: configPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{"approve": {Status: store.NodeWaiting}}, Approvals: map[string]string{}, Waiting: &store.WaitingState{NodeID: "approve", Kind: "approval", Message: "Continue?"}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(run); err != nil {
		t.Fatal(err)
	}
	plan := candidateDynamicPlan()
	record := &dynamicplan.Record{ID: "plan-operator123456", Status: "running", Profile: "code", ConfigPath: configPath, CurrentRunID: run.ID, ExecutionRunIDs: []string{run.ID}, Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: plan}}}
	planStore := dynamicplan.Store{Workspace: workspace}
	if err := planStore.Save(record); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunService.Pause(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "paused" {
		t.Fatalf("plan status after pause = %q", loaded.Status)
	}
	if _, err := service.RunService.ResumePaused(context.Background(), run.ID, false); err != nil {
		t.Fatal(err)
	}
	loaded, err = planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "waiting" {
		t.Fatalf("plan status after waiting resume = %q", loaded.Status)
	}
	if _, err := service.RunService.Abandon(context.Background(), run.ID, "operator stopped campaign"); err != nil {
		t.Fatal(err)
	}
	loaded, err = planStore.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != "abandoned" {
		t.Fatalf("plan status after abandon = %q", loaded.Status)
	}
	state, err := service.RunService.GetRun(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunAbandoned {
		t.Fatalf("run status after abandon = %q", state.Status)
	}
}

func TestPlanForkPreservesStructuredTaskSource(t *testing.T) {
	workspace := t.TempDir()
	if _, err := profile.Init("code", workspace, false); err != nil {
		t.Fatal(err)
	}
	service, err := newTestServices(workspace, filepath.Join(workspace, ".takt", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := candidateDynamicPlan()
	source := &tasksource.Task{
		APIVersion: tasksource.ProtocolV1Alpha1,
		Kind:       "Task",
		ID:         "github:acme/app#42",
		Title:      "Fix issue",
		Goal:       candidate.Goal,
		Source: tasksource.Source{
			Adapter: "github", Kind: "github.issue", Reference: "acme/app#42", Revision: "sha256:immutable",
		},
	}
	planned, err := service.PlanService.Plan(context.Background(), PlanRequest{Goal: candidate.Goal, Profile: "code", Candidate: &candidate, TaskSource: source})
	if err != nil {
		t.Fatal(err)
	}
	sourceRunID := "run-plan-fork-source"
	now := time.Now().UTC()
	run := &store.RunState{ID: sourceRunID, Status: store.RunCompleted, WorkflowPath: "workflow.yaml", ConfigPath: service.ConfigPath, Workspace: workspace, ExecutionWorkspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := (store.FS{Workspace: workspace}).Save(run); err != nil {
		t.Fatal(err)
	}
	planned.Record.CurrentRunID = sourceRunID
	planned.Record.ExecutionRunIDs = []string{sourceRunID}
	if err := service.PlanService.savePlanRecord(planned.Record); err != nil {
		t.Fatal(err)
	}
	forked, err := service.ForkService.Fork(context.Background(), ForkRequest{RunID: sourceRunID})
	if err != nil {
		t.Fatal(err)
	}
	if forked.Plan == nil || forked.Plan.Record.TaskSource == nil {
		t.Fatalf("fork lost task source: %#v", forked)
	}
	got := forked.Plan.Record.TaskSource.Source
	if got.Adapter != "github" || got.Reference != "acme/app#42" || got.Revision != "sha256:immutable" {
		t.Fatalf("fork changed task provenance: %#v", forked.Plan.Record.TaskSource)
	}
}

func TestSavePlanRecordRedactsRunConfigSecrets(t *testing.T) {
	workspace := t.TempDir()
	defaultConfig := filepath.Join(workspace, "default.yaml")
	runConfig := filepath.Join(workspace, "run.yaml")
	const envName = "TAKT_PLAN_REDACTION_VALUE"
	const secret = "plan-secret-42"
	t.Setenv(envName, secret)
	mustWriteControlTest(t, defaultConfig, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
`)
	mustWriteControlTest(t, runConfig, `apiVersion: takt/v1alpha1
kind: Config
models:
  demo:
    provider: test
    id: demo
assistants:
  worker:
    type: mock
    env:
      VALUE: secret://`+envName+`
`)
	service, err := newTestServices(workspace, defaultConfig)
	if err != nil {
		t.Fatal(err)
	}
	record := &dynamicplan.Record{
		ID:         "plan-redaction-test",
		Status:     "running",
		Profile:    "code",
		ConfigPath: runConfig,
		Results:    map[string]string{"inspect": `{"summary":"` + secret + `"}`},
		LastError:  "failed with " + secret,
		Steering:   []dynamicplan.Steering{{Message: "keep " + secret}},
		Revisions:  []dynamicplan.Revision{},
	}
	if err := service.PlanService.savePlanRecord(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := (dynamicplan.Store{Workspace: workspace}).Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(mustJSONTest(t, loaded))
	if strings.Contains(raw, secret) {
		t.Fatalf("plan record leaked secret: %s", raw)
	}
	if loaded.LastError != "failed with <redacted>" || loaded.Steering[0].Message != "keep <redacted>" || !strings.Contains(loaded.Results["inspect"], "<redacted>") {
		t.Fatalf("plan record was not redacted: %#v", loaded)
	}
}

func TestPlanForkFingerprintChangesWithPlanContent(t *testing.T) {
	record := &dynamicplan.Record{BlockCatalogFingerprint: "catalog", Revisions: []dynamicplan.Revision{{Plan: dynamicplan.Plan{Goal: "one"}}}}
	first := planForkFingerprint(record)
	if first == "" {
		t.Fatal("empty plan fork fingerprint")
	}
	record.Revisions[0].Plan.Goal = "two"
	second := planForkFingerprint(record)
	if second == "" || second == first {
		t.Fatalf("fingerprint did not change: %q -> %q", first, second)
	}
}

func TestAttentionIncludesParkedPlanWithFailureCode(t *testing.T) {
	workspace := t.TempDir()
	configPath := filepath.Join(workspace, "config.yaml")
	writeControlFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	service, err := newTestServices(workspace, configPath)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	record := &dynamicplan.Record{ID: "plan-attention-parked", Status: "parked", ConfigPath: configPath, Results: map[string]string{}, CreatedAt: now, UpdatedAt: now, Revisions: []dynamicplan.Revision{{Number: 1, Reason: "test", CreatedAt: now, Plan: dynamicplan.Plan{Goal: "x"}}}}
	parkPlan(record, "OWNER_DECISION_REQUIRED", "choose a safe path", "owner", "reply with steering", false)
	if err := (dynamicplan.Store{Workspace: workspace}).Save(record); err != nil {
		t.Fatal(err)
	}
	items, err := service.RunService.Attention()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].PlanID != record.ID || items[0].Reason != "owner_decision_required" {
		t.Fatalf("attention = %#v", items)
	}
}
