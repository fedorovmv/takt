package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/application"
	"takt/internal/store"
	"takt/internal/tooling"
)

func TestOrdinaryEvaluationRunUsesOneRootAndAuthoredChildren(t *testing.T) {
	workspace, workflow, config, cases := writeOrdinaryEvaluationFixture(t, true)
	result, err := (evaluationEngine{}).Flow(context.Background(), tooling.FlowEvaluationRequest{
		SuitePath: workflow, Target: "target.yaml", ConfigPath: config, CasesDir: cases,
		InvocationWorkspace: workspace, OutputDir: filepath.Join(workspace, ".takt", "evals", "ordinary"), Repeat: 2,
		Gates: map[string]tooling.FlowEvaluationGate{"valid_rate": {Min: float64Pointer(1)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := result.(*application.RunStatsResult)
	if !ok || stats.Status != store.RunCompleted || stats.Total != 4 || stats.Evaluated != 4 || !stats.GatesPassed {
		t.Fatalf("result=%#v", result)
	}
	fs := store.FS{Workspace: workspace}
	ids, err := fs.ListRunIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 5 {
		t.Fatalf("runs=%v", ids)
	}
	root, err := fs.Load(stats.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(root.ChildRunIDs) != 4 || len(root.Nodes["cases"].MatrixBranches) != 4 {
		t.Fatalf("root=%+v", root)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".takt", "evals", "ordinary", "report.json")); !os.IsNotExist(err) {
		t.Fatalf("ordinary evaluation wrote legacy report: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".takt", "evals", "ordinary", "progress.json")); !os.IsNotExist(err) {
		t.Fatalf("ordinary evaluation wrote legacy progress: %v", err)
	}
}

func TestOrdinaryEvaluationGateFailsAfterCompletedRunIsDurable(t *testing.T) {
	workspace, workflow, config, cases := writeOrdinaryEvaluationFixture(t, false)
	result, err := (evaluationEngine{}).Flow(context.Background(), tooling.FlowEvaluationRequest{
		SuitePath: workflow, Target: "target.yaml", ConfigPath: config, CasesDir: cases,
		InvocationWorkspace: workspace, OutputDir: filepath.Join(workspace, ".takt", "evals", "gate"), Repeat: 1,
		Gates: map[string]tooling.FlowEvaluationGate{"valid_rate": {Min: float64Pointer(1)}},
	})
	var gateFailure *application.AssessmentGateFailureError
	if !errors.As(err, &gateFailure) {
		t.Fatalf("error=%v", err)
	}
	stats, ok := result.(*application.RunStatsResult)
	if !ok || stats.Status != store.RunCompleted || stats.GatesPassed || stats.ValidRate.Numerator != 0 || stats.ValidRate.Denominator != 2 {
		t.Fatalf("result=%#v", result)
	}
	reloaded, loadErr := (store.FS{Workspace: workspace}).Load(stats.RunID)
	if loadErr != nil || reloaded.Status != store.RunCompleted || reloaded.ResultRevision == 0 {
		t.Fatalf("reloaded=%+v err=%v", reloaded, loadErr)
	}
}

func TestOrdinaryEvaluationPreflightFailureCreatesNoRun(t *testing.T) {
	workspace, workflow, config, cases := writeOrdinaryEvaluationFixture(t, true)
	if err := os.Remove(filepath.Join(cases, "b", "expected.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := (evaluationEngine{}).Flow(context.Background(), tooling.FlowEvaluationRequest{
		SuitePath: workflow, Target: "target.yaml", ConfigPath: config, CasesDir: cases,
		InvocationWorkspace: workspace, OutputDir: filepath.Join(workspace, ".takt", "evals", "broken"), Repeat: 2,
	})
	if err == nil || !strings.Contains(err.Error(), "expected.yaml") {
		t.Fatalf("error=%v", err)
	}
	ids, listErr := (store.FS{Workspace: workspace}).ListRunIDs()
	if listErr != nil || len(ids) != 0 {
		t.Fatalf("runs=%v err=%v", ids, listErr)
	}
}

func writeOrdinaryEvaluationFixture(t *testing.T, valid bool) (string, string, string, string) {
	t.Helper()
	workspace := t.TempDir()
	workflow := filepath.Join(workspace, "evaluate.yaml")
	config := filepath.Join(workspace, "config.yaml")
	cases := filepath.Join(workspace, "cases")
	validation := "false"
	if valid {
		validation = "true"
	}
	writeBootstrapFixture(t, workflow, `name: evaluate
input:
  format: json
  schema:
    type: object
    properties:
      cases: {type: array, items: {type: object}}
      gates: {type: object}
      identity: {type: object}
      protocol_version: {type: string}
      type: {type: string}
    required: [cases, gates, identity, protocol_version, type]
nodes:
  - id: cases
    matrix:
      items_from: $INPUTS.cases
      nodes:
        - id: candidate
          workflow:
            path: $MATRIX.item.workflow_path
            repository: $MATRIX.item.repository
            input: $MATRIX.item.input
            isolation: none
        - id: validate
          depends_on: [candidate]
          trigger_rule: all_done
          bash: |
            printf '%s' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":`+validation+`}'
        - id: evidence
          depends_on: [candidate, validate]
          trigger_rule: all_done
          bash: printf evidence
          output_type: evaluation-evidence
          output_mime: text/plain
        - id: assess
          depends_on: [validate, evidence]
          trigger_rule: all_done
          assessment:
            role: primary
            target_run_id: $candidate.child_run_id
            result_from: $validate.output
            scope: {case_id: $MATRIX.item.case_id, repeat: $MATRIX.item.repeat}
            evidence: [$evidence.artifacts.evaluation-evidence]
      output_node: assess
`)
	writeBootstrapFixture(t, config, "apiVersion: takt/v1alpha1\nkind: Config\n")
	for _, id := range []string{"b", "a"} {
		root := filepath.Join(cases, id)
		writeBootstrapFixture(t, filepath.Join(root, "input.md"), "input "+id)
		writeBootstrapFixture(t, filepath.Join(root, "expected.yaml"), "oracle: {expected: true}\n")
		writeBootstrapFixture(t, filepath.Join(root, "workspace", "target.yaml"), "name: target\nnodes:\n  - id: done\n    bash: printf done\n")
	}
	return workspace, workflow, config, cases
}

func writeBootstrapFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func float64Pointer(value float64) *float64 { return &value }
