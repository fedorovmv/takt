package assistant

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/execution"
	"takt/internal/spec"
)

func fakeOpenCode(caseName string) OpenCode {
	return NewOpenCode(spec.AssistantSpec{
		Type:           "opencode",
		Binary:         fakeOpenCodeBinary,
		Args:           []string{"--fake-case", caseName},
		Agent:          "build",
		AutoApprove:    true,
		MaxOutputBytes: 64 * 1024,
		Env:            map[string]string{"TAKT_OC_TEST_ENV": "{{run.id}}/{{node.id}}", "TAKT_RUN_ID": "spoofed"},
	})
}

func fakeOpenCodeRequest(workspace string) Request {
	return Request{
		RunID:       "run-opencode-contract",
		NodeID:      "implement",
		Attempt:     3,
		Prompt:      "implement the route",
		Workspace:   workspace,
		ModelName:   "large",
		Model:       spec.ModelSpec{Provider: "openai", ID: "qwen-test", Params: map[string]any{"variant": "high"}},
		SessionMode: "fresh",
		NativeHooks: json.RawMessage(`{"post_tool_use":[{"matcher":"Write"}]}`),
		Metadata:    map[string]string{"suite": "opencode-contract"},
	}
}

func TestOpenCodeRunPreservesContextPriorityWithRealOverflow(t *testing.T) {
	tests := []struct {
		name     string
		wantKind execution.Kind
		context  func(*OpenCode) (context.Context, context.CancelFunc)
	}{
		{
			name:     "timeout plus overflow",
			wantKind: execution.KindTimedOut,
			context: func(adapter *OpenCode) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				adapter.onOutputTruncated = func() { <-ctx.Done() }
				return ctx, cancel
			},
		},
		{
			name:     "cancel plus overflow",
			wantKind: execution.KindCancelled,
			context: func(adapter *OpenCode) (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				adapter.onOutputTruncated = cancel
				return ctx, cancel
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := fakeOpenCode("overflow")
			adapter.spec.MaxOutputBytes = 1024
			ctx, cancel := tt.context(&adapter)
			defer cancel()
			result, err := adapter.Run(ctx, fakeOpenCodeRequest(t.TempDir()))
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

func TestOpenCodeAdapterContract(t *testing.T) {
	t.Run("success and request mapping", func(t *testing.T) {
		workspace := t.TempDir()
		result, err := fakeOpenCode("success").Run(context.Background(), fakeOpenCodeRequest(workspace))
		if err != nil {
			t.Fatal(err)
		}
		if result.Output != "fake OpenCode completed" || result.ExitCode != 0 || result.SessionID != "ses-opencode-1" || result.Resumed {
			t.Fatalf("unexpected result: %+v", result)
		}
		if result.ResolvedModel == nil || result.ResolvedModel.Provider != "openai" || result.ResolvedModel.ID != "qwen-test" {
			t.Fatalf("resolved model missing: %+v", result.ResolvedModel)
		}
		if !strings.Contains(result.AssistantVersion, "1.2.3-test") {
			t.Fatalf("assistant version missing: %q", result.AssistantVersion)
		}
		if result.Usage == nil || result.Usage.InputTokens != 101 || result.Usage.OutputTokens != 17 || math.Abs(result.Usage.Cost-0.0042) > 1e-9 {
			t.Fatalf("usage missing: %+v", result.Usage)
		}
		var structured struct {
			Adapter               string `json:"adapter"`
			UsageSemantics        string `json:"usage_semantics"`
			ModelResolutionSource string `json:"model_resolution_source"`
			EventCount            int    `json:"event_count"`
			StepCount             int    `json:"step_count"`
			ToolCalls             int    `json:"tool_calls"`
		}
		if err := json.Unmarshal(result.Structured, &structured); err != nil {
			t.Fatal(err)
		}
		if structured.Adapter != "opencode" || structured.UsageSemantics != "sum_step_finish" || structured.ModelResolutionSource != "requested_cli_model" || structured.EventCount != 4 || structured.StepCount != 1 || structured.ToolCalls != 1 {
			t.Fatalf("unexpected structured metadata: %+v", structured)
		}
		data, err := os.ReadFile(filepath.Join(workspace, ".fake-opencode-observed.json"))
		if err != nil {
			t.Fatal(err)
		}
		var observed map[string]any
		if err := json.Unmarshal(data, &observed); err != nil {
			t.Fatal(err)
		}
		if observed["model"] != "openai/qwen-test" || observed["agent"] != "build" || observed["variant"] != "high" || observed["auto"] != true {
			t.Fatalf("OpenCode options not mapped: %#v", observed)
		}
		if observed["prompt"] != "implement the route" || observed["run_id"] != "run-opencode-contract" || observed["node_id"] != "implement" {
			t.Fatalf("request not transported: %#v", observed)
		}
		if !strings.Contains(observed["metadata"].(string), "opencode-contract") || !strings.Contains(observed["native_hooks"].(string), "post_tool_use") {
			t.Fatalf("metadata/native hooks not transported: %#v", observed)
		}
	})

	t.Run("fresh ignores stale session", func(t *testing.T) {
		req := fakeOpenCodeRequest(t.TempDir())
		req.SessionID = "stale-session"
		result, err := fakeOpenCode("success").Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "ses-opencode-1" || result.Resumed {
			t.Fatalf("unexpected fresh result: %+v", result)
		}
	})

	t.Run("resume", func(t *testing.T) {
		req := fakeOpenCodeRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "ses-123"
		result, err := fakeOpenCode("success").Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "ses-123" || !result.Resumed {
			t.Fatalf("unexpected resume result: %+v", result)
		}
	})

	t.Run("stderr warning remains diagnostic", func(t *testing.T) {
		result, err := fakeOpenCode("warning").Run(context.Background(), fakeOpenCodeRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(result.Stderr, "fake warning") || result.Output != "fake OpenCode completed" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	for _, tc := range []struct {
		name     string
		caseName string
		kind     execution.Kind
	}{
		{name: "resume mismatch", caseName: "resume-mismatch", kind: execution.KindProtocol},
		{name: "malformed event", caseName: "malformed", kind: execution.KindProtocol},
		{name: "error event with zero process exit", caseName: "error-zero-exit", kind: execution.KindExit},
		{name: "process exit", caseName: "exit", kind: execution.KindExit},
		{name: "missing usage", caseName: "missing-usage", kind: execution.KindProtocol},
		{name: "negative usage", caseName: "negative-usage", kind: execution.KindProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := fakeOpenCodeRequest(t.TempDir())
			if tc.caseName == "resume-mismatch" {
				req.SessionMode = "resume"
				req.SessionID = "ses-123"
			}
			_, err := fakeOpenCode(tc.caseName).Run(context.Background(), req)
			if execution.KindOf(err) != tc.kind {
				t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
			}
		})
	}

	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		_, err := fakeOpenCode("timeout").Run(ctx, fakeOpenCodeRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("provider diagnostics survive timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		result, err := fakeOpenCode("provider-timeout").Run(ctx, fakeOpenCodeRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
		for _, fragment := range []string{"retrying request 2/3", "connection refused"} {
			if !strings.Contains(result.Output, fragment) {
				t.Fatalf("diagnostic %q missing from output: %+v", fragment, result)
			}
			if !strings.Contains(err.Error(), fragment) {
				t.Fatalf("diagnostic %q missing from error: %v", fragment, err)
			}
		}
		if !strings.Contains(result.Stderr, "provider endpoint unavailable") {
			t.Fatalf("stderr diagnostic was lost: %+v", result)
		}
		if !strings.Contains(result.Stdout, `"type":"error"`) {
			t.Fatalf("structured provider error was lost: %+v", result)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		_, err := fakeOpenCode("cancel").Run(ctx, fakeOpenCodeRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindCancelled {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("output overflow", func(t *testing.T) {
		adapter := fakeOpenCode("overflow")
		adapter.spec.MaxOutputBytes = 1024
		result, err := adapter.Run(context.Background(), fakeOpenCodeRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol || !result.Truncated {
			t.Fatalf("unexpected overflow result: %+v err=%v", result, err)
		}
	})

	for _, flag := range []string{"run", "--format", "--model", "-m", "--agent", "--session", "-s", "--continue", "-c", "--dir", "--variant", "--auto", "--version"} {
		t.Run("reserved argument "+strings.ReplaceAll(flag, "/", "_"), func(t *testing.T) {
			adapter := fakeOpenCode("success")
			adapter.spec.Args = []string{flag}
			_, err := adapter.Run(context.Background(), fakeOpenCodeRequest(t.TempDir()))
			if execution.KindOf(err) != execution.KindProtocol {
				t.Fatalf("unexpected kind for %s: %s (%v)", flag, execution.KindOf(err), err)
			}
		})
	}

	t.Run("missing binary", func(t *testing.T) {
		adapter := NewOpenCode(spec.AssistantSpec{Type: "opencode", Binary: "definitely-missing-opencode"})
		_, err := adapter.Run(context.Background(), fakeOpenCodeRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindStart {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})
}

func TestOpenCodeAdapterOptInSmoke(t *testing.T) {
	if os.Getenv("TAKT_OPENCODE_SMOKE") != "1" {
		t.Skip("set TAKT_OPENCODE_SMOKE=1 and model/provider variables to run a real OpenCode prompt")
	}
	model := os.Getenv("TAKT_OPENCODE_SMOKE_MODEL")
	provider := os.Getenv("TAKT_OPENCODE_SMOKE_PROVIDER")
	if model == "" || provider == "" {
		t.Fatal("TAKT_OPENCODE_SMOKE_MODEL and TAKT_OPENCODE_SMOKE_PROVIDER are required")
	}
	binary := os.Getenv("TAKT_OPENCODE_BINARY")
	if binary == "" {
		binary = "opencode"
	}
	if _, err := exec.LookPath(binary); err != nil {
		t.Fatal(err)
	}
	adapter := NewOpenCode(spec.AssistantSpec{Type: "opencode", Binary: binary, Agent: os.Getenv("TAKT_OPENCODE_SMOKE_AGENT"), MaxOutputBytes: 2 * 1024 * 1024})
	req := Request{
		RunID: "opencode-smoke", NodeID: "smoke", Attempt: 1,
		Prompt: "Reply with exactly: TAKT_OPENCODE_SMOKE_OK", Workspace: t.TempDir(), ModelName: "smoke",
		Model: spec.ModelSpec{Provider: provider, ID: model}, SessionMode: "fresh",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := adapter.Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "TAKT_OPENCODE_SMOKE_OK") {
		t.Fatalf("unexpected OpenCode smoke output: %q", result.Output)
	}
	if result.SessionID == "" || result.ResolvedModel == nil || result.ResolvedModel.Provider != provider || result.ResolvedModel.ID != model || result.Usage == nil {
		t.Fatalf("OpenCode smoke did not expose execution identity/usage: %+v", result)
	}
}

func TestOpenCodePolicyConfig(t *testing.T) {
	req := fakeOpenCodeRequest(t.TempDir())
	req.Policy = Policy{
		AllowedTools: []string{"read", "grep"}, ToolsRestricted: true,
		DeniedTools: []string{"bash"}, Skills: []string{"review"}, SkillsRestricted: true,
		Filesystem: "read_only", MCPPath: "mcp.json", MCPConfig: json.RawMessage(`{"docs":{"type":"remote","url":"https://example.invalid/mcp"}}`),
	}
	env, err := openCodeEnvironment(spec.AssistantSpec{Env: map[string]string{"OPENCODE_CONFIG_CONTENT": `{"snapshot":false}`}}, req)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(values["OPENCODE_CONFIG_CONTENT"]), &config); err != nil {
		t.Fatal(err)
	}
	if config["snapshot"] != false {
		t.Fatalf("existing inline config was lost: %#v", config)
	}
	permission, ok := config["permission"].(map[string]any)
	if !ok || permission["*"] != "deny" || permission["read"] != "allow" || permission["bash"] != "deny" || permission["edit"] != "deny" || permission["write"] != "deny" {
		t.Fatalf("OpenCode permissions are wrong: %#v", config["permission"])
	}
	if _, ok := config["mcp"].(map[string]any); !ok {
		t.Fatalf("MCP config was not injected: %#v", config)
	}
	if values["TAKT_POLICY_JSON"] == "" {
		t.Fatal("policy audit environment is missing")
	}
}

func TestOpenCodeReadOnlyOverridesExplicitWriteAllow(t *testing.T) {
	config, err := openCodePolicyConfig(Policy{AllowedTools: []string{"read", "write"}, ToolsRestricted: true, Filesystem: "read_only"})
	if err != nil {
		t.Fatal(err)
	}
	permission := config["permission"].(map[string]any)
	if permission["read"] != "allow" || permission["write"] != "deny" {
		t.Fatalf("read_only did not override explicit write allow: %#v", permission)
	}
}

func TestOpenCodePromptInjectsPathSkill(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("Always inspect tests first."), 0o644); err != nil {
		t.Fatal(err)
	}
	prompt, err := openCodePrompt("Fix the issue.", Policy{Skills: []string{dir}, SkillsRestricted: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Always inspect tests first.") || !strings.Contains(prompt, "<task>\nFix the issue.") {
		t.Fatalf("skill was not injected into OpenCode prompt: %s", prompt)
	}
}
