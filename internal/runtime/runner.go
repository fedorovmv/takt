package runtime

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

	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/definition"
	"takt/internal/execution"
	"takt/internal/gitworktree"
	"takt/internal/spec"
	"takt/internal/store"
)

var ErrWaiting = fmt.Errorf("workflow is waiting for input")

type RunFailedError struct {
	RunID  string
	NodeID string
	Code   string
	Cause  string
}

func (e *RunFailedError) Error() string {
	if e.NodeID == "" {
		return fmt.Sprintf("run %s failed: %s", e.RunID, e.Cause)
	}
	return fmt.Sprintf("run %s failed at node %s: %s", e.RunID, e.NodeID, e.Cause)
}

type Runner struct {
	Workflow         *spec.Workflow
	Config           *spec.Config
	WorkflowPath     string
	ConfigPath       string
	ControlWorkspace string
	Workspace        string
	Commands         command.Resolver
	Store            store.Repository
	Assistants       assistant.Resolver
	startOptions     StartOptions
	inheritedPolicy  assistant.Policy
}

type StartOptions struct {
	RunID           string
	ParentRunID     string
	ParentNodeID    string
	Worktree        *bool
	WorktreeBase    string
	KeepWorktree    bool
	AllowDirty      bool
	InheritedPolicy *assistant.Policy
}

func New(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) *Runner {
	return &Runner{
		Workflow: wf, Config: cfg, WorkflowPath: workflowPath, ConfigPath: configPath,
		ControlWorkspace: workspace, Workspace: workspace,
		Commands: buildCommandResolver(workflowPath, workspace, workspace),
		Store:    store.FS{Workspace: workspace}, Assistants: assistant.Factory{Config: cfg},
	}
}

func buildCommandResolver(workflowPath, executionWorkspace, controlWorkspace string) command.Resolver {
	workflowDir := filepath.Dir(workflowPath)
	home, _ := os.UserHomeDir()
	// Definitions and bundled commands belong to the control checkout. Keep
	// those sources authoritative after execution moves into a worktree; only
	// use worktree-local project commands as a final project-level fallback.
	dirs := []string{filepath.Join(controlWorkspace, ".takt", "commands")}
	dirs = append(dirs, ancestorCommandDirs(workflowDir, controlWorkspace)...)
	if controlWorkspace != executionWorkspace {
		dirs = append(dirs, filepath.Join(executionWorkspace, ".takt", "commands"))
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".takt", "commands"))
	}
	return command.Resolver{Dirs: dirs}
}

func (r *Runner) SetExecutionWorkspace(workspace string) {
	r.Workspace = workspace
	r.Commands = buildCommandResolver(r.WorkflowPath, workspace, r.ControlWorkspace)
}

func (r *Runner) SetStartOptions(options StartOptions) {
	r.startOptions = options
	if options.InheritedPolicy != nil {
		r.inheritedPolicy = *options.InheritedPolicy
	}
}

func ancestorCommandDirs(start, stop string) []string {
	start, _ = filepath.Abs(start)
	stop, _ = filepath.Abs(stop)
	seen := map[string]bool{}
	var dirs []string
	for dir := start; ; dir = filepath.Dir(dir) {
		commandDir := filepath.Join(dir, "commands")
		if !seen[commandDir] {
			dirs = append(dirs, commandDir)
			seen[commandDir] = true
		}
		if dir == stop || dir == filepath.Dir(dir) {
			break
		}
		rel, err := filepath.Rel(stop, dir)
		if err == nil && (rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			break
		}
	}
	return dirs
}

func (r *Runner) Start(ctx context.Context, input string) (*store.RunState, error) {
	return r.StartWithOptions(ctx, input, StartOptions{})
}

func (r *Runner) StartWithOptions(ctx context.Context, input string, options StartOptions) (*store.RunState, error) {
	r.startOptions = options
	if options.InheritedPolicy != nil {
		r.inheritedPolicy = *options.InheritedPolicy
	}
	id := options.RunID
	if id == "" {
		var err error
		id, err = newID()
		if err != nil {
			return nil, err
		}
	}
	if err := store.ValidateRunID(id); err != nil {
		return nil, err
	}
	if value, ok := r.Store.(cancellationStore); ok {
		_ = value.ClearCancel(id)
	}
	worktreeSpec := r.Workflow.Worktree
	if options.Worktree != nil {
		worktreeSpec.Enabled = *options.Worktree
	}
	if options.WorktreeBase != "" {
		worktreeSpec.Base = options.WorktreeBase
	}
	if options.KeepWorktree {
		worktreeSpec.Cleanup = "manual"
	}
	if options.AllowDirty {
		worktreeSpec.AllowDirty = true
	}
	if worktreeSpec.Enabled && worktreeSpec.Cleanup == "" {
		worktreeSpec.Cleanup = "on_success"
	}
	var worktreeState *store.WorktreeState
	if worktreeSpec.Enabled {
		info, prepareErr := gitworktree.Prepare(ctx, r.ControlWorkspace, id, r.Workflow.Metadata.Name, gitworktree.Options{
			Base: worktreeSpec.Base, BranchPrefix: worktreeSpec.BranchPrefix, AllowDirty: worktreeSpec.AllowDirty,
		})
		if prepareErr != nil {
			return nil, prepareErr
		}
		r.Workspace = info.ExecutionWorkspace
		r.Commands = buildCommandResolver(r.WorkflowPath, r.Workspace, r.ControlWorkspace)
		worktreeState = &store.WorktreeState{
			Enabled: true, RepositoryRoot: info.RepositoryRoot, ControlWorkspace: info.ControlWorkspace,
			ExecutionWorkspace: info.ExecutionWorkspace, Path: info.Path, Branch: info.Branch,
			BaseRef: info.BaseRef, BaseCommit: info.BaseCommit, Cleanup: worktreeSpec.Cleanup, BaseDirty: info.BaseDirty,
		}
	}
	fingerprints, err := definition.Compute(r.Workflow, r.Config, r.WorkflowPath, r.ConfigPath, r.Commands)
	if err != nil {
		r.rollbackPreparedWorktree(worktreeState)
		return nil, err
	}
	now := time.Now().UTC()
	state := &store.RunState{
		ID: id, Status: store.RunRunning, ParentRunID: options.ParentRunID, ParentNodeID: options.ParentNodeID, WorkflowPath: r.WorkflowPath, ConfigPath: r.ConfigPath,
		Workspace: r.ControlWorkspace, ExecutionWorkspace: r.Workspace, Worktree: worktreeState, RunOptions: runOptionsState(options), InheritedPolicy: policyState(r.inheritedPolicy, nil), Input: input, Nodes: map[string]*store.NodeState{},
		Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		WorkflowFingerprint: fingerprints.Workflow,
		ConfigFingerprint:   fingerprints.Config,
		CommandsFingerprint: fingerprints.Commands,
	}
	for _, node := range r.Workflow.Nodes {
		state.Nodes[node.ID] = &store.NodeState{Status: store.NodePending, Hidden: node.Hidden, PublicParent: node.PublicParent}
	}
	data := map[string]any{"workflow": r.Workflow.Metadata.Name, "execution_workspace": r.Workspace}
	if options.ParentRunID != "" {
		data["parent_run_id"] = options.ParentRunID
		data["parent_node_id"] = options.ParentNodeID
	}
	if worktreeState != nil {
		data["worktree"] = map[string]any{"path": worktreeState.Path, "branch": worktreeState.Branch, "base_commit": worktreeState.BaseCommit}
	}
	if err := r.commit(state, "run.started", "", data); err != nil {
		r.rollbackPreparedWorktree(worktreeState)
		return nil, err
	}
	return r.resume(ctx, state)
}

func runOptionsState(options StartOptions) store.RunOptionsState {
	mode := "auto"
	if options.Worktree != nil {
		if *options.Worktree {
			mode = "enabled"
		} else {
			mode = "disabled"
		}
	}
	return store.RunOptionsState{
		WorktreeMode: mode, WorktreeBase: options.WorktreeBase,
		KeepWorktree: options.KeepWorktree, AllowDirty: options.AllowDirty,
	}
}

func StartOptionsFromState(state *store.RunState) StartOptions {
	options := StartOptions{
		WorktreeBase: state.RunOptions.WorktreeBase,
		KeepWorktree: state.RunOptions.KeepWorktree,
		AllowDirty:   state.RunOptions.AllowDirty,
	}
	if state.InheritedPolicy != nil {
		policy := policyFromState(state.InheritedPolicy)
		options.InheritedPolicy = &policy
	}
	switch state.RunOptions.WorktreeMode {
	case "enabled":
		value := true
		options.Worktree = &value
	case "disabled":
		value := false
		options.Worktree = &value
	}
	return options
}

func (r *Runner) prepareDynamicWorktree(ctx context.Context, state *store.RunState, internal *spec.InternalNodeSpec) error {
	if internal == nil || internal.Worktree == nil || !internal.Worktree.Enabled {
		return nil
	}
	if state.Worktree != nil && state.Worktree.Enabled && !state.Worktree.Removed {
		return nil
	}
	policy := *internal.Worktree
	options := r.startOptions
	if options.Worktree != nil {
		policy.Enabled = *options.Worktree
	}
	if !policy.Enabled {
		return nil
	}
	if options.WorktreeBase != "" {
		policy.Base = options.WorktreeBase
	}
	if options.KeepWorktree {
		policy.Cleanup = "manual"
	}
	if options.AllowDirty {
		policy.AllowDirty = true
	}
	if policy.Cleanup == "" {
		policy.Cleanup = "on_success"
	}
	info, err := gitworktree.Prepare(ctx, r.ControlWorkspace, state.ID, internal.WorkflowName, gitworktree.Options{
		Base: policy.Base, BranchPrefix: policy.BranchPrefix, AllowDirty: policy.AllowDirty,
	})
	if err != nil {
		return err
	}
	value := &store.WorktreeState{
		Enabled: true, RepositoryRoot: info.RepositoryRoot, ControlWorkspace: info.ControlWorkspace,
		ExecutionWorkspace: info.ExecutionWorkspace, Path: info.Path, Branch: info.Branch,
		BaseRef: info.BaseRef, BaseCommit: info.BaseCommit, Cleanup: policy.Cleanup, BaseDirty: info.BaseDirty,
	}
	previousWorkspace := r.Workspace
	r.SetExecutionWorkspace(info.ExecutionWorkspace)
	state.ExecutionWorkspace = info.ExecutionWorkspace
	state.Worktree = value
	if err := r.commit(state, "worktree.created", "", map[string]any{
		"path": value.Path, "branch": value.Branch, "base_commit": value.BaseCommit,
		"workflow": internal.WorkflowName, "dynamic": true,
	}); err != nil {
		r.SetExecutionWorkspace(previousWorkspace)
		state.ExecutionWorkspace = previousWorkspace
		state.Worktree = nil
		r.rollbackPreparedWorktree(value)
		return err
	}
	return nil
}

func (r *Runner) rollbackPreparedWorktree(value *store.WorktreeState) {
	if value == nil || value.Path == "" || value.RepositoryRoot == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = gitworktree.Remove(ctx, value.RepositoryRoot, value.Path, true)
	_, _ = gitworktree.DeleteBranchIfUnchanged(ctx, value.RepositoryRoot, value.Branch, value.BaseCommit)
}

func (r *Runner) VerifyDefinitions(state *store.RunState) error {
	actual, err := definition.Compute(r.Workflow, r.Config, r.WorkflowPath, r.ConfigPath, r.Commands)
	if err != nil {
		return err
	}
	return definition.Verify(definition.Fingerprints{
		Workflow: state.WorkflowFingerprint,
		Config:   state.ConfigFingerprint,
		Commands: state.CommandsFingerprint,
	}, actual)
}

func (r *Runner) Resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	if state.Status == store.RunCompleted || state.Status == store.RunCancelled {
		return state, nil
	}
	if state.Status == store.RunFailed {
		return state, &RunFailedError{RunID: state.ID, NodeID: state.CurrentNode, Code: state.ErrorCode, Cause: state.Error}
	}
	if err := r.VerifyDefinitions(state); err != nil {
		return state, err
	}
	if state.Status == store.RunWaiting && state.Waiting != nil {
		if state.Waiting.Kind != "child_run" {
			return state, ErrWaiting
		}
		if node := state.Nodes[state.Waiting.NodeID]; node != nil {
			node.Status = store.NodePending
		}
		state.Waiting = nil
	}
	state.Status = store.RunRunning
	state.CurrentNodes = nil
	state.Error, state.ErrorCode = "", ""
	if err := r.commit(state, "run.resumed", "", nil); err != nil {
		return state, err
	}
	return r.resume(ctx, state)
}

func (r *Runner) resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	if state.CancelRequested {
		return r.cancelState(state, "cancel_requested")
	}
	err := r.executeGraph(ctx, state, r.Workflow.Nodes, nil)
	if errors.Is(err, ErrWaiting) {
		return state, ErrWaiting
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			kind := execution.KindCancelled
			if errors.Is(err, context.DeadlineExceeded) {
				kind = execution.KindTimedOut
			} else if r.cancellationRequested(state.ID) {
				state.CancelRequested = true
			}
			state.Status = store.RunCancelled
			state.ErrorCode = string(kind)
			state.Error = err.Error()
			state.CurrentNode = ""
			state.CurrentNodes = nil
			if finalizeErr := r.finalizeWorktree(state, store.RunCancelled); finalizeErr != nil {
				return state, finalizeErr
			}
			if commitErr := r.commit(state, "run.cancelled", "", map[string]any{"error": err.Error()}); commitErr != nil {
				return state, commitErr
			}
			return state, err
		}
		return state, err
	}

	status, nodeID, code, cause := graphResult(r.Workflow.Nodes, state.Nodes)
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.Status = status
	state.ErrorCode = code
	state.Error = cause
	state.Usage = aggregateRunUsage(state.Nodes)
	if status == store.RunCompleted {
		state.Output = defaultRunOutput(r.Workflow.Nodes, state.Nodes)
	}
	switch status {
	case store.RunCompleted:
		if err := r.commit(state, "run.completed", "", nil); err != nil {
			return state, err
		}
		if err := r.finalizeWorktree(state, store.RunCompleted); err != nil {
			return state, err
		}
		return state, nil
	case store.RunCancelled:
		if err := r.finalizeWorktree(state, store.RunCancelled); err != nil {
			return state, err
		}
		if err := r.commit(state, "run.cancelled", nodeID, map[string]any{"error": cause, "code": code}); err != nil {
			return state, err
		}
		return state, &RunFailedError{RunID: state.ID, NodeID: nodeID, Code: code, Cause: cause}
	default:
		if err := r.finalizeWorktree(state, store.RunFailed); err != nil {
			return state, err
		}
		if err := r.commit(state, "run.failed", nodeID, map[string]any{"error": cause, "code": code}); err != nil {
			return state, err
		}
		return state, &RunFailedError{RunID: state.ID, NodeID: nodeID, Code: code, Cause: cause}
	}
}

func (r *Runner) executeGraph(ctx context.Context, state *store.RunState, nodes []spec.Node, previous map[string]store.NodeState) error {
	for {
		if state.CancelRequested || r.cancellationRequested(state.ID) {
			return context.Canceled
		}
		progress := false
		runnable := make([]spec.Node, 0, len(nodes))
		for _, node := range nodes {
			ns := state.Nodes[node.ID]
			if ns == nil {
				ns = &store.NodeState{Status: store.NodePending}
				state.Nodes[node.ID] = ns
			}
			if ns.Terminal() {
				continue
			}
			if node.Guard != "" {
				guard := state.Nodes[node.Guard]
				if guard == nil || !guard.Terminal() {
					continue
				}
				if guard.Status != store.NodeCompleted {
					ns.Status = store.NodeSkipped
					ns.ErrorCode = "container_guard_not_satisfied"
					progress = true
					if err := r.commit(state, "node.skipped", node.ID, map[string]any{"reason": "container_guard", "guard": node.Guard}); err != nil {
						return err
					}
					continue
				}
			}
			if ns.Status == store.NodeWaiting {
				return ErrWaiting
			}
			ready, skip := dependenciesReady(node, state.Nodes)
			if skip {
				ns.Status = store.NodeSkipped
				ns.ErrorCode = "trigger_rule_not_satisfied"
				progress = true
				if err := r.commit(state, "node.skipped", node.ID, map[string]any{"reason": "trigger_rule"}); err != nil {
					return err
				}
				continue
			}
			if !ready {
				continue
			}
			ok, err := evalWhen(node.When, state)
			if err != nil {
				ns.Status = store.NodeErrored
				ns.ErrorCode = "when_evaluation_failed"
				ns.Error = err.Error()
				progress = true
				if commitErr := r.commit(state, "node.errored", node.ID, map[string]any{"error": err.Error(), "code": ns.ErrorCode}); commitErr != nil {
					return commitErr
				}
				continue
			}
			if !ok {
				ns.Status = store.NodeSkipped
				ns.ErrorCode = "when_false"
				progress = true
				if err := r.commit(state, "node.skipped", node.ID, map[string]any{"reason": "when"}); err != nil {
					return err
				}
				continue
			}
			runnable = append(runnable, node)
		}

		parallel := make([]spec.Node, 0, len(runnable))
		sequential := make([]spec.Node, 0, len(runnable))
		for _, node := range runnable {
			if parallelEligible(node) {
				parallel = append(parallel, node)
			} else {
				sequential = append(sequential, node)
			}
		}
		if len(parallel) >= 2 {
			progress = true
			if err := r.runParallelWave(ctx, state, parallel, previous); err != nil {
				return err
			}
		} else {
			sequential = append(parallel, sequential...)
		}
		for _, node := range sequential {
			progress = true
			if err := r.runNode(ctx, state, node, previous); err != nil {
				return err
			}
		}
		if graphTerminal(nodes, state.Nodes) {
			return nil
		}
		if !progress {
			for _, node := range nodes {
				ns := state.Nodes[node.ID]
				if ns == nil || ns.Terminal() {
					continue
				}
				ns.Status = store.NodeBlocked
				ns.ErrorCode = "unresolved_dependencies"
				ns.Error = "node cannot become runnable because its dependencies or trigger rules are unresolved"
				if err := r.commit(state, "node.blocked", node.ID, map[string]any{"error": ns.Error, "code": ns.ErrorCode}); err != nil {
					return err
				}
			}
		}
	}
}

func (r *Runner) runNode(ctx context.Context, state *store.RunState, node spec.Node, loopPrevious map[string]store.NodeState) error {
	ns := state.Nodes[node.ID]
	max := node.Attempts.Max
	if max <= 0 {
		max = 1
	}
	hooks := mergeHooks(r.Workflow.Hooks, node.Hooks)
	if node.Internal != nil {
		hooks = spec.HookSet{}
	}
	for ns.Attempts < max {
		ns.Attempts++
		ns.Status = store.NodeRunning
		ns.Error, ns.ErrorCode = "", ""
		state.CurrentNode = node.ID
		state.CurrentNodes = nil
		if err := r.commit(state, "node.started", node.ID, map[string]any{"attempt": ns.Attempts}); err != nil {
			return err
		}

		attemptCtx, cancel, err := nodeContext(ctx, node.Timeout)
		if err != nil {
			return r.finishNodeError(state, node.ID, "invalid_timeout", err, execResult{})
		}
		attemptCtx, cancelWatch := r.watchCancellation(attemptCtx, state.ID)
		originalCancel := cancel
		cancel = func() { cancelWatch(); originalCancel() }

		decision, feedback, hookErr := r.runHooks(attemptCtx, state, node, hooks.BeforeNode, loopPrevious)
		if hookErr != nil {
			cancel()
			return r.finishAttemptExecutionError(state, node.ID, hookErr, execResult{})
		}
		if decision == "retry" {
			cancel()
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			if err := r.commit(state, "node.retry", node.ID, map[string]any{"feedback": feedback, "phase": "before_node"}); err != nil {
				return err
			}
			continue
		}
		if decision == "fail" {
			cancel()
			return r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("before_node hook failed: %s", feedback), execResult{})
		}

		result, execErr := r.execute(attemptCtx, state, node, loopPrevious)
		if errors.Is(execErr, ErrWaiting) {
			cancel()
			// Waiting is a suspension point, not a consumed attempt. Persist the
			// rollback so a separate CLI process can resume the same attempt.
			ns.Attempts--
			reason := "approval"
			if state.Waiting != nil && state.Waiting.Kind != "" {
				reason = state.Waiting.Kind
			}
			if err := r.commit(state, "node.suspended", node.ID, map[string]any{"reason": reason}); err != nil {
				return err
			}
			return ErrWaiting
		}
		// The node timeout/cancellation covers the whole attempt, including
		// container nodes such as loop_group. A child graph may finish by
		// returning a derived error (for example loop exhaustion) after the
		// parent attempt context has already expired. Preserve the context
		// classification before allow_failure or generic error handling can
		// turn it into an ordinary exit failure.
		if contextErr := attemptContextError(attemptCtx, "node attempt"); contextErr != nil {
			// Specialized adapters may already have classified the same parent
			// timeout/cancellation and attached provider diagnostics. Preserve
			// that richer error instead of replacing it with the generic node
			// attempt message. Derived exit/protocol errors are still overridden
			// by the authoritative parent context classification.
			kind := execution.KindOf(execErr)
			if execErr == nil || (kind != execution.KindTimedOut && kind != execution.KindCancelled) {
				execErr = contextErr
			}
		}
		recordExecution(ns, result, execErr)
		applyExecResult(ns, result)
		mergeRunArtifacts(state, result.Artifacts)
		accumulateUsage(ns, result.Usage)
		if execErr != nil && node.AllowFailure && execution.IsExit(execErr) {
			execErr = nil
			if err := r.commit(state, "node.failure_allowed", node.ID, map[string]any{"exit_code": result.ExitCode}); err != nil {
				cancel()
				return err
			}
		}
		if execErr != nil {
			kind := execution.KindOf(execErr)
			if shouldRetryAttempt(node.Attempts, kind, ns.Attempts, max) {
				cancel()
				ns.Feedback = joinFeedback(ns.Feedback, execErr.Error())
				if node.Attempts.RetrySession == "fresh" {
					ns.SessionID = ""
				}
				if err := r.commit(state, "node.retry", node.ID, map[string]any{"feedback": ns.Feedback, "phase": "attempts", "kind": string(kind)}); err != nil {
					return err
				}
				continue
			}
			if kind == execution.KindCancelled || kind == execution.KindTimedOut {
				cancel()
				return r.finishAttemptExecutionError(state, node.ID, execErr, result)
			}
			decision, feedback, hookErr = r.runHooks(attemptCtx, state, node, hooks.OnFailure, loopPrevious)
			if hookErr != nil {
				cancel()
				return r.finishAttemptExecutionError(state, node.ID, hookErr, result)
			}
			if decision == "retry" {
				cancel()
				ns.Feedback = joinFeedback(ns.Feedback, feedback, execErr.Error())
				if err := r.commit(state, "node.retry", node.ID, map[string]any{"feedback": ns.Feedback, "phase": "on_failure"}); err != nil {
					return err
				}
				continue
			}
			if decision == "fail" && feedback != "" {
				ns.Feedback = joinFeedback(ns.Feedback, feedback)
			}
			cancel()
			if err := r.finishNodeExecutionError(state, node.ID, kind, execErr, result); err != nil {
				return err
			}
			return nil
		}

		decision, feedback, hookErr = r.runHooks(attemptCtx, state, node, hooks.AfterNode, loopPrevious)
		if hookErr != nil {
			cancel()
			return r.finishAttemptExecutionError(state, node.ID, hookErr, result)
		}
		if decision == "retry" {
			cancel()
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			if err := r.commit(state, "node.retry", node.ID, map[string]any{"feedback": feedback, "phase": "after_node"}); err != nil {
				return err
			}
			continue
		}
		if decision == "fail" {
			cancel()
			return r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("after_node hook failed: %s", feedback), result)
		}

		decision, feedback, hookErr = r.runHooks(attemptCtx, state, node, hooks.BeforeComplete, loopPrevious)
		if hookErr != nil {
			cancel()
			return r.finishAttemptExecutionError(state, node.ID, hookErr, result)
		}
		if decision == "retry" {
			cancel()
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			if err := r.commit(state, "node.retry", node.ID, map[string]any{"feedback": feedback, "phase": "before_complete"}); err != nil {
				return err
			}
			continue
		}
		if decision == "fail" {
			cancel()
			return r.finishNodeFailure(state, node.ID, "hook_failed", fmt.Errorf("before_complete hook failed: %s", feedback), result)
		}
		if err := attemptContextError(attemptCtx, "node attempt"); err != nil {
			cancel()
			return r.finishAttemptExecutionError(state, node.ID, err, result)
		}
		if err := r.captureDeclaredArtifact(state, node, loopPrevious); err != nil {
			cancel()
			return r.finishNodeError(state, node.ID, "artifact", fmt.Errorf("persist typed artifact: %w", err), result)
		}

		cancel()
		ns.Status = store.NodeCompleted
		state.CurrentNode = ""
		state.CurrentNodes = nil
		if err := r.commit(state, "node.completed", node.ID, map[string]any{"attempts": ns.Attempts, "exit_code": ns.ExitCode, "output_truncated": ns.OutputTruncated, "usage": ns.Usage, "artifacts": ns.Artifacts}); err != nil {
			return err
		}
		return nil
	}
	return r.finishNodeFailure(state, node.ID, "attempts_exhausted", fmt.Errorf("node %q exhausted %d attempts; feedback: %s", node.ID, max, ns.Feedback), execResult{})
}

func (r *Runner) finishAttemptExecutionError(state *store.RunState, nodeID string, err error, result execResult) error {
	var execErr *execution.Error
	if !errors.As(err, &execErr) {
		return err
	}
	kind := execErr.Kind
	if commitErr := r.finishNodeExecutionError(state, nodeID, kind, err, result); commitErr != nil {
		return commitErr
	}
	if kind == execution.KindCancelled {
		return context.Canceled
	}
	return nil
}

func attemptContextError(ctx context.Context, op string) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	kind := execution.KindCancelled
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		kind = execution.KindTimedOut
	}
	return &execution.Error{Kind: kind, ExitCode: -1, Op: op, Err: ctx.Err()}
}

type execResult struct {
	Output           string
	Stdout           string
	Stderr           string
	ExitCode         int
	SessionID        string
	Resumed          bool
	Truncated        bool
	Usage            *assistant.ProtocolUsage
	Assistant        string
	AssistantVersion string
	RequestedModel   *store.ModelRef
	ResolvedModel    *store.ModelRef
	Artifacts        []store.ArtifactRef
}

func (r *Runner) execute(ctx context.Context, state *store.RunState, node spec.Node, loopPrevious map[string]store.NodeState) (execResult, error) {
	local := loopPrevious
	if local == nil {
		local = map[string]store.NodeState{}
	}
	artifacts := r.Store.ArtifactsDir(state.ID)
	feedback := state.Nodes[node.ID].Feedback
	switch {
	case node.Bash != "":
		return runBash(ctx, r.Workspace, renderTemplate(node.Bash, state, local, feedback, artifacts))
	case node.Script != nil:
		result, err := r.runScript(ctx, state, node, local, feedback, artifacts)
		if err == nil && node.OutputFormat != nil {
			normalized, validationErr := validateAndNormalizeOutput(result.Output, node.OutputFormat)
			if validationErr != nil {
				return result, &execution.Error{Kind: execution.KindProtocol, ExitCode: result.ExitCode, Op: "validate structured output", Err: validationErr}
			}
			result.Output = normalized
		}
		return result, err
	case node.Approval != nil:
		if answer, ok := state.Approvals[node.ID]; ok {
			output := ""
			if node.Approval.CaptureResponse {
				output = answer
			}
			return execResult{Output: output, Stdout: output, ExitCode: 0}, nil
		}
		message := renderTemplate(node.Approval.Message, state, local, feedback, artifacts)
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: message, Kind: "approval"}
		state.Nodes[node.ID].Status = store.NodeWaiting
		if err := r.commit(state, "approval.requested", node.ID, map[string]any{"message": message}); err != nil {
			return execResult{}, err
		}
		return execResult{}, ErrWaiting
	case node.LoopGroup != nil:
		return r.runLoopGroup(ctx, state, node)
	case node.WorkflowRun != nil:
		return r.runChildWorkflow(ctx, state, node, local, feedback, artifacts)
	case node.Internal != nil:
		switch node.Internal.Mode {
		case "noop":
			return execResult{ExitCode: 0}, nil
		case "worktree":
			if err := r.prepareDynamicWorktree(ctx, state, node.Internal); err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "prepare worktree", Err: err}
			}
			return execResult{Output: r.Workspace, Stdout: r.Workspace, ExitCode: 0}, nil
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
	case node.Command != "" || node.Prompt != "":
		prompt := node.Prompt
		assistantName, modelName := node.Assistant, node.Model
		if node.Command != "" {
			cmd, err := r.Commands.Resolve(node.Command)
			if err != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve command", Err: err}
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
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: fmt.Errorf("node %q does not resolve an assistant", node.ID)}
		}
		model, ok := r.Config.Models[modelName]
		if !ok {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve model", Err: fmt.Errorf("node %q references unknown model %q", node.ID, modelName)}
		}
		resolver := r.Assistants
		if resolver == nil {
			resolver = assistant.Factory{Config: r.Config}
		}
		adapter, err := resolver.Resolve(assistantName)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: err}
		}
		policy, err := resolveNodePolicy(node, r.WorkflowPath, r.inheritedPolicy)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve node policy", Err: err}
		}
		capabilities, err := validateAdapterPolicy(adapter, policy)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "validate assistant capabilities", Err: err}
		}
		if state.Nodes[node.ID].Policy == nil {
			state.Nodes[node.ID].Policy = policyState(policy, capabilities)
			if state.Nodes[node.ID].Policy != nil {
				if err := r.commit(state, "node.policy.applied", node.ID, map[string]any{"policy": state.Nodes[node.ID].Policy}); err != nil {
					return execResult{}, err
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
		prompt = renderTemplate(prompt, state, local, feedback, artifacts)
		result, err := adapter.Run(ctx, assistant.Request{RunID: state.ID, NodeID: node.ID, Attempt: state.Nodes[node.ID].Attempts, Prompt: prompt, Workspace: r.Workspace, ModelName: modelName, Model: model, SessionMode: sessionMode, SessionID: sessionID, NativeHooks: node.NativeHooks, Policy: policy})
		execResult := execResult{
			Output: result.Output, Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode, SessionID: result.SessionID,
			Resumed: result.Resumed, Truncated: result.Truncated, Usage: result.Usage,
			Assistant:        assistantName,
			AssistantVersion: result.AssistantVersion,
			RequestedModel:   &store.ModelRef{Name: modelName, Provider: model.Provider, ID: model.ID, Params: cloneParams(model.Params)},
		}
		if result.ResolvedModel != nil {
			execResult.ResolvedModel = &store.ModelRef{Name: result.ResolvedModel.Name, Provider: result.ResolvedModel.Provider, ID: result.ResolvedModel.ID, Params: cloneParams(result.ResolvedModel.Params)}
		}
		if err == nil && node.OutputFormat != nil {
			normalized, validationErr := validateAndNormalizeOutput(result.Output, node.OutputFormat)
			if validationErr != nil {
				return execResult, &execution.Error{Kind: execution.KindProtocol, ExitCode: result.ExitCode, Op: "validate structured output", Err: validationErr}
			}
			execResult.Output = normalized
		}
		return execResult, err
	default:
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "execute node", Err: fmt.Errorf("unsupported node %q", node.ID)}
	}
}

func collectNodeOutputs(state *store.RunState, ids []string) (string, error) {
	values := make([]any, 0, len(ids))
	for _, id := range ids {
		source := state.Nodes[id]
		if source == nil {
			return "", fmt.Errorf("result source %q is missing", id)
		}
		raw := strings.TrimSpace(source.Output)
		if raw != "" {
			var value any
			if json.Unmarshal([]byte(raw), &value) == nil {
				values = append(values, value)
				continue
			}
		}
		values = append(values, source.Output)
	}
	b, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Runner) runLoopGroup(ctx context.Context, state *store.RunState, parent spec.Node) (execResult, error) {
	seen := make(map[string]struct{}, len(parent.LoopGroup.Nodes))
	for _, child := range parent.LoopGroup.Nodes {
		if child.LoopGroup != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("nested loop_group is not supported in v1alpha1: %s.%s", parent.ID, child.ID)}
		}
		if _, duplicate := seen[child.ID]; duplicate {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("duplicate child node id %q", child.ID)}
		}
		seen[child.ID] = struct{}{}
	}
	parentState := state.Nodes[parent.ID]
	previous := parentState.LoopPrevious
	startIteration := 1
	if parentState.LoopIteration > 0 {
		startIteration = parentState.LoopIteration
	}
	for iteration := startIteration; iteration <= parent.LoopGroup.MaxIterations; iteration++ {
		resuming := parentState.LoopIteration == iteration
		if !resuming {
			for _, child := range parent.LoopGroup.Nodes {
				if _, exists := state.Nodes[child.ID]; exists {
					return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("child node id %q collides with existing runtime state", child.ID)}
				}
			}
			for _, child := range parent.LoopGroup.Nodes {
				state.Nodes[child.ID] = &store.NodeState{Status: store.NodePending, Hidden: child.Hidden, PublicParent: child.PublicParent}
			}
			parentState.LoopIteration = iteration
			if err := r.commit(state, "loop.iteration.started", parent.ID, map[string]any{"iteration": iteration}); err != nil {
				return execResult{}, err
			}
		}
		if err := r.executeGraph(ctx, state, parent.LoopGroup.Nodes, previous); err != nil {
			return execResult{}, err
		}
		local := make(map[string]store.NodeState, len(parent.LoopGroup.Nodes))
		for _, child := range parent.LoopGroup.Nodes {
			local[child.ID] = *state.Nodes[child.ID]
			delete(state.Nodes, child.ID)
			delete(state.Approvals, child.ID)
		}
		check, exists := local[parent.LoopGroup.Until.Node]
		if !exists {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("until node %q missing", parent.LoopGroup.Until.Node)}
		}
		parentState.LoopPrevious = local
		parentState.LoopIteration = 0
		if err := r.commit(state, "loop.iteration.completed", parent.ID, map[string]any{
			"iteration": iteration, "until_node": parent.LoopGroup.Until.Node,
			"exit_code": check.ExitCode, "status": check.Status,
		}); err != nil {
			return execResult{}, err
		}
		if untilSatisfied(parent.LoopGroup.Until, check) {
			return execResult{Output: check.Output, Stdout: check.Stdout, Stderr: check.Stderr, ExitCode: check.ExitCode, SessionID: check.SessionID, Truncated: check.OutputTruncated}, nil
		}
		previous = local
	}
	return execResult{}, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "loop_group", Err: fmt.Errorf("loop_group %q exhausted %d iterations", parent.ID, parent.LoopGroup.MaxIterations)}
}

func (r *Runner) runHooks(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, local map[string]store.NodeState) (string, string, error) {
	for _, hook := range hooks {
		result, err := runBash(ctx, r.Workspace, renderTemplate(hook.Bash, state, local, state.Nodes[node.ID].Feedback, r.Store.ArtifactsDir(state.ID)))
		if err == nil && result.ExitCode == 0 {
			continue
		}
		if err != nil {
			kind := execution.KindOf(err)
			if kind == execution.KindCancelled || kind == execution.KindTimedOut {
				return "", strings.TrimSpace(result.Output), err
			}
		}
		feedback := strings.TrimSpace(result.Output)
		if feedback == "" && err != nil {
			feedback = err.Error()
		}
		action := hook.OnFailure.Action
		if action == "" {
			action = "fail"
		}
		if commitErr := r.commit(state, "hook.failed", node.ID, map[string]any{"hook": hook.ID, "action": action, "feedback": feedback}); commitErr != nil {
			return "", feedback, commitErr
		}
		switch action {
		case "continue":
			continue
		case "retry":
			if hook.OnFailure.Session == "fresh" && state.Nodes[node.ID] != nil {
				state.Nodes[node.ID].SessionID = ""
			}
			return "retry", feedback, nil
		case "fail":
			return "fail", feedback, nil
		default:
			return "", feedback, fmt.Errorf("hook %q has unsupported action %q", hook.ID, action)
		}
	}
	return "continue", "", nil
}

func applyExecResult(node *store.NodeState, result execResult) {
	if node == nil {
		return
	}
	node.Output = result.Output
	node.Stdout = result.Stdout
	node.Stderr = result.Stderr
	node.ExitCode = result.ExitCode
	node.SessionID = result.SessionID
	node.Resumed = result.Resumed
	node.OutputTruncated = result.Truncated
	if result.Assistant != "" {
		node.Assistant = result.Assistant
	}
	if result.AssistantVersion != "" {
		node.AssistantVersion = result.AssistantVersion
	}
	if result.RequestedModel != nil {
		copy := *result.RequestedModel
		copy.Params = cloneParams(result.RequestedModel.Params)
		node.RequestedModel = &copy
	}
	if result.ResolvedModel != nil {
		copy := *result.ResolvedModel
		copy.Params = cloneParams(result.ResolvedModel.Params)
		node.ResolvedModel = &copy
	}
	if len(result.Artifacts) > 0 {
		node.Artifacts = cloneArtifacts(result.Artifacts)
	}
}

func mergeRunArtifacts(state *store.RunState, artifacts []store.ArtifactRef) {
	for _, artifact := range artifacts {
		state.Artifacts = appendArtifactUnique(state.Artifacts, artifact)
	}
}

func cloneParams(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func accumulateUsage(node *store.NodeState, usage *assistant.ProtocolUsage) {
	if node == nil || usage == nil {
		return
	}
	if node.Usage == nil {
		node.Usage = &store.Usage{}
	}
	node.Usage.InputTokens += usage.InputTokens
	node.Usage.OutputTokens += usage.OutputTokens
	node.Usage.Cost += usage.Cost
}

func recordExecution(node *store.NodeState, result execResult, err error) {
	if node == nil {
		return
	}
	status := store.NodeCompleted
	errorCode := ""
	errorText := ""
	if err != nil {
		kind := execution.KindOf(err)
		errorCode = string(kind)
		errorText = err.Error()
		switch kind {
		case execution.KindExit:
			status = store.NodeFailed
		case execution.KindCancelled:
			status = store.NodeCancelled
		case execution.KindTimedOut:
			status = store.NodeTimedOut
		default:
			status = store.NodeErrored
		}
	}
	record := store.ExecutionState{
		Attempt:          node.Attempts,
		Status:           status,
		Assistant:        result.Assistant,
		AssistantVersion: result.AssistantVersion,
		RequestedModel:   cloneModelRef(result.RequestedModel),
		ResolvedModel:    cloneModelRef(result.ResolvedModel),
		SessionID:        result.SessionID,
		Resumed:          result.Resumed,
		ExitCode:         result.ExitCode,
		ErrorCode:        errorCode,
		Error:            errorText,
		OutputTruncated:  result.Truncated,
	}
	if result.Usage != nil {
		record.Usage = &store.Usage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			Cost:         result.Usage.Cost,
		}
	}
	node.Executions = append(node.Executions, record)
}

func cloneModelRef(model *store.ModelRef) *store.ModelRef {
	if model == nil {
		return nil
	}
	copy := *model
	copy.Params = cloneParams(model.Params)
	return &copy
}

func (r *Runner) finishNodeExecutionError(state *store.RunState, nodeID string, kind execution.Kind, err error, result execResult) error {
	status := store.NodeErrored
	switch kind {
	case execution.KindExit:
		status = store.NodeFailed
	case execution.KindCancelled:
		status = store.NodeCancelled
	case execution.KindTimedOut:
		status = store.NodeTimedOut
	}
	ns := state.Nodes[nodeID]
	ns.Status = status
	ns.ErrorCode = string(kind)
	ns.Error = err.Error()
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node."+status, nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "exit_code": ns.ExitCode, "usage": ns.Usage})
}

func (r *Runner) finishNodeError(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeErrored
	ns.ErrorCode = code
	ns.Error = err.Error()
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node.errored", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "usage": ns.Usage})
}

func (r *Runner) finishNodeFailure(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeFailed
	ns.ErrorCode = code
	ns.Error = err.Error()
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node.failed", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "usage": ns.Usage})
}

func (r *Runner) commit(state *store.RunState, eventType, nodeID string, data map[string]any) error {
	return r.Store.Commit(state, store.Event{Type: eventType, NodeID: nodeID, Data: data})
}

func mergeHooks(global, local spec.HookSet) spec.HookSet {
	return spec.HookSet{
		BeforeNode:     append(append([]spec.HookSpec{}, global.BeforeNode...), local.BeforeNode...),
		AfterNode:      append(append([]spec.HookSpec{}, global.AfterNode...), local.AfterNode...),
		BeforeComplete: append(append([]spec.HookSpec{}, global.BeforeComplete...), local.BeforeComplete...),
		OnFailure:      append(append([]spec.HookSpec{}, global.OnFailure...), local.OnFailure...),
	}
}

func dependenciesReady(node spec.Node, states map[string]*store.NodeState) (ready bool, skip bool) {
	if len(node.DependsOn) == 0 {
		return true, false
	}
	completed, failed, terminal := 0, 0, 0
	for _, dep := range node.DependsOn {
		state := states[dep]
		if state == nil || !state.Terminal() {
			continue
		}
		terminal++
		if state.Successful() {
			completed++
		}
		if state.FailedLike() {
			failed++
		}
	}
	if terminal != len(node.DependsOn) {
		return false, false
	}
	rule := node.TriggerRule
	if rule == "" {
		rule = "all_success"
	}
	switch rule {
	case "all_done":
		return true, false
	case "none_failed_min_one_success":
		return failed == 0 && completed > 0, failed > 0 || completed == 0
	case "one_success":
		return completed > 0, completed == 0
	default:
		return completed == len(node.DependsOn), completed != len(node.DependsOn)
	}
}

func graphTerminal(nodes []spec.Node, states map[string]*store.NodeState) bool {
	for _, node := range nodes {
		state := states[node.ID]
		if state == nil || !state.Terminal() {
			return false
		}
	}
	return true
}

func graphResult(nodes []spec.Node, states map[string]*store.NodeState) (status, nodeID, code, cause string) {
	for _, node := range nodes {
		state := states[node.ID]
		if state == nil {
			continue
		}
		if state.Status == store.NodeCancelled {
			return store.RunCancelled, node.ID, state.ErrorCode, state.Error
		}
	}
	for _, node := range nodes {
		state := states[node.ID]
		if state != nil && state.FailedLike() {
			return store.RunFailed, node.ID, state.ErrorCode, state.Error
		}
	}
	return store.RunCompleted, "", "", ""
}

func untilSatisfied(until spec.UntilSpec, node store.NodeState) bool {
	if node.Status != store.NodeCompleted {
		return false
	}
	if until.ExitCode != nil && node.ExitCode != *until.ExitCode {
		return false
	}
	if until.OutputContains != "" && !strings.Contains(node.Output, until.OutputContains) {
		return false
	}
	return true
}

func shouldRetryAttempt(policy spec.AttemptsSpec, kind execution.Kind, attempt, max int) bool {
	if attempt >= max || len(policy.RetryOn) == 0 {
		return false
	}
	for _, value := range policy.RetryOn {
		if value == string(kind) {
			return true
		}
	}
	return false
}

func joinFeedback(values ...string) string {
	var clean []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			clean = append(clean, strings.TrimSpace(value))
		}
	}
	return strings.Join(clean, "\n\n")
}

func (r *Runner) finalizeWorktree(state *store.RunState, terminalStatus string) error {
	value := state.Worktree
	if value == nil || !value.Enabled || value.Removed || value.Path == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	status, inspectErr := gitworktree.Inspect(ctx, value.Path)
	if inspectErr == nil {
		value.Dirty = status.Dirty
	} else {
		value.CleanupError = inspectErr.Error()
	}
	if terminalStatus != store.RunCompleted {
		value.RetainedReason = terminalStatus
		return nil
	}
	data := map[string]any{"path": value.Path, "branch": value.Branch, "dirty": value.Dirty}
	if value.Cleanup == "manual" {
		value.RetainedReason = "manual_cleanup"
		data["reason"] = value.RetainedReason
		return r.commit(state, "worktree.retained", "", data)
	}
	if value.CleanupError != "" {
		value.RetainedReason = "inspection_failed"
		data["reason"], data["error"] = value.RetainedReason, value.CleanupError
		return r.commit(state, "worktree.retained", "", data)
	}
	if value.Dirty {
		value.RetainedReason = "uncommitted_changes"
		data["reason"] = value.RetainedReason
		return r.commit(state, "worktree.retained", "", data)
	}
	if err := gitworktree.Remove(ctx, value.RepositoryRoot, value.Path, false); err != nil {
		value.CleanupError = err.Error()
		value.RetainedReason = "cleanup_failed"
		data["reason"], data["error"] = value.RetainedReason, value.CleanupError
		return r.commit(state, "worktree.retained", "", data)
	}
	value.Removed = true
	value.RemovedAt = time.Now().UTC()
	value.RetainedReason = ""
	branchRemoved, branchErr := gitworktree.DeleteBranchIfUnchanged(ctx, value.RepositoryRoot, value.Branch, value.BaseCommit)
	value.BranchRemoved = branchRemoved
	if branchErr != nil {
		value.BranchCleanupError = branchErr.Error()
	}
	return r.commit(state, "worktree.removed", "", map[string]any{"path": value.Path, "branch": value.Branch, "branch_removed": branchRemoved, "branch_cleanup_error": value.BranchCleanupError})
}

func nodeContext(parent context.Context, timeout string) (context.Context, context.CancelFunc, error) {
	if strings.TrimSpace(timeout) == "" {
		ctx, cancel := context.WithCancel(parent)
		return ctx, cancel, nil
	}
	duration, err := time.ParseDuration(timeout)
	if err != nil || duration <= 0 {
		return nil, func() {}, fmt.Errorf("invalid timeout %q", timeout)
	}
	ctx, cancel := context.WithTimeout(parent, duration)
	return ctx, cancel, nil
}

func newID() (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(bytes), nil
}

// StableNodeOrder is exposed for tests and future parallel scheduling.
func StableNodeOrder(nodes []spec.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	sort.Strings(out)
	return out
}
