package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type options struct {
	caseName string
	dir      string
	model    string
	agent    string
	variant  string
	session  string
	auto     bool
}

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("opencode 1.2.3-test")
		return
	}
	if len(os.Args) < 2 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "expected opencode run")
		os.Exit(2)
	}
	opts, err := parse(os.Args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	prompt, _ := io.ReadAll(os.Stdin)
	if opts.dir == "" {
		opts.dir, _ = os.Getwd()
	}
	_ = os.MkdirAll(opts.dir, 0o755)
	observed := map[string]any{
		"model": opts.model, "agent": opts.agent, "variant": opts.variant, "session": opts.session, "auto": opts.auto,
		"prompt": string(prompt), "run_id": os.Getenv("TAKT_RUN_ID"), "node_id": os.Getenv("TAKT_NODE_ID"),
		"attempt": os.Getenv("TAKT_ATTEMPT"), "metadata": os.Getenv("TAKT_METADATA_JSON"), "native_hooks": os.Getenv("TAKT_NATIVE_HOOKS_JSON"),
	}
	if data, err := json.Marshal(observed); err == nil {
		_ = os.WriteFile(filepath.Join(opts.dir, ".fake-opencode-observed.json"), data, 0o644)
	}

	session := opts.session
	if session == "" {
		session = "ses-opencode-1"
	}
	if opts.caseName == "resume-mismatch" {
		session = "ses-other"
	}

	switch opts.caseName {
	case "timeout", "cancel":
		time.Sleep(5 * time.Second)
		return
	case "provider-timeout":
		fmt.Fprintln(os.Stderr, "provider endpoint unavailable; retrying request 2/3")
		emit(session, "error", map[string]any{"error": map[string]any{
			"name": "APIConnectionError",
			"data": map[string]any{"message": "dial tcp provider.example:443: connection refused"},
		}})
		time.Sleep(8 * time.Second)
		return
	case "exit":
		fmt.Fprintln(os.Stderr, "fake opencode exited")
		os.Exit(7)
	case "malformed":
		fmt.Println("not-json")
		return
	case "error-zero-exit":
		emit(session, "error", map[string]any{"error": map[string]any{"name": "APIError", "data": map[string]any{"message": "provider failed"}}})
		return
	case "error-zero-exit-stderr-transient":
		fmt.Fprintln(os.Stderr, "provider connection reset")
		emit(session, "error", map[string]any{"error": map[string]any{"name": "APIError", "data": map[string]any{"message": "request failed"}}})
		return
	case "error-name-transient":
		emit(session, "error", map[string]any{"error": map[string]any{"name": "APIConnectionError: connection reset", "data": map[string]any{"message": "request failed"}}})
		return
	case "provider-503", "provider-429", "provider-connection-reset", "provider-401":
		errorData := map[string]any{"message": "provider failed"}
		switch opts.caseName {
		case "provider-503":
			errorData = map[string]any{"message": "provider service unavailable", "statusCode": 503, "retryAfterMs": 1200}
		case "provider-429":
			errorData = map[string]any{"message": "provider rate limit", "statusCode": 429, "retryAfterMs": 250}
		case "provider-connection-reset":
			errorData = map[string]any{"message": "provider connection reset", "retryAfterMs": -1}
		case "provider-401":
			errorData = map[string]any{"message": "provider unauthorized", "statusCode": 401}
		}
		emit(session, "error", map[string]any{"error": map[string]any{"name": "APIError", "data": errorData}})
		emitStepFinish(session, 101, 17, 0.0042)
		return
	case "missing-usage":
		emitText(session, "fake OpenCode completed")
		return
	case "negative-usage":
		emitText(session, "fake OpenCode completed")
		emitStepFinish(session, -1, 2, 0.1)
		return
	case "overflow":
		fmt.Fprint(os.Stderr, strings.Repeat("x", 8192))
		time.Sleep(100 * time.Millisecond)
		return
	case "warning":
		fmt.Fprintln(os.Stderr, "fake warning on stderr")
	}

	emit(session, "step_start", map[string]any{"part": map[string]any{
		"id": "part-start", "messageID": "msg-1", "sessionID": session, "type": "step-start",
	}})
	emit(session, "tool_use", map[string]any{"part": map[string]any{
		"id": "part-tool", "messageID": "msg-1", "sessionID": session, "type": "tool", "tool": "write",
		"state": map[string]any{"status": "completed", "input": map[string]any{"path": "route.yaml"}, "output": "ok"},
	}})
	emitText(session, "fake OpenCode completed")
	emitStepFinish(session, 101, 17, 0.0042)
}

func parse(args []string) (options, error) {
	opts := options{caseName: "success"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			i++
			if i >= len(args) || args[i] != "json" {
				return opts, fmt.Errorf("expected --format json")
			}
		case "--dir":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --dir value")
			}
			opts.dir = args[i]
		case "--model":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --model value")
			}
			opts.model = args[i]
		case "--agent":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --agent value")
			}
			opts.agent = args[i]
		case "--variant":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --variant value")
			}
			opts.variant = args[i]
		case "--session":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --session value")
			}
			opts.session = args[i]
		case "--auto":
			opts.auto = true
		case "--fake-case":
			i++
			if i >= len(args) {
				return opts, fmt.Errorf("missing --fake-case value")
			}
			opts.caseName = args[i]
		default:
			if strings.HasPrefix(args[i], "--fake-delay=") {
				_, _ = time.ParseDuration(strings.TrimPrefix(args[i], "--fake-delay="))
				continue
			}
			if strings.HasPrefix(args[i], "--fake-exit=") {
				_, _ = strconv.Atoi(strings.TrimPrefix(args[i], "--fake-exit="))
				continue
			}
			return opts, fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return opts, nil
}

func emitText(session, text string) {
	emit(session, "text", map[string]any{"part": map[string]any{
		"id": "part-text", "messageID": "msg-1", "sessionID": session, "type": "text", "text": text,
		"time": map[string]any{"start": 1, "end": 2},
	}})
}

func emitStepFinish(session string, input, output int, cost float64) {
	emit(session, "step_finish", map[string]any{"part": map[string]any{
		"id": "part-finish", "messageID": "msg-1", "sessionID": session, "type": "step-finish", "reason": "stop",
		"tokens": map[string]any{"input": input, "output": output, "reasoning": 3, "cache": map[string]any{"read": 5, "write": 1}},
		"cost":   cost,
	}})
}

func emit(session, kind string, fields map[string]any) {
	record := map[string]any{"type": kind, "timestamp": time.Now().UnixMilli(), "sessionID": session}
	for key, value := range fields {
		record[key] = value
	}
	data, _ := json.Marshal(record)
	fmt.Println(string(data))
}
