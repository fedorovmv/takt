package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
)

// OpenCode integrates the OpenCode coding-agent CLI through its official
// non-interactive JSON event stream. OpenCode remains responsible for its
// internal tool loop, files, shell, MCP, skills, and session persistence;
// Takt supplies the prompt and normalizes the final result.
type OpenCode struct {
	spec              spec.AssistantSpec
	onOutputTruncated func()
}

func NewOpenCode(assistantSpec spec.AssistantSpec) OpenCode {
	return OpenCode{spec: assistantSpec}
}

// WithOutputTruncatedObserver returns a copy that invokes observer when the
// shared stdout/stderr budget is exceeded. The callback is intended for
// deterministic contract tests and does not replace adapter cancellation.
func (o OpenCode) WithOutputTruncatedObserver(observer func()) OpenCode {
	o.onOutputTruncated = observer
	return o
}

const defaultOpenCodeOutputLimit = 10 * 1024 * 1024

func (o OpenCode) Run(ctx context.Context, req Request) (Result, error) {
	binary := strings.TrimSpace(o.spec.Binary)
	if binary == "" {
		binary = "opencode"
	}
	if err := validateOpenCodeArgs(o.spec.Args); err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "opencode adapter", Err: err}
	}

	env, err := openCodeEnvironment(o.spec, req)
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "opencode policy", Err: err}
	}
	prompt, err := openCodePrompt(req.Prompt, req.Policy)
	if err != nil {
		return Result{}, &execution.Error{Kind: execution.KindProtocol, Op: "opencode skills", Err: err}
	}
	version, err := probeOpenCodeVersion(ctx, binary, req.Workspace, env)
	if err != nil {
		return Result{}, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	args := openCodeArgs(o.spec, req)
	cmd := exec.CommandContext(runCtx, binary, args...)
	execution.ConfigureCommand(cmd)
	cmd.Dir = req.Workspace
	cmd.Env = env
	cmd.Stdin = strings.NewReader(prompt)

	limit := o.spec.MaxOutputBytes
	if limit == 0 {
		limit = defaultOpenCodeOutputLimit
	}
	var limitOnce sync.Once
	budget := core.NewOutputBudget(limit, func() {
		if o.onOutputTruncated != nil {
			o.onOutputTruncated()
		}
		limitOnce.Do(cancel)
	})
	stdout := newLimitedBuffer(budget)
	stderr := newLimitedBuffer(budget)
	cmd.Stdout, cmd.Stderr = stdout, stderr

	runErr := cmd.Run()
	rawStdout, rawStderr := stdout.String(), stderr.String()
	diagnostic := openCodeDiagnostics(rawStdout, rawStderr)
	result := Result{
		ExitCode:         0,
		Stdout:           rawStdout,
		Stderr:           rawStderr,
		AssistantVersion: version,
		Truncated:        stdout.Truncated() || stderr.Truncated(),
	}
	if diagnostic != "" {
		result.Output = diagnostic
	}

	if priorityErr := openCodePriorityError(ctx, result.Truncated, diagnostic); priorityErr != nil {
		result.ExitCode = -1
		return result, priorityErr
	}

	osExitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			result.ExitCode = -1
			return result, &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "opencode run", Err: runErr}
		}
		osExitCode = exitErr.ExitCode()
		result.ExitCode = osExitCode
	}

	if strings.TrimSpace(rawStdout) == "" {
		result.Output = strings.TrimSpace(rawStderr)
		if runErr != nil {
			return result, &execution.Error{Kind: execution.KindExit, ExitCode: osExitCode, Op: "opencode run", Err: runErr}
		}
		return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "opencode events", Err: fmt.Errorf("opencode produced no JSON events")}
	}

	parsed, parseErr := decodeOpenCodeEvents([]byte(rawStdout), req)
	if parseErr != nil {
		result.Output = combineOutput(rawStdout, rawStderr)
		result.ExitCode = -1
		return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "opencode events", Err: parseErr}
	}
	result.Output = parsed.Output
	result.SessionID = parsed.SessionID
	result.Resumed = parsed.Resumed
	result.ResolvedModel = parsed.ResolvedModel
	result.Usage = parsed.Usage
	result.Structured = parsed.Structured

	if len(parsed.Errors) > 0 {
		if result.ExitCode == 0 {
			result.ExitCode = 1
		}
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: result.ExitCode, Op: "opencode agent", Err: errors.New(strings.Join(parsed.Errors, "; "))}
	}
	if runErr != nil {
		return result, &execution.Error{Kind: execution.KindExit, ExitCode: osExitCode, Op: "opencode run", Err: runErr}
	}
	if parsed.Usage == nil {
		result.ExitCode = -1
		return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "opencode events", Err: fmt.Errorf("successful run did not emit step_finish usage")}
	}
	return result, nil
}

func openCodePriorityError(ctx context.Context, truncated bool, diagnostic string) error {
	if ctx != nil && ctx.Err() != nil {
		kind := execution.KindCancelled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = execution.KindTimedOut
		}
		return &execution.Error{Kind: kind, ExitCode: -1, Op: "opencode run", Err: withOpenCodeDiagnostic(ctx.Err(), diagnostic)}
	}
	if truncated {
		return &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "opencode run", Err: withOpenCodeDiagnostic(fmt.Errorf("opencode output exceeded max_output_bytes"), diagnostic)}
	}
	return nil
}

func withOpenCodeDiagnostic(base error, diagnostic string) error {
	diagnostic = strings.TrimSpace(diagnostic)
	if diagnostic == "" {
		return base
	}
	return fmt.Errorf("%w; OpenCode diagnostics: %s", base, diagnostic)
}

// openCodeDiagnostics extracts the most useful provider/transport messages
// available before a timeout or cancellation. OpenCode may report retries on
// stderr and structured provider errors as JSON events on stdout. The raw
// streams remain available separately in Result.Stdout and Result.Stderr.
func openCodeDiagnostics(rawStdout, rawStderr string) string {
	messages := make([]string, 0, 8)
	seen := make(map[string]struct{})
	appendMessage := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if len(value) > 2048 {
			value = value[:2048] + "…"
		}
		if _, found := seen[value]; found {
			return
		}
		seen[value] = struct{}{}
		messages = append(messages, value)
	}

	// Structured provider errors are more valuable than generic retry logs,
	// so reserve space for them even when stderr is noisy.
	scanner := bufio.NewScanner(strings.NewReader(rawStdout))
	maxLine := len(rawStdout) + 1
	if maxLine < 64*1024 {
		maxLine = 64 * 1024
	}
	scanner.Buffer(make([]byte, 64*1024), maxLine)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event openCodeEvent
		if json.Unmarshal(line, &event) != nil || event.Type != "error" {
			continue
		}
		appendMessage(openCodeErrorMessage(event.Error))
		if len(messages) >= 4 {
			break
		}
	}
	for _, line := range strings.Split(rawStderr, "\n") {
		appendMessage(line)
		if len(messages) >= 8 {
			break
		}
	}
	return strings.Join(messages, "; ")
}

func openCodeArgs(s spec.AssistantSpec, req Request) []string {
	args := []string{"run", "--format", "json", "--dir", req.Workspace}
	if req.Model.Provider != "" && req.Model.ID != "" {
		args = append(args, "--model", req.Model.Provider+"/"+req.Model.ID)
	}
	if s.Agent != "" {
		args = append(args, "--agent", s.Agent)
	}
	if variant := openCodeVariant(req.Model.Params); variant != "" {
		args = append(args, "--variant", variant)
	}
	if s.AutoApprove {
		args = append(args, "--auto")
	}
	mode, id := effectiveSession(req.SessionMode, req.SessionID)
	if mode == "resume" {
		args = append(args, "--session", id)
	}
	args = append(args, s.Args...)
	return args
}

func validateOpenCodeArgs(args []string) error {
	reserved := map[string]struct{}{
		"run": {}, "--format": {}, "--model": {}, "-m": {}, "--agent": {}, "--session": {}, "-s": {},
		"--continue": {}, "-c": {}, "--fork": {}, "--dir": {}, "--variant": {}, "--auto": {},
		"--dangerously-skip-permissions": {}, "--yolo": {}, "--interactive": {}, "-i": {}, "--mini": {},
		"--command": {}, "--file": {}, "-f": {}, "--attach": {}, "--port": {}, "--title": {}, "--thinking": {},
		"--version": {}, "-v": {}, "--help": {}, "-h": {},
	}
	for _, arg := range args {
		key := arg
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		if _, found := reserved[key]; found {
			return fmt.Errorf("opencode args cannot override reserved option %q", key)
		}
	}
	return nil
}

func openCodeVariant(params map[string]any) string {
	for _, key := range []string{"variant", "reasoning_effort"} {
		if value, ok := params[key].(string); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func openCodePrompt(prompt string, policy Policy) (string, error) {
	var injected []string
	for _, value := range policy.Skills {
		path := filepath.Clean(value)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // named skills are discovered by OpenCode itself
			}
			return "", fmt.Errorf("resolve skill %q: %w", value, err)
		}
		if info.IsDir() {
			path = filepath.Join(path, "SKILL.md")
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read skill %q: %w", value, err)
		}
		injected = append(injected, fmt.Sprintf("<takt-skill source=%q>\n%s\n</takt-skill>", path, strings.TrimSpace(string(body))))
	}
	if len(injected) == 0 {
		return prompt, nil
	}
	return "The following Takt skills are mandatory instructions for this node.\n\n" + strings.Join(injected, "\n\n") + "\n\n<task>\n" + prompt + "\n</task>\n", nil
}

func openCodeEnvironment(s spec.AssistantSpec, req Request) ([]string, error) {
	values := make(map[string]string)
	for _, entry := range os.Environ() {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	for key, value := range s.Env {
		values[key] = renderArg(value, req)
	}
	paramsJSON, _ := json.Marshal(req.Model.Params)
	metadataJSON, _ := json.Marshal(req.Metadata)
	mode, sessionID := effectiveSession(req.SessionMode, req.SessionID)
	values["TAKT_RUN_ID"] = req.RunID
	values["TAKT_NODE_ID"] = req.NodeID
	values["TAKT_ATTEMPT"] = strconv.Itoa(req.Attempt)
	values["TAKT_WORKSPACE"] = req.Workspace
	values["TAKT_MODEL_NAME"] = req.ModelName
	values["TAKT_MODEL_PROVIDER"] = req.Model.Provider
	values["TAKT_MODEL_ID"] = req.Model.ID
	values["TAKT_MODEL_PARAMS_JSON"] = string(paramsJSON)
	values["TAKT_SESSION_MODE"] = mode
	values["TAKT_SESSION_ID"] = sessionID
	values["TAKT_METADATA_JSON"] = string(metadataJSON)
	values["TAKT_NATIVE_HOOKS_JSON"] = ""
	policyJSON, _ := json.Marshal(req.Policy)
	values["TAKT_POLICY_JSON"] = string(policyJSON)
	if len(req.NativeHooks) > 0 {
		if compact, err := compactJSON(req.NativeHooks); err == nil {
			values["TAKT_NATIVE_HOOKS_JSON"] = compact
		}
	}
	if policyConfig, err := openCodePolicyConfig(req.Policy); err != nil {
		return nil, err
	} else if len(policyConfig) > 0 {
		base := map[string]any{}
		if raw := strings.TrimSpace(values["OPENCODE_CONFIG_CONTENT"]); raw != "" {
			if err := json.Unmarshal([]byte(raw), &base); err != nil {
				return nil, fmt.Errorf("parse existing OPENCODE_CONFIG_CONTENT: %w", err)
			}
		}
		mergeOpenCodeConfig(base, policyConfig)
		encoded, err := json.Marshal(base)
		if err != nil {
			return nil, err
		}
		values["OPENCODE_CONFIG_CONTENT"] = string(encoded)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env, nil
}

func openCodePolicyConfig(policy Policy) (map[string]any, error) {
	config := map[string]any{}
	permission := map[string]any{}
	if policy.ToolsRestricted || len(policy.AllowedTools) > 0 {
		permission["*"] = "deny"
		for _, tool := range policy.AllowedTools {
			permission[tool] = "allow"
		}
	}
	for _, tool := range policy.DeniedTools {
		permission[tool] = "deny"
	}
	if policy.Filesystem == "read_only" {
		if !policy.ToolsRestricted && len(policy.AllowedTools) == 0 {
			permission["*"] = "deny"
			for _, tool := range []string{"read", "glob", "grep", "lsp", "skill"} {
				permission[tool] = "allow"
			}
		}
		permission["edit"] = "deny"
		permission["write"] = "deny"
		permission["bash"] = "deny"
		permission["task"] = "deny"
	}
	if policy.SkillsRestricted || len(policy.Skills) > 0 {
		rules := map[string]any{"*": "deny"}
		for _, skill := range policy.Skills {
			rules[openCodeSkillName(skill)] = "allow"
		}
		permission["skill"] = rules
	}
	if len(permission) > 0 {
		config["permission"] = permission
	}
	if len(policy.MCPConfig) > 0 {
		var raw map[string]any
		if err := json.Unmarshal(policy.MCPConfig, &raw); err != nil {
			return nil, fmt.Errorf("parse MCP config %s: %w", policy.MCPPath, err)
		}
		if value, ok := raw["mcp"].(map[string]any); ok {
			config["mcp"] = value
		} else {
			config["mcp"] = raw
		}
	}
	return config, nil
}

func openCodeSkillName(value string) string {
	value = filepath.Clean(value)
	if strings.EqualFold(filepath.Base(value), "SKILL.md") {
		return filepath.Base(filepath.Dir(value))
	}
	return filepath.Base(value)
}

func mergeOpenCodeConfig(base, overlay map[string]any) {
	for key, value := range overlay {
		current, currentOK := base[key].(map[string]any)
		next, nextOK := value.(map[string]any)
		if currentOK && nextOK {
			mergeOpenCodeConfig(current, next)
			continue
		}
		base[key] = value
	}
}

func probeOpenCodeVersion(ctx context.Context, binary, workspace string, env []string) (string, error) {
	return probeOpenCodeVersionWithTimeout(ctx, binary, workspace, env, 10*time.Second)
}

func probeOpenCodeVersionWithTimeout(ctx context.Context, binary, workspace string, env []string, timeout time.Duration) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, binary, "--version")
	execution.ConfigureCommand(cmd)
	cmd.Dir = workspace
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		kind := execution.KindCancelled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			kind = execution.KindTimedOut
		}
		return "", &execution.Error{Kind: kind, ExitCode: -1, Op: "opencode version", Err: ctx.Err()}
	}
	if probeCtx.Err() != nil {
		return "", &execution.Error{Kind: execution.KindTimedOut, ExitCode: -1, Op: "opencode version", Err: probeCtx.Err()}
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", &execution.Error{Kind: execution.KindExit, ExitCode: exitErr.ExitCode(), Op: "opencode version", Err: fmt.Errorf("version probe exited with code %d", exitErr.ExitCode())}
		}
		return "", &execution.Error{Kind: execution.KindStart, ExitCode: -1, Op: "opencode version", Err: err}
	}
	version := strings.TrimSpace(string(output))
	if version == "" {
		return "", &execution.Error{Kind: execution.KindProtocol, ExitCode: -1, Op: "opencode version", Err: fmt.Errorf("empty version output")}
	}
	return version, nil
}

type openCodeEvent struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp,omitempty"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
	Model     *struct {
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	} `json:"model,omitempty"`
}

type openCodePart struct {
	ID        string `json:"id,omitempty"`
	MessageID string `json:"messageID,omitempty"`
	SessionID string `json:"sessionID,omitempty"`
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Tokens    *struct {
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning,omitempty"`
		Cache     struct {
			Read  int `json:"read,omitempty"`
			Write int `json:"write,omitempty"`
		} `json:"cache,omitempty"`
	} `json:"tokens,omitempty"`
	Cost float64 `json:"cost,omitempty"`
}

type decodedOpenCode struct {
	Output        string
	SessionID     string
	Resumed       bool
	ResolvedModel *ProtocolModel
	Usage         *ProtocolUsage
	Structured    json.RawMessage
	Errors        []string
}

func decodeOpenCodeEvents(raw []byte, req Request) (decodedOpenCode, error) {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	max := len(raw) + 1
	if max < 64*1024 {
		max = 64 * 1024
	}
	scanner.Buffer(make([]byte, 64*1024), max)

	var sessionID string
	textOrder := make([]string, 0)
	textByID := make(map[string]string)
	seenSteps := make(map[string]struct{})
	usage := ProtocolUsage{}
	usageSeen := false
	errorsSeen := make([]string, 0)
	toolCalls := 0
	eventCount := 0
	unknownEvents := 0
	resolved := &ProtocolModel{Name: req.ModelName, Provider: req.Model.Provider, ID: req.Model.ID, Params: cloneProtocolParams(req.Model.Params)}
	resolutionSource := "requested_cli_model"

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		eventCount++
		var event openCodeEvent
		if err := json.Unmarshal(line, &event); err != nil {
			return decodedOpenCode{}, fmt.Errorf("decode event %d: %w", eventCount, err)
		}
		if event.Type == "" {
			return decodedOpenCode{}, fmt.Errorf("event %d has no type", eventCount)
		}
		if event.SessionID != "" {
			if sessionID == "" {
				sessionID = event.SessionID
			} else if sessionID != event.SessionID {
				return decodedOpenCode{}, fmt.Errorf("event stream changed session from %q to %q", sessionID, event.SessionID)
			}
		}
		if event.Model != nil && event.Model.ProviderID != "" && event.Model.ModelID != "" {
			resolved.Provider = event.Model.ProviderID
			resolved.ID = event.Model.ModelID
			resolutionSource = "event_model"
		}

		switch event.Type {
		case "text", "step_start", "step_finish", "tool_use", "reasoning":
			if len(event.Part) == 0 {
				return decodedOpenCode{}, fmt.Errorf("%s event %d has no part", event.Type, eventCount)
			}
			var part openCodePart
			if err := json.Unmarshal(event.Part, &part); err != nil {
				return decodedOpenCode{}, fmt.Errorf("decode %s part: %w", event.Type, err)
			}
			if part.SessionID != "" && sessionID != "" && part.SessionID != sessionID {
				return decodedOpenCode{}, fmt.Errorf("part session %q differs from event session %q", part.SessionID, sessionID)
			}
			switch event.Type {
			case "text":
				if part.Type != "text" {
					return decodedOpenCode{}, fmt.Errorf("text event contains part type %q", part.Type)
				}
				key := part.ID
				if key == "" {
					key = fmt.Sprintf("text-%d", eventCount)
				}
				if _, found := textByID[key]; !found {
					textOrder = append(textOrder, key)
				}
				textByID[key] = part.Text
			case "step_finish":
				if part.Type != "step-finish" {
					return decodedOpenCode{}, fmt.Errorf("step_finish event contains part type %q", part.Type)
				}
				key := part.ID
				if key == "" {
					key = fmt.Sprintf("step-%d", eventCount)
				}
				if _, found := seenSteps[key]; found {
					continue
				}
				seenSteps[key] = struct{}{}
				if part.Tokens == nil {
					return decodedOpenCode{}, fmt.Errorf("step_finish part %q has no tokens", key)
				}
				if part.Tokens.Input < 0 || part.Tokens.Output < 0 || part.Tokens.Reasoning < 0 || part.Tokens.Cache.Read < 0 || part.Tokens.Cache.Write < 0 || part.Cost < 0 {
					return decodedOpenCode{}, fmt.Errorf("step_finish part %q contains negative usage", key)
				}
				usageSeen = true
				usage.InputTokens += part.Tokens.Input
				usage.OutputTokens += part.Tokens.Output
				usage.Cost += part.Cost
			case "tool_use":
				toolCalls++
			}
		case "error":
			errorsSeen = append(errorsSeen, openCodeErrorMessage(event.Error))
		default:
			unknownEvents++
		}
	}
	if err := scanner.Err(); err != nil {
		return decodedOpenCode{}, fmt.Errorf("scan event stream: %w", err)
	}
	if eventCount == 0 {
		return decodedOpenCode{}, fmt.Errorf("empty event stream")
	}
	if sessionID == "" {
		return decodedOpenCode{}, fmt.Errorf("event stream did not expose sessionID")
	}
	mode, requestedSessionID := effectiveSession(req.SessionMode, req.SessionID)
	resumed := mode == "resume"
	if resumed && sessionID != requestedSessionID {
		return decodedOpenCode{}, fmt.Errorf("opencode resumed session %q instead of %q", sessionID, requestedSessionID)
	}

	texts := make([]string, 0, len(textOrder))
	for _, key := range textOrder {
		if text := strings.TrimSpace(textByID[key]); text != "" {
			texts = append(texts, text)
		}
	}
	structured, _ := json.Marshal(map[string]any{
		"adapter":                 "opencode",
		"usage_semantics":         "sum_step_finish",
		"model_resolution_source": resolutionSource,
		"event_count":             eventCount,
		"step_count":              len(seenSteps),
		"tool_calls":              toolCalls,
		"unknown_events":          unknownEvents,
		"error_events":            len(errorsSeen),
	})
	var usageResult *ProtocolUsage
	if usageSeen {
		copy := usage
		usageResult = &copy
	}
	return decodedOpenCode{
		Output:        strings.Join(texts, "\n"),
		SessionID:     sessionID,
		Resumed:       resumed,
		ResolvedModel: resolved,
		Usage:         usageResult,
		Structured:    structured,
		Errors:        errorsSeen,
	}, nil
}

func openCodeErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "opencode session error"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	if object, ok := value.(map[string]any); ok {
		if data, ok := object["data"].(map[string]any); ok {
			if message, ok := data["message"].(string); ok && strings.TrimSpace(message) != "" {
				return message
			}
		}
		if message, ok := object["message"].(string); ok && strings.TrimSpace(message) != "" {
			return message
		}
		if name, ok := object["name"].(string); ok && strings.TrimSpace(name) != "" {
			return name
		}
	}
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func cloneProtocolParams(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
func (o OpenCode) Capabilities() []string {
	return mergeCapabilities([]string{CapabilityToolPolicy, CapabilitySkills, CapabilityMCP, CapabilitySandboxFilesystem}, o.spec.Capabilities)
}

func (o OpenCode) CapabilityDeclaration() CapabilityDeclaration {
	return CapabilityDeclaration{
		Capabilities: o.Capabilities(), EventTypes: []string{EventSessionStarted, EventSessionResumed, EventMessage, EventUsage, EventDiagnostic, EventCompleted, EventFailed},
		SessionEvents: true, UsageEvents: true,
	}
}

// ProbeVersion checks the configured OpenCode CLI without starting an agent session.
func ProbeVersion(ctx context.Context, assistantSpec spec.AssistantSpec, workspace string) (string, error) {
	binary := strings.TrimSpace(assistantSpec.Binary)
	if binary == "" {
		binary = "opencode"
	}
	env, err := openCodeEnvironment(assistantSpec, Request{Workspace: workspace, Attempt: 1})
	if err != nil {
		return "", err
	}
	return probeOpenCodeVersion(ctx, binary, workspace, env)
}
