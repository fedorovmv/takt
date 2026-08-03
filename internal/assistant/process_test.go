package assistant

import (
	"context"
	"testing"

	"takt/internal/spec"
)

func TestProcessUsesStdinAndModelEnvironment(t *testing.T) {
	p := Process{spec: spec.AssistantSpec{Type: "process", Argv: []string{"bash", "-lc", `read -r prompt; printf '%s|%s|%s' "$HARNESS_MODEL_NAME" "$HARNESS_MODEL_ID" "$prompt"`}}}
	got, err := p.Run(context.Background(), Request{Prompt: "hello", Workspace: t.TempDir(), ModelName: "large", Model: spec.ModelSpec{Provider: "demo", ID: "qwen"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "large|qwen|hello" {
		t.Fatalf("unexpected output %q", got.Output)
	}
}
