package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
)

type attemptDisposition uint8

const (
	attemptDone attemptDisposition = iota
	attemptRetry
)

func (r *Runner) runAttempt(ctx context.Context, state *store.RunState, node spec.Node, hooks spec.HookSet, loopPrevious map[string]store.NodeState, max int) (attemptDisposition, error) {
	ns := state.Nodes[node.ID]
	ns.Attempts++
	ns.Status = store.NodeRunning
	ns.Error, ns.ErrorCode = "", ""
	state.CurrentNode = node.ID
	state.CurrentNodes = nil
	if err := r.commit(state, "node.started", node.ID, map[string]any{"attempt": ns.Attempts}); err != nil {
		return attemptDone, err
	}

	attemptCtx, cancel, err := nodeContext(ctx, node.Timeout)
	if err != nil {
		return attemptDone, r.finishNodeError(state, node.ID, "invalid_timeout", err, execResult{})
	}
	attemptCtx, cancelWatch := r.watchCancellation(attemptCtx, state.ID)
	originalCancel := cancel
	cancel = func() { cancelWatch(); originalCancel() }
	defer cancel()

	retry, err := r.runBeforeAttemptHooks(attemptCtx, state, node, hooks.BeforeNode, loopPrevious)
	if err != nil || retry {
		return disposition(retry), err
	}

	result, execErr, err := r.executeAttempt(attemptCtx, state, node, loopPrevious, max)
	if err != nil {
		return attemptDone, err
	}
	if execErr != nil {
		return r.handleFailedAttempt(attemptCtx, state, node, hooks.OnFailure, loopPrevious, result, execErr, max)
	}
	return r.finishSuccessfulAttempt(attemptCtx, state, node, hooks, loopPrevious, result)
}

func disposition(retry bool) attemptDisposition {
	if retry {
		return attemptRetry
	}
	return attemptDone
}

func (r *Runner) runBeforeAttemptHooks(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, loopPrevious map[string]store.NodeState) (bool, error) {
	ns := state.Nodes[node.ID]
	decision, feedback, hookErr := r.runHooks(ctx, state, node, hooks, loopPrevious)
	if hookErr != nil {
		return false, r.finishAttemptExecutionError(state, node.ID, hookErr, execResult{})
	}
	switch decision {
	case "retry":
		ns.Feedback = joinFeedback(ns.Feedback, feedback)
		if err := r.scheduleRetry(state, node, "hook", "before_node", feedback); err != nil {
			return false, err
		}
		return true, nil
	case "fail":
		return false, r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("before_node hook failed: %s", feedback))
	default:
		return false, nil
	}
}

func (r *Runner) executeAttempt(ctx context.Context, state *store.RunState, node spec.Node, loopPrevious map[string]store.NodeState, max int) (execResult, error, error) {
	ns := state.Nodes[node.ID]
	result, execErr := r.execute(ctx, state, node, loopPrevious)
	if errors.Is(execErr, ErrWaiting) {
		ns.Attempts--
		reason := "approval"
		if state.Waiting != nil && state.Waiting.Kind != "" {
			reason = state.Waiting.Kind
		}
		if err := r.commit(state, "node.suspended", node.ID, map[string]any{"reason": reason}); err != nil {
			return result, nil, err
		}
		return result, nil, ErrWaiting
	}
	if errors.Is(execErr, ErrPaused) {
		ns.Attempts--
		ns.Status = store.NodePending
		if err := r.commit(state, "node.suspended", node.ID, map[string]any{"reason": "paused"}); err != nil {
			return result, nil, err
		}
		return result, nil, ErrPaused
	}
	if err := r.flushAssistantEvents(state, node.ID, result.AssistantEvents, "adapter"); err != nil {
		return result, nil, err
	}
	if contextErr := attemptContextError(ctx, "node attempt"); contextErr != nil {
		kind := execution.KindOf(execErr)
		if execErr == nil || (kind != execution.KindTimedOut && kind != execution.KindCancelled) {
			execErr = contextErr
		}
	}
	if execErr == nil && result.Truncated && r.loopPredicateNode(node.ID) {
		execErr = &execution.Error{Kind: execution.KindProtocol, ExitCode: result.ExitCode, Op: "predicate output", Err: fmt.Errorf("node %q output was truncated before predicate evaluation", node.ID)}
	}
	retryable := execErr != nil && shouldRetryAttempt(node.Attempts, execution.KindOf(execErr), ns.Attempts, max)
	recordExecution(ns, result, execErr)
	if execErr != nil {
		d := r.diagnosticFor(string(execution.KindOf(execErr)), execErr, retryable)
		ns.Diagnostic = &d
		if len(ns.Executions) > 0 {
			ns.Executions[len(ns.Executions)-1].Diagnostic = cloneDiagnostic(&d)
		}
	} else {
		ns.Diagnostic = nil
	}
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)
	accumulateUsage(ns, result.Usage)
	if execErr != nil && node.AllowFailure && execution.IsExit(execErr) {
		execErr = nil
		if err := r.commit(state, "node.failure_allowed", node.ID, map[string]any{"exit_code": result.ExitCode}); err != nil {
			return result, nil, err
		}
	}
	return result, execErr, nil
}

func (r *Runner) handleFailedAttempt(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, loopPrevious map[string]store.NodeState, result execResult, execErr error, max int) (attemptDisposition, error) {
	ns := state.Nodes[node.ID]
	kind := execution.KindOf(execErr)
	if isProviderRetry(execErr) && result.ProviderAttempt > 0 {
		if result.ProviderAttempt < providerRetryMax {
			return attemptDone, r.scheduleProviderRetry(state, node.ID, result, execErr)
		}
		if err := r.commit(state, "provider.retry.exhausted", node.ID, map[string]any{"provider_attempt": result.ProviderAttempt}); err != nil {
			return attemptDone, err
		}
		return attemptDone, r.finishNodeExecutionError(state, node.ID, kind, execErr, result)
	}
	if shouldRetryAttempt(node.Attempts, kind, ns.Attempts, max) {
		ns.Feedback = joinFeedback(ns.Feedback, execErr.Error())
		if node.Attempts.RetrySession == "fresh" {
			ns.SessionID = ""
		}
		if err := r.scheduleRetry(state, node, string(kind), "attempts", ns.Feedback); err != nil {
			return attemptDone, err
		}
		return attemptRetry, nil
	}
	if kind == execution.KindCancelled || kind == execution.KindTimedOut {
		return attemptDone, r.finishAttemptExecutionError(state, node.ID, execErr, result)
	}
	decision, feedback, hookErr := r.runHooks(ctx, state, node, hooks, loopPrevious)
	if hookErr != nil {
		return attemptDone, r.finishAttemptExecutionError(state, node.ID, hookErr, result)
	}
	if decision == "retry" {
		ns.Feedback = joinFeedback(ns.Feedback, feedback, execErr.Error())
		if err := r.scheduleRetry(state, node, "hook", "on_failure", ns.Feedback); err != nil {
			return attemptDone, err
		}
		return attemptRetry, nil
	}
	if decision == "fail" && feedback != "" {
		ns.Feedback = joinFeedback(ns.Feedback, feedback)
	}
	if err := r.finishNodeExecutionError(state, node.ID, kind, execErr, result); err != nil {
		return attemptDone, err
	}
	return attemptDone, nil
}

func isProviderRetry(err error) bool {
	return execution.KindOf(err) == execution.KindProviderUnavailable
}

func providerRetryAfter(err error) time.Duration {
	var value *execution.Error
	if errors.As(err, &value) {
		return value.RetryAfter
	}
	return 0
}

func (r *Runner) scheduleProviderRetry(state *store.RunState, nodeID string, result execResult, err error) error {
	ns := state.Nodes[nodeID]
	if result.SessionID == "" {
		return r.finishNodeExecutionError(state, nodeID, execution.KindProtocol, &execution.Error{Kind: execution.KindProtocol, Op: "provider retry", Err: fmt.Errorf("provider-unavailable result omitted session id")}, result)
	}
	next := result.ProviderAttempt + 1
	delay := providerRetryDelay(result.ProviderAttempt, providerRetryAfter(err))
	notBefore := time.Now().UTC().Add(delay)
	diagnostic := r.diagnosticFor(string(execution.KindProviderUnavailable), err, true)
	ns.Diagnostic = &diagnostic
	if len(ns.Executions) > 0 {
		ns.Executions[len(ns.Executions)-1].Diagnostic = cloneDiagnostic(&diagnostic)
	}
	ns.Status = store.NodePending
	ns.Error, ns.ErrorCode = "", ""
	ns.Retry = &store.RetryState{Scope: "provider", ProviderAttempt: next, NextAttempt: ns.Attempts, NotBefore: notBefore, Delay: delay.String(), Kind: string(execution.KindProviderUnavailable), Fingerprint: diagnostic.Fingerprint}
	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "provider.retry.scheduled", nodeID, map[string]any{"provider_attempt": next, "not_before": notBefore, "delay": delay.String(), "fingerprint": diagnostic.Fingerprint})
}

func (r *Runner) runProviderExecution(ctx context.Context, state *store.RunState, node spec.Node, hooks spec.HookSet, loopPrevious map[string]store.NodeState, max int) error {
	ns := state.Nodes[node.ID]
	if err := r.awaitRetry(ctx, state, node.ID); err != nil {
		return err
	}
	attemptCtx, cancel, err := nodeContext(ctx, node.Timeout)
	if err != nil {
		return r.finishNodeError(state, node.ID, "invalid_timeout", err, execResult{})
	}
	attemptCtx, cancelWatch := r.watchCancellation(attemptCtx, state.ID)
	defer func() { cancelWatch(); cancel() }()
	result, execErr := r.execute(attemptCtx, state, node, loopPrevious)
	if err := r.flushAssistantEvents(state, node.ID, result.AssistantEvents, "adapter"); err != nil {
		return err
	}
	if contextErr := attemptContextError(attemptCtx, "provider retry"); contextErr != nil {
		kind := execution.KindOf(execErr)
		if execErr == nil || (kind != execution.KindTimedOut && kind != execution.KindCancelled) {
			execErr = contextErr
		}
	}
	recordExecution(ns, result, execErr)
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)
	accumulateUsage(ns, result.Usage)
	if isProviderRetry(execErr) && result.ProviderAttempt < providerRetryMax {
		return r.scheduleProviderRetry(state, node.ID, result, execErr)
	}
	if isProviderRetry(execErr) {
		ns.Retry = nil
		if err := r.commit(state, "provider.retry.exhausted", node.ID, map[string]any{"provider_attempt": result.ProviderAttempt}); err != nil {
			return err
		}
		return r.finishNodeExecutionError(state, node.ID, execution.KindProviderUnavailable, execErr, result)
	}
	if execErr != nil {
		ns.Retry = nil
		return r.handleProviderFailure(ctx, state, node, hooks.OnFailure, loopPrevious, result, execErr, max)
	}
	ns.Retry = nil
	_, err = r.finishSuccessfulAttempt(attemptCtx, state, node, hooks, loopPrevious, result)
	return err
}

func (r *Runner) handleProviderFailure(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, loopPrevious map[string]store.NodeState, result execResult, execErr error, max int) error {
	_, err := r.handleFailedAttempt(ctx, state, node, hooks, loopPrevious, result, execErr, max)
	return err
}

func (r *Runner) finishSuccessfulAttempt(ctx context.Context, state *store.RunState, node spec.Node, hooks spec.HookSet, loopPrevious map[string]store.NodeState, result execResult) (attemptDisposition, error) {
	ns := state.Nodes[node.ID]
	for _, phase := range []struct {
		name  string
		hooks []spec.HookSpec
	}{
		{name: "after_node", hooks: hooks.AfterNode},
		{name: "before_complete", hooks: hooks.BeforeComplete},
	} {
		decision, feedback, hookErr := r.runHooks(ctx, state, node, phase.hooks, loopPrevious)
		if hookErr != nil {
			return attemptDone, r.finishAttemptExecutionError(state, node.ID, hookErr, result)
		}
		if decision == "retry" {
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			if err := r.scheduleRetry(state, node, "hook", phase.name, feedback); err != nil {
				return attemptDone, err
			}
			return attemptRetry, nil
		}
		if decision == "fail" {
			return attemptDone, r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("%s hook failed: %s", phase.name, feedback))
		}
	}
	if err := attemptContextError(ctx, "node attempt"); err != nil {
		return attemptDone, r.finishAttemptExecutionError(state, node.ID, err, result)
	}
	if err := r.captureDeclaredArtifact(state, node, loopPrevious); err != nil {
		return attemptDone, r.finishNodeError(state, node.ID, "artifact", fmt.Errorf("persist typed artifact: %w", err), result)
	}
	ns.Status = store.NodeCompleted
	ns.Diagnostic = nil
	ns.Retry = nil
	state.CurrentNode = ""
	state.CurrentNodes = nil
	if err := r.commit(state, "node.completed", node.ID, map[string]any{"attempts": ns.Attempts, "exit_code": ns.ExitCode, "output_truncated": ns.OutputTruncated, "usage": ns.Usage, "artifacts": ns.Artifacts}); err != nil {
		return attemptDone, err
	}
	return attemptDone, nil
}
