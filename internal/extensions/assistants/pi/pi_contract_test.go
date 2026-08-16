package pi

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	core "takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
)

type testDeadlineContext struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func newTestDeadlineContext() (*testDeadlineContext, context.CancelFunc) {
	ctx := &testDeadlineContext{Context: context.Background(), done: make(chan struct{})}
	return ctx, func() { ctx.once.Do(func() { close(ctx.done) }) }
}

func (c *testDeadlineContext) Done() <-chan struct{} { return c.done }
func (c *testDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func fakePi(caseName string) Pi {
	return NewPi(spec.AssistantSpec{
		Type:           "pi",
		Binary:         fakePiBinary,
		Args:           []string{"--fake-case", caseName},
		SessionDir:     "/tmp/takt-fake-pi-sessions",
		ProjectTrust:   "approve",
		MaxOutputBytes: 64 * 1024,
		Env:            map[string]string{"TAKT_PI_TEST_ENV": "{{run.id}}/{{node.id}}", "TAKT_RUN_ID": "spoofed"},
	})
}

func fakePiRequest(workspace string) Request {
	return Request{
		RunID:       "run-pi-contract",
		NodeID:      "implement",
		Attempt:     3,
		Prompt:      "implement the route",
		Workspace:   workspace,
		ModelName:   "large",
		Model:       spec.ModelSpec{Provider: "openai", ID: "gpt-test", Params: map[string]any{"reasoning_effort": "high"}},
		SessionMode: "fresh",
		NativeHooks: json.RawMessage(`{"post_tool_use":[{"matcher":"Write"}]}`),
		Metadata:    map[string]string{"suite": "pi-contract"},
	}
}

func TestPiPriorityError(t *testing.T) {
	t.Run("timeout plus overflow keeps timed out", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		<-ctx.Done()
		err := piPriorityError(ctx, true)
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("cancel plus overflow keeps cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := piPriorityError(ctx, true)
		if execution.KindOf(err) != execution.KindCancelled {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})
}

func TestPiRunPreservesContextPriorityWithRealOverflow(t *testing.T) {
	tests := []struct {
		name     string
		caseName string
		wantKind execution.Kind
		context  func(*Pi) (context.Context, context.CancelFunc)
	}{
		{
			name:     "timeout plus overflow",
			caseName: "timeout-overflow",
			wantKind: execution.KindTimedOut,
			context: func(adapter *Pi) (context.Context, context.CancelFunc) {
				ctx, cancel := newTestDeadlineContext()
				adapter.onOutputTruncated = cancel
				return ctx, cancel
			},
		},
		{
			name:     "cancel plus overflow",
			caseName: "cancel-overflow",
			wantKind: execution.KindCancelled,
			context: func(adapter *Pi) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				adapter.onOutputTruncated = cancel
				return ctx, cancel
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := fakePi(tt.caseName)
			adapter.spec.MaxOutputBytes = 1024
			ctx, cancel := tt.context(&adapter)
			defer cancel()

			result, err := adapter.Run(ctx, fakePiRequest(t.TempDir()))
			if execution.KindOf(err) != tt.wantKind {
				t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
			}
			if !result.Truncated {
				t.Fatalf("overflow diagnostic was lost: %+v", result)
			}
			if ctx.Err() == nil || ctx.Done() == nil {
				t.Fatalf("parent context did not complete correctly: err=%v done=%v", ctx.Err(), ctx.Done())
			}
		})
	}
}

func TestPiProviderFailures(t *testing.T) {
	for _, caseName := range []string{"provider-503", "provider-429", "provider-connection-reset", "provider-connection-error"} {
		t.Run(caseName, func(t *testing.T) {
			result, err := fakePi(caseName).Run(context.Background(), fakePiRequest(t.TempDir()))
			if execution.KindOf(err) != execution.KindProviderUnavailable {
				t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
			}
			var executionErr *execution.Error
			if !errors.As(err, &executionErr) || executionErr.RetryAfter != 0 {
				t.Fatalf("Pi internal retry delay leaked as RetryAfter: %+v", executionErr)
			}
			if result.SessionID == "" || result.Usage == nil || result.Usage.InputTokens != 111 || result.Usage.OutputTokens != 22 {
				t.Fatalf("failure lost result identity or usage: %+v", result)
			}
			var structured struct {
				LowLevelRuns     int `json:"low_level_runs"`
				AutomaticRetries int `json:"automatic_retries"`
			}
			if err := json.Unmarshal(result.Structured, &structured); err != nil {
				t.Fatal(err)
			}
			if structured.LowLevelRuns != 1 || structured.AutomaticRetries != 1 {
				t.Fatalf("unexpected retry counters: %+v", structured)
			}
		})
	}
}

func TestPiAdapterContract(t *testing.T) {
	t.Run("emits compact live tool and message events", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		var events []string
		liveContext := 0
		req.Emit = func(event core.Event) {
			events = append(events, event.Type+":"+event.Tool+":"+event.Message+":"+string(event.Input))
			if event.Type == EventMessage && event.Usage != nil {
				liveContext = event.Usage.InputTokens
			}
		}
		if _, err := fakePi("live-events").Run(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(events, "\n")
		for _, want := range []string{"tool.started:read::", `"path":"main.go"`, "tool.completed:read::", "message::inspected main.go:"} {
			if !strings.Contains(joined, want) {
				t.Fatalf("events missing %q:\n%s", want, joined)
			}
		}
		if liveContext != 128430 {
			t.Fatalf("live context tokens=%d", liveContext)
		}
	})

	t.Run("stops after completed tool when provider turn stalls", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		var events []string
		req.Emit = func(event core.Event) { events = append(events, event.Type+":"+event.Tool) }
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_, err := fakePi("tool-then-hang").Run(ctx, req)
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
		if got := strings.Join(events, "\n"); !strings.Contains(got, "tool.started:bash") || !strings.Contains(got, "tool.completed:bash") {
			t.Fatalf("tool progress missing before timeout:\n%s", got)
		}
	})

	t.Run("success and request mapping", func(t *testing.T) {
		result, err := fakePi("success").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != "fake Pi completed" || result.ExitCode != 0 || result.SessionID != "fake-pi-session-1" || result.Resumed {
			t.Fatalf("unexpected result: %+v", result)
		}
		if result.ResolvedModel == nil || result.ResolvedModel.Provider != "openai" || result.ResolvedModel.ID != "gpt-test" {
			t.Fatalf("resolved model missing: %+v", result.ResolvedModel)
		}
		if !strings.Contains(result.AssistantVersion, "0.83.0") {
			t.Fatalf("assistant version missing: %q", result.AssistantVersion)
		}
		if result.Usage == nil || result.Usage.InputTokens != 111 || result.Usage.OutputTokens != 22 || math.Abs(result.Usage.Cost-0.0125) > 1e-9 {
			t.Fatalf("usage missing: %+v", result.Usage)
		}
		var structured struct {
			Adapter          string `json:"adapter"`
			PiVersion        string `json:"pi_version"`
			UsageSemantics   string `json:"usage_semantics"`
			LowLevelRuns     int    `json:"low_level_runs"`
			AutomaticRetries int    `json:"automatic_retries"`
			StatsAfter       struct {
				Observed map[string]any `json:"observed"`
			} `json:"stats_after"`
		}
		if err := json.Unmarshal(result.Structured, &structured); err != nil {
			t.Fatal(err)
		}
		if structured.Adapter != "pi" || !strings.Contains(structured.PiVersion, "0.83.0") || structured.UsageSemantics != "attempt_delta" {
			t.Fatalf("unexpected structured metadata: %+v", structured)
		}
		if structured.LowLevelRuns != 1 || structured.AutomaticRetries != 0 {
			t.Fatalf("unexpected run counters: %+v", structured)
		}
		observed := structured.StatsAfter.Observed
		if observed["provider"] != "openai" || observed["model"] != "gpt-test" || observed["thinking"] != "high" {
			t.Fatalf("model arguments not mapped: %#v", observed)
		}
		if observed["project_trust"] != "approve" || observed["session_dir"] != "/tmp/takt-fake-pi-sessions" {
			t.Fatalf("Pi options not mapped: %#v", observed)
		}
		if observed["prompt"] != "implement the route" || observed["run_id"] != "run-pi-contract" || observed["node_id"] != "implement" {
			t.Fatalf("request not transported: %#v", observed)
		}
		if !strings.Contains(observed["metadata"].(string), "pi-contract") || !strings.Contains(observed["native_hooks"].(string), "post_tool_use") {
			t.Fatalf("metadata/native hooks not transported: %#v", observed)
		}
	})

	t.Run("waits for agent settled across automatic retry", func(t *testing.T) {
		started := time.Now()
		result, err := fakePi("retry-before-settled").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(started); elapsed < 100*time.Millisecond {
			t.Fatalf("adapter returned before agent_settled: %s", elapsed)
		}
		if result.Output != "fake Pi completed" {
			t.Fatalf("adapter returned partial output: %q", result.Output)
		}
		var structured struct {
			LowLevelRuns     int `json:"low_level_runs"`
			AutomaticRetries int `json:"automatic_retries"`
		}
		if err := json.Unmarshal(result.Structured, &structured); err != nil {
			t.Fatal(err)
		}
		if structured.LowLevelRuns != 2 || structured.AutomaticRetries != 1 {
			t.Fatalf("unexpected retry counters: %+v", structured)
		}
	})

	t.Run("fresh ignores stale session", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		req.SessionMode = "fresh"
		req.SessionID = "stale-session"
		result, err := fakePi("success").Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "fake-pi-session-1" || result.Resumed {
			t.Fatalf("unexpected fresh result: %+v", result)
		}
	})

	t.Run("resume", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		result, err := fakePi("success").Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "session-123" || !result.Resumed {
			t.Fatalf("unexpected resume result: %+v", result)
		}
		if result.Usage == nil || result.Usage.InputTokens != 111 || result.Usage.OutputTokens != 22 || math.Abs(result.Usage.Cost-0.0125) > 1e-9 {
			t.Fatalf("resume usage must be per-attempt delta: %+v", result.Usage)
		}
	})

	t.Run("resume mismatch", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		_, err := fakePi("resume-mismatch").Run(context.Background(), req)
		if execution.KindOf(err) != execution.KindProtocol {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("decreasing cumulative stats are protocol error", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		_, err := fakePi("stats-decrease").Run(context.Background(), req)
		if execution.KindOf(err) != execution.KindProtocol {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("disappearing cumulative stats are protocol error", func(t *testing.T) {
		req := fakePiRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		_, err := fakePi("stats-disappear").Run(context.Background(), req)
		if execution.KindOf(err) != execution.KindProtocol {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("explicit zero cumulative usage remains valid", func(t *testing.T) {
		result, err := fakePi("stats-zero").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Usage == nil || result.Usage.InputTokens != 0 || result.Usage.OutputTokens != 0 || result.Usage.Cost != 0 {
			t.Fatalf("unexpected zero usage: %+v", result.Usage)
		}
	})

	t.Run("response model overrides selected model", func(t *testing.T) {
		result, err := fakePi("resolved-model").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.ResolvedModel == nil || result.ResolvedModel.Provider != "openai" || result.ResolvedModel.ID != "resolved-gpt-test" {
			t.Fatalf("responseModel was not preserved: %+v", result.ResolvedModel)
		}
	})

	t.Run("start", func(t *testing.T) {
		adapter := NewPi(spec.AssistantSpec{Type: "pi", Binary: "definitely-missing-pi-binary"})
		_, err := adapter.Run(context.Background(), fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindStart {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("process exit", func(t *testing.T) {
		_, err := fakePi("exit").Run(context.Background(), fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindExit {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		_, err := fakePi("timeout").Run(ctx, fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(80*time.Millisecond, cancel)
		_, err := fakePi("cancel").Run(ctx, fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindCancelled {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("concurrent output", func(t *testing.T) {
		result, err := fakePi("concurrent-output").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Truncated || len(result.Stderr) != 4096 {
			t.Fatalf("unexpected concurrent output result: truncated=%v stderr=%d", result.Truncated, len(result.Stderr))
		}
	})

	t.Run("output limit", func(t *testing.T) {
		adapter := fakePi("concurrent-output")
		adapter.spec.MaxOutputBytes = 512
		result, err := adapter.Run(context.Background(), fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol || !result.Truncated {
			t.Fatalf("unexpected output-limit result: kind=%s result=%+v err=%v", execution.KindOf(err), result, err)
		}
	})

	t.Run("single JSONL record cannot bypass output limit", func(t *testing.T) {
		adapter := fakePi("huge-line")
		adapter.spec.MaxOutputBytes = 512
		result, err := adapter.Run(context.Background(), fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol || !result.Truncated {
			t.Fatalf("unexpected huge-line result: kind=%s result=%+v err=%v", execution.KindOf(err), result, err)
		}
	})

	t.Run("fire-and-forget set_editor_text", func(t *testing.T) {
		result, err := fakePi("extension-ui-set-editor-text").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != "fake Pi completed" {
			t.Fatalf("unexpected output: %q", result.Output)
		}
	})

	t.Run("transient RPC noise does not consume durable output budget", func(t *testing.T) {
		result, err := fakePi("transient-rpc-noise").Run(context.Background(), fakePiRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Truncated {
			t.Fatalf("transient RPC records exhausted output budget: stdout=%d", len(result.Stdout))
		}
		if strings.Contains(result.Stdout, "message_update") || strings.Contains(result.Stdout, "setTitle") {
			t.Fatalf("transient RPC records leaked into durable stdout: %s", result.Stdout)
		}
	})

	t.Run("single transient RPC record cannot bypass output limit", func(t *testing.T) {
		adapter := fakePi("huge-transient-record")
		adapter.spec.MaxOutputBytes = 512
		result, err := adapter.Run(context.Background(), fakePiRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol || !result.Truncated {
			t.Fatalf("unexpected transient record result: kind=%s result=%+v err=%v", execution.KindOf(err), result, err)
		}
	})

	for _, tc := range []struct {
		name     string
		caseName string
		kind     execution.Kind
	}{
		{name: "malformed RPC", caseName: "malformed", kind: execution.KindProtocol},
		{name: "multiple JSON records on one line", caseName: "two-json-on-line", kind: execution.KindProtocol},
		{name: "prompt rejected", caseName: "prompt-rejected", kind: execution.KindExit},
		{name: "agent failure", caseName: "agent-failure", kind: execution.KindExit},
		{name: "model output limit", caseName: "output-limit", kind: execution.KindExit},
		{name: "interactive extension UI", caseName: "extension-ui", kind: execution.KindProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := fakePi(tc.caseName).Run(context.Background(), fakePiRequest(t.TempDir()))
			if execution.KindOf(err) != tc.kind {
				t.Fatalf("unexpected kind for %s: %s (%v)", tc.caseName, execution.KindOf(err), err)
			}
			if tc.caseName == "output-limit" && (result.SessionID != "fake-pi-session-1" || result.Usage == nil || result.Usage.OutputTokens != 22 || !strings.Contains(result.Output, "output limit")) {
				t.Fatalf("output-limit evidence was lost: %+v", result)
			}
		})
	}

	for _, flag := range []string{
		"--mode", "--provider", "--model", "--thinking", "--session", "--session-id", "--session-id=forced",
		"--session-dir", "--no-session", "--continue", "-c", "--resume", "-r", "--fork",
		"--print", "-p", "--version", "-v", "--help", "-h", "--approve", "-a", "--no-approve", "-na",
	} {
		t.Run("reserved argument "+strings.ReplaceAll(flag, "/", "_"), func(t *testing.T) {
			adapter := fakePi("success")
			adapter.spec.Args = []string{flag}
			_, err := adapter.Run(context.Background(), fakePiRequest(t.TempDir()))
			if execution.KindOf(err) != execution.KindProtocol {
				t.Fatalf("unexpected kind for %s: %s (%v)", flag, execution.KindOf(err), err)
			}
		})
	}
}

func TestPiProgressEventExposesLastModelInputTokens(t *testing.T) {
	event, ok := piProgressEvent(piRPCRecord{Type: "message_end", Raw: json.RawMessage(`{"type":"message_end","message":{"role":"assistant","content":[],"usage":{"input":128430,"output":512}}}`)})
	if !ok || event.Type != EventMessage || event.Usage == nil || event.Usage.InputTokens != 128430 || event.Usage.OutputTokens != 512 {
		t.Fatalf("event=%+v ok=%t", event, ok)
	}
}

func TestPiAdapterOptInSmoke(t *testing.T) {
	if os.Getenv("TAKT_PI_SMOKE") != "1" {
		t.Skip("set TAKT_PI_SMOKE=1 and TAKT_PI_SMOKE_MODEL to run a real Pi prompt")
	}
	model := os.Getenv("TAKT_PI_SMOKE_MODEL")
	provider := os.Getenv("TAKT_PI_SMOKE_PROVIDER")
	if model == "" || provider == "" {
		t.Fatal("TAKT_PI_SMOKE_MODEL and TAKT_PI_SMOKE_PROVIDER are required")
	}
	binary := os.Getenv("TAKT_PI_BINARY")
	if binary == "" {
		binary = "pi"
	}
	if _, err := exec.LookPath(binary); err != nil {
		t.Fatal(err)
	}
	adapter := NewPi(spec.AssistantSpec{Type: "pi", Binary: binary, ProjectTrust: "deny", MaxOutputBytes: 2 * 1024 * 1024})
	req := Request{
		RunID: "pi-smoke", NodeID: "smoke", Attempt: 1,
		Prompt:    "Reply with exactly: TAKT_PI_SMOKE_OK",
		Workspace: t.TempDir(), ModelName: "smoke",
		Model: spec.ModelSpec{Provider: provider, ID: model}, SessionMode: "fresh",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := adapter.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "TAKT_PI_SMOKE_OK") {
		t.Fatalf("unexpected Pi smoke output: %q", result.Output)
	}
	if result.ResolvedModel == nil || result.ResolvedModel.Provider == "" || result.ResolvedModel.ID == "" {
		t.Fatalf("Pi smoke did not expose resolved model: %+v", result.ResolvedModel)
	}
	if result.SessionID == "" || result.AssistantVersion == "" {
		t.Fatalf("Pi smoke did not expose session/version: %+v", result)
	}
	freshSessionID := result.SessionID
	req.Attempt = 2
	req.Prompt = "Reply with exactly: TAKT_PI_RESUME_OK"
	req.SessionMode = "resume"
	req.SessionID = freshSessionID
	resumed, err := adapter.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resumed.Output, "TAKT_PI_RESUME_OK") || resumed.SessionID != freshSessionID || !resumed.Resumed {
		t.Fatalf("Pi smoke did not resume exact session: fresh=%+v resumed=%+v", result, resumed)
	}
}

func TestPiPolicyArguments(t *testing.T) {
	skill := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(skill, []byte("# skill"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := fakePiRequest(t.TempDir())
	req.Policy = Policy{
		AllowedTools: []string{"read", "grep"}, ToolsRestricted: true,
		DeniedTools: []string{"write"}, Skills: []string{skill}, SkillsRestricted: true,
		Filesystem: "read_only",
	}
	if err := validatePiPolicy(req.Policy); err != nil {
		t.Fatal(err)
	}
	args := piArgs(spec.AssistantSpec{}, req)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tools grep,read") || !strings.Contains(joined, "--no-skills") || !strings.Contains(joined, "--skill "+skill) {
		t.Fatalf("Pi policy was not translated to CLI args: %v", args)
	}
	if strings.Contains(joined, "write") {
		t.Fatalf("denied write tool leaked into Pi allowlist: %v", args)
	}
}
