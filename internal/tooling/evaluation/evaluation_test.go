package evaluation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/validation"
)

func TestRunCreatesIsolatedWorkspacesAndReport(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workflow := `name: evaluation-test
provider: fake
model: m
nodes:
  - id: implement
    prompt: |
      Process this input:
      $ARGUMENTS
`
	config := `apiVersion: takt/v1alpha1
kind: Config
models:
  m:
    provider: test
    id: model
assistants:
  fake:
    type: mock
`
	mustWrite(t, workflowPath, workflow, 0o644)
	mustWrite(t, configPath, config, 0o644)
	mustWrite(t, filepath.Join(casesDir, "a.md"), "first", 0o644)
	mustWrite(t, filepath.Join(casesDir, "b.md"), "second", 0o644)
	mustWrite(t, filepath.Join(templateDir, "marker.txt"), "template", 0o644)
	if err := os.MkdirAll(filepath.Join(templateDir, ".takt", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(templateDir, ".takt", "runs", "stale"), "stale", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir, Repeat: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 4 || report.Summary.ByStatus["completed"] != 4 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	for _, record := range report.Runs {
		if record.RunID == "" || record.Status != "completed" || record.Attempts != 1 {
			t.Fatalf("unexpected record: %+v", record)
		}
		if _, err := os.Stat(filepath.Join(record.Workspace, "marker.txt")); err != nil {
			t.Fatalf("template was not copied: %v", err)
		}
		if _, err := os.Stat(filepath.Join(record.Workspace, ".takt", "runs", "stale")); !os.IsNotExist(err) {
			t.Fatalf("template runtime state leaked into workspace: %v", err)
		}
	}
	loaded, err := LoadReport(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Summary.Total != report.Summary.Total {
		t.Fatalf("loaded report differs: %+v", loaded.Summary)
	}
}

func TestRunRejectsExistingWorkspaceWithoutReplace(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir, filepath.Join(outputDir, "workspaces", "case-001")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, "name: x\nnodes:\n  - id: done\n    bash: |\n      true\n", 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil {
		t.Fatal("expected existing workspace error")
	}
	if report == nil || len(report.Runs) != 1 || report.Runs[0].Status != "infrastructure_error" {
		t.Fatalf("unexpected partial report: %+v", report)
	}
}

func mustWrite(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsCaseIDCollisionsBeforeCreatingOutput(t *testing.T) {
	root := t.TempDir()
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(casesDir, "a b.md"), "first", 0o644)
	mustWrite(t, filepath.Join(casesDir, "a+b.md"), "second", 0o644)

	_, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: filepath.Join(root, "workflow.yaml"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
		CasesDir:     casesDir, WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), `case id collision "a-b"`) {
		t.Fatalf("expected normalized case id collision, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("output directory was created despite preflight failure: %v", statErr)
	}
}

func TestRunRejectsOverlappingWorkspaceTemplateAndOutput(t *testing.T) {
	root := t.TempDir()
	templateDir := filepath.Join(root, "template")
	casesDir := filepath.Join(root, "cases")
	for _, dir := range []string{templateDir, casesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)
	outputDir := filepath.Join(templateDir, "results")

	_, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: filepath.Join(root, "workflow.yaml"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
		CasesDir:     casesDir, WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("expected workspace/output overlap error, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("nested output directory was created despite preflight failure: %v", statErr)
	}
}

func TestRecordFromStatePreservesResumeAndDiagnostics(t *testing.T) {
	state := &store.RunState{
		ID: "run-1", Status: store.RunFailed,
		CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(102, 0),
		Nodes: map[string]*store.NodeState{
			"validate": {
				Status: store.NodeFailed, Attempts: 2, SessionID: "session-1", Resumed: true,
				ExitCode: 7, ErrorCode: "exit", Error: "validator failed",
				Feedback: "ROUTE_INVALID", Output: `{"valid":false}`,
			},
		},
	}
	record := recordFromState("case", 1, "/workspace", state)
	node := record.Nodes["validate"]
	if record.Resumed != 1 || !node.Resumed || node.Feedback != "ROUTE_INVALID" || node.Error != "validator failed" || node.DiagnosticOutput != `{"valid":false}` {
		t.Fatalf("diagnostic fields were not preserved: record=%+v node=%+v", record, node)
	}
}

func TestRunRejectsOutputNestedThroughWorkspaceTemplateSymlink(t *testing.T) {
	root := t.TempDir()
	realTemplate := filepath.Join(root, "real-template")
	linkedTemplate := filepath.Join(root, "template-link")
	casesDir := filepath.Join(root, "cases")
	for _, dir := range []string{realTemplate, casesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realTemplate, linkedTemplate); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)
	outputDir := filepath.Join(linkedTemplate, "results")
	_, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: filepath.Join(root, "workflow.yaml"), ConfigPath: filepath.Join(root, "config.yaml"),
		CasesDir: casesDir, WorkspaceTemplate: linkedTemplate, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("expected canonical overlap error, got %v", err)
	}
}

func TestRunRecordsStrategyModelsAndQualityMetrics(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: quality-test
provider: fake
model: m
nodes:
  - id: implement
    prompt: generate
  - id: quality
    depends_on: [implement]
    bash: |
      printf '%s\n' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":88,"checks":{"syntax":{"passed":true,"score":100}},"diagnostics":[{"code":"STYLE","severity":"warning","path":"route.yaml"}]}'
`, 0o644)
	mustWrite(t, configPath, `apiVersion: takt/v1alpha1
kind: Config
models:
  m:
    provider: test-provider
    id: test-model
    params:
      reasoning_effort: high
assistants:
  fake:
    type: mock
`, 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "first", 0o644)
	mustWrite(t, filepath.Join(casesDir, "two.md"), "second", 0o644)
	mustWrite(t, filepath.Join(templateDir, "route-validator"), "validator-v1", 0o755)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		StrategyID: "mock-quality-v1", BenchmarkID: "route-quality-10",
		QualityNode: "quality", GenerationNode: "implement",
		ValidatorID: "route-validator", ValidatorVersion: "1.2.3", ValidatorPath: "route-validator",
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ReportVersion != ReportVersion || report.TaktVersion == "" {
		t.Fatalf("report identity missing: %+v", report)
	}
	if report.Strategy.ID != "mock-quality-v1" || len(report.Strategy.Fingerprint) != 64 || len(report.Strategy.WorkflowFingerprint) != 64 {
		t.Fatalf("strategy identity missing: %+v", report.Strategy)
	}
	if report.Benchmark.ID != "route-quality-10" || report.Benchmark.CaseCount != 2 || len(report.Benchmark.DatasetFingerprint) != 64 || len(report.Benchmark.WorkspaceFingerprint) != 64 || len(report.Benchmark.Validator.Fingerprint) != 64 {
		t.Fatalf("benchmark identity missing: %+v", report.Benchmark)
	}
	if report.Summary.QualityRuns != 2 || report.Summary.Valid != 2 || floatValue(report.Summary.SuccessAt1) != 1 || floatValue(report.Summary.FinalSuccessRate) != 1 || floatValue(report.Summary.AverageScore) != 88 {
		t.Fatalf("quality summary mismatch: %+v", report.Summary)
	}
	if report.Summary.DiagnosticsByCode["STYLE"] != 2 || report.Summary.DiagnosticsBySeverity["warning"] != 2 {
		t.Fatalf("diagnostic summary mismatch: %+v", report.Summary)
	}
	for _, record := range report.Runs {
		if record.Quality == nil || !record.Quality.Valid || record.AttemptsToValid == nil || *record.AttemptsToValid != 1 || !record.ValidAtFirstAttempt {
			t.Fatalf("quality record mismatch: %+v", record)
		}
		node := record.Nodes["implement"]
		if node.Assistant != "fake" || node.RequestedModel == nil || node.RequestedModel.Provider != "test-provider" || node.RequestedModel.ID != "test-model" {
			t.Fatalf("model identity missing: %+v", node)
		}
	}
}

func TestRunRejectsMalformedQualityResult(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: malformed-quality
nodes:
  - id: implement
    bash: |
      true
  - id: quality
    depends_on: [implement]
    bash: |
      echo '{"valid":true}'
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported validation protocol_version") {
		t.Fatalf("expected quality contract error, got report=%+v err=%v", report, err)
	}
	if report == nil || len(report.Runs) != 1 || report.Runs[0].Status != "evaluation_error" || report.Runs[0].ErrorCode != "quality_contract" {
		t.Fatalf("quality error not preserved: %+v", report)
	}
}

func TestFinishReportUsesTotalCostPerValidResult(t *testing.T) {
	score := 80.0
	report := &SuiteReport{
		StartedAt: time.Now().Add(-time.Second),
		Summary:   newSummary(),
		Runs: []RunRecord{
			{Cost: 2, DurationMS: 100, AttemptsToValid: intPointer(1), QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true, Score: &score}},
			{Cost: 3, DurationMS: 200, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: false}},
		},
	}
	for _, record := range report.Runs {
		addSummary(&report.Summary, record)
	}
	finishReport(report)
	if report.Summary.Valid != 1 || floatValue(report.Summary.CostPerValid) != 5 || floatValue(report.Summary.AmortizedEndToEndMSPerValid) != 300 {
		t.Fatalf("failed runs were not included in cost/time per valid: %+v", report.Summary)
	}
}

func TestEvaluationReportSchemaMatchesVersion(t *testing.T) {
	path := filepath.Join("..", "..", "..", "schemas", "evaluation-report.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	if properties["report_version"].(map[string]any)["const"] != ReportVersion {
		t.Fatal("evaluation report schema version differs from runtime")
	}
	defs := schema["$defs"].(map[string]any)
	node := defs["node"].(map[string]any)["properties"].(map[string]any)
	if _, ok := node["assistant_version"]; !ok {
		t.Fatal("evaluation report schema misses assistant_version")
	}
	if _, ok := node["executions"]; !ok {
		t.Fatal("evaluation report schema misses per-execution records")
	}
	if _, ok := defs["execution"]; !ok {
		t.Fatal("evaluation report schema misses execution definition")
	}
	summary := defs["summary"].(map[string]any)["properties"].(map[string]any)
	if _, ok := summary["by_assistant_version"]; !ok {
		t.Fatal("evaluation report schema misses by_assistant_version")
	}
	if _, ok := summary["usage_by_execution_identity"]; !ok {
		t.Fatal("evaluation report schema misses usage_by_execution_identity")
	}
	if _, ok := summary["amortized_end_to_end_ms_per_valid"]; !ok {
		t.Fatal("evaluation report schema misses renamed amortized duration metric")
	}
	if _, ok := summary["duration_per_valid_ms"]; ok {
		t.Fatal("evaluation report schema still exposes ambiguous duration_per_valid_ms")
	}
	run := defs["run"].(map[string]any)["properties"].(map[string]any)
	if _, ok := run["quality_node_status"]; !ok {
		t.Fatal("evaluation report schema misses quality_node_status")
	}
	benchmark := defs["benchmark"].(map[string]any)["properties"].(map[string]any)
	if _, ok := benchmark["workspace_fingerprint"]; !ok {
		t.Fatal("evaluation report schema misses workspace_fingerprint")
	}
}

func TestMissingQualityAfterRunFailureCountsAsInvalid(t *testing.T) {
	record := RunRecord{
		Status: "failed", Cost: 1, DurationMS: 50, QualityExpected: true,
		QualityError: `quality node "quality" did not run: status=skipped`,
		Nodes:        map[string]NodeRecord{},
	}
	report := &SuiteReport{StartedAt: time.Now().Add(-time.Second), Summary: newSummary(), Runs: []RunRecord{record}}
	addSummary(&report.Summary, record)
	finishReport(report)
	if report.Summary.QualityRuns != 1 || report.Summary.Valid != 0 || report.Summary.Invalid != 1 || floatValue(report.Summary.FinalSuccessRate) != 0 {
		t.Fatalf("missing quality result was not counted as invalid: %+v", report.Summary)
	}
}

func floatValue(value *float64) float64 {
	if value == nil {
		return -1
	}
	return *value
}

func TestRunCountsSkippedQualityAfterGenerationFailureAsInvalid(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: failed-generation
nodes:
  - id: implement
    bash: |
      exit 7
  - id: quality
    depends_on: [implement]
    bash: |
      printf '%s\n' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true}'
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err != nil {
		t.Fatalf("a domain run failure must remain a benchmark result, got %v", err)
	}
	if len(report.Runs) != 1 {
		t.Fatalf("unexpected runs: %+v", report.Runs)
	}
	run := report.Runs[0]
	if run.Status != store.RunFailed || run.Quality != nil || !strings.Contains(run.QualityError, "status=skipped") {
		t.Fatalf("failed generation was not represented as invalid quality: %+v", run)
	}
	if report.Summary.QualityRuns != 1 || report.Summary.Invalid != 1 || report.Summary.Valid != 0 {
		t.Fatalf("unexpected quality summary: %+v", report.Summary)
	}
}

func TestWorkspaceFingerprintTracksCopiedContext(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "context.md"), "version one", 0o644)
	first, err := hashWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "context.md"), "version two", 0o644)
	second, err := hashWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("workspace content change did not change fingerprint")
	}
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "bin", "generated"), "ignored", 0o755)
	third, err := hashWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if second != third {
		t.Fatal("generated bin directory changed workspace fingerprint")
	}
	if err := os.Chmod(filepath.Join(root, "context.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	fourth, err := hashWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if third == fourth {
		t.Fatal("workspace file mode change did not change fingerprint")
	}
}

func TestZeroQualityMetricsRemainExplicitInJSON(t *testing.T) {
	record := RunRecord{
		Status: "failed", QualityExpected: true, Nodes: map[string]NodeRecord{},
	}
	report := &SuiteReport{StartedAt: time.Now().Add(-time.Second), Summary: newSummary(), Runs: []RunRecord{record}}
	addSummary(&report.Summary, record)
	finishReport(report)

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	summary := decoded["summary"].(map[string]any)
	for _, key := range []string{"success_at_1", "final_success_rate", "average_score", "cost_per_valid", "amortized_end_to_end_ms_per_valid"} {
		if _, ok := summary[key]; !ok {
			t.Fatalf("summary key %q disappeared from JSON: %s", key, data)
		}
	}
	if summary["success_at_1"] != float64(0) || summary["final_success_rate"] != float64(0) {
		t.Fatalf("zero success metrics were not serialized explicitly: %+v", summary)
	}
	if summary["average_score"] != nil || summary["cost_per_valid"] != nil || summary["amortized_end_to_end_ms_per_valid"] != nil {
		t.Fatalf("unavailable metrics must be null, not zero: %+v", summary)
	}
}

func TestFailedQualityNodeCannotContributeValidResult(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: failed-quality
nodes:
  - id: implement
    bash: |
      true
  - id: quality
    depends_on: [implement]
    bash: |
      exec /bin/sh -c 'printf "%s\n" "$1"; exit "$2"' sh '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":100}' 7
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err != nil {
		t.Fatalf("failed quality execution must remain a benchmark result, got %v", err)
	}
	run := report.Runs[0]
	if run.Quality == nil || !run.Quality.Valid || run.QualityNodeStatus != string(store.NodeFailed) || !strings.Contains(run.QualityError, "did not complete: status=failed") {
		t.Fatalf("failed quality envelope was not preserved: %+v", run)
	}
	if run.AttemptsToValid == nil || *run.AttemptsToValid != 0 || run.ValidAtFirstAttempt {
		t.Fatalf("failed quality node was treated as valid: %+v", run)
	}
	if report.Summary.Valid != 0 || report.Summary.Invalid != 1 || report.Summary.ScoredRuns != 1 || floatValue(report.Summary.AverageScore) != 100 || floatValue(report.Summary.FinalSuccessRate) != 0 {
		t.Fatalf("failed quality node changed success metrics or lost score: %+v", report.Summary)
	}
}

func TestFailedQualityNodePreservesInvalidEnvelopeDiagnostics(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: invalid-quality
nodes:
  - id: implement
    bash: |
      true
  - id: quality
    depends_on: [implement]
    bash: |
      exec /bin/sh -c 'printf "%s\n" "$1"; exit "$2"' sh '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"score":42,"diagnostics":[{"code":"ROUTE_INVALID","severity":"error","path":"route.yaml","message":"invalid route"}]}' 1
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err != nil {
		t.Fatalf("invalid validation result is a benchmark outcome, got %v", err)
	}
	run := report.Runs[0]
	if run.Quality == nil || run.Quality.Valid || run.Quality.Score == nil || *run.Quality.Score != 42 || len(run.Quality.Diagnostics) != 1 {
		t.Fatalf("invalid quality envelope was not preserved: %+v", run)
	}
	if run.QualityNodeStatus != string(store.NodeFailed) || !strings.Contains(run.QualityError, "status=failed") {
		t.Fatalf("quality execution status was not preserved: %+v", run)
	}
	if report.Summary.Valid != 0 || report.Summary.Invalid != 1 || report.Summary.ScoredRuns != 1 || floatValue(report.Summary.AverageScore) != 42 {
		t.Fatalf("invalid quality score was not aggregated: %+v", report.Summary)
	}
	if report.Summary.DiagnosticsByCode["ROUTE_INVALID"] != 1 || report.Summary.DiagnosticsBySeverity["error"] != 1 {
		t.Fatalf("invalid quality diagnostics were not aggregated: %+v", report.Summary)
	}
}

func TestFailedQualityNodeWithMalformedEnvelopeIsContractError(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: malformed-failed-quality
nodes:
  - id: implement
    bash: |
      true
  - id: quality
    depends_on: [implement]
    bash: |
      exec /bin/sh -c 'printf "%s\n" "$1"; exit "$2"' sh '{"valid":false}' 1
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported validation protocol_version") {
		t.Fatalf("expected failed malformed quality contract error, got report=%+v err=%v", report, err)
	}
	if report == nil || len(report.Runs) != 1 || report.Runs[0].Status != "evaluation_error" || report.Runs[0].ErrorCode != "quality_contract" {
		t.Fatalf("failed malformed quality error not preserved: %+v", report)
	}
}

func TestBenchmarkFingerprintIncludesValidatorIDAndVersion(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, "name: fingerprint\nnodes:\n  - id: done\n    bash: |\n      true\n", 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	run := func(id, version, output string) *SuiteReport {
		report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
			WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
			WorkspaceTemplate: templateDir, OutputDir: filepath.Join(root, output),
			ValidatorID: id, ValidatorVersion: version,
		})
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	first := run("route-validator", "1.0.0", "first")
	second := run("route-validator", "2.0.0", "second")
	third := run("other-validator", "1.0.0", "third")
	if first.Benchmark.Fingerprint == second.Benchmark.Fingerprint {
		t.Fatal("validator version did not change benchmark fingerprint")
	}
	if first.Benchmark.Fingerprint == third.Benchmark.Fingerprint {
		t.Fatal("validator ID did not change benchmark fingerprint")
	}
}

func TestUsageIsAttributedPerExecutionIdentity(t *testing.T) {
	state := &store.RunState{
		ID: "run", Status: store.RunCompleted,
		CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(101, 0),
		Nodes: map[string]*store.NodeState{
			"generate": {
				Status: store.NodeCompleted, Attempts: 2,
				Assistant: "pi", AssistantVersion: "0.83.0",
				ResolvedModel: &store.ModelRef{Provider: "router", ID: "model-b"},
				Usage:         &store.Usage{InputTokens: 30, OutputTokens: 3, Cost: 0.3},
				Executions: []store.ExecutionState{
					{Attempt: 1, Status: store.NodeCompleted, Assistant: "pi", AssistantVersion: "0.83.0", ResolvedModel: &store.ModelRef{Provider: "router", ID: "model-a"}, Usage: &store.Usage{InputTokens: 10, OutputTokens: 1, Cost: 0.1}},
					{Attempt: 2, Status: store.NodeCompleted, Assistant: "pi", AssistantVersion: "0.84.0", ResolvedModel: &store.ModelRef{Provider: "router", ID: "model-b"}, Usage: &store.Usage{InputTokens: 20, OutputTokens: 2, Cost: 0.2}},
				},
			},
		},
	}
	record := recordFromState("case", 1, "/workspace", state)
	if record.MixedIdentityNodes != 1 || !record.Nodes["generate"].MixedIdentity || len(record.Nodes["generate"].Executions) != 2 {
		t.Fatalf("mixed execution identities were not preserved: %+v", record)
	}
	summary := newSummary()
	addSummary(&summary, record)
	if summary.MixedExecutionIdentityNodes != 1 || len(summary.UsageByExecutionIdentity) != 2 {
		t.Fatalf("usage identities were merged: %+v", summary)
	}
	var total UsageBreakdown
	for _, usage := range summary.UsageByExecutionIdentity {
		total.Executions += usage.Executions
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.Cost += usage.Cost
	}
	if total.Executions != 2 || total.InputTokens != 30 || total.OutputTokens != 3 || math.Abs(total.Cost-0.3) > 1e-9 {
		t.Fatalf("per-identity usage does not match aggregate usage: %+v", summary.UsageByExecutionIdentity)
	}
}

func TestFailedQualityNodeDecodesStdoutAndKeepsStderrDiagnostic(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: stderr-quality
nodes:
  - id: implement
    bash: |
      true
  - id: quality
    depends_on: [implement]
    bash: |
      exec /bin/sh -c 'printf "%s\n" "$1"; printf "%s\n" "validator cache is cold" >&2; exit 1' sh '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"score":37,"diagnostics":[{"code":"ROUTE_WARNING","severity":"warning","path":"route.yaml","message":"route is incomplete"}]}'
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory,
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
		QualityNode: "quality", GenerationNode: "implement",
	})
	if err != nil {
		t.Fatalf("stderr must not corrupt a valid stdout envelope: %v", err)
	}
	run := report.Runs[0]
	if run.Quality == nil || run.Quality.Valid || run.Quality.Score == nil || *run.Quality.Score != 37 {
		t.Fatalf("stdout validation envelope was not decoded: %+v", run)
	}
	node := run.Nodes["quality"]
	if !strings.Contains(node.Stdout, `"protocol_version":"takt-validation/v1alpha1"`) {
		t.Fatalf("quality stdout was not preserved: %+v", node)
	}
	if strings.Contains(node.Stdout, "validator cache is cold") {
		t.Fatalf("stderr leaked into stdout: %+v", node)
	}
	if !strings.Contains(node.Stderr, "validator cache is cold") {
		t.Fatalf("quality stderr was not preserved: %+v", node)
	}
	if !strings.Contains(node.DiagnosticOutput, "ROUTE_WARNING") || !strings.Contains(node.DiagnosticOutput, "validator cache is cold") {
		t.Fatalf("combined diagnostic output lost one of the streams: %+v", node)
	}
	if report.Summary.DiagnosticsByCode["ROUTE_WARNING"] != 1 || report.Summary.DiagnosticsBySeverity["warning"] != 1 {
		t.Fatalf("quality diagnostics were not aggregated: %+v", report.Summary)
	}
	if report.Summary.Valid != 0 || report.Summary.Invalid != 1 || floatValue(report.Summary.AverageScore) != 37 {
		t.Fatalf("invalid quality outcome was aggregated incorrectly: %+v", report.Summary)
	}
}

func TestRunCapturesTimeToValidRetriesAndDiagnosticFingerprints(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, `name: metrics
nodes:
  - id: implement
    bash: |
      if [ ! -f attempt ]; then touch attempt; echo first >&2; exit 7; fi
      echo ok
    attempts:
      max: 2
      retry_on: [exit]
      backoff:
        initial: 1ms
        multiplier: 1
        max: 1ms
  - id: quality
    depends_on: [implement]
    bash: |
      printf '%s\n' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":100,"checks":{},"diagnostics":[]}'
`, 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)
	report, err := Run(context.Background(), RunOptions{ExecutionFactory: testExecutionFactory, WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir, WorkspaceTemplate: templateDir, OutputDir: outputDir, QualityNode: "quality", GenerationNode: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || report.Runs[0].TimeToValidMS == nil {
		t.Fatalf("time-to-valid missing: %+v", report.Runs)
	}
	if report.Runs[0].RetryScheduled != 1 || report.Summary.RetryScheduled != 1 {
		t.Fatalf("retry metrics = run:%d summary:%d", report.Runs[0].RetryScheduled, report.Summary.RetryScheduled)
	}
	if len(report.Runs[0].RetryFingerprints) != 1 {
		t.Fatalf("retry fingerprints = %#v", report.Runs[0].RetryFingerprints)
	}
	if report.Summary.FailedExecutions != 1 {
		t.Fatalf("failed executions = %d", report.Summary.FailedExecutions)
	}
	if len(report.Summary.DiagnosticsByFingerprint) == 0 {
		t.Fatalf("diagnostic fingerprints missing: %+v", report.Summary)
	}
	if report.Summary.AverageTimeToValidMS == nil {
		t.Fatal("average time-to-valid missing")
	}
}

func TestComparePairsRunsAndReportsTransitions(t *testing.T) {
	fp := strings.Repeat("a", 64)
	one := int64(100)
	two := int64(80)
	baseline := &SuiteReport{Strategy: StrategyIdentity{ID: "base"}, Benchmark: BenchmarkIdentity{Fingerprint: fp}, Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "a", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: false}, Labels: map[string]string{"category": "http"}},
		{CaseID: "b", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}, TimeToValidMS: &one, Labels: map[string]string{"category": "branch"}},
	}}
	candidate := &SuiteReport{Strategy: StrategyIdentity{ID: "candidate"}, Benchmark: BenchmarkIdentity{Fingerprint: fp}, Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "a", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}, TimeToValidMS: &two, Labels: map[string]string{"category": "http"}},
		{CaseID: "b", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}, TimeToValidMS: &two, Labels: map[string]string{"category": "branch"}},
	}}
	for _, r := range baseline.Runs {
		addSummary(&baseline.Summary, r)
	}
	finishReport(baseline)
	for _, r := range candidate.Runs {
		addSummary(&candidate.Summary, r)
	}
	finishReport(candidate)
	comparison, err := Compare(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Outcomes.CandidateOnlyValid != 1 || comparison.Outcomes.BothValid != 1 {
		t.Fatalf("outcomes = %+v", comparison.Outcomes)
	}
	if len(comparison.ByCategory) != 2 {
		t.Fatalf("category breakdown = %+v", comparison.ByCategory)
	}
	if comparison.Metrics.FinalSuccessRate.DeltaPP == nil || *comparison.Metrics.FinalSuccessRate.DeltaPP != 50 {
		t.Fatalf("delta pp = %+v", comparison.Metrics.FinalSuccessRate)
	}
}

func TestRunMatrixExecutesStrategiesAndGates(t *testing.T) {
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	template := filepath.Join(root, "workspace")
	if err := os.MkdirAll(cases, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(template, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cases, "one.md"), "case", 0o644)
	mustWrite(t, filepath.Join(cases, "cases.yaml"), `apiVersion: takt/evaluation/v1alpha1
kind: CaseManifest
cases:
  one:
    category: smoke
`, 0o644)
	mustWrite(t, filepath.Join(root, "config.yaml"), "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	workflow := func(valid bool) string {
		return fmt.Sprintf(`name: strategy
nodes:
  - id: implement
    bash: "true"
  - id: quality
    depends_on: [implement]
    bash: |
      printf '%%s\n' '{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":%t,"checks":{},"diagnostics":[]}'
`, valid)
	}
	mustWrite(t, filepath.Join(root, "base.yaml"), workflow(false), 0o644)
	mustWrite(t, filepath.Join(root, "candidate.yaml"), workflow(true), 0o644)
	mustWrite(t, filepath.Join(root, "matrix.yaml"), `apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: smoke
benchmark:
  id: smoke-benchmark
  baseline_strategy: baseline
  cases: cases
  case_manifest: cases/cases.yaml
  workspace_template: workspace
  repeat: 2
  quality_node: quality
  generation_node: implement
strategies:
  - id: baseline
    workflow: base.yaml
    config: config.yaml
  - id: candidate
    workflow: candidate.yaml
    config: config.yaml
gates:
  - strategy: candidate
    final_success_rate_min: 1
    unstable_cases_max: 0
`, 0o644)
	report, err := RunMatrix(context.Background(), MatrixRunOptions{ExecutionFactory: testExecutionFactory, MatrixPath: filepath.Join(root, "matrix.yaml"), OutputDir: filepath.Join(root, "results")})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Strategies) != 2 || len(report.Comparisons) != 1 {
		t.Fatalf("matrix report = %+v", report)
	}
	if report.Comparisons[0].Outcomes.CandidateOnlyValid != 2 {
		t.Fatalf("comparison = %+v", report.Comparisons[0])
	}
	if report.ExperimentFingerprint == "" {
		t.Fatal("experiment fingerprint missing")
	}
	if _, err := os.Stat(filepath.Join(root, "results", "benchmark.json")); err != nil {
		t.Fatal(err)
	}
}

func TestExperimentFingerprintIgnoresMatrixFileLocation(t *testing.T) {
	base := MatrixReport{BenchmarkID: "bench", BaselineStrategy: "base", Repeat: 3, MatrixFingerprint: strings.Repeat("a", 64), Strategies: []MatrixStrategyResult{
		{ID: "base", Strategy: StrategyIdentity{Fingerprint: strings.Repeat("b", 64)}, Benchmark: BenchmarkIdentity{Fingerprint: strings.Repeat("c", 64)}},
		{ID: "candidate", Strategy: StrategyIdentity{Fingerprint: strings.Repeat("d", 64)}, Benchmark: BenchmarkIdentity{Fingerprint: strings.Repeat("c", 64)}},
	}}
	other := base
	other.MatrixFingerprint = strings.Repeat("e", 64)
	left, err := experimentFingerprint(&base)
	if err != nil {
		t.Fatal(err)
	}
	right, err := experimentFingerprint(&other)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("experiment identity depends on matrix file bytes: %s != %s", left, right)
	}
	other.Repeat = 4
	changed, err := experimentFingerprint(&other)
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatal("experiment identity ignored repeat")
	}
}

func TestLoadMatrixRequiresExplicitBaselineAndNonNegativeRegression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	base := `apiVersion: takt/evaluation/v1alpha1
kind: EvaluationMatrix
metadata:
  name: strict
benchmark:
  id: strict-v1
  cases: cases
  workspace_template: workspace
  quality_node: validate
  generation_node: implement
strategies:
  - id: baseline
    workflow: baseline.yaml
    config: config.yaml
  - id: candidate
    workflow: candidate.yaml
    config: config.yaml
`
	if _, _, _, err := LoadMatrix(write("missing-baseline.yaml", base)); err == nil || !strings.Contains(err.Error(), "baseline_strategy") {
		t.Fatalf("expected missing baseline error, got %v", err)
	}
	withBaseline := strings.Replace(base, "  id: strict-v1\n", "  id: strict-v1\n  baseline_strategy: baseline\n", 1) + `gates:
  - strategy: candidate
    cost_per_valid_max_regression_percent: -1
`
	if _, _, _, err := LoadMatrix(write("negative-regression.yaml", withBaseline)); err == nil || !strings.Contains(err.Error(), "must be >= 0") {
		t.Fatalf("expected negative regression error, got %v", err)
	}
}

func TestMatrixRejectsExplicitZeroRepeat(t *testing.T) {
	zero := 0
	matrix := Matrix{APIVersion: MatrixAPIVersion, Kind: MatrixKind, Metadata: MatrixMetadata{Name: "x"}, Benchmark: MatrixBenchmark{ID: "x", BaselineStrategy: "a", Cases: "cases", WorkspaceTemplate: "workspace", QualityNode: "quality", GenerationNode: "generation", Repeat: &zero}, Strategies: []MatrixStrategy{{ID: "a", Workflow: "a.yaml", Config: "a-config.yaml"}, {ID: "b", Workflow: "b.yaml", Config: "b-config.yaml"}}}
	if err := validateMatrix(&matrix); err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Fatalf("expected repeat validation error, got %v", err)
	}
}

func TestFinishReportClassifiesStableAndUnstableCases(t *testing.T) {
	report := &SuiteReport{StartedAt: time.Now().UTC(), Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "stable-valid", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}},
		{CaseID: "stable-valid", Repeat: 2, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}},
		{CaseID: "stable-invalid", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: false}},
		{CaseID: "stable-invalid", Repeat: 2, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: false}},
		{CaseID: "unstable", Repeat: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}},
		{CaseID: "unstable", Repeat: 2, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: false}},
	}}
	for _, run := range report.Runs {
		addSummary(&report.Summary, run)
	}
	finishReport(report)
	if report.Summary.StableValidCases != 1 || report.Summary.StableInvalidCases != 1 || report.Summary.UnstableCases != 1 {
		t.Fatalf("stability summary = %+v", report.Summary)
	}
}

func TestAddSummaryCountsFailedExecutionCost(t *testing.T) {
	summary := newSummary()
	addSummary(&summary, RunRecord{Status: "completed", Nodes: map[string]NodeRecord{
		"implement": {Executions: []ExecutionRecord{
			{Status: "failed", Usage: &store.Usage{Cost: 1.25}},
			{Status: "completed", Usage: &store.Usage{Cost: 0.75}},
		}},
	}})
	if summary.FailedExecutions != 1 || math.Abs(summary.FailedExecutionCost-1.25) > 1e-9 {
		t.Fatalf("failed execution summary = %+v", summary)
	}
}

func TestApplyRuntimeMetricsUsesDurableEventTimesAndImmediateRetryFingerprint(t *testing.T) {
	workspace := t.TempDir()
	repository := store.FS{Workspace: workspace}
	created := time.Date(2026, 8, 8, 9, 0, 0, 0, time.UTC)
	state := &store.RunState{ID: "run-metrics", CreatedAt: created, Status: store.RunCompleted, Nodes: map[string]*store.NodeState{}}
	if err := repository.Commit(state, store.Event{Type: "node.retry", NodeID: "implement", Time: created.Add(250 * time.Millisecond), Data: map[string]any{"fingerprint": strings.Repeat("a", 64)}}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Commit(state, store.Event{Type: "node.completed", NodeID: "quality", Time: created.Add(1500 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	record := RunRecord{QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true}}
	applyRuntimeMetrics(&record, state, repository, "quality")
	if record.TimeToValidMS == nil || *record.TimeToValidMS != 1500 {
		t.Fatalf("time-to-valid = %v", record.TimeToValidMS)
	}
	if record.RetryScheduled != 1 || len(record.RetryFingerprints) != 1 || record.RetryFingerprints[0] != strings.Repeat("a", 64) {
		t.Fatalf("retry metrics = %+v", record)
	}
}

func TestProviderRetryMetricsRetainDiagnosticsWithoutWorkflowAttemptInflation(t *testing.T) {
	state := &store.RunState{ID: "provider-metrics", Nodes: map[string]*store.NodeState{"implement": {Attempts: 1, ProviderAttempts: 3}}}
	record := RunRecord{Attempts: 1}
	applyRuntimeMetricsFromEvents(&record, state, []store.Event{{Type: "provider.retry.scheduled", NodeID: "implement", Data: map[string]any{"fingerprint": "provider-fingerprint"}}}, "")
	if record.RetryScheduled != 1 || !reflect.DeepEqual(record.RetryFingerprints, []string{"provider-fingerprint"}) {
		t.Fatalf("provider retry diagnostics=%+v", record)
	}
	if record.Attempts != 1 || record.AttemptsToValid != nil {
		t.Fatalf("provider retries inflated workflow metrics=%+v", record)
	}
}

func TestEvaluateGateReturnsFailureWithoutLosingReportSemantics(t *testing.T) {
	min := 0.9
	gate := MatrixGate{Strategy: "candidate", FinalSuccessRateMin: &min}
	baseline := &SuiteReport{Summary: newSummary()}
	candidate := &SuiteReport{Summary: newSummary()}
	value := 0.5
	candidate.Summary.FinalSuccessRate = &value
	results := evaluateGate(gate, baseline, candidate)
	if len(results) != 1 || results[0].Passed {
		t.Fatalf("gate results = %+v", results)
	}
}

func TestTaskMatrixSummaryComparisonAndGate(t *testing.T) {
	runs := []TaskRunRecord{
		{CaseID: "ordinary", Repeat: 1, RouteCorrect: true, FinalSuccess: true, PlanRevisions: 1},
		{CaseID: "dynamic", Repeat: 1, RouteCorrect: true, FinalSuccess: true, PlanRevisions: 2, ReplannerRuns: 1, ReplanExpected: true, ReplanExpectation: true},
		{CaseID: "allowed-input", Repeat: 1, RouteCorrect: true, NeedsInput: true, NeedsInputAllowed: true, PlanRevisions: 1},
	}
	summary := summarizeTaskRuns(runs)
	if summary.RouteAccuracy != 1 || math.Abs(summary.FinalSuccessRate-(2.0/3.0)) > 1e-9 || summary.ReplanExpectationRate != 1 || math.Abs(summary.AveragePlanRevisions-(4.0/3.0)) > 1e-9 || summary.UnexpectedNeedsInput != 0 {
		t.Fatalf("task summary = %+v", summary)
	}
	baseline := TaskStrategyResult{ID: "baseline", Runs: []TaskRunRecord{{CaseID: "ordinary", Repeat: 1, RouteCorrect: true, FinalSuccess: true}, {CaseID: "dynamic", Repeat: 1, RouteCorrect: false, FinalSuccess: true}}}
	candidate := TaskStrategyResult{ID: "candidate", Runs: runs}
	comparison := compareTaskStrategies(baseline, candidate)
	if comparison.CandidateOnlyRouteCorrect != 1 || comparison.BothRouteCorrect != 1 || comparison.BothSuccess != 2 {
		t.Fatalf("task comparison = %+v", comparison)
	}
	min := 1.0
	gate := TaskMatrixGate{Strategy: "candidate", RouteAccuracyMin: &min, ReplanExpectationRateMin: &min}
	for _, result := range evaluateTaskGate(gate, summary) {
		if !result.Passed {
			t.Fatalf("unexpected task gate failure: %+v", result)
		}
	}
}

func TestTaskMatrixRejectsExplicitZeroRepeat(t *testing.T) {
	zero := 0
	matrix := TaskMatrix{APIVersion: MatrixAPIVersion, Kind: TaskMatrixKind, Metadata: MatrixMetadata{Name: "x"}, Benchmark: TaskMatrixBenchmark{ID: "x", BaselineStrategy: "a", Cases: "cases.yaml", Repeat: &zero}, Strategies: []TaskMatrixStrategy{{ID: "a", WorkspaceTemplate: "a"}, {ID: "b", WorkspaceTemplate: "b"}}}
	if err := validateTaskMatrix(&matrix); err == nil || !strings.Contains(err.Error(), "repeat") {
		t.Fatalf("expected repeat validation error, got %v", err)
	}
}

func TestCommitEvaluationStateRedactsApprovalAndPriorOutputs(t *testing.T) {
	workspace := t.TempDir()
	const envName = "TAKT_EVAL_PLAIN_VALUE"
	const secret = "evaluation-secret-47"
	t.Setenv(envName, secret)
	cfg := &spec.Config{Assistants: map[string]spec.AssistantSpec{
		"worker": {Env: map[string]string{"TOKEN": "secret://" + envName}},
	}}
	state := &store.RunState{
		ID: "run-eval-redaction", Status: store.RunRunning, ConfigPath: "override-config.yaml",
		Nodes:     map[string]*store.NodeState{"approve": {Status: store.NodePending, Output: secret, Stdout: secret, Stderr: secret}},
		Approvals: map[string]string{"approve": secret},
	}
	repo := store.FS{Workspace: workspace}
	if err := commitEvaluationState(repo, state, store.Event{Type: "approval.answered", Data: map[string]any{"answer": secret}}, cfg); err != nil {
		t.Fatal(err)
	}
	persisted, err := repo.Load(state.ID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := repo.ReadEvents(state.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(struct {
		State  *store.RunState
		Events []store.Event
	}{persisted, events})
	if strings.Contains(string(raw), secret) {
		t.Fatalf("evaluation approval commit leaked secret: %s", raw)
	}
}

func TestTaskSummaryCountsRepeatPairsIndependently(t *testing.T) {
	runs := []TaskRunRecord{
		{CaseID: "same", Repeat: 1, RouteCorrect: true, FinalSuccess: true, PlanRevisions: 1},
		{CaseID: "same", Repeat: 2, RouteCorrect: false, FinalSuccess: true, PlanRevisions: 1},
	}
	summary := summarizeTaskRuns(runs)
	if summary.Total != 2 || math.Abs(summary.RouteAccuracy-0.5) > 1e-9 || summary.FinalSuccessRate != 1 {
		t.Fatalf("repeat aggregation = %+v", summary)
	}
}

func TestTaskWorkspaceFingerprintMatchesCopiedContentBoundary(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".git/objects", ".takt/runs/r1", ".takt/plans/p1", ".takt/locks", ".takt/host-sessions", ".takt/notifications"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "ignored.txt"), []byte(dir), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := hashTaskWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".takt", "runs", "r1", "ignored.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterIgnored, err := hashTaskWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterIgnored != before {
		t.Fatalf("runtime-only directory changed fingerprint: before=%s after=%s", before, afterIgnored)
	}
	copyDir := filepath.Join(t.TempDir(), "copy")
	if err := copyTaskTree(root, copyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(copyDir, ".git")); !os.IsNotExist(err) {
		t.Fatalf(".git copied into task workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(copyDir, ".takt", "runs")); !os.IsNotExist(err) {
		t.Fatalf("runtime .takt/runs copied into task workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "kept.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterKept, err := hashTaskWorkspaceTemplate(root)
	if err != nil {
		t.Fatal(err)
	}
	if afterKept == before {
		t.Fatal("copied workspace content did not change fingerprint")
	}
}

func TestTaskReplanExpectationStartsAtSecondRevision(t *testing.T) {
	if taskCaseExpectsReplan(0) || taskCaseExpectsReplan(1) || !taskCaseExpectsReplan(2) {
		t.Fatal("replan expectation must start at min_plan_revisions=2")
	}
}
