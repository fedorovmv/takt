package assistant

import (
	"context"
	"testing"
	"time"

	"takt/internal/execution"

	"takt/internal/spec"
)

func TestProcessUsesStdinAndModelEnvironment(t *testing.T) {
	p := Process{spec: spec.AssistantSpec{Type: "process", Argv: []string{"bash", "-lc", `read -r prompt; printf '%s|%s|%s' "$TAKT_MODEL_NAME" "$TAKT_MODEL_ID" "$prompt"`}}}
	got, err := p.Run(context.Background(), Request{Prompt: "hello", Workspace: t.TempDir(), ModelName: "large", Model: spec.ModelSpec{Provider: "demo", ID: "qwen"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Output != "large|qwen|hello" {
		t.Fatalf("unexpected output %q", got.Output)
	}
}

func TestProcessClassifiesStartError(t *testing.T) {
	p := Process{spec: spec.AssistantSpec{Type: "process", Argv: []string{"definitely-missing-takt-binary"}}}
	_, err := p.Run(context.Background(), Request{Workspace: t.TempDir()})
	if err == nil {
		t.Fatal("expected error")
	}
	if execution.KindOf(err) != execution.KindStart {
		t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
	}
}

func TestProcessTimeoutAndOutputLimit(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		p := Process{spec: spec.AssistantSpec{Type: "process", Argv: []string{"bash", "-lc", "sleep 2"}}}
		_, err := p.Run(ctx, Request{Workspace: t.TempDir()})
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("output limit", func(t *testing.T) {
		p := Process{spec: spec.AssistantSpec{Type: "process", Argv: []string{"bash", "-lc", "printf 1234567890"}, MaxOutputBytes: 5}}
		result, err := p.Run(context.Background(), Request{Workspace: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != "12345" || !result.Truncated {
			t.Fatalf("unexpected result: %+v", result)
		}
	})
}

func TestProcessOutputLimitIsRaceSafeAcrossStdoutAndStderr(t *testing.T) {
	p := Process{spec: spec.AssistantSpec{
		Type: "process",
		Argv: []string{"bash", "-lc", `
			(for i in $(seq 1 2000); do printf o; done) &
			(for i in $(seq 1 2000); do printf e >&2; done) &
			wait
		`},
		MaxOutputBytes: 128,
	}}
	result, err := p.Run(context.Background(), Request{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated {
		t.Fatalf("expected truncated output, got %+v", result)
	}
	// combineOutput may insert one separator newline between the independently
	// captured streams. The process bytes themselves remain within the shared budget.
	if len(result.Output) > 129 {
		t.Fatalf("shared output budget exceeded: got %d bytes", len(result.Output))
	}
}
