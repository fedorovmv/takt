package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type options struct {
	caseName     string
	mode         string
	provider     string
	model        string
	thinking     string
	session      string
	sessionDir   string
	projectTrust string
}

type fakeState struct {
	mu            sync.Mutex
	prompt        string
	promptStarted bool
	settled       bool
	finalText     string
	messages      []any
	baseInput     int
	baseOutput    int
	baseCost      float64
	attemptInput  int
	attemptOutput int
	attemptCost   float64
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("takt-fake-pi 0.83.0")
		return
	}
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if opts.mode != "rpc" {
		fmt.Fprintln(os.Stderr, "fake Pi requires --mode rpc")
		os.Exit(2)
	}
	if opts.caseName == "exit" {
		os.Exit(7)
	}

	sessionID := "fake-pi-session-1"
	if opts.session != "" {
		sessionID = opts.session
	}
	if opts.caseName == "resume-mismatch" && opts.session != "" {
		sessionID = "different-session"
	}
	if opts.session != "" {
		_ = os.Setenv("TAKT_FAKE_PI_RESUME", "1")
	}

	state := &fakeState{attemptInput: 111, attemptOutput: 22, attemptCost: 0.0125}
	if opts.session != "" {
		state.baseInput = 1000
		state.baseOutput = 200
		state.baseCost = 0.5
	}
	writer := &safeWriter{}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var command map[string]any
		if err := json.Unmarshal(line, &command); err != nil {
			writeResponse(writer, "", "unknown", false, nil, "invalid JSON")
			continue
		}
		id, _ := command["id"].(string)
		typeName, _ := command["type"].(string)
		switch typeName {
		case "get_state":
			if opts.caseName == "malformed-state" {
				writeResponse(writer, id, typeName, true, map[string]any{"sessionId": 42}, "")
				continue
			}
			writeResponse(writer, id, typeName, true, map[string]any{
				"model":         map[string]any{"provider": opts.provider, "id": opts.model},
				"thinkingLevel": opts.thinking,
				"isStreaming":   false,
				"sessionFile":   "/tmp/" + sessionID + ".jsonl",
				"sessionId":     sessionID,
				"messageCount":  2,
			}, "")
		case "get_session_stats":
			observed := map[string]any{
				"provider":      opts.provider,
				"model":         opts.model,
				"thinking":      opts.thinking,
				"session":       opts.session,
				"session_dir":   opts.sessionDir,
				"project_trust": opts.projectTrust,
				"prompt":        state.promptValue(),
				"run_id":        os.Getenv("TAKT_RUN_ID"),
				"node_id":       os.Getenv("TAKT_NODE_ID"),
				"metadata":      os.Getenv("TAKT_METADATA_JSON"),
				"native_hooks":  os.Getenv("TAKT_NATIVE_HOOKS_JSON"),
			}
			if opts.caseName == "stats-disappear" && state.settledValue() {
				writeResponse(writer, id, typeName, true, map[string]any{"observed": observed}, "")
				continue
			}
			input, output, cost := state.stats(opts.caseName)
			writeResponse(writer, id, typeName, true, map[string]any{
				"tokens":   map[string]any{"input": input, "output": output, "total": input + output},
				"cost":     cost,
				"observed": observed,
			}, "")
		case "prompt":
			prompt, _ := command["message"].(string)
			state.startPrompt(prompt)
			if opts.caseName == "prompt-rejected" {
				writeResponse(writer, id, typeName, false, nil, "prompt rejected by fake Pi")
				continue
			}
			writeResponse(writer, id, typeName, true, nil, "")
			handlePrompt(opts, writer, state)
		case "get_messages":
			writeResponse(writer, id, typeName, true, map[string]any{"messages": state.messagesValue()}, "")
		case "get_last_assistant_text":
			writeResponse(writer, id, typeName, true, map[string]any{"text": state.textValue()}, "")
		default:
			writeResponse(writer, id, typeName, false, nil, "unsupported fake command")
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func handlePrompt(opts options, writer *safeWriter, state *fakeState) {
	caseName := opts.caseName
	switch caseName {
	case "timeout", "cancel":
		for {
			time.Sleep(time.Hour)
		}
	case "timeout-overflow", "cancel-overflow":
		// Let the prompt response drain before overflowing stderr so the test
		// synchronizes parent context completion with the actual limit event.
		time.Sleep(100 * time.Millisecond)
		_, _ = os.Stderr.WriteString(strings.Repeat("overflow", 64*1024))
		for {
			time.Sleep(time.Hour)
		}
	case "route-dsl":
		valid := opts.session != "" && strings.Contains(state.promptValue(), "ROUTE_INVALID")
		content := "apiVersion: synapse/v1\nkind: Route\nvalid: false\n"
		text := "created an invalid route for validator feedback"
		if valid {
			content = "apiVersion: synapse/v1\nkind: Route\nvalid: true\nsteps:\n  - from: http\n  - transform: jq\n  - to: target\n"
			text = "corrected route.yaml using validator feedback"
		}
		_ = os.WriteFile("route.yaml", []byte(content), 0o644)
		message := assistantMessage(opts, text, "stop", "")
		state.finish(text, []any{message})
		writeJSON(writer, map[string]any{"type": "agent_start"})
		writeJSON(writer, map[string]any{"type": "message_end", "message": message})
		writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{message}, "willRetry": false})
		writeJSON(writer, map[string]any{"type": "agent_settled"})
	case "malformed":
		writer.line([]byte("{not-json}\n"))
	case "huge-line":
		writer.line([]byte(strings.Repeat("x", 1024*1024)))
	case "two-json-on-line":
		writer.line([]byte("{\"type\":\"agent_end\"}{\"type\":\"agent_settled\"}\n"))
	case "extension-ui":
		writeJSON(writer, map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "confirm", "title": "Confirm"})
	case "extension-ui-set-editor-text":
		writeJSON(writer, map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "set_editor_text", "text": "updated"})
		emitSuccess(writer, state, opts)
	case "transient-rpc-noise":
		partial := strings.Repeat("x", 4096)
		for i := 0; i < 32; i++ {
			writeJSON(writer, map[string]any{"type": "extension_ui_request", "id": fmt.Sprintf("title-%d", i), "method": "setTitle", "title": "working"})
			writeJSON(writer, map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant", "content": partial}})
		}
		emitSuccess(writer, state, opts)
	case "huge-transient-record":
		writeJSON(writer, map[string]any{"type": "message_update", "message": map[string]any{"role": "assistant", "content": strings.Repeat("x", 4096)}})
	case "live-events":
		writeJSON(writer, map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "read", "args": map[string]any{"path": "main.go"}})
		writeJSON(writer, map[string]any{"type": "tool_execution_end", "toolCallId": "call-1", "toolName": "read", "isError": false})
		writeJSON(writer, map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "inspected main.go"}}}})
		emitSuccess(writer, state, opts)
	case "tool-then-hang":
		writeJSON(writer, map[string]any{"type": "tool_execution_start", "toolCallId": "call-1", "toolName": "bash", "args": map[string]any{"command": "go test ./..."}})
		writeJSON(writer, map[string]any{"type": "tool_execution_end", "toolCallId": "call-1", "toolName": "bash", "isError": false})
		for {
			time.Sleep(time.Hour)
		}
	case "agent-failure":
		message := assistantMessage(opts, "", "error", "fake model failure")
		state.finish("", []any{message})
		writeJSON(writer, map[string]any{"type": "agent_start"})
		writeJSON(writer, map[string]any{"type": "message_end", "message": message})
		writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{message}, "willRetry": false})
		writeJSON(writer, map[string]any{"type": "agent_settled"})
	case "retry-before-settled":
		first := assistantMessage(opts, "", "error", "transient fake failure")
		state.setPartial("partial Pi result", []any{first})
		writeJSON(writer, map[string]any{"type": "agent_start"})
		writeJSON(writer, map[string]any{"type": "message_end", "message": first})
		writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{first}, "willRetry": true})
		writeJSON(writer, map[string]any{"type": "auto_retry_start", "attempt": 1, "delayMs": 100})
		go func() {
			time.Sleep(120 * time.Millisecond)
			writeJSON(writer, map[string]any{"type": "auto_retry_end", "success": true, "attempt": 1})
			emitSuccess(writer, state, opts)
		}()
	case "concurrent-output":
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = os.Stderr.WriteString(strings.Repeat("e", 4096))
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				writeJSON(writer, map[string]any{"type": "message_update", "assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "x"}})
			}
		}()
		wg.Wait()
		emitSuccess(writer, state, opts)
	default:
		emitSuccess(writer, state, opts)
	}
}

func assistantMessage(opts options, text, stopReason, errorMessage string) map[string]any {
	responseModel := opts.model
	if opts.caseName == "resolved-model" {
		responseModel = "resolved-" + opts.model
	}
	message := map[string]any{
		"role":          "assistant",
		"provider":      opts.provider,
		"model":         opts.model,
		"responseModel": responseModel,
		"content":       []any{map[string]any{"type": "text", "text": text}},
		"stopReason":    stopReason,
	}
	if errorMessage != "" {
		message["errorMessage"] = errorMessage
	}
	return message
}

func emitSuccess(writer *safeWriter, state *fakeState, opts ...options) {
	resolved := options{provider: "openai", model: "fake-model"}
	if len(opts) > 0 {
		resolved = opts[0]
	}
	message := assistantMessage(resolved, "fake Pi completed", "stop", "")
	state.finish("fake Pi completed", []any{message})
	writeJSON(writer, map[string]any{"type": "agent_start"})
	writeJSON(writer, map[string]any{"type": "message_end", "message": message})
	writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{message}, "willRetry": false})
	writeJSON(writer, map[string]any{"type": "agent_settled"})
}

func (s *fakeState) startPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prompt = prompt
	s.promptStarted = true
}

func (s *fakeState) setPartial(text string, messages []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalText = text
	s.messages = messages
}

func (s *fakeState) finish(text string, messages []any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finalText = text
	s.messages = messages
	s.settled = true
}

func (s *fakeState) stats(caseName string) (int, int, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if caseName == "stats-zero" {
		return 0, 0, 0
	}
	input, output, cost := s.baseInput, s.baseOutput, s.baseCost
	if s.settled {
		if caseName == "stats-decrease" {
			return input - 1, output, cost
		}
		input += s.attemptInput
		output += s.attemptOutput
		cost += s.attemptCost
	}
	return input, output, cost
}

func (s *fakeState) settledValue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settled
}

func (s *fakeState) promptValue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.prompt
}

func (s *fakeState) textValue() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.finalText
}

func (s *fakeState) messagesValue() []any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]any(nil), s.messages...)
}

func parseArgs(args []string) (options, error) {
	opts := options{caseName: "success", projectTrust: "default"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		next := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing value for %s", arg)
			}
			i++
			return args[i], nil
		}
		var value string
		var err error
		switch arg {
		case "--mode":
			value, err = next()
			opts.mode = value
		case "--provider":
			value, err = next()
			opts.provider = value
		case "--model":
			value, err = next()
			opts.model = value
		case "--thinking":
			value, err = next()
			opts.thinking = value
		case "--session":
			value, err = next()
			opts.session = value
		case "--session-dir":
			value, err = next()
			opts.sessionDir = value
		case "--approve", "-a":
			opts.projectTrust = "approve"
		case "--no-approve", "-na":
			opts.projectTrust = "deny"
		case "--fake-case":
			value, err = next()
			opts.caseName = value
		case "--fake-delay":
			value, err = next()
			if err == nil {
				d, parseErr := time.ParseDuration(value)
				if parseErr != nil {
					return opts, parseErr
				}
				time.Sleep(d)
			}
		default:
			if strings.HasPrefix(arg, "--") || strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown fake Pi argument %s", arg)
			}
		}
		if err != nil {
			return opts, err
		}
	}
	return opts, nil
}

type safeWriter struct{ mu sync.Mutex }

func (w *safeWriter) line(line []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = os.Stdout.Write(line)
}
func writeJSON(w *safeWriter, value any) {
	encoded, _ := json.Marshal(value)
	w.line(append(encoded, '\n'))
}
func writeResponse(w *safeWriter, id, command string, success bool, data any, message string) {
	value := map[string]any{"id": id, "type": "response", "command": command, "success": success}
	if data != nil {
		value["data"] = data
	}
	if message != "" {
		value["error"] = message
	}
	writeJSON(w, value)
}
