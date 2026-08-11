package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"takt/internal/runcontrol"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

type RunListRequest struct {
	Status        string `json:"status,omitempty"`
	ActiveOnly    bool   `json:"active_only,omitempty"`
	AttentionOnly bool   `json:"attention_only,omitempty"`
	RootOnly      bool   `json:"root_only,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

type AttentionState struct {
	Required bool   `json:"required"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	NodeID   string `json:"node_id,omitempty"`
}

type RunListEntry struct {
	ID             string              `json:"id"`
	Workflow       string              `json:"workflow"`
	Status         string              `json:"status"`
	EffectiveState string              `json:"effective_status"`
	ParentRunID    string              `json:"parent_run_id,omitempty"`
	CurrentNode    string              `json:"current_node,omitempty"`
	CurrentNodes   []string            `json:"current_nodes,omitempty"`
	Attention      AttentionState      `json:"attention"`
	Usage          *store.Usage        `json:"usage,omitempty"`
	ArtifactCount  int                 `json:"artifact_count"`
	ChildRunCount  int                 `json:"child_run_count"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
	PausedAt       *time.Time          `json:"paused_at,omitempty"`
	ErrorCode      string              `json:"error_code,omitempty"`
	Error          string              `json:"error,omitempty"`
	Waiting        *store.WaitingState `json:"waiting,omitempty"`
}

type AttentionItem struct {
	Kind      string        `json:"kind"`
	Run       *RunListEntry `json:"run,omitempty"`
	PlanID    string        `json:"plan_id,omitempty"`
	Status    string        `json:"status,omitempty"`
	Reason    string        `json:"reason"`
	Message   string        `json:"message,omitempty"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type RunSummary struct {
	RunID              string              `json:"run_id"`
	Workflow           string              `json:"workflow"`
	Status             string              `json:"status"`
	EffectiveStatus    string              `json:"effective_status"`
	Attention          AttentionState      `json:"attention"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	DurationSeconds    int64               `json:"duration_seconds"`
	NodesTotal         int                 `json:"nodes_total"`
	NodesCompleted     int                 `json:"nodes_completed"`
	NodesFailed        int                 `json:"nodes_failed"`
	NodesWaiting       int                 `json:"nodes_waiting"`
	ChildRuns          int                 `json:"child_runs"`
	DescendantRuns     int                 `json:"descendant_runs"`
	Usage              *store.Usage        `json:"usage,omitempty"`
	Artifacts          []store.ArtifactRef `json:"artifacts,omitempty"`
	Output             string              `json:"output,omitempty"`
	ErrorCode          string              `json:"error_code,omitempty"`
	Error              string              `json:"error,omitempty"`
	CurrentNode        string              `json:"current_node,omitempty"`
	CurrentNodes       []string            `json:"current_nodes,omitempty"`
	Waiting            *store.WaitingState `json:"waiting,omitempty"`
	RecoveryCount      int                 `json:"recovery_count,omitempty"`
	OperatorRetryCount int                 `json:"operator_retry_count,omitempty"`
}

type PauseResult struct {
	RunID          string   `json:"run_id"`
	Status         string   `json:"status"`
	Requested      bool     `json:"requested"`
	AffectedRunIDs []string `json:"affected_run_ids"`
	SafeBoundary   bool     `json:"safe_boundary"`
}

type RetryRequest struct {
	RunID    string `json:"run_id"`
	NodeID   string `json:"node_id,omitempty"`
	Detached bool   `json:"-"`
}

type RecoverResult struct {
	Recovered []string          `json:"recovered"`
	Skipped   map[string]string `json:"skipped,omitempty"`
}

func (s *RunService) ListRuns(request RunListRequest) ([]RunListEntry, error) {
	st := s.store
	ids, err := st.ListRunIDs()
	if err != nil {
		return nil, err
	}
	entries := make([]RunListEntry, 0, len(ids))
	for _, id := range ids {
		state, loadErr := st.Load(id)
		if errors.Is(loadErr, os.ErrNotExist) {
			// A detached start may have created the Run directory but not yet
			// atomically published state.json. The registry omits that transient
			// entry; malformed published state still fails closed below.
			continue
		}
		if loadErr != nil {
			return nil, fmt.Errorf("load run %s: %w", id, loadErr)
		}
		if request.RootOnly && state.ParentRunID != "" {
			continue
		}
		entry := runListEntry(st, state)
		if request.Status != "" && entry.EffectiveState != request.Status && entry.Status != request.Status {
			continue
		}
		if request.ActiveOnly && terminalRun(entry.Status) && entry.Status != store.RunPaused {
			continue
		}
		if request.AttentionOnly && !entry.Attention.Required {
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Attention.Required != entries[j].Attention.Required {
			return entries[i].Attention.Required
		}
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	if request.Limit > 0 && len(entries) > request.Limit {
		entries = entries[:request.Limit]
	}
	return entries, nil
}

func runListEntry(st RunStore, state *store.RunState) RunListEntry {
	effective := state.Status
	if requested, _ := st.PauseRequested(state.ID); requested && state.Status == store.RunRunning {
		effective = store.RunPausing
	}
	if requested, _, _ := st.AbandonRequested(state.ID); requested && !terminalRun(state.Status) {
		effective = "abandoning"
	}
	workflowName := filepath.Base(state.WorkflowPath)
	if wf, err := workflow.Load(state.WorkflowPath); err == nil && wf.Metadata.Name != "" {
		workflowName = wf.Metadata.Name
	}
	return RunListEntry{
		ID: state.ID, Workflow: workflowName, Status: state.Status, EffectiveState: effective,
		ParentRunID: state.ParentRunID, CurrentNode: state.CurrentNode, CurrentNodes: append([]string(nil), state.CurrentNodes...),
		Attention: attentionForRun(state), Usage: state.Usage, ArtifactCount: len(state.Artifacts), ChildRunCount: len(state.ChildRunIDs),
		CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, PausedAt: state.PausedAt, ErrorCode: state.ErrorCode,
		Error: state.Error, Waiting: cloneWaiting(state.Waiting),
	}
}

func cloneWaiting(value *store.WaitingState) *store.WaitingState {
	if value == nil {
		return nil
	}
	out := *value
	out.ChildRunIDs = append([]string(nil), value.ChildRunIDs...)
	return &out
}

func attentionForRun(state *store.RunState) AttentionState {
	if state == nil {
		return AttentionState{}
	}
	if state.Status == store.RunWaiting && state.Waiting != nil {
		reason := state.Waiting.Kind
		if reason == "" {
			reason = "approval"
		}
		if reason == "child_run" {
			return AttentionState{}
		}
		return AttentionState{Required: true, Reason: reason, Message: state.Waiting.Message, NodeID: state.Waiting.NodeID}
	}
	for nodeID, node := range state.Nodes {
		if node == nil || node.External == nil {
			continue
		}
		for _, call := range node.External.ToolCalls {
			if call != nil && call.ApprovalNeeded && (call.Status == "requested" || call.Status == "waiting") {
				return AttentionState{Required: true, Reason: "tool_approval", Message: call.Tool, NodeID: nodeID}
			}
		}
	}
	switch state.Status {
	case store.RunFailed:
		reason := state.ErrorCode
		if reason == "" {
			reason = "run_failed"
		}
		return AttentionState{Required: true, Reason: reason, Message: state.Error, NodeID: state.CurrentNode}
	case store.RunPaused:
		return AttentionState{Required: true, Reason: "paused", Message: "run is paused by operator"}
	}
	return AttentionState{}
}

func (s *RunService) Attention() ([]AttentionItem, error) {
	runs, err := s.ListRuns(RunListRequest{AttentionOnly: true})
	if err != nil {
		return nil, err
	}
	items := make([]AttentionItem, 0, len(runs))
	for i := range runs {
		run := runs[i]
		items = append(items, AttentionItem{Kind: "run", Run: &run, Status: run.EffectiveState, Reason: run.Attention.Reason, Message: run.Attention.Message, UpdatedAt: run.UpdatedAt})
	}
	if s.planHooks.Attention != nil {
		planItems, hookErr := s.planHooks.Attention()
		if hookErr != nil {
			return nil, hookErr
		}
		items = append(items, planItems...)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	return items, nil
}

func (s *RunService) Summary(runID string, recursive bool) (*RunSummary, error) {
	st := s.store
	root, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	entry := runListEntry(st, root)
	summary := &RunSummary{
		RunID: root.ID, Workflow: entry.Workflow, Status: root.Status, EffectiveStatus: entry.EffectiveState,
		Attention: entry.Attention, CreatedAt: root.CreatedAt, UpdatedAt: root.UpdatedAt,
		DurationSeconds: int64(root.UpdatedAt.Sub(root.CreatedAt).Seconds()), ChildRuns: len(root.ChildRunIDs),
		Usage: cloneUsage(root.Usage), Output: root.Output, ErrorCode: root.ErrorCode, Error: root.Error,
		CurrentNode: root.CurrentNode, CurrentNodes: append([]string(nil), root.CurrentNodes...), Waiting: cloneWaiting(root.Waiting),
		RecoveryCount: root.RecoveryCount, OperatorRetryCount: len(root.OperatorRetries),
	}
	countNodes(summary, root)
	appendUniqueArtifacts(summary, root.Artifacts)
	if recursive {
		seen := map[string]bool{root.ID: true}
		queue := append([]string(nil), root.ChildRunIDs...)
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			child, loadErr := st.Load(id)
			if errors.Is(loadErr, os.ErrNotExist) {
				// Governed child IDs are linked before the child's initial state is
				// atomically published. A recursive summary is a projection and must
				// tolerate that short publication window just like runTreeIDs does.
				continue
			}
			if loadErr != nil {
				return nil, loadErr
			}
			summary.DescendantRuns++
			countNodes(summary, child)
			summary.Usage = addUsage(summary.Usage, child.Usage)
			appendUniqueArtifacts(summary, child.Artifacts)
			queue = append(queue, child.ChildRunIDs...)
		}
	}
	sort.Slice(summary.Artifacts, func(i, j int) bool {
		if summary.Artifacts[i].CreatedAt.Equal(summary.Artifacts[j].CreatedAt) {
			return summary.Artifacts[i].ID < summary.Artifacts[j].ID
		}
		return summary.Artifacts[i].CreatedAt.Before(summary.Artifacts[j].CreatedAt)
	})
	return summary, nil
}

func countNodes(summary *RunSummary, state *store.RunState) {
	for _, node := range state.Nodes {
		if node == nil || node.Hidden || node.PublicParent != "" {
			continue
		}
		summary.NodesTotal++
		switch node.Status {
		case store.NodeCompleted, store.NodeSkipped:
			summary.NodesCompleted++
		case store.NodeFailed, store.NodeErrored, store.NodeBlocked, store.NodeTimedOut, store.NodeCancelled:
			summary.NodesFailed++
		case store.NodeWaiting:
			summary.NodesWaiting++
		}
	}
}

func cloneUsage(value *store.Usage) *store.Usage {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func addUsage(left, right *store.Usage) *store.Usage {
	if left == nil && right == nil {
		return nil
	}
	if left == nil {
		left = &store.Usage{}
	}
	if right != nil {
		left.InputTokens += right.InputTokens
		left.OutputTokens += right.OutputTokens
		left.Cost += right.Cost
	}
	return left
}

func appendUniqueArtifacts(summary *RunSummary, values []store.ArtifactRef) {
	seen := map[string]bool{}
	for _, item := range summary.Artifacts {
		seen[item.ProducerRunID+"\x00"+item.ID] = true
	}
	for _, item := range values {
		key := item.ProducerRunID + "\x00" + item.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		summary.Artifacts = append(summary.Artifacts, item)
	}
}

func (s *RunService) Pause(ctx context.Context, runID string) (*PauseResult, error) {
	st := s.store
	root, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	if terminalRun(root.Status) {
		return nil, fmt.Errorf("cannot pause terminal run %s with status %s", runID, root.Status)
	}
	ids, err := runTreeIDs(st, root)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		state, loadErr := st.Load(id)
		if errors.Is(loadErr, os.ErrNotExist) {
			// The parent can publish a governed child ID before the child state is
			// created. Pre-arm the operator marker so the child pauses before any
			// work if it starts after this request.
			if err := st.RequestPause(id); err != nil {
				return nil, err
			}
			continue
		}
		if loadErr != nil {
			return nil, loadErr
		}
		if terminalRun(state.Status) || state.Status == store.RunPaused {
			continue
		}
		if state.Status == store.RunWaiting {
			release, lockErr := runcontrol.AcquireLock(st, id)
			if lockErr != nil {
				return nil, lockErr
			}
			state, loadErr = st.Load(id)
			if loadErr == nil && state.Status == store.RunWaiting {
				now := time.Now().UTC()
				state.PausedFrom = store.RunWaiting
				state.Status = store.RunPaused
				state.PausedAt = &now
				state.PauseRequested = false
				loadErr = runcontrol.CommitRedacted(s.configPath, st, state, store.Event{Type: "run.paused", Data: map[string]any{"from": store.RunWaiting}})
			}
			_ = release()
			if loadErr != nil {
				return nil, loadErr
			}
			continue
		}
		if err := st.RequestPause(id); err != nil {
			return nil, err
		}
	}
	status := root.Status
	planStatus := "paused"
	if root.Status == store.RunRunning || root.Status == store.RunPausing {
		status = store.RunPausing
		planStatus = "pausing"
	}
	if err := s.setOwningPlanStatus(ctx, runID, planStatus, ""); err != nil {
		return nil, err
	}
	return &PauseResult{RunID: runID, Status: status, Requested: true, AffectedRunIDs: ids, SafeBoundary: true}, nil
}

func (s *RunService) ResumePaused(ctx context.Context, runID string, detached bool) (*store.RunState, error) {
	st := s.store
	state, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	if state.Status != store.RunPaused {
		return nil, fmt.Errorf("run %s is not paused", runID)
	}
	ids, err := runTreeIDs(st, state)
	if err != nil {
		return nil, err
	}
	// Validate first, mutate second. A mistaken resume must never destroy a
	// durable pause request on the run tree.
	for _, id := range ids {
		if err := st.ClearPause(id); err != nil {
			return nil, err
		}
	}
	if state.PausedFrom == store.RunWaiting && state.Waiting != nil && state.Waiting.Kind != "child_run" {
		release, lockErr := runcontrol.AcquireLock(st, state.ID)
		if lockErr != nil {
			return nil, lockErr
		}
		state, err = st.Load(state.ID)
		if err == nil {
			state.Status = store.RunWaiting
			state.PausedAt = nil
			state.PausedFrom = ""
			err = runcontrol.CommitRedacted(s.configPath, st, state, store.Event{Type: "run.resumed", Data: map[string]any{"to": store.RunWaiting}})
		}
		_ = release()
		if err != nil {
			return nil, err
		}
		if err := s.setOwningPlanStatus(ctx, runID, "waiting", ""); err != nil {
			return nil, err
		}
		return runcontrol.DurablePublicRun(st, state)
	}
	if err := s.setOwningPlanStatus(ctx, runID, "running", ""); err != nil {
		return nil, err
	}
	if detached {
		detached := detachedContext(ctx)
		go func() { _, _ = s.Resume(detached, runID) }()
		return runcontrol.DurablePublicRun(st, state)
	}
	return s.Resume(ctx, runID)
}

func (s *RunService) Abandon(ctx context.Context, runID, reason string) (any, error) {
	st := s.store
	root, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	if terminalRun(root.Status) {
		return nil, fmt.Errorf("cannot abandon terminal run %s with status %s", root.ID, root.Status)
	}
	ids, err := runTreeIDs(st, root)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		state, loadErr := st.Load(id)
		if errors.Is(loadErr, os.ErrNotExist) {
			if err := st.RequestAbandon(id, reason); err != nil {
				return nil, err
			}
			continue
		}
		if loadErr != nil {
			return nil, loadErr
		}
		if terminalRun(state.Status) {
			continue
		}
		if err := st.RequestAbandon(id, reason); err != nil {
			return nil, err
		}
		if state.Status == store.RunWaiting || state.Status == store.RunPaused {
			release, lockErr := runcontrol.AcquireLock(st, id)
			if lockErr != nil {
				return nil, lockErr
			}
			state, loadErr = st.Load(id)
			if loadErr == nil {
				runner, runnerErr := s.runnerForState(state)
				if runnerErr == nil {
					_, loadErr = runner.Resume(ctx, state)
				}
			}
			_ = release()
			if loadErr != nil && !errors.Is(loadErr, runtime.ErrAbandoned) {
				return nil, loadErr
			}
		}
	}
	if err := s.setOwningPlanStatus(ctx, runID, "abandoned", strings.TrimSpace(reason)); err != nil {
		return nil, err
	}
	return map[string]any{"run_id": runID, "status": "abandoning", "affected_run_ids": ids}, nil
}

func runTreeIDs(st RunStore, root *store.RunState) ([]string, error) {
	ids := []string{root.ID}
	seen := map[string]bool{root.ID: true}
	queue := append([]string(nil), root.ChildRunIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		child, err := st.Load(id)
		if errors.Is(err, os.ErrNotExist) {
			// A linked child may not have published its initial state yet. Its ID is
			// still part of the tree so operator markers can be pre-armed.
			continue
		}
		if err != nil {
			return nil, err
		}
		queue = append(queue, child.ChildRunIDs...)
	}
	return ids, nil
}

func (s *RunService) Retry(ctx context.Context, request RetryRequest) (*store.RunState, error) {
	st := s.store
	release, err := runcontrol.AcquireLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := st.Load(request.RunID)
	if err != nil {
		return nil, err
	}
	if err := requireCurrentWorkflowContract(state); err != nil {
		return nil, err
	}
	if state.Status != store.RunFailed && state.Status != store.RunCancelled {
		return nil, fmt.Errorf("run %s cannot retry from status %s", state.ID, state.Status)
	}
	if state.Worktree != nil && state.Worktree.Enabled && state.Worktree.Removed {
		return nil, fmt.Errorf("run %s worktree was removed; fork the run instead of retrying in place", state.ID)
	}
	wf, err := workflow.Load(state.WorkflowPath)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(request.NodeID)
	if target == "" {
		target = failedNodeID(state, wf.Nodes)
	}
	if target == "" && state.Status == store.RunCancelled {
		target = firstIncompleteNodeID(state, wf.Nodes)
	}
	if target == "" {
		return nil, fmt.Errorf("run %s has no failed node to retry", state.ID)
	}
	reset := dependentClosure(wf.Nodes, target)
	if len(reset) == 0 {
		return nil, fmt.Errorf("node %q not found in run %s", target, state.ID)
	}
	for _, node := range wf.Nodes {
		if !reset[node.ID] {
			continue
		}
		previous := state.Nodes[node.ID]
		if previous == nil {
			continue
		}
		if node.LoopGroup != nil && len(previous.LoopIterations) >= node.LoopGroup.MaxIterations {
			return nil, fmt.Errorf("run %s loop %q reached max_iterations; fork with a new definition", state.ID, node.ID)
		}
		state.OperatorRetries = append(state.OperatorRetries, store.OperatorRetryState{NodeID: node.ID, RequestedAt: time.Now().UTC(), PreviousStatus: previous.Status, PreviousAttempts: previous.Attempts, PreviousError: previous.Error})
		for _, childID := range append(append([]string(nil), previous.ChildRunIDs...), previous.ChildRunID) {
			if childID != "" {
				if err := st.RequestAbandon(childID, "superseded by operator retry"); err != nil {
					return nil, err
				}
			}
		}
		resetState := &store.NodeState{
			Status:       store.NodePending,
			Hidden:       previous.Hidden,
			PublicParent: previous.PublicParent,
			Executions:   append([]store.ExecutionState(nil), previous.Executions...),
			Artifacts:    append([]store.ArtifactRef(nil), previous.Artifacts...),
		}
		resetState.LoopPrevious = previous.LoopPrevious
		resetState.LoopIterations = previous.LoopIterations
		resetState.LoopIteration = 0
		resetState.SessionID = previous.SessionID
		resetState.Resumed = previous.Resumed
		state.Nodes[node.ID] = resetState
	}
	state.Status = store.RunRunning
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.Waiting = nil
	state.Output = ""
	state.ErrorCode = ""
	state.Error = ""
	state.CancelRequested = false
	state.PauseRequested = false
	state.PausedAt = nil
	state.PausedFrom = ""
	if err := st.ClearCancel(state.ID); err != nil {
		return nil, err
	}
	if err := st.ClearPause(state.ID); err != nil {
		return nil, err
	}
	if err := st.ClearAbandon(state.ID); err != nil {
		return nil, err
	}
	if err := runcontrol.CommitRedacted(s.configPath, st, state, store.Event{Type: "run.retry_requested", NodeID: target, Data: map[string]any{"reset_nodes": sortedKeys(reset)}}); err != nil {
		return nil, err
	}
	if err := s.setOwningPlanStatus(ctx, state.ID, "running", ""); err != nil {
		return nil, err
	}
	runner, err := s.runnerForState(state)
	if err != nil {
		return nil, err
	}
	if request.Detached {
		detached := detachedContext(ctx)
		go func() {
			result, runErr := runner.Resume(detached, state)
			if runErr == nil || errors.Is(runErr, runtime.ErrWaiting) || errors.Is(runErr, runtime.ErrPaused) {
				_, _ = s.resumeParentChain(detached, st, result)
			}
		}()
		return runcontrol.DurablePublicRun(st, state)
	}
	result, runErr := runner.Resume(ctx, state)
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.Is(runErr, runtime.ErrPaused) {
		return nil, runErr
	}
	return runcontrol.DurablePublicRun(st, result)
}

func failedNodeID(state *store.RunState, nodes []spec.Node) string {
	if state.CurrentNode != "" {
		if node := state.Nodes[state.CurrentNode]; node != nil && node.FailedLike() {
			return state.CurrentNode
		}
	}
	for _, node := range nodes {
		if value := state.Nodes[node.ID]; value != nil && value.FailedLike() {
			return node.ID
		}
	}
	return ""
}

func dependentClosure(nodes []spec.Node, target string) map[string]bool {
	known := false
	out := map[string]bool{target: true}
	for _, node := range nodes {
		if node.ID == target {
			known = true
		}
	}
	if !known {
		return map[string]bool{}
	}
	changed := true
	for changed {
		changed = false
		for _, node := range nodes {
			if out[node.ID] {
				continue
			}
			if out[node.Guard] || intersects(node.DependsOn, out) || referencesAny(node.When, out) {
				out[node.ID] = true
				changed = true
			}
		}
	}
	return out
}

func intersects(values []string, set map[string]bool) bool {
	for _, value := range values {
		if set[value] {
			return true
		}
	}
	return false
}

func referencesAny(expression string, set map[string]bool) bool {
	for value := range set {
		if strings.Contains(expression, "nodes."+value+".") {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

type RunForkRequest struct {
	RunID    string `json:"run_id"`
	Input    string `json:"input,omitempty"`
	Detached bool   `json:"-"`
}

func (s *RunService) ForkRun(ctx context.Context, request RunForkRequest) (*StartResult, error) {
	st := s.store
	state, err := st.Load(request.RunID)
	if err != nil {
		return nil, err
	}
	if err := requireCurrentWorkflowContract(state); err != nil {
		return nil, err
	}
	input := state.Input
	if strings.TrimSpace(request.Input) != "" {
		input = request.Input
	}
	worktree := state.RunOptions.WorktreeMode == "enabled"
	var worktreePtr *bool
	if state.RunOptions.WorktreeMode != "auto" {
		worktreePtr = &worktree
	}
	started, err := s.Start(ctx, StartRequest{Selector: state.WorkflowPath, ConfigPath: state.ConfigPath, Input: input, Detached: request.Detached, Worktree: worktreePtr, WorktreeBase: state.RunOptions.WorktreeBase, KeepWorktree: state.RunOptions.KeepWorktree, AllowDirty: state.RunOptions.AllowDirty})
	if err != nil {
		return nil, err
	}
	forked, err := st.Load(started.RunID)
	if err != nil {
		return nil, err
	}
	forked.ForkedFromRunID = state.ID
	forked.ForkSourceFingerprint = runForkFingerprint(state)
	if err := runcontrol.CommitRedacted("", st, forked, store.Event{Type: "run.forked", Data: map[string]any{"source_run_id": state.ID, "source_fingerprint": forked.ForkSourceFingerprint}}); err != nil {
		return nil, err
	}
	started.State = forked.PublicView()
	return started, nil
}

func firstIncompleteNodeID(state *store.RunState, nodes []spec.Node) string {
	for _, node := range nodes {
		value := state.Nodes[node.ID]
		if value == nil || (value.Status != store.NodeCompleted && value.Status != store.NodeSkipped) {
			return node.ID
		}
	}
	return ""
}

func runForkFingerprint(state *store.RunState) string {
	if state == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{state.WorkflowFingerprint, state.ConfigFingerprint, state.CommandsFingerprint, fmt.Sprintf("%d", state.Revision)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *RunService) RecoverInterruptedRuns(ctx context.Context) (*RecoverResult, error) {
	return s.recoverInterruptedRuns(ctx, true)
}

// RecoverInterruptedRunsForeground is used by a one-shot CLI process. It does
// not return until recovered local Runs have reached their next durable boundary;
// otherwise the CLI exit would immediately orphan the freshly recovered worker.
func (s *RunService) RecoverInterruptedRunsForeground(ctx context.Context) (*RecoverResult, error) {
	return s.recoverInterruptedRuns(ctx, false)
}

func (s *RunService) recoverInterruptedRuns(ctx context.Context, detached bool) (*RecoverResult, error) {
	st := s.store
	ids, err := st.ListRunIDs()
	if err != nil {
		return nil, err
	}
	result := &RecoverResult{Skipped: map[string]string{}}
	type candidate struct {
		state *store.RunState
		depth int
	}
	candidates := make([]candidate, 0)
	states := map[string]*store.RunState{}
	for _, id := range ids {
		state, loadErr := st.Load(id)
		if loadErr != nil {
			return nil, loadErr
		}
		states[id] = state
	}
	for _, state := range states {
		if state.Status != store.RunRunning && state.Status != store.RunPausing {
			continue
		}
		if processAlive(state.ExecutorPID) {
			result.Skipped[state.ID] = fmt.Sprintf("executor pid %d is alive", state.ExecutorPID)
			continue
		}
		depth := 0
		for parent := state.ParentRunID; parent != ""; {
			depth++
			value := states[parent]
			if value == nil {
				break
			}
			parent = value.ParentRunID
		}
		candidates = append(candidates, candidate{state: state, depth: depth})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].depth > candidates[j].depth })
	for _, item := range candidates {
		state := item.state
		release, lockErr := runcontrol.AcquireLock(st, state.ID)
		if lockErr != nil {
			result.Skipped[state.ID] = lockErr.Error()
			continue
		}
		current, loadErr := st.Load(state.ID)
		recovered := false
		if loadErr == nil && (current.Status == store.RunRunning || current.Status == store.RunPausing) && !processAlive(current.ExecutorPID) {
			loadErr = s.recoverRunState(st, current)
			if loadErr == nil {
				loadErr = s.setOwningPlanStatus(ctx, current.ID, "running", "")
			}
			if loadErr == nil {
				result.Recovered = append(result.Recovered, current.ID)
				recovered = true
			}
		}
		if releaseErr := release(); loadErr == nil && releaseErr != nil {
			loadErr = releaseErr
		}
		if loadErr != nil {
			return nil, loadErr
		}
		if recovered {
			if detached {
				go s.continueRecoveredRun(current.ID)
			} else {
				if err := s.continueRecoveredRunContext(ctx, current.ID); err != nil {
					return nil, err
				}
			}
		}
	}
	return result, nil
}

func (s *RunService) recoverRunState(st RunStore, state *store.RunState) error {
	now := time.Now().UTC()
	for _, nodeID := range append(append([]string(nil), state.CurrentNodes...), state.CurrentNode) {
		if nodeID == "" {
			continue
		}
		node := state.Nodes[nodeID]
		if node == nil || node.Status != store.NodeRunning {
			continue
		}
		node.Executions = append(node.Executions, store.ExecutionState{Attempt: node.Attempts, Status: store.NodeErrored, ErrorCode: "worker_lost", Error: "executor process ended before node completion"})
		if node.Attempts > 0 {
			node.Attempts--
		}
		node.Status = store.NodePending
		node.ErrorCode = "worker_lost"
		node.Error = "executor process ended before node completion"
		node.External = nil
	}
	state.Status = store.RunRunning
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.ErrorCode = ""
	state.Error = ""
	state.RecoveryCount++
	state.LastRecoveredAt = &now
	state.ExecutorPID = os.Getpid()
	state.HeartbeatAt = &now
	return runcontrol.CommitRedacted(s.configPath, st, state, store.Event{Type: "run.recovered", Data: map[string]any{"recovery_count": state.RecoveryCount, "reason": "executor_lost"}})
}

func (s *RunService) continueRecoveredRun(runID string) {
	_ = s.continueRecoveredRunContext(durableContext(), runID)
}

func (s *RunService) continueRecoveredRunContext(ctx context.Context, runID string) error {
	state, err := s.Resume(ctx, runID)
	if err != nil && !errors.Is(err, runtime.ErrWaiting) && !errors.Is(err, runtime.ErrPaused) {
		return err
	}
	if state != nil {
		_, parentErr := s.resumeParentChain(ctx, s.store, state)
		if parentErr != nil && !errors.Is(parentErr, runtime.ErrWaiting) && !errors.Is(parentErr, runtime.ErrPaused) {
			return parentErr
		}
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func (s *RunService) setOwningPlanStatus(ctx context.Context, runID, status, lastError string) error {
	if s.planHooks.SetOwningRunStatus == nil {
		return nil
	}
	return s.planHooks.SetOwningRunStatus(ctx, runID, status, lastError)
}
