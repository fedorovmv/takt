package agentadapter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const EventProtocolV2 = "takt-agent-events/v2"

type Model struct {
	Name     string         `json:"name"`
	Provider string         `json:"provider"`
	ID       string         `json:"id"`
	Params   map[string]any `json:"params,omitempty"`
}

type SessionRequest struct {
	Mode string `json:"mode"`
	ID   string `json:"id,omitempty"`
}
type Limits struct {
	TimeoutMS      int64 `json:"timeout_ms,omitempty"`
	MaxOutputBytes int   `json:"max_output_bytes,omitempty"`
}

type Policy struct {
	AllowedTools     []string        `json:"allowed_tools,omitempty"`
	DeniedTools      []string        `json:"denied_tools,omitempty"`
	ToolsRestricted  bool            `json:"tools_restricted,omitempty"`
	Skills           []string        `json:"skills,omitempty"`
	SkillsRestricted bool            `json:"skills_restricted,omitempty"`
	MCPPath          string          `json:"mcp_path,omitempty"`
	MCPConfig        json.RawMessage `json:"mcp_config,omitempty"`
	Filesystem       string          `json:"filesystem,omitempty"`
	Network          string          `json:"network,omitempty"`
	Requires         []string        `json:"requires,omitempty"`
}

type Request struct {
	ProtocolVersion string            `json:"protocol_version"`
	Type            string            `json:"type"`
	RunID           string            `json:"run_id"`
	NodeID          string            `json:"node_id"`
	Attempt         int               `json:"attempt"`
	Prompt          string            `json:"prompt"`
	Workspace       string            `json:"workspace"`
	Model           Model             `json:"model"`
	Session         SessionRequest    `json:"session"`
	NativeHooks     json.RawMessage   `json:"native_hooks,omitempty"`
	Policy          *Policy           `json:"policy,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
	Limits          Limits            `json:"limits,omitempty"`
}

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

type Result struct {
	ProtocolVersion string          `json:"protocol_version"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	Output          string          `json:"output,omitempty"`
	Structured      json.RawMessage `json:"structured,omitempty"`
	Session         *SessionResult  `json:"session,omitempty"`
	ExitCode        *int            `json:"exit_code"`
	FailureKind     string          `json:"failure_kind,omitempty"`
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

type ToolDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}
type ToolDecisionMessage struct {
	ProtocolVersion string       `json:"protocol_version"`
	Type            string       `json:"type"`
	CallID          string       `json:"call_id"`
	Decision        ToolDecision `json:"decision"`
}

func ValidateRequest(v Request) error {
	if v.ProtocolVersion != ProtocolV1Alpha2 {
		return fmt.Errorf("request protocol_version must be %s", ProtocolV1Alpha2)
	}
	if v.Type != "request" {
		return fmt.Errorf("request type must be request")
	}
	if strings.TrimSpace(v.RunID) == "" {
		return fmt.Errorf("request run_id is required")
	}
	if strings.TrimSpace(v.NodeID) == "" {
		return fmt.Errorf("request node_id is required")
	}
	if v.Attempt < 1 {
		return fmt.Errorf("request attempt must be at least 1")
	}
	if strings.TrimSpace(v.Model.Provider) == "" || strings.TrimSpace(v.Model.ID) == "" {
		return fmt.Errorf("request model provider and id are required")
	}
	if v.Session.Mode != "fresh" && v.Session.Mode != "resume" {
		return fmt.Errorf("request session mode must be fresh or resume")
	}
	if v.Session.Mode == "resume" && strings.TrimSpace(v.Session.ID) == "" {
		return fmt.Errorf("resume request requires session id")
	}
	if v.Limits.TimeoutMS < 0 || v.Limits.MaxOutputBytes < 0 {
		return fmt.Errorf("request limits cannot be negative")
	}
	for k := range v.Environment {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("request environment contains empty key")
		}
	}
	for k := range v.Metadata {
		if strings.TrimSpace(k) == "" {
			return fmt.Errorf("request metadata contains empty key")
		}
	}
	if v.Policy != nil {
		for _, values := range [][]string{v.Policy.AllowedTools, v.Policy.DeniedTools, v.Policy.Skills, v.Policy.Requires} {
			if err := validateUniqueStrings(values); err != nil {
				return fmt.Errorf("request policy: %w", err)
			}
		}
		if v.Policy.Filesystem != "" && v.Policy.Filesystem != "read_only" {
			return fmt.Errorf("request policy filesystem must be read_only")
		}
		if v.Policy.Network != "" && v.Policy.Network != "deny" {
			return fmt.Errorf("request policy network must be deny")
		}
	}
	return nil
}

func ValidateDeclaration(v Declaration) error {
	if v.Protocol != EventProtocolV2 {
		return fmt.Errorf("declaration protocol must be %s", EventProtocolV2)
	}
	if err := validateUniqueStrings(v.Capabilities); err != nil {
		return fmt.Errorf("declaration capabilities: %w", err)
	}
	if err := validateUniqueStrings(v.EventTypes); err != nil {
		return fmt.Errorf("declaration event_types: %w", err)
	}
	allowed := map[string]bool{"session.started": true, "session.resumed": true, "message": true, "tool.requested": true, "tool.allowed": true, "tool.denied": true, "tool.started": true, "tool.completed": true, "artifact.declared": true, "usage": true, "diagnostic": true, "completed": true, "failed": true}
	for _, event := range v.EventTypes {
		if !allowed[event] {
			return fmt.Errorf("unsupported event type %q", event)
		}
	}
	return nil
}

func ValidateResult(v Result, requestedSessionID string) error {
	if v.ProtocolVersion != ProtocolV1Alpha2 {
		return fmt.Errorf("result protocol_version must be %s", ProtocolV1Alpha2)
	}
	if v.Type != "result" {
		return fmt.Errorf("result type must be result")
	}
	if v.Status != "completed" && v.Status != "failed" {
		return fmt.Errorf("result status must be completed or failed")
	}
	if v.ExitCode == nil {
		return fmt.Errorf("result exit_code is required")
	}
	if v.Status == "completed" && *v.ExitCode != 0 {
		return fmt.Errorf("completed result has non-zero exit_code")
	}
	if v.Status == "failed" && *v.ExitCode == 0 {
		return fmt.Errorf("failed result has zero exit_code")
	}
	if v.FailureKind != "" {
		if v.Status != "failed" {
			return fmt.Errorf("failure_kind is valid only for failed result")
		}
		switch v.FailureKind {
		case "exit", "timed_out", "cancelled":
		default:
			return fmt.Errorf("unsupported failure_kind %q", v.FailureKind)
		}
	}
	if v.Usage != nil && (v.Usage.InputTokens < 0 || v.Usage.OutputTokens < 0 || v.Usage.Cost < 0) {
		return fmt.Errorf("result contains negative usage")
	}
	if requestedSessionID != "" && (v.Session == nil || !v.Session.Resumed || v.Session.ID != requestedSessionID) {
		return fmt.Errorf("requested session %q was not resumed exactly", requestedSessionID)
	}
	return nil
}

func ValidateToolRequest(v ToolRequest) error {
	if strings.TrimSpace(v.CallID) == "" || strings.TrimSpace(v.Tool) == "" {
		return fmt.Errorf("tool request requires call_id and tool")
	}
	return nil
}

func ValidateToolDecision(v ToolDecisionMessage) error {
	if v.ProtocolVersion != ProtocolV1Alpha2 || v.Type != "tool.decision" || strings.TrimSpace(v.CallID) == "" {
		return fmt.Errorf("invalid tool decision envelope")
	}
	if v.Decision.Decision != "allow" && v.Decision.Decision != "deny" {
		return fmt.Errorf("tool decision must be allow or deny")
	}
	return nil
}

func validateUniqueStrings(values []string) error {
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("contains empty value")
		}
		if seen[value] {
			return fmt.Errorf("contains duplicate %q", value)
		}
		seen[value] = true
	}
	return nil
}

func NormalizeDeclaration(v Declaration) Declaration {
	v.Protocol = strings.TrimSpace(v.Protocol)
	v.Capabilities = uniqueSorted(v.Capabilities)
	v.EventTypes = uniqueSorted(v.EventTypes)
	return v
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
