package bootstrap

import (
	"context"
	"fmt"

	"takt/internal/application"
	"takt/internal/assistant"
	"takt/internal/domainadapter"
	"takt/internal/evaluation"
	"takt/internal/redact"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
)

type evaluationEngine struct{}

func (evaluationEngine) Run(ctx context.Context, req application.EvaluationRunRequest) (any, error) {
	return evaluation.Run(ctx, evaluation.RunOptions{
		ExecutionFactory: evaluationExecutionFactory,
		WorkflowPath:     req.WorkflowPath, ConfigPath: req.ConfigPath, CasesDir: req.CasesDir,
		CaseManifestPath: req.CaseManifestPath, WorkspaceTemplate: req.WorkspaceTemplate,
		OutputDir: req.OutputDir, Repeat: req.Repeat, ApprovalAnswer: req.ApprovalAnswer, Replace: req.Replace,
		StrategyID: req.StrategyID, BenchmarkID: req.BenchmarkID, QualityNode: req.QualityNode,
		GenerationNode: req.GenerationNode, ValidatorID: req.ValidatorID, ValidatorVersion: req.ValidatorVersion,
		ValidatorPath: req.ValidatorPath,
	})
}

func (evaluationEngine) Benchmark(ctx context.Context, req application.EvaluationBenchmarkRequest) (any, error) {
	return evaluation.RunMatrix(ctx, evaluation.MatrixRunOptions{ExecutionFactory: evaluationExecutionFactory, MatrixPath: req.MatrixPath, OutputDir: req.OutputDir, Repeat: req.Repeat, Replace: req.Replace})
}

func (evaluationEngine) TaskBenchmark(ctx context.Context, req application.EvaluationBenchmarkRequest) (any, error) {
	return evaluation.RunTaskMatrix(ctx, evaluation.TaskMatrixRunOptions{
		MatrixPath: req.MatrixPath, OutputDir: req.OutputDir, Repeat: req.Repeat, Replace: req.Replace,
		CaseRunner: func(ctx context.Context, workspace, goal, profileName string) (evaluation.TaskCaseExecution, error) {
			app, err := New(workspace, ".takt/config.yaml")
			if err != nil {
				return evaluation.TaskCaseExecution{}, err
			}
			observed, runErr := app.Services.EvaluateTaskCase(ctx, goal, profileName)
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

func evaluationExecutionFactory(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) (evaluation.Execution, error) {
	def := runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: workflowPath, ConfigPath: configPath, ControlWorkspace: workspace}
	deps := runtime.Dependencies{
		Commands:   runtime.NewCommandResolver(workflowPath, workspace, workspace),
		Store:      store.FS{Workspace: workspace},
		Assistants: assistant.Factory{Config: cfg},
		Adapters:   domainadapter.Factory{Config: cfg},
		Redactor:   redact.NewFromConfig(cfg),
	}
	return evaluation.Execution{Runner: runtime.NewWithDependencies(def, deps), Store: store.FS{Workspace: workspace}}, nil
}
