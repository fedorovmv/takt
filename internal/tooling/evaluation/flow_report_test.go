package evaluation

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	report := SuiteReport{ReportVersion: ReportVersion, TaktVersion: "test", Strategy: StrategyIdentity{ID: "s", Fingerprint: sha, WorkflowFingerprint: sha, ConfigFingerprint: sha, CommandsFingerprint: sha}, Benchmark: BenchmarkIdentity{ID: "b", Fingerprint: sha, DatasetFingerprint: sha, WorkspaceFingerprint: sha, CaseCount: 1}, Environment: EnvironmentIdentity{}, Mode: "flow", Runs: []RunRecord{{CaseID: "c", Repeat: 1, Status: store.RunCompleted, Workspace: "w", AttemptsToValid: nil, Nodes: map[string]NodeRecord{}, Mode: "flow", Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{ProtocolVersion: validation.ProtocolV1Alpha1, Type: "validation_result", Valid: true}}, Outcome: "true_accept", RunPassed: boolPointer(true)}}, Summary: newSummary()}
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
