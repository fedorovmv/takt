package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"takt/internal/assessment"
	"takt/internal/store"
	"takt/internal/validation"
)

func TestAssessmentsQueriesTargetAndAssessorAndCalculatesStale(t *testing.T) {
	workspace := t.TempDir()
	fs := store.FS{Workspace: workspace}
	target := savedTerminalAssessmentTarget(t, fs, workspace)
	assessor := &store.RunState{ID: "run-assessor", Status: store.RunCompleted, WorkflowPath: "assessor.yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	primary := writeAssessmentQueryFixture(t, fs, assessor, target, "primary-assessment", assessment.RolePrimary, time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC))
	writeAssessmentQueryFixture(t, fs, assessor, target, "advisory-assessment", assessment.RoleAdvisory, time.Date(2026, 8, 20, 1, 1, 0, 0, time.UTC))
	if err := fs.Save(assessor); err != nil {
		t.Fatal(err)
	}
	target.Artifacts = append(target.Artifacts, primary)
	if err := fs.Commit(target, store.Event{Type: "artifact.propagated"}); err != nil {
		t.Fatal(err)
	}
	services, err := New(workspace, filepath.Join(workspace, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	byTarget, err := services.RunService.Assessments(AssessmentQuery{RunID: target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(byTarget.Assessments) != 2 || byTarget.Assessments[0].Relation != "target" || byTarget.Assessments[0].Stale {
		t.Fatalf("target assessments = %+v", byTarget)
	}
	byAssessor, err := services.RunService.Assessments(AssessmentQuery{RunID: assessor.ID, Role: assessment.RolePrimary})
	if err != nil {
		t.Fatal(err)
	}
	if len(byAssessor.Assessments) != 1 || byAssessor.Assessments[0].Relation != "assessor" || byAssessor.Assessments[0].Assessment.ID != primary.ID {
		t.Fatalf("assessor assessments = %+v", byAssessor)
	}

	target.ResultRevision = target.Revision + 1
	if err := fs.Commit(target, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := services.RunService.Assessments(AssessmentQuery{RunID: target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(fresh.Assessments) != 0 {
		t.Fatalf("stale assessments were included by default: %+v", fresh)
	}
	withStale, err := services.RunService.Assessments(AssessmentQuery{RunID: target.ID, IncludeStale: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(withStale.Assessments) != 2 || !withStale.Assessments[0].Stale || !withStale.Assessments[1].Stale {
		t.Fatalf("stale assessments = %+v", withStale)
	}
}

func TestAssessmentsReturnsAssessmentCorruptWithProducerIdentity(t *testing.T) {
	workspace := t.TempDir()
	fs := store.FS{Workspace: workspace}
	target := savedTerminalAssessmentTarget(t, fs, workspace)
	assessor := &store.RunState{ID: "run-assessor", Status: store.RunCompleted, WorkflowPath: "assessor.yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	artifact := writeAssessmentQueryFixture(t, fs, assessor, target, "corrupt-assessment", assessment.RoleAdvisory, time.Now().UTC())
	if err := fs.Save(assessor); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact.Path, []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	services, err := New(workspace, filepath.Join(workspace, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = services.RunService.Assessments(AssessmentQuery{RunID: target.ID, IncludeStale: true})
	var corrupt *assessment.CorruptError
	if !errors.As(err, &corrupt) || corrupt.ProducerRunID != assessor.ID || corrupt.ArtifactID != artifact.ID {
		t.Fatalf("corrupt error = %#v (%v)", corrupt, err)
	}
}

func TestAssessmentsRejectsWrongAssessmentMIME(t *testing.T) {
	workspace := t.TempDir()
	fs := store.FS{Workspace: workspace}
	target := savedTerminalAssessmentTarget(t, fs, workspace)
	assessor := &store.RunState{ID: "run-assessor", Status: store.RunCompleted, WorkflowPath: "assessor.yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	artifact := writeAssessmentQueryFixture(t, fs, assessor, target, "wrong-mime", assessment.RoleAdvisory, time.Now().UTC())
	assessor.Artifacts[0].MIME = "application/json"
	if err := fs.Save(assessor); err != nil {
		t.Fatal(err)
	}
	services, err := New(workspace, filepath.Join(workspace, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = services.RunService.Assessments(AssessmentQuery{RunID: target.ID})
	var corrupt *assessment.CorruptError
	if !errors.As(err, &corrupt) || corrupt.ArtifactID != artifact.ID {
		t.Fatalf("wrong MIME error = %#v (%v)", corrupt, err)
	}
}

func TestAssessmentsRecoversLegacyTargetResultRevisionFromEvents(t *testing.T) {
	workspace := t.TempDir()
	fs := store.FS{Workspace: workspace}
	target := savedTerminalAssessmentTarget(t, fs, workspace)
	assessor := &store.RunState{ID: "run-assessor", Status: store.RunCompleted, WorkflowPath: "assessor.yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	writeAssessmentQueryFixture(t, fs, assessor, target, "legacy-assessment", assessment.RoleAdvisory, time.Now().UTC())
	if err := fs.Save(assessor); err != nil {
		t.Fatal(err)
	}
	target.ResultRevision = 0
	if err := fs.Commit(target, store.Event{Type: "legacy.migrated"}); err != nil {
		t.Fatal(err)
	}
	services, err := New(workspace, filepath.Join(workspace, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := services.RunService.Assessments(AssessmentQuery{RunID: target.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Assessments) != 1 || result.Assessments[0].Stale {
		t.Fatalf("legacy assessments = %+v", result)
	}
}

func savedTerminalAssessmentTarget(t *testing.T, fs store.FS, workspace string) *store.RunState {
	t.Helper()
	now := time.Now().UTC()
	target := &store.RunState{ID: "run-target", Status: store.RunRunning, WorkflowPath: "target.yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := fs.Save(target); err != nil {
		t.Fatal(err)
	}
	target.Status = store.RunCompleted
	target.ResultRevision = 1
	if err := fs.Commit(target, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	return target
}

func writeAssessmentQueryFixture(t *testing.T, fs store.FS, assessor, target *store.RunState, id, role string, createdAt time.Time) store.ArtifactRef {
	t.Helper()
	scope := assessment.Scope{}
	evidence := []assessment.EvidenceRef{}
	if role == assessment.RolePrimary {
		scope = assessment.Scope{CaseID: "case-a", Repeat: 1}
		evidence = []assessment.EvidenceRef{{ProducerRunID: assessor.ID, ArtifactID: "evidence", SHA256: hex.EncodeToString(make([]byte, 32))}}
	}
	value := assessment.Envelope{
		ProtocolVersion: assessment.ProtocolV1Alpha1, Type: assessment.TypeAssessment, ID: id, Role: role,
		Target:   assessment.Target{RunID: target.ID, Revision: target.ResultRevision, Status: target.Status},
		Assessor: assessment.Assessor{RunID: assessor.ID, NodeID: "assess", Revision: 1}, Scope: scope,
		Result:  validation.Result{ProtocolVersion: validation.ProtocolV1Alpha1, Type: "validation_result", Valid: true},
		Outcome: assessment.OutcomeTrueAccept, Evidence: evidence, CreatedAt: createdAt,
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fs.ArtifactsDir(assessor.ID), id+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	artifact := store.ArtifactRef{ID: id, Type: assessment.TypeAssessment, MIME: "application/vnd.takt.assessment+json", Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), ProducerRunID: assessor.ID, ProducerNodeID: "assess", Attempt: 1, CreatedAt: createdAt}
	assessor.Artifacts = append(assessor.Artifacts, artifact)
	return artifact
}
