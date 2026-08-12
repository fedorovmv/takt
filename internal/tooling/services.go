package tooling

import (
	"context"
	"fmt"

	"takt/internal/assistant"
	cfgpkg "takt/internal/config"
	"takt/internal/tooling/compatibility"
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

type FlowEvaluationRequest struct {
	SuitePath, CaseID, OutputDir, InvocationWorkspace string
	Repeat                                            int
	KeepWorkspaces                                    bool
}

type EvaluationEngine interface {
	Run(context.Context, EvaluationRunRequest) (any, error)
	Benchmark(context.Context, EvaluationBenchmarkRequest) (any, error)
	TaskBenchmark(context.Context, EvaluationBenchmarkRequest) (any, error)
	Flow(context.Context, FlowEvaluationRequest) (any, error)
	FlowInit(context.Context, string, string) (any, error)
	Compare(context.Context, string, string) (any, error)
	Report(context.Context, string) (any, error)
}

type EvaluationService struct{ engine EvaluationEngine }

func NewEvaluation(engine EvaluationEngine) *EvaluationService {
	return &EvaluationService{engine: engine}
}
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
func (s *EvaluationService) Flow(ctx context.Context, req FlowEvaluationRequest) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.Flow(ctx, req)
}
func (s *EvaluationService) FlowInit(ctx context.Context, workflowSelector, output string) (any, error) {
	if s == nil || s.engine == nil {
		return nil, fmt.Errorf("evaluation service is not configured")
	}
	return s.engine.FlowInit(ctx, workflowSelector, output)
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

type CompatibilityMatrix = compatibility.Matrix
type CompatibilityFieldMatrix = compatibility.FieldMatrix
type CompatibilityReport = compatibility.CheckReport

type CompatibilityService struct {
	workspace  string
	configPath string
	providers  assistant.Registry
}

func NewCompatibility(workspace, configPath string, providers assistant.Registry) *CompatibilityService {
	return &CompatibilityService{workspace: workspace, configPath: configPath, providers: providers}
}
func (s *CompatibilityService) Matrix() CompatibilityMatrix { return compatibility.CurrentMatrix() }
func (s *CompatibilityService) Fields() CompatibilityFieldMatrix {
	return compatibility.CurrentFieldMatrix()
}
func (s *CompatibilityService) Check(ctx context.Context, live bool) (CompatibilityReport, error) {
	cfg, err := cfgpkg.Load(s.configPath)
	if err != nil {
		return CompatibilityReport{}, err
	}
	return compatibility.Check(ctx, cfg, compatibility.CheckOptions{Workspace: s.workspace, Live: live, Providers: s.providers}), nil
}

type Services struct {
	Evaluation    *EvaluationService
	Compatibility *CompatibilityService
}
