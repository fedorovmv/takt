package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/authoring"
	cfgpkg "takt/internal/config"
	"takt/internal/profile"
	"takt/internal/runtime"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

type WorkflowListEntry struct {
	Name        string `json:"name"`
	Selector    string `json:"selector"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
}

type WorkflowDescription struct {
	Selector    string           `json:"selector"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Nodes       []map[string]any `json:"nodes"`
}

type StartRequest struct {
	Selector           string `json:"selector"`
	Input              string `json:"input,omitempty"`
	ConfigPath         string `json:"config_path,omitempty"`
	ExecutionWorkspace string `json:"-"`
	Worktree           *bool  `json:"worktree,omitempty"`
	WorktreeBase       string `json:"worktree_base,omitempty"`
	KeepWorktree       bool   `json:"keep_worktree,omitempty"`
	AllowDirty         bool   `json:"allow_dirty_worktree,omitempty"`
	Detached           bool   `json:"detached,omitempty"`
}

type StartResult struct {
	RunID    string          `json:"run_id"`
	Accepted bool            `json:"accepted"`
	State    *store.RunState `json:"state,omitempty"`
}

type ArtifactQuery struct {
	NodeID    string
	Type      string
	Recursive bool
}

type EventsResult struct {
	RunID         string        `json:"run_id"`
	AfterRevision uint64        `json:"after_revision"`
	NextRevision  uint64        `json:"next_revision"`
	Events        []store.Event `json:"events"`
	TimedOut      bool          `json:"timed_out,omitempty"`
}

func (s *CatalogService) ListWorkflows(profileName string) ([]WorkflowListEntry, error) {
	resolved, err := profile.Resolve(profileName, s.workspace)
	if err != nil {
		return nil, err
	}
	entries := make([]WorkflowListEntry, 0, len(resolved.Manifest.Workflows)+1)
	defaultWorkflow, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		return nil, err
	}
	entries = append(entries, WorkflowListEntry{
		Name: defaultWorkflow.Metadata.Name, Selector: resolved.Name,
		Description: defaultWorkflow.Metadata.Description, Default: true,
	})
	names := make([]string, 0, len(resolved.Manifest.Workflows))
	for name := range resolved.Manifest.Workflows {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		selected, err := resolved.SelectWorkflow(name)
		if err != nil {
			return nil, err
		}
		wf, err := workflow.Load(selected.WorkflowPath)
		if err != nil {
			return nil, fmt.Errorf("profile workflow %q: %w", name, err)
		}
		entries = append(entries, WorkflowListEntry{Name: name, Selector: resolved.Name + ":" + name, Description: wf.Metadata.Description})
	}
	return entries, nil
}

func (s *CatalogService) DescribeWorkflow(selector string) (*WorkflowDescription, error) {
	resolved, err := profile.Resolve(selector, s.workspace)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Load(resolved.WorkflowPath)
	if err != nil {
		return nil, err
	}
	selected := resolved.Name
	if resolved.WorkflowName != "" {
		selected += ":" + resolved.WorkflowName
	}
	nodes := make([]map[string]any, 0)
	for _, node := range wf.Nodes {
		if node.Hidden || node.PublicParent != "" {
			continue
		}
		nodes = append(nodes, map[string]any{
			"id": node.ID, "depends_on": node.DependsOn, "when": node.When,
			"trigger_rule": node.TriggerRule, "kind": nodeKind(node),
		})
	}
	return &WorkflowDescription{Selector: selected, Name: wf.Metadata.Name, Description: wf.Metadata.Description, Nodes: nodes}, nil
}

func nodeKind(node spec.Node) string {
	switch {
	case node.Command != "":
		return "command"
	case node.Prompt != "":
		return "prompt"
	case node.Bash != "":
		return "bash"
	case node.Script != nil:
		return "script"
	case node.Approval != nil:
		return "approval"
	case node.LoopGroup != nil:
		return "loop_group"
	case node.WorkflowRun != nil:
		return "workflow"
	case node.Internal != nil:
		return "internal"
	default:
		return "unknown"
	}
}

func (s *RunService) Start(ctx context.Context, request StartRequest) (*StartResult, error) {
	prepared, err := s.prepareStart(ctx, request)
	if err != nil {
		return nil, err
	}
	if !request.Detached {
		state, runErr := prepared.runner.StartWithOptions(ctx, prepared.input, prepared.options)
		if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.Is(runErr, runtime.ErrPaused) {
			return nil, runErr
		}
		public, err := durablePublicRun(s.store, state)
		if err != nil {
			return nil, err
		}
		return &StartResult{RunID: state.ID, Accepted: true, State: public}, nil
	}

	runID := prepared.options.RunID
	result := make(chan startOutcome, 1)
	go func() {
		state, runErr := prepared.runner.StartWithOptions(detachedContext(ctx), prepared.input, prepared.options)
		if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.Is(runErr, runtime.ErrPaused) {
			s.setLaunchError(runID, runErr)
		}
		result <- startOutcome{state: state, err: runErr}
	}()

	runStore := s.store
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case outcome := <-result:
			// Detached execution is an accepted durable operation once a Run state
			// exists. Even an immediate terminal failure is observed through run.get,
			// attention and notifications instead of racing the start RPC response.
			if outcome.state != nil {
				public, loadErr := durablePublicRun(runStore, outcome.state)
				if loadErr != nil {
					return nil, loadErr
				}
				return &StartResult{RunID: runID, Accepted: true, State: public}, nil
			}
			if outcome.err != nil && !errors.Is(outcome.err, runtime.ErrWaiting) && !errors.Is(outcome.err, runtime.ErrPaused) {
				return nil, outcome.err
			}
			return &StartResult{RunID: runID, Accepted: true}, nil
		case <-ticker.C:
			state, loadErr := runStore.Load(runID)
			if loadErr == nil {
				return &StartResult{RunID: runID, Accepted: true, State: state.PublicView()}, nil
			}
			if !errors.Is(loadErr, os.ErrNotExist) {
				return nil, loadErr
			}
		case <-deadline.C:
			return &StartResult{RunID: runID, Accepted: true}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type preparedStart struct {
	runner  *runtime.Runner
	input   string
	options runtime.StartOptions
}

type startOutcome struct {
	state *store.RunState
	err   error
}

func (s *RunService) prepareStart(ctx context.Context, request StartRequest) (*preparedStart, error) {
	selector := strings.TrimSpace(request.Selector)
	if selector == "" {
		return nil, fmt.Errorf("selector is required")
	}
	wfPath, cfgPath, resolved, err := resolveWorkflow(s.workspace, s.configPath, selector, request.ConfigPath)
	if err != nil {
		return nil, err
	}
	wf, err := workflow.Load(wfPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if resolved != nil {
		catalog, catalogErr := catalogForResolved(resolved)
		if catalogErr != nil {
			return nil, catalogErr
		}
		if _, preflightErr := preflightCatalogAdapters(ctx, catalog, cfg, s.adapterFactory(cfg)); preflightErr != nil {
			return nil, preflightErr
		}
	}
	runner := s.runnerFactory(runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: wfPath, ConfigPath: cfgPath, ControlWorkspace: s.workspace}, RunnerOptions{})
	if err := workflow.ValidateReferences(wf, cfg, runner.CommandResolver()); err != nil {
		return nil, err
	}
	if err := runtime.ValidateCapabilities(wf, cfg, wfPath, runner.CommandResolver()); err != nil {
		return nil, fmt.Errorf("capability validation: %w", err)
	}
	diagnostics := authoring.Analyze(wf, runner.CommandResolver())
	if authoring.HasErrors(diagnostics) {
		return nil, &authoring.Error{Diagnostics: diagnostics}
	}
	input := request.Input
	inputCandidate := request.Input
	if request.Input != "" && !filepath.IsAbs(inputCandidate) {
		inputCandidate = filepath.Join(s.workspace, inputCandidate)
	}
	if resolved != nil {
		input, err = profile.PrepareInput(resolved.EffectiveInput(), inputCandidate)
	} else if raw, readErr := os.ReadFile(inputCandidate); request.Input != "" && readErr == nil {
		input = string(raw)
	}
	if err != nil {
		return nil, err
	}
	input, err = runtime.ValidateWorkflowInput(input, wf.Input)
	if err != nil {
		return nil, err
	}
	runID, err := newRunID()
	if err != nil {
		return nil, err
	}
	return &preparedStart{runner: runner, input: input, options: runtime.StartOptions{
		RunID: runID, ExecutionWorkspace: request.ExecutionWorkspace, Worktree: request.Worktree, WorktreeBase: request.WorktreeBase,
		KeepWorktree: request.KeepWorktree, AllowDirty: request.AllowDirty,
	}}, nil
}

func resolveWorkflow(workspace, defaultConfigPath, value, configOverride string) (string, string, *profile.Resolved, error) {
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspace, candidate)
	}
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		wfPath, err := filepath.Abs(candidate)
		if err != nil {
			return "", "", nil, err
		}
		cfgPath := defaultConfigPath
		if configOverride != "" {
			cfgPath, err = absoluteFromWorkspace(workspace, configOverride)
		}
		return wfPath, cfgPath, nil, err
	}
	resolved, err := profile.Resolve(value, workspace)
	if err != nil {
		return "", "", nil, err
	}
	cfgPath := resolved.ConfigPath
	if configOverride != "" {
		cfgPath, err = absoluteFromWorkspace(workspace, configOverride)
		if err != nil {
			return "", "", nil, err
		}
	}
	return resolved.WorkflowPath, cfgPath, resolved, nil
}

func absoluteFromWorkspace(workspace, value string) (string, error) {
	if !filepath.IsAbs(value) {
		value = filepath.Join(workspace, value)
	}
	return filepath.Abs(value)
}

func newRunID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(raw[:]), nil
}

func durablePublicRun(st interface {
	Load(string) (*store.RunState, error)
}, state *store.RunState) (*store.RunState, error) {
	if state == nil {
		return nil, nil
	}
	persisted, err := st.Load(state.ID)
	if err != nil {
		return nil, err
	}
	return persisted.PublicView(), nil
}

func (s *RunService) GetRun(runID string) (*store.RunState, error) {
	state, err := (s.store).Load(runID)
	if err != nil {
		if launchErr := s.launchError(runID); launchErr != nil {
			return nil, launchErr
		}
		return nil, err
	}
	return state.PublicView(), nil
}

func (s *RunService) Resume(ctx context.Context, runID string) (*store.RunState, error) {
	st := s.store
	release, err := acquireRunLock(st, runID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	runner, err := s.runnerForState(state)
	if err != nil {
		return nil, err
	}
	state, runErr := runner.Resume(ctx, state)
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.Is(runErr, runtime.ErrPaused) {
		return nil, runErr
	}
	return durablePublicRun(st, state)
}

func (s *RunService) Answer(ctx context.Context, runID, requestedNodeID, value string) (*store.RunState, error) {
	st := s.store
	target, nodeID, err := resolveApprovalTarget(st, runID, requestedNodeID)
	if err != nil {
		return nil, err
	}
	release, err := acquireRunLock(st, target.ID)
	if err != nil {
		return nil, err
	}
	target, err = st.Load(target.ID)
	if err != nil {
		_ = release()
		return nil, err
	}
	runner, err := s.runnerForState(target)
	if err != nil {
		_ = release()
		return nil, err
	}
	if err := runner.VerifyDefinitions(target); err != nil {
		_ = release()
		return nil, err
	}
	if target.Waiting == nil || target.Waiting.Kind == "child_run" {
		_ = release()
		return nil, fmt.Errorf("run %s is not waiting for an approval", target.ID)
	}
	if target.Approvals == nil {
		target.Approvals = map[string]string{}
	}
	target.Approvals[nodeID] = value
	if ns := target.Nodes[nodeID]; ns != nil {
		ns.Status = store.NodePending
	}
	target.Status = store.RunRunning
	target.Waiting = nil
	if err := commitRedacted(s.configPath, st, target, store.Event{Type: "approval.answered", NodeID: nodeID, Data: map[string]any{"value_captured": true}}); err != nil {
		_ = release()
		return nil, err
	}
	target, runErr := runner.Resume(ctx, target)
	_ = release()
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
		return nil, runErr
	}
	root, cascadeErr := s.resumeParentChain(ctx, st, target)
	if cascadeErr != nil && !errors.Is(cascadeErr, runtime.ErrWaiting) && !errors.Is(cascadeErr, runtime.ErrPaused) {
		return nil, cascadeErr
	}
	return durablePublicRun(st, root)
}

func resolveApprovalTarget(st RunStore, runID, requestedNodeID string) (*store.RunState, string, error) {
	state, err := st.Load(runID)
	if err != nil {
		return nil, "", err
	}
	allowed := map[string]bool{requestedNodeID: false}
	for state.Waiting != nil && state.Waiting.Kind == "child_run" {
		allowed[state.Waiting.NodeID] = true
		if node := state.Nodes[state.Waiting.NodeID]; node != nil && node.PublicParent != "" {
			allowed[node.PublicParent] = true
		}
		childIDs := append([]string(nil), state.Waiting.ChildRunIDs...)
		if len(childIDs) == 0 && state.Waiting.ChildRunID != "" {
			childIDs = []string{state.Waiting.ChildRunID}
		}
		if len(childIDs) != 1 {
			return nil, "", fmt.Errorf("run %s has %d child runs waiting; answer one child run directly: %s", state.ID, len(childIDs), strings.Join(childIDs, ", "))
		}
		state, err = st.Load(childIDs[0])
		if err != nil {
			return nil, "", err
		}
	}
	if state.Waiting == nil {
		return nil, "", fmt.Errorf("run %s is not waiting for approval", state.ID)
	}
	nodeID := state.Waiting.NodeID
	allowed[nodeID] = true
	if node := state.Nodes[nodeID]; node != nil && node.PublicParent != "" {
		allowed[node.PublicParent] = true
	}
	if !allowed[requestedNodeID] {
		return nil, "", fmt.Errorf("run is not waiting for approval node %q", requestedNodeID)
	}
	return state, nodeID, nil
}

func (s *RunService) resumeParentChain(ctx context.Context, st RunStore, child *store.RunState) (*store.RunState, error) {
	current := child
	for current != nil && current.ParentRunID != "" {
		release, err := acquireRunLock(st, current.ParentRunID)
		if err != nil {
			return current, err
		}
		parent, err := st.Load(current.ParentRunID)
		if err != nil {
			_ = release()
			return current, err
		}
		// Answering or completing a child must not implicitly resume an operator-
		// paused parent. Preserve the tree-level pause; explicit ResumePaused will
		// later consume the child_run suspension and continue safely.
		if parent.Status == store.RunPaused {
			_ = release()
			return parent, runtime.ErrPaused
		}
		runner, err := s.runnerForState(parent)
		if err != nil {
			_ = release()
			return current, err
		}
		parent, runErr := runner.Resume(ctx, parent)
		_ = release()
		current = parent
		if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.Is(runErr, runtime.ErrPaused) {
			return current, runErr
		}
	}
	return current, nil
}

func (s *RunService) runnerForState(state *store.RunState) (*runtime.Runner, error) {
	wf, err := workflow.Load(state.WorkflowPath)
	if err != nil {
		return nil, err
	}
	cfg, err := cfgpkg.Load(state.ConfigPath)
	if err != nil {
		return nil, err
	}
	runner := s.runnerFactory(runtime.Definition{Workflow: wf, Config: cfg, WorkflowPath: state.WorkflowPath, ConfigPath: state.ConfigPath, ControlWorkspace: state.Workspace}, RunnerOptions{})
	runner.SetStartOptions(runtime.StartOptionsFromState(state))
	if state.ExecutionWorkspace != "" {
		if state.Worktree != nil && state.Worktree.Enabled && !state.Worktree.Removed {
			if info, statErr := os.Stat(state.ExecutionWorkspace); statErr != nil || !info.IsDir() {
				return nil, fmt.Errorf("managed worktree for run %s is unavailable at %s", state.ID, state.ExecutionWorkspace)
			}
		}
		runner.SetExecutionWorkspace(state.ExecutionWorkspace)
	}
	if err := workflow.ValidateReferences(wf, cfg, runner.CommandResolver()); err != nil {
		return nil, err
	}
	return runner, nil
}

func (s *RunService) Cancel(ctx context.Context, runID, reason string) (any, error) {
	if strings.TrimSpace(reason) == "" {
		reason = "cancelled by MCP client"
	}
	st := s.store
	state, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	if terminalRun(state.Status) {
		return nil, fmt.Errorf("cannot cancel terminal run %s with status %s", state.ID, state.Status)
	}
	if err := s.cancelRunTree(st, state, reason, false); err != nil {
		return nil, err
	}
	state, err = st.Load(state.ID)
	if err != nil {
		return nil, err
	}
	if state.Status == store.RunWaiting {
		release, lockErr := acquireRunLock(st, state.ID)
		if lockErr != nil {
			return nil, lockErr
		}
		state, err = st.Load(state.ID)
		if err == nil {
			var runner *runtime.Runner
			runner, err = s.runnerForState(state)
			if err == nil {
				state, _ = runner.Cancel(state, reason)
			}
		}
		_ = release()
		if err != nil {
			return nil, err
		}
		root, cascadeErr := s.resumeParentChain(ctx, st, state)
		if cascadeErr == nil || errors.Is(cascadeErr, runtime.ErrWaiting) {
			return durablePublicRun(st, root)
		}
		return nil, cascadeErr
	}
	return map[string]any{"run_id": state.ID, "status": state.Status, "cancel_requested": true, "children": state.ChildRunIDs}, nil
}

func terminalRun(status string) bool {
	return status == store.RunCompleted || status == store.RunFailed || status == store.RunCancelled || status == store.RunAbandoned
}

func (s *RunService) cancelRunTree(st RunStore, state *store.RunState, reason string, includeSelf bool) error {
	for _, childID := range state.ChildRunIDs {
		child, err := st.Load(childID)
		if err != nil {
			continue
		}
		if err := s.cancelRunTree(st, child, reason, true); err != nil {
			return err
		}
	}
	if !includeSelf {
		return st.RequestCancel(state.ID)
	}
	if terminalRun(state.Status) {
		return nil
	}
	if err := st.RequestCancel(state.ID); err != nil {
		return err
	}
	if state.Status != store.RunWaiting {
		return nil
	}
	release, err := acquireRunLock(st, state.ID)
	if err != nil {
		return err
	}
	defer release()
	state, err = st.Load(state.ID)
	if err != nil {
		return err
	}
	runner, err := s.runnerForState(state)
	if err != nil {
		return err
	}
	_, cancelErr := runner.Cancel(state, reason)
	if cancelErr != nil && !errors.Is(cancelErr, context.Canceled) {
		return cancelErr
	}
	return nil
}

func (s *RunService) Children(runID string) (map[string]any, error) {
	st := s.store
	parent, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	fanOutMeta := map[string]map[string]any{}
	for nodeID, node := range parent.Nodes {
		if node == nil {
			continue
		}
		for _, item := range node.ChildRuns {
			var decoded any
			if err := json.Unmarshal(item.Item, &decoded); err != nil {
				decoded = string(item.Item)
			}
			fanOutMeta[item.RunID] = map[string]any{"node_id": nodeID, "attempt": item.Attempt, "index": item.Index, "item": decoded}
		}
	}
	children := make([]map[string]any, 0, len(parent.ChildRunIDs))
	for _, id := range parent.ChildRunIDs {
		child, loadErr := st.Load(id)
		if loadErr != nil {
			children = append(children, map[string]any{"id": id, "error": loadErr.Error()})
			continue
		}
		value := map[string]any{"id": child.ID, "status": child.Status, "workflow_path": child.WorkflowPath,
			"parent_node_id": child.ParentNodeID, "execution_workspace": child.ExecutionWorkspace, "usage": child.Usage}
		if meta := fanOutMeta[id]; meta != nil {
			value["fan_out"] = meta
		}
		children = append(children, value)
	}
	return map[string]any{"run_id": parent.ID, "children": children}, nil
}

func (s *RunService) Artifacts(runID string, query ArtifactQuery) (map[string]any, error) {
	st := s.store
	root, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	runs := []*store.RunState{root}
	if query.Recursive {
		queue := append([]string(nil), root.ChildRunIDs...)
		seen := map[string]bool{root.ID: true}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if seen[id] {
				continue
			}
			seen[id] = true
			child, loadErr := st.Load(id)
			if loadErr != nil {
				return nil, loadErr
			}
			runs = append(runs, child)
			queue = append(queue, child.ChildRunIDs...)
		}
	}
	artifacts := make([]store.ArtifactRef, 0)
	seenArtifacts := map[string]bool{}
	for _, run := range runs {
		for _, artifact := range run.Artifacts {
			if query.NodeID != "" && artifact.ProducerNodeID != query.NodeID {
				continue
			}
			if query.Type != "" && artifact.Type != query.Type {
				continue
			}
			key := artifact.ProducerRunID + "\x00" + artifact.ID
			if seenArtifacts[key] {
				continue
			}
			seenArtifacts[key] = true
			artifacts = append(artifacts, artifact)
		}
	}
	sort.Slice(artifacts, func(i, j int) bool {
		if artifacts[i].ProducerRunID != artifacts[j].ProducerRunID {
			return artifacts[i].ProducerRunID < artifacts[j].ProducerRunID
		}
		if artifacts[i].ProducerNodeID != artifacts[j].ProducerNodeID {
			return artifacts[i].ProducerNodeID < artifacts[j].ProducerNodeID
		}
		return artifacts[i].Type < artifacts[j].Type
	})
	return map[string]any{"run_id": root.ID, "artifacts": artifacts}, nil
}

func (s *RunService) Events(ctx context.Context, runID string, afterRevision uint64, limit int, wait time.Duration) (*EventsResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if wait < 0 {
		return nil, fmt.Errorf("wait must not be negative")
	}
	if wait > 30*time.Second {
		wait = 30 * time.Second
	}
	st := s.store
	deadline := time.Now().Add(wait)
	for {
		events, err := st.ReadEvents(runID, afterRevision, limit)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 || wait == 0 {
			next := afterRevision
			if len(events) > 0 {
				next = events[len(events)-1].Revision
			}
			return &EventsResult{RunID: runID, AfterRevision: afterRevision, NextRevision: next, Events: events}, nil
		}
		if !time.Now().Before(deadline) {
			return &EventsResult{RunID: runID, AfterRevision: afterRevision, NextRevision: afterRevision, Events: []store.Event{}, TimedOut: true}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (s *RunService) setLaunchError(runID string, err error) {
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	s.launchErrors[runID] = err
}

func (s *RunService) launchError(runID string) error {
	s.launchMu.Lock()
	defer s.launchMu.Unlock()
	return s.launchErrors[runID]
}
