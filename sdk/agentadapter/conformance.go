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

type Declaration struct {
	Protocol       string   `json:"protocol"`
	Capabilities   []string `json:"capabilities,omitempty"`
	EventTypes     []string `json:"event_types,omitempty"`
	SessionEvents  bool     `json:"session_events,omitempty"`
	ToolEvents     bool     `json:"tool_events,omitempty"`
	ToolControl    bool     `json:"tool_control,omitempty"`
	ArtifactEvents bool     `json:"artifact_events,omitempty"`
	UsageEvents    bool     `json:"usage_events,omitempty"`
}

type ToolRequest struct {
	CallID    string          `json:"call_id"`
	Tool      string          `json:"tool"`
	Input     json.RawMessage `json:"input,omitempty"`
	Message   string          `json:"message,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
}

type SessionResult struct {
	ID      string `json:"id,omitempty"`
	Resumed bool   `json:"resumed,omitempty"`
}

type Usage struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
}

type Model struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	ID       string         `json:"id"`
	Params   map[string]any `json:"params,omitempty"`
}

type Result struct {
	ProtocolVersion string          `json:"protocol_version"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	Output          string          `json:"output,omitempty"`
	Structured      json.RawMessage `json:"structured,omitempty"`
	Session         *SessionResult  `json:"session,omitempty"`
	ExitCode        *int            `json:"exit_code"`
	ResolvedModel   *Model          `json:"resolved_model,omitempty"`
	Usage           *Usage          `json:"usage,omitempty"`
}

type TranscriptRecord struct {
	ProtocolVersion string          `json:"protocol_version"`
	Type            string          `json:"type"`
	Declaration     *Declaration    `json:"declaration,omitempty"`
	Event           json.RawMessage `json:"event,omitempty"`
	ToolRequest     *ToolRequest    `json:"tool_request,omitempty"`
	Result          *Result         `json:"result,omitempty"`
}

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
			declared = true
			if record.Declaration.Protocol != "" && record.Declaration.Protocol != "takt-agent-events/v2" {
				return report, fmt.Errorf("unsupported event protocol %q", record.Declaration.Protocol)
			}
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
			if record.ToolRequest == nil || record.ToolRequest.CallID == "" || record.ToolRequest.Tool == "" {
				return report, fmt.Errorf("tool.request requires call_id and tool")
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
			if record.Result == nil || record.Result.ExitCode == nil {
				return report, fmt.Errorf("terminal result requires exit_code")
			}
			if record.Result.ProtocolVersion != ProtocolV1Alpha2 || record.Result.Type != "result" {
				return report, fmt.Errorf("terminal result must use %s result", ProtocolV1Alpha2)
			}
			if record.Result.Status != "completed" && record.Result.Status != "failed" {
				return report, fmt.Errorf("terminal result status must be completed or failed")
			}
			if record.Result.Status == "completed" && *record.Result.ExitCode != 0 {
				return report, fmt.Errorf("completed result has non-zero exit_code")
			}
			if record.Result.Status == "failed" && *record.Result.ExitCode == 0 {
				return report, fmt.Errorf("failed result has zero exit_code")
			}
			if record.Result.Usage != nil && (record.Result.Usage.InputTokens < 0 || record.Result.Usage.OutputTokens < 0 || record.Result.Usage.Cost < 0) {
				return report, fmt.Errorf("terminal result contains negative usage")
			}
			if options.RequestedSessionID != "" {
				if record.Result.Session == nil || !record.Result.Session.Resumed || record.Result.Session.ID != options.RequestedSessionID {
					return report, fmt.Errorf("requested session %q was not resumed exactly", options.RequestedSessionID)
				}
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
