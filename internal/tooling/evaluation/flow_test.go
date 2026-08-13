package evaluation

import (
	"context"
	"encoding/json"
	"errors"
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

func TestRunFlowTracesCaseStages(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	var trace []string
	_, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, HostPATH: "host-path",
		Trace: func(line string) { trace = append(trace, line) }, AssistantIdleTimeout: 5 * time.Minute,
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			request.Trace("run.accepted run=run")
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(trace, "\n")
	for _, want := range []string{"assistant_idle_timeout=5m0s", "case.prepare case=case repeat=1", "validator.preflight case=case", "run.accepted run=run", "validator.completed case=case", "evidence.written case=case", "report.written path="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestRunFlowDefaultsAssistantIdleTimeout(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	var observed time.Duration
	_, err := RunFlow(context.Background(), FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, HostPATH: "host-path",
		CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
			observed = request.AssistantIdleTimeout
			return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if observed != 5*time.Minute {
		t.Fatalf("assistant idle timeout=%s want=5m", observed)
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
	request := flowAuthoritativeRequest(FlowCase{ID: "case", SCMPath: "/corpus/scm", ExpectedPath: "/expected"}, 2, &store.RunState{ID: "run", Status: store.RunFailed, ExecutionWorkspace: "/workspace"}, &PreparedFlowRepeat{BaselineWorkspace: "/baseline"}, map[string]string{"run": "/artifacts"})
	if request.ExternalState == nil || request.ExternalState.SCMDir != "/workspace/.takt/evals/scm" || request.Run.ArtifactsDir != "/artifacts" {
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

func TestFlowBenchmarkIdentityIncludesPreparedSCMIdentity(t *testing.T) {
	base := flowBenchmarkFingerprint("suite", "cases", "validator", "id", "version", "path", "oracle", 1, time.Minute, []string{"prepared-one"})
	changed := flowBenchmarkFingerprint("suite", "cases", "validator", "id", "version", "path", "oracle", 1, time.Minute, []string{"prepared-two"})
	if base == changed {
		t.Fatal("prepared SCM identity did not affect benchmark fingerprint")
	}
}

func TestFlowBenchmarkIdentityIncludesAssistantIdleTimeout(t *testing.T) {
	base := flowBenchmarkFingerprint("suite", "cases", "validator", "id", "version", "path", "oracle", 1, time.Minute, []string{"prepared"})
	changed := flowBenchmarkFingerprint("suite", "cases", "validator", "id", "version", "path", "oracle", 1, 2*time.Minute, []string{"prepared"})
	if base == changed {
		t.Fatal("assistant idle timeout did not affect benchmark fingerprint")
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

func TestRunFlowRedactsPersistedReport(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	secret := "flow-report-secret"
	t.Setenv("FLOW_REPORT_SECRET", secret)
	mustWrite(t, filepath.Join(root, "config.yaml"), "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  fake:\n    type: mock\n    env:\n      TOKEN: secret://FLOW_REPORT_SECRET\n", 0644)
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunFailed, ExecutionWorkspace: request.Workspace, Error: secret, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, fmt.Errorf("callback %s", secret)
	}})
	if report == nil || (err != nil && !strings.Contains(err.Error(), "flow evaluation gates failed")) {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	data, readErr := os.ReadFile(filepath.Join(root, "out", "report.json"))
	if readErr != nil || strings.Contains(string(data), secret) || !json.Valid(data) {
		t.Fatalf("read=%v json=%v report=%s", readErr, json.Valid(data), data)
	}
}

func TestRunFlowKeepsPriorCaseSecretsRedacted(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "a", "b")
	first, second := "first-flow-secret", "second-flow-secret"
	t.Setenv("FIRST_FLOW_SECRET", first)
	t.Setenv("SECOND_FLOW_SECRET", second)
	mustWrite(t, filepath.Join(root, "config.yaml"), "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  fake:\n    type: mock\n    env:\n      TOKEN: secret://FIRST_FLOW_SECRET\n", 0644)
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		secret := first
		if strings.Contains(request.Workspace, "/b/") {
			secret = second
		} else {
			mustWrite(t, filepath.Join(root, "config.yaml"), "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  fake:\n    type: mock\n    env:\n      TOKEN: secret://SECOND_FLOW_SECRET\n", 0644)
		}
		return FlowCaseRunResult{States: []*store.RunState{{ID: filepath.Base(request.Workspace), Status: store.RunFailed, ExecutionWorkspace: request.Workspace, Error: secret, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
	}})
	if report == nil || (err != nil && !strings.Contains(err.Error(), "flow evaluation gates failed")) {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	data, readErr := os.ReadFile(filepath.Join(root, "out", "report.json"))
	returned, marshalErr := json.Marshal(report)
	if readErr != nil || marshalErr != nil || strings.Contains(string(data), first) || strings.Contains(string(data), second) || strings.Contains(string(returned), first) || strings.Contains(string(returned), second) {
		t.Fatalf("read=%v marshal=%v report=%s returned=%s", readErr, marshalErr, data, returned)
	}
}

func TestRunFlowReturnsPersistenceErrorAfterCleanupFailure(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	_, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}, Cleanup: func(context.Context) (*store.RunState, error) {
			_ = os.Remove(filepath.Join(root, "out", "report.json"))
			if mkdirErr := os.Mkdir(filepath.Join(root, "out", "report.json"), 0755); mkdirErr != nil {
				t.Fatal(mkdirErr)
			}
			return nil, errors.New("cleanup failed")
		}}, nil
	}})
	if err == nil || !strings.Contains(err.Error(), "persist cleanup flow report") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunFlowRejectsExistingOrOverlappingOutputBeforeCallback(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	for _, output := range []string{filepath.Join(root, "existing"), root, filepath.Join(root, "cases", "output")} {
		if filepath.Base(output) == "existing" {
			if err := os.Mkdir(output, 0755); err != nil {
				t.Fatal(err)
			}
		}
		called := false
		_, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: output, InvocationWorkspace: root, CaseRunner: func(context.Context, FlowCaseRunRequest) (FlowCaseRunResult, error) {
			called = true
			return FlowCaseRunResult{}, nil
		}})
		if err == nil || called {
			t.Fatalf("output=%s err=%v called=%v", output, err, called)
		}
	}
}

func TestRunFlowAllowsOutputBelowInvocationWorkspace(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	called := false
	_, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(root, ".takt", "evals", "out"), InvocationWorkspace: root, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		called = true
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
	}})
	if err != nil || !called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestRunFlowRejectsOutputSymlinkedIntoCases(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	link := filepath.Join(root, "out-link")
	if err := os.Symlink(filepath.Join(root, "cases"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	called := false
	_, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(link, "new"), InvocationWorkspace: root, CaseRunner: func(context.Context, FlowCaseRunRequest) (FlowCaseRunResult, error) {
		called = true
		return FlowCaseRunResult{}, nil
	}})
	if err == nil || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
}

func TestRunFlowReportsOutputRelativeToSymlinkedInvocation(t *testing.T) {
	root, suitePath := writeFlowRunSuite(t, "case")
	invocation := filepath.Join(t.TempDir(), "invocation")
	if err := os.Symlink(root, invocation); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, OutputDir: filepath.Join(invocation, ".takt", "evals", "out"), InvocationWorkspace: invocation, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
	}})
	if err != nil || report == nil || report.OutputDir != ".takt/evals/out" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestRunFlowDerivesTimestampedDefaultOutput(t *testing.T) {
	suiteRoot, suitePath := writeFlowRunSuite(t, "case")
	invocation := t.TempDir()
	now := time.Date(2026, 8, 13, 12, 34, 56, 123456789, time.FixedZone("other", 3*60*60))
	t.Setenv("TAKT_FLOW_VALIDATOR_MODE", validFlowEnvelope)
	report, err := RunFlow(context.Background(), FlowRunOptions{SuitePath: suitePath, InvocationWorkspace: invocation, Now: func() time.Time { return now }, CaseRunner: func(_ context.Context, request FlowCaseRunRequest) (FlowCaseRunResult, error) {
		return FlowCaseRunResult{States: []*store.RunState{{ID: "run", Status: store.RunCompleted, ExecutionWorkspace: request.Workspace, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(".takt", "evals", filepath.Base(suiteRoot), "20260813T093456.123456789Z")
	if report.OutputDir != want {
		t.Fatalf("output_dir=%q want %q", report.OutputDir, want)
	}
	if _, err := os.Stat(filepath.Join(invocation, want, "report.json")); err != nil {
		t.Fatal(err)
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
