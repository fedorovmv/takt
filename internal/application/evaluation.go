package application

import (
	"context"
	"fmt"
)

type EvaluationRunRequest struct {
	WorkflowPath, ConfigPath, CasesDir, CaseManifestPath string
	WorkspaceTemplate, OutputDir                         string
	Repeat                                               int
	ApprovalAnswer                                       string
	Replace                                              bool
	StrategyID, BenchmarkID                              string
	QualityNode, GenerationNode                          string
	ValidatorID, ValidatorVersion, ValidatorPath         string
}

type EvaluationBenchmarkRequest struct {
	MatrixPath string
	OutputDir  string
	Repeat     int
	Replace    bool
}

type EvaluationEngine interface {
	Run(context.Context, EvaluationRunRequest) (any, error)
	Benchmark(context.Context, EvaluationBenchmarkRequest) (any, error)
	TaskBenchmark(context.Context, EvaluationBenchmarkRequest) (any, error)
	Compare(context.Context, string, string) (any, error)
	Report(context.Context, string) (any, error)
}

type EvaluationService struct{ engine EvaluationEngine }

func (s *EvaluationService) Run(ctx context.Context, req EvaluationRunRequest) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.Run(ctx, req)
}

func (s *EvaluationService) Benchmark(ctx context.Context, req EvaluationBenchmarkRequest) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.Benchmark(ctx, req)
}

func (s *EvaluationService) TaskBenchmark(ctx context.Context, req EvaluationBenchmarkRequest) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.TaskBenchmark(ctx, req)
}

func (s *EvaluationService) Compare(ctx context.Context, baseline, candidate string) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.Compare(ctx, baseline, candidate)
}

func (s *EvaluationService) Report(ctx context.Context, outputDir string) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.Report(ctx, outputDir)
}
