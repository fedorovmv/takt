package tooling

import (
	"context"
	"reflect"
	"testing"
)

type flowEvaluationEngine struct {
	request          FlowEvaluationRequest
	selector, output string
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
func (e *flowEvaluationEngine) Flow(_ context.Context, request FlowEvaluationRequest) (any, error) {
	e.request = request
	return "flow", nil
}
func (e *flowEvaluationEngine) FlowInit(_ context.Context, selector, output string) (any, error) {
	e.selector, e.output = selector, output
	return "init", nil
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

func TestFlowEvaluationServiceRejectsNilEngine(t *testing.T) {
	if _, err := (*EvaluationService)(nil).Flow(context.Background(), FlowEvaluationRequest{}); err == nil {
		t.Fatal("expected configuration error")
	}
}
