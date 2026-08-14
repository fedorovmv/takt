package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/store"
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
	for _, want := range []string{"run.accepted run=", "run.started run=", "node.started run=", "node.completed run=", "run.completed run="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
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
		"provider.retry.scheduled run=run-1 node=implement provider_attempt=1/3 delay=2s not_before=2026-08-13T12:00:02Z kind=provider_unavailable fingerprint=abc",
		"provider.retry.ready run=run-1 node=implement provider_attempt=2/3",
		"provider.retry.exhausted run=run-1 node=implement provider_attempts=3 max_provider_attempts=3 kind=provider_unavailable fingerprint=abc",
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
	activity.record(flowActivityRecord{RunID: "child-1", NodeID: "review", Attempt: 2, LastActivity: "provider.streaming", LastActivityAt: now.Add(-4 * time.Second), IdleTimeout: 5 * time.Minute, Active: true})
	var trace []string
	traceFlowRunningNodes(func(line string) { trace = append(trace, line) }, state, activity, now)
	if got, want := strings.Join(trace, "\n"), "node.active run=child-1 node=review attempt=2 idle=4s idle_limit=5m0s last_activity=provider.streaming awaiting=provider_stream\nnode.active run=run-1 node=active attempt=1 idle=2m26s idle_limit=5m0s last_activity=tool.completed(write) awaiting=provider_response"; got != want {
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
		"child_run.failed run=child-a parent=root code=protocol",
		"child_node.errored run=child-a node=review code=protocol",
		"child_run.cancelled run=child-b parent=root code=cancelled",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
}

func TestTraceEvaluationAssistantEventShowsSafeProgress(t *testing.T) {
	var trace []string
	write := func(line string) { trace = append(trace, line) }
	traceEvaluationAssistantEvent(write, "child-1", "implement", assistant.Event{Type: assistant.EventToolStarted, Tool: "read", Input: json.RawMessage(`{"path":"internal/du/du.go"}`)})
	traceEvaluationAssistantEvent(write, "child-1", "implement", assistant.Event{Type: assistant.EventMessage, Message: strings.Repeat("result ", 40)})
	joined := strings.Join(trace, "\n")
	for _, want := range []string{`assistant.tool.started run=child-1 node=implement tool=read model= session= path="internal/du/du.go"`, `assistant.message run=child-1 node=implement model= session= text="result result`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("trace missing %q:\n%s", want, joined)
		}
	}
	if len(trace[1]) > 260 {
		t.Fatalf("message preview is too large: %d", len(trace[1]))
	}
}

func TestTraceEvaluationAssistantFailureShowsIdleTimeout(t *testing.T) {
	var trace []string
	traceEvaluationAssistantEvent(func(line string) { trace = append(trace, line) }, "run-1", "review", assistant.Event{
		Type: assistant.EventFailed, Message: "assistant idle timeout: node idle timeout exceeded", SessionID: "session-1",
	})
	if got := strings.Join(trace, "\n"); !strings.Contains(got, `assistant.failed run=run-1 node=review session=session-1 error="assistant idle timeout: node idle timeout exceeded"`) {
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
	result, err := (evaluationEngine{}).pollFlowCase(ctx, app, started.RunID, "yes", nil, nil)
	if err != context.Canceled || !result.ContextCancelled || len(result.States) == 0 || result.States[0].Status != store.RunCancelled {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
