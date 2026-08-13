package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

type fixedToolController struct {
	decision ToolDecision
}

func (c fixedToolController) Decide(_ context.Context, _ ToolRequest) (ToolDecision, error) {
	return c.decision, nil
}

func TestProcessV1Alpha2NegotiatesToolDecisionAndEmitsEvents(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/worker.py"
	code := `import json, sys
req=json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":["tool_control"],"event_types":["tool.requested","tool.allowed","tool.started","tool.completed"],"tool_events":True,"tool_control":True}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"tool.request","tool_request":{"call_id":"call-1","tool":"read","input":{"path":"README.md"},"session_id":"session-1"}}), flush=True)
dec=json.loads(sys.stdin.readline())
if dec["decision"]["decision"] == "allow":
  print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"event","event":{"type":"tool.started","tool":"read","call_id":"call-1","session_id":"session-1"}}), flush=True)
  print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"event","event":{"type":"tool.completed","tool":"read","call_id":"call-1","output":{"ok":True},"session_id":"session-1"}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","output":"done","session":{"id":"session-1","resumed":False},"exit_code":0}}), flush=True)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}}}
	var events []Event
	result, err := p.Run(context.Background(), Request{
		Prompt: "work", Workspace: dir, Policy: Policy{ToolsRestricted: true, AllowedTools: []string{"read"}},
		ToolControl: fixedToolController{decision: ToolDecision{Decision: "allow", Reason: "approved"}},
		Emit:        func(event Event) { events = append(events, event) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Output != "done" || result.SessionID != "session-1" {
		t.Fatalf("result = %#v", result)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type)
	}
	want := []string{EventToolRequested, EventToolAllowed, EventToolStarted, EventToolCompleted}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %#v, want %#v", types, want)
	}
}

func TestProcessV1Alpha2PolicyDeniesBeforeToolStart(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/worker.py"
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","tool_events":True,"tool_control":True}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"tool.request","tool_request":{"call_id":"call-1","tool":"write","input":{"path":"x"}}}), flush=True)
dec=json.loads(sys.stdin.readline())
assert dec["decision"]["decision"] == "deny"
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","output":"denied safely","exit_code":0}}), flush=True)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}}}
	var events []Event
	_, err := p.Run(context.Background(), Request{Workspace: dir, Policy: Policy{DeniedTools: []string{"write"}}, Emit: func(event Event) { events = append(events, event) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Type != EventToolRequested || events[1].Type != EventToolDenied {
		t.Fatalf("events = %#v", events)
	}
}

func TestProcessV1Alpha2ProtocolErrorTerminatesWorker(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/worker.py"
	pidFile := dir + "/worker.pid"
	code := `import os, sys, time
open(sys.argv[1], "w").write(str(os.getpid()))
print("not-json", flush=True)
time.sleep(30)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script, pidFile}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProtocol {
		t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
	}
	raw, readErr := os.ReadFile(pidFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(raw)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if killErr := syscall.Kill(pid, 0); killErr == nil {
		t.Fatalf("worker process %d remains alive after protocol failure", pid)
	}
}

func TestProcessV1Alpha2DoesNotInferToolControlFromProtocolVersion(t *testing.T) {
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"adapter"}, Capabilities: []string{CapabilityAgentEventsV2, CapabilitySessionEvents, CapabilityUsageEvents}}}
	decl := p.CapabilityDeclaration()
	if decl.ToolControl || decl.ToolEvents || decl.ArtifactEvents {
		t.Fatalf("v1alpha2 overclaimed capabilities: %+v", decl)
	}
	if !decl.SessionEvents || !decl.UsageEvents {
		t.Fatalf("configured event capabilities missing: %+v", decl)
	}
}

func TestProcessV1Alpha2RejectsConfiguredCapabilityMissingFromStreamDeclaration(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/worker.py"
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":["agent_events_v2"],"event_types":["completed"]}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","output":"done","exit_code":0}}), flush=True)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}, Capabilities: []string{CapabilityUsageEvents}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProtocol || !strings.Contains(err.Error(), "usage_events") {
		t.Fatalf("expected capability protocol failure, got %v", err)
	}
}

func TestProcessV1Alpha2RejectsToolRequestWithoutDeclaredToolControl(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/worker.py"
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":["agent_events_v2"],"event_types":["message"]}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"tool.request","tool_request":{"call_id":"call-1","tool":"read"}}), flush=True)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}, Capabilities: []string{CapabilityAgentEventsV2}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProtocol || !strings.Contains(err.Error(), "without declared tool_control") {
		t.Fatalf("expected tool-control protocol failure, got %v", err)
	}
}

func TestProcessV1Alpha2MapsDeclaredTimedOutFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.py")
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":[],"event_types":[]}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"failed","failure_kind":"timed_out","exit_code":55}}), flush=True)
sys.exit(55)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindTimedOut {
		t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
	}
}

func TestProcessV1Alpha2RejectsNestedV1Alpha1ProviderUnavailableResult(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.py")
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":[],"event_types":[]}}), flush=True)
print(sys.argv[1], flush=True)
sys.exit(1)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	result := `{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha1","type":"result","status":"failed","failure_kind":"provider_unavailable","session":{"id":"session-1"},"exit_code":1}}`
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script, result}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProtocol {
		t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
	}
}

func TestProcessFailureKindValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		protocol string
		result   string
	}{
		{name: "unknown failure kind", protocol: ProtocolV1Alpha2, result: `{"status":"failed","failure_kind":"other","exit_code":1}`},
		{name: "negative retry delay", protocol: ProtocolV1Alpha2, result: `{"status":"failed","failure_kind":"provider_unavailable","retry_after_ms":-1,"exit_code":1,"session":{"id":"session-1"}}`},
		{name: "retry delay on exit", protocol: ProtocolV1Alpha2, result: `{"status":"failed","failure_kind":"exit","retry_after_ms":1,"exit_code":1}`},
		{name: "provider unavailable missing session", protocol: ProtocolV1Alpha2, result: `{"status":"failed","failure_kind":"provider_unavailable","exit_code":1}`},
		{name: "v1alpha1 provider failure", protocol: ProtocolV1Alpha1, result: `{"status":"failed","failure_kind":"provider_unavailable","exit_code":1,"session":{"id":"session-1"}}`},
		{name: "v1alpha1 retry delay", protocol: ProtocolV1Alpha1, result: `{"status":"failed","failure_kind":"exit","retry_after_ms":1,"exit_code":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			script := filepath.Join(dir, "worker.py")
			if tc.protocol == ProtocolV1Alpha2 {
				result := `{"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result",` + tc.result[1:]
				code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":[],"event_types":[]}}), flush=True)
print(sys.argv[1], flush=True)
sys.exit(1)
`
				if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
					t.Fatal(err)
				}
				p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: tc.protocol, Argv: []string{"python3", script, result}}}
				_, err := p.Run(context.Background(), Request{Workspace: dir})
				if execution.KindOf(err) != execution.KindProtocol {
					t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
				}
				return
			}
			result := `{"protocol_version":"takt-assistant/v1alpha1","type":"result",` + tc.result[1:]
			code := `import sys
print(sys.argv[1])
sys.exit(1)
`
			if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
				t.Fatal(err)
			}
			p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: tc.protocol, Argv: []string{"python3", script, result}}}
			_, err := p.Run(context.Background(), Request{Workspace: dir})
			if execution.KindOf(err) != execution.KindProtocol {
				t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
			}
		})
	}
}

func TestProcessV1Alpha2EmptyEventTypesIsDenyAll(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.py")
	code := `import json, sys
json.loads(sys.stdin.readline())
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"capabilities","declaration":{"protocol":"takt-agent-events/v2","capabilities":[],"event_types":[]}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"event","event":{"type":"message","message":"must be denied"}}), flush=True)
print(json.dumps({"protocol_version":"takt-assistant/v1alpha2","type":"result","result":{"protocol_version":"takt-assistant/v1alpha2","type":"result","status":"completed","exit_code":0}}), flush=True)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script}}}
	_, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProtocol || !strings.Contains(err.Error(), `event "message" was not declared`) {
		t.Fatalf("empty event_types must deny all events, got %v", err)
	}
}
