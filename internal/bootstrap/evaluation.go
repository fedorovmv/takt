package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	hostPATH := os.Getenv("PATH")
	if hostPATH == "" {
		return nil, fmt.Errorf("flow evaluation requires non-empty host PATH")
	}
	return evaluation.RunFlow(ctx, evaluation.FlowRunOptions{
		SuitePath: req.SuitePath, CaseID: req.CaseID, OutputDir: req.OutputDir, InvocationWorkspace: req.InvocationWorkspace,
		Repeat: req.Repeat, KeepWorkspaces: req.KeepWorkspaces, ModelPreset: req.ModelPreset, ModelOverrides: req.ModelOverrides, Now: time.Now, HostPATH: hostPATH, CaseRunner: e.runFlowCase, Trace: req.Trace, AssistantIdleTimeout: req.AssistantIdleTimeout,
	})
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
	app, err := newApp(req.Workspace, req.ConfigPath, func(runID, nodeID string, event assistant.Event) {
		traceEvaluationAssistantEvent(req.Trace, runID, nodeID, event)
		activity.recordEvent(runID, nodeID, event, req.AssistantIdleTimeout)
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
	traceEvaluation(req.Trace, "run.accepted run=%s", started.RunID)
	return e.pollFlowCase(ctx, app, started.RunID, req.ApprovalAnswer, req.Trace, activity)
}

type flowActivityRecord struct {
	RunID, NodeID, LastActivity string
	Attempt                     int
	LastActivityAt              time.Time
	IdleTimeout                 time.Duration
	Active                      bool
}

type flowActivityTracker struct {
	mu      sync.RWMutex
	records map[string]flowActivityRecord
}

func newFlowActivityTracker() *flowActivityTracker {
	return &flowActivityTracker{records: map[string]flowActivityRecord{}}
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
	active := event.Type != assistant.EventCompleted && event.Type != assistant.EventFailed
	t.record(flowActivityRecord{RunID: runID, NodeID: nodeID, Attempt: traceAttempt(event.Data["attempt"]), LastActivity: traceActivityName(event), LastActivityAt: at, IdleTimeout: idle, Active: active})
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
	return event.Type
}

func traceEvaluationAssistantEvent(trace func(string), runID, nodeID string, event assistant.Event) {
	if trace == nil {
		return
	}
	switch event.Type {
	case assistant.EventSessionStarted, assistant.EventSessionResumed:
		traceEvaluation(trace, "assistant.%s run=%s node=%s assistant=%v model=%v/%v attempt=%v session=%s", event.Type, runID, nodeID, event.Data["assistant"], event.Provider, event.Data["model_id"], event.Data["attempt"], event.SessionID)
	case assistant.EventToolStarted, assistant.EventToolCompleted:
		line := fmt.Sprintf("assistant.%s run=%s node=%s tool=%s model=%s session=%s", event.Type, runID, nodeID, event.Tool, event.Provider, event.SessionID)
		if summary := traceToolInput(event.Input); summary != "" {
			line += " " + summary
		}
		if event.Type == assistant.EventToolCompleted {
			line += fmt.Sprintf(" error=%v", event.Data["error"])
		}
		trace(line)
	case assistant.EventMessage:
		message := strings.Join(strings.Fields(event.Message), " ")
		if len(message) > 160 {
			message = message[:160] + "..."
		}
		if message != "" {
			traceEvaluation(trace, "assistant.message run=%s node=%s model=%s session=%s text=%q", runID, nodeID, event.Provider, event.SessionID, message)
		}
	case assistant.EventUsage:
		if event.Usage != nil {
			traceEvaluation(trace, "assistant.usage run=%s node=%s input=%d output=%d cost=%.6g", runID, nodeID, event.Usage.InputTokens, event.Usage.OutputTokens, event.Usage.Cost)
		}
	case assistant.EventCompleted:
		traceEvaluation(trace, "assistant.%s run=%s node=%s session=%s", event.Type, runID, nodeID, event.SessionID)
	case assistant.EventFailed:
		traceEvaluation(trace, "assistant.failed run=%s node=%s session=%s error=%q", runID, nodeID, event.SessionID, strings.Join(strings.Fields(event.Message), " "))
	}
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

func (e evaluationEngine) pollFlowCase(ctx context.Context, app *App, runID, answer string, trace func(string), activity *flowActivityTracker) (evaluation.FlowCaseRunResult, error) {
	poll := func() (*store.RunState, error) { return app.Core.RunService.GetRun(runID) }
	var revision uint64
	lastRunningTrace := time.Time{}
	for {
		state, err := poll()
		if err != nil {
			if ctx.Err() != nil {
				return e.cancelFlowCase(ctx, app, runID, trace)
			}
			return evaluation.FlowCaseRunResult{}, err
		}
		if trace != nil {
			events, err := app.Core.RunService.Events(ctx, runID, revision, 200, 0)
			if err != nil {
				if ctx.Err() != nil {
					return e.cancelFlowCase(ctx, app, runID, trace)
				}
				return evaluation.FlowCaseRunResult{}, err
			}
			for _, event := range events.Events {
				traceEvaluationEvent(trace, runID, event)
			}
			revision = events.NextRevision
			if lastRunningTrace.IsZero() || time.Since(lastRunningTrace) >= 30*time.Second {
				traceFlowRunningNodes(trace, state, activity, time.Now())
				lastRunningTrace = time.Now()
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
			if node := state.Nodes[state.Waiting.NodeID]; node == nil || node.Status != store.NodeWaiting || node.Attempts != 0 {
				break
			}
			if _, err := app.Core.RunService.Answer(ctx, runID, state.Waiting.NodeID, answer); err != nil {
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
			traceEvaluation(trace, "node.active run=%s node=%s attempt=%d idle=unknown idle_limit=unknown last_activity=unknown awaiting=assistant_progress_or_completion", state.ID, id, node.Attempts)
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
	}
	traceEvaluation(trace, "node.active run=%s node=%s attempt=%d idle=%s idle_limit=%s last_activity=%s awaiting=%s", record.RunID, record.NodeID, record.Attempt, idle, record.IdleTimeout, record.LastActivity, awaiting)
}

func traceEvaluationEvent(trace func(string), runID string, event store.Event) {
	if trace == nil {
		return
	}
	line := event.Type + " run=" + runID
	if event.NodeID != "" {
		line += " node=" + event.NodeID
	}
	if providerAttempt, ok := event.Data["provider_attempt"]; ok {
		if maxAttempts, exists := event.Data["max_provider_attempts"]; exists {
			line += fmt.Sprintf(" provider_attempt=%v/%v", providerAttempt, maxAttempts)
		} else {
			line += fmt.Sprintf(" provider_attempt=%v", providerAttempt)
		}
	}
	if providerAttempts, ok := event.Data["provider_attempts"]; ok {
		line += fmt.Sprintf(" provider_attempts=%v", providerAttempts)
	}
	if maxAttempts, ok := event.Data["max_provider_attempts"]; ok && event.Data["provider_attempt"] == nil {
		line += fmt.Sprintf(" max_provider_attempts=%v", maxAttempts)
	}
	for _, field := range []string{"delay", "not_before", "kind", "fingerprint"} {
		if value, ok := event.Data[field]; ok {
			line += fmt.Sprintf(" %s=%v", field, value)
		}
	}
	if attempt, ok := event.Data["attempt"]; ok {
		line += fmt.Sprintf(" attempt=%v", attempt)
	}
	if code, ok := event.Data["code"]; ok {
		line += fmt.Sprintf(" code=%v", code)
	}
	trace(line)
}

func traceEvaluation(trace func(string), format string, args ...any) {
	if trace != nil {
		trace(fmt.Sprintf(format, args...))
	}
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
		traceEvaluation(trace, "child_run.%s run=%s parent=%s code=%s", state.Status, state.ID, state.ParentRunID, state.ErrorCode)
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
			traceEvaluation(trace, "child_node.%s run=%s node=%s code=%s", node.Status, state.ID, id, node.ErrorCode)
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
