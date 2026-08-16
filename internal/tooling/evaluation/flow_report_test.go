package evaluation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"takt/internal/store"
	"takt/internal/validation"
)

type flowReportStore map[string]*store.RunState

func (s flowReportStore) RunDir(string) string                      { return "" }
func (s flowReportStore) ArtifactsDir(string) string                { return "" }
func (s flowReportStore) Save(*store.RunState) error                { return nil }
func (s flowReportStore) Commit(*store.RunState, store.Event) error { return nil }
func (s flowReportStore) Load(id string) (*store.RunState, error)   { return s[id], nil }

func TestClassifyFlowRecord(t *testing.T) {
	for _, tc := range []struct {
		name, status, outcome string
		valid, passed         bool
	}{
		{"true accept", store.RunCompleted, "true_accept", true, true},
		{"false accept", store.RunCompleted, "false_accept", false, false},
		{"true reject", store.RunFailed, "true_reject", false, false},
		{"false reject", store.RunFailed, "false_reject", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record := RunRecord{Status: tc.status, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: tc.valid}}}
			ClassifyFlowRecord(&record)
			if record.Outcome != tc.outcome || record.RunPassed == nil || *record.RunPassed != tc.passed {
				t.Fatalf("record=%+v", record)
			}
		})
	}
}

func TestRecordFromFlowStatesUsesRootUsageAndUniqueActionNodes(t *testing.T) {
	root := &store.RunState{ID: "root", Status: store.RunCompleted, Usage: &store.Usage{InputTokens: 7, OutputTokens: 8, Cost: 9}, Nodes: map[string]*store.NodeState{
		"container": {Hidden: true, Usage: &store.Usage{InputTokens: 100}},
		"action":    {Attempts: 1, Executions: []store.ExecutionState{{Attempt: 1, Status: store.NodeCompleted}}},
		"loop":      {LoopIterations: []store.LoopIterationState{{Iteration: 1, Nodes: map[string]store.NodeState{"body": {Attempts: 1, Executions: []store.ExecutionState{{Attempt: 1, Status: store.NodeCompleted}}}}}}},
		"child":     {ChildRunID: "child-run"},
	}}
	child := &store.RunState{ID: "child-run", Nodes: map[string]*store.NodeState{"child-action": {Attempts: 1, Executions: []store.ExecutionState{{Attempt: 1, Status: store.NodeCompleted}}}}}
	record := recordFromFlowStates("case", 1, "workspace", root, flowReportStore{"child-run": child})
	if record.InputTokens != 7 || record.OutputTokens != 8 || record.Cost != 9 || record.Attempts != 3 || len(record.Nodes) != 3 {
		t.Fatalf("record=%+v", record)
	}
	if _, ok := record.Nodes["root/loop[001]/body"]; !ok {
		t.Fatalf("loop snapshot missing: %+v", record.Nodes)
	}
}

func TestRecordFromFlowStatesDiscoversLoopChildAndGrandchild(t *testing.T) {
	root := &store.RunState{ID: "root", Nodes: map[string]*store.NodeState{
		"loop": {LoopIterations: []store.LoopIterationState{{Iteration: 1, Nodes: map[string]store.NodeState{
			"child": {ChildRunID: "child-run"},
		}}}},
	}}
	child := &store.RunState{ID: "child-run", Nodes: map[string]*store.NodeState{
		"child-action": {Attempts: 1, Executions: []store.ExecutionState{{Attempt: 1, Status: store.NodeCompleted, Assistant: "child"}}},
		"grandchild":   {ChildRunID: "grandchild-run"},
	}}
	grandchild := &store.RunState{ID: "grandchild-run", Nodes: map[string]*store.NodeState{
		"grandchild-action": {Attempts: 1, Executions: []store.ExecutionState{{Attempt: 1, Status: store.NodeCompleted, Assistant: "grandchild"}}},
	}}
	record := recordFromFlowStates("case", 1, "workspace", root, flowReportStore{"child-run": child, "grandchild-run": grandchild})
	childNode := record.Nodes["root/child-run/child-action"]
	grandchildNode := record.Nodes["root/child-run/grandchild-run/grandchild-action"]
	if record.Attempts != 2 || len(record.Nodes) != 2 || len(childNode.Executions) != 1 || childNode.Executions[0].Assistant != "child" || len(grandchildNode.Executions) != 1 || grandchildNode.Executions[0].Assistant != "grandchild" {
		t.Fatalf("record=%+v", record)
	}
}

func TestFinishFlowReportRetainsQualityAggregation(t *testing.T) {
	score := 80.0
	diagnostic := validation.Diagnostic{Severity: "warning", Code: "CHECK"}
	report := &SuiteReport{Mode: "flow", StartedAt: time.Now().Add(-time.Second), Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "valid", Mode: "flow", Status: store.RunCompleted, Cost: 2, DurationMS: 100, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true, Score: &score, Diagnostics: []validation.Diagnostic{diagnostic}}}, Nodes: map[string]NodeRecord{}},
		{CaseID: "invalid", Mode: "flow", Status: store.RunCompleted, Cost: 3, DurationMS: 200, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}}, Nodes: map[string]NodeRecord{}},
	}}
	for i := range report.Runs {
		ClassifyFlowRecord(&report.Runs[i])
		addSummary(&report.Summary, report.Runs[i])
	}
	finishReport(report)
	if report.Summary.SuccessAt1 != nil || report.Summary.AverageAttemptsToValid != nil || report.Summary.Valid != 1 || report.Summary.QualityRuns != 2 || report.Summary.ScoredRuns != 1 || floatValue(report.Summary.AverageScore) != 80 || floatValue(report.Summary.CostPerValid) != 5 || report.Summary.AmortizedEndToEndMSPerValid == nil || report.Summary.DiagnosticsByCode["CHECK"] != 1 || report.Summary.DiagnosticsBySeverity["warning"] != 1 {
		t.Fatalf("summary=%+v", report.Summary)
	}
}

func TestFlowReportSchemaAcceptsReport(t *testing.T) {
	zero := 0.0
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	report := SuiteReport{ReportVersion: ReportVersion, TaktVersion: "test", Strategy: StrategyIdentity{ID: "s", Fingerprint: sha, WorkflowFingerprint: sha, ConfigFingerprint: sha, CommandsFingerprint: sha}, Benchmark: BenchmarkIdentity{ID: "b", Fingerprint: sha, DatasetFingerprint: sha, WorkspaceFingerprint: sha, CaseCount: 1}, Environment: EnvironmentIdentity{}, Mode: "flow", Runs: []RunRecord{{CaseID: "c", Repeat: 1, Status: store.RunCompleted, Workspace: "w", AttemptsToValid: nil, Nodes: map[string]NodeRecord{"work": {Executions: []ExecutionRecord{{Attempt: 1, ProviderAttempt: 1, Status: "completed"}}}}, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{ProtocolVersion: validation.ProtocolV1Alpha1, Type: "validation_result", Valid: true}}, Outcome: "true_accept", RunPassed: boolPointer(true)}}, Summary: newSummary()}
	report.Summary.Flow = &FlowSummary{ValidRate: &zero, FalseAcceptRate: &zero, FalseRejectRate: &zero, FlowCompletionRate: &zero, ValidationErrorRate: &zero}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	schemaPath := filepath.Join("..", "..", "..", "schemas", "evaluation-report.schema.json")
	src, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("report", doc); err != nil {
		t.Fatal(err)
	}
	sch, err := compiler.Compile("report")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if err := sch.Validate(value); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyFlowRecordLeavesValidatorErrorsAndPausesUnevaluated(t *testing.T) {
	for _, record := range []*RunRecord{
		{Status: store.RunFailed, Validation: &FlowValidationRecord{Status: "error", ErrorCode: "validator_exit"}},
		{Status: store.RunPaused, Validation: &FlowValidationRecord{Status: "error", ErrorCode: "run_paused"}},
	} {
		ClassifyFlowRecord(record)
		if record.Outcome != "" || record.RunPassed != nil {
			t.Fatalf("record=%+v", record)
		}
	}
}

func TestRunFlowClassifiesProviderUnavailableWithoutValidatorResult(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	report, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root,
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			return FlowCaseRunResult{States: []*store.RunState{{
				ID: "run", Status: store.RunFailed, ErrorCode: "provider_unavailable",
				Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{},
			}}}, nil
		},
	})
	if _, ok := err.(*FlowGateFailureError); !ok {
		t.Fatal(err)
	}
	record := report.Runs[0]
	if record.Outcome != "infrastructure_error" || record.RunPassed != nil || record.QualityExpected || record.Quality != nil {
		t.Fatalf("record=%+v", record)
	}
}

func TestFlowSummaryDenominatorsAndStability(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "stable", Status: store.RunCompleted, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}},
		{CaseID: "stable", Status: store.RunCompleted, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}},
		{CaseID: "unstable", Status: store.RunCompleted, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}},
		{CaseID: "unstable", Status: store.RunFailed, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}}},
		{CaseID: "ignored", Status: store.RunFailed, Mode: "flow", Validation: &FlowValidationRecord{Status: "error", ErrorCode: "validator_exit"}},
	}}
	for i := range report.Runs {
		ClassifyFlowRecord(&report.Runs[i])
		addSummary(&report.Summary, report.Runs[i])
	}
	finishReport(report)
	flow := report.Summary.Flow
	if flow == nil || flow.EvaluatedRuns != 4 || flow.FlowCompleted != 3 || flow.TrueAccept != 3 || flow.TrueReject != 1 || flow.ValidationErrors != 1 {
		t.Fatalf("flow=%+v", flow)
	}
	if flow.ValidRate == nil || *flow.ValidRate != 0.75 || flow.FalseAcceptRate == nil || *flow.FalseAcceptRate != 0 || flow.ValidationErrorRate == nil || *flow.ValidationErrorRate != 0.2 {
		t.Fatalf("flow=%+v", flow)
	}
	if report.Summary.StableValidCases != 1 || report.Summary.UnstableCases != 1 || report.Summary.StableInvalidCases != 0 {
		t.Fatalf("summary=%+v", report.Summary)
	}
}

func TestProviderUnavailableFlowRecordIsExcludedFromQualityRates(t *testing.T) {
	record := RunRecord{
		CaseID: "provider-unavailable", Mode: "flow", Status: store.RunFailed, ErrorCode: "provider_unavailable",
		InputTokens: 12, OutputTokens: 7,
		Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}},
	}
	ClassifyFlowRecord(&record)
	if record.Outcome != "infrastructure_error" || record.RunPassed != nil || record.QualityExpected || record.Quality != nil {
		t.Fatalf("provider-unavailable classification = %+v", record)
	}
	summary := newSummary()
	addSummary(&summary, record)
	if summary.Flow.EvaluatedRuns != 0 || summary.Flow.InfrastructureErrors != 1 || summary.QualityRuns != 0 || summary.Invalid != 0 || summary.InputTokens != 12 || summary.OutputTokens != 7 {
		t.Fatalf("provider-unavailable summary = %+v", summary)
	}
}

func TestRecoveredProviderRetryUsesTerminalExecutionForClassification(t *testing.T) {
	record := RunRecord{
		Mode: "flow", Status: store.RunCompleted,
		Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}},
		Nodes: map[string]NodeRecord{
			"work": {
				Status: string(store.NodeCompleted),
				Executions: []ExecutionRecord{
					{Status: string(store.NodeFailed), ErrorCode: "provider_unavailable"},
					{Status: string(store.NodeCompleted)},
				},
			},
		},
	}
	ClassifyFlowRecord(&record)
	if record.Outcome != "true_accept" || record.RunPassed == nil || !*record.RunPassed {
		t.Fatalf("recovered record classified as %+v", record)
	}
	staleRoot := record
	staleRoot.ErrorCode = "provider_unavailable"
	ClassifyFlowRecord(&staleRoot)
	if staleRoot.Outcome != "true_accept" {
		t.Fatalf("stale root evidence changed completed outcome: %+v", staleRoot)
	}
	completedExecution := RunRecord{Status: store.RunCompleted, Nodes: map[string]NodeRecord{
		"work": {Status: string(store.NodeCompleted), Executions: []ExecutionRecord{{Status: string(store.NodeCompleted), ErrorCode: "provider_unavailable", Diagnostic: &store.DiagnosticState{Kind: "provider_unavailable"}}}},
	}}
	ClassifyFlowRecord(&completedExecution)
	if completedExecution.Outcome != "" {
		t.Fatalf("completed execution evidence was treated as outage: %+v", completedExecution)
	}
}

func TestExhaustedProviderRetryUsesTerminalStructuredEvidence(t *testing.T) {
	for _, record := range []RunRecord{
		{Status: store.RunFailed, Nodes: map[string]NodeRecord{
			"work": {Status: string(store.NodeFailed), Executions: []ExecutionRecord{{Status: string(store.NodeFailed), ErrorCode: "provider_unavailable"}, {Status: string(store.NodeFailed), Diagnostic: &store.DiagnosticState{Kind: "provider_unavailable"}}}},
		}},
		{Status: store.RunFailed, Nodes: map[string]NodeRecord{
			"work": {Status: string(store.NodeFailed), Diagnostic: &store.DiagnosticState{Code: "provider_unavailable"}},
		}},
	} {
		ClassifyFlowRecord(&record)
		if record.Outcome != "infrastructure_error" || record.RunPassed != nil {
			t.Fatalf("exhausted record classified as %+v", record)
		}
	}
	messageOnly := RunRecord{Status: store.RunFailed, Error: "provider_unavailable"}
	ClassifyFlowRecord(&messageOnly)
	if messageOnly.Outcome != "" {
		t.Fatalf("human-readable message was treated as evidence: %+v", messageOnly)
	}
}

func TestFinishFlowReportExcludesProviderUnavailableFromQualityAggregates(t *testing.T) {
	score := 80.0
	timeToValid := int64(10)
	report := &SuiteReport{Mode: "flow", Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "quality", Mode: "flow", Status: store.RunCompleted, TimeToValidMS: &timeToValid, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}, Nodes: map[string]NodeRecord{}},
		{CaseID: "outage", Mode: "flow", Status: store.RunFailed, ErrorCode: "provider_unavailable", TimeToValidMS: &timeToValid, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true, Score: &score}}, Nodes: map[string]NodeRecord{}},
	}}
	for i := range report.Runs {
		ClassifyFlowRecord(&report.Runs[i])
		addSummary(&report.Summary, report.Runs[i])
	}
	finishFlowReport(report)
	if report.Summary.ScoredRuns != 0 || report.Summary.AverageScore != nil || floatValue(report.Summary.AverageTimeToValidMS) != 10 || report.Summary.StableValidCases != 1 || report.Summary.StableInvalidCases != 0 || report.Summary.UnstableCases != 0 {
		t.Fatalf("summary=%+v", report.Summary)
	}
}

func TestFlowSummaryNullAndMeasuredZeroRates(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: newSummary(), Runs: []RunRecord{{Mode: "flow", Status: store.RunFailed, Validation: &FlowValidationRecord{Status: "error"}}}}
	addSummary(&report.Summary, report.Runs[0])
	finishReport(report)
	if report.Summary.Flow.ValidRate != nil || report.Summary.Flow.FalseAcceptRate != nil || report.Summary.Flow.FalseRejectRate != nil {
		t.Fatalf("quality rates must be null: %+v", report.Summary.Flow)
	}
	if report.Summary.Flow.FlowCompletionRate == nil || *report.Summary.Flow.FlowCompletionRate != 0 || report.Summary.Flow.ValidationErrorRate == nil || *report.Summary.Flow.ValidationErrorRate != 1 {
		t.Fatalf("total rates=%+v", report.Summary.Flow)
	}
}

func TestFlowValidationWithoutResultDoesNotEnterQualityTotals(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: newSummary(), Runs: []RunRecord{{Mode: "flow", Status: store.RunFailed, Validation: &FlowValidationRecord{Status: "completed"}}}}
	addSummary(&report.Summary, report.Runs[0])
	finishReport(report)
	if report.Summary.Flow.EvaluatedRuns != 0 || report.Summary.Flow.ValidationErrors != 1 || report.Summary.QualityRuns != 0 || report.Summary.Invalid != 0 {
		t.Fatalf("summary=%+v", report.Summary)
	}
}

func TestApplyFlowGates(t *testing.T) {
	zero, one := 0.0, 1.0
	summary := Summary{Flow: &FlowSummary{ValidRate: &one, ValidationErrorRate: &zero, FlowCompletionRate: &one}}
	for _, result := range ApplyFlowGates(FlowGates{ValidRate: FlowThreshold{Min: &one}, ValidationErrorRate: FlowThreshold{Max: &zero}, FlowCompletionRate: FlowThreshold{Min: &one}}, summary) {
		if !result.Passed {
			t.Fatal(result)
		}
	}
	bad := 0.5
	results := ApplyFlowGates(FlowGates{ValidRate: FlowThreshold{Min: &bad}}, Summary{Flow: &FlowSummary{}})
	if len(results) != 1 || results[0].Passed {
		t.Fatalf("results=%+v", results)
	}
}

func TestCompareFlow(t *testing.T) {
	fingerprint := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	baseline := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{Flow: &FlowSummary{}}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow", Outcome: "false_accept", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}}}}}
	candidate := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{Flow: &FlowSummary{}}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow", Outcome: "true_accept", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}}}}
	got, err := Compare(baseline, candidate)
	if err != nil || got.Metrics.Flow == nil || got.Cases[0].BaselineOutcome == nil || *got.Cases[0].BaselineOutcome != "false_accept" || got.Cases[0].CandidateOutcome == nil || *got.Cases[0].CandidateOutcome != "true_accept" {
		t.Fatalf("compare=%+v err=%v", got, err)
	}
	if _, err := Compare(baseline, &SuiteReport{Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}}); err == nil {
		t.Fatal("accepted mixed flow comparison")
	}
}

func TestCompareIncludesExecutionResourceDeltas(t *testing.T) {
	fingerprint := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	base := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{InputTokens: 100, OutputTokens: 20, Attempts: 2, DurationMS: 1000}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow"}}}
	candidate := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{InputTokens: 150, OutputTokens: 30, Attempts: 3, DurationMS: 1300}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow"}}}
	got, err := Compare(base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range []struct {
		name string
		got  MetricComparison
		want float64
	}{
		{"input_tokens", got.Metrics.InputTokens, 50},
		{"output_tokens", got.Metrics.OutputTokens, 10},
		{"attempts", got.Metrics.TotalAttempts, 1},
		{"duration_ms", got.Metrics.TotalDurationMS, 300},
	} {
		if metric.got.Delta == nil || *metric.got.Delta != metric.want {
			t.Fatalf("%s delta=%v want=%v", metric.name, metric.got.Delta, metric.want)
		}
	}
}

func TestCompareStringFormatsResourceCountsAndDurationsForHumans(t *testing.T) {
	fingerprint := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	base := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{InputTokens: 1325526, OutputTokens: 57480, Attempts: 5, DurationMS: 596287}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow"}}}
	candidate := &SuiteReport{Mode: "flow", Benchmark: BenchmarkIdentity{Fingerprint: fingerprint}, Summary: Summary{InputTokens: 1325527, OutputTokens: 57482, Attempts: 6, DurationMS: 596290}, Runs: []RunRecord{{CaseID: "a", Repeat: 1, Mode: "flow"}}}
	got, err := Compare(base, candidate)
	if err != nil {
		t.Fatal(err)
	}
	text := got.String()
	for _, want := range []string{
		"Total tokens        1 383 006        1 383 009        +3 (+0.0%)",
		"Duration            9m56.287s        9m56.29s         +3ms (+0.0%)",
		"Node attempts       5                6                +1 (+20.0%)",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("comparison text misses %q:\n%s", want, text)
		}
	}
}

func TestCompareStringExplainsWhetherEachChangeIsGoodOrBad(t *testing.T) {
	fingerprint := strings.Repeat("d", 64)
	zero, one := 0.0, 1.0
	baseline := &SuiteReport{
		Mode: "flow", OutputDir: ".takt/evals/feature-development/a",
		Strategy:  StrategyIdentity{ModelPreset: "preset-a", Models: map[string]string{"implementation": "gemini/model-a", "review": "gemini/model-a"}},
		Benchmark: BenchmarkIdentity{ID: "suite.yaml", Fingerprint: fingerprint},
		Summary: Summary{
			Total: 3, Valid: 3, InputTokens: 3301501, OutputTokens: 254918, Attempts: 15, DurationMS: 1954032, CostPerValid: &zero, AverageTimeToValidMS: floatPointer(652605),
			FinalSuccessRate: &one, Flow: &FlowSummary{EvaluatedRuns: 3, FlowCompleted: 3, TrueAccept: 3, ValidRate: &one, FalseAcceptRate: &zero, FalseRejectRate: &zero, FlowCompletionRate: &one, ValidationErrorRate: &zero},
		},
	}
	candidate := &SuiteReport{
		Mode: "flow", OutputDir: ".takt/evals/feature-development/b",
		Strategy:  StrategyIdentity{ModelPreset: "preset-b", Models: map[string]string{"implementation": "openai/model-b", "review": "openai/model-b"}},
		Benchmark: BenchmarkIdentity{ID: "suite.yaml", Fingerprint: fingerprint},
		Summary: Summary{
			Total: 3, Invalid: 3, InputTokens: 5866113, OutputTokens: 144611, Attempts: 13, DurationMS: 2521825,
			FinalSuccessRate: &zero, Flow: &FlowSummary{EvaluatedRuns: 3, FlowCompleted: 3, FalseAccept: 3, ValidRate: &zero, FalseAcceptRate: &one, FalseRejectRate: &zero, FlowCompletionRate: &one, ValidationErrorRate: &zero},
		},
	}
	for repeat := 1; repeat <= 3; repeat++ {
		baseline.Runs = append(baseline.Runs, RunRecord{CaseID: "implement-basic", Repeat: repeat, Mode: "flow", Status: store.RunCompleted, Outcome: "true_accept", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}})
		candidate.Runs = append(candidate.Runs, RunRecord{CaseID: "implement-basic", Repeat: repeat, Mode: "flow", Status: store.RunCompleted, Outcome: "false_accept", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}}})
	}
	comparison, err := Compare(baseline, candidate)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(comparison.String()), " ")
	for _, want := range []string{
		"Assessment B compared with A",
		"SUMMARY Overall WORSE Correctness WORSE Reliability WORSE Efficiency WORSE Evidence 3 paired runs",
		"Preset A preset-a Preset B preset-b",
		"Valid products 3/3 (100%) 0/3 (0%) -3 WORSE",
		"Flow completed 3/3 (100%) 3/3 (100%) 0 SAME",
		"False accepts 0/3 (0%) 3/3 (100%) +3 WORSE",
		"Total tokens 3 556 419 6 010 724 +2 454 305 (+69.0%) WORSE",
		"Duration 32m34.032s 42m1.825s +9m27.793s (+29.1%) WORSE",
		"Node attempts 15 13 -2 (-13.3%) BETTER",
		"Time to valid 10m52.605s no valid result no result WORSE",
		"MODELS Alias A B implementation gemini/model-a openai/model-b",
		"implement-basic#1 true_accept false_accept WORSE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("comparison text misses %q:\n%s", want, comparison.String())
		}
	}
}

func TestBuildStatsSummarizesReport(t *testing.T) {
	averageTimeToValidMS := 596287.0
	fingerprint := "fa5ecf8482cc841061af1e8d75d0547f15b59e9dee434d052a0bda3ae999f81a"
	report := &SuiteReport{
		Mode: "flow", Workflow: "code:feature-development", OutputDir: ".takt/evals/run-a",
		Strategy:  StrategyIdentity{ModelPreset: "gemini", Models: map[string]string{"implementation": "gemini/gemini-3.7-flash-high", "review": "gemini/gemini-3.7-flash-high"}},
		Benchmark: BenchmarkIdentity{ID: "suite.yaml", Fingerprint: fingerprint, Validator: ValidatorIdentity{ID: "mini-du", Version: "2", Fingerprint: fingerprint}},
		Summary: Summary{
			Total: 2, Valid: 1, Invalid: 1, Attempts: 5, InputTokens: 1325526, OutputTokens: 57480, DurationMS: 595168, Cost: 0.5,
			AverageTimeToValidMS: &averageTimeToValidMS, Flow: &FlowSummary{},
			UsageByExecutionIdentity: map[string]UsageBreakdown{"assistant=coding-agent|version=0.84.1|requested=implementation=gemini/gemini-3.7-flash-high|resolved=implementation=gemini/gemini-3.7-flash-high": {Executions: 2, InputTokens: 815650, OutputTokens: 37746}},
		},
		Runs: []RunRecord{{CaseID: "implement-basic", Repeat: 1, Status: "completed", Outcome: "true_accept", Attempts: 5, InputTokens: 1325526, OutputTokens: 57480, DurationMS: 595168, Nodes: map[string]NodeRecord{
			"run-a/implement": {Assistant: "coding-agent", RequestedModel: &store.ModelRef{Name: "implementation", Provider: "gemini", ID: "gemini-3.7-flash-high"}, Executions: []ExecutionRecord{{Assistant: "coding-agent", Attempt: 1, ProviderAttempt: 1, SessionID: "019ffb26-419f-78fc-b25f-6a7b4f54e2d1", Usage: &store.Usage{InputTokens: 535026, OutputTokens: 31236}}}},
			"run-a/validate":  {Attempts: 1, Executions: []ExecutionRecord{{Attempt: 1}}},
			"run-a/summary":   {Assistant: "coding-agent", RequestedModel: &store.ModelRef{Name: "review", Provider: "gemini", ID: "gemini-3.7-flash-high"}, Executions: []ExecutionRecord{{Assistant: "coding-agent", Usage: &store.Usage{InputTokens: 112129, OutputTokens: 4863}}}},
		}}, {CaseID: "b", Repeat: 1, Status: "failed", Outcome: "true_reject"}},
	}
	stats := BuildStats(report)
	if stats == nil || stats.ReportVersion != "takt-evaluation-stats/v1alpha1" || stats.Total != 2 || stats.Valid != 1 || stats.InputTokens != 1325526 || stats.OutputTokens != 57480 || stats.TotalTokens != 1383006 || stats.Cost != 0.5 || len(stats.Cases) != 2 {
		t.Fatalf("stats=%+v", stats)
	}
	text := stats.String()
	for _, want := range []string{"RUN", "RESULT", "RESOURCES", "MODELS", "USAGE", "CASES", "ASSISTANT STEPS", "ASSISTANT SESSIONS", "suite.yaml  fa5ecf8482cc", "mini-du@2", "9m55.168s", "9m56.287s", "1 383 006", "Node attempts", "Assistant executions", "implementation", "gemini/gemini-3.7-flash-high", "coding-agent@0.84.1", "implement-basic", "summary", "fresh", "019ffb26-419f-78fc-b25f-6a7b4f54e2d1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("stats text misses %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{"fingerprint=", "|requested=", "595168ms", "1325526"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("stats text contains unreadable %q:\n%s", unwanted, text)
		}
	}
}

func TestStatsShowsPerCaseFailureCause(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: Summary{Total: 1, Invalid: 1}, Runs: []RunRecord{{
		CaseID: "implement-basic", Repeat: 1, Status: store.RunCompleted, Outcome: "false_accept",
		Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false, Diagnostics: []validation.Diagnostic{{Code: "mini_du_invalid", Message: "missing pull request effect", Severity: "error"}}}},
	}}}
	stats := BuildStats(report)
	if len(stats.Cases) != 1 || stats.Cases[0].Cause != "mini_du_invalid: missing pull request effect" || stats.Cases[0].CauseSource != "validator" {
		t.Fatalf("cases=%+v", stats.Cases)
	}
	text := strings.Join(strings.Fields(stats.String()), " ")
	if !strings.Contains(text, "FAILURES Case Outcome Source Cause implement-basic#1 false_accept validator mini_du_invalid: missing pull request effect") {
		t.Fatalf("stats missing failure explanation:\n%s", stats.String())
	}
}

func TestStatsPrefersRuntimeCauseOverValidatorForFailedRun(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: Summary{Total: 1, Invalid: 1}, Runs: []RunRecord{{
		CaseID: "implement-symlink-and-hardlink", Repeat: 1, Status: store.RunFailed, Outcome: "true_reject",
		Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false, Diagnostics: []validation.Diagnostic{{Code: "mini_du_invalid", Message: "missing artifact implementation.md", Severity: "error"}}}},
		Nodes:      map[string]NodeRecord{"implement": {Status: store.NodeFailed, ErrorCode: "exit", Error: "pi agent exited with code 1: Connection error."}},
	}}}
	stats := BuildStats(report)
	if len(stats.Cases) != 1 || stats.Cases[0].CauseSource != "node:implement" || stats.Cases[0].Cause != "exit: pi agent exited with code 1: Connection error." {
		t.Fatalf("cases=%+v", stats.Cases)
	}
}

func TestFlowStatsCaptureAssistantNodeWallTimeFromEvents(t *testing.T) {
	started := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	record := RunRecord{
		RunID: "run-a", CaseID: "case-a", Repeat: 1,
		Nodes: map[string]NodeRecord{"run-a/implement": {Assistant: "coding-agent", Executions: []ExecutionRecord{{Assistant: "coding-agent"}}}},
	}
	applyRuntimeMetricsFromEvents(&record, &store.RunState{ID: "run-a"}, []store.Event{
		{RunID: "run-a", NodeID: "implement", Type: "node.started", Time: started},
		{RunID: "run-a", NodeID: "implement", Type: "node.completed", Time: started.Add(1500 * time.Millisecond)},
	}, "")

	encoded, err := json.Marshal(record.Nodes["run-a/implement"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"duration_ms":1500`)) {
		t.Fatalf("node duration is not persisted: %s", encoded)
	}
	stats := BuildStats(&SuiteReport{Runs: []RunRecord{record}, Summary: Summary{}})
	if text := stats.String(); !strings.Contains(text, "1.5s") {
		t.Fatalf("stats do not show assistant wall time:\n%s", text)
	}
}
