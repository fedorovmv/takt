package evaluation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/execution"
	"takt/internal/redact"
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
			Prompt: "redacted analyzer prompt", PromptFingerprint: strings.Repeat("a", 64),
			Usage: &store.Usage{InputTokens: 11, OutputTokens: 7, Cost: 0.25}, Output: `{"primary_class":"unknown","failure_mode":"unknown","confidence":"low","root_cause":"unknown","causal_chain":[],"evidence":[{"path":"run.json","pointer":"/status","fact":"unknown"}],"contributing_factors":[],"recommended_actions":[],"missing_evidence":[],"disagreement":{"with_deterministic_cause":false,"explanation":"unknown"}}`,
		}},
	}}}
	report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1, Status: store.RunCompleted}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, result, nil)
	if report.AnalysisStatus != "completed" || report.Session.Adapter != "pi" || report.Session.SessionID != "analysis-session" || report.Prompt != "redacted analyzer prompt" || report.PromptFingerprint != strings.Repeat("a", 64) || report.Usage.InputTokens != 11 || report.Usage.OutputTokens != 7 || report.Usage.Cost != 0.25 {
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

func TestValidateAdvisoryAnalysisCitationsAgainstManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run.json"), []byte(`{"status":"failed","nodes":[{"status":"failed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "validator.stderr"), []byte("warning\nerror\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := AnalysisEvidenceManifest{Files: []AnalysisEvidenceFile{{Path: "run.json"}, {Path: "validator.stderr"}}}
	valid := AdvisoryAnalysis{Evidence: []AdvisoryEvidence{{Path: "run.json", Pointer: "/status", Fact: "failed"}}, CausalChain: []AdvisoryCausalLink{{Fact: "validator failed", Consequence: "case rejected", Evidence: []string{"validator.stderr#line:2"}}}}
	if err := validateAdvisoryAnalysisEvidence(valid, manifest, root); err != nil {
		t.Fatalf("valid citations rejected: %v", err)
	}
	for name, value := range map[string]AdvisoryAnalysis{
		"missing path": {Evidence: []AdvisoryEvidence{{Path: "missing.json", Pointer: "/status", Fact: "x"}}},
		"traversal":    {Evidence: []AdvisoryEvidence{{Path: "../run.json", Pointer: "/status", Fact: "x"}}},
		"bad pointer":  {Evidence: []AdvisoryEvidence{{Path: "run.json", Pointer: "/missing", Fact: "x"}}},
		"bad line":     {Evidence: []AdvisoryEvidence{{Path: "validator.stderr", Pointer: "line:4", Fact: "x"}}},
		"bad causal":   {CausalChain: []AdvisoryCausalLink{{Fact: "x", Consequence: "y", Evidence: []string{"run.json#/missing"}}}},
	} {
		if err := validateAdvisoryAnalysisEvidence(value, manifest, root); err == nil {
			t.Errorf("%s citation unexpectedly accepted", name)
		}
	}
}

func TestAnalysisFailureWithoutNodeDiagnosticsHasSchemaSafeError(t *testing.T) {
	result := FlowCaseRunResult{States: []*store.RunState{{Status: store.RunCancelled, Nodes: map[string]*store.NodeState{"analyze": {Status: store.NodeBlocked}}}}}
	report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, result, nil)
	if report.AnalysisStatus != "not_run" || report.ErrorCode == "" || report.Error == "" {
		t.Fatalf("schema-unsafe failure report=%+v", report)
	}
}

func TestAnalysisFailureMapsProviderOutageWithoutSnapshot(t *testing.T) {
	err := &execution.Error{Kind: execution.KindProviderUnavailable, Op: "analysis", Err: errors.New("provider unavailable")}
	report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, FlowCaseRunResult{}, err)
	if report.AnalysisStatus != "provider_unavailable" || report.ErrorCode != "provider_unavailable" || report.Error == "" {
		t.Fatalf("provider failure report=%+v", report)
	}
}

func TestAnalysisCaseReportMapsTerminalStatusesWithFallbackDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, nodeStatus, errorCode, want string
	}{
		{name: "timeout", nodeStatus: store.NodeTimedOut, want: "timed_out"},
		{name: "cancelled", nodeStatus: store.NodeCancelled, want: "not_run"},
		{name: "blocked", nodeStatus: store.NodeBlocked, want: "not_run"},
		{name: "provider", nodeStatus: store.NodeFailed, errorCode: "provider_unavailable", want: "provider_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := FlowCaseRunResult{States: []*store.RunState{{Status: tc.nodeStatus, Nodes: map[string]*store.NodeState{"analyze": {Status: tc.nodeStatus, ErrorCode: tc.errorCode}}}}}
			report := analysisCaseReportFromRun(AnalysisCase{CaseID: "case", Repeat: 1}, RunRecord{CaseID: "case", Repeat: 1}, AnalysisModel{Preset: "legacy", Alias: "takt_analyze", Provider: "fake", ID: "model"}, result, nil)
			if report.AnalysisStatus != tc.want || report.ErrorCode != tc.want || report.Error == "" {
				t.Fatalf("report=%+v", report)
			}
		})
	}
}

func TestCaptureAnalysisSessionEvidenceRedactsAndUsesRelativePath(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "source-session.jsonl")
	if err := os.WriteFile(session, []byte("{\"text\":\"known-secret\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	redactor := &redact.Redactor{}
	redactor.AddSecret("known-secret")
	report := AnalysisSession{Adapter: "pi", SessionID: "session-1", SessionPath: session, SessionEvidence: "unavailable"}
	if err := captureAnalysisSessionEvidence(root, &report, redactor); err != nil {
		t.Fatal(err)
	}
	if report.SessionEvidence != "recorded" || report.SessionEvidencePath != "sessions/analyze.jsonl" {
		t.Fatalf("session report=%+v", report)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(report.SessionEvidencePath)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "known-secret") {
		t.Fatal("analysis session leaked secret")
	}
}

func TestCaptureAnalysisSessionEvidenceBoundsPostRedactionExpansion(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "source-session.jsonl")
	if err := os.WriteFile(session, []byte(strings.Repeat("s", maxSessionEvidenceBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	redactor := &redact.Redactor{}
	redactor.AddSecret("s")
	report := AnalysisSession{Adapter: "pi", SessionID: "session-1", SessionPath: session, SessionEvidence: "unavailable"}
	if err := captureAnalysisSessionEvidence(root, &report, redactor); err != nil {
		t.Fatal(err)
	}
	if report.SessionEvidence != "unavailable" || report.SessionEvidencePath != "" {
		t.Fatalf("expanded session should be unavailable: %+v", report)
	}
}

func TestCaptureAnalysisSessionEvidenceFailsClosedForNULSecret(t *testing.T) {
	root := t.TempDir()
	session := filepath.Join(root, "source-session.jsonl")
	if err := os.WriteFile(session, append([]byte{0}, []byte("known-secret")...), 0o644); err != nil {
		t.Fatal(err)
	}
	redactor := &redact.Redactor{}
	redactor.AddSecret("known-secret")
	report := AnalysisSession{Adapter: "pi", SessionID: "session-1", SessionPath: session, SessionEvidence: "unavailable"}
	if err := captureAnalysisSessionEvidence(root, &report, redactor); err == nil {
		t.Fatal("expected binary secret to fail closed")
	}
}

func TestPersistAnalysisTraceBoundsPostRedactionExpansion(t *testing.T) {
	root := t.TempDir()
	redactor := &redact.Redactor{}
	redactor.AddSecret("s")
	if err := persistAnalysisTrace(root, []string{strings.Repeat("s", maxAnalysisTraceBytes)}, redactor); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "trace.log"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxAnalysisTraceBytes {
		t.Fatalf("trace exceeds limit after redaction: %d", len(data))
	}
}

func TestAnalyzeFlowPersistsCaseTraceBeforeCleanup(t *testing.T) {
	output := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: takt/v1alpha1\nkind: Config\nmodels:\n  takt_analyze: {provider: fake, id: model}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(output, "cases", "case", "repeat-001"), 0o755); err != nil {
		t.Fatal(err)
	}
	reportData, err := json.Marshal(SuiteReport{Runs: []RunRecord{{CaseID: "case", Repeat: 1, Status: "failed", Outcome: "false_accept"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "report.json"), reportData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(output, "cases", "case", "repeat-001", "run.json"), []byte(`{"states":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cleanupSawTrace := false
	resultJSON := `{"primary_class":"unknown","failure_mode":"unknown","confidence":"low","root_cause":"unknown","causal_chain":[],"evidence":[{"path":"run.json","pointer":"/states","fact":"empty"}],"contributing_factors":[],"recommended_actions":[],"missing_evidence":[],"disagreement":{"with_deterministic_cause":false,"explanation":"none"}}`
	_, err = AnalyzeFlow(context.Background(), AnalysisRunOptions{
		OutputDir: output, ConfigPath: configPath, Now: func() time.Time { return time.Unix(10, 0).UTC() },
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			return FlowCaseRunResult{
				States: []*store.RunState{{Status: store.RunCompleted, CreatedAt: time.Unix(10, 0), UpdatedAt: time.Unix(11, 0), Nodes: map[string]*store.NodeState{
					"analyze": {Status: store.NodeCompleted, Output: resultJSON},
				}}},
				Cleanup: func(context.Context) (*store.RunState, error) {
					_, statErr := os.Stat(filepath.Join(filepath.Dir(request.Workspace), "trace.log"))
					cleanupSawTrace = statErr == nil
					return nil, nil
				},
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cleanupSawTrace {
		t.Fatal("case trace was not persisted before cleanup")
	}
}
