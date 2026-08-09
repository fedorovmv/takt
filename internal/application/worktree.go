package application

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"takt/internal/gitworktree"
	"takt/internal/runcontrol"
	"takt/internal/store"
)

type WorktreeListEntry struct {
	RunID              string `json:"run_id"`
	RunStatus          string `json:"run_status"`
	Path               string `json:"path"`
	Branch             string `json:"branch,omitempty"`
	BaseCommit         string `json:"base_commit,omitempty"`
	Dirty              bool   `json:"dirty,omitempty"`
	Removed            bool   `json:"removed,omitempty"`
	RetainedReason     string `json:"retained_reason,omitempty"`
	ExecutionWorkspace string `json:"execution_workspace,omitempty"`
}

type WorktreeService struct {
	workspace string
	store     WorktreeStore
}

func (s *WorktreeService) List(ctx context.Context) ([]WorktreeListEntry, error) {
	runsDir := filepath.Join(s.workspace, ".takt", "runs")
	entries, err := os.ReadDir(runsDir)
	if errors.Is(err, os.ErrNotExist) {
		return []WorktreeListEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result []WorktreeListEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		state, loadErr := s.store.Load(entry.Name())
		if loadErr != nil {
			return nil, loadErr
		}
		if state.Worktree == nil || !state.Worktree.Enabled {
			continue
		}
		wt := state.Worktree
		if !wt.Removed {
			if status, inspectErr := gitworktree.Inspect(ctx, wt.Path); inspectErr == nil {
				wt.Dirty = status.Dirty
			}
		}
		result = append(result, WorktreeListEntry{
			RunID: state.ID, RunStatus: state.Status, Path: wt.Path, Branch: wt.Branch,
			BaseCommit: wt.BaseCommit, Dirty: wt.Dirty, Removed: wt.Removed,
			RetainedReason: wt.RetainedReason, ExecutionWorkspace: wt.ExecutionWorkspace,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RunID < result[j].RunID })
	return result, nil
}

func (s *WorktreeService) Remove(ctx context.Context, runID string, force bool) (*store.RunState, error) {
	release, err := runcontrol.AcquireLock(s.store, runID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := s.store.Load(runID)
	if err != nil {
		return nil, err
	}
	if state.Status == store.RunRunning || state.Status == store.RunWaiting {
		return nil, fmt.Errorf("cannot remove worktree for active run %s with status %s", state.ID, state.Status)
	}
	wt := state.Worktree
	if wt == nil || !wt.Enabled {
		return nil, fmt.Errorf("run %s has no managed worktree", state.ID)
	}
	if wt.Removed {
		return state.PublicView(), nil
	}
	if status, inspectErr := gitworktree.Inspect(ctx, wt.Path); inspectErr == nil {
		wt.Dirty = status.Dirty
	}
	if wt.Dirty && !force {
		return nil, fmt.Errorf("worktree %s has uncommitted changes; inspect it or pass --force", wt.Path)
	}
	if err := gitworktree.Remove(ctx, wt.RepositoryRoot, wt.Path, force); err != nil {
		return nil, err
	}
	wt.Removed = true
	wt.RemovedAt = time.Now().UTC()
	wt.RetainedReason = ""
	wt.CleanupError = ""
	branchRemoved, branchErr := gitworktree.DeleteBranchIfUnchanged(ctx, wt.RepositoryRoot, wt.Branch, wt.BaseCommit)
	wt.BranchRemoved = branchRemoved
	if branchErr != nil {
		wt.BranchCleanupError = branchErr.Error()
	}
	if err := runcontrol.CommitRedacted("", s.store, state, store.Event{Type: "worktree.removed", Data: map[string]any{
		"path": wt.Path, "branch": wt.Branch, "manual": true, "force": force,
		"branch_removed": branchRemoved, "branch_cleanup_error": wt.BranchCleanupError,
	}}); err != nil {
		return nil, err
	}
	return state.PublicView(), nil
}

func (s *WorktreeService) Prune(ctx context.Context) error {
	return gitworktree.Prune(ctx, s.workspace)
}
