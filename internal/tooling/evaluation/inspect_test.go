package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
	"takt/internal/validation"
)

func TestInspectFlowEvaluationExplainsSavedFailureEvidence(t *testing.T) {
	dir := t.TempDir()
	control := filepath.Join(dir, "control")
	execution := filepath.Join(control, ".takt", "worktrees", "run-1")
	report := &SuiteReport{
		ReportVersion: ReportVersion, Mode: "flow", Workflow: "code:feature-development", OutputDir: dir,
		Runs: []RunRecord{{
			CaseID: "implement-basic", Repeat: 1, RunID: "run-1", Status: store.RunCompleted, Outcome: "false_accept",
			Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false, Diagnostics: []validation.Diagnostic{{Code: "mini_du_invalid", Message: "missing pull request effect", Severity: "error"}}}},
			Nodes: map[string]NodeRecord{
				"validate":  {Status: store.NodeFailed, ErrorCode: "exit", Error: "bash exited with code 2"},
				"create-pr": {Status: store.NodeSkipped, ErrorCode: "trigger_rule_not_satisfied"},
			},
		}},
	}
	if err := writeReport(dir, report); err != nil {
		t.Fatal(err)
	}
	repeatRoot := filepath.Join(dir, "cases", "implement-basic", "repeat-001")
	if err := os.MkdirAll(filepath.Join(repeatRoot, "artifacts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repeatRoot, "source"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "run.json"), flowRunEvidence{RootRunID: "run-1", States: []*store.RunState{{ID: "run-1", Workspace: control, ExecutionWorkspace: execution}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repeatRoot, "validation-result.json"), []byte(`{"status":"completed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repeatRoot, "diff.patch"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repeatRoot, "repository.bundle"), []byte("bundle"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repeatRoot, "artifacts", "manifest.json"), []byte(`{"artifacts":[{"evidence_path":"files/run-1/pr-url.txt"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	activity := `{"events":[{"time":"2026-08-15T00:00:00Z","type":"assistant.tool.started","run_id":"run-1","node_id":"implement","revision":1,"tool":"write","input":{"path":"` + filepath.ToSlash(filepath.Join(control, "internal", "du", "du.go")) + `"}}]}`
	if err := os.WriteFile(filepath.Join(repeatRoot, "activity.json"), []byte(activity), 0o644); err != nil {
		t.Fatal(err)
	}

	inspection, err := InspectFlowEvaluation(dir, "implement-basic", 1)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.ReportVersion != InspectionReportVersion || len(inspection.Cases) != 1 {
		t.Fatalf("inspection=%+v", inspection)
	}
	item := inspection.Cases[0]
	if item.Cause.Source != "validator" || item.Cause.Confidence != "CONFIRMED" || item.Cause.Message != "mini_du_invalid: missing pull request effect" {
		t.Fatalf("cause=%+v", item.Cause)
	}
	if len(item.Nodes) != 2 || item.Evidence.DiffBytes != 0 || item.Evidence.SCMCalls != 0 || item.Evidence.SCMCallsRecorded || len(item.Evidence.Artifacts) != 1 {
		t.Fatalf("case=%+v", item)
	}
	if len(item.Observations) != 1 || item.Observations[0].Code != "control_workspace_mutation" || item.Observations[0].Confidence != "CONFIRMED" {
		t.Fatalf("observations=%+v", item.Observations)
	}
	text := strings.Join(strings.Fields(inspection.String()), " ")
	for _, want := range []string{
		"CASE implement-basic#1 Run run-1 Status completed Outcome false_accept",
		"REPORTED CAUSE CONFIRMED validator mini_du_invalid: missing pull request effect",
		"validate failed exit: bash exited with code 2",
		"SCM calls 0 (not recorded)",
		"Source cases/implement-basic/repeat-001/source",
		"Git bundle cases/implement-basic/repeat-001/repository.bundle",
		"Activity cases/implement-basic/repeat-001/activity.json",
		"Artifact cases/implement-basic/repeat-001/artifacts/files/run-1/pr-url.txt",
		"control_workspace_mutation CONFIRMED assistant mutation targeted control workspace instead of execution workspace",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("inspection misses %q:\n%s", want, inspection.String())
		}
	}
}

func TestInspectFlowEvaluationFiltersCases(t *testing.T) {
	dir := t.TempDir()
	report := &SuiteReport{ReportVersion: ReportVersion, Mode: "flow", OutputDir: dir, Runs: []RunRecord{{CaseID: "a", Repeat: 1}, {CaseID: "a", Repeat: 2}, {CaseID: "b", Repeat: 1}}, StartedAt: time.Now()}
	if err := writeReport(dir, report); err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectFlowEvaluation(dir, "a", 2)
	if err != nil || len(inspection.Cases) != 1 || inspection.Cases[0].Repeat != 2 {
		t.Fatalf("inspection=%+v err=%v", inspection, err)
	}
	if _, err := InspectFlowEvaluation(dir, "missing", 0); err == nil {
		t.Fatal("missing case was accepted")
	}
}

func TestInspectFlowEvaluationRejectsEvidencePathEscapes(t *testing.T) {
	t.Run("case id", func(t *testing.T) {
		dir := t.TempDir()
		report := &SuiteReport{ReportVersion: ReportVersion, Mode: "flow", OutputDir: dir, Runs: []RunRecord{{CaseID: "../../outside", Repeat: 1}}}
		if err := writeReport(dir, report); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectFlowEvaluation(dir, "", 0); err == nil || !strings.Contains(err.Error(), "escapes evaluation output") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("artifact manifest", func(t *testing.T) {
		dir := t.TempDir()
		report := &SuiteReport{ReportVersion: ReportVersion, Mode: "flow", OutputDir: dir, Runs: []RunRecord{{CaseID: "case", Repeat: 1}}}
		if err := writeReport(dir, report); err != nil {
			t.Fatal(err)
		}
		repeatRoot := filepath.Join(dir, "cases", "case", "repeat-001")
		if err := os.MkdirAll(filepath.Join(repeatRoot, "artifacts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repeatRoot, "artifacts", "manifest.json"), []byte(`{"artifacts":[{"evidence_path":"../../../../../secret"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectFlowEvaluation(dir, "", 0); err == nil || !strings.Contains(err.Error(), "escapes evaluation output") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		dir := t.TempDir()
		report := &SuiteReport{ReportVersion: ReportVersion, Mode: "flow", OutputDir: dir, Runs: []RunRecord{{CaseID: "case", Repeat: 1}}}
		if err := writeReport(dir, report); err != nil {
			t.Fatal(err)
		}
		outside := t.TempDir()
		if err := os.MkdirAll(filepath.Join(outside, "repeat-001"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "cases", "case")); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if _, err := InspectFlowEvaluation(dir, "", 0); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestInspectWorkspaceActivityResolvesRelativeToolPaths(t *testing.T) {
	dir := t.TempDir()
	repeatRoot := filepath.Join(dir, "cases", "case", "repeat-001")
	control := filepath.Join(dir, "control")
	execution := filepath.Join(control, ".takt", "worktrees", "run-1")
	if err := os.MkdirAll(repeatRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "run.json"), flowRunEvidence{States: []*store.RunState{{ID: "run-1", Workspace: control, ExecutionWorkspace: execution}}}, nil); err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(execution, filepath.Join(control, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	activity := flowActivityEvidence{Events: []flowActivityEvent{{Type: "assistant.tool.started", RunID: "run-1", Tool: "write", Input: map[string]any{"path": relative}}}}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "activity.json"), activity, nil); err != nil {
		t.Fatal(err)
	}
	observations, err := inspectWorkspaceActivity(dir, repeatRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(observations) != 1 || observations[0].Code != "control_workspace_mutation" {
		t.Fatalf("observations=%+v", observations)
	}
}

func TestInspectFlowEvaluationBuildsDeterministicCausalChain(t *testing.T) {
	dir := t.TempDir()
	report := &SuiteReport{ReportVersion: ReportVersion, Mode: "flow", OutputDir: dir, Runs: []RunRecord{{
		CaseID: "case", Repeat: 1, RunID: "run-1", Status: store.RunFailed, Outcome: "true_reject",
		Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false, Diagnostics: []validation.Diagnostic{{Code: "mini_du_invalid", Message: "missing artifact implementation.md"}}}},
	}}}
	if err := writeReport(dir, report); err != nil {
		t.Fatal(err)
	}
	repeatRoot := filepath.Join(dir, "cases", "case", "repeat-001")
	stdout := strings.Join([]string{
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"toolCall","name":"read","arguments":{"path":"main.go"}},{"type":"toolCall","name":"bash","arguments":{"command":"pwd"}}],"stopReason":"toolUse"}}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"plan"}],"usage":{"output":16384},"stopReason":"length"}}`,
	}, "\n")
	state := &store.RunState{ID: "run-1", Status: store.RunFailed, Nodes: map[string]*store.NodeState{
		"implement": {Status: store.NodeCompleted, Assistant: "coding-agent", Stdout: stdout},
		"validate":  {Status: store.NodeFailed, ErrorCode: "exit", Error: "bash exited with code 2"},
		"create-pr": {Status: store.NodeSkipped, ErrorCode: "trigger_rule_not_satisfied"},
		"summary":   {Status: store.NodeSkipped, ErrorCode: "trigger_rule_not_satisfied"},
	}}
	if err := writeFlowJSON(filepath.Join(repeatRoot, "run.json"), flowRunEvidence{RootRunID: "run-1", States: []*store.RunState{state}}, nil); err != nil {
		t.Fatal(err)
	}

	inspection, err := InspectFlowEvaluation(dir, "case", 1)
	if err != nil {
		t.Fatal(err)
	}
	chain := inspection.Cases[0].CausalChain
	wantCodes := []string{"assistant_output_limit", "no_direct_write_tools", "completed_without_result", "deterministic_validation_failed", "downstream_skipped"}
	if len(chain) != len(wantCodes) {
		t.Fatalf("causal chain=%+v", chain)
	}
	for index, want := range wantCodes {
		if chain[index].Code != want || chain[index].Confidence != "CONFIRMED" {
			t.Fatalf("causal chain[%d]=%+v want=%s", index, chain[index], want)
		}
	}
	text := strings.Join(strings.Fields(inspection.String()), " ")
	for _, want := range []string{
		"CAUSAL CHAIN",
		"assistant_output_limit CONFIRMED node implement reached model output limit after 16384 output tokens",
		"no_direct_write_tools CONFIRMED node implement started tools bash=1, read=1 but no direct file-write tool",
		"completed_without_result CONFIRMED node implement was recorded completed with empty assistant output",
		"deterministic_validation_failed CONFIRMED node validate failed: exit: bash exited with code 2",
		"downstream_skipped CONFIRMED downstream nodes were skipped: create-pr, summary",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("inspection misses %q:\n%s", want, inspection.String())
		}
	}
}

func TestInspectCausalChainDoesNotCallSkippedValidationFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.json")
	state := &store.RunState{ID: "run-1", Nodes: map[string]*store.NodeState{"validate": {Status: store.NodeSkipped, ErrorCode: "trigger_rule_not_satisfied"}}}
	if err := writeFlowJSON(path, flowRunEvidence{RootRunID: "run-1", States: []*store.RunState{state}}, nil); err != nil {
		t.Fatal(err)
	}
	chain, err := inspectRunCausalChain(path, "run.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range chain {
		if item.Code == "deterministic_validation_failed" {
			t.Fatalf("skipped validation was called failed: %+v", chain)
		}
	}
}
