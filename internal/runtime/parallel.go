package runtime

import (
	"context"
	"errors"
	"sync"

	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
)

type parallelNodeResult struct {
	node   spec.Node
	result execResult
	err    error
}

func parallelEligible(node spec.Node) bool {
	if node.Command == "" && node.Prompt == "" && node.Bash == "" {
		return false
	}
	if node.Attempts.Max > 1 {
		return false
	}
	return runtimeHookSetEmpty(node.Hooks)
}

func runtimeHookSetEmpty(hooks spec.HookSet) bool {
	return len(hooks.BeforeNode) == 0 && len(hooks.AfterNode) == 0 && len(hooks.BeforeComplete) == 0 && len(hooks.OnFailure) == 0
}

// runParallelWave executes independent, already-runnable leaf actions at the
// same time. State transitions and persistence remain serialized before and
// after the wave, so one Run and one event stream remain authoritative.
func (r *Runner) runParallelWave(ctx context.Context, state *store.RunState, nodes []spec.Node, previous map[string]store.NodeState) error {
	state.CurrentNode = ""
	state.CurrentNodes = make([]string, 0, len(nodes))
	for _, node := range nodes {
		state.CurrentNodes = append(state.CurrentNodes, node.ID)
		ns := state.Nodes[node.ID]
		ns.Attempts++
		ns.Status = store.NodeRunning
		ns.Error, ns.ErrorCode = "", ""
		if err := r.commit(state, "node.started", node.ID, map[string]any{"attempt": ns.Attempts, "parallel": true}); err != nil {
			return err
		}
	}

	results := make(chan parallelNodeResult, len(nodes))
	var wg sync.WaitGroup
	for _, node := range nodes {
		node := node
		wg.Add(1)
		go func() {
			defer wg.Done()
			attemptCtx, cancel, err := nodeContext(ctx, node.Timeout)
			if err != nil {
				results <- parallelNodeResult{node: node, err: &execution.Error{Kind: execution.KindInternal, Op: "node timeout", Err: err}}
				return
			}
			defer cancel()
			result, execErr := r.execute(attemptCtx, state, node, previous)
			if contextErr := attemptContextError(attemptCtx, "node attempt"); contextErr != nil {
				kind := execution.KindOf(execErr)
				if execErr == nil || (kind != execution.KindTimedOut && kind != execution.KindCancelled) {
					execErr = contextErr
				}
			}
			results <- parallelNodeResult{node: node, result: result, err: execErr}
		}()
	}
	wg.Wait()
	close(results)

	byID := make(map[string]parallelNodeResult, len(nodes))
	for result := range results {
		byID[result.node.ID] = result
	}
	cancelled := false
	for _, node := range nodes {
		item := byID[node.ID]
		ns := state.Nodes[node.ID]
		recordExecution(ns, item.result, item.err)
		applyExecResult(ns, item.result)
		accumulateUsage(ns, item.result.Usage)
		if item.err != nil && node.AllowFailure && execution.IsExit(item.err) {
			item.err = nil
			if err := r.commit(state, "node.failure_allowed", node.ID, map[string]any{"exit_code": item.result.ExitCode}); err != nil {
				return err
			}
		}
		if item.err != nil {
			kind := execution.KindOf(item.err)
			if err := r.finishNodeExecutionError(state, node.ID, kind, item.err, item.result); err != nil {
				return err
			}
			if kind == execution.KindCancelled {
				cancelled = true
			}
			continue
		}
		ns.Status = store.NodeCompleted
		if err := r.commit(state, "node.completed", node.ID, map[string]any{"attempts": ns.Attempts, "exit_code": ns.ExitCode, "output_truncated": ns.OutputTruncated, "usage": ns.Usage, "parallel": true}); err != nil {
			return err
		}
	}
	completedIDs := append([]string(nil), state.CurrentNodes...)
	state.CurrentNodes = nil
	if err := r.commit(state, "parallel.wave.completed", "", map[string]any{"nodes": completedIDs}); err != nil {
		return err
	}
	if cancelled || errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return nil
}
