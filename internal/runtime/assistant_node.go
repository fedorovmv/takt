package runtime

import (
	"encoding/json"
	"fmt"

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
	assistantName, modelName := node.Assistant, node.Model
	if node.Command != "" {
		cmd, err := r.Commands.Resolve(node.Command)
		if err != nil {
			return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve command", Err: err}
		}
		prompt = cmd.Body
		if assistantName == "" {
			assistantName = cmd.Assistant
		}
		if modelName == "" {
			modelName = cmd.Model
		}
	}
	if assistantName == "" {
		assistantName = r.Workflow.Defaults.Assistant
	}
	if modelName == "" {
		modelName = r.Workflow.Defaults.Model
	}
	if assistantName == "" {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: fmt.Errorf("node %q does not resolve an assistant", node.ID)}
	}
	model, ok := r.Config.Models[modelName]
	if !ok {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve model", Err: fmt.Errorf("node %q references unknown model %q", node.ID, modelName)}
	}
	policy, err := resolveNodePolicy(node, r.WorkflowPath, r.inheritedPolicy)
	if err != nil {
		return resolvedAssistantNode{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve node policy", Err: err}
	}
	var capabilities []string
	if node.Executor != "external" {
		resolver := r.Assistants
		if resolver == nil {
			resolver = assistant.Factory{Config: r.Config}
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
	}
	if state.Nodes[node.ID].Policy == nil {
		state.Nodes[node.ID].Policy = policyState(policy, capabilities)
		if state.Nodes[node.ID].Policy != nil {
			if err := r.commit(state, "node.policy.applied", node.ID, map[string]any{"policy": state.Nodes[node.ID].Policy}); err != nil {
				return resolvedAssistantNode{}, err
			}
		}
	}
	sessionMode := node.Session
	if sessionMode == "" {
		sessionMode = r.Workflow.Defaults.Session
	}
	if sessionMode == "" {
		sessionMode = "fresh"
	}
	sessionID := state.Nodes[node.ID].SessionID
	if sessionMode == "fresh" {
		sessionID = ""
	}
	return resolvedAssistantNode{
		Prompt: renderTemplate(prompt, state, local, feedback, artifacts), AssistantName: assistantName,
		ModelName: modelName, Model: model, Policy: policy, Capabilities: capabilities,
		SessionMode: sessionMode, SessionID: sessionID,
	}, nil
}

func (r *Runner) executeExternalNode(state *store.RunState, node spec.Node, resolved resolvedAssistantNode) (execResult, error) {
	ns := state.Nodes[node.ID]
	if ns.External == nil || ns.External.Attempt != ns.Attempts {
		var outputFormat json.RawMessage
		if node.OutputFormat != nil {
			outputFormat, _ = json.Marshal(node.OutputFormat)
		}
		ns.External = &store.ExternalExecutionState{
			Status: "pending", Attempt: ns.Attempts, Prompt: resolved.Prompt, Workspace: r.Workspace,
			Assistant:      resolved.AssistantName,
			RequestedModel: &store.ModelRef{Name: resolved.ModelName, Provider: resolved.Model.Provider, ID: resolved.Model.ID, Params: cloneParams(resolved.Model.Params)},
			SessionMode:    resolved.SessionMode, SessionID: resolved.SessionID,
			Policy: policyState(resolved.Policy, resolved.Capabilities), OutputFormat: outputFormat,
		}
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: "external executor must claim and complete this node", Kind: "external_node"}
		ns.Status = store.NodeWaiting
		if err := r.commit(state, "external_node.requested", node.ID, map[string]any{
			"attempt": ns.Attempts, "assistant": resolved.AssistantName, "model": resolved.ModelName,
			"workspace": r.Workspace, "policy": ns.External.Policy,
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
