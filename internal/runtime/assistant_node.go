package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
)

type resolvedAssistantNode struct {
	Prompt        string
	AssistantName string
	ModelName     string
	Model         spec.ModelSpec
	Policy        assistant.Policy
	Capabilities  []string
	SessionMode   string
	SessionID     string
}

func (r *Runner) resolveAssistantNode(state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (resolvedAssistantNode, error) {
	prompt := node.Prompt
	assistantName, modelName := node.Provider, node.Model
	if node.Command != "" {
		cmd, err := r.commands.Resolve(node.Command)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve command", Err: err}
		}
		prompt = cmd.Body
		if assistantName == "" {
			assistantName = cmd.Provider
			if assistantName == "" {
				assistantName = cmd.Assistant
			}
		}
		if modelName == "" {
			modelName = cmd.Model
		}
	}
	if assistantName == "" {
		assistantName = r.workflow.Provider
	}
	if modelName == "" {
		modelName = r.workflow.Model
	}
	if assistantName == "" {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: fmt.Errorf("node %q does not resolve an assistant", node.ID)}
	}
	model, ok := r.config.Models[modelName]
	if !ok {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve model", Err: fmt.Errorf("node %q references unknown model %q", node.ID, modelName)}
	}
	policy, err := resolveNodePolicy(node, r.workflowPath, r.inheritedPolicy)
	if err != nil {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve node policy", Err: err}
	}
	var capabilities []string
	if node.Executor != "external" {
		resolver := r.assistants
		if resolver == nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: fmt.Errorf("assistant resolver dependency is required")}
		}
		adapter, err := resolver.Resolve(assistantName)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: err}
		}
		capabilities, err = validateAdapterPolicy(adapter, policy)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "validate assistant capabilities", Err: err}
		}
	} else {
		// The external executor receives the effective policy and must attest to
		// its own capabilities when claiming the task. The policy is still
		// persisted before the hand-off.
		capabilities = assistant.RequiredCapabilities(policy)
		if node.ToolApproval != nil {
			capabilities = append(capabilities, assistant.CapabilityToolControl, assistant.CapabilityAgentEventsV2, assistant.CapabilityToolEvents)
			capabilities = uniqueStrings(capabilities)
		}
	}
	if state.Nodes[node.ID].Policy == nil {
		state.Nodes[node.ID].Policy = policyState(policy, capabilities)
		if state.Nodes[node.ID].Policy != nil {
			if err := r.commit(state, "node.policy.applied", node.ID, map[string]any{"policy": state.Nodes[node.ID].Policy}); err != nil {
				return resolvedAssistantNode{}, err
			}
		}
	}
	sessionMode := node.Context
	if sessionMode == "" {
		sessionMode = "fresh"
	}
	sessionID := state.Nodes[node.ID].SessionID
	if sessionMode == "fresh" && !state.Nodes[node.ID].Resumed {
		sessionID = ""
	}
	if state.Nodes[node.ID].Resumed && sessionID != "" {
		sessionMode = "resume"
	}
	if r.loopFreshContextForNode(node.ID) {
		// fresh_context is the loop-level override: the iteration must not
		// resume an upstream shared session.
		sessionMode, sessionID = "fresh", ""
	} else if node.Context == "shared" {
		source, err := r.sharedSessionSource(state, local, node, assistantName, modelName)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindProtocol, Op: "resolve shared session", Err: err}
		}
		sessionMode, sessionID = "resume", source
	}
	renderedPrompt, err := renderTemplate(prompt, state, local, feedback, artifacts)
	if err != nil {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "render assistant prompt", Err: err}
	}
	if node.OutputFormat != nil {
		outputFormat, err := json.Marshal(node.OutputFormat)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "render assistant output contract", Err: err}
		}
		renderedPrompt += "\n\nRequired JSON output contract (return exactly one JSON value without Markdown fences or commentary):\n" + string(outputFormat)
	}
	return resolvedAssistantNode{
		Prompt: renderedPrompt, AssistantName: assistantName,
		ModelName: modelName, Model: model, Policy: policy, Capabilities: capabilities,
		SessionMode: sessionMode, SessionID: sessionID,
	}, nil
}

func (r *Runner) loopFreshContextForNode(nodeID string) bool {
	if r.workflow == nil {
		return false
	}
	var visit func([]spec.Node) bool
	visit = func(nodes []spec.Node) bool {
		for _, parent := range nodes {
			if parent.LoopGroup == nil {
				continue
			}
			for _, child := range parent.LoopGroup.Nodes {
				if child.ID == nodeID {
					return parent.LoopGroup.FreshContext
				}
			}
			if visit(parent.LoopGroup.Nodes) {
				return true
			}
		}
		return false
	}
	return visit(r.workflow.Nodes)
}

func (r *Runner) sharedSessionSource(state *store.RunState, local map[string]store.NodeState, node spec.Node, provider, model string) (string, error) {
	if r.workflow == nil {
		return "", fmt.Errorf("node %q has no workflow definition", node.ID)
	}
	byID := make(map[string][]spec.Node)
	var collect func([]spec.Node)
	collect = func(nodes []spec.Node) {
		for _, candidate := range nodes {
			byID[candidate.ID] = append(byID[candidate.ID], candidate)
			if candidate.LoopGroup != nil {
				collect(candidate.LoopGroup.Nodes)
			}
		}
	}
	collect(r.workflow.Nodes)

	seen := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, candidate := range byID[id] {
			for _, dependency := range candidate.DependsOn {
				visit(dependency)
			}
		}
	}
	for _, dependency := range node.DependsOn {
		visit(dependency)
	}

	candidateIDs := make([]string, 0, len(seen))
	for id := range seen {
		candidates := byID[id]
		if len(candidates) != 1 {
			return "", fmt.Errorf("node %q has ambiguous upstream ancestor %q", node.ID, id)
		}
		candidate := candidates[0]
		if candidate.Command == "" && candidate.Prompt == "" {
			continue
		}
		candidateProvider, candidateModel := r.nodeBinding(candidate)
		if candidateProvider != provider || candidateModel != model {
			continue
		}
		candidateIDs = append(candidateIDs, id)
	}
	nearest := make([]string, 0, len(candidateIDs))
	for _, candidate := range candidateIDs {
		shadowed := false
		for _, other := range candidateIDs {
			if candidate == other {
				continue
			}
			if runtimeNodeDependsOn(other, candidate, byID, map[string]bool{}) {
				shadowed = true
				break
			}
		}
		if !shadowed {
			nearest = append(nearest, candidate)
		}
	}
	if len(nearest) == 0 {
		return "", fmt.Errorf("node %q has no upstream session", node.ID)
	}
	if len(nearest) > 1 {
		return "", fmt.Errorf("node %q has ambiguous upstream sessions", node.ID)
	}
	var sessions []string
	for _, id := range nearest {
		var candidateState *store.NodeState
		if state != nil {
			candidateState = state.Nodes[id]
		}
		if candidateState == nil {
			if value, ok := local[id]; ok {
				candidateState = &value
			}
		}
		if candidateState == nil || candidateState.SessionID == "" {
			continue
		}
		sessions = append(sessions, candidateState.SessionID)
	}
	if len(sessions) == 0 {
		return "", fmt.Errorf("node %q has no upstream session", node.ID)
	}
	if len(sessions) > 1 {
		return "", fmt.Errorf("node %q has ambiguous upstream sessions", node.ID)
	}
	return sessions[0], nil
}

func runtimeNodeDependsOn(nodeID, target string, byID map[string][]spec.Node, seen map[string]bool) bool {
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	items := byID[nodeID]
	if len(items) != 1 {
		return false
	}
	for _, dependency := range items[0].DependsOn {
		if dependency == target || runtimeNodeDependsOn(dependency, target, byID, seen) {
			return true
		}
	}
	return false
}

func (r *Runner) nodeBinding(node spec.Node) (string, string) {
	provider, model := node.Provider, node.Model
	if node.Command != "" {
		if command, err := r.commands.Resolve(node.Command); err == nil {
			if provider == "" {
				provider = command.Provider
				if provider == "" {
					provider = command.Assistant
				}
			}
			if model == "" {
				model = command.Model
			}
		}
	}
	if provider == "" && r.workflow != nil {
		provider = r.workflow.Provider
	}
	if model == "" && r.workflow != nil {
		model = r.workflow.Model
	}
	return provider, model
}

func (r *Runner) executeExternalNode(state *store.RunState, node spec.Node, resolved resolvedAssistantNode) (execResult, error) {
	ns := state.Nodes[node.ID]
	if ns.External == nil || ns.External.Attempt != ns.Attempts {
		var outputFormat json.RawMessage
		if node.OutputFormat != nil {
			outputFormat, _ = json.Marshal(node.OutputFormat)
		}
		ns.External = &store.ExternalExecutionState{
			Status: "pending", Attempt: ns.Attempts, Prompt: resolved.Prompt, Workspace: r.workspace, ToolCalls: map[string]*store.ToolCallState{},
			IdleTimeout: node.IdleTimeout, LastActivityAt: time.Now().UTC(),
			Assistant:      resolved.AssistantName,
			RequestedModel: &store.ModelRef{Name: resolved.ModelName, Provider: resolved.Model.Provider, ID: resolved.Model.ID, Params: cloneParams(resolved.Model.Params)},
			SessionMode:    resolved.SessionMode, SessionID: resolved.SessionID,
			Policy: policyState(resolved.Policy, resolved.Capabilities), OutputFormat: outputFormat,
		}
		if node.SideEffect != nil {
			ns.External.SideEffectMode = node.SideEffect.Mode
			ns.External.IdempotencyKey = strings.TrimSpace(node.SideEffect.IdempotencyKey)
			if ns.External.IdempotencyKey == "" {
				ns.External.IdempotencyKey = state.ID + ":" + node.ID
			}
		}
		if node.ToolApproval != nil {
			ns.External.ToolApproval = &store.ToolApprovalState{Mode: node.ToolApproval.Mode, Tools: append([]string(nil), node.ToolApproval.Tools...), Message: node.ToolApproval.Message}
		}
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: "external executor must claim and complete this node", Kind: "external_node"}
		ns.Status = store.NodeWaiting
		if err := r.commit(state, "external_node.requested", node.ID, map[string]any{
			"attempt": ns.Attempts, "assistant": resolved.AssistantName, "model": resolved.ModelName,
			"workspace": r.workspace, "policy": ns.External.Policy,
		}); err != nil {
			return execResult{}, err
		}
		return execResult{}, ErrWaiting
	}

	external := ns.External
	switch external.Status {
	case "pending", "claimed":
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: "external executor must complete this node", Kind: "external_node"}
		ns.Status = store.NodeWaiting
		return execResult{}, ErrWaiting
	case "completed", "failed":
		if external.Result == nil {
			return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "external executor", Err: fmt.Errorf("external result is missing")}
		}
		result := external.Result
		execResult := execResult{
			Output: result.Output, Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode,
			SessionID: result.SessionID, Resumed: result.Resumed, Assistant: resolved.AssistantName,
			AssistantVersion: result.AssistantVersion,
			RequestedModel:   &store.ModelRef{Name: resolved.ModelName, Provider: resolved.Model.Provider, ID: resolved.Model.ID, Params: cloneParams(resolved.Model.Params)},
		}
		if result.ResolvedModel != nil {
			execResult.ResolvedModel = result.ResolvedModel
		}
		if result.Usage != nil {
			execResult.Usage = &assistant.ProtocolUsage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, Cost: result.Usage.Cost}
		}
		if external.Status == "failed" || result.ExitCode != 0 || result.Error != "" {
			kind := execution.KindExit
			switch execution.Kind(result.ErrorCode) {
			case execution.KindStart, execution.KindProtocol, execution.KindInternal, execution.KindTimedOut, execution.KindCancelled:
				kind = execution.Kind(result.ErrorCode)
			}
			message := result.Error
			if message == "" {
				message = fmt.Sprintf("external executor returned exit code %d", result.ExitCode)
			}
			return execResult, &execution.Error{Kind: kind, ExitCode: result.ExitCode, Op: "external executor", Err: fmt.Errorf("%s", message)}
		}
		return execResult, nil
	default:
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "external executor", Err: fmt.Errorf("unsupported external status %q", external.Status)}
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
