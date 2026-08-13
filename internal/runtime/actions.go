package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/flowref"
	"takt/internal/spec"
	"takt/internal/store"
)

type actionContext struct {
	local     map[string]store.NodeState
	artifacts string
	feedback  string
}

func (r *Runner) actionContext(state *store.RunState, node spec.Node, loopPrevious map[string]store.NodeState) actionContext {
	local := loopPrevious
	if local == nil {
		local = map[string]store.NodeState{}
	}
	return actionContext{local: local, artifacts: r.store.ArtifactsDir(state.ID), feedback: state.Nodes[node.ID].Feedback}
}

func (r *Runner) executeBashAction(ctx context.Context, state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	rendered, err := renderTemplateSurface(node.Bash, flowref.Shell, state, action.local, action.feedback, action.artifacts, nil)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render bash node", Err: err}
	}
	return r.runBashWithEnv(ctx, node, rendered, shellEnvironment(state, action.feedback, action.artifacts))
}

func (r *Runner) executeScriptAction(ctx context.Context, state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	result, err := r.runScript(ctx, state, node, action.local, action.feedback, action.artifacts)
	return normalizeStructuredResult(result, err, node.OutputFormat, "validate structured output")
}

func (r *Runner) executeApprovalAction(state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	if answer, ok := state.Approvals[node.ID]; ok {
		output := ""
		if node.Approval.CaptureResponse {
			output = answer
		}
		return execResult{Output: output, Stdout: output, ExitCode: 0}, nil
	}
	message, err := renderTemplate(node.Approval.Message, state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render approval message", Err: err}
	}
	state.Status = store.RunWaiting
	kind := "approval"
	eventType := "approval.requested"
	if node.Approval.CaptureResponse {
		kind = "question"
		eventType = "question.requested"
	}
	state.Waiting = &store.WaitingState{NodeID: node.ID, Message: message, Kind: kind}
	state.Nodes[node.ID].Status = store.NodeWaiting
	if err := r.commit(state, eventType, node.ID, map[string]any{"message": message}); err != nil {
		return execResult{}, err
	}
	return execResult{}, ErrWaiting
}

func (r *Runner) executeCancelAction(state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	reason, err := renderTemplate(node.Cancel, state, action.local, action.feedback, action.artifacts)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render cancel reason", Err: err}
	}
	state.CancelRequested = true
	state.CancelSource = "workflow"
	state.CancelNodePath = canonicalNodePathForState(state, node.ID)
	state.CancelIteration = activeLoopIteration(state, node.ID)
	state.CancelReason = reason
	return execResult{Output: reason, Stdout: reason, ExitCode: 0}, nil
}

func shellEnvironment(state *store.RunState, feedback, artifactsDir string) map[string]string {
	env := map[string]string{"ARGUMENTS": state.Input, "FEEDBACK": feedback, "ARTIFACTS_DIR": artifactsDir}
	if state.Worktree != nil && strings.TrimSpace(state.Worktree.BaseRef) != "" {
		env["BASE_BRANCH"] = state.Worktree.BaseRef
	}
	return env
}

func canonicalNodePathForState(state *store.RunState, nodeID string) string {
	if state != nil {
		if node := state.Nodes[nodeID]; node != nil && node.Path != "" {
			return node.Path
		}
	}
	return canonicalNodePath(nodeID)
}

func activeLoopIteration(state *store.RunState, nodeID string) int {
	if state == nil {
		return 0
	}
	child := state.Nodes[nodeID]
	for _, node := range state.Nodes {
		if node == nil || node.LoopIteration <= 0 {
			continue
		}
		if nodeID == strings.TrimPrefix(node.Path, "/") || (child != nil && strings.HasPrefix(child.Path, node.Path+"/")) {
			return node.LoopIteration
		}
	}
	return 0
}

func (r *Runner) executeInternalAction(ctx context.Context, state *store.RunState, node spec.Node) (execResult, error) {
	switch node.Internal.Mode {
	case "noop":
		return execResult{ExitCode: 0}, nil
	case "worktree":
		if err := r.prepareDynamicWorktree(ctx, state, node.Internal); err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "prepare worktree", Err: err}
		}
		return execResult{Output: r.workspace, Stdout: r.workspace, ExitCode: 0}, nil
	case "result":
		source := state.Nodes[node.Internal.ResultFrom]
		if source == nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "workflow group result", Err: fmt.Errorf("result source %q is missing", node.Internal.ResultFrom)}
		}
		return execResult{Output: source.Output, Stdout: source.Stdout, Stderr: source.Stderr, ExitCode: source.ExitCode, SessionID: source.SessionID, Resumed: source.Resumed, Truncated: source.OutputTruncated}, nil
	case "collect":
		output, err := collectNodeOutputs(state, node.Internal.ResultsFrom)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "workflow group collect", Err: err}
		}
		return execResult{Output: output, Stdout: output, ExitCode: 0}, nil
	default:
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "workflow group", Err: fmt.Errorf("unsupported internal mode %q", node.Internal.Mode)}
	}
}

func (r *Runner) executeAdapterAction(ctx context.Context, state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	result, err := r.runDomainAdapter(ctx, state, node, action.local, action.feedback, action.artifacts)
	return normalizeStructuredResult(result, err, node.OutputFormat, "validate adapter structured output")
}

func (r *Runner) executeAssistantAction(ctx context.Context, state *store.RunState, node spec.Node, action actionContext) (execResult, error) {
	resolved, err := r.resolveAssistantNode(state, node, action.local, action.feedback, action.artifacts)
	if err != nil {
		return execResult{}, err
	}
	if node.Executor == "external" {
		result, err := r.executeExternalNode(state, node, resolved)
		return normalizeStructuredResult(result, err, node.OutputFormat, "validate structured output")
	}
	idleTimeout := node.IdleTimeout
	if idleTimeout == "" && r.assistantIdleTimeout > 0 {
		idleTimeout = r.assistantIdleTimeout.String()
	}
	idle, idleErr := newIdleMonitor(ctx, idleTimeout)
	if idleErr != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "node idle timeout", Err: idleErr}
	}
	defer idle.Close()
	ctx = idle.Context()
	resolver := r.assistants
	if resolver == nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: fmt.Errorf("assistant resolver dependency is required")}
	}
	adapter, err := resolver.Resolve(resolved.AssistantName)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: err}
	}
	collector := &assistantEventCollector{onEvent: idle.Touch, observe: func(event assistant.Event) {
		if r.assistantEvents != nil {
			r.assistantEvents(state.ID, node.ID, redactAssistantEvent(r.redactor, event))
		}
	}}
	sessionEvent := assistant.EventSessionStarted
	if resolved.SessionMode == "resume" && resolved.SessionID != "" {
		sessionEvent = assistant.EventSessionResumed
	}
	collector.Emit(assistant.Event{Type: sessionEvent, Provider: resolved.Model.Provider, SessionID: resolved.SessionID, Data: map[string]any{
		"assistant": resolved.AssistantName, "attempt": state.Nodes[node.ID].Attempts, "session_mode": resolved.SessionMode,
		"model_name": resolved.ModelName, "model_id": resolved.Model.ID, "idle_timeout": idleTimeout,
	}})
	request := assistant.Request{
		RunID: state.ID, NodeID: node.ID, Attempt: state.Nodes[node.ID].Attempts,
		Prompt: resolved.Prompt, Workspace: r.workspace, ModelName: resolved.ModelName, Model: resolved.Model,
		SessionMode: resolved.SessionMode, SessionID: resolved.SessionID, NativeHooks: node.NativeHooks, Policy: resolved.Policy,
		Emit: collector.Emit,
		Activity: func(kind string) {
			idle.Touch()
			if r.assistantActivity != nil {
				r.assistantActivity(state.ID, node.ID, kind)
			}
		},
	}
	if r.redactor == nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant secrets", Err: fmt.Errorf("redactor dependency is required")}
	}
	if r.config != nil {
		for _, assistantSpec := range r.config.Assistants {
			assistant.RegisterRenderedEnvSecrets(r.redactor, assistantSpec, request)
		}
	}
	result, err := adapter.Run(ctx, request)
	if err == nil && resolved.SessionMode == "resume" && resolved.SessionID != "" && result.SessionID != resolved.SessionID {
		err = &execution.Error{Kind: execution.KindProtocol, Op: "assistant resume", Err: fmt.Errorf("assistant returned session %q, requested %q", result.SessionID, resolved.SessionID)}
	}
	if errors.Is(context.Cause(ctx), ErrIdleTimeout) {
		err = &execution.Error{Kind: execution.KindTimedOut, ExitCode: -1, Op: "assistant idle timeout", Err: ErrIdleTimeout}
	}
	events, eventErr := collectAssistantResultEvents(collector, resolved, result, err)
	if eventErr != nil && err == nil {
		err = eventErr
	}
	executed := execResult{
		Output: result.Output, Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, SessionID: result.SessionID,
		Resumed: result.Resumed, Truncated: result.Truncated, Usage: result.Usage,
		Assistant:        resolved.AssistantName,
		AssistantVersion: result.AssistantVersion,
		AssistantEvents:  events,
		RequestedModel:   &store.ModelRef{Name: resolved.ModelName, Provider: resolved.Model.Provider, ID: resolved.Model.ID, Params: cloneParams(resolved.Model.Params)},
	}
	if result.ResolvedModel != nil {
		executed.ResolvedModel = &store.ModelRef{Name: result.ResolvedModel.Name, Provider: result.ResolvedModel.Provider, ID: result.ResolvedModel.ID, Params: cloneParams(result.ResolvedModel.Params)}
	}
	return normalizeStructuredResult(executed, err, node.OutputFormat, "validate structured output")
}

func normalizeStructuredResult(result execResult, execErr error, schema *spec.OutputFormat, op string) (execResult, error) {
	if execErr != nil || schema == nil {
		return result, execErr
	}
	normalized, err := validateAndNormalizeOutput(result.Output, schema)
	if err != nil {
		return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: result.ExitCode, Op: op, Err: err}
	}
	result.Output = normalized
	return result, nil
}
