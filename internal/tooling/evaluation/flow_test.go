package evaluation

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
	"takt/internal/validation"
)

func TestRunFlowUsesLexicalCaseRepeatOrderAndPreflightsBeforeCallback(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "b", "a")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	var calls []FlowCaseRunRequest
	report, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, Repeat: 2, HostPATH: "host-path",
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			calls = append(calls, request)
			return FlowCaseRunResult{States: []*store.RunState{{
				ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace,
				CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0), Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{},
			}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := flowRequests(calls), []string{"a:repeat-001", "a:repeat-002", "b:repeat-001", "b:repeat-002"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("callback order=%v want=%v", got, want)
	}
	if report.Mode != "flow" || report.Summary.Total != 4 || report.Environment.PATHSHA256 == "" || report.Environment.OracleMetadataSHA256 == "" {
		t.Fatalf("report=%+v", report)
	}
	for _, request := range calls {
		if request.Selector != filepath.Join(root, "flow.yaml") || request.ConfigPath == "" || request.InputValue == "" || request.ApprovalAnswer != "approve" {
			t.Fatalf("request=%+v", request)
		}
	}
}

func TestRunFlowSkipsValidatorForPausedAndCallerCancellation(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	ctx, cancel := context.WithCancel(context.Background())
	report, err := RunFlow(ctx, FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root,
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			cancel()
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCancelled, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}, ContextCancelled: true}, nil
		},
	})
	if report == nil || len(report.Runs) != 1 || report.Runs[0].Validation == nil || report.Runs[0].Validation.ErrorCode != "validator_cancelled" {
		t.Fatalf("report=%+v", report)
	}
	if err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}

func TestRunFlowPausedWritesPartialReportWithoutCleanup(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	cleaned := false
	report, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root,
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunPaused, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}, Cleanup: func(context.Context) (*store.RunState, error) { cleaned = true; return nil, nil }}, nil
		},
	})
	if err == nil || report == nil || len(report.Runs) != 1 || report.Runs[0].Validation == nil || report.Runs[0].Validation.ErrorCode != "run_paused" || cleaned {
		t.Fatalf("report=%+v err=%v cleaned=%v", report, err, cleaned)
	}
	if _, readErr := os.ReadFile(filepath.Join(root, "out", "report.json")); readErr != nil {
		t.Fatal(readErr)
	}
}

func TestFinishFlowReportIsIdempotentAcrossPartialWrites(t *testing.T) {
	report := &SuiteReport{Mode: "flow", Summary: newSummary(), Runs: []RunRecord{
		{CaseID: "valid", Mode: "flow", Status: store.RunCompleted, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: true}}, Nodes: map[string]NodeRecord{}},
		{CaseID: "invalid", Mode: "flow", Status: store.RunFailed, Validation: &FlowValidationRecord{Status: "completed", Result: &validation.Result{Valid: false}}, Nodes: map[string]NodeRecord{}},
	}}
	for i := range report.Runs {
		ClassifyFlowRecord(&report.Runs[i])
		addSummary(&report.Summary, report.Runs[i])
	}
	finishFlowReport(report)
	first := [3]int{report.Summary.StableValidCases, report.Summary.StableInvalidCases, report.Summary.UnstableCases}
	finishFlowReport(report)
	if got := [3]int{report.Summary.StableValidCases, report.Summary.StableInvalidCases, report.Summary.UnstableCases}; got != first {
		t.Fatalf("first=%v after second finish=%v", first, got)
	}
}

func TestFlowAuthoritativeRequestIncludesSCM(t *testing.T) {
	request := flowAuthoritativeRequest(FlowCase{ID: "case", SCMPath: "/scm", ExpectedPath: "/expected"}, 2, &store.RunState{ID: "run", Status: store.RunFailed, ExecutionWorkspace: "/workspace"}, &PreparedFlowRepeat{BaselineWorkspace: "/baseline"}, map[string]string{"run": "/artifacts"})
	if request.ExternalState == nil || request.ExternalState.SCMDir != "/scm" || request.Run.ArtifactsDir != "/artifacts" {
		t.Fatalf("request=%+v", request)
	}
}

func TestRunFlowLoadsConfigBeforeCallback(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	called := false
	_, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root,
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			called = true
			if err := os.Remove(request.ConfigPath); err != nil {
				t.Fatal(err)
			}
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
		},
	})
	if !called || err != nil {
		t.Fatalf("called=%v err=%v", called, err)
	}
}

func TestRunFlowBenchmarkIdentityIncludesValidatorVersion(t *testing.T) {
	run := func(id, version string) string {
		root, suitePath := writeFlowRunSuite(t, "case")
		suite, err := os.ReadFile(suitePath)
		if err != nil {
			t.Fatal(err)
		}
		suite = []byte(strings.Replace(strings.Replace(string(suite), "id: test", "id: "+id, 1), "version: '1'", "version: '"+version+"'", 1))
		if err := os.WriteFile(suitePath, suite, 0644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
		report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		return report.Benchmark.Fingerprint
	}
	if first, second := run("test", "1"), run("other", "2"); first == second {
		t.Fatal("validator identity did not affect benchmark fingerprint")
	}
}

func TestRunFlowDetectsStrategyDriftBeforeCleanup(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	cleaned := 0
	report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, Repeat: 2, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		fingerprint := "one"
		if strings.Contains(request.Workspace, "repeat-002") {
			fingerprint = "two"
		}
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, WorkflowFingerprint: fingerprint, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}, Cleanup: func(context.Context) (*store.RunState, error) { cleaned++; return nil, nil }}, nil
	}})
	if err == nil || !strings.Contains(err.Error(), "strategy_identity_drift") || report == nil || len(report.Runs) != 2 || cleaned != 1 || report.Strategy.WorkflowFingerprint != "one" {
		t.Fatalf("report=%+v err=%v cleaned=%d", report, err, cleaned)
	}
}

func flowRequests(requests []FlowCaseRunRequest) []string {
	out := make([]string, len(requests))
	for i, request := range requests {
		out[i] = filepath.Base(filepath.Dir(filepath.Dir(request.Workspace))) + ":" + filepath.Base(filepath.Dir(request.Workspace))
	}
	return out
}

func writeFlowRunSuite(t *testing.T, ids ...string) (string, string) {
	t.Helper()
	root := t.TempDir()
	cases := filepath.Join(root, "cases")
	if err := os.MkdirAll(cases, 0755); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		caseRoot := filepath.Join(cases, id)
		for _, dir := range []string{filepath.Join(caseRoot, "workspace")} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
		}
		mustWrite(t, filepath.Join(caseRoot, "workspace", "main.txt"), id, 0644)
		mustWrite(t, filepath.Join(caseRoot, "input.md"), "input "+id, 0644)
		mustWrite(t, filepath.Join(caseRoot, "expected.yaml"), "oracle: {expected: true}\n", 0644)
	}
	mustWrite(t, filepath.Join(root, "flow.yaml"), "name: test\nnodes:\n  - id: done\n    bash: true\n", 0644)
	mustWrite(t, filepath.Join(root, "config.yaml"), "apiVersion: takt/v1alpha1\nkind: Config\n", 0644)
	mustWrite(t, filepath.Join(root, "validator"), "validator", 0755)
	suitePath := filepath.Join(root, "suite.yaml")
	mustWrite(t, suitePath, fmt.Sprintf("version: %s\nworkflow: flow.yaml\nconfig: config.yaml\ncases: {directory: cases}\napprovals: {default: approve}\nvalidator:\n  id: test\n  version: '1'\n  command: [%q, %q, %q]\n  path: validator\n  timeout: 10s\n  max_output_bytes: 4096\n", FlowSuiteVersion, os.Args[0], "-test.run=TestFlowValidatorHelperProcess", "--"), 0644)
	return root, suitePath
}
