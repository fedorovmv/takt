package runtime

import (
	"strings"
	"testing"

	"takt/internal/store"
)

func TestStrictTemplateRequiredOptionalAndDefault(t *testing.T) {
	state := &store.RunState{Input: "request", Nodes: map[string]*store.NodeState{
		"producer": {Output: `{"summary":"ok","empty":""}`, Status: store.NodeCompleted},
	}, Approvals: map[string]string{}}
	value, err := renderTemplate("${nodes.producer.output.summary}|${nodes.producer.output.missing?}|${nodes.producer.output.empty:-fallback}|${nodes.producer.output.missing:-default}", state, nil, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if value != "ok||fallback|default" {
		t.Fatalf("rendered = %q", value)
	}
	_, err = renderTemplate("${nodes.producer.output.missing}", state, nil, "", "")
	if err == nil || !strings.Contains(err.Error(), "unresolved template expression") {
		t.Fatalf("expected unresolved error, got %v", err)
	}
}
