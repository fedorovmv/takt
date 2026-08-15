package evaluation

import (
	"context"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
)

func TestSelectAnalysisCasesDefaultsAndRepeatRule(t *testing.T) {
	r := &SuiteReport{Runs: []RunRecord{{CaseID: "ok", Repeat: 1, Outcome: "true_accept"}, {CaseID: "false-accept", Repeat: 1, Outcome: "false_accept"}, {CaseID: "outage", Repeat: 1, Outcome: "infrastructure_error"}}}
	got, err := SelectAnalysisCases(r, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].CaseID != "false-accept" || got[1].CaseID != "outage" {
		t.Fatalf("selected=%v", got)
	}
	if _, err := SelectAnalysisCases(r, "", 1); err == nil {
		t.Fatal("repeat without case should fail")
	}
	got, err = SelectAnalysisCases(r, "ok", 0)
	if err != nil || len(got) != 1 || got[0].CaseID != "ok" {
		t.Fatalf("explicit case=%v err=%v", got, err)
	}
}

func TestSelectAnalysisCasesExplicitCaseIncludesTrueAccept(t *testing.T) {
	r := &SuiteReport{Runs: []RunRecord{{CaseID: "ok", Repeat: 1, Outcome: "true_accept"}, {CaseID: "ok", Repeat: 2, Outcome: "true_accept"}}}
	got, err := SelectAnalysisCases(r, "ok", 2)
	if err != nil || len(got) != 1 || got[0].CaseID != "ok" || got[0].Repeat != 2 {
		t.Fatalf("selected=%v err=%v", got, err)
	}
}

func TestAnalysisReportStringShowsDeterministicAndAdvisorySections(t *testing.T) {
	report := AnalysisRunReport{
		Status: "completed",
		Model:  AnalysisModel{Provider: "gemini", ID: "gemini-3.7-flash-high"},
		Analyses: []AnalysisCaseReport{{
			CaseID: "implement-basic", Repeat: 1,
			Deterministic:  AnalysisDeterministic{Outcome: "false_accept", CauseSource: "validator", Cause: "mini_du_invalid"},
			AnalysisStatus: "completed",
			Analysis: &AdvisoryAnalysis{
				PrimaryClass: "candidate", FailureMode: "missing_artifact", Confidence: "high",
				RootCause: "validator-required implementation.md was absent",
				Evidence:  []AdvisoryEvidence{{Path: "cases/implement-basic/repeat-001/validation-result.json", Pointer: "/result/diagnostics/0"}},
			},
		}},
	}
	got := report.String()
	for _, want := range []string{
		"ANALYSIS",
		"  Status        completed",
		"  Model         gemini/gemini-3.7-flash-high",
		"CASE implement-basic#1",
		"  Deterministic false_accept validator mini_du_invalid",
		"  Advisory     candidate / missing_artifact high",
		"  Root cause   validator-required implementation.md was absent",
		"  Evidence     cases/implement-basic/repeat-001/validation-result.json#/result/diagnostics/0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(strings.ToLower(got), "quality verdict") {
		t.Fatalf("human analysis output contains a second quality verdict: %s", got)
	}
}

func TestAnalysisReportStringShowsUnavailableEvidence(t *testing.T) {
	got := (AnalysisRunReport{Status: "failed", Analyses: []AnalysisCaseReport{{CaseID: "outage", Repeat: 1, AnalysisStatus: "provider_unavailable"}}}).String()
	if !strings.Contains(got, "Evidence     UNAVAILABLE") {
		t.Fatalf("output=%s", got)
	}
}

func TestAnalysisCaseReportUsesAnalyzerNodeUsageAndSessionMetadata(t *testing.T) {
	result := FlowCaseRunResult{States: []*store.RunState{{
		Status:    store.RunCompleted,
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(4, 0),
		Usage: &store.Usage{InputTokens: 900, OutputTokens: 800, Cost: 9},
		Nodes: map[string]*store.NodeState{"analyze": {
			Status: store.NodeCompleted, Adapter: "pi", SessionID: "analysis-session", SessionPath: "/tmp/analysis.jsonl",
			Usage: &store.Usage{InputTokens: 11, OutputTokens: 7, Cost: 0.25}, Output: `{"primary_class":"unknown","failure_mode":"unknown","confidence":"low","root_cause":"unknown","causal_chain":[],"evidence":[{"path":"run.json","pointer":"/status","fact":"unknown"}],"contributing_factors":[],"recommended_actions":[],"missing_evidence":[],"disagreement":{"with_deterministic_cause":false,"explanation":"unknown"}}`,
		}},
	}}}
	report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1, Status: store.RunCompleted}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, result, nil)
	if report.AnalysisStatus != "completed" || report.Session.Adapter != "pi" || report.Session.SessionID != "analysis-session" || report.Usage.InputTokens != 11 || report.Usage.OutputTokens != 7 || report.Usage.Cost != 0.25 {
		t.Fatalf("report=%+v", report)
	}
}

func TestFailedAnalysisCaseHasSchemaRequiredFingerprintAndSessionAdapter(t *testing.T) {
	report := failedAnalysisCase(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, "not_run", context.DeadlineExceeded)
	if report.EvidenceFingerprint == "" || report.Session.Adapter == "" || report.ErrorCode != "not_run" || report.Error == "" {
		t.Fatalf("report=%+v", report)
	}
}

func TestAnalysisCaseReportRejectsSchemaIncompleteJSON(t *testing.T) {
	result := FlowCaseRunResult{States: []*store.RunState{{
		Status:    store.RunCompleted,
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
		Nodes: map[string]*store.NodeState{"analyze": {Status: store.NodeCompleted, Output: `{}`}},
	}}}
	report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, result, nil)
	if report.AnalysisStatus != "protocol" || report.ErrorCode != "protocol" {
		t.Fatalf("report=%+v", report)
	}
}
