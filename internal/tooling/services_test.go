package tooling

import (
	"context"
	"testing"
)

type flowEvaluationEngine struct{ request FlowEvaluationRequest }

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
func (e *flowEvaluationEngine) Flow(_ context.Context, request FlowEvaluationRequest) (any, error) {
	e.request = request
	return "flow", nil
}

func TestFlowEvaluationServiceForwardsRequest(t *testing.T) {
	engine := &flowEvaluationEngine{}
	request := FlowEvaluationRequest{SuitePath: "suite.yaml", CaseID: "one", OutputDir: "out", InvocationWorkspace: "work", Repeat: 2, KeepWorkspaces: true}
	result, err := NewEvaluation(engine).Flow(context.Background(), request)
	if err != nil || result != "flow" || engine.request != request {
		t.Fatalf("result=%#v request=%#v err=%v", result, engine.request, err)
	}
}

func TestFlowEvaluationServiceRejectsNilEngine(t *testing.T) {
	if _, err := (*EvaluationService)(nil).Flow(context.Background(), FlowEvaluationRequest{}); err == nil {
		t.Fatal("expected configuration error")
	}
}
