// Package agentadapter contains dependency-light conformance helpers for
// out-of-process coding-agent adapters. It validates the public
// takt-assistant/v1alpha2 transcript without importing Takt runtime internals,
// so adapter projects can reuse it as a test dependency.
package agentadapter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const ProtocolV1Alpha2 = "takt-assistant/v1alpha2"

type Options struct {
	RequestedSessionID   string
	RequireDeclaration   bool
	RequireToolControl   bool
	RequiredCapabilities []string
}

type Report struct {
	Records      int      `json:"records"`
	Events       int      `json:"events"`
	ToolRequests int      `json:"tool_requests"`
	Terminal     bool     `json:"terminal"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// ValidateTranscript validates one captured stdout stream from a v1alpha2
// adapter. It catches protocol failures that should be proven once by every
// Codex/Qwen/Pi wrapper: missing declaration/result, malformed records,
// duplicate tool request IDs, unsupported capability claims and broken resume
// identity. Host-specific enforcement still needs its own fixture/live tests.
func ValidateTranscript(r io.Reader, options Options) (Report, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	report := Report{}
	declared := false
	declaredToolControl := false
	terminal := false
	callIDs := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if terminal {
			return report, fmt.Errorf("record after terminal result")
		}
		var record TranscriptRecord
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&record); err != nil {
			return report, fmt.Errorf("record %d: %w", report.Records+1, err)
		}
		var trailing any
		if err := dec.Decode(&trailing); err != io.EOF {
			if err == nil {
				return report, fmt.Errorf("record %d contains multiple JSON values", report.Records+1)
			}
			return report, fmt.Errorf("record %d trailing data: %w", report.Records+1, err)
		}
		report.Records++
		if record.ProtocolVersion != ProtocolV1Alpha2 {
			return report, fmt.Errorf("record %d uses protocol %q", report.Records, record.ProtocolVersion)
		}
		if options.RequireDeclaration && !declared && record.Type != "capabilities" {
			return report, fmt.Errorf("capability declaration must precede %s", record.Type)
		}
		switch record.Type {
		case "capabilities":
			if declared || record.Declaration == nil {
				return report, fmt.Errorf("invalid capabilities record")
			}
			if err := ValidateDeclaration(*record.Declaration); err != nil {
				return report, err
			}
			declared = true
			report.Capabilities = append([]string(nil), record.Declaration.Capabilities...)
			declaredToolControl = record.Declaration.ToolControl
			if options.RequireToolControl && !declaredToolControl {
				return report, fmt.Errorf("adapter does not declare required tool_control")
			}
			for _, required := range options.RequiredCapabilities {
				if !contains(record.Declaration.Capabilities, required) {
					return report, fmt.Errorf("adapter does not declare required capability %q", required)
				}
			}
		case "event":
			if len(record.Event) == 0 || string(record.Event) == "null" {
				return report, fmt.Errorf("event record requires event")
			}
			report.Events++
		case "tool.request":
			if record.ToolRequest == nil {
				return report, fmt.Errorf("tool.request requires tool_request")
			}
			if err := ValidateToolRequest(*record.ToolRequest); err != nil {
				return report, err
			}
			if callIDs[record.ToolRequest.CallID] {
				return report, fmt.Errorf("duplicate tool request call_id %q", record.ToolRequest.CallID)
			}
			callIDs[record.ToolRequest.CallID] = true
			report.ToolRequests++
			if !declared || !declaredToolControl {
				return report, fmt.Errorf("tool request without declared tool_control")
			}
		case "result":
			if record.Result == nil {
				return report, fmt.Errorf("terminal result requires result")
			}
			if err := ValidateResult(*record.Result, options.RequestedSessionID); err != nil {
				return report, err
			}
			terminal = true
			report.Terminal = true
		default:
			return report, fmt.Errorf("unsupported record type %q", record.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		return report, err
	}
	if options.RequireDeclaration && !declared {
		return report, fmt.Errorf("capability declaration is required")
	}
	if !terminal {
		return report, fmt.Errorf("terminal result is missing")
	}
	return report, nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
