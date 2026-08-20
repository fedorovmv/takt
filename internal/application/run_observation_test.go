package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/assessment"
	"takt/internal/store"
	"takt/internal/validation"
)

func TestRunStatsStatusAndInspectShareAssessmentFacts(t *testing.T) {
	service, fs, root, second := observationFixture(t)
	status, err := service.RunService.Status(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := service.RunService.Stats(RunStatsQuery{RunID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := service.RunService.Inspect(RunInspectQuery{RunID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if status.Status != store.RunCompleted || stats.Status != status.Status || inspection.Status != status.Status {
		t.Fatalf("status=%+v stats=%+v inspect=%+v", status, stats, inspection)
	}
	if status.Matrix.Total != 2 || status.Matrix.Completed != 2 || stats.Total != 2 || stats.Evaluated != 2 {
		t.Fatalf("status=%+v stats=%+v", status, stats)
	}
	if stats.Outcomes[assessment.OutcomeTrueAccept] != 1 || stats.Outcomes[assessment.OutcomeFalseAccept] != 1 || stats.ValidRate.Numerator != 1 || stats.ValidRate.Denominator != 2 || stats.ValidRate.Value == nil || *stats.ValidRate.Value != 0.5 {
		t.Fatalf("stats=%+v", stats)
	}
	if status.Attempts == 0 || status.Attempts != stats.Attempts || stats.Attempts != inspection.Attempts || status.Usage == nil || stats.Usage.InputTokens != status.Usage.InputTokens || inspection.Usage.InputTokens != status.Usage.InputTokens {
		t.Fatalf("status=%+v stats=%+v inspect=%+v", status, stats, inspection)
	}
	if len(inspection.Cases) != 2 || inspection.Cases[1].Cause.Source != "validator" || inspection.Cases[1].Cause.Code != "WRONG" || len(inspection.Cases[1].Evidence) != 1 {
		t.Fatalf("inspection=%+v", inspection)
	}
	if inspection.Cause.Source != "validator" || inspection.Cause.Code != "WRONG" {
		t.Fatalf("inspection cause=%+v", inspection.Cause)
	}

	second.ResultRevision++
	if err := fs.Commit(second, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := service.RunService.Stats(RunStatsQuery{RunID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Evaluated != 1 || fresh.Outcomes[assessment.OutcomeFalseAccept] != 0 || fresh.ValidationErrorRate.Numerator != 1 || fresh.ValidationErrorRate.Denominator != 2 || fresh.FlowCompletionRate.Numerator != 2 {
		t.Fatalf("stale assessment was included: %+v", fresh)
	}
}

func TestRunStatsFlowCompletionCountsDistinctMatrixScopes(t *testing.T) {
	service, fs, root, _ := observationFixture(t)
	first, err := fs.Load(root.ChildRunIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	for index := range root.Artifacts {
		artifact := &root.Artifacts[index]
		if artifact.ID != "assessment-b" {
			continue
		}
		raw, readErr := os.ReadFile(artifact.Path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		value, decodeErr := assessment.Decode(raw)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		value.Target.RunID = first.ID
		raw, marshalErr := json.Marshal(value)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(artifact.Path, raw, 0o644); writeErr != nil {
			t.Fatal(writeErr)
		}
		sum := sha256.Sum256(raw)
		artifact.SHA256 = hex.EncodeToString(sum[:])
		artifact.Size = int64(len(raw))
		break
	}
	if err := fs.Save(root); err != nil {
		t.Fatal(err)
	}
	stats, err := service.RunService.Stats(RunStatsQuery{RunID: root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FlowCompletionRate.Numerator != 2 || stats.FlowCompletionRate.Denominator != 2 {
		t.Fatalf("flow completion=%+v", stats.FlowCompletionRate)
	}
}

func TestRunStatsIgnoresExternalPrimaryAssessmentTargetingEvaluation(t *testing.T) {
	service, fs, root, _ := observationFixture(t)
	root.Input = `{"gates":{"valid_rate":{"min":0.6}}}`
	if err := fs.Save(root); err != nil {
		t.Fatal(err)
	}
	external := &store.RunState{
		ID: "run-external-assessor", Status: store.RunCompleted, WorkflowPath: "assessor.yaml", ConfigPath: root.ConfigPath,
		Workspace: root.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: root.CreatedAt, UpdatedAt: root.UpdatedAt,
	}
	externalArtifact := writeAssessmentQueryFixture(t, fs, external, root, "external-primary", assessment.RolePrimary, root.UpdatedAt.Add(time.Second))
	raw, err := os.ReadFile(externalArtifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	externalValue, err := assessment.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	externalValue.Scope.CaseID = "external-case"
	raw, err = json.Marshal(externalValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(externalArtifact.Path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	external.Artifacts[0].SHA256 = hex.EncodeToString(sum[:])
	external.Artifacts[0].Size = int64(len(raw))
	if err := fs.Save(external); err != nil {
		t.Fatal(err)
	}
	stats, err := service.RunService.Stats(RunStatsQuery{RunID: root.ID, CheckGates: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Evaluated != 2 || stats.ValidRate.Numerator != 1 || stats.ValidRate.Denominator != 2 || stats.FlowCompletionRate.Numerator != 2 || stats.FlowCompletionRate.Denominator != 2 || stats.GatesPassed {
		t.Fatalf("external assessment contaminated stats: %+v", stats)
	}
	assessments, err := service.RunService.Assessments(AssessmentQuery{RunID: root.ID, IncludeStale: true})
	if err != nil {
		t.Fatal(err)
	}
	foundExternal := false
	for _, record := range assessments.Assessments {
		if record.Assessment.ID == "external-primary" {
			foundExternal = true
			if record.Relation != "target" {
				t.Fatalf("external assessment relation = %q", record.Relation)
			}
		}
	}
	if !foundExternal {
		t.Fatalf("run assessment omitted external target relation: %+v", assessments)
	}
}

func TestRunInspectFindsMatrixBranchNodeByPublicID(t *testing.T) {
	service, _, root, _ := observationFixture(t)
	inspection, err := service.RunService.Inspect(RunInspectQuery{RunID: root.ID, NodeID: "validate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Nodes) != 2 {
		t.Fatalf("nodes=%+v", inspection.Nodes)
	}
	for index, node := range inspection.Nodes {
		if node.NodeID != "validate" || node.BranchIndex == nil || *node.BranchIndex != index {
			t.Fatalf("node[%d]=%+v", index, node)
		}
	}
}

func TestAssessmentGateReportsExactRatioWithoutMutatingRun(t *testing.T) {
	service, _, root, _ := observationFixture(t)
	stats, err := service.RunService.Stats(RunStatsQuery{RunID: root.ID, CheckGates: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.GatesPassed || len(stats.Gates) != 1 || stats.Gates[0].Passed || stats.Gates[0].Numerator != 1 || stats.Gates[0].Denominator != 2 || !strings.Contains(stats.Gates[0].Message, "1/2") {
		t.Fatalf("gates=%+v", stats.Gates)
	}
	reloaded, err := service.RunService.GetRun(root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != store.RunCompleted || reloaded.Revision != root.Revision {
		t.Fatalf("gate mutated Run: before=%+v after=%+v", root, reloaded)
	}
}

func observationFixture(t *testing.T) (*Services, store.FS, *store.RunState, *store.RunState) {
	t.Helper()
	workspace := t.TempDir()
	fs := store.FS{Workspace: workspace}
	now := time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)
	first := observationTarget(t, fs, workspace, "run-target-a", store.RunCompleted, now)
	second := observationTarget(t, fs, workspace, "run-target-b", store.RunCompleted, now.Add(time.Second))
	root := &store.RunState{
		ID: "run-evaluation", Status: store.RunRunning,
		WorkflowPath: "evaluate.yaml", ConfigPath: filepath.Join(workspace, "config.yaml"), Workspace: workspace,
		Input: `{"gates":{"valid_rate":{"min":1}}}`, InputFormat: "json", Output: "done",
		Nodes: map[string]*store.NodeState{"cases": {
			Status: store.NodeCompleted, Attempts: 1, MatrixBranches: []store.MatrixBranchState{
				{Index: 0, Item: json.RawMessage(`{"case_id":"a"}`), ItemFingerprint: strings.Repeat("a", 64), Status: store.NodeCompleted, Nodes: map[string]store.NodeState{"cases__candidate": {Status: store.NodeCompleted, Attempts: 1, ChildRunID: first.ID}, "cases__validate": {Status: store.NodeCompleted, Attempts: 1}}},
				{Index: 1, Item: json.RawMessage(`{"case_id":"b"}`), ItemFingerprint: strings.Repeat("b", 64), Status: store.NodeCompleted, Nodes: map[string]store.NodeState{"cases__candidate": {Status: store.NodeCompleted, Attempts: 1, ChildRunID: second.ID}, "cases__validate": {Status: store.NodeCompleted, Attempts: 2}}},
			},
		}},
		Approvals: map[string]string{}, ChildRunIDs: []string{first.ID, second.ID}, Usage: &store.Usage{InputTokens: 10, OutputTokens: 4, Cost: 0.2}, CreatedAt: now, UpdatedAt: now.Add(3 * time.Second),
	}
	if err := fs.Save(root); err != nil {
		t.Fatal(err)
	}
	root.Status = store.RunCompleted
	root.ResultRevision = 1
	if err := fs.Commit(root, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	writeObservationAssessment(t, fs, root, first, "assessment-a", "case-a", true, nil, assessment.OutcomeTrueAccept, now)
	writeObservationAssessment(t, fs, root, second, "assessment-b", "case-b", false, []validation.Diagnostic{{Code: "WRONG", Severity: "error", Message: "product check failed"}}, assessment.OutcomeFalseAccept, now.Add(time.Second))
	if err := fs.Save(root); err != nil {
		t.Fatal(err)
	}
	service, err := New(workspace, root.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	return service, fs, root, second
}

func observationTarget(t *testing.T, fs store.FS, workspace, id, status string, now time.Time) *store.RunState {
	t.Helper()
	state := &store.RunState{ID: id, Status: store.RunRunning, WorkflowPath: id + ".yaml", ConfigPath: "config.yaml", Workspace: workspace, Nodes: map[string]*store.NodeState{"work": {Status: store.NodeCompleted, Attempts: 1}}, Approvals: map[string]string{}, Usage: &store.Usage{InputTokens: 5, OutputTokens: 2, Cost: 0.1}, CreatedAt: now, UpdatedAt: now}
	if err := fs.Save(state); err != nil {
		t.Fatal(err)
	}
	state.Status = status
	state.ResultRevision = 1
	if err := fs.Commit(state, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	return state
}

func writeObservationAssessment(t *testing.T, fs store.FS, assessor, target *store.RunState, id, caseID string, valid bool, diagnostics []validation.Diagnostic, outcome string, createdAt time.Time) {
	t.Helper()
	evidenceID := "evidence-" + caseID
	evidencePath := filepath.Join(fs.ArtifactsDir(assessor.ID), evidenceID+".txt")
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	evidenceRaw := []byte(caseID)
	if err := os.WriteFile(evidencePath, evidenceRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	evidenceSum := sha256.Sum256(evidenceRaw)
	assessor.Artifacts = append(assessor.Artifacts, store.ArtifactRef{ID: evidenceID, Type: "evaluation-evidence", MIME: "text/plain", Path: evidencePath, SHA256: hex.EncodeToString(evidenceSum[:]), Size: int64(len(evidenceRaw)), ProducerRunID: assessor.ID, ProducerNodeID: "evidence", Attempt: 1, CreatedAt: createdAt})
	value := assessment.Envelope{
		ProtocolVersion: assessment.ProtocolV1Alpha1, Type: assessment.TypeAssessment, ID: id, Role: assessment.RolePrimary,
		Target: assessment.Target{RunID: target.ID, Revision: target.ResultRevision, Status: target.Status}, Assessor: assessment.Assessor{RunID: assessor.ID, NodeID: "assess", Revision: assessor.Revision},
		Scope: assessment.Scope{CaseID: caseID, Repeat: 1}, Result: validation.Result{ProtocolVersion: validation.ProtocolV1Alpha1, Type: "validation_result", Valid: valid, Diagnostics: diagnostics}, Outcome: outcome,
		Evidence: []assessment.EvidenceRef{{ProducerRunID: assessor.ID, ArtifactID: evidenceID, SHA256: hex.EncodeToString(evidenceSum[:])}}, CreatedAt: createdAt,
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(fs.ArtifactsDir(assessor.ID), id+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	assessor.Artifacts = append(assessor.Artifacts, store.ArtifactRef{ID: id, Type: assessment.TypeAssessment, MIME: assessment.MIMEAssessment, Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), ProducerRunID: assessor.ID, ProducerNodeID: "assess", Attempt: 1, CreatedAt: createdAt})
}
