package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"takt/internal/store"
)

type cancellationStore interface {
	RequestCancel(string) error
	CancelRequested(string) (bool, error)
	ClearCancel(string) error
}

type operatorStore interface {
	RequestPause(string) error
	PauseRequested(string) (bool, error)
	ClearPause(string) error
	RequestAbandon(string, string) error
	AbandonRequested(string) (bool, string, error)
	ClearAbandon(string) error
}

func (r *Runner) cancellationRequested(runID string) bool {
	value, ok := r.store.(cancellationStore)
	if !ok {
		return false
	}
	requested, err := value.CancelRequested(runID)
	return err == nil && requested
}

func (r *Runner) pauseRequested(runID string) bool {
	value, ok := r.store.(operatorStore)
	if !ok {
		return false
	}
	requested, err := value.PauseRequested(runID)
	return err == nil && requested
}

func (r *Runner) abandonmentRequested(runID string) (bool, string) {
	value, ok := r.store.(operatorStore)
	if !ok {
		return false, ""
	}
	requested, reason, err := value.AbandonRequested(runID)
	if err != nil {
		return false, ""
	}
	return requested, reason
}

func (r *Runner) watchCancellation(parent context.Context, runID string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	value, ok := r.store.(cancellationStore)
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
				if requested, _ := r.abandonmentRequested(runID); requested {
					cancel()
					return
				}
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

func (r *Runner) abandonState(state *store.RunState, reason string) (*store.RunState, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "abandoned by operator"
	}
	now := time.Now().UTC()
	state.CancelRequested = false
	state.PauseRequested = false
	state.Status = store.RunAbandoned
	state.Waiting = nil
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.ErrorCode = "abandoned"
	state.Error = reason
	state.AbandonedAt = &now
	state.AbandonReason = reason
	for _, node := range state.Nodes {
		if node != nil && !node.Terminal() {
			node.Status = store.NodeCancelled
			node.ErrorCode = "abandoned"
			node.Error = reason
		}
	}
	if value, ok := r.store.(cancellationStore); ok {
		for _, childID := range state.ChildRunIDs {
			child, loadErr := r.store.Load(childID)
			if loadErr == nil && terminalRunStatus(child.Status) {
				continue
			}
			if operator, ok := r.store.(operatorStore); ok {
				_ = operator.RequestAbandon(childID, reason)
			} else {
				_ = value.RequestCancel(childID)
			}
		}
	}
	if err := r.finalizeWorktree(state, store.RunAbandoned); err != nil {
		return state, err
	}
	if err := r.commit(state, "run.abandoned", "", map[string]any{"reason": reason, "children": state.ChildRunIDs}); err != nil {
		return state, err
	}
	if value, ok := r.store.(operatorStore); ok {
		if err := value.ClearAbandon(state.ID); err != nil {
			return state, err
		}
		if err := value.ClearPause(state.ID); err != nil {
			return state, err
		}
	}
	if value, ok := r.store.(cancellationStore); ok {
		if err := value.ClearCancel(state.ID); err != nil {
			return state, err
		}
	}
	return state, ErrAbandoned
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
	if value, ok := r.store.(cancellationStore); ok {
		for _, childID := range state.ChildRunIDs {
			child, loadErr := r.store.Load(childID)
			if loadErr == nil && (child.Status == store.RunCompleted || child.Status == store.RunCancelled || child.Status == store.RunFailed || child.Status == store.RunAbandoned) {
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
	if value, ok := r.store.(cancellationStore); ok {
		if err := value.ClearCancel(state.ID); err != nil {
			return state, err
		}
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
