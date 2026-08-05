package runtime

import (
	"context"
	"fmt"
	"time"

	"takt/internal/store"
)

type cancellationStore interface {
	RequestCancel(string) error
	CancelRequested(string) (bool, error)
	ClearCancel(string) error
}

func (r *Runner) cancellationRequested(runID string) bool {
	value, ok := r.Store.(cancellationStore)
	if !ok {
		return false
	}
	requested, err := value.CancelRequested(runID)
	return err == nil && requested
}

func (r *Runner) watchCancellation(parent context.Context, runID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	value, ok := r.Store.(cancellationStore)
	if !ok {
		return ctx, cancel
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				requested, err := value.CancelRequested(runID)
				if err == nil && requested {
					cancel()
					return
				}
			}
		}
	}()
	return ctx, cancel
}

func (r *Runner) Cancel(state *store.RunState, reason string) (*store.RunState, error) {
	if terminalRunStatus(state.Status) {
		return state, fmt.Errorf("cannot cancel terminal run %s with status %s", state.ID, state.Status)
	}
	return r.cancelState(state, reason)
}

func (r *Runner) cancelState(state *store.RunState, reason string) (*store.RunState, error) {
	if reason == "" {
		reason = "cancel_requested"
	}
	state.CancelRequested = true
	state.Status = store.RunCancelled
	state.Waiting = nil
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.ErrorCode = "cancelled"
	state.Error = reason
	for _, node := range state.Nodes {
		if node != nil && !node.Terminal() {
			node.Status = store.NodeCancelled
			node.ErrorCode = "cancelled"
			node.Error = reason
		}
	}
	if value, ok := r.Store.(cancellationStore); ok {
		for _, childID := range state.ChildRunIDs {
			child, loadErr := r.Store.Load(childID)
			if loadErr == nil && (child.Status == store.RunCompleted || child.Status == store.RunCancelled || child.Status == store.RunFailed) {
				continue
			}
			_ = value.RequestCancel(childID)
		}
	}
	if err := r.finalizeWorktree(state, store.RunCancelled); err != nil {
		return state, err
	}
	if err := r.commit(state, "run.cancelled", "", map[string]any{"reason": reason, "children": state.ChildRunIDs}); err != nil {
		return state, err
	}
	if value, ok := r.Store.(cancellationStore); ok {
		_ = value.ClearCancel(state.ID)
	}
	return state, context.Canceled
}

func RequestCancellation(repository store.Repository, runID string) error {
	value, ok := repository.(cancellationStore)
	if !ok {
		return fmt.Errorf("run store does not support cancellation")
	}
	return value.RequestCancel(runID)
}
