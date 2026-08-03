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
	"testing"
	"time"

	"takt/internal/execution"
	"takt/internal/spec"
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
	cmd := exec.Command("go", "build", "-o", fakeAssistantBinary, "./cmd/takt-fake-assistant")
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
		Metadata:    map[string]string{"suite": "contract"},
	}
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
