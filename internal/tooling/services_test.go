package tooling

import (
	"context"
	"reflect"
	"testing"
)

type flowEvaluationEngine struct {
	request          FlowEvaluationRequest
	selector, output string
	statsPath        string
	statusPath       string
	inspectRequest   EvaluationInspectRequest
	analyzeRequest   EvaluationAnalyzeRequest
}

func (e *flowEvaluationEngine) Run(context.Context, EvaluationRunRequest) (any, error) {
	return nil, nil
}
func (e *flowEvaluationEngine) Benchmark(context.Context, EvaluationBenchmarkRequest) (any, error) {
	return nil, nil
}
func (e *flowEvaluationEngine) TaskBenchmark(context.Context, EvaluationBenchmarkRequest) (any, error) {
	return nil, nil
}
func (e *flowEvaluationEngine) Compare(context.Context, string, string) (any, error) { return nil, nil }
func (e *flowEvaluationEngine) Report(context.Context, string) (any, error)          { return nil, nil }
func (e *flowEvaluationEngine) Stats(_ context.Context, path string) (any, error) {
	e.statsPath = path
	return "stats", nil
}
func (e *flowEvaluationEngine) Status(_ context.Context, path string) (any, error) {
	e.statusPath = path
	return "status", nil
}
func (e *flowEvaluationEngine) Inspect(_ context.Context, request EvaluationInspectRequest) (any, error) {
	e.inspectRequest = request
	return "inspection", nil
}
func (e *flowEvaluationEngine) Flow(_ context.Context, request FlowEvaluationRequest) (any, error) {
	e.request = request
	return "flow", nil
}
func (e *flowEvaluationEngine) FlowInit(_ context.Context, selector, output string) (any, error) {
	e.selector, e.output = selector, output
	return "init", nil
}
func (e *flowEvaluationEngine) Analyze(_ context.Context, request EvaluationAnalyzeRequest) (any, error) {
	e.analyzeRequest = request
	return "analyze", nil
}

func TestFlowEvaluationServiceForwardsRequest(t *testing.T) {
	engine := &flowEvaluationEngine{}
	request := FlowEvaluationRequest{SuitePath: "suite.yaml", CaseID: "one", OutputDir: "out", InvocationWorkspace: "work", Repeat: 2, KeepWorkspaces: true}
	result, err := NewEvaluation(engine).Flow(context.Background(), request)
	if err != nil || result != "flow" || !reflect.DeepEqual(engine.request, request) {
		t.Fatalf("result=%#v request=%#v err=%v", result, engine.request, err)
	}
}

func TestFlowInitEvaluationServiceForwardsRequest(t *testing.T) {
	engine := &flowEvaluationEngine{}
	result, err := NewEvaluation(engine).FlowInit(context.Background(), "code:feature-development", "out")
	if err != nil || result != "init" || engine.selector != "code:feature-development" || engine.output != "out" {
		t.Fatalf("result=%#v engine=%#v err=%v", result, engine, err)
	}
}

func TestEvaluationStatsServiceForwardsOutputDirectory(t *testing.T) {
	engine := &flowEvaluationEngine{}
	result, err := NewEvaluation(engine).Stats(context.Background(), "run-a")
	if err != nil || result != "stats" || engine.statsPath != "run-a" {
		t.Fatalf("result=%#v path=%q err=%v", result, engine.statsPath, err)
	}
}

func TestEvaluationStatusServiceForwardsOutputDirectory(t *testing.T) {
	engine := &flowEvaluationEngine{}
	result, err := NewEvaluation(engine).Status(context.Background(), "run-a")
	if err != nil || result != "status" || engine.statusPath != "run-a" {
		t.Fatalf("result=%#v path=%q err=%v", result, engine.statusPath, err)
	}
}

func TestEvaluationInspectServiceForwardsFilters(t *testing.T) {
	engine := &flowEvaluationEngine{}
	request := EvaluationInspectRequest{OutputDir: "run-a", CaseID: "case-a", Repeat: 2}
	result, err := NewEvaluation(engine).Inspect(context.Background(), request)
	if err != nil || result != "inspection" || !reflect.DeepEqual(engine.inspectRequest, request) {
		t.Fatalf("result=%#v request=%#v err=%v", result, engine.inspectRequest, err)
	}
}

func TestEvaluationAnalyzeServiceForwardsRequest(t *testing.T) {
	engine := &flowEvaluationEngine{}
	request := EvaluationAnalyzeRequest{OutputDir: "run", ConfigPath: "analyzer.yaml", CaseID: "c", Repeat: 2, ModelPreset: "gemini", Trace: func(string) {}}
	result, err := NewEvaluation(engine).Analyze(context.Background(), request)
	if err != nil || result != "analyze" || engine.analyzeRequest.OutputDir != request.OutputDir || engine.analyzeRequest.ConfigPath != request.ConfigPath || engine.analyzeRequest.CaseID != request.CaseID || engine.analyzeRequest.Repeat != request.Repeat || engine.analyzeRequest.ModelPreset != request.ModelPreset || engine.analyzeRequest.Trace == nil {
		t.Fatalf("result=%#v request=%#v err=%v", result, engine.analyzeRequest, err)
	}
}

func TestFlowEvaluationServiceRejectsNilEngine(t *testing.T) {
	if _, err := (*EvaluationService)(nil).Flow(context.Background(), FlowEvaluationRequest{}); err == nil {
		t.Fatal("expected configuration error")
	}
}
