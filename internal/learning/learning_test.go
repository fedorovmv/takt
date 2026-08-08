package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/evaluation"
	"takt/internal/store"
)

func TestHumanReviewedSkillLearningLoop(t *testing.T) {
	workspace := t.TempDir()
	st := store.FS{Workspace: workspace}
	for i := 1; i <= 2; i++ {
		now := time.Now().UTC()
		id := "run-learning-" + string(rune('0'+i))
		state := &store.RunState{
			ID: id, Status: store.RunFailed, WorkflowPath: "workflow.yaml", ConfigPath: "config.yaml", Workspace: workspace, ExecutionWorkspace: workspace,
			Nodes:     map[string]*store.NodeState{"validate": {Status: store.NodeFailed, Diagnostic: &store.DiagnosticState{Code: "VALIDATION", Message: "same failure", Fingerprint: "sha256:repeat"}}},
			Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		}
		if err := st.Save(state); err != nil {
			t.Fatal(err)
		}
	}
	manager := Manager{Workspace: workspace}
	patterns, err := manager.Scan(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 || patterns[0].Fingerprint != "diagnostic:sha256:repeat" || patterns[0].Count != 2 {
		t.Fatalf("patterns=%+v", patterns)
	}
	proposal, err := manager.Propose(context.Background(), ProposeRequest{PatternFingerprint: patterns[0].Fingerprint, CandidateKind: "skill", Name: "repeat-validation", ExpectedBenefit: "avoid repeating the same validation failure", MinRuns: 2})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != StatusPending || proposal.Candidate.SHA256 == "" || len(proposal.Pattern.RunIDs) != 2 {
		t.Fatalf("proposal=%+v", proposal)
	}
	if _, err := manager.Stage(proposal.ID); err == nil {
		t.Fatal("pending proposal staged without review/evaluation")
	}
	proposal, err = manager.Review(proposal.ID, "accept", "guidance is reusable")
	if err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(workspace, "evaluation.json")
	report := map[string]any{"report_version": evaluation.MatrixReportVersion, "matrix_fingerprint": "sha256:matrix", "benchmark_id": "learning-regression", "passed": true, "gates": []map[string]any{{"strategy": "candidate", "passed": true, "message": "no regression"}}}
	raw, _ := json.Marshal(report)
	if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	proposal, err = manager.Evaluate(proposal.ID, reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Evaluation == nil || !proposal.Evaluation.Passed || proposal.Status != StatusAccepted {
		t.Fatalf("evaluation=%+v", proposal)
	}
	proposal, err = manager.Stage(proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Status != StatusReady || proposal.ReadyPath == "" {
		t.Fatalf("ready=%+v", proposal)
	}
	readySkill := filepath.Join(workspace, filepath.FromSlash(proposal.ReadyPath), "SKILL.md")
	content, err := os.ReadFile(readySkill)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "diagnostic:sha256:repeat") {
		t.Fatalf("ready skill lost provenance: %s", content)
	}
}

func TestEvaluationFailureBlocksStaging(t *testing.T) {
	workspace := t.TempDir()
	manager := Manager{Workspace: workspace}
	now := time.Now().UTC()
	for _, id := range []string{"run-a", "run-b"} {
		state := &store.RunState{ID: id, Status: store.RunFailed, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: workspace, Nodes: map[string]*store.NodeState{"n": {Status: store.NodeFailed, Diagnostic: &store.DiagnosticState{Fingerprint: "fp", Message: "bad"}}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
		if err := (store.FS{Workspace: workspace}).Save(state); err != nil {
			t.Fatal(err)
		}
	}
	p, err := manager.Propose(context.Background(), ProposeRequest{PatternFingerprint: "diagnostic:fp", CandidateKind: "skill", Name: "failure-guide", ExpectedBenefit: "reduce failures"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Review(p.ID, "accept", "reviewed"); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(workspace, "failed.json")
	raw := []byte(`{"report_version":"takt-evaluation-matrix/v1alpha1","matrix_fingerprint":"sha256:matrix","benchmark_id":"learning-regression","passed":false,"gates":[{"strategy":"candidate","passed":false,"message":"regression"}]}`)
	if err := os.WriteFile(reportPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	p, err = manager.Evaluate(p.ID, reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusEvaluationFailed {
		t.Fatalf("status=%s", p.Status)
	}
	if _, err := manager.Stage(p.ID); err == nil {
		t.Fatal("failed evaluation staged")
	}
}

func TestSkillCandidateNameMustMatchProposal(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	for _, id := range []string{"run-name-a", "run-name-b"} {
		state := &store.RunState{ID: id, Status: store.RunFailed, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: workspace, Nodes: map[string]*store.NodeState{"n": {Status: store.NodeFailed, Diagnostic: &store.DiagnosticState{Fingerprint: "same-name-pattern", Message: "bad"}}}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
		if err := (store.FS{Workspace: workspace}).Save(state); err != nil {
			t.Fatal(err)
		}
	}
	candidate := filepath.Join(workspace, "SKILL.md")
	if err := os.WriteFile(candidate, []byte("---\nname: other-name\ndescription: reusable\n---\n\n# Candidate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (Manager{Workspace: workspace}).Propose(context.Background(), ProposeRequest{PatternFingerprint: "diagnostic:same-name-pattern", CandidateKind: "skill", Name: "expected-name", CandidatePath: candidate, ExpectedBenefit: "reduce failures"})
	if err == nil || !strings.Contains(err.Error(), "must match candidate name") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompletedWorkflowPatternIncludesDefinitionContext(t *testing.T) {
	workspace := t.TempDir()
	now := time.Now().UTC()
	states := []*store.RunState{
		{ID: "run-success-a", Status: store.RunCompleted, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: workspace, WorkflowFingerprint: "wf-hash", ConfigFingerprint: "cfg-a", CommandsFingerprint: "cmd", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now},
		{ID: "run-success-b", Status: store.RunCompleted, WorkflowPath: "wf", ConfigPath: "cfg", Workspace: workspace, WorkflowFingerprint: "wf-hash", ConfigFingerprint: "cfg-b", CommandsFingerprint: "cmd", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now},
	}
	for _, state := range states {
		if err := (store.FS{Workspace: workspace}).Save(state); err != nil {
			t.Fatal(err)
		}
	}
	patterns, err := (Manager{Workspace: workspace}).Scan(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 0 {
		t.Fatalf("different config definitions were conflated: %+v", patterns)
	}
}

func TestListFailsClosedOnCorruptProposal(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".takt", "learning", "proposals", "learn-0123456789abcdef01234567")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proposal.json"), []byte(`{"apiVersion":"wrong"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Manager{Workspace: workspace}).List(); err == nil {
		t.Fatal("corrupt proposal was silently hidden")
	}
}
