package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/domainadapter"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/tooling"
	"takt/internal/tooling/evaluation"
)

type evaluationEngine struct{ providers assistant.Registry }

func (e evaluationEngine) Run(ctx context.Context, req tooling.EvaluationRunRequest) (any, error) {
	return evaluation.Run(ctx, evaluation.RunOptions{
		ExecutionFactory: e.executionFactory,
		WorkflowPath:     req.WorkflowPath, ConfigPath: req.ConfigPath, CasesDir: req.CasesDir,
		CaseManifestPath: req.CaseManifestPath, WorkspaceTemplate: req.WorkspaceTemplate,
		OutputDir: req.OutputDir, Repeat: req.Repeat, ApprovalAnswer: req.ApprovalAnswer, Replace: req.Replace,
		StrategyID: req.StrategyID, BenchmarkID: req.BenchmarkID, QualityNode: req.QualityNode,
		GenerationNode: req.GenerationNode, ValidatorID: req.ValidatorID, ValidatorVersion: req.ValidatorVersion,
		ValidatorPath: req.ValidatorPath, ModelPreset: req.ModelPreset, ModelOverrides: req.ModelOverrides,
	})
}

func (e evaluationEngine) Benchmark(ctx context.Context, req tooling.EvaluationBenchmarkRequest) (any, error) {
	return evaluation.RunMatrix(ctx, evaluation.MatrixRunOptions{ExecutionFactory: e.executionFactory, MatrixPath: req.MatrixPath, OutputDir: req.OutputDir, Repeat: req.Repeat, Replace: req.Replace})
}

func (e evaluationEngine) Flow(ctx context.Context, req tooling.FlowEvaluationRequest) (any, error) {
	legacy, err := evaluation.IsLegacyFlowSuite(req.SuitePath)
	if err != nil {
		return nil, err
	}
	hostPATH := os.Getenv("PATH")
	if hostPATH == "" {
		return nil, fmt.Errorf("flow evaluation requires non-empty host PATH")
	}
	if !legacy {
		return e.runOrdinaryEvaluation(ctx, req, hostPATH)
	}
	if req.Deprecation != nil {
		req.Deprecation("takt-flow-evaluation/v1alpha1 uses the deprecated legacy runner")
	}
	return evaluation.RunFlow(ctx, evaluation.FlowRunOptions{
		SuitePath: req.SuitePath, CaseID: req.CaseID, OutputDir: req.OutputDir, InvocationWorkspace: req.InvocationWorkspace,
		Repeat: req.Repeat, KeepWorkspaces: req.KeepWorkspaces, ModelPreset: req.ModelPreset, ModelOverrides: req.ModelOverrides, Now: time.Now, HostPATH: hostPATH, CaseRunner: e.runFlowCase, Trace: req.Trace, AssistantIdleTimeout: req.AssistantIdleTimeout,
	})
}

func (e evaluationEngine) runOrdinaryEvaluation(ctx context.Context, req tooling.FlowEvaluationRequest, hostPATH string) (any, error) {
	gates := make(map[string]evaluation.EvaluationGate, len(req.Gates))
	for name, gate := range req.Gates {
		gates[name] = evaluation.EvaluationGate{Min: gate.Min, Max: gate.Max}
	}
	prepared, err := evaluation.PrepareEvaluationInput(ctx, evaluation.EvaluationInputOptions{
		WorkflowPath: req.SuitePath, Target: req.Target, ApprovalAnswer: req.ApprovalAnswer, ConfigPath: req.ConfigPath, CasesDir: req.CasesDir,
		CaseID: req.CaseID, OutputDir: req.OutputDir, Workspace: req.InvocationWorkspace, Repeat: req.Repeat,
		Gates: gates, ModelPreset: req.ModelPreset, ModelOverrides: req.ModelOverrides, Now: time.Now, HostPATH: hostPATH,
	})
	if err != nil {
		return nil, err
	}
	activity := newFlowActivityTracker()
	traceContext := newFlowTraceContext()
	app, err := newApp(req.InvocationWorkspace, prepared.ConfigPath, func(runID, nodeID string, event assistant.Event) {
		activity.recordEvent(runID, nodeID, event, req.AssistantIdleTimeout)
		record, _ := activity.get(runID, nodeID)
		traceEvaluationAssistantEvent(traceContext, req.Trace, runID, nodeID, record.Attempt, event)
	}, func(runID, nodeID, kind string) {
		activity.record(flowActivityRecord{RunID: runID, NodeID: nodeID, LastActivity: kind, LastActivityAt: time.Now(), Active: true})
	}, req.AssistantIdleTimeout)
	if err != nil {
		return nil, err
	}
	started, err := app.Core.RunService.Start(ctx, application.StartRequest{
		Selector: req.SuitePath, Input: string(prepared.JSON), ConfigPath: prepared.ConfigPath,
		ModelPreset: prepared.ModelPreset, ModelOverrides: req.ModelOverrides, Detached: true, KeepWorktree: req.KeepWorkspaces,
	})
	if err != nil {
		return nil, err
	}
	if started.RunID == "" {
		return nil, fmt.Errorf("evaluation start returned no run ID")
	}
	traceScoped(req.Trace, started.RunID, "", 0, "accepted", "id="+started.RunID)
	snapshot, pollErr := e.pollFlowCase(ctx, app, started.RunID, req.ApprovalAnswer, req.Trace, activity, nil)
	stats, statsErr := app.Core.RunService.Stats(application.RunStatsQuery{RunID: started.RunID, CheckGates: true})
	cleanupErr := e.cleanupOrdinaryEvaluation(ctx, app, prepared, snapshot, req.KeepWorkspaces)
	if cleanupErr != nil {
		return stats, errors.Join(pollErr, statsErr, cleanupErr)
	}
	if statsErr != nil {
		return nil, errors.Join(pollErr, statsErr)
	}
	if pollErr != nil {
		return stats, pollErr
	}
	if len(snapshot.States) == 0 || snapshot.States[0] == nil || snapshot.States[0].Status != store.RunCompleted {
		status, message := "unknown", ""
		if len(snapshot.States) > 0 && snapshot.States[0] != nil {
			status, message = snapshot.States[0].Status, snapshot.States[0].Error
		}
		return stats, fmt.Errorf("evaluation run %s ended with status %s: %s", started.RunID, status, message)
	}
	return stats, stats.GateFailure()
}

func (e evaluationEngine) cleanupOrdinaryEvaluation(ctx context.Context, app *App, prepared *evaluation.PreparedEvaluationInput, result evaluation.FlowCaseRunResult, keep bool) error {
	if keep || prepared == nil || len(result.States) == 0 || result.States[0] == nil || !terminalFlowRun(result.States[0].Status) {
		return nil
	}
	for _, state := range result.States {
		if state == nil || state.Worktree == nil || !state.Worktree.Enabled || state.Worktree.Removed {
			continue
		}
		if !terminalFlowRun(state.Status) {
			return fmt.Errorf("ordinary evaluation cleanup: run %s is still %s", state.ID, state.Status)
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for index := len(result.States) - 1; index >= 0; index-- {
		state := result.States[index]
		if state == nil || state.Worktree == nil || !state.Worktree.Enabled || state.Worktree.Removed {
			continue
		}
		if _, err := app.Core.WorktreeService.Remove(cleanupCtx, state.ID, true); err != nil {
			return fmt.Errorf("remove ordinary evaluation worktree %s: %w", state.ID, err)
		}
	}
	for _, item := range prepared.Input.Cases {
		repeatRoot := filepath.Join(prepared.OutputDir, "workspaces", item.CaseID, fmt.Sprintf("repeat-%03d", item.Repeat))
		paths := make([]string, 0, 3)
		controlWorkspace, baselineWorkspace := "", ""
		bareRemote := ""
		for _, name := range []string{"control", "baseline", "origin.git"} {
			path := filepath.Join(repeatRoot, name)
			if _, err := os.Lstat(path); err == nil {
				paths = append(paths, path)
				switch name {
				case "control":
					controlWorkspace = path
				case "baseline":
					baselineWorkspace = path
				case "origin.git":
					bareRemote = path
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect ordinary evaluation cleanup path %s: %w", path, err)
			}
		}
		if len(paths) == 0 {
			continue
		}
		if err := evaluation.CleanupFlowRepeat(prepared.OutputDir, evaluation.FlowCleanupPaths{ControlWorkspace: controlWorkspace, BaselineWorkspace: baselineWorkspace, BareRemote: bareRemote, Created: paths}); err != nil {
			return fmt.Errorf("cleanup ordinary evaluation repeat %s#%d: %w", item.CaseID, item.Repeat, err)
		}
	}
	return nil
}

func (e evaluationEngine) Analyze(ctx context.Context, req tooling.EvaluationAnalyzeRequest) (any, error) {
	report, err := evaluation.AnalyzeFlow(ctx, evaluation.AnalysisRunOptions{
		OutputDir: req.OutputDir, ConfigPath: req.ConfigPath, CaseID: req.CaseID,
		Repeat: req.Repeat, ModelPreset: req.ModelPreset, Language: req.Language, Trace: req.Trace,
		Now: time.Now, CaseRunner: func(caseCtx context.Context, flowReq evaluation.FlowCaseRunRequest) (evaluation.FlowCaseRunResult, error) {
			if flowReq.AssistantIdleTimeout <= 0 {
				flowReq.AssistantIdleTimeout = 5 * time.Minute
			}
			return e.runFlowCase(caseCtx, flowReq)
		},
	})
	if report == nil {
		return nil, err
	}
	return report, err
}

func (evaluationEngine) FlowInit(_ context.Context, workflowSelector, output string) (any, error) {
	absOutput, err := filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	if err := evaluation.InitFlowSuite(workflowSelector, absOutput); err != nil {
		return nil, err
	}
	return map[string]any{"output": absOutput, "created": true}, nil
}

func (e evaluationEngine) runFlowCase(ctx context.Context, req evaluation.FlowCaseRunRequest) (evaluation.FlowCaseRunResult, error) {
	activity := newFlowActivityTracker()
	traceContext := newFlowTraceContext()
	app, err := newApp(req.Workspace, req.ConfigPath, func(runID, nodeID string, event assistant.Event) {
		activity.recordEvent(runID, nodeID, event, req.AssistantIdleTimeout)
		record, _ := activity.get(runID, nodeID)
		traceEvaluationAssistantEvent(traceContext, req.Trace, runID, nodeID, record.Attempt, event)
	}, func(runID, nodeID, kind string) {
		activity.record(flowActivityRecord{RunID: runID, NodeID: nodeID, LastActivity: kind, LastActivityAt: time.Now(), Active: true})
	}, req.AssistantIdleTimeout)
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
	started, err := app.Core.RunService.Start(ctx, application.StartRequest{
		Selector: req.Selector, Input: req.InputValue, ConfigPath: req.ConfigPath, ModelPreset: req.ModelPreset, ModelOverrides: req.ModelOverrides,
		Detached: true, KeepWorktree: true,
	})
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
	if started.RunID == "" {
		return evaluation.FlowCaseRunResult{}, fmt.Errorf("flow evaluation start returned no run ID")
	}
	traceScoped(req.Trace, started.RunID, "", 0, "accepted", "id="+started.RunID)
	return e.pollFlowCase(ctx, app, started.RunID, req.ApprovalAnswer, req.Trace, activity, req.Progress)
}

type flowActivityRecord struct {
	RunID, NodeID, LastActivity string
	Attempt                     int
	ContextTokens               int
	ContextKnown                bool
	LastActivityAt              time.Time
	IdleTimeout                 time.Duration
	Active                      bool
	ProviderState               string
	ProviderStateSince          time.Time
	ProviderCall                int
	ProviderRetry               int
	ProviderMaxRetries          int
	ProviderDelayMS             int
	LastProviderError           string
}

type flowActivityTracker struct {
	mu                   sync.RWMutex
	records              map[string]flowActivityRecord
	assistantTimings     evaluation.FlowAssistantTimings
	assistantCallTimings map[string]flowAssistantCallTiming
	assistantInvocations map[string]int
	toolStarted          map[string]time.Time
}

func newFlowActivityTracker() *flowActivityTracker {
	return &flowActivityTracker{records: map[string]flowActivityRecord{}, assistantCallTimings: map[string]flowAssistantCallTiming{}, assistantInvocations: map[string]int{}, toolStarted: map[string]time.Time{}}
}

type flowAssistantCallTiming struct {
	WaitMS   int64
	StreamMS int64
	TotalMS  int64
}

func (t *flowActivityTracker) record(record flowActivityRecord) {
	if t == nil {
		return
	}
	t.mu.Lock()
	previous := t.records[record.RunID+"\x00"+record.NodeID]
	if record.Attempt == 0 {
		record.Attempt = previous.Attempt
	}
	if record.IdleTimeout == 0 {
		record.IdleTimeout = previous.IdleTimeout
	}
	if !record.ContextKnown && previous.ContextKnown {
		record.ContextTokens, record.ContextKnown = previous.ContextTokens, true
	}
	if record.ProviderState == "" {
		record.ProviderState = previous.ProviderState
		record.ProviderStateSince = previous.ProviderStateSince
	}
	if record.ProviderCall == 0 {
		record.ProviderCall = previous.ProviderCall
	}
	if record.ProviderRetry == 0 {
		record.ProviderRetry = previous.ProviderRetry
	}
	if record.ProviderMaxRetries == 0 {
		record.ProviderMaxRetries = previous.ProviderMaxRetries
	}
	if record.ProviderDelayMS == 0 {
		record.ProviderDelayMS = previous.ProviderDelayMS
	}
	if record.LastProviderError == "" {
		record.LastProviderError = previous.LastProviderError
	}
	t.records[record.RunID+"\x00"+record.NodeID] = record
	t.mu.Unlock()
}

func (t *flowActivityTracker) recordEvent(runID, nodeID string, event assistant.Event, fallback time.Duration) {
	var idle time.Duration
	if value, ok := event.Data["idle_timeout"].(string); ok && value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			idle = parsed
		}
	}
	if idle == 0 && (event.Type == assistant.EventSessionStarted || event.Type == assistant.EventSessionResumed) {
		idle = fallback
	}
	at := event.Time
	if at.IsZero() {
		at = time.Now()
	}
	t.recordAssistantTiming(runID, nodeID, event, at)
	active := event.Type != assistant.EventCompleted && event.Type != assistant.EventFailed
	record := flowActivityRecord{RunID: runID, NodeID: nodeID, Attempt: traceAttempt(event.Data["attempt"]), LastActivity: traceActivityName(event), LastActivityAt: at, IdleTimeout: idle, Active: active}
	if event.Type == assistant.EventMessage && event.Usage != nil {
		record.ContextTokens, record.ContextKnown = event.Usage.InputTokens, true
	}
	if event.Type == assistant.EventDiagnostic {
		record.Attempt = 0
		record.ProviderState = providerObservationState(fmt.Sprint(event.Data["code"]), event.Data["success"])
		if record.ProviderState != "" {
			record.ProviderStateSince = at
			record.ProviderCall = traceAttempt(event.Data["call"])
			record.ProviderRetry = traceAttempt(event.Data["attempt"])
			record.ProviderMaxRetries = traceAttempt(event.Data["max_attempts"])
			record.ProviderDelayMS = traceAttempt(event.Data["delay_ms"])
			record.LastProviderError = strings.Join(strings.Fields(event.Message), " ")
			if runes := []rune(record.LastProviderError); len(runes) > 512 {
				record.LastProviderError = string(runes[:512]) + "..."
			}
		}
	}
	t.record(record)
}

func (t *flowActivityTracker) recordAssistantTiming(runID, nodeID string, event assistant.Event, at time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch event.Type {
	case assistant.EventSessionStarted, assistant.EventSessionResumed:
		t.assistantInvocations[flowAssistantInvocationKey(runID, nodeID)]++
	case assistant.EventDiagnostic:
		code, _ := event.Data["code"].(string)
		call := traceAttempt(event.Data["call"])
		if call > 0 && (code == "pi.stream.started" || code == "pi.message.completed") {
			generation := t.assistantInvocations[flowAssistantInvocationKey(runID, nodeID)]
			t.recordCumulativeAssistantTiming(runID, nodeID, event.SessionID, generation, call, code, event.Data)
			return
		}
		switch code {
		case "pi.stream.started":
			t.assistantTimings.WaitMS += timingMilliseconds(event.Data["wait_ms"])
		case "pi.message.completed":
			t.assistantTimings.StreamMS += timingMilliseconds(event.Data["stream_ms"])
			t.assistantTimings.TotalMS += timingMilliseconds(event.Data["total_ms"])
		}
	case assistant.EventToolStarted:
		if event.CallID != "" {
			t.toolStarted[flowToolTimingKey(runID, nodeID, event.CallID)] = at
		}
	case assistant.EventToolCompleted:
		if event.CallID == "" {
			return
		}
		key := flowToolTimingKey(runID, nodeID, event.CallID)
		started, ok := t.toolStarted[key]
		if !ok {
			return
		}
		if duration := at.Sub(started).Milliseconds(); duration > 0 {
			t.assistantTimings.ToolMS += duration
		}
		delete(t.toolStarted, key)
	}
}

func (t *flowActivityTracker) recordCumulativeAssistantTiming(runID, nodeID, sessionID string, generation, call int, code string, data map[string]any) {
	key := flowAssistantTimingKey(runID, nodeID, sessionID, generation, call)
	previous := t.assistantCallTimings[key]
	current := previous
	switch code {
	case "pi.stream.started":
		current.WaitMS = maxTimingMilliseconds(current.WaitMS, timingMilliseconds(data["wait_ms"]))
	case "pi.message.completed":
		current.StreamMS = maxTimingMilliseconds(current.StreamMS, timingMilliseconds(data["stream_ms"]))
		current.TotalMS = maxTimingMilliseconds(current.TotalMS, timingMilliseconds(data["total_ms"]))
	}
	t.assistantCallTimings[key] = current
	t.assistantTimings.WaitMS += current.WaitMS - previous.WaitMS
	t.assistantTimings.StreamMS += current.StreamMS - previous.StreamMS
	t.assistantTimings.TotalMS += current.TotalMS - previous.TotalMS
}

func flowAssistantInvocationKey(runID, nodeID string) string {
	return runID + "\x00" + nodeID
}

func flowAssistantTimingKey(runID, nodeID, sessionID string, generation, call int) string {
	return runID + "\x00" + nodeID + "\x00" + strconv.Itoa(generation) + "\x00" + sessionID + "\x00" + strconv.Itoa(call)
}

func maxTimingMilliseconds(previous, current int64) int64 {
	if current > previous {
		return current
	}
	return previous
}

func flowToolTimingKey(runID, nodeID, callID string) string {
	return runID + "\x00" + nodeID + "\x00" + callID
}

func timingMilliseconds(value any) int64 {
	switch value := value.(type) {
	case int:
		if value > 0 {
			return int64(value)
		}
	case int64:
		if value > 0 {
			return value
		}
	case float64:
		if value > 0 {
			return int64(value)
		}
	case json.Number:
		if parsed, err := value.Int64(); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 0
}

func providerObservationState(code string, success any) string {
	switch code {
	case "pi.turn.started":
		return "awaiting_response"
	case "pi.message.started":
		return "response_started"
	case "pi.stream.started":
		return "streaming"
	case "pi.message.completed":
		return "response_completed"
	case "pi.auto_retry.started":
		return "retry_backoff"
	case "pi.auto_retry.completed":
		if value, ok := success.(bool); ok && !value {
			return "retry_failed"
		}
		return "retrying"
	case "pi.agent.started":
		return "agent_started"
	case "pi.agent.completed":
		return "agent_completed"
	default:
		return ""
	}
}

func (t *flowActivityTracker) get(runID, nodeID string) (flowActivityRecord, bool) {
	if t == nil {
		return flowActivityRecord{}, false
	}
	t.mu.RLock()
	record, ok := t.records[runID+"\x00"+nodeID]
	t.mu.RUnlock()
	return record, ok
}

func (t *flowActivityTracker) active() []flowActivityRecord {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	var records []flowActivityRecord
	for _, record := range t.records {
		if record.Active {
			records = append(records, record)
		}
	}
	t.mu.RUnlock()
	sort.Slice(records, func(i, j int) bool {
		if records[i].RunID != records[j].RunID {
			return records[i].RunID < records[j].RunID
		}
		return records[i].NodeID < records[j].NodeID
	})
	return records
}

func (t *flowActivityTracker) currentContext() (int, bool) {
	if t == nil {
		return 0, false
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	maxTokens := 0
	known := false
	for _, record := range t.records {
		if !record.Active || !record.ContextKnown {
			continue
		}
		if !known || record.ContextTokens > maxTokens {
			maxTokens = record.ContextTokens
			known = true
		}
	}
	return maxTokens, known
}

func (t *flowActivityTracker) assistantTimingSnapshot() evaluation.FlowAssistantTimings {
	if t == nil {
		return evaluation.FlowAssistantTimings{}
	}
	t.mu.RLock()
	timings := t.assistantTimings
	t.mu.RUnlock()
	return timings
}

func traceAttempt(value any) int {
	switch value := value.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func traceActivityName(event assistant.Event) string {
	if event.Type == assistant.EventToolStarted || event.Type == assistant.EventToolCompleted {
		return event.Type + "(" + event.Tool + ")"
	}
	if event.Type == assistant.EventDiagnostic {
		if code, ok := event.Data["code"].(string); ok && code != "" {
			return code
		}
	}
	return event.Type
}

type flowTraceContext struct {
	mu       sync.Mutex
	sessions map[string]string
}

func newFlowTraceContext() *flowTraceContext {
	return &flowTraceContext{sessions: map[string]string{}}
}

func (c *flowTraceContext) announceSession(runID, nodeID, sessionID string) string {
	if c == nil || sessionID == "" {
		return ""
	}
	key := runID + "\x00" + nodeID
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions[key] == sessionID {
		return ""
	}
	c.sessions[key] = sessionID
	return "session=" + sessionID
}

func traceEvaluationAssistantEvent(context *flowTraceContext, trace func(string), runID, nodeID string, attempt int, event assistant.Event) {
	if trace == nil {
		return
	}
	session := context.announceSession(runID, nodeID, event.SessionID)
	switch event.Type {
	case assistant.EventSessionStarted, assistant.EventSessionResumed:
		details := []string{fmt.Sprintf("assistant=%v", event.Data["assistant"]), "model=" + traceModel(event), session}
		traceScoped(trace, runID, nodeID, attempt, strings.ReplaceAll(event.Type, ".", " "), details...)
	case assistant.EventToolStarted, assistant.EventToolCompleted:
		details := []string{}
		if summary := traceToolInput(event.Input); summary != "" {
			details = append(details, summary)
		}
		if event.Type == assistant.EventToolCompleted {
			details = append(details, fmt.Sprintf("error=%v", event.Data["error"]))
		}
		details = append(details, session)
		traceScoped(trace, runID, nodeID, attempt, event.Tool+" "+strings.TrimPrefix(event.Type, "tool."), details...)
	case assistant.EventMessage:
		message := strings.Join(strings.Fields(event.Message), " ")
		if len(message) > 160 {
			message = message[:160] + "..."
		}
		if message != "" {
			traceScoped(trace, runID, nodeID, attempt, "message", fmt.Sprintf("text=%q", message), session)
		}
	case assistant.EventUsage:
		if event.Usage != nil {
			traceScoped(trace, runID, nodeID, attempt, "usage", fmt.Sprintf("input=%d output=%d cost=%.6g", event.Usage.InputTokens, event.Usage.OutputTokens, event.Usage.Cost), session)
		}
	case assistant.EventDiagnostic:
		code, _ := event.Data["code"].(string)
		details := []string{"code=" + valueOrDash(code)}
		if call := traceAttempt(event.Data["call"]); call > 0 {
			details = append(details, fmt.Sprintf("call=%d", call))
		}
		if retry := traceAttempt(event.Data["attempt"]); retry > 0 {
			maxRetries := traceAttempt(event.Data["max_attempts"])
			if maxRetries > 0 {
				details = append(details, fmt.Sprintf("retry=%d/%d", retry, maxRetries))
			} else {
				details = append(details, fmt.Sprintf("retry=%d", retry))
			}
		}
		if delay := traceAttempt(event.Data["delay_ms"]); delay > 0 {
			details = append(details, "delay="+(time.Duration(delay)*time.Millisecond).String())
		}
		for _, metric := range []struct{ key, label string }{{"wait_ms", "wait"}, {"total_ms", "total"}, {"stream_ms", "stream"}} {
			if value := traceAttempt(event.Data[metric.key]); value > 0 {
				details = append(details, metric.label+"="+(time.Duration(value)*time.Millisecond).String())
			}
		}
		if message := strings.Join(strings.Fields(event.Message), " "); message != "" {
			if len(message) > 160 {
				message = message[:160] + "..."
			}
			details = append(details, fmt.Sprintf("error=%q", message))
		}
		traceScoped(trace, runID, nodeID, attempt, "observation", details...)
	case assistant.EventCompleted:
		traceScoped(trace, runID, nodeID, attempt, "assistant completed", session)
	case assistant.EventFailed:
		traceScoped(trace, runID, nodeID, attempt, "assistant failed", fmt.Sprintf("error=%q", strings.Join(strings.Fields(event.Message), " ")), session)
	}
}

func traceModel(event assistant.Event) string {
	modelID := fmt.Sprint(event.Data["model_id"])
	if modelID == "<nil>" {
		modelID = ""
	}
	if event.Provider == "" {
		return valueOrDash(modelID)
	}
	if modelID == "" {
		return event.Provider
	}
	return event.Provider + "/" + modelID
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func traceToolInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	for _, key := range []string{"path", "command", "query", "pattern"} {
		if text, ok := value[key].(string); ok && text != "" {
			text = strings.Join(strings.Fields(text), " ")
			if len(text) > 120 {
				text = text[:120] + "..."
			}
			return fmt.Sprintf("%s=%q", key, text)
		}
	}
	return ""
}

func (e evaluationEngine) pollFlowCase(ctx context.Context, app *App, runID, answer string, trace func(string), activity *flowActivityTracker, progress func(evaluation.FlowRuntimeProgress) (*evaluation.FlowProgress, error)) (evaluation.FlowCaseRunResult, error) {
	poll := func() (*store.RunState, error) { return app.Core.RunService.GetRun(runID) }
	var eventRevision, progressRevision uint64
	var progressPublished bool
	var progressUpdated time.Time
	var latestProgress *evaluation.FlowProgress
	lastRunningTrace := time.Time{}
	for {
		state, err := poll()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && ctx.Err() == nil {
				select {
				case <-ctx.Done():
				case <-time.After(50 * time.Millisecond):
					continue
				}
			}
			if ctx.Err() != nil {
				return e.cancelFlowCase(ctx, app, runID, trace)
			}
			return evaluation.FlowCaseRunResult{}, err
		}
		now := time.Now()
		if progress != nil && shouldPublishFlowProgress(progressPublished, progressRevision, state.Revision, progressUpdated, now) {
			snapshot, snapshotErr := app.Core.RunService.EvaluationSnapshot(runID)
			if snapshotErr != nil {
				return evaluation.FlowCaseRunResult{}, snapshotErr
			}
			latestProgress, err = progress(summarizeFlowRuntimeProgress(snapshot.States, activity))
			if err != nil {
				result, cancelErr := e.cancelFlowCase(ctx, app, runID, trace)
				return result, errors.Join(err, cancelErr)
			}
			progressPublished = true
			progressRevision = state.Revision
			progressUpdated = now
		}
		if trace != nil {
			events, err := app.Core.RunService.Events(ctx, runID, eventRevision, 200, 0)
			if err != nil {
				if ctx.Err() != nil {
					return e.cancelFlowCase(ctx, app, runID, trace)
				}
				return evaluation.FlowCaseRunResult{}, err
			}
			for _, event := range events.Events {
				traceEvaluationEvent(trace, runID, event)
			}
			eventRevision = events.NextRevision
			if lastRunningTrace.IsZero() || time.Since(lastRunningTrace) >= 30*time.Second {
				if latestProgress != nil {
					traceFlowProgressSnapshot(trace, latestProgress)
				}
				traceFlowRunningNodes(trace, state, activity, now)
				lastRunningTrace = now
			}
		}
		switch state.Status {
		case store.RunWaiting:
			if ctx.Err() != nil {
				return e.cancelFlowCase(ctx, app, runID, trace)
			}
			if answer == "" {
				return e.flowSnapshot(app, runID, state, nil, trace)
			}
			if state.Waiting == nil {
				return evaluation.FlowCaseRunResult{}, fmt.Errorf("waiting run %s has no waiting state", runID)
			}
			// approval.requested makes waiting durable before the starting runner
			// commits node.suspended. Wait for that quiescent state before Answer.
			requestedNodeID := state.Waiting.NodeID
			node := state.Nodes[requestedNodeID]
			if node == nil || node.Status != store.NodeWaiting {
				snapshot, snapshotErr := app.Core.RunService.EvaluationSnapshot(runID)
				if snapshotErr != nil {
					return evaluation.FlowCaseRunResult{}, snapshotErr
				}
				waitingState := snapshot.Root
				for waitingState.Waiting != nil && waitingState.Waiting.Kind == "child_run" && waitingState.Waiting.ChildRunID != "" {
					var child *store.RunState
					for _, candidate := range snapshot.States {
						if candidate.ID == waitingState.Waiting.ChildRunID {
							child = candidate
							break
						}
					}
					if child == nil {
						break
					}
					waitingState = child
				}
				if waitingState.Waiting != nil {
					node = waitingState.Nodes[waitingState.Waiting.NodeID]
				}
			}
			if node == nil || node.Status != store.NodeWaiting || node.Attempts != 0 {
				break
			}
			if _, err := app.Core.RunService.Answer(ctx, runID, requestedNodeID, answer); err != nil {
				if ctx.Err() != nil {
					return e.cancelFlowCase(ctx, app, runID, trace)
				}
				return evaluation.FlowCaseRunResult{}, err
			}
		case store.RunCompleted, store.RunFailed, store.RunCancelled, store.RunAbandoned, store.RunPausing, store.RunPaused:
			return e.flowSnapshot(app, runID, state, nil, trace)
		}
		select {
		case <-ctx.Done():
			return e.cancelFlowCase(ctx, app, runID, trace)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func shouldPublishFlowProgress(published bool, lastRevision, revision uint64, lastUpdated, now time.Time) bool {
	return !published || revision != lastRevision || now.Sub(lastUpdated) >= 10*time.Second
}

func summarizeFlowRuntimeProgress(states []*store.RunState, activity *flowActivityTracker) evaluation.FlowRuntimeProgress {
	progress := evaluation.FlowRuntimeProgress{RunningNodes: []string{}, Timings: &evaluation.FlowRuntimeTimings{}}
	if len(states) == 0 || states[0] == nil {
		return progress
	}
	root := states[0]
	progress.RunID = root.ID
	progress.Status = root.Status
	if root.Usage != nil {
		progress.InputTokens = root.Usage.InputTokens
		progress.OutputTokens = root.Usage.OutputTokens
		progress.Cost = root.Usage.Cost
	}
	for _, state := range states {
		if state == nil {
			continue
		}
		progress.TotalNodes += len(state.Nodes)
		for id, node := range state.Nodes {
			if node == nil {
				continue
			}
			progress.NodeAttempts += node.Attempts
			progress.ProviderAttempts += node.ProviderAttempts
			if node.Status == store.NodeCompleted {
				progress.CompletedNodes++
			}
			if node.Status == store.NodeRunning {
				progress.RunningNodes = append(progress.RunningNodes, id)
			}
		}
	}
	sort.Strings(progress.RunningNodes)
	if activity != nil {
		progress.ContextTokens, progress.ContextKnown = activity.currentContext()
		progress.Timings.Assistant = activity.assistantTimingSnapshot()
		for _, record := range activity.active() {
			if record.ProviderState == "" {
				continue
			}
			progress.AssistantActivity = append(progress.AssistantActivity, evaluation.FlowAssistantProgress{
				RunID: record.RunID, NodeID: record.NodeID, Attempt: record.Attempt, State: record.ProviderState,
				Since: record.ProviderStateSince, Call: record.ProviderCall, Retry: record.ProviderRetry,
				MaxRetries: record.ProviderMaxRetries, DelayMS: record.ProviderDelayMS, LastError: record.LastProviderError,
			})
		}
	}
	return progress
}

func traceFlowProgressSnapshot(trace func(string), progress *evaluation.FlowProgress) {
	if trace == nil || progress == nil {
		return
	}
	current, phase := "-", "-"
	if progress.Current != nil {
		current = fmt.Sprintf("%s#%d", progress.Current.CaseID, progress.Current.Repeat)
		phase = progress.Current.Phase
	}
	trace(fmt.Sprintf("EVAL | progress | completed=%d/%d current=%s phase=%s", progress.CompletedRuns, progress.TotalRuns, current, phase))
	runtime := progress.Runtime
	traceScoped(trace, runtime.RunID, "", 0, "progress", fmt.Sprintf("status=%s nodes=%d/%d running=%s node_attempts=%d provider_attempts=%d tokens=%d cost=%g", runtime.Status, runtime.CompletedNodes, runtime.TotalNodes, valueOrDash(strings.Join(runtime.RunningNodes, ",")), runtime.NodeAttempts, runtime.ProviderAttempts, runtime.InputTokens+runtime.OutputTokens, runtime.Cost))
}

func traceFlowRunningNodes(trace func(string), state *store.RunState, activity *flowActivityTracker, now time.Time) {
	if trace == nil || state == nil {
		return
	}
	seen := map[string]bool{}
	for _, record := range activity.active() {
		traceFlowActivity(trace, record, now)
		seen[record.RunID+"\x00"+record.NodeID] = true
	}
	for id, node := range state.Nodes {
		if node == nil || node.Status != store.NodeRunning {
			continue
		}
		if seen[state.ID+"\x00"+id] {
			continue
		}
		record, ok := activity.get(state.ID, id)
		if !ok {
			traceScoped(trace, state.ID, id, node.Attempts, "active", "idle=unknown/unknown context=unknown last=unknown awaiting=assistant_progress_or_completion")
			continue
		}
		if !record.Active {
			continue
		}
		traceFlowActivity(trace, record, now)
	}
}

func traceFlowActivity(trace func(string), record flowActivityRecord, now time.Time) {
	idle := now.Sub(record.LastActivityAt).Truncate(time.Second)
	if idle < 0 {
		idle = 0
	}
	awaiting := "assistant_progress_or_completion"
	if strings.HasPrefix(record.LastActivity, "tool.completed") {
		awaiting = "provider_response"
	} else if record.LastActivity == "provider.streaming" {
		awaiting = "provider_stream"
	} else if record.LastActivity == "pi.auto_retry.started" {
		awaiting = "provider_retry_backoff"
	} else if record.LastActivity == "pi.turn.started" {
		awaiting = "provider_response"
	} else if record.LastActivity == "pi.stream.started" {
		awaiting = "provider_stream"
	}
	traceScoped(trace, record.RunID, record.NodeID, record.Attempt, "active", fmt.Sprintf("idle=%s/%s context=%s last=%s awaiting=%s", idle, record.IdleTimeout, traceContextSize(record), record.LastActivity, awaiting))
}

func traceContextSize(record flowActivityRecord) string {
	if !record.ContextKnown {
		return "unknown"
	}
	return formatTraceCount(int64(record.ContextTokens)) + "t"
}

func formatTraceCount(value int64) string {
	raw := strconv.FormatInt(value, 10)
	start := 0
	if strings.HasPrefix(raw, "-") {
		start = 1
	}
	for index := len(raw) - 3; index > start; index -= 3 {
		raw = raw[:index] + " " + raw[index:]
	}
	return raw
}

func traceEvaluationEvent(trace func(string), runID string, event store.Event) {
	if trace == nil {
		return
	}
	details := []string{}
	if providerAttempt, ok := event.Data["provider_attempt"]; ok {
		if maxAttempts, exists := event.Data["max_provider_attempts"]; exists {
			details = append(details, fmt.Sprintf("provider_attempt=%v/%v", providerAttempt, maxAttempts))
		} else {
			details = append(details, fmt.Sprintf("provider_attempt=%v", providerAttempt))
		}
	}
	if providerAttempts, ok := event.Data["provider_attempts"]; ok {
		details = append(details, fmt.Sprintf("provider_attempts=%v", providerAttempts))
	}
	if maxAttempts, ok := event.Data["max_provider_attempts"]; ok && event.Data["provider_attempt"] == nil {
		details = append(details, fmt.Sprintf("max_provider_attempts=%v", maxAttempts))
	}
	for _, field := range []string{"delay", "not_before", "kind", "fingerprint"} {
		if value, ok := event.Data[field]; ok {
			details = append(details, fmt.Sprintf("%s=%v", field, value))
		}
	}
	if code, ok := event.Data["code"]; ok {
		details = append(details, fmt.Sprintf("code=%v", code))
	}
	attempt := traceAttempt(event.Data["attempt"])
	if attempt == 0 {
		attempt = traceAttempt(event.Data["attempts"])
	}
	eventName := strings.ReplaceAll(event.Type, ".", " ")
	if event.NodeID != "" {
		eventName = strings.TrimPrefix(eventName, "node ")
	}
	traceScoped(trace, runID, event.NodeID, attempt, eventName, strings.Join(details, " "))
}

func traceScoped(trace func(string), runID, nodeID string, attempt int, event string, details ...string) {
	if trace == nil {
		return
	}
	line := traceRunScope(runID, nodeID, attempt) + " | " + event
	if detail := joinTraceDetails(details...); detail != "" {
		line += " | " + detail
	}
	trace(line)
}

func traceRunScope(runID, nodeID string, attempt int) string {
	scope := "RUN " + shortTraceRunID(runID)
	if nodeID != "" {
		scope += " · NODE " + nodeID
		if attempt > 0 {
			scope += fmt.Sprintf("#%d", attempt)
		}
	}
	return scope
}

func shortTraceRunID(runID string) string {
	if strings.HasPrefix(runID, "run-") && len(runID) > len("run-")+8 {
		return runID[len("run-") : len("run-")+8]
	}
	return runID
}

func joinTraceDetails(values ...string) string {
	filtered := values[:0]
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	return strings.Join(filtered, " ")
}

func traceFlowChildSnapshot(trace func(string), states []*store.RunState) {
	if trace == nil || len(states) < 2 {
		return
	}
	children := append([]*store.RunState(nil), states[1:]...)
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	for _, state := range children {
		if state == nil {
			continue
		}
		traceScoped(trace, state.ID, "", 0, "child "+state.Status, "id="+state.ID, "parent="+state.ParentRunID, "code="+state.ErrorCode)
		ids := make([]string, 0, len(state.Nodes))
		for id := range state.Nodes {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			node := state.Nodes[id]
			if node == nil || node.Status == store.NodeCompleted || node.Status == store.NodePending || node.Status == store.NodeSkipped {
				continue
			}
			traceScoped(trace, state.ID, id, node.Attempts, node.Status, "code="+node.ErrorCode)
		}
	}
}

func (e evaluationEngine) cancelFlowCase(ctx context.Context, app *App, runID string, trace func(string)) (evaluation.FlowCaseRunResult, error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if state, err := app.Core.RunService.GetRun(runID); err == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
		return e.flowSnapshot(app, runID, state, ctx.Err(), trace)
	}
	if _, err := app.Core.RunService.Cancel(cleanupCtx, runID, "flow evaluation context cancelled"); err != nil {
		if state, loadErr := app.Core.RunService.GetRun(runID); loadErr == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
			return e.flowSnapshot(app, runID, state, ctx.Err(), trace)
		}
		return evaluation.FlowCaseRunResult{}, err
	}
	for cleanupCtx.Err() == nil {
		state, err := app.Core.RunService.GetRun(runID)
		if err == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
			return e.flowSnapshot(app, runID, state, ctx.Err(), trace)
		}
		select {
		case <-cleanupCtx.Done():
		case <-time.After(50 * time.Millisecond):
		}
	}
	return evaluation.FlowCaseRunResult{}, ctx.Err()
}

func terminalFlowRun(status string) bool {
	return status == store.RunCompleted || status == store.RunFailed || status == store.RunCancelled || status == store.RunAbandoned
}

func (e evaluationEngine) flowSnapshot(app *App, runID string, observed *store.RunState, callbackErr error, trace func(string)) (evaluation.FlowCaseRunResult, error) {
	snapshot, err := app.Core.RunService.EvaluationSnapshot(runID)
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
	traceFlowChildSnapshot(trace, snapshot.States)
	result := evaluation.FlowCaseRunResult{States: snapshot.States, Events: snapshot.Events, Artifacts: snapshot.Artifacts, ArtifactDirs: snapshot.ArtifactDirs, ContextCancelled: callbackErr != nil}
	result.Cleanup = func(ctx context.Context) (*store.RunState, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		latest, err := app.Core.RunService.GetRun(runID)
		if err != nil {
			return nil, err
		}
		if latest.Worktree == nil || !latest.Worktree.Enabled {
			return latest, nil
		}
		if latest.Status == store.RunPausing || latest.Status == store.RunPaused {
			return latest, nil
		}
		if latest.Status == store.RunWaiting {
			if _, err := app.Core.RunService.Cancel(cleanupCtx, runID, "flow evaluation cleanup"); err != nil {
				return nil, err
			}
			for !terminalFlowRun(latest.Status) {
				if cleanupCtx.Err() != nil {
					return nil, cleanupCtx.Err()
				}
				time.Sleep(50 * time.Millisecond)
				latest, err = app.Core.RunService.GetRun(runID)
				if err != nil {
					return nil, err
				}
			}
		}
		return app.Core.WorktreeService.Remove(cleanupCtx, runID, true)
	}
	return result, callbackErr
}

func (evaluationEngine) TaskBenchmark(ctx context.Context, req tooling.EvaluationBenchmarkRequest) (any, error) {
	return evaluation.RunTaskMatrix(ctx, evaluation.TaskMatrixRunOptions{
		MatrixPath: req.MatrixPath, OutputDir: req.OutputDir, Repeat: req.Repeat, Replace: req.Replace,
		CaseRunner: func(ctx context.Context, workspace, goal, profileName string) (evaluation.TaskCaseExecution, error) {
			app, err := New(workspace, ".takt/config.yaml")
			if err != nil {
				return evaluation.TaskCaseExecution{}, err
			}
			observed, runErr := app.Experimental.EvaluateTaskCase(ctx, goal, profileName)
			return evaluation.TaskCaseExecution{
				PlanID: observed.PlanID, RunID: observed.RunID, Status: observed.Status,
				Route: observed.Route, Template: observed.Template, Workflow: observed.Workflow,
				PlanRevisions: observed.PlanRevisions, ReplannerRuns: observed.ReplannerRuns,
				ExecutionRuns: observed.ExecutionRuns, RouterFallback: observed.RouterFallback,
				InputTokens: observed.InputTokens, OutputTokens: observed.OutputTokens, Cost: observed.Cost,
			}, runErr
		},
	})
}

func (evaluationEngine) Compare(_ context.Context, baselineDir, candidateDir string) (any, error) {
	baseline, err := evaluation.LoadReport(baselineDir)
	if err != nil {
		return nil, err
	}
	candidate, err := evaluation.LoadReport(candidateDir)
	if err != nil {
		return nil, err
	}
	return evaluation.Compare(baseline, candidate)
}

func (evaluationEngine) Report(_ context.Context, outputDir string) (any, error) {
	if report, err := evaluation.LoadReport(outputDir); err == nil {
		return report, nil
	} else if matrixReport, matrixErr := evaluation.LoadMatrixReport(outputDir); matrixErr == nil {
		return matrixReport, nil
	} else if taskReport, taskErr := evaluation.LoadTaskMatrixReport(outputDir); taskErr == nil {
		return taskReport, nil
	} else {
		return nil, fmt.Errorf("load evaluation report: suite=%v; matrix=%v; task_matrix=%v", err, matrixErr, taskErr)
	}
}

func (evaluationEngine) Stats(_ context.Context, outputDir string) (any, error) {
	report, err := evaluation.LoadReport(outputDir)
	progress, progressErr := evaluation.LoadFlowProgress(outputDir)
	if progressErr == nil {
		if err == nil {
			stats := evaluation.BuildStats(report)
			if progress.Status == "running" {
				// Keep checkpointed case details, but mark the suite incomplete and
				// overlay the current live phase/timing snapshot.
				evaluation.ApplyLiveProgressStats(stats, *progress, time.Now().UTC())
			} else {
				evaluation.ApplyProgressStatus(stats, *progress)
			}
			return stats, nil
		}
		if progress.Status == "running" {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, err
			}
			return evaluation.BuildProgressStats(*progress, time.Now().UTC()), nil
		}
	}
	if progressErr != nil && !errors.Is(progressErr, os.ErrNotExist) {
		return nil, progressErr
	}
	if err == nil {
		return evaluation.BuildStats(report), nil
	}
	return nil, err
}

func (evaluationEngine) Status(_ context.Context, outputDir string) (any, error) {
	return evaluation.LoadFlowProgress(outputDir)
}

func (evaluationEngine) Inspect(_ context.Context, request tooling.EvaluationInspectRequest) (any, error) {
	return evaluation.InspectFlowEvaluation(request.OutputDir, request.CaseID, request.Repeat)
}

func (e evaluationEngine) executionFactory(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) (evaluation.Execution, error) {
	def := runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: workflowPath, ConfigPath: configPath, ControlWorkspace: workspace}
	deps := runtime.Dependencies{
		Commands:   runtime.NewCommandResolver(workflowPath, workspace, workspace),
		Store:      store.FS{Workspace: workspace},
		Assistants: assistant.Factory{Config: cfg, Providers: e.providers},
		Adapters:   domainadapter.Factory{Config: cfg},
		Redactor:   redact.NewFromConfig(cfg),
	}
	return evaluation.Execution{Runner: runtime.NewWithDependencies(def, deps), Store: store.FS{Workspace: workspace}}, nil
}
