package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"takt/internal/execution"
	"takt/internal/redact"
	"takt/internal/spec"
	agentadaptersdk "takt/sdk/agentadapter"
)

var fakeAssistantBinary string

func TestMain(m *testing.M) {
	root, err := moduleRoot()
	if err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "takt-fake-assistant-")
	if err != nil {
		panic(err)
	}
	fakeAssistantBinary = filepath.Join(dir, "takt-fake-assistant")
	if runtime.GOOS == "windows" {
		fakeAssistantBinary += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", fakeAssistantBinary, "./internal/testsupport/cmd/takt-fake-assistant")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		panic("build fake assistant: " + err.Error() + ": " + string(output))
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..")), nil
}

func protocolProcess(caseName string, extra ...string) Process {
	argv := []string{fakeAssistantBinary, "--case", caseName}
	argv = append(argv, extra...)
	return Process{spec: spec.AssistantSpec{
		Type:           "process",
		Protocol:       ProtocolV1Alpha1,
		Argv:           argv,
		MaxOutputBytes: 32 * 1024,
	}}
}

func protocolProcessV2(caseName string, extra ...string) Process {
	argv := []string{fakeAssistantBinary, "--case", caseName}
	argv = append(argv, extra...)
	return Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: argv, MaxOutputBytes: 32 * 1024}}
}

func protocolRequest(workspace string) Request {
	return Request{
		RunID:       "run-contract",
		NodeID:      "implement",
		Attempt:     2,
		Prompt:      "implement the task",
		Workspace:   workspace,
		ModelName:   "large",
		Model:       spec.ModelSpec{Provider: "openai-compatible", ID: "qwen-test", Params: map[string]any{"reasoning": "high"}},
		SessionMode: "fresh",
		NativeHooks: json.RawMessage(`{"post_tool_use":[{"matcher":"Write"}]}`),
		Metadata:    map[string]string{"suite": "contract"},
	}
}

func TestFakeAssistantV1Alpha2UsesPublicConformanceKit(t *testing.T) {
	p := protocolProcessV2("success")
	result, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	report, err := agentadaptersdk.ValidateTranscript(strings.NewReader(result.Stdout), agentadaptersdk.Options{RequireDeclaration: true, RequireToolControl: true, RequiredCapabilities: []string{"skills"}})
	if err != nil {
		t.Fatalf("public conformance kit rejected repository wrapper: %v\n%s", err, result.Stdout)
	}
	if !report.Terminal || report.Records != 2 {
		t.Fatalf("unexpected conformance report: %+v", report)
	}
}

func TestProcessV1Alpha2MapsProviderUnavailableFixture(t *testing.T) {
	fixturePath := filepath.Join(moduleRootMust(t), "internal", "assistant", "testdata", "v1alpha2", "provider-unavailable.json")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "worker.py")
	code := `import sys
sys.stdin.readline()
sys.stdout.buffer.write(open(sys.argv[1], "rb").read())
sys.stderr.write("provider diagnostic\\n")
sys.exit(1)
`
	if err := os.WriteFile(script, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha2, Argv: []string{"python3", script, fixturePath}}}
	result, err := p.Run(context.Background(), Request{Workspace: dir})
	if execution.KindOf(err) != execution.KindProviderUnavailable {
		t.Fatalf("kind=%s err=%v", execution.KindOf(err), err)
	}
	var classified *execution.Error
	if !errors.As(err, &classified) || classified.RetryAfter != 2500*time.Millisecond {
		t.Fatalf("retry delay not preserved: %v", err)
	}
	if result.SessionID != "session-provider-1" || result.Resumed {
		t.Fatalf("session = %#v", result)
	}
	if result.Stdout != string(fixture) || result.Stderr != "provider diagnostic\\n" {
		t.Fatalf("raw output = stdout %q stderr %q", result.Stdout, result.Stderr)
	}
	if result.Usage == nil || result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 25 {
		t.Fatalf("usage = %#v", result.Usage)
	}
}

func moduleRootMust(t *testing.T) string {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFakeAssistantContract(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		p := protocolProcess("success")
		result, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.ExitCode != 0 || result.Output != "fake assistant completed" || result.SessionID != "fake-session-1" || result.Resumed {
			t.Fatalf("unexpected result: %+v", result)
		}
		if result.Usage == nil || result.Usage.InputTokens != 100 || result.Usage.OutputTokens != 25 {
			t.Fatalf("usage not decoded: %+v", result.Usage)
		}
		var structured map[string]any
		if err := json.Unmarshal(result.Structured, &structured); err != nil {
			t.Fatal(err)
		}
		if structured["run_id"] != "run-contract" || structured["node_id"] != "implement" || structured["attempt"] != float64(2) || structured["model_id"] != "qwen-test" {
			t.Fatalf("request fields were not transported: %#v", structured)
		}
		metadata, ok := structured["metadata"].(map[string]any)
		if !ok || metadata["suite"] != "contract" {
			t.Fatalf("metadata was not transported: %#v", structured["metadata"])
		}
		hooks, ok := structured["native_hooks"].(map[string]any)
		if !ok {
			t.Fatalf("native_hooks were not transported: %#v", structured["native_hooks"])
		}
		entries, ok := hooks["post_tool_use"].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("unexpected native_hooks: %#v", hooks)
		}
	})

	t.Run("exit", func(t *testing.T) {
		p := protocolProcess("exit", "--exit-code", "7")
		result, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindExit {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
		if result.ExitCode != 7 || result.Output != "fake assistant failed" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("start", func(t *testing.T) {
		p := Process{spec: spec.AssistantSpec{Type: "process", Protocol: ProtocolV1Alpha1, Argv: []string{"definitely-missing-takt-fake-assistant"}}}
		_, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindStart {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		p := protocolProcess("timeout", "--delay", "5s")
		_, err := p.Run(ctx, protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindTimedOut {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(50*time.Millisecond, cancel)
		p := protocolProcess("cancel", "--delay", "5s")
		_, err := p.Run(ctx, protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindCancelled {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("concurrent output", func(t *testing.T) {
		p := protocolProcess("concurrent-output")
		result, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if err != nil {
			t.Fatal(err)
		}
		if result.Truncated || len(result.Stderr) != 4096 || result.Output != "fake assistant completed" {
			t.Fatalf("unexpected concurrent output result: truncated=%v stderr=%d output=%q", result.Truncated, len(result.Stderr), result.Output)
		}
	})

	t.Run("malformed result", func(t *testing.T) {
		p := protocolProcess("malformed-result")
		_, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	for _, tc := range []struct {
		name     string
		caseName string
	}{
		{name: "bad protocol version", caseName: "bad-version"},
		{name: "bad envelope type", caseName: "bad-type"},
		{name: "unknown result field", caseName: "unknown-field"},
		{name: "unknown status", caseName: "unknown-status"},
		{name: "missing exit code", caseName: "missing-exit-code"},
		{name: "null exit code", caseName: "null-exit-code"},
		{name: "completed with nonzero exit", caseName: "completed-nonzero"},
		{name: "failed with zero exit", caseName: "failed-zero"},
		{name: "two JSON results", caseName: "two-results"},
		{name: "OS zero envelope nonzero mismatch", caseName: "os-envelope-mismatch-zero"},
		{name: "OS nonzero envelope mismatch", caseName: "os-envelope-mismatch-nonzero"},
		{name: "negative input tokens", caseName: "negative-input-tokens"},
		{name: "negative output tokens", caseName: "negative-output-tokens"},
		{name: "negative cost", caseName: "negative-cost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := protocolProcess(tc.caseName)
			_, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
			if execution.KindOf(err) != execution.KindProtocol {
				t.Fatalf("unexpected kind for %s: %s (%v)", tc.caseName, execution.KindOf(err), err)
			}
		})
	}

	t.Run("fresh", func(t *testing.T) {
		p := protocolProcess("fresh")
		req := protocolRequest(t.TempDir())
		req.SessionMode = "fresh"
		req.SessionID = "stale-session-must-not-be-sent"
		result, err := p.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "fresh-session" || result.Resumed {
			t.Fatalf("unexpected fresh result: %+v", result)
		}
	})

	t.Run("resume", func(t *testing.T) {
		p := protocolProcess("resume")
		req := protocolRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		result, err := p.Run(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if result.SessionID != "session-123" || !result.Resumed {
			t.Fatalf("unexpected resume result: %+v", result)
		}
	})

	t.Run("resume failure", func(t *testing.T) {
		p := protocolProcess("resume-failed")
		req := protocolRequest(t.TempDir())
		req.SessionMode = "resume"
		req.SessionID = "session-123"
		_, err := p.Run(context.Background(), req)
		if execution.KindOf(err) != execution.KindProtocol {
			t.Fatalf("unexpected kind: %s (%v)", execution.KindOf(err), err)
		}
	})

	t.Run("protocol output limit", func(t *testing.T) {
		p := protocolProcess("concurrent-output")
		p.spec.MaxOutputBytes = 64
		result, err := p.Run(context.Background(), protocolRequest(t.TempDir()))
		if execution.KindOf(err) != execution.KindProtocol || !result.Truncated {
			t.Fatalf("unexpected limit result: kind=%s result=%+v err=%v", execution.KindOf(err), result, err)
		}
	})
}

func TestFakeAssistantRejectsMultipleRequestValues(t *testing.T) {
	req := buildProtocolRequest(context.Background(), protocolRequest(t.TempDir()), spec.AssistantSpec{}, nil, time.Now())
	encoded, err := encodeProtocolRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(fakeAssistantBinary, "--case", "success")
	cmd.Stdin = strings.NewReader(string(encoded) + string(encoded))
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected fake assistant to reject multiple request values")
	}
	ee, ok := err.(*exec.ExitError)
	if !ok || ee.ExitCode() != 64 {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(output), "multiple JSON values") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestProtocolRequestEnvironmentAndLimits(t *testing.T) {
	p := protocolProcess("success")
	p.spec.Env = map[string]string{"FAKE_MODEL": "{{model.id}}", "FAKE_ATTEMPT": "{{attempt}}"}
	p.spec.MaxOutputBytes = 12345
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := p.Run(ctx, protocolRequest(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedModel == nil || result.ResolvedModel.ID != "qwen-test" {
		t.Fatalf("resolved model not decoded: %+v", result.ResolvedModel)
	}
	var structured map[string]any
	if err := json.Unmarshal(result.Structured, &structured); err != nil {
		t.Fatal(err)
	}
	env, ok := structured["environment"].(map[string]any)
	if !ok || env["FAKE_MODEL"] != "qwen-test" || env["FAKE_ATTEMPT"] != "2" {
		t.Fatalf("rendered environment was not transported: %#v", structured["environment"])
	}
	if structured["max_output_bytes"] != float64(12345) {
		t.Fatalf("max output limit was not transported: %#v", structured)
	}
	timeoutMS, ok := structured["timeout_ms"].(float64)
	if !ok || timeoutMS <= 0 || timeoutMS > 2000 {
		t.Fatalf("timeout limit was not transported: %#v", structured)
	}
}

func TestRenderArgIncludesContractIdentifiers(t *testing.T) {
	req := protocolRequest(t.TempDir())
	got := renderArg("{{run.id}}|{{node.id}}|{{attempt}}", req)
	want := "run-contract|implement|" + strconv.Itoa(req.Attempt)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestProtocolRequestCarriesNodePolicy(t *testing.T) {
	req := protocolRequest(t.TempDir())
	req.Policy = Policy{AllowedTools: []string{"read"}, ToolsRestricted: true, Requires: []string{"custom"}}
	protocol := buildProtocolRequest(context.Background(), req, spec.AssistantSpec{}, nil, time.Now())
	if protocol.Policy == nil || !protocol.Policy.ToolsRestricted || len(protocol.Policy.AllowedTools) != 1 || protocol.Policy.Requires[0] != "custom" {
		t.Fatalf("policy was not included in protocol request: %+v", protocol.Policy)
	}
}

func TestRegisterRenderedEnvSecretsAfterRequestTemplating(t *testing.T) {
	const name = "TAKT_RENDERED_REF_VALUE"
	const secret = "short"
	t.Setenv(name, secret)
	r := redact.NewFromEnvironment()
	if got := r.String(secret); got != secret {
		t.Fatalf("test precondition failed: secret auto-registered: %q", got)
	}
	RegisterRenderedEnvSecrets(r, spec.AssistantSpec{Env: map[string]string{"TOKEN": "{{prompt}}"}}, Request{Prompt: "secret://" + name})
	if got := r.String(secret); got != "<redacted>" {
		t.Fatalf("rendered SecretRef was not registered: %q", got)
	}
}
