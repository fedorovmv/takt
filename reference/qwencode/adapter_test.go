package qwencode

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	sdk "takt/sdk/agentadapter"
)

func TestAdapterFreshAndResumeConformToPublicSDK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tmp := t.TempDir()
	argsLog := filepath.Join(tmp, "args.log")
	fake := filepath.Join(tmp, "qwen")
	writeExecutable(t, fake, `#!/bin/sh
set -eu
printf '%s\n' "$@" > "$QWEN_ARGS_LOG"
session=s-fresh
prev=""
for arg in "$@"; do
  if [ "$prev" = "--resume" ]; then session="$arg"; fi
  prev="$arg"
done
printf '{"type":"system","subtype":"session_start","session_id":"%s","model":"qwen-test"}\n' "$session"
printf '{"type":"assistant","session_id":"%s","message":{"content":[{"type":"text","text":"done"}]}}\n' "$session"
printf '{"type":"result","subtype":"success","session_id":"%s","result":"done","usage":{"input_tokens":7,"output_tokens":3,"cost":0.25}}\n' "$session"
`)

	for _, tc := range []struct{ name, mode, id string }{
		{name: "fresh", mode: "fresh"},
		{name: "resume", mode: "resume", id: "resume-17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := sdk.Request{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "request", RunID: "run-1", NodeID: "work", Attempt: 1, Prompt: "do it", Workspace: tmp, Model: sdk.Model{Provider: "qwen", ID: "qwen3-coder"}, Session: sdk.SessionRequest{Mode: tc.mode, ID: tc.id}, Environment: map[string]string{"QWEN_ARGS_LOG": argsLog}, Limits: sdk.Limits{TimeoutMS: 1234}}
			var input, output, diag bytes.Buffer
			if err := json.NewEncoder(&input).Encode(req); err != nil {
				t.Fatal(err)
			}
			code := (Adapter{Binary: fake}).Serve(context.Background(), &input, &output, &diag)
			if code != 0 {
				t.Fatalf("code=%d diag=%s output=%s", code, diag.String(), output.String())
			}
			report, err := sdk.ValidateTranscript(bytes.NewReader(output.Bytes()), sdk.Options{RequireDeclaration: true, RequestedSessionID: tc.id, RequiredCapabilities: []string{"agent_events_v2", "session_events", "usage_events"}})
			if err != nil {
				t.Fatalf("conformance: %v\n%s", err, output.String())
			}
			if !report.Terminal || report.Events < 4 {
				t.Fatalf("report=%+v", report)
			}
			args, _ := os.ReadFile(argsLog)
			text := string(args)
			for _, want := range []string{"--output-format\nstream-json", "--safe-mode", "--approval-mode\nyolo", "--model\nqwen3-coder", "--max-wall-time\n2s"} {
				if !strings.Contains(text, want) {
					t.Fatalf("args missing %q:\n%s", want, text)
				}
			}
			if tc.id != "" && !strings.Contains(text, "--resume\n"+tc.id) {
				t.Fatalf("resume arg missing: %s", text)
			}
		})
	}
}

func TestAdapterRejectsUnsupportedPolicyBeforeLaunchingQwen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "launched")
	fake := filepath.Join(tmp, "qwen")
	writeExecutable(t, fake, "#!/bin/sh\ntouch \"$QWEN_LAUNCHED\"\nexit 0\n")
	req := sdk.Request{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "request", RunID: "run-1", NodeID: "work", Attempt: 1, Prompt: "do it", Workspace: tmp, Model: sdk.Model{Provider: "qwen", ID: "qwen"}, Session: sdk.SessionRequest{Mode: "fresh"}, Environment: map[string]string{"QWEN_LAUNCHED": marker}, Policy: &sdk.Policy{ToolsRestricted: true, AllowedTools: []string{"read"}}}
	var input, output, diag bytes.Buffer
	_ = json.NewEncoder(&input).Encode(req)
	code := (Adapter{Binary: fake}).Serve(context.Background(), &input, &output, &diag)
	if code == 0 {
		t.Fatalf("unsupported policy accepted: %s", output.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("qwen launched despite fail-closed policy")
	}
	if _, err := sdk.ValidateTranscript(bytes.NewReader(output.Bytes()), sdk.Options{RequireDeclaration: true}); err != nil {
		t.Fatalf("failure transcript invalid: %v\n%s", err, output.String())
	}
}

func TestReferencePackageDoesNotImportRuntimeInternals(t *testing.T) {
	raw, err := os.ReadFile("adapter.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"takt/internal/`)) {
		t.Fatal("reference adapter imports internal package")
	}
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestQwenWallTimeRoundsUpToDocumentedSeconds(t *testing.T) {
	cases := map[int64]string{1: "1s", 999: "1s", 1000: "1s", 1001: "2s", 61000: "61s"}
	for ms, want := range cases {
		if got := qwenWallTime(ms); got != want {
			t.Fatalf("%dms => %q, want %q", ms, got, want)
		}
	}
}

func TestAdapterMapsQwenBudgetExitToTimedOutFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "qwen")
	writeExecutable(t, fake, `#!/bin/sh
printf '{"type":"system","subtype":"session_start","session_id":"s-timeout","model":"qwen-test"}\n'
printf '{"type":"result","subtype":"error","is_error":true,"session_id":"s-timeout","result":"budget exceeded"}\n'
exit 55
`)
	req := sdk.Request{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "request", RunID: "run-1", NodeID: "work", Attempt: 1, Prompt: "do it", Workspace: tmp, Model: sdk.Model{Provider: "qwen", ID: "qwen"}, Session: sdk.SessionRequest{Mode: "fresh"}}
	var input, output, diag bytes.Buffer
	_ = json.NewEncoder(&input).Encode(req)
	code := (Adapter{Binary: fake}).Serve(context.Background(), &input, &output, &diag)
	if code != 55 {
		t.Fatalf("code=%d diag=%s output=%s", code, diag.String(), output.String())
	}
	dec := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var terminal sdk.Result
	for dec.More() {
		var rec sdk.TranscriptRecord
		if err := dec.Decode(&rec); err != nil {
			t.Fatal(err)
		}
		if rec.Result != nil {
			terminal = *rec.Result
		}
	}
	if terminal.FailureKind != "timed_out" || terminal.ExitCode == nil || *terminal.ExitCode != 55 {
		t.Fatalf("terminal=%+v", terminal)
	}
}

func TestResumeMismatchDoesNotEmitSessionResumed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	tmp := t.TempDir()
	fake := filepath.Join(tmp, "qwen")
	writeExecutable(t, fake, `#!/bin/sh
printf '{"type":"system","subtype":"session_start","session_id":"wrong","model":"qwen-test"}\n'
sleep 1
`)
	req := sdk.Request{ProtocolVersion: sdk.ProtocolV1Alpha2, Type: "request", RunID: "run-1", NodeID: "work", Attempt: 1, Prompt: "do it", Workspace: tmp, Model: sdk.Model{Provider: "qwen", ID: "qwen"}, Session: sdk.SessionRequest{Mode: "resume", ID: "wanted"}}
	var input, output, diag bytes.Buffer
	_ = json.NewEncoder(&input).Encode(req)
	if code := (Adapter{Binary: fake}).Serve(context.Background(), &input, &output, &diag); code == 0 {
		t.Fatalf("mismatch accepted: %s", output.String())
	}
	if strings.Contains(output.String(), `"type":"session.resumed"`) {
		t.Fatalf("resume event emitted before identity validation: %s", output.String())
	}
}
