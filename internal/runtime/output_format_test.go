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
	got := renderTemplate("${nodes.route.output.details.level}", state, nil, "", "")
	if got != "full" {
		t.Fatalf("unexpected rendered JSON field %q", got)
	}
}
