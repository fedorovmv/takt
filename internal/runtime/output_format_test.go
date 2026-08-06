package runtime

import (
	"context"
	"strings"
	"testing"

	"takt/internal/assistant"
	"takt/internal/spec"
	"takt/internal/store"
)

func TestStructuredOutputIsValidatedAndNormalized(t *testing.T) {
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "structured"},
		Defaults:   spec.Defaults{Assistant: "demo", Model: "m"},
		Nodes: []spec.Node{{
			ID:     "route",
			Prompt: "route",
			OutputFormat: &spec.OutputFormat{
				Type: "object",
				Properties: map[string]spec.OutputFormat{
					"workflow": {Type: "string", Enum: []string{"assist", "review"}},
				},
				Required: []string{"workflow"},
			},
		}},
	}
	cfg := &spec.Config{
		Models:     map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}},
		Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", t.TempDir())
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
			return assistant.Result{Output: " { \"workflow\" : \"assist\" } ", ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Nodes["route"].Output; got != `{"workflow":"assist"}` {
		t.Fatalf("unexpected normalized output %q", got)
	}
}

func TestStructuredOutputPreservesRawStdout(t *testing.T) {
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "raw-stdout"},
		Defaults: spec.Defaults{Assistant: "demo", Model: "m"},
		Nodes: []spec.Node{{ID: "route", Prompt: "route", OutputFormat: &spec.OutputFormat{
			Type: "object", Properties: map[string]spec.OutputFormat{"workflow": {Type: "string"}}, Required: []string{"workflow"},
		}}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	r := New(wf, cfg, "<workflow>", "<config>", t.TempDir())
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
			return assistant.Result{Output: ` { "workflow" : "assist" } `, Stdout: "{\"type\":\"start\"}\n{\"type\":\"result\"}\n", ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	node := state.Nodes["route"]
	if node.Output != `{"workflow":"assist"}` {
		t.Fatalf("unexpected normalized output %q", node.Output)
	}
	if node.Stdout != "{\"type\":\"start\"}\n{\"type\":\"result\"}\n" {
		t.Fatalf("raw stdout was overwritten: %q", node.Stdout)
	}
}

func TestStructuredOutputFailureIsProtocolError(t *testing.T) {
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1",
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: "structured"},
		Defaults:   spec.Defaults{Assistant: "demo", Model: "m"},
		Nodes: []spec.Node{{
			ID:     "route",
			Prompt: "route",
			OutputFormat: &spec.OutputFormat{
				Type:       "object",
				Properties: map[string]spec.OutputFormat{"workflow": {Type: "string", Enum: []string{"assist"}}},
				Required:   []string{"workflow"},
			},
		}},
	}
	cfg := &spec.Config{
		Models:     map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}},
		Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}},
	}
	r := New(wf, cfg, "<workflow>", "<config>", t.TempDir())
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(context.Context, assistant.Request) (assistant.Result, error) {
			return assistant.Result{Output: `{"workflow":"missing"}`, ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err == nil {
		t.Fatal("expected structured output failure")
	}
	if state.Nodes["route"].ErrorCode != "protocol" || !strings.Contains(state.Nodes["route"].Error, "not in enum") {
		t.Fatalf("unexpected node state: %+v", state.Nodes["route"])
	}
}

func TestJSONOutputFieldsAreAvailableToConditionsAndTemplates(t *testing.T) {
	state := &store.RunState{
		Input: "request",
		Nodes: map[string]*store.NodeState{
			"route": {Status: store.NodeCompleted, Output: `{"workflow":"assist","details":{"level":"full"}}`},
		},
	}
	ok, err := evalWhen(`nodes.route.output.workflow == "assist"`, state)
	if err != nil || !ok {
		t.Fatalf("unexpected condition result ok=%v err=%v", ok, err)
	}
	got, err := renderTemplate("${nodes.route.output.details.level}", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "full" {
		t.Fatalf("unexpected rendered JSON field %q", got)
	}
}

func TestStructuredOutputIntegerPreservesLargeExactValues(t *testing.T) {
	schema := &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"value": {Type: "integer"}}, Required: []string{"value"}}
	for _, raw := range []string{`{"value":9007199254740993123456789}`, `{"value":1e30}`, `{"value":100.000}`} {
		if _, err := validateAndNormalizeOutput(raw, schema); err != nil {
			t.Fatalf("%s should be an integer: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"value":9007199254740993.5}`, `{"value":1e-3}`, `{"value":100.001}`} {
		if _, err := validateAndNormalizeOutput(raw, schema); err == nil {
			t.Fatalf("%s should not be an integer", raw)
		}
	}
}

func TestStructuredOutputRetryPolicyPassesValidationFeedback(t *testing.T) {
	wf := &spec.Workflow{
		APIVersion: "takt/v1alpha1", Kind: "Workflow", Metadata: spec.Metadata{Name: "structured-retry"},
		Defaults: spec.Defaults{Assistant: "demo", Model: "m", Session: "reuse"},
		Nodes: []spec.Node{{
			ID: "route", Prompt: "Choose route.\nFeedback:\n${feedback}",
			Attempts: spec.AttemptsSpec{Max: 2, RetryOn: []string{"protocol"}, RetrySession: "fresh"},
			OutputFormat: &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{
				"workflow": {Type: "string", Enum: []string{"assist"}},
			}, Required: []string{"workflow"}},
		}},
	}
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{"m": {Provider: "test", ID: "m"}}, Assistants: map[string]spec.AssistantSpec{"demo": {Type: "mock"}}}
	var prompts []string
	r := New(wf, cfg, "<workflow>", "<config>", t.TempDir())
	r.Assistants = resolverFunc(func(string) (assistant.Adapter, error) {
		return adapterFunc(func(_ context.Context, req assistant.Request) (assistant.Result, error) {
			prompts = append(prompts, req.Prompt)
			if len(prompts) == 1 {
				return assistant.Result{Output: `{"workflow":"missing"}`, Stdout: "raw-first", SessionID: "session-1", ExitCode: 0}, nil
			}
			if req.SessionID != "" {
				t.Fatalf("fresh retry reused session %q", req.SessionID)
			}
			return assistant.Result{Output: `{"workflow":"assist"}`, Stdout: "raw-second", ExitCode: 0}, nil
		}), nil
	})
	state, err := r.Start(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "not in enum") {
		t.Fatalf("validation feedback was not passed to retry: %#v", prompts)
	}
	node := state.Nodes["route"]
	if node.Attempts != 2 || node.Output != `{"workflow":"assist"}` || node.Stdout != "raw-second" {
		t.Fatalf("unexpected retried node: %+v", node)
	}
}

func TestOutputFormatArrayCardinalityAndUniqueness(t *testing.T) {
	format := &spec.OutputFormat{Type: "array", MinItems: 1, UniqueItems: true, Items: &spec.OutputFormat{Type: "string"}}
	if _, err := validateAndNormalizeOutput(`[]`, format); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Fatalf("expected minItems error, got %v", err)
	}
	if _, err := validateAndNormalizeOutput(`["code","code"]`, format); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected uniqueItems error, got %v", err)
	}
	if got, err := validateAndNormalizeOutput(`["code","security"]`, format); err != nil || got != `["code","security"]` {
		t.Fatalf("valid unique array: got=%q err=%v", got, err)
	}
}

func TestWhenSupportsAndOrExpressions(t *testing.T) {
	state := &store.RunState{Nodes: map[string]*store.NodeState{
		"scope": {Status: store.NodeCompleted, Output: `{"status":"ready","code":"OK"}`},
		"check": {Status: store.NodeCompleted, Output: `{"status":"failed"}`},
	}}
	ok, err := evalWhen(`nodes.scope.output.status == "ready" && nodes.scope.output.code == "OK"`, state)
	if err != nil || !ok {
		t.Fatalf("and expression: ok=%v err=%v", ok, err)
	}
	ok, err = evalWhen(`nodes.check.output.status == "ready" || nodes.scope.output.status == "ready"`, state)
	if err != nil || !ok {
		t.Fatalf("or expression: ok=%v err=%v", ok, err)
	}
}
