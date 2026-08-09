package evaluation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/version"
	"takt/internal/yamlcodec"
)

const TaskMatrixKind = "TaskEvaluationMatrix"
const TaskCaseManifestKind = "TaskCaseManifest"
const TaskMatrixReportVersion = "takt-task-evaluation-matrix/v1alpha1"

type TaskMatrix struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   MatrixMetadata       `json:"metadata"`
	Benchmark  TaskMatrixBenchmark  `json:"benchmark"`
	Strategies []TaskMatrixStrategy `json:"strategies"`
	Gates      []TaskMatrixGate     `json:"gates,omitempty"`
}

type TaskMatrixBenchmark struct {
	ID               string `json:"id"`
	BaselineStrategy string `json:"baseline_strategy"`
	Cases            string `json:"cases"`
	Repeat           *int   `json:"repeat,omitempty"`
	Profile          string `json:"profile,omitempty"`
}

type TaskMatrixStrategy struct {
	ID                string `json:"id"`
	WorkspaceTemplate string `json:"workspace_template"`
	Profile           string `json:"profile,omitempty"`
}

type TaskMatrixGate struct {
	Strategy                 string   `json:"strategy"`
	RouteAccuracyMin         *float64 `json:"route_accuracy_min,omitempty"`
	FinalSuccessRateMin      *float64 `json:"final_success_rate_min,omitempty"`
	ReplanExpectationRateMin *float64 `json:"replan_expectation_rate_min,omitempty"`
	UnexpectedNeedsInputMax  *int     `json:"unexpected_needs_input_max,omitempty"`
	RouterFallbacksMax       *int     `json:"router_fallbacks_max,omitempty"`
}

type TaskCaseManifest struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Cases      map[string]TaskCase `json:"cases"`
}

type TaskCase struct {
	Goal             string            `json:"goal"`
	ExpectedRoute    string            `json:"expected_route"`
	ExpectedTemplate string            `json:"expected_template,omitempty"`
	ExpectedWorkflow string            `json:"expected_workflow,omitempty"`
	ExpectedStatus   string            `json:"expected_status,omitempty"`
	MinPlanRevisions int               `json:"min_plan_revisions,omitempty"`
	AllowNeedsInput  bool              `json:"allow_needs_input,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

type TaskCaseExecution struct {
	PlanID         string
	RunID          string
	Status         string
	Route          string
	Template       string
	Workflow       string
	PlanRevisions  int
	ReplannerRuns  int
	ExecutionRuns  int
	RouterFallback bool
	InputTokens    int
	OutputTokens   int
	Cost           float64
}

type TaskCaseRunner func(ctx context.Context, workspace, goal, profile string) (TaskCaseExecution, error)

type TaskMatrixRunOptions struct {
	MatrixPath string
	OutputDir  string
	Repeat     int
	Replace    bool
	CaseRunner TaskCaseRunner
}

type TaskRunRecord struct {
	CaseID            string            `json:"case_id"`
	Repeat            int               `json:"repeat"`
	Labels            map[string]string `json:"labels,omitempty"`
	PlanID            string            `json:"plan_id,omitempty"`
	RunID             string            `json:"run_id,omitempty"`
	Status            string            `json:"status"`
	Route             string            `json:"route,omitempty"`
	Template          string            `json:"template,omitempty"`
	Workflow          string            `json:"workflow,omitempty"`
	RouteCorrect      bool              `json:"route_correct"`
	FinalSuccess      bool              `json:"final_success"`
	NeedsInput        bool              `json:"needs_input"`
	NeedsInputAllowed bool              `json:"needs_input_allowed,omitempty"`
	PlanRevisions     int               `json:"plan_revisions"`
	ReplannerRuns     int               `json:"replanner_runs"`
	ExecutionRuns     int               `json:"execution_runs"`
	RouterFallback    bool              `json:"router_fallback"`
	InputTokens       int               `json:"input_tokens"`
	OutputTokens      int               `json:"output_tokens"`
	Cost              float64           `json:"cost"`
	DurationMS        int64             `json:"duration_ms"`
	Error             string            `json:"error,omitempty"`
	ReplanExpected    bool              `json:"replan_expected"`
	ReplanExpectation bool              `json:"replan_expectation_met"`
}

type TaskSummary struct {
	Total                 int     `json:"total"`
	RouteCorrect          int     `json:"route_correct"`
	RouteAccuracy         float64 `json:"route_accuracy"`
	FinalSuccess          int     `json:"final_success"`
	FinalSuccessRate      float64 `json:"final_success_rate"`
	ExpectedReplanCases   int     `json:"expected_replan_cases"`
	ReplanExpectationsMet int     `json:"replan_expectations_met"`
	ReplanExpectationRate float64 `json:"replan_expectation_rate"`
	UnexpectedNeedsInput  int     `json:"unexpected_needs_input"`
	RouterFallbacks       int     `json:"router_fallbacks"`
	AveragePlanRevisions  float64 `json:"average_plan_revisions"`
	AverageReplannerRuns  float64 `json:"average_replanner_runs"`
	InputTokens           int     `json:"input_tokens"`
	OutputTokens          int     `json:"output_tokens"`
	Cost                  float64 `json:"cost"`
	DurationMS            int64   `json:"duration_ms"`
}

type TaskStrategyResult struct {
	ID          string          `json:"id"`
	Fingerprint string          `json:"fingerprint"`
	OutputDir   string          `json:"output_dir"`
	Summary     TaskSummary     `json:"summary"`
	Runs        []TaskRunRecord `json:"runs"`
}

type TaskPairwiseOutcome struct {
	CaseID                 string `json:"case_id"`
	Repeat                 int    `json:"repeat"`
	BaselineRouteCorrect   bool   `json:"baseline_route_correct"`
	CandidateRouteCorrect  bool   `json:"candidate_route_correct"`
	BaselineFinalSuccess   bool   `json:"baseline_final_success"`
	CandidateFinalSuccess  bool   `json:"candidate_final_success"`
	BaselinePlanRevisions  int    `json:"baseline_plan_revisions"`
	CandidatePlanRevisions int    `json:"candidate_plan_revisions"`
}

type TaskCompareReport struct {
	BaselineStrategy          string                `json:"baseline_strategy"`
	CandidateStrategy         string                `json:"candidate_strategy"`
	BaselineOnlyRouteCorrect  int                   `json:"baseline_only_route_correct"`
	CandidateOnlyRouteCorrect int                   `json:"candidate_only_route_correct"`
	BothRouteCorrect          int                   `json:"both_route_correct"`
	BothRouteWrong            int                   `json:"both_route_wrong"`
	BaselineOnlySuccess       int                   `json:"baseline_only_success"`
	CandidateOnlySuccess      int                   `json:"candidate_only_success"`
	BothSuccess               int                   `json:"both_success"`
	BothFailed                int                   `json:"both_failed"`
	Outcomes                  []TaskPairwiseOutcome `json:"outcomes"`
}

type TaskMatrixReport struct {
	ReportVersion         string               `json:"report_version"`
	TaktVersion           string               `json:"takt_version"`
	StartedAt             time.Time            `json:"started_at"`
	FinishedAt            time.Time            `json:"finished_at"`
	DurationMS            int64                `json:"duration_ms"`
	MatrixPath            string               `json:"matrix_path"`
	MatrixFingerprint     string               `json:"matrix_fingerprint"`
	ExperimentFingerprint string               `json:"experiment_fingerprint"`
	BenchmarkID           string               `json:"benchmark_id"`
	BaselineStrategy      string               `json:"baseline_strategy"`
	Repeat                int                  `json:"repeat"`
	Strategies            []TaskStrategyResult `json:"strategies"`
	Comparisons           []TaskCompareReport  `json:"comparisons"`
	Gates                 []GateResult         `json:"gates,omitempty"`
	Passed                bool                 `json:"passed"`
}

type TaskGateFailureError struct{ Report *TaskMatrixReport }

func (e *TaskGateFailureError) Error() string { return "task evaluation regression gate failed" }

func LoadTaskMatrix(path string) (*TaskMatrix, string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", "", err
	}
	var matrix TaskMatrix
	if err := yamlcodec.Unmarshal(data, &matrix); err != nil {
		return nil, "", "", fmt.Errorf("decode task evaluation matrix: %w", err)
	}
	if err := validateTaskMatrix(&matrix); err != nil {
		return nil, "", "", err
	}
	sum := sha256.Sum256(data)
	return &matrix, abs, hex.EncodeToString(sum[:]), nil
}

func validateTaskMatrix(matrix *TaskMatrix) error {
	if matrix.APIVersion != MatrixAPIVersion || matrix.Kind != TaskMatrixKind {
		return fmt.Errorf("task evaluation matrix must be %s %s", MatrixAPIVersion, TaskMatrixKind)
	}
	if strings.TrimSpace(matrix.Metadata.Name) == "" || strings.TrimSpace(matrix.Benchmark.ID) == "" {
		return fmt.Errorf("task evaluation matrix metadata.name and benchmark.id are required")
	}
	if strings.TrimSpace(matrix.Benchmark.Cases) == "" {
		return fmt.Errorf("benchmark.cases is required")
	}
	if matrix.Benchmark.Repeat != nil && *matrix.Benchmark.Repeat < 1 {
		return fmt.Errorf("benchmark.repeat must be >= 1 when specified")
	}
	if len(matrix.Strategies) < 2 {
		return fmt.Errorf("task evaluation matrix requires at least two strategies")
	}
	seen := map[string]bool{}
	for _, strategy := range matrix.Strategies {
		if strings.TrimSpace(strategy.ID) == "" || strings.TrimSpace(strategy.WorkspaceTemplate) == "" {
			return fmt.Errorf("every task strategy requires id and workspace_template")
		}
		if seen[strategy.ID] {
			return fmt.Errorf("duplicate task strategy id %q", strategy.ID)
		}
		seen[strategy.ID] = true
	}
	if !seen[matrix.Benchmark.BaselineStrategy] {
		return fmt.Errorf("baseline strategy %q is not defined", matrix.Benchmark.BaselineStrategy)
	}
	for _, gate := range matrix.Gates {
		if !seen[gate.Strategy] {
			return fmt.Errorf("gate references unknown strategy %q", gate.Strategy)
		}
		for name, value := range map[string]*float64{"route_accuracy_min": gate.RouteAccuracyMin, "final_success_rate_min": gate.FinalSuccessRateMin, "replan_expectation_rate_min": gate.ReplanExpectationRateMin} {
			if value != nil && (*value < 0 || *value > 1) {
				return fmt.Errorf("gate %s for %s must be in [0,1]", name, gate.Strategy)
			}
		}
		if gate.UnexpectedNeedsInputMax != nil && *gate.UnexpectedNeedsInputMax < 0 {
			return fmt.Errorf("gate unexpected_needs_input_max must be >= 0")
		}
		if gate.RouterFallbacksMax != nil && *gate.RouterFallbacksMax < 0 {
			return fmt.Errorf("gate router_fallbacks_max must be >= 0")
		}
	}
	return nil
}

func loadTaskCases(path string) (TaskCaseManifest, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskCaseManifest{}, "", err
	}
	var manifest TaskCaseManifest
	if err := yamlcodec.Unmarshal(data, &manifest); err != nil {
		return manifest, "", fmt.Errorf("decode task case manifest: %w", err)
	}
	if manifest.APIVersion != MatrixAPIVersion || manifest.Kind != TaskCaseManifestKind {
		return manifest, "", fmt.Errorf("task case manifest must be %s %s", MatrixAPIVersion, TaskCaseManifestKind)
	}
	if len(manifest.Cases) == 0 {
		return manifest, "", fmt.Errorf("task case manifest requires at least one case")
	}
	for id, item := range manifest.Cases {
		if strings.TrimSpace(id) == "" || strings.TrimSpace(item.Goal) == "" || strings.TrimSpace(item.ExpectedRoute) == "" {
			return manifest, "", fmt.Errorf("task case %q requires goal and expected_route", id)
		}
		switch item.ExpectedRoute {
		case "workflow", "template", "dynamic":
		default:
			return manifest, "", fmt.Errorf("task case %q expected_route must be workflow, template, or dynamic", id)
		}
		if item.MinPlanRevisions < 0 {
			return manifest, "", fmt.Errorf("task case %q min_plan_revisions must be >= 0", id)
		}
		if item.ExpectedStatus == "" {
			item.ExpectedStatus = "completed"
			manifest.Cases[id] = item
		}
	}
	sum := sha256.Sum256(data)
	return manifest, hex.EncodeToString(sum[:]), nil
}

func RunTaskMatrix(ctx context.Context, opts TaskMatrixRunOptions) (*TaskMatrixReport, error) {
	if opts.CaseRunner == nil {
		return nil, fmt.Errorf("task evaluation case runner is required")
	}
	matrix, matrixPath, matrixFingerprint, err := LoadTaskMatrix(opts.MatrixPath)
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
	} else if _, statErr := os.Stat(output); statErr == nil {
		return nil, fmt.Errorf("benchmark output %s already exists; use --replace", output)
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return nil, err
	}
	casesPath := resolveMatrixPath(baseDir, matrix.Benchmark.Cases)
	cases, caseFingerprint, err := loadTaskCases(casesPath)
	if err != nil {
		return nil, err
	}
	repeat := 1
	if matrix.Benchmark.Repeat != nil {
		repeat = *matrix.Benchmark.Repeat
	}
	if opts.Repeat > 0 {
		repeat = opts.Repeat
	}
	started := time.Now().UTC()
	report := &TaskMatrixReport{ReportVersion: TaskMatrixReportVersion, TaktVersion: version.Value, StartedAt: started, MatrixPath: matrixPath, MatrixFingerprint: matrixFingerprint, BenchmarkID: matrix.Benchmark.ID, BaselineStrategy: matrix.Benchmark.BaselineStrategy, Repeat: repeat, Passed: true}
	caseIDs := make([]string, 0, len(cases.Cases))
	for id := range cases.Cases {
		caseIDs = append(caseIDs, id)
	}
	sort.Strings(caseIDs)
	for _, strategy := range matrix.Strategies {
		template := resolveMatrixPath(baseDir, strategy.WorkspaceTemplate)
		fingerprint, hashErr := hashTaskWorkspaceTemplate(template)
		if hashErr != nil {
			return report, fmt.Errorf("strategy %s fingerprint: %w", strategy.ID, hashErr)
		}
		strategyResult := TaskStrategyResult{ID: strategy.ID, Fingerprint: fingerprint, OutputDir: filepath.Join(output, "strategies", sanitizeCaseID(strategy.ID))}
		for _, caseID := range caseIDs {
			for rep := 1; rep <= repeat; rep++ {
				workspace := filepath.Join(strategyResult.OutputDir, "workspaces", sanitizeCaseID(caseID), fmt.Sprintf("repeat-%02d", rep))
				run, runErr := runTaskCase(ctx, opts.CaseRunner, template, workspace, strategy, matrix.Benchmark.Profile, caseID, cases.Cases[caseID], rep)
				strategyResult.Runs = append(strategyResult.Runs, run)
				if runErr != nil && run.Status == "infrastructure_error" {
					return report, fmt.Errorf("strategy %s case %s repeat %d: %w", strategy.ID, caseID, rep, runErr)
				}
			}
		}
		strategyResult.Summary = summarizeTaskRuns(strategyResult.Runs)
		if err := os.MkdirAll(strategyResult.OutputDir, 0o755); err != nil {
			return report, err
		}
		if err := writeTaskStrategyReport(strategyResult.OutputDir, &strategyResult); err != nil {
			return report, err
		}
		report.Strategies = append(report.Strategies, strategyResult)
	}
	baseline := taskStrategyByID(report.Strategies, report.BaselineStrategy)
	for _, candidate := range report.Strategies {
		if candidate.ID != report.BaselineStrategy {
			report.Comparisons = append(report.Comparisons, compareTaskStrategies(*baseline, candidate))
		}
	}
	for _, gate := range matrix.Gates {
		strategy := taskStrategyByID(report.Strategies, gate.Strategy)
		for _, result := range evaluateTaskGate(gate, strategy.Summary) {
			report.Gates = append(report.Gates, result)
			if !result.Passed {
				report.Passed = false
			}
		}
	}
	report.ExperimentFingerprint, err = hashJSON(struct {
		BenchmarkID, CasesFingerprint string
		Repeat                        int
		Strategies                    []struct{ ID, Fingerprint string }
	}{BenchmarkID: matrix.Benchmark.ID, CasesFingerprint: caseFingerprint, Repeat: repeat, Strategies: taskFingerprintInputs(report.Strategies)})
	if err != nil {
		return report, err
	}
	report.FinishedAt = time.Now().UTC()
	report.DurationMS = report.FinishedAt.Sub(started).Milliseconds()
	if err := writeTaskMatrixReport(output, report); err != nil {
		return report, err
	}
	if !report.Passed {
		return report, &TaskGateFailureError{Report: report}
	}
	return report, nil
}

func runTaskCase(ctx context.Context, runner TaskCaseRunner, template, workspace string, strategy TaskMatrixStrategy, defaultProfile, caseID string, item TaskCase, repeat int) (TaskRunRecord, error) {
	record := TaskRunRecord{CaseID: caseID, Repeat: repeat, Labels: item.Labels, Status: "not_started", ReplanExpected: taskCaseExpectsReplan(item.MinPlanRevisions)}
	started := time.Now()
	if err := os.RemoveAll(workspace); err != nil {
		record.Status = "infrastructure_error"
		record.Error = err.Error()
		return record, err
	}
	if err := copyTaskTree(template, workspace); err != nil {
		record.Status = "infrastructure_error"
		record.Error = err.Error()
		return record, err
	}
	if err := initializeTaskWorkspaceGit(template, workspace); err != nil {
		record.Status = "infrastructure_error"
		record.Error = err.Error()
		return record, err
	}
	profile := strategy.Profile
	if profile == "" {
		profile = defaultProfile
	}
	if profile == "" {
		profile = "code"
	}
	execution, runErr := runner(ctx, workspace, item.Goal, profile)
	record.DurationMS = time.Since(started).Milliseconds()
	record.PlanID = execution.PlanID
	record.RunID = execution.RunID
	record.Status = execution.Status
	record.Route = execution.Route
	record.Template = execution.Template
	record.Workflow = execution.Workflow
	record.PlanRevisions = execution.PlanRevisions
	record.ReplannerRuns = execution.ReplannerRuns
	record.ExecutionRuns = execution.ExecutionRuns
	record.RouterFallback = execution.RouterFallback
	record.InputTokens = execution.InputTokens
	record.OutputTokens = execution.OutputTokens
	record.Cost = execution.Cost
	record.NeedsInput = record.Status == "waiting" || record.Status == "paused" || record.Status == "parked"
	record.NeedsInputAllowed = item.AllowNeedsInput
	record.RouteCorrect = record.Route == item.ExpectedRoute && (item.ExpectedTemplate == "" || record.Template == item.ExpectedTemplate) && (item.ExpectedWorkflow == "" || record.Workflow == item.ExpectedWorkflow)
	record.FinalSuccess = record.Status == item.ExpectedStatus && (!record.NeedsInput || record.NeedsInputAllowed)
	if record.ReplanExpected {
		record.ReplanExpectation = record.PlanRevisions >= item.MinPlanRevisions
	} else {
		record.ReplanExpectation = true
	}
	if runErr != nil && !record.FinalSuccess {
		record.Error = runErr.Error()
		if record.Status == "not_started" {
			record.Status = "failed"
		}
	}
	return record, runErr
}

func taskCaseExpectsReplan(minPlanRevisions int) bool {
	// Revision 1 is the initial plan. Replanning is observable only when a case
	// requires at least revision 2.
	return minPlanRevisions > 1
}

func routeHasSignal(raw json.RawMessage, signal string) bool {
	var value struct {
		Signals []string `json:"signals"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, item := range value.Signals {
		if item == signal {
			return true
		}
	}
	return false
}

func summarizeTaskRuns(runs []TaskRunRecord) TaskSummary {
	s := TaskSummary{Total: len(runs)}
	for _, r := range runs {
		if r.RouteCorrect {
			s.RouteCorrect++
		}
		if r.FinalSuccess {
			s.FinalSuccess++
		}
		if r.ReplanExpected {
			s.ExpectedReplanCases++
			if r.ReplanExpectation {
				s.ReplanExpectationsMet++
			}
		}
		if r.NeedsInput && !r.NeedsInputAllowed {
			s.UnexpectedNeedsInput++
		}
		if r.RouterFallback {
			s.RouterFallbacks++
		}
		s.AveragePlanRevisions += float64(r.PlanRevisions)
		s.AverageReplannerRuns += float64(r.ReplannerRuns)
		s.InputTokens += r.InputTokens
		s.OutputTokens += r.OutputTokens
		s.Cost += r.Cost
		s.DurationMS += r.DurationMS
	}
	if s.Total > 0 {
		s.RouteAccuracy = float64(s.RouteCorrect) / float64(s.Total)
		s.FinalSuccessRate = float64(s.FinalSuccess) / float64(s.Total)
		s.AveragePlanRevisions /= float64(s.Total)
		s.AverageReplannerRuns /= float64(s.Total)
	}
	if s.ExpectedReplanCases > 0 {
		s.ReplanExpectationRate = float64(s.ReplanExpectationsMet) / float64(s.ExpectedReplanCases)
	} else {
		s.ReplanExpectationRate = 1
	}
	return s
}

func compareTaskStrategies(baseline, candidate TaskStrategyResult) TaskCompareReport {
	out := TaskCompareReport{BaselineStrategy: baseline.ID, CandidateStrategy: candidate.ID}
	indexed := map[string]TaskRunRecord{}
	for _, r := range baseline.Runs {
		indexed[fmt.Sprintf("%s#%d", r.CaseID, r.Repeat)] = r
	}
	for _, c := range candidate.Runs {
		b, ok := indexed[fmt.Sprintf("%s#%d", c.CaseID, c.Repeat)]
		if !ok {
			continue
		}
		o := TaskPairwiseOutcome{CaseID: c.CaseID, Repeat: c.Repeat, BaselineRouteCorrect: b.RouteCorrect, CandidateRouteCorrect: c.RouteCorrect, BaselineFinalSuccess: b.FinalSuccess, CandidateFinalSuccess: c.FinalSuccess, BaselinePlanRevisions: b.PlanRevisions, CandidatePlanRevisions: c.PlanRevisions}
		out.Outcomes = append(out.Outcomes, o)
		switch {
		case b.RouteCorrect && c.RouteCorrect:
			out.BothRouteCorrect++
		case b.RouteCorrect:
			out.BaselineOnlyRouteCorrect++
		case c.RouteCorrect:
			out.CandidateOnlyRouteCorrect++
		default:
			out.BothRouteWrong++
		}
		switch {
		case b.FinalSuccess && c.FinalSuccess:
			out.BothSuccess++
		case b.FinalSuccess:
			out.BaselineOnlySuccess++
		case c.FinalSuccess:
			out.CandidateOnlySuccess++
		default:
			out.BothFailed++
		}
	}
	return out
}

func evaluateTaskGate(gate TaskMatrixGate, summary TaskSummary) []GateResult {
	var out []GateResult
	add := func(name string, passed bool, message string) {
		out = append(out, GateResult{Strategy: gate.Strategy, Passed: passed, Message: name + ": " + message})
	}
	if gate.RouteAccuracyMin != nil {
		add("route_accuracy", summary.RouteAccuracy >= *gate.RouteAccuracyMin, fmt.Sprintf("%.4f >= %.4f", summary.RouteAccuracy, *gate.RouteAccuracyMin))
	}
	if gate.FinalSuccessRateMin != nil {
		add("final_success_rate", summary.FinalSuccessRate >= *gate.FinalSuccessRateMin, fmt.Sprintf("%.4f >= %.4f", summary.FinalSuccessRate, *gate.FinalSuccessRateMin))
	}
	if gate.ReplanExpectationRateMin != nil {
		add("replan_expectation_rate", summary.ReplanExpectationRate >= *gate.ReplanExpectationRateMin, fmt.Sprintf("%.4f >= %.4f", summary.ReplanExpectationRate, *gate.ReplanExpectationRateMin))
	}
	if gate.UnexpectedNeedsInputMax != nil {
		add("unexpected_needs_input", summary.UnexpectedNeedsInput <= *gate.UnexpectedNeedsInputMax, fmt.Sprintf("%d <= %d", summary.UnexpectedNeedsInput, *gate.UnexpectedNeedsInputMax))
	}
	if gate.RouterFallbacksMax != nil {
		add("router_fallbacks", summary.RouterFallbacks <= *gate.RouterFallbacksMax, fmt.Sprintf("%d <= %d", summary.RouterFallbacks, *gate.RouterFallbacksMax))
	}
	return out
}

func taskStrategyByID(items []TaskStrategyResult, id string) *TaskStrategyResult {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
func taskFingerprintInputs(items []TaskStrategyResult) []struct{ ID, Fingerprint string } {
	out := make([]struct{ ID, Fingerprint string }, 0, len(items))
	for _, item := range items {
		out = append(out, struct{ ID, Fingerprint string }{item.ID, item.Fingerprint})
	}
	return out
}

func copyTaskTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(rel)
		if entry.IsDir() && taskWorkspaceRuntimeDir(slash) {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func initializeTaskWorkspaceGit(template, workspace string) error {
	if _, err := os.Stat(filepath.Join(template, ".git")); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	commands := [][]string{
		{"init", "-q"},
		{"config", "user.email", "takt-eval@example.invalid"},
		{"config", "user.name", "Takt Evaluation"},
		{"add", "."},
		{"commit", "-qm", "task evaluation baseline"},
	}
	for _, args := range commands {
		cmd := exec.Command("git", args...)
		cmd.Dir = workspace
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	exclude := strings.Join([]string{".takt/plans/", ".takt/runs/", ".takt/locks/", ".takt/host-sessions/", ".takt/notifications/", ""}, "\n")
	if err := os.WriteFile(filepath.Join(workspace, ".git", "info", "exclude"), []byte(exclude), 0o644); err != nil {
		return fmt.Errorf("write task evaluation git excludes: %w", err)
	}
	return nil
}

func hashTaskWorkspaceTemplate(root string) (string, error) {
	h := sha256.New()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		slash := filepath.ToSlash(rel)
		if entry.IsDir() && taskWorkspaceRuntimeDir(slash) {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	for _, path := range files {
		rel, _ := filepath.Rel(root, path)
		data, e := os.ReadFile(path)
		if e != nil {
			return "", e
		}
		_, _ = h.Write([]byte(filepath.ToSlash(rel)))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func taskWorkspaceRuntimeDir(slash string) bool {
	switch slash {
	case ".git", ".takt/runs", ".takt/plans", ".takt/locks", ".takt/host-sessions", ".takt/notifications":
		return true
	default:
		return false
	}
}

func writeTaskStrategyReport(dir string, report *TaskStrategyResult) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.json"), append(data, '\n'), 0o644)
}
func writeTaskMatrixReport(dir string, report *TaskMatrixReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "task-benchmark.json.tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "task-benchmark.json"))
}
func LoadTaskMatrixReport(outputDir string) (*TaskMatrixReport, error) {
	abs, err := filepath.Abs(outputDir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(abs, "task-benchmark.json"))
	if err != nil {
		return nil, err
	}
	var report TaskMatrixReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	if report.ReportVersion != TaskMatrixReportVersion {
		return nil, fmt.Errorf("unsupported task evaluation report version %q", report.ReportVersion)
	}
	return &report, nil
}
