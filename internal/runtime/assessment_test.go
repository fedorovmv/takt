package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/assessment"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestAssessmentRecordsInvalidResultWithoutMutatingTarget(t *testing.T) {
	dir := t.TempDir()
	targetRunner := New(&spec.Workflow{Name: "target", Nodes: []spec.Node{{ID: "done", Bash: "printf done"}}}, &spec.Config{}, filepath.Join(dir, "target.yaml"), "<config>", dir)
	target, err := targetRunner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-target"})
	if err != nil {
		t.Fatal(err)
	}
	if target.Status != store.RunCompleted || target.ResultRevision == 0 {
		t.Fatalf("target = %+v", target)
	}
	targetPath := filepath.Join((store.FS{Workspace: dir}).RunDir(target.ID), "state.json")
	targetBefore, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}

	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"diagnostics":[]}'`},
		{ID: "evidence", Bash: "printf evidence", OutputType: "evaluation-evidence", OutputMIME: "text/plain"},
		{ID: "assess", DependsOn: []string{"validate", "evidence"}, Assessment: &spec.AssessmentSpec{
			Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output",
			Scope:    map[string]string{"case_id": "case-a", "repeat": "1"},
			Evidence: []string{"$evidence.artifacts.evaluation-evidence"},
		}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted {
		t.Fatalf("assessor status = %s", state.Status)
	}
	node := state.Nodes["assess"]
	if len(node.Artifacts) != 1 || len(state.Artifacts) != 2 {
		t.Fatalf("assessment artifacts: node=%+v run=%+v", node.Artifacts, state.Artifacts)
	}
	artifact := node.Artifacts[0]
	if artifact.Type != assessment.TypeAssessment || artifact.MIME != "application/vnd.takt.assessment+json" {
		t.Fatalf("artifact = %+v", artifact)
	}
	raw, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := assessment.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if value.Result.Valid || value.Outcome != assessment.OutcomeFalseAccept || value.Target.RunID != target.ID || value.Target.Revision != target.ResultRevision {
		t.Fatalf("assessment = %+v", value)
	}
	if value.Assessor.RunID != state.ID || value.Assessor.NodeID != "assess" || value.Assessor.Revision == 0 || value.Assessor.Revision > state.Revision {
		t.Fatalf("assessor provenance = %+v, run revision=%d", value.Assessor, state.Revision)
	}
	if len(value.Evidence) != 1 || value.Evidence[0].ArtifactID != state.Nodes["evidence"].Artifacts[0].ID {
		t.Fatalf("evidence = %+v", value.Evidence)
	}
	targetAfter, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetAfter) != string(targetBefore) {
		t.Fatal("assessment mutated target state")
	}
}

func TestAssessmentRejectsAssistantProducedPrimaryResult(t *testing.T) {
	dir := t.TempDir()
	fs := store.FS{Workspace: dir}
	target := completedAssessmentTarget(t, fs, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Prompt: "judge"},
		{ID: "assess", Assessment: &spec.AssessmentSpec{Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output"}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state := &store.RunState{ID: "run-assessor", Nodes: map[string]*store.NodeState{
		"validate": {Status: store.NodeCompleted, Output: `{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}`},
		"assess":   {Status: store.NodeRunning, Attempts: 1},
	}}
	_, err := runner.executeAssessmentAction(state, wf.Nodes[1], actionContext{})
	if err == nil || !strings.Contains(err.Error(), "not deterministic") {
		t.Fatalf("assistant primary result error = %v", err)
	}
}

func TestAssessmentMissingEvidenceFailsWithSpecificDiagnostic(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence", Bash: "printf evidence", When: `$INPUTS.input == "present"`, OutputType: "evaluation-evidence"},
		{ID: "assess", DependsOn: []string{"validate", "evidence"}, TriggerRule: "all_done", Assessment: &spec.AssessmentSpec{
			Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output",
			Scope:    map[string]string{"case_id": "case-a", "repeat": "1"},
			Evidence: []string{"$evidence.artifacts.evaluation-evidence"},
		}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	if got := state.Nodes["assess"].ErrorCode; got != "evidence_missing" {
		t.Fatalf("assessment error code = %q, want evidence_missing", got)
	}
}

func TestAssessmentRejectsTamperedEvidence(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence", Bash: "printf evidence", OutputType: "evaluation-evidence"},
		{ID: "tamper", DependsOn: []string{"evidence"}, Bash: "printf changed > $evidence.artifacts.evaluation-evidence.path"},
		{ID: "assess", DependsOn: []string{"validate", "tamper"}, Assessment: &spec.AssessmentSpec{
			Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output",
			Scope: map[string]string{"case_id": "case-a", "repeat": "1"}, Evidence: []string{"$evidence.artifacts.evaluation-evidence"},
		}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed || state.Nodes["assess"].ErrorCode != "evidence_missing" || !strings.Contains(state.Nodes["assess"].Error, "checksum") {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestAssessmentAcceptsDeterministicGovernedValidatorOutput(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	validatorPath := filepath.Join(dir, "validator.yaml")
	if err := os.WriteFile(validatorPath, []byte(`name: validator
nodes:
  - id: validate
    bash: |
      printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'
`), 0o644); err != nil {
		t.Fatal(err)
	}
	parentPath := filepath.Join(dir, "assessor.yaml")
	if err := os.WriteFile(parentPath, []byte(`name: assessor
nodes:
  - id: validator
    workflow:
      path: validator.yaml
      isolation: inherit
  - id: evidence
    bash: printf evidence
    output_type: evaluation-evidence
  - id: assess
    depends_on: [validator, evidence]
    assessment:
      role: primary
      target_run_id: run-target
      result_from: $validator.output
      scope: {case_id: case-a, repeat: "1"}
      evidence: [$evidence.artifacts.evaluation-evidence]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, parentPath, "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || len(state.Nodes["assess"].Artifacts) != 1 || state.Nodes["validator"].ChildRunID == target.ID {
		t.Fatalf("state = %+v", state)
	}
}

func TestAssessmentRejectsMalformedResultAsProtocolError(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: "printf malformed"},
		{ID: "assess", DependsOn: []string{"validate"}, Assessment: &spec.AssessmentSpec{Role: "advisory", TargetRunID: target.ID, ResultFrom: "$validate.output"}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed || state.Nodes["assess"].ErrorCode != "protocol" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestAssessmentRejectsNonterminalTarget(t *testing.T) {
	dir := t.TempDir()
	fs := store.FS{Workspace: dir}
	now := time.Now().UTC()
	target := &store.RunState{ID: "run-target", Status: store.RunRunning, WorkflowPath: "target.yaml", ConfigPath: "config.yaml", Workspace: dir, Input: "", Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := fs.Save(target); err != nil {
		t.Fatal(err)
	}
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "assess", DependsOn: []string{"validate"}, Assessment: &spec.AssessmentSpec{Role: "advisory", TargetRunID: target.ID, ResultFrom: "$validate.output"}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed || state.Nodes["assess"].Status != store.NodeErrored {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestAssessmentRejectsDuplicatePrimaryScope(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	primary := func(result, evidence string) *spec.AssessmentSpec {
		return &spec.AssessmentSpec{Role: "primary", TargetRunID: target.ID, ResultFrom: "$" + result + ".output",
			Scope: map[string]string{"case_id": "case-a", "repeat": "1"}, Evidence: []string{"$" + evidence + ".artifacts.evaluation-evidence"}}
	}
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate1", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence1", Bash: "printf one", OutputType: "evaluation-evidence"},
		{ID: "assess1", DependsOn: []string{"validate1", "evidence1"}, Assessment: primary("validate1", "evidence1")},
		{ID: "validate2", DependsOn: []string{"assess1"}, Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence2", DependsOn: []string{"assess1"}, Bash: "printf two", OutputType: "evaluation-evidence"},
		{ID: "assess2", DependsOn: []string{"validate2", "evidence2"}, Assessment: primary("validate2", "evidence2")},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed || state.Nodes["assess2"].ErrorCode != "assessment_ambiguous" || !strings.Contains(state.Nodes["assess2"].Error, "ambiguous") {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestAssessmentPersistenceFailureRemovesUnregisteredArtifact(t *testing.T) {
	dir := t.TempDir()
	fs := store.FS{Workspace: dir}
	target := completedAssessmentTarget(t, fs, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence", Bash: "printf evidence", OutputType: "evaluation-evidence"},
		{ID: "assess", DependsOn: []string{"validate", "evidence"}, Assessment: &spec.AssessmentSpec{
			Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output",
			Scope: map[string]string{"case_id": "case-a", "repeat": "1"}, Evidence: []string{"$evidence.artifacts.evaluation-evidence"},
		}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	runner.store = &failAssessmentRecordedStore{Repository: fs}
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	path := filepath.Join(fs.ArtifactsDir(state.ID), "nodes", "assess", "1", "assessment.json")
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("unregistered assessment remains at %s: %v", path, statErr)
	}
}

func TestWriteAtomicAssessmentArtifactNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "assessment.json")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicArtifact(path, []byte("replacement")); err == nil {
		t.Fatal("existing assessment artifact was overwritten")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "original" {
		t.Fatalf("artifact = %q", raw)
	}
}

func TestAssessmentRejectsTargetRevisionChangeDuringCapture(t *testing.T) {
	dir := t.TempDir()
	fs := store.FS{Workspace: dir}
	target := completedAssessmentTarget(t, fs, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "assess", DependsOn: []string{"validate"}, Assessment: &spec.AssessmentSpec{Role: "advisory", TargetRunID: target.ID, ResultFrom: "$validate.output"}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	changing := &changingTargetStore{Repository: fs, targetID: target.ID}
	runner.store = changing
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err == nil || state.Status != store.RunFailed || changing.loads < 2 {
		t.Fatalf("state=%+v err=%v target_loads=%d", state, err, changing.loads)
	}
}

func TestAssessmentOperatorRetryCreatesNewImmutableAttempt(t *testing.T) {
	dir := t.TempDir()
	fs := store.FS{Workspace: dir}
	target := completedAssessmentTarget(t, fs, dir, "run-target")
	wf := &spec.Workflow{Name: "assessor", Nodes: []spec.Node{
		{ID: "validate", Bash: `printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'`},
		{ID: "evidence", Bash: "printf evidence", OutputType: "evaluation-evidence"},
		{ID: "assess", DependsOn: []string{"validate", "evidence"}, Assessment: &spec.AssessmentSpec{
			Role: "primary", TargetRunID: target.ID, ResultFrom: "$validate.output",
			Scope: map[string]string{"case_id": "case-a", "repeat": "1"}, Evidence: []string{"$evidence.artifacts.evaluation-evidence"},
		}},
	}}
	runner := New(wf, &spec.Config{}, filepath.Join(dir, "assessor.yaml"), "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: "run-assessor"})
	if err != nil {
		t.Fatal(err)
	}
	old := state.Nodes["assess"].Artifacts[0]
	target.ResultRevision = target.Revision + 1
	if err := fs.Commit(target, store.Event{Type: "run.completed"}); err != nil {
		t.Fatal(err)
	}
	state.Status = store.RunRunning
	state.ResultRevision = 0
	state.Nodes["assess"].Status = store.NodeRunning
	state.Nodes["assess"].Attempts = 2
	if err := fs.Commit(state, store.Event{Type: "run.retry_requested", NodeID: "assess"}); err != nil {
		t.Fatal(err)
	}
	result, err := runner.executeAssessmentAction(state, wf.Nodes[2], runner.actionContext(state, wf.Nodes[2], nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].ID == old.ID || result.Artifacts[0].Attempt != 2 {
		t.Fatalf("old=%+v new=%+v", old, result.Artifacts)
	}
}

type changingTargetStore struct {
	store.Repository
	targetID string
	loads    int
}

func (s *changingTargetStore) Load(id string) (*store.RunState, error) {
	state, err := s.Repository.Load(id)
	if err != nil || id != s.targetID {
		return state, err
	}
	s.loads++
	if s.loads > 1 {
		state.ResultRevision++
		state.Revision++
	}
	return state, nil
}

type failAssessmentRecordedStore struct {
	store.Repository
	failed bool
}

func (s *failAssessmentRecordedStore) Commit(state *store.RunState, event store.Event) error {
	if event.Type == "assessment.recorded" && !s.failed {
		s.failed = true
		return errors.New("assessment persistence failed")
	}
	return s.Repository.Commit(state, event)
}

func completedAssessmentTarget(t *testing.T, fs store.FS, dir, id string) *store.RunState {
	t.Helper()
	runner := New(&spec.Workflow{Name: "target", Nodes: []spec.Node{{ID: "done", Bash: "printf done"}}}, &spec.Config{}, filepath.Join(dir, "target.yaml"), "<config>", dir)
	target, err := runner.StartWithOptions(context.Background(), "", StartOptions{RunID: id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Load(id); err != nil {
		t.Fatal(err)
	}
	return target
}
