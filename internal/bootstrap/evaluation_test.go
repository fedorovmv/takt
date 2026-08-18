package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/store"
	"takt/internal/tooling"
	"takt/internal/tooling/evaluation"
)

func TestFlowProviderUnavailableRecoversWithSamePiSession(t *testing.T) {
	root, suitePath := writeProviderUnavailableFlowSuite(t, map[string]string{"case": "ordinary input"})
	prefix := filepath.Join(t.TempDir(), "provider")
	writeProviderUnavailableFlowConfig(t, root, "provider-sequence", "--fake-state-prefix", prefix, "--fake-failures", "2")

	report, err := evaluation.RunFlow(context.Background(), evaluation.FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, HostPATH: os.Getenv("PATH"),
		CaseRunner: (evaluationEngine{}).runFlowCase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 1 || report.Runs[0].Validation == nil || report.Runs[0].Validation.Result == nil || !report.Runs[0].Validation.Result.Valid {
		t.Fatalf("report=%+v", report)
	}
	for _, node := range report.Runs[0].Nodes {
		if len(node.Executions) != 3 {
			continue
		}
		for _, execution := range node.Executions {
			if execution.SessionID != "fake-pi-session-1" {
				t.Fatalf("session was not preserved: %+v", node.Executions)
			}
		}
		return
	}
	t.Fatalf("executions=%+v", report.Runs[0].Nodes)
}

func TestEvaluationStatsReadsRunningProgress(t *testing.T) {
	dir := t.TempDir()
	started := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	progress := &evaluation.FlowProgress{
		ReportVersion: evaluation.FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir,
		StartedAt: started, UpdatedAt: started.Add(5 * time.Second), TotalRuns: 2,
		Runtime: evaluation.FlowRuntimeProgress{RunningNodes: []string{}, InputTokens: 9, OutputTokens: 3, Timings: &evaluation.FlowRuntimeTimings{Assistant: evaluation.FlowAssistantTimings{WaitMS: 4000}}},
		Results: evaluation.FlowProgressResults{},
	}
	if err := evaluation.WriteFlowProgress(dir, progress); err != nil {
		t.Fatal(err)
	}
	value, err := (evaluationEngine{}).Stats(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := value.(*evaluation.EvaluationStats)
	if !ok || stats.Status != "running" || stats.Complete || stats.InputTokens != 9 || stats.Timings == nil || stats.Timings.Assistant.WaitMS != 4000 {
		t.Fatalf("stats=%#v", value)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (evaluationEngine{}).Stats(context.Background(), dir); err == nil {
		t.Fatal("malformed checkpoint report was hidden by progress fallback")
	}
}

func TestEvaluationStatsOverlaysFailedProgressOnCheckpointedReport(t *testing.T) {
	for _, test := range []struct {
		name          string
		completedRuns int
		totalRuns     int
		wantComplete  bool
	}{
		{name: "failure before all runs", completedRuns: 1, totalRuns: 2, wantComplete: false},
		{name: "gate failure after all runs", completedRuns: 2, totalRuns: 2, wantComplete: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			started := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
			report := evaluation.SuiteReport{ReportVersion: evaluation.ReportVersion, Mode: "flow", Workflow: "flow", OutputDir: dir, StartedAt: started, FinishedAt: started.Add(time.Second), Summary: evaluation.Summary{Total: test.totalRuns, Valid: test.completedRuns}}
			data, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "report.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			progress := &evaluation.FlowProgress{ReportVersion: evaluation.FlowProgressVersion, Status: "failed", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir, StartedAt: started, UpdatedAt: started.Add(2 * time.Second), TotalRuns: test.totalRuns, CompletedRuns: test.completedRuns, Runtime: evaluation.FlowRuntimeProgress{RunningNodes: []string{}}, Results: evaluation.FlowProgressResults{Valid: test.completedRuns}}
			if err := evaluation.WriteFlowProgress(dir, progress); err != nil {
				t.Fatal(err)
			}
			value, err := (evaluationEngine{}).Stats(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			stats, ok := value.(*evaluation.EvaluationStats)
			if !ok || stats.Status != "failed" || stats.Complete != test.wantComplete {
				t.Fatalf("stats=%#v want status=failed complete=%t", value, test.wantComplete)
			}
		})
	}
}

func TestEvaluationStatsRejectsMalformedProgressWithCheckpointedReport(t *testing.T) {
	dir := t.TempDir()
	started := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	report, err := json.Marshal(evaluation.SuiteReport{ReportVersion: evaluation.ReportVersion, Mode: "flow", Workflow: "flow", OutputDir: dir, StartedAt: started, FinishedAt: started.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), report, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, evaluation.FlowProgressFile), []byte(`{"report_version":"takt-flow-evaluation-progress/v1alpha1","status":"running","suite":"suite.yaml","workflow":"flow","output_dir":"out","started_at":"2026-08-18T12:00:00Z","updated_at":"2026-08-18T12:00:01Z","total_runs":1,"completed_runs":0,"runtime":{"running_nodes":[],"timings":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (evaluationEngine{}).Stats(context.Background(), dir); err == nil {
		t.Fatal("malformed progress was hidden by checkpointed report")
	}
}

func TestEvaluationStatsIncludesActivePhaseForCheckpointedReport(t *testing.T) {
	dir := t.TempDir()
	started := time.Now().UTC().Add(-10 * time.Second)
	report, err := json.Marshal(evaluation.SuiteReport{ReportVersion: evaluation.ReportVersion, Mode: "flow", Workflow: "flow", OutputDir: dir, StartedAt: started, FinishedAt: started.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), report, 0o644); err != nil {
		t.Fatal(err)
	}
	progress := &evaluation.FlowProgress{
		ReportVersion: evaluation.FlowProgressVersion, Status: "running", Suite: "suite.yaml", Workflow: "flow", OutputDir: dir,
		StartedAt: started, UpdatedAt: time.Now().UTC(), TotalRuns: 1, Current: &evaluation.FlowProgressCurrent{CaseID: "case", Repeat: 1, Ordinal: 1, Phase: "workflow", PhaseStartedAt: time.Now().UTC().Add(-2 * time.Second)},
		Runtime: evaluation.FlowRuntimeProgress{RunningNodes: []string{}, Timings: &evaluation.FlowRuntimeTimings{Phases: evaluation.FlowPhaseTimings{WorkflowMS: 1000}}},
	}
	if err := evaluation.WriteFlowProgress(dir, progress); err != nil {
		t.Fatal(err)
	}
	value, err := (evaluationEngine{}).Stats(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	stats, ok := value.(*evaluation.EvaluationStats)
	if !ok || stats.Timings == nil || stats.Timings.Phases.WorkflowMS <= 1000 {
		t.Fatalf("stats=%#v", value)
	}
}

func TestEvaluationAnalyzeRequiresExistingConfigBeforeRun(t *testing.T) {
	output := filepath.Join(t.TempDir(), "evaluation")
	result, err := (evaluationEngine{}).Analyze(context.Background(), tooling.EvaluationAnalyzeRequest{OutputDir: output, ConfigPath: filepath.Join(output, "missing.yaml")})
	if result != nil || err == nil || !strings.Contains(err.Error(), "analysis config") {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestFlowProviderUnavailableSuiteContinuesToFollowingCase(t *testing.T) {
	root, suitePath := writeProviderUnavailableFlowSuite(t, map[string]string{
		"a-outage": "TAKT_FAKE_PROVIDER_EXHAUSTED",
		"b-normal": "ordinary input",
	})
	writeProviderUnavailableFlowConfig(t, root, "provider-by-prompt")

	report, err := evaluation.RunFlow(context.Background(), evaluation.FlowRunOptions{
		SuitePath: suitePath, OutputDir: filepath.Join(root, "out"), InvocationWorkspace: root, HostPATH: os.Getenv("PATH"),
		CaseRunner: (evaluationEngine{}).runFlowCase,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Runs) != 2 || report.Runs[0].Outcome != "infrastructure_error" || report.Runs[1].Outcome != "true_accept" {
		t.Fatalf("runs=%+v", report.Runs)
	}
	flow := report.Summary.Flow
	if flow == nil || flow.InfrastructureErrors != 1 || flow.EvaluatedRuns != 1 || report.Summary.QualityRuns != 1 || flow.ValidRate == nil || *flow.ValidRate != 1 || flow.FalseAcceptRate == nil || *flow.FalseAcceptRate != 0 || flow.FalseRejectRate == nil || *flow.FalseRejectRate != 0 {
		t.Fatalf("summary=%+v", report.Summary)
	}
}

func writeProviderUnavailableFlowSuite(t *testing.T, inputs map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for id, input := range inputs {
		caseRoot := filepath.Join(root, "cases", id)
		if err := os.MkdirAll(filepath.Join(caseRoot, "workspace"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseRoot, "workspace", "main.txt"), []byte(id), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseRoot, "input.md"), []byte(input), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(caseRoot, "expected.yaml"), []byte("oracle: {expected: true}\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "flow.yaml"), []byte("name: provider-unavailable\nprovider: pi\nmodel: fake\nnodes:\n  - id: implement\n    prompt: $ARGUMENTS\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "validator"), []byte("#!/bin/sh\ncat >/dev/null\nprintf '%s\\n' '{\"protocol_version\":\"takt-validation/v1alpha1\",\"type\":\"validation_result\",\"valid\":true}'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	suitePath := filepath.Join(root, "suite.yaml")
	flowCompletion := "1"
	if len(inputs) > 1 {
		flowCompletion = "0.5"
	}
	suite := "version: takt-flow-evaluation/v1alpha1\nworkflow: flow.yaml\nconfig: config.yaml\ncases: {directory: cases}\nvalidator:\n  id: provider-test\n  version: '1'\n  command: [./validator]\n  path: validator\n  timeout: 10s\n  max_output_bytes: 4096\ngates:\n  flow_completion_rate: {min: " + flowCompletion + "}\n"
	if err := os.WriteFile(suitePath, []byte(suite), 0644); err != nil {
		t.Fatal(err)
	}
	return root, suitePath
}

func writeProviderUnavailableFlowConfig(t *testing.T, root, fakeCase string, args ...string) {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "takt-fake-pi")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, "./internal/testsupport/cmd/takt-fake-pi")
	build.Dir = projectRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake Pi: %v: %s", err, output)
	}
	values := append([]string{"--fake-case", fakeCase}, args...)
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = `"` + strings.ReplaceAll(value, `"`, `\\"`) + `"`
	}
	config := "apiVersion: takt/v1alpha1\nkind: Config\ndefault_assistant: pi\nmodels:\n  fake: {provider: openai, id: fake-model}\nassistants:\n  pi:\n    type: pi\n    binary: " + strconv.Quote(binary) + "\n    args: [" + strings.Join(quoted, ", ") + "]\n    project_trust: approve\n"
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestFlowEvaluationTraceReportsDurableEvents(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-trace\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var trace []string
	_, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{
		Workspace: workspace, Selector: workflow, ConfigPath: config,
		Trace: func(line string) { trace = append(trace, line) },
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(trace, "\n")
	for _, want := range []string{"| accepted | id=run-", "| run started", "NODE done#1 | started", "NODE done#1 | completed", "| run completed"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestFlowEvaluationPollWaitsForAcceptedRunState(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := New(workspace, config)
	if err != nil {
		t.Fatal(err)
	}
	runID := "run-delayed-state"
	committed := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		state := &store.RunState{ID: runID, Status: store.RunCompleted, Nodes: map[string]*store.NodeState{}, Approvals: map[string]string{}}
		committed <- (store.FS{Workspace: workspace}).Commit(state, store.Event{Type: "run.completed"})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := (evaluationEngine{}).pollFlowCase(ctx, app, runID, "", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-committed; err != nil {
		t.Fatal(err)
	}
	if len(result.States) != 1 || result.States[0].Status != store.RunCompleted {
		t.Fatalf("result=%#v", result)
	}
}

func TestProviderRetryTraceShowsRunNodeAndAttempts(t *testing.T) {
	var trace []string
	write := func(line string) { trace = append(trace, line) }
	traceEvaluationEvent(write, "run-1", store.Event{Type: "provider.retry.scheduled", NodeID: "implement", Data: map[string]any{"scope": "provider", "provider_attempt": 1, "max_provider_attempts": 3, "delay": "2s", "not_before": "2026-08-13T12:00:02Z", "kind": "provider_unavailable", "fingerprint": "abc"}})
	traceEvaluationEvent(write, "run-1", store.Event{Type: "provider.retry.ready", NodeID: "implement", Data: map[string]any{"scope": "provider", "provider_attempt": 2, "max_provider_attempts": 3}})
	traceEvaluationEvent(write, "run-1", store.Event{Type: "provider.retry.exhausted", NodeID: "implement", Data: map[string]any{"scope": "provider", "provider_attempts": 3, "max_provider_attempts": 3, "kind": "provider_unavailable", "fingerprint": "abc"}})
	joined := strings.Join(trace, "\n")
	for _, want := range []string{
		"RUN run-1 · NODE implement | provider retry scheduled | provider_attempt=1/3 delay=2s not_before=2026-08-13T12:00:02Z kind=provider_unavailable fingerprint=abc",
		"RUN run-1 · NODE implement | provider retry ready | provider_attempt=2/3",
		"RUN run-1 · NODE implement | provider retry exhausted | provider_attempts=3 max_provider_attempts=3 kind=provider_unavailable fingerprint=abc",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestTraceFlowRunningNodesReportsCurrentDurableStatus(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 15, 6, 0, time.UTC)
	state := &store.RunState{ID: "run-1", Nodes: map[string]*store.NodeState{
		"pending": {Status: store.NodePending},
		"active":  {Status: store.NodeRunning},
	}}
	activity := newFlowActivityTracker()
	activity.record(flowActivityRecord{RunID: "run-1", NodeID: "active", Attempt: 1, LastActivity: "tool.completed(write)", LastActivityAt: now.Add(-2*time.Minute - 26*time.Second), IdleTimeout: 5 * time.Minute, Active: true})
	activity.recordEvent("child-1", "review", assistant.Event{Type: assistant.EventMessage, Time: now.Add(-5 * time.Second), Usage: &assistant.ProtocolUsage{InputTokens: 128430}}, 5*time.Minute)
	activity.record(flowActivityRecord{RunID: "child-1", NodeID: "review", Attempt: 2, LastActivity: "provider.streaming", LastActivityAt: now.Add(-4 * time.Second), IdleTimeout: 5 * time.Minute, Active: true})
	var trace []string
	traceFlowRunningNodes(func(line string) { trace = append(trace, line) }, state, activity, now)
	if got, want := strings.Join(trace, "\n"), "RUN child-1 · NODE review#2 | active | idle=4s/5m0s context=128 430t last=provider.streaming awaiting=provider_stream\nRUN run-1 · NODE active#1 | active | idle=2m26s/5m0s context=unknown last=tool.completed(write) awaiting=provider_response"; got != want {
		t.Fatalf("trace=%q want=%q", got, want)
	}
	activity.recordEvent("child-1", "review", assistant.Event{Type: assistant.EventCompleted, Time: now}, 5*time.Minute)
	trace = nil
	traceFlowRunningNodes(func(line string) { trace = append(trace, line) }, state, activity, now)
	if got := strings.Join(trace, "\n"); strings.Contains(got, "child-1") {
		t.Fatalf("terminal child remained active: %q", got)
	}
	activity.record(flowActivityRecord{RunID: "run-1", NodeID: "active", LastActivity: "provider.streaming", LastActivityAt: now, Active: true})
	activity.recordEvent("run-1", "active", assistant.Event{Type: assistant.EventToolStarted, Tool: "read", Time: now}, 10*time.Minute)
	if record, _ := activity.get("run-1", "active"); record.IdleTimeout != 5*time.Minute || record.Attempt != 1 {
		t.Fatalf("activity lost node-specific session metadata: %+v", record)
	}
}

func TestTraceFlowChildFailuresFromSnapshot(t *testing.T) {
	states := []*store.RunState{
		{ID: "root", Status: store.RunFailed},
		{ID: "child-b", ParentRunID: "root", Status: store.RunCancelled, ErrorCode: "cancelled", Nodes: map[string]*store.NodeState{"review": {Status: store.NodeCancelled, ErrorCode: "cancelled"}}},
		{ID: "child-a", ParentRunID: "root", Status: store.RunFailed, ErrorCode: "protocol", Nodes: map[string]*store.NodeState{"review": {Status: store.NodeErrored, ErrorCode: "protocol"}}},
	}
	var trace []string
	traceFlowChildSnapshot(func(line string) { trace = append(trace, line) }, states)
	joined := strings.Join(trace, "\n")
	for _, want := range []string{
		"RUN child-a | child failed | id=child-a parent=root code=protocol",
		"RUN child-a · NODE review | errored | code=protocol",
		"RUN child-b | child cancelled | id=child-b parent=root code=cancelled",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestTraceEvaluationAssistantEventShowsSafeProgress(t *testing.T) {
	var trace []string
	write := func(line string) { trace = append(trace, line) }
	formatter := newFlowTraceContext()
	traceEvaluationAssistantEvent(formatter, write, "child-1", "implement", 1, assistant.Event{Type: assistant.EventToolStarted, Tool: "read", Provider: "gemini", SessionID: "session-1", Input: json.RawMessage(`{"path":"internal/du/du.go"}`)})
	traceEvaluationAssistantEvent(formatter, write, "child-1", "implement", 1, assistant.Event{Type: assistant.EventToolCompleted, Tool: "read", Provider: "gemini", SessionID: "session-1", Data: map[string]any{"error": false}})
	traceEvaluationAssistantEvent(formatter, write, "child-1", "implement", 1, assistant.Event{Type: assistant.EventMessage, Message: strings.Repeat("result ", 40)})
	joined := strings.Join(trace, "\n")
	for _, want := range []string{`RUN child-1 · NODE implement#1 | read started | path="internal/du/du.go" session=session-1`, `RUN child-1 · NODE implement#1 | read completed | error=false`, `RUN child-1 · NODE implement#1 | message | text="result result`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
	if strings.Count(joined, "session=session-1") != 1 {
		t.Fatalf("session id must be announced once:\n%s", joined)
	}
	if len(trace[2]) > 260 {
		t.Fatalf("message preview is too large: %d", len(trace[2]))
	}
}

func TestTraceEvaluationAssistantEventShowsProviderObservations(t *testing.T) {
	var trace []string
	write := func(line string) { trace = append(trace, line) }
	tracker := newFlowActivityTracker()
	event := assistant.Event{Type: assistant.EventDiagnostic, Time: time.Date(2026, 8, 17, 14, 3, 10, 0, time.UTC), Message: "HTTP 500 unavailable", Data: map[string]any{
		"code": "pi.auto_retry.started", "attempt": 1, "max_attempts": 3, "delay_ms": 2000, "call": 2,
	}}
	tracker.recordEvent("run-1", "implement", event, 15*time.Minute)
	traceEvaluationAssistantEvent(newFlowTraceContext(), write, "run-1", "implement", 1, event)
	traceEvaluationAssistantEvent(newFlowTraceContext(), write, "run-1", "implement", 1, assistant.Event{Type: assistant.EventDiagnostic, Data: map[string]any{
		"code": "pi.message.completed", "call": 2, "wait_ms": 38123, "total_ms": 581000, "stream_ms": 542877,
	}})
	record, _ := tracker.get("run-1", "implement")
	traceFlowActivity(write, record, event.Time.Add(time.Second))
	joined := strings.Join(trace, "\n")
	for _, want := range []string{
		"observation | code=pi.auto_retry.started call=2 retry=1/3 delay=2s error=\"HTTP 500 unavailable\"",
		"observation | code=pi.message.completed call=2 wait=38.123s total=9m41s stream=9m2.877s",
		"last=pi.auto_retry.started awaiting=provider_retry_backoff",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace misses %q:\n%s", want, joined)
		}
	}
}

func TestTraceEvaluationAssistantFailureShowsIdleTimeout(t *testing.T) {
	var trace []string
	traceEvaluationAssistantEvent(newFlowTraceContext(), func(line string) { trace = append(trace, line) }, "run-1", "review", 1, assistant.Event{
		Type: assistant.EventFailed, Message: "assistant idle timeout: node idle timeout exceeded", SessionID: "session-1",
	})
	if got := strings.Join(trace, "\n"); !strings.Contains(got, `RUN run-1 · NODE review#1 | assistant failed | error="assistant idle timeout: node idle timeout exceeded" session=session-1`) {
		t.Fatalf("trace=%q", got)
	}
}

func TestFlowEvaluationCaseReturnsDetachedSnapshotAndDefersCleanup(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-case\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{Workspace: workspace, Selector: workflow, ConfigPath: config})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.States) != 1 || result.States[0].Status != "completed" || len(result.Events) == 0 || result.Cleanup == nil {
		t.Fatalf("result=%#v", result)
	}
	if _, err := result.Cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeFlowRuntimeProgress(t *testing.T) {
	root := &store.RunState{
		ID: "run-1", Status: store.RunRunning, Usage: &store.Usage{InputTokens: 120, OutputTokens: 30, Cost: 0.25},
		Nodes: map[string]*store.NodeState{
			"review":    {Status: store.NodeRunning, Attempts: 2, ProviderAttempts: 3},
			"implement": {Status: store.NodeCompleted, Attempts: 1, ProviderAttempts: 1},
			"route":     {Status: store.NodeRunning, Attempts: 1, ProviderAttempts: 0},
		},
	}
	child := &store.RunState{ID: "child-1", Nodes: map[string]*store.NodeState{"fix": {Status: store.NodeCompleted, Attempts: 2, ProviderAttempts: 2}}}
	activity := newFlowActivityTracker()
	activity.recordEvent("child-1", "review", assistant.Event{Type: assistant.EventMessage, Usage: &assistant.ProtocolUsage{InputTokens: 43439}}, 5*time.Minute)
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventDiagnostic, Time: time.Date(2026, 8, 17, 13, 59, 59, 0, time.UTC), Message: strings.Repeat("x", 600), Data: map[string]any{"code": "pi.auto_retry.started", "attempt": 1, "max_attempts": 3}}, 5*time.Minute)
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventDiagnostic, Time: time.Date(2026, 8, 17, 14, 0, 0, 0, time.UTC), Data: map[string]any{"code": "pi.stream.started", "call": 4}}, 5*time.Minute)
	got := summarizeFlowRuntimeProgress([]*store.RunState{root, child}, activity)
	if got.RunID != "run-1" || got.Status != store.RunRunning || got.TotalNodes != 4 || got.CompletedNodes != 2 || got.NodeAttempts != 6 || got.ProviderAttempts != 6 || got.InputTokens != 120 || got.OutputTokens != 30 || got.Cost != 0.25 {
		t.Fatalf("progress=%+v", got)
	}
	if want := []string{"review", "route"}; !reflect.DeepEqual(got.RunningNodes, want) {
		t.Fatalf("running=%v want=%v", got.RunningNodes, want)
	}
	if !got.ContextKnown || got.ContextTokens != 43439 {
		t.Fatalf("context=%d known=%t", got.ContextTokens, got.ContextKnown)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"assistant_activity"`, `"state":"streaming"`, `"node_id":"review"`, `"call":4`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("progress JSON misses %q: %s", want, encoded)
		}
	}
	if len(got.AssistantActivity) != 1 || len(got.AssistantActivity[0].LastError) > 515 {
		t.Fatalf("progress provider error was not bounded: %+v", got.AssistantActivity)
	}
}

func TestSummarizeFlowRuntimeProgressAggregatesAssistantTimings(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	activity := newFlowActivityTracker()
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventDiagnostic, Time: now, Data: map[string]any{"code": "pi.stream.started", "wait_ms": 1200}}, 5*time.Minute)
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventDiagnostic, Time: now.Add(4 * time.Second), Data: map[string]any{"code": "pi.message.completed", "total_ms": 5000, "stream_ms": 3800}}, 5*time.Minute)
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventToolStarted, Tool: "read", CallID: "call-1", Time: now.Add(5 * time.Second)}, 5*time.Minute)
	activity.recordEvent("run-1", "review", assistant.Event{Type: assistant.EventToolCompleted, Tool: "read", CallID: "call-1", Time: now.Add(5700 * time.Millisecond)}, 5*time.Minute)

	progress := summarizeFlowRuntimeProgress([]*store.RunState{{ID: "run-1", Status: store.RunRunning, Nodes: map[string]*store.NodeState{"review": {Status: store.NodeRunning}}}}, activity)
	if progress.Timings == nil {
		t.Fatal("assistant timings are missing")
	}
	want := (evaluation.FlowAssistantTimings{WaitMS: 1200, StreamMS: 3800, TotalMS: 5000, ToolMS: 700})
	if got := progress.Timings.Assistant; got != want {
		t.Fatalf("assistant timings=%+v want=%+v", got, want)
	}
}

func TestFlowEvaluationPublishesProgressWithoutTrace(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-progress\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var updates []evaluation.FlowRuntimeProgress
	_, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{
		Workspace: workspace, Selector: workflow, ConfigPath: config,
		Progress: func(progress evaluation.FlowRuntimeProgress) (*evaluation.FlowProgress, error) {
			updates = append(updates, progress)
			return &evaluation.FlowProgress{Runtime: progress}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) == 0 || updates[len(updates)-1].Status != store.RunCompleted {
		t.Fatalf("updates=%+v", updates)
	}
}

func TestFlowProgressRefreshDecision(t *testing.T) {
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name                  string
		published             bool
		lastRevision, current uint64
		lastUpdated           time.Time
		want                  bool
	}{
		{name: "first snapshot", want: true},
		{name: "durable revision", published: true, lastRevision: 1, current: 2, lastUpdated: now, want: true},
		{name: "unchanged before interval", published: true, lastRevision: 2, current: 2, lastUpdated: now.Add(-9 * time.Second)},
		{name: "unchanged at interval", published: true, lastRevision: 2, current: 2, lastUpdated: now.Add(-10 * time.Second), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := shouldPublishFlowProgress(test.published, test.lastRevision, test.current, test.lastUpdated, now); got != test.want {
				t.Fatalf("got=%t want=%t", got, test.want)
			}
		})
	}
}

func TestFlowEvaluationPropagatesProgressWriteFailure(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-progress-error\nnodes:\n  - id: done\n    bash: 'true'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := "write progress"
	result, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{
		Workspace: workspace, Selector: workflow, ConfigPath: config,
		Progress: func(evaluation.FlowRuntimeProgress) (*evaluation.FlowProgress, error) { return nil, errors.New(want) },
	})
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err=%v", err)
	}
	if len(result.States) != 1 || !terminalFlowRun(result.States[0].Status) {
		t.Fatalf("progress failure left detached run active: %#v", result.States)
	}
}

func TestFlowEvaluationCaseAnswersWaitingApproval(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-approval\nnodes:\n  - id: approve\n    approval:\n      message: Continue?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (evaluationEngine{}).runFlowCase(context.Background(), evaluation.FlowCaseRunRequest{Workspace: workspace, Selector: workflow, ConfigPath: config, ApprovalAnswer: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.States) != 1 || result.States[0].Status != "completed" || result.States[0].Approvals["approve"] != "yes" {
		t.Fatalf("result=%#v", result)
	}
}

func TestFlowEvaluationCancellationDuringAnswerReturnsSnapshot(t *testing.T) {
	workspace := t.TempDir()
	config := filepath.Join(workspace, "config.yaml")
	workflow := filepath.Join(workspace, "workflow.yaml")
	if err := os.WriteFile(config, []byte("apiVersion: takt/v1alpha1\nkind: Config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workflow, []byte("name: flow-cancel\nnodes:\n  - id: approve\n    approval:\n      message: Continue?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := New(workspace, config)
	if err != nil {
		t.Fatal(err)
	}
	started, err := app.Core.RunService.Start(context.Background(), application.StartRequest{Selector: workflow, ConfigPath: config, Detached: true, KeepWorktree: true})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		state, loadErr := app.Core.RunService.GetRun(started.RunID)
		if loadErr == nil && state.Status == store.RunWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not reach waiting: %v", loadErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (evaluationEngine{}).pollFlowCase(ctx, app, started.RunID, "yes", nil, nil, nil)
	if err != context.Canceled || !result.ContextCancelled || len(result.States) == 0 || result.States[0].Status != store.RunCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
