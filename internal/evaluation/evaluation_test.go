package evaluation

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	workflow := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: evaluation-test
defaults:
  assistant: fake
  model: m
nodes:
  - id: implement
    prompt: |
      Process this input:
      ${input}
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

	report, err := Run(context.Background(), RunOptions{
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
	mustWrite(t, workflowPath, "apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: x\nnodes:\n  - id: done\n    bash: |\n      true\n", 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{
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

	_, err := Run(context.Background(), RunOptions{
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

	_, err := Run(context.Background(), RunOptions{
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
	_, err := Run(context.Background(), RunOptions{
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: quality-test
defaults:
  assistant: fake
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

	report, err := Run(context.Background(), RunOptions{
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
		if record.Quality == nil || !record.Quality.Valid || record.AttemptsToValid != 1 || !record.ValidAtFirstAttempt {
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: malformed-quality
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

	report, err := Run(context.Background(), RunOptions{
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
			{Cost: 2, DurationMS: 100, AttemptsToValid: 1, QualityExpected: true, QualityNodeStatus: string(store.NodeCompleted), Quality: &validation.Result{Valid: true, Score: &score}},
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
	path := filepath.Join("..", "..", "schemas", "evaluation-report.schema.json")
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: failed-generation
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

	report, err := Run(context.Background(), RunOptions{
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: failed-quality
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

	report, err := Run(context.Background(), RunOptions{
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
	if run.AttemptsToValid != 0 || run.ValidAtFirstAttempt {
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: invalid-quality
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

	report, err := Run(context.Background(), RunOptions{
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
	mustWrite(t, workflowPath, `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: malformed-failed-quality
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

	report, err := Run(context.Background(), RunOptions{
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
	mustWrite(t, workflowPath, "apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: fingerprint\nnodes:\n  - id: done\n    bash: |\n      true\n", 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "one.md"), "case", 0o644)

	run := func(id, version, output string) *SuiteReport {
		report, err := Run(context.Background(), RunOptions{
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
