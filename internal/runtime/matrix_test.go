package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func TestMatrixExecutesOrderedBranchesInOneRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.yaml")
	if err := os.WriteFile(path, []byte(`name: matrix
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items:
          type: object
          properties:
            name: {type: string}
          required: [name]
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      as: case
      nodes:
        - id: emit
          bash: printf '%s:%s/%s' '$case.name' $MATRIX.index $MATRIX.total
      output_node: emit
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, path, "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), `{"cases":[{"name":"alpha"},{"name":"beta"}]}`, StartOptions{RunID: "run-matrix"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RunCompleted || len(state.ChildRunIDs) != 0 {
		t.Fatalf("state = %+v", state)
	}
	var output []string
	if err := json.Unmarshal([]byte(state.Output), &output); err != nil {
		t.Fatal(err)
	}
	if len(output) != 2 || output[0] != "alpha:0/2" || output[1] != "beta:1/2" {
		t.Fatalf("output = %#v", output)
	}
	matrix := state.Nodes["cases"]
	if len(matrix.MatrixBranches) != 2 || matrix.MatrixBranches[0].Index != 0 || matrix.MatrixBranches[1].Index != 1 {
		t.Fatalf("matrix branches = %+v", matrix.MatrixBranches)
	}
	if matrix.MatrixBranches[0].Nodes["cases__emit"].Path != "/cases[0]/emit" || matrix.MatrixBranches[1].Nodes["cases__emit"].Path != "/cases[1]/emit" {
		t.Fatalf("branch paths = %+v", matrix.MatrixBranches)
	}
	public := state.PublicView()
	if _, ok := public.Nodes["cases"].MatrixBranches[0].Nodes["emit"]; !ok {
		t.Fatalf("public matrix snapshot = %+v", public.Nodes["cases"].MatrixBranches)
	}
	branch := public.Nodes["cases"].MatrixBranches[0]
	branch.Nodes["emit"] = store.NodeState{Status: store.NodeFailed}
	public.Nodes["cases"].MatrixBranches[0] = branch
	if state.Nodes["cases"].MatrixBranches[0].Nodes["cases__emit"].Status != store.NodeCompleted {
		t.Fatal("public matrix history aliases durable state")
	}
	ids, err := (store.FS{Workspace: dir}).ListRunIDs()
	if err != nil || len(ids) != 1 || ids[0] != state.ID {
		t.Fatalf("Run IDs = %v, err=%v", ids, err)
	}
}

func TestMatrixRejectsCanonicalDuplicateItems(t *testing.T) {
	state := &store.RunState{Input: `{"cases":[{"a":1,"b":2},{"b":2,"a":1}]}`, InputFormat: "json"}
	if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil {
		t.Fatal("object duplicates with different key order were accepted")
	}
	state.Input = `{"cases":[1,1.0]}`
	if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil {
		t.Fatal("numerically equivalent duplicate items were accepted")
	}
}

func TestMatrixItemsRejectTrailingJSON(t *testing.T) {
	state := &store.RunState{Input: `{"cases":[1]} {}`, InputFormat: "json"}
	if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON error = %v", err)
	}
}

func TestMatrixItemsRequireJSONArray(t *testing.T) {
	for _, input := range []string{`{"cases":null}`, `{"cases":{"name":"one"}}`} {
		state := &store.RunState{Input: input, InputFormat: "json"}
		if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil || !strings.Contains(err.Error(), "JSON array") {
			t.Fatalf("input %s error = %v", input, err)
		}
	}
	state := &store.RunState{Nodes: map[string]*store.NodeState{"discover": {Output: `{"cases":null}`}}}
	if _, _, _, err := resolveMatrixItems("$discover.output.cases", state); err == nil || !strings.Contains(err.Error(), "JSON array") {
		t.Fatalf("node null error = %v", err)
	}
}

func TestMatrixItemsRejectMoreThanLimit(t *testing.T) {
	items := make([]int, maxMatrixItems+1)
	raw, err := json.Marshal(map[string]any{"cases": items})
	if err != nil {
		t.Fatal(err)
	}
	state := &store.RunState{Input: string(raw), InputFormat: "json"}
	if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil || !strings.Contains(err.Error(), "maximum is 1024") {
		t.Fatalf("limit error = %v", err)
	}
}

func TestMatrixItemsPreserveNumberPrecision(t *testing.T) {
	state := &store.RunState{Input: `{"cases":[9007199254740992,9007199254740993]}`, InputFormat: "json"}
	_, items, _, err := resolveMatrixItems("$INPUTS.cases", state)
	if err != nil {
		t.Fatal(err)
	}
	if string(items[0]) != "9007199254740992" || string(items[1]) != "9007199254740993" {
		t.Fatalf("items = %q", items)
	}
}

func TestMatrixItemsCanonicalizeLargeExponentWithoutFloat(t *testing.T) {
	state := &store.RunState{Input: `{"cases":[1e9223372036854775808,10e9223372036854775807]}`, InputFormat: "json"}
	if _, _, _, err := resolveMatrixItems("$INPUTS.cases", state); err == nil || !strings.Contains(err.Error(), "duplicate items") {
		t.Fatalf("large equivalent numbers error = %v", err)
	}
}

func TestPrepareMatrixRejectsChangedItemsOnResume(t *testing.T) {
	state := &store.RunState{Nodes: map[string]*store.NodeState{"cases": {
		MatrixFingerprint: "before",
		MatrixBranches:    []store.MatrixBranchState{{Index: 0, Item: json.RawMessage(`1`)}},
	}}}
	runner := &Runner{}
	err := runner.prepareMatrix(state, spec.Node{ID: "cases"}, state.Nodes["cases"], []json.RawMessage{json.RawMessage(`2`)}, "after")
	if err == nil || !strings.Contains(err.Error(), "matrix_items_changed") {
		t.Fatalf("resume error = %v", err)
	}
}

func TestMatrixPrimaryAssessmentRequiresArtifact(t *testing.T) {
	nodes := []spec.Node{{ID: "assess", Assessment: &spec.AssessmentSpec{Role: "primary"}}}
	if _, err := matrixPrimaryAssessment(nodes, map[string]store.NodeState{"assess": {Status: store.NodeSkipped}}); err == nil || !strings.Contains(err.Error(), "produced 0") {
		t.Fatalf("cardinality error = %v", err)
	}
}

func TestMatrixResumeDoesNotReplayCompletedBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.yaml")
	if err := os.WriteFile(path, []byte(`name: matrix-resume
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items: {type: string}
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: emit
          bash: printf '%s\n' '$MATRIX.item' >> order.txt; printf '%s' '$MATRIX.item'
      output_node: emit
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	base := store.FS{Workspace: dir}
	runner := New(wf, &spec.Config{}, path, "<config>", dir)
	runner.store = &failAfterMatrixBranchCommit{Repository: base}
	if _, err := runner.StartWithOptions(context.Background(), `{"cases":["alpha","beta"]}`, StartOptions{RunID: "run-matrix"}); err == nil {
		t.Fatal("expected simulated crash")
	}
	persisted, err := base.Load("run-matrix")
	if err != nil {
		t.Fatal(err)
	}
	matrix := persisted.Nodes["cases"]
	if len(matrix.MatrixBranches) != 2 || matrix.MatrixBranches[0].Status != store.NodeCompleted || matrix.MatrixBranches[1].Status != store.NodePending {
		t.Fatalf("persisted branches = %+v", matrix.MatrixBranches)
	}
	matrix.Status = store.NodePending
	matrix.Attempts--
	persisted.CurrentNode = ""
	persisted.Status = store.RunRunning
	if err := base.Commit(persisted, store.Event{Type: "run.recovered"}); err != nil {
		t.Fatal(err)
	}
	runner.store = base
	resumed, err := runner.Resume(context.Background(), persisted)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "order.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "alpha\nbeta\n" || resumed.Status != store.RunCompleted {
		t.Fatalf("order=%q state=%+v", raw, resumed)
	}
}

func TestMatrixApprovalResumesSameBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.yaml")
	if err := os.WriteFile(path, []byte(`name: matrix-approval
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items: {type: string}
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: approve
          approval:
            message: Approve $MATRIX.item
            capture_response: true
        - id: emit
          depends_on: [approve]
          bash: printf '%s:%s' '$MATRIX.item' $approve.output
      output_node: emit
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	runner := New(wf, &spec.Config{}, path, "<config>", dir)
	state, err := runner.StartWithOptions(context.Background(), `{"cases":["alpha"]}`, StartOptions{RunID: "run-matrix"})
	if !errors.Is(err, ErrWaiting) || state.Nodes["cases"].MatrixActiveIndex == nil || *state.Nodes["cases"].MatrixActiveIndex != 0 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
	approvalID := state.Waiting.NodeID
	state.Approvals[approvalID] = "yes"
	state.Nodes[approvalID].Status = store.NodePending
	state.Nodes[approvalID].Attempts = 0
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := runner.store.Commit(state, store.Event{Type: "approval.answered", NodeID: approvalID}); err != nil {
		t.Fatal(err)
	}
	resumed, err := runner.Resume(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Output != `["alpha:yes"]` || len(resumed.Nodes["cases"].MatrixBranches) != 1 {
		t.Fatalf("resumed = %+v", resumed)
	}
}

func TestMatrixBranchesKeepDistinctTypedArtifacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "matrix.yaml")
	if err := os.WriteFile(path, []byte(`name: matrix-artifacts
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items:
          type: object
          properties: {name: {type: string}}
          required: [name]
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: evidence
          bash: printf '%s' '$MATRIX.item.name'
          output_type: evaluation-evidence
      output_node: evidence
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, path, "<config>", dir).Start(context.Background(), `{"cases":[{"name":"alpha"},{"name":"beta"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Artifacts) != 2 || len(state.Nodes["cases"].Artifacts) != 2 {
		t.Fatalf("artifacts: Run=%+v matrix=%+v", state.Artifacts, state.Nodes["cases"].Artifacts)
	}
	first := state.Nodes["cases"].MatrixBranches[0].Nodes["cases__evidence"].Artifacts[0]
	second := state.Nodes["cases"].MatrixBranches[1].Nodes["cases__evidence"].Artifacts[0]
	if first.ID == second.ID || first.Path == second.Path {
		t.Fatalf("branch artifacts collide: first=%+v second=%+v", first, second)
	}
	for artifact, want := range map[store.ArtifactRef]string{first: "alpha", second: "beta"} {
		raw, err := os.ReadFile(artifact.Path)
		if err != nil || string(raw) != want {
			t.Fatalf("artifact %s = %q, err=%v", artifact.ID, raw, err)
		}
	}
}

func TestMatrixRecordsOnePrimaryAssessmentPerBranch(t *testing.T) {
	dir := t.TempDir()
	target := completedAssessmentTarget(t, store.FS{Workspace: dir}, dir, "run-target")
	path := filepath.Join(dir, "matrix.yaml")
	if err := os.WriteFile(path, []byte(`name: matrix-assessment
input:
  format: json
  schema:
    type: object
    properties:
      cases:
        type: array
        items:
          type: object
          properties: {case_id: {type: string}}
          required: [case_id]
    required: [cases]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: validate
          bash: printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'
        - id: evidence
          bash: printf '%s' '$MATRIX.item.case_id'
          output_type: evaluation-evidence
        - id: assess
          depends_on: [validate, evidence]
          assessment:
            role: primary
            target_run_id: run-target
            result_from: $validate.output
            scope: {case_id: $MATRIX.item.case_id, repeat: "1"}
            evidence: [$evidence.artifacts.evaluation-evidence]
      output_node: assess
`), 0o644); err != nil {
		t.Fatal(err)
	}
	wf, err := workflow.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	state, err := New(wf, &spec.Config{}, path, "<config>", dir).Start(context.Background(), `{"cases":[{"case_id":"a"},{"case_id":"b"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	branches := state.Nodes["cases"].MatrixBranches
	if state.Status != store.RunCompleted || len(branches) != 2 || branches[0].PrimaryAssessmentID == "" || branches[1].PrimaryAssessmentID == "" || branches[0].PrimaryAssessmentID == branches[1].PrimaryAssessmentID {
		t.Fatalf("target=%+v branches=%+v", target, branches)
	}
}

type failAfterMatrixBranchCommit struct {
	store.Repository
	failed bool
}

func (s *failAfterMatrixBranchCommit) Commit(state *store.RunState, event store.Event) error {
	if err := s.Repository.Commit(state, event); err != nil {
		return err
	}
	if event.Type == "matrix.branch.completed" && !s.failed {
		s.failed = true
		return errors.New("simulated crash after matrix branch commit")
	}
	return nil
}
