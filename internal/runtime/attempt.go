package runtime

import (
	"context"
	"errors"
	"fmt"

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
		return false, r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("before_node hook failed: %s", feedback), execResult{})
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
			return attemptDone, r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("%s hook failed: %s", phase.name, feedback), result)
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
