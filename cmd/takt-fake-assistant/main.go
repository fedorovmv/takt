package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"takt/internal/assistant"
)

func main() {
	caseName := flag.String("case", "success", "contract case")
	exitCode := flag.Int("exit-code", 7, "exit code for exit case")
	delay := flag.Duration("delay", 5*time.Second, "delay for timeout/cancel cases")
	flag.Parse()

	request, err := readRequest(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(64)
	}

	switch *caseName {
	case "success":
		writeResult(successResult(request, "fake-session-1", false, 0))
	case "exit":
		result := successResult(request, sessionID(request), request.Session.Mode == "resume", *exitCode)
		result.Status = "failed"
		result.Output = "fake assistant failed"
		writeResult(result)
		os.Exit(*exitCode)
	case "timeout", "cancel":
		time.Sleep(*delay)
		writeResult(successResult(request, sessionID(request), request.Session.Mode == "resume", 0))
	case "concurrent-output":
		result := successResult(request, sessionID(request), request.Session.Mode == "resume", 0)
		payload, err := json.Marshal(result)
		if err != nil {
			panic(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = os.Stdout.Write(payload)
			_, _ = os.Stdout.Write([]byte("\n" + strings.Repeat(" ", 4096)))
		}()
		go func() {
			defer wg.Done()
			_, _ = os.Stderr.Write([]byte(strings.Repeat("e", 4096)))
		}()
		wg.Wait()
	case "malformed-result":
		_, _ = io.WriteString(os.Stdout, `{"protocol_version":`)
	case "bad-version":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["protocol_version"] = "takt-assistant/v999"
		})
	case "bad-type":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["type"] = "request"
		})
	case "unknown-field":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["unexpected"] = true
		})
	case "unknown-status":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["status"] = "partial"
		})
	case "missing-exit-code":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			delete(v, "exit_code")
		})
	case "null-exit-code":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["exit_code"] = nil
		})
	case "completed-nonzero":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["exit_code"] = 7
		})
	case "failed-zero":
		writeMutatedResult(successResult(request, sessionID(request), false, 0), func(v map[string]any) {
			v["status"] = "failed"
		})
	case "two-results":
		writeResult(successResult(request, sessionID(request), false, 0))
		writeResult(successResult(request, sessionID(request), false, 0))
	case "os-envelope-mismatch-zero":
		result := successResult(request, sessionID(request), false, 7)
		result.Status = "failed"
		writeResult(result)
	case "os-envelope-mismatch-nonzero":
		result := successResult(request, sessionID(request), false, 7)
		result.Status = "failed"
		writeResult(result)
		os.Exit(8)
	case "negative-input-tokens":
		result := successResult(request, sessionID(request), false, 0)
		result.Usage.InputTokens = -1
		writeResult(result)
	case "negative-output-tokens":
		result := successResult(request, sessionID(request), false, 0)
		result.Usage.OutputTokens = -1
		writeResult(result)
	case "negative-cost":
		result := successResult(request, sessionID(request), false, 0)
		result.Usage.Cost = -0.01
		writeResult(result)
	case "fresh":
		if request.Session.Mode != "fresh" || request.Session.ID != "" {
			writeProtocolFailure(65, "expected fresh session without id")
			return
		}
		writeResult(successResult(request, "fresh-session", false, 0))
	case "resume":
		if request.Session.Mode != "resume" || request.Session.ID == "" {
			writeProtocolFailure(66, "expected resume session with id")
			return
		}
		writeResult(successResult(request, request.Session.ID, true, 0))
	case "session-cycle":
		if request.Session.Mode == "fresh" && request.Session.ID == "" {
			writeResult(successResult(request, "cycle-session", false, 0))
			return
		}
		if request.Session.Mode == "resume" && request.Session.ID == "cycle-session" {
			writeResult(successResult(request, request.Session.ID, true, 0))
			return
		}
		writeProtocolFailure(67, "unexpected session cycle request")
	case "resume-failed":
		result := successResult(request, "replacement-session", false, 0)
		writeResult(result)
	default:
		fmt.Fprintf(os.Stderr, "unknown case %q\n", *caseName)
		os.Exit(64)
	}
}

func readRequest(r io.Reader) (assistant.ProtocolRequest, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var request assistant.ProtocolRequest
	if err := dec.Decode(&request); err != nil {
		return assistant.ProtocolRequest{}, fmt.Errorf("decode request: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return assistant.ProtocolRequest{}, fmt.Errorf("decode request: multiple JSON values")
		}
		return assistant.ProtocolRequest{}, fmt.Errorf("decode request trailing data: %w", err)
	}
	if request.ProtocolVersion != assistant.ProtocolV1Alpha1 || request.Type != "request" {
		return assistant.ProtocolRequest{}, fmt.Errorf("unsupported request envelope")
	}
	return request, nil
}

func successResult(request assistant.ProtocolRequest, id string, resumed bool, code int) assistant.ProtocolResult {
	status := "completed"
	if code != 0 {
		status = "failed"
	}
	var nativeHooks any
	if len(request.NativeHooks) > 0 {
		_ = json.Unmarshal(request.NativeHooks, &nativeHooks)
	}
	structured, _ := json.Marshal(map[string]any{
		"run_id":           request.RunID,
		"node_id":          request.NodeID,
		"attempt":          request.Attempt,
		"model_id":         request.Model.ID,
		"prompt":           request.Prompt,
		"workspace":        request.Workspace,
		"session_mode":     request.Session.Mode,
		"session_id":       request.Session.ID,
		"environment":      request.Environment,
		"metadata":         request.Metadata,
		"native_hooks":     nativeHooks,
		"timeout_ms":       request.Limits.TimeoutMS,
		"max_output_bytes": request.Limits.MaxOutputBytes,
	})
	return assistant.ProtocolResult{
		ProtocolVersion: assistant.ProtocolV1Alpha1,
		Type:            "result",
		Status:          status,
		Output:          "fake assistant completed",
		Structured:      structured,
		Session:         &assistant.ProtocolSessionResult{ID: id, Resumed: resumed},
		ExitCode:        intPtr(code),
		ResolvedModel:   &request.Model,
		Usage:           &assistant.ProtocolUsage{InputTokens: 100, OutputTokens: 25},
	}
}

func sessionID(request assistant.ProtocolRequest) string {
	if request.Session.ID != "" {
		return request.Session.ID
	}
	return "fake-session-1"
}

func writeResult(result assistant.ProtocolResult) {
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}

func writeMutatedResult(result assistant.ProtocolResult, mutate func(map[string]any)) {
	encoded, err := json.Marshal(result)
	if err != nil {
		panic(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		panic(err)
	}
	mutate(value)
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		panic(err)
	}
}

func writeProtocolFailure(code int, message string) {
	result := assistant.ProtocolResult{
		ProtocolVersion: assistant.ProtocolV1Alpha1,
		Type:            "result",
		Status:          "failed",
		Output:          message,
		ExitCode:        intPtr(code),
	}
	writeResult(result)
}

func intPtr(v int) *int { return &v }
