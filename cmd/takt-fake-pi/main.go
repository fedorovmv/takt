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
	if opts.caseName == "version-empty" {
		return
	}

	sessionID := "fake-pi-session-1"
	if opts.session != "" {
		sessionID = opts.session
	}
	if opts.caseName == "resume-mismatch" && opts.session != "" {
		sessionID = "different-session"
	}

	writer := &safeWriter{}
	scanner := bufio.NewScanner(os.Stdin)
	// RPC records can contain large prompts. Match the real protocol's JSONL
	// framing without imposing Scanner's small default token size.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lastPrompt := ""
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
		case "prompt":
			lastPrompt, _ = command["message"].(string)
			if opts.caseName == "prompt-rejected" {
				writeResponse(writer, id, typeName, false, nil, "prompt rejected by fake Pi")
				continue
			}
			writeResponse(writer, id, typeName, true, nil, "")
			switch opts.caseName {
			case "timeout", "cancel":
				for {
					time.Sleep(time.Hour)
				}
			case "malformed":
				writer.line([]byte("{not-json}\n"))
			case "huge-line":
				writer.line([]byte(strings.Repeat("x", 1024*1024)))
			case "two-json-on-line":
				writer.line([]byte("{\"type\":\"agent_end\"}{\"type\":\"agent_end\"}\n"))
			case "extension-ui":
				writeJSON(writer, map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "confirm", "title": "Confirm"})
			case "agent-failure":
				writeJSON(writer, map[string]any{"type": "agent_start"})
				writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{map[string]any{
					"role": "assistant", "stopReason": "error", "errorMessage": "fake model failure",
				}}})
			default:
				if opts.caseName == "concurrent-output" {
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
				}
				writeJSON(writer, map[string]any{"type": "agent_start"})
				writeJSON(writer, map[string]any{"type": "message_end", "message": map[string]any{
					"role":       "assistant",
					"content":    []any{map[string]any{"type": "text", "text": "fake Pi completed"}},
					"stopReason": "stop",
				}})
				writeJSON(writer, map[string]any{"type": "agent_end", "messages": []any{map[string]any{
					"role": "assistant", "stopReason": "stop",
				}}})
			}
		case "get_last_assistant_text":
			writeResponse(writer, id, typeName, true, map[string]any{"text": "fake Pi completed"}, "")
		case "get_session_stats":
			writeResponse(writer, id, typeName, true, map[string]any{
				"tokens": map[string]any{"inputTokens": 111, "outputTokens": 22},
				"cost":   0.0125,
				"observed": map[string]any{
					"provider":      opts.provider,
					"model":         opts.model,
					"thinking":      opts.thinking,
					"session":       opts.session,
					"session_dir":   opts.sessionDir,
					"project_trust": opts.projectTrust,
					"prompt":        lastPrompt,
					"run_id":        os.Getenv("TAKT_RUN_ID"),
					"node_id":       os.Getenv("TAKT_NODE_ID"),
					"metadata":      os.Getenv("TAKT_METADATA_JSON"),
					"native_hooks":  os.Getenv("TAKT_NATIVE_HOOKS_JSON"),
				},
			}, "")
		default:
			writeResponse(writer, id, typeName, false, nil, "unsupported fake command")
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
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
			if strings.HasPrefix(arg, "--") {
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
