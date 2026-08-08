package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/version"
	"takt/internal/yamlmini"
)

const MatrixAPIVersion = "takt/evaluation/v1alpha1"
const MatrixKind = "EvaluationMatrix"
const MatrixReportVersion = "takt-evaluation-matrix/v1alpha1"

type Matrix struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   MatrixMetadata   `json:"metadata"`
	Benchmark  MatrixBenchmark  `json:"benchmark"`
	Strategies []MatrixStrategy `json:"strategies"`
	Gates      []MatrixGate     `json:"gates,omitempty"`
}

type MatrixMetadata struct {
	Name string `json:"name"`
}

type MatrixBenchmark struct {
	ID                string          `json:"id"`
	BaselineStrategy  string          `json:"baseline_strategy"`
	Cases             string          `json:"cases"`
	CaseManifest      string          `json:"case_manifest,omitempty"`
	WorkspaceTemplate string          `json:"workspace_template"`
	Repeat            *int            `json:"repeat,omitempty"`
	ApprovalAnswer    string          `json:"approval_answer,omitempty"`
	QualityNode       string          `json:"quality_node"`
	GenerationNode    string          `json:"generation_node"`
	Validator         MatrixValidator `json:"validator,omitempty"`
}

type MatrixValidator struct {
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
}

type MatrixStrategy struct {
	ID       string `json:"id"`
	Workflow string `json:"workflow"`
	Config   string `json:"config"`
}

type MatrixGate struct {
	Strategy                         string   `json:"strategy"`
	SuccessAt1Min                    *float64 `json:"success_at_1_min,omitempty"`
	FinalSuccessRateMin              *float64 `json:"final_success_rate_min,omitempty"`
	CostPerValidMaxRegressionPercent *float64 `json:"cost_per_valid_max_regression_percent,omitempty"`
	TimeToValidMaxRegressionPercent  *float64 `json:"time_to_valid_max_regression_percent,omitempty"`
	UnstableCasesMax                 *int     `json:"unstable_cases_max,omitempty"`
}

type MatrixRunOptions struct {
	ExecutionFactory ExecutionFactory
	MatrixPath       string
	OutputDir        string
	Repeat           int
	Replace          bool
}

type MatrixStrategyResult struct {
	ID        string            `json:"id"`
	OutputDir string            `json:"output_dir"`
	Strategy  StrategyIdentity  `json:"strategy"`
	Benchmark BenchmarkIdentity `json:"benchmark"`
	Summary   Summary           `json:"summary"`
}

type GateResult struct {
	Strategy string `json:"strategy"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
}

type MatrixReport struct {
	ReportVersion         string                 `json:"report_version"`
	TaktVersion           string                 `json:"takt_version"`
	StartedAt             time.Time              `json:"started_at"`
	FinishedAt            time.Time              `json:"finished_at"`
	DurationMS            int64                  `json:"duration_ms"`
	MatrixPath            string                 `json:"matrix_path"`
	MatrixFingerprint     string                 `json:"matrix_fingerprint"`
	ExperimentFingerprint string                 `json:"experiment_fingerprint"`
	BenchmarkID           string                 `json:"benchmark_id"`
	BaselineStrategy      string                 `json:"baseline_strategy"`
	Repeat                int                    `json:"repeat"`
	Strategies            []MatrixStrategyResult `json:"strategies"`
	Comparisons           []CompareReport        `json:"comparisons"`
	Gates                 []GateResult           `json:"gates,omitempty"`
	Passed                bool                   `json:"passed"`
}

func LoadMatrix(path string) (*Matrix, string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", "", err
	}
	var matrix Matrix
	if err := yamlmini.Unmarshal(data, &matrix); err != nil {
		return nil, "", "", fmt.Errorf("decode evaluation matrix: %w", err)
	}
	if err := validateMatrix(&matrix); err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(data)
	return &matrix, abs, hex.EncodeToString(sum[:]), nil
}

func validateMatrix(matrix *Matrix) error {
	if matrix.APIVersion != MatrixAPIVersion || matrix.Kind != MatrixKind {
		return fmt.Errorf("evaluation matrix must be %s %s", MatrixAPIVersion, MatrixKind)
	}
	if strings.TrimSpace(matrix.Metadata.Name) == "" || strings.TrimSpace(matrix.Benchmark.ID) == "" {
		return fmt.Errorf("evaluation matrix metadata.name and benchmark.id are required")
	}
	if matrix.Benchmark.Repeat != nil && *matrix.Benchmark.Repeat < 1 {
		return fmt.Errorf("benchmark.repeat must be >= 1 when specified")
	}
	if strings.TrimSpace(matrix.Benchmark.Cases) == "" || strings.TrimSpace(matrix.Benchmark.WorkspaceTemplate) == "" || strings.TrimSpace(matrix.Benchmark.QualityNode) == "" || strings.TrimSpace(matrix.Benchmark.GenerationNode) == "" {
		return fmt.Errorf("benchmark cases, workspace_template, quality_node, and generation_node are required")
	}
	if len(matrix.Strategies) < 2 {
		return fmt.Errorf("evaluation matrix requires at least two strategies")
	}
	seen := map[string]bool{}
	for _, strategy := range matrix.Strategies {
		id := strings.TrimSpace(strategy.ID)
		if id == "" || strings.TrimSpace(strategy.Workflow) == "" || strings.TrimSpace(strategy.Config) == "" {
			return fmt.Errorf("every strategy requires id, workflow, and config")
		}
		if seen[id] {
			return fmt.Errorf("duplicate strategy id %q", id)
		}
		seen[id] = true
	}
	if strings.TrimSpace(matrix.Benchmark.BaselineStrategy) == "" {
		return fmt.Errorf("benchmark.baseline_strategy is required")
	}
	if !seen[matrix.Benchmark.BaselineStrategy] {
		return fmt.Errorf("baseline strategy %q is not defined", matrix.Benchmark.BaselineStrategy)
	}
	for _, gate := range matrix.Gates {
		if !seen[gate.Strategy] {
			return fmt.Errorf("gate references unknown strategy %q", gate.Strategy)
		}
		for name, value := range map[string]*float64{
			"success_at_1_min":       gate.SuccessAt1Min,
			"final_success_rate_min": gate.FinalSuccessRateMin,
		} {
			if value != nil && (*value < 0 || *value > 1) {
				return fmt.Errorf("gate %s for %s must be in [0,1]", name, gate.Strategy)
			}
		}
		for name, value := range map[string]*float64{
			"cost_per_valid_max_regression_percent": gate.CostPerValidMaxRegressionPercent,
			"time_to_valid_max_regression_percent":  gate.TimeToValidMaxRegressionPercent,
		} {
			if value != nil && *value < 0 {
				return fmt.Errorf("gate %s for %s must be >= 0", name, gate.Strategy)
			}
		}
		if gate.UnstableCasesMax != nil && *gate.UnstableCasesMax < 0 {
			return fmt.Errorf("gate unstable_cases_max for %s must be >= 0", gate.Strategy)
		}
	}
	return nil
}

func RunMatrix(ctx context.Context, opts MatrixRunOptions) (*MatrixReport, error) {
	matrix, matrixPath, matrixFingerprint, err := LoadMatrix(opts.MatrixPath)
	if err != nil {
		return nil, err
	}
	baseDir := filepath.Dir(matrixPath)
	output := opts.OutputDir
	if strings.TrimSpace(output) == "" {
		output = filepath.Join(baseDir, ".takt", "evals", matrix.Metadata.Name)
	}
	if !filepath.IsAbs(output) {
		output = filepath.Join(baseDir, output)
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if opts.Replace {
		if err := os.RemoveAll(output); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(output); err == nil {
		return nil, fmt.Errorf("benchmark output %s already exists; use --replace", output)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	report := &MatrixReport{
		ReportVersion: MatrixReportVersion, TaktVersion: version.Value, StartedAt: started,
		MatrixPath: matrixPath, MatrixFingerprint: matrixFingerprint, BenchmarkID: matrix.Benchmark.ID,
		BaselineStrategy: matrix.Benchmark.BaselineStrategy, Passed: true,
	}
	repeat := 1
	if matrix.Benchmark.Repeat != nil {
		repeat = *matrix.Benchmark.Repeat
	}
	if opts.Repeat > 0 {
		repeat = opts.Repeat
	}
	report.Repeat = repeat
	cases := resolveMatrixPath(baseDir, matrix.Benchmark.Cases)
	template := resolveMatrixPath(baseDir, matrix.Benchmark.WorkspaceTemplate)
	caseManifest := ""
	if matrix.Benchmark.CaseManifest != "" {
		caseManifest = resolveMatrixPath(baseDir, matrix.Benchmark.CaseManifest)
	}
	validatorPath := matrix.Benchmark.Validator.Path
	if validatorPath != "" {
		validatorPath = resolveMatrixPath(baseDir, validatorPath)
	}
	loaded := map[string]*SuiteReport{}
	for _, strategy := range matrix.Strategies {
		strategyOutput := filepath.Join(output, "strategies", sanitizeCaseID(strategy.ID))
		suite, runErr := Run(ctx, RunOptions{
			ExecutionFactory: opts.ExecutionFactory,
			WorkflowPath:     resolveMatrixPath(baseDir, strategy.Workflow), ConfigPath: resolveMatrixPath(baseDir, strategy.Config),
			CasesDir: cases, WorkspaceTemplate: template, OutputDir: strategyOutput, Repeat: repeat,
			ApprovalAnswer: matrix.Benchmark.ApprovalAnswer, Replace: true, StrategyID: strategy.ID, BenchmarkID: matrix.Benchmark.ID,
			QualityNode: matrix.Benchmark.QualityNode, GenerationNode: matrix.Benchmark.GenerationNode,
			ValidatorID: matrix.Benchmark.Validator.ID, ValidatorVersion: matrix.Benchmark.Validator.Version, ValidatorPath: validatorPath,
			CaseManifestPath: caseManifest,
		})
		if runErr != nil {
			return report, fmt.Errorf("strategy %s: %w", strategy.ID, runErr)
		}
		loaded[strategy.ID] = suite
		report.Strategies = append(report.Strategies, MatrixStrategyResult{ID: strategy.ID, OutputDir: strategyOutput, Strategy: suite.Strategy, Benchmark: suite.Benchmark, Summary: suite.Summary})
	}
	baseline := loaded[matrix.Benchmark.BaselineStrategy]
	for _, strategy := range matrix.Strategies {
		if strategy.ID == matrix.Benchmark.BaselineStrategy {
			continue
		}
		comparison, err := Compare(baseline, loaded[strategy.ID])
		if err != nil {
			return report, fmt.Errorf("compare %s to baseline: %w", strategy.ID, err)
		}
		report.Comparisons = append(report.Comparisons, *comparison)
	}
	for _, gate := range matrix.Gates {
		results := evaluateGate(gate, baseline, loaded[gate.Strategy])
		for _, result := range results {
			report.Gates = append(report.Gates, result)
			if !result.Passed {
				report.Passed = false
			}
		}
	}
	fingerprint, err := experimentFingerprint(report)
	if err != nil {
		return report, err
	}
	report.ExperimentFingerprint = fingerprint
	report.FinishedAt = time.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(started).Milliseconds()
	if err := writeMatrixReport(output, report); err != nil {
		return report, err
	}
	if !report.Passed {
		return report, &GateFailureError{Report: report}
	}
	return report, nil
}

type GateFailureError struct{ Report *MatrixReport }

func (e *GateFailureError) Error() string { return "evaluation gates failed" }

func evaluateGate(gate MatrixGate, baseline, candidate *SuiteReport) []GateResult {
	var results []GateResult
	absolute := func(name string, actual, minimum *float64) {
		if minimum == nil {
			return
		}
		passed := actual != nil && *actual >= *minimum
		message := fmt.Sprintf("%s actual=%s min=%.6g", name, formatMetric(actual), *minimum)
		results = append(results, GateResult{Strategy: gate.Strategy, Passed: passed, Message: message})
	}
	absolute("success_at_1", candidate.Summary.SuccessAt1, gate.SuccessAt1Min)
	absolute("final_success_rate", candidate.Summary.FinalSuccessRate, gate.FinalSuccessRateMin)
	relative := func(name string, base, actual, maxRegression *float64) {
		if maxRegression == nil {
			return
		}
		passed := base != nil && actual != nil
		regression := 0.0
		if passed {
			if *base == 0 {
				passed = *actual <= 0
			} else {
				regression = (*actual - *base) / abs(*base) * 100
				passed = regression <= *maxRegression
			}
		}
		results = append(results, GateResult{Strategy: gate.Strategy, Passed: passed, Message: fmt.Sprintf("%s regression=%.6g%% max=%.6g%%", name, regression, *maxRegression)})
	}
	relative("cost_per_valid", baseline.Summary.CostPerValid, candidate.Summary.CostPerValid, gate.CostPerValidMaxRegressionPercent)
	relative("average_time_to_valid_ms", baseline.Summary.AverageTimeToValidMS, candidate.Summary.AverageTimeToValidMS, gate.TimeToValidMaxRegressionPercent)
	if gate.UnstableCasesMax != nil {
		passed := candidate.Summary.UnstableCases <= *gate.UnstableCasesMax
		results = append(results, GateResult{Strategy: gate.Strategy, Passed: passed, Message: fmt.Sprintf("unstable_cases actual=%d max=%d", candidate.Summary.UnstableCases, *gate.UnstableCasesMax)})
	}
	return results
}

func resolveMatrixPath(baseDir, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(baseDir, value)
}

func experimentFingerprint(report *MatrixReport) (string, error) {
	type strategyFP struct{ ID, Strategy, Benchmark string }
	items := make([]strategyFP, 0, len(report.Strategies))
	for _, strategy := range report.Strategies {
		items = append(items, strategyFP{strategy.ID, strategy.Strategy.Fingerprint, strategy.Benchmark.Fingerprint})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return hashJSON(struct {
		BenchmarkID string       `json:"benchmark_id"`
		Baseline    string       `json:"baseline"`
		Repeat      int          `json:"repeat"`
		Strategies  []strategyFP `json:"strategies"`
	}{report.BenchmarkID, report.BaselineStrategy, report.Repeat, items})
}

func LoadMatrixReport(outputDir string) (*MatrixReport, error) {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "benchmark.json"))
	if err != nil {
		return nil, err
	}
	var report MatrixReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode evaluation matrix report: %w", err)
	}
	return &report, nil
}

func writeMatrixReport(output string, report *MatrixReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(output, "benchmark.json"), append(data, '\n'), 0o644)
}

func formatMetric(value *float64) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprintf("%.6g", *value)
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (r MatrixReport) String() string {
	lines := []string{fmt.Sprintf("benchmark: %s", r.BenchmarkID), fmt.Sprintf("experiment: %s", r.ExperimentFingerprint), fmt.Sprintf("repeat: %d", r.Repeat), fmt.Sprintf("passed: %t", r.Passed)}
	for _, strategy := range r.Strategies {
		lines = append(lines, fmt.Sprintf("%s: success@1=%s final=%s attempts=%s cost/valid=%s time-to-valid=%s unstable=%d", strategy.ID, formatMetric(strategy.Summary.SuccessAt1), formatMetric(strategy.Summary.FinalSuccessRate), formatMetric(strategy.Summary.AverageAttemptsToValid), formatMetric(strategy.Summary.CostPerValid), formatMetric(strategy.Summary.AverageTimeToValidMS), strategy.Summary.UnstableCases))
	}
	for _, gate := range r.Gates {
		lines = append(lines, fmt.Sprintf("gate %s: %t — %s", gate.Strategy, gate.Passed, gate.Message))
	}
	return strings.Join(lines, "\n")
}
