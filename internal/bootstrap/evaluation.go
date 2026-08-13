package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
		ValidatorPath: req.ValidatorPath,
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
		Repeat: req.Repeat, KeepWorkspaces: req.KeepWorkspaces, Now: time.Now, HostPATH: hostPATH, CaseRunner: e.runFlowCase, Trace: req.Trace,
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
	app, err := New(req.Workspace, req.ConfigPath)
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
	started, err := app.Core.RunService.Start(ctx, application.StartRequest{
		Selector: req.Selector, Input: req.InputValue, ConfigPath: req.ConfigPath,
		Detached: true, KeepWorktree: true,
	})
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
	if started.RunID == "" {
		return evaluation.FlowCaseRunResult{}, fmt.Errorf("flow evaluation start returned no run ID")
	}
	traceEvaluation(req.Trace, "run.accepted run=%s", started.RunID)
	return e.pollFlowCase(ctx, app, started.RunID, req.ApprovalAnswer, req.Trace)
}

func (e evaluationEngine) pollFlowCase(ctx context.Context, app *App, runID, answer string, trace func(string)) (evaluation.FlowCaseRunResult, error) {
	poll := func() (*store.RunState, error) { return app.Core.RunService.GetRun(runID) }
	var revision uint64
	lastRunningTrace := time.Time{}
	for {
		state, err := poll()
		if err != nil {
			if ctx.Err() != nil {
				return e.cancelFlowCase(ctx, app, runID)
			}
			return evaluation.FlowCaseRunResult{}, err
		}
		if trace != nil {
			events, err := app.Core.RunService.Events(ctx, runID, revision, 200, 0)
			if err != nil {
				if ctx.Err() != nil {
					return e.cancelFlowCase(ctx, app, runID)
				}
				return evaluation.FlowCaseRunResult{}, err
			}
			for _, event := range events.Events {
				traceEvaluationEvent(trace, event)
			}
			revision = events.NextRevision
			if lastRunningTrace.IsZero() || time.Since(lastRunningTrace) >= 10*time.Second {
				traceFlowRunningNodes(trace, state)
				lastRunningTrace = time.Now()
			}
		}
		switch state.Status {
		case store.RunWaiting:
			if ctx.Err() != nil {
				return e.cancelFlowCase(ctx, app, runID)
			}
			if answer == "" {
				return e.flowSnapshot(app, runID, state, nil)
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
					return e.cancelFlowCase(ctx, app, runID)
				}
				return evaluation.FlowCaseRunResult{}, err
			}
		case store.RunCompleted, store.RunFailed, store.RunCancelled, store.RunAbandoned, store.RunPausing, store.RunPaused:
			return e.flowSnapshot(app, runID, state, nil)
		}
		select {
		case <-ctx.Done():
			return e.cancelFlowCase(ctx, app, runID)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func traceFlowRunningNodes(trace func(string), state *store.RunState) {
	if trace == nil || state == nil {
		return
	}
	for id, node := range state.Nodes {
		if node == nil || node.Status != store.NodeRunning {
			continue
		}
		traceEvaluation(trace, "node.running node=%s attempt=%d", id, node.Attempts)
	}
}

func traceEvaluationEvent(trace func(string), event store.Event) {
	if trace == nil {
		return
	}
	line := event.Type
	if event.NodeID != "" {
		line += " node=" + event.NodeID
	}
	if attempt, ok := event.Data["attempt"]; ok {
		line += fmt.Sprintf(" attempt=%v", attempt)
	}
	trace(line)
}

func traceEvaluation(trace func(string), format string, args ...any) {
	if trace != nil {
		trace(fmt.Sprintf(format, args...))
	}
}

func (e evaluationEngine) cancelFlowCase(ctx context.Context, app *App, runID string) (evaluation.FlowCaseRunResult, error) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if state, err := app.Core.RunService.GetRun(runID); err == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
		return e.flowSnapshot(app, runID, state, ctx.Err())
	}
	if _, err := app.Core.RunService.Cancel(cleanupCtx, runID, "flow evaluation context cancelled"); err != nil {
		if state, loadErr := app.Core.RunService.GetRun(runID); loadErr == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
			return e.flowSnapshot(app, runID, state, ctx.Err())
		}
		return evaluation.FlowCaseRunResult{}, err
	}
	for cleanupCtx.Err() == nil {
		state, err := app.Core.RunService.GetRun(runID)
		if err == nil && (terminalFlowRun(state.Status) || state.Status == store.RunPausing || state.Status == store.RunPaused) {
			return e.flowSnapshot(app, runID, state, ctx.Err())
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

func (e evaluationEngine) flowSnapshot(app *App, runID string, observed *store.RunState, callbackErr error) (evaluation.FlowCaseRunResult, error) {
	snapshot, err := app.Core.RunService.EvaluationSnapshot(runID)
	if err != nil {
		return evaluation.FlowCaseRunResult{}, err
	}
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
