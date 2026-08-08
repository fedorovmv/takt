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
	"strconv"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/definition"
	"takt/internal/diagnostic"
	"takt/internal/domainadapter"
	"takt/internal/execution"
	"takt/internal/gitworktree"
	"takt/internal/redact"
	"takt/internal/spec"
	"takt/internal/store"
)

var ErrWaiting = fmt.Errorf("workflow is waiting for input")
var ErrPaused = fmt.Errorf("workflow is paused")
var ErrAbandoned = fmt.Errorf("workflow was abandoned")

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
	workflow         *spec.Workflow
	config           *spec.Config
	workflowPath     string
	configPath       string
	controlWorkspace string
	workspace        string
	commands         command.Resolver
	store            store.Repository
	assistants       assistant.Resolver
	adapters         domainadapter.Resolver
	redactor         *redact.Redactor
	startOptions     StartOptions
	inheritedPolicy  assistant.Policy
}

// Definition is the immutable workflow definition and path context supplied
// to a Runner. Runtime does not resolve profiles or transport arguments.
type Definition struct {
	Workflow         *spec.Workflow
	Config           *spec.Config
	WorkflowPath     string
	ConfigPath       string
	ControlWorkspace string
}

// Dependencies are runtime capabilities supplied by the composition root.
// The interfaces remain deliberately concrete-to-domain rather than generic DI.
type Dependencies struct {
	Commands   command.Resolver
	Store      store.Repository
	Assistants assistant.Resolver
	Adapters   domainadapter.Resolver
	Redactor   *redact.Redactor
}

type StartOptions struct {
	RunID              string
	ParentRunID        string
	ParentNodeID       string
	ExecutionWorkspace string
	Worktree           *bool
	WorktreeBase       string
	KeepWorktree       bool
	AllowDirty         bool
	InheritedPolicy    *assistant.Policy
}

func NewWithDependencies(def Definition, deps Dependencies) *Runner {
	return &Runner{
		workflow: def.Workflow, config: def.Config, workflowPath: def.WorkflowPath, configPath: def.ConfigPath,
		controlWorkspace: def.ControlWorkspace, workspace: def.ControlWorkspace,
		commands: deps.Commands, store: deps.Store, assistants: deps.Assistants, adapters: deps.Adapters,
		redactor: deps.Redactor,
	}
}

// CommandResolver exposes the immutable command lookup configured for this runner.
// Callers may inspect it for preflight validation but cannot replace runtime dependencies.
func (r *Runner) CommandResolver() command.Resolver { return r.commands }

func NewCommandResolver(workflowPath, executionWorkspace, controlWorkspace string) command.Resolver {
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
	r.workspace = workspace
	r.commands = NewCommandResolver(r.workflowPath, workspace, r.controlWorkspace)
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
	if strings.TrimSpace(options.ExecutionWorkspace) != "" {
		executionWorkspace, err := filepath.Abs(options.ExecutionWorkspace)
		if err != nil {
			return nil, fmt.Errorf("resolve execution workspace: %w", err)
		}
		info, err := os.Stat(executionWorkspace)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("execution workspace is unavailable at %s", executionWorkspace)
		}
		r.SetExecutionWorkspace(executionWorkspace)
	}
	worktreeSpec := r.workflow.Worktree
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
		info, prepareErr := gitworktree.Prepare(ctx, r.controlWorkspace, id, r.workflow.Metadata.Name, gitworktree.Options{
			Base: worktreeSpec.Base, BranchPrefix: worktreeSpec.BranchPrefix, AllowDirty: worktreeSpec.AllowDirty,
		})
		if prepareErr != nil {
			return nil, prepareErr
		}
		r.workspace = info.ExecutionWorkspace
		r.commands = NewCommandResolver(r.workflowPath, r.workspace, r.controlWorkspace)
		worktreeState = &store.WorktreeState{
			Enabled: true, RepositoryRoot: info.RepositoryRoot, ControlWorkspace: info.ControlWorkspace,
			ExecutionWorkspace: info.ExecutionWorkspace, Path: info.Path, Branch: info.Branch,
			BaseRef: info.BaseRef, BaseCommit: info.BaseCommit, Cleanup: worktreeSpec.Cleanup, BaseDirty: info.BaseDirty,
		}
	}
	fingerprints, err := definition.Compute(r.workflow, r.config, r.workflowPath, r.configPath, r.commands)
	if err != nil {
		r.rollbackPreparedWorktree(worktreeState)
		return nil, err
	}
	now := time.Now().UTC()
	state := &store.RunState{
		ID: id, Status: store.RunRunning, ParentRunID: options.ParentRunID, ParentNodeID: options.ParentNodeID, WorkflowPath: r.workflowPath, ConfigPath: r.configPath,
		Workspace: r.controlWorkspace, ExecutionWorkspace: r.workspace, Worktree: worktreeState, RunOptions: runOptionsState(options), InheritedPolicy: policyState(r.inheritedPolicy, nil), Input: input, Nodes: map[string]*store.NodeState{},
		Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now, ExecutorPID: os.Getpid(), HeartbeatAt: &now,
		WorkflowFingerprint: fingerprints.Workflow,
		ConfigFingerprint:   fingerprints.Config,
		CommandsFingerprint: fingerprints.Commands,
	}
	for _, node := range r.workflow.Nodes {
		state.Nodes[node.ID] = &store.NodeState{Status: store.NodePending, Path: canonicalNodePath(node.ID), Hidden: node.Hidden, PublicParent: node.PublicParent}
	}
	data := map[string]any{"workflow": r.workflow.Metadata.Name, "execution_workspace": r.workspace}
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
	info, err := gitworktree.Prepare(ctx, r.controlWorkspace, state.ID, internal.WorkflowName, gitworktree.Options{
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
	previousWorkspace := r.workspace
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
	actual, err := definition.Compute(r.workflow, r.config, r.workflowPath, r.configPath, r.commands)
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
	if state.Status == store.RunCompleted || state.Status == store.RunCancelled || state.Status == store.RunAbandoned {
		return state, nil
	}
	if state.Status == store.RunFailed {
		return state, &RunFailedError{RunID: state.ID, NodeID: state.CurrentNode, Code: state.ErrorCode, Cause: state.Error}
	}
	if err := r.VerifyDefinitions(state); err != nil {
		return state, err
	}
	if requested, reason := r.abandonmentRequested(state.ID); requested {
		return r.abandonState(state, reason)
	}
	if state.CancelRequested || r.cancellationRequested(state.ID) {
		return r.cancelState(state, "cancel_requested")
	}
	if state.Waiting != nil {
		if state.Waiting.Kind != "child_run" {
			if state.Status == store.RunPaused {
				return state, ErrPaused
			}
			return state, ErrWaiting
		}
		// A parent can be paused while it is waiting for a governed child. If the
		// child finishes while the tree remains paused, explicit resume must be
		// able to consume that durable child_run suspension instead of leaving the
		// parent in running+waiting with no executor.
		if state.Status == store.RunWaiting || (state.Status == store.RunPaused && state.PausedFrom == store.RunWaiting) {
			if node := state.Nodes[state.Waiting.NodeID]; node != nil {
				node.Status = store.NodePending
			}
			state.Waiting = nil
		}
	}
	state.PauseRequested = false
	state.PausedAt = nil
	state.PausedFrom = ""
	state.Status = store.RunRunning
	state.CurrentNodes = nil
	state.Error, state.ErrorCode = "", ""
	if err := r.commit(state, "run.resumed", "", nil); err != nil {
		return state, err
	}
	return r.resume(ctx, state)
}

func (r *Runner) resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	if requested, reason := r.abandonmentRequested(state.ID); requested {
		return r.abandonState(state, reason)
	}
	if state.CancelRequested {
		return r.cancelState(state, "cancel_requested")
	}
	err := r.executeGraph(ctx, state, r.workflow.Nodes, nil)
	if errors.Is(err, ErrWaiting) {
		return state, ErrWaiting
	}
	if errors.Is(err, ErrPaused) {
		now := time.Now().UTC()
		previousStatus := state.Status
		state.Status = store.RunPaused
		state.PauseRequested = false
		if state.PausedFrom == "" {
			state.PausedFrom = previousStatus
		}
		state.PausedAt = &now
		state.CurrentNode = ""
		state.CurrentNodes = nil
		if commitErr := r.commit(state, "run.paused", "", nil); commitErr != nil {
			return state, commitErr
		}
		// Clear the request only after the paused state is durable. If persistence
		// fails, the marker remains and recovery cannot silently discard an
		// operator decision.
		if value, ok := r.store.(operatorStore); ok {
			if clearErr := value.ClearPause(state.ID); clearErr != nil {
				return state, clearErr
			}
		}
		return state, ErrPaused
	}
	if errors.Is(err, ErrAbandoned) {
		requested, reason := r.abandonmentRequested(state.ID)
		if !requested {
			reason = "abandoned by operator"
		}
		return r.abandonState(state, reason)
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if requested, reason := r.abandonmentRequested(state.ID); requested {
				return r.abandonState(state, reason)
			}
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

	status, nodeID, code, cause := graphResult(r.workflow.Nodes, state.Nodes)
	state.CurrentNode = ""
	state.CurrentNodes = nil
	state.Status = status
	state.ErrorCode = code
	state.Error = cause
	state.Usage = aggregateRunUsage(state.Nodes)
	if status == store.RunCompleted {
		state.Output = defaultRunOutput(r.workflow.Nodes, state.Nodes)
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
		if requested, _ := r.abandonmentRequested(state.ID); requested {
			return ErrAbandoned
		}
		if state.CancelRequested || r.cancellationRequested(state.ID) {
			return context.Canceled
		}
		if r.pauseRequested(state.ID) {
			state.PauseRequested = true
			return ErrPaused
		}
		progress := false
		runnable := make([]spec.Node, 0, len(nodes))
		for _, node := range nodes {
			ns := state.Nodes[node.ID]
			if ns == nil {
				ns = &store.NodeState{Status: store.NodePending, Path: canonicalNodePath(node.ID)}
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
			// Safe pause is a scheduling boundary, not merely an outer-loop hint.
			// Re-check before every sequential launch so a pause that arrives while
			// an earlier node is finishing cannot publish the next external task or
			// start another hook/attempt in the same scheduler wave.
			if r.pauseRequested(state.ID) {
				state.PauseRequested = true
				return ErrPaused
			}
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
	hooks := mergeHooks(r.workflow.Hooks, node.Hooks)
	if node.Internal != nil {
		hooks = spec.HookSet{}
	}
	for ns.Attempts < max {
		if r.pauseRequested(state.ID) {
			state.PauseRequested = true
			return ErrPaused
		}
		if err := r.awaitRetry(ctx, state, node.ID); err != nil {
			return err
		}
		disposition, err := r.runAttempt(ctx, state, node, hooks, loopPrevious, max)
		if err != nil {
			return err
		}
		if disposition == attemptRetry {
			continue
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
	cause := context.Cause(ctx)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(cause, ErrIdleTimeout) {
		kind = execution.KindTimedOut
	}
	if cause == nil {
		cause = ctx.Err()
	}
	return &execution.Error{Kind: kind, ExitCode: -1, Op: op, Err: cause}
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
	AssistantEvents  []assistant.Event
	DomainOperation  *store.DomainOperationState
	Sandbox          *store.SandboxState
}

func (r *Runner) execute(ctx context.Context, state *store.RunState, node spec.Node, loopPrevious map[string]store.NodeState) (execResult, error) {
	action := r.actionContext(state, node, loopPrevious)
	switch {
	case node.Bash != "":
		return r.executeBashAction(ctx, state, node, action)
	case node.Script != nil:
		return r.executeScriptAction(ctx, state, node, action)
	case node.Approval != nil:
		return r.executeApprovalAction(state, node, action)
	case node.LoopGroup != nil:
		return r.runLoopGroup(ctx, state, node)
	case node.WorkflowRun != nil:
		return r.runChildWorkflow(ctx, state, node, action.local, action.feedback, action.artifacts)
	case node.Internal != nil:
		return r.executeInternalAction(ctx, state, node)
	case node.Adapter != nil:
		return r.executeAdapterAction(ctx, state, node, action)
	case node.Command != "" || node.Prompt != "":
		return r.executeAssistantAction(ctx, state, node, action)
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
	} else if len(parentState.LoopIterations) > 0 {
		last := parentState.LoopIterations[len(parentState.LoopIterations)-1]
		if last.Satisfied {
			check, ok := last.Nodes[last.UntilNode]
			if !ok {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("durable satisfied iteration %d is missing until node %q", last.Iteration, last.UntilNode)}
			}
			return execResult{Output: check.Output, Stdout: check.Stdout, Stderr: check.Stderr, ExitCode: check.ExitCode, SessionID: check.SessionID, Truncated: check.OutputTruncated}, nil
		}
		// loop.iteration.completed is durable before the next iteration starts.
		// A crash in that boundary window must continue after the last durable
		// iteration rather than replay iteration 1 and its side effects.
		startIteration = len(parentState.LoopIterations) + 1
	}
	if startIteration > parent.LoopGroup.MaxIterations {
		return execResult{}, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "loop_group", Err: fmt.Errorf("loop_group %q exhausted %d iterations", parent.ID, parent.LoopGroup.MaxIterations)}
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
				state.Nodes[child.ID] = &store.NodeState{Status: store.NodePending, Path: canonicalNodePath(child.ID), Hidden: child.Hidden, PublicParent: child.PublicParent}
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
		satisfied := untilSatisfied(parent.LoopGroup.Until, check)
		historyNodes := cloneLoopNodes(local)
		parentState.LoopPrevious = cloneLoopNodes(local)
		if len(parentState.LoopIterations) >= spec.MaxLoopGroupIterations {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("loop_group %q iteration history exceeds %d entries", parent.ID, spec.MaxLoopGroupIterations)}
		}
		parentState.LoopIterations = append(parentState.LoopIterations, store.LoopIterationState{
			Iteration: iteration, Nodes: historyNodes, UntilNode: parent.LoopGroup.Until.Node,
			ExitCode: check.ExitCode, Status: check.Status, Satisfied: satisfied, CompletedAt: time.Now().UTC(),
		})
		parentState.LoopIteration = 0
		if err := r.commit(state, "loop.iteration.completed", parent.ID, map[string]any{
			"iteration": iteration, "until_node": parent.LoopGroup.Until.Node,
			"exit_code": check.ExitCode, "status": check.Status, "satisfied": satisfied,
		}); err != nil {
			return execResult{}, err
		}
		if satisfied {
			return execResult{Output: check.Output, Stdout: check.Stdout, Stderr: check.Stderr, ExitCode: check.ExitCode, SessionID: check.SessionID, Truncated: check.OutputTruncated}, nil
		}
		previous = local
	}
	return execResult{}, &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "loop_group", Err: fmt.Errorf("loop_group %q exhausted %d iterations", parent.ID, parent.LoopGroup.MaxIterations)}
}

func cloneLoopNodes(in map[string]store.NodeState) map[string]store.NodeState {
	if in == nil {
		return nil
	}
	// Loop history is an immutable durable snapshot. Use a structural copy so
	// LoopPrevious and LoopIterations[last].Nodes never share nested maps,
	// slices, pointers, or execution state that a later resume/retry may mutate.
	raw, err := json.Marshal(in)
	if err == nil {
		var out map[string]store.NodeState
		if json.Unmarshal(raw, &out) == nil {
			return out
		}
	}
	out := make(map[string]store.NodeState, len(in))
	for id, node := range in {
		out[id] = node
	}
	return out
}

func (r *Runner) runHooks(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, local map[string]store.NodeState) (string, string, error) {
	for _, hook := range hooks {
		rendered, renderErr := renderTemplate(hook.Bash, state, local, state.Nodes[node.ID].Feedback, r.store.ArtifactsDir(state.ID))
		if renderErr != nil {
			return "fail", renderErr.Error(), &execution.Error{Kind: execution.KindInternal, Op: "render hook", Err: renderErr}
		}
		result, err := r.runBash(ctx, node, rendered)
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
	if result.DomainOperation != nil {
		copy := *result.DomainOperation
		copy.Capabilities = append([]string(nil), result.DomainOperation.Capabilities...)
		node.DomainOperation = &copy
	}
	if result.Sandbox != nil {
		copy := *result.Sandbox
		node.Sandbox = &copy
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
	diagnostic := r.diagnosticFor(ns.ErrorCode, err, false)
	ns.Diagnostic = &diagnostic
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node."+status, nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "exit_code": ns.ExitCode, "usage": ns.Usage, "diagnostic": ns.Diagnostic})
}

func (r *Runner) finishNodeError(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeErrored
	ns.ErrorCode = code
	ns.Error = err.Error()
	diagnostic := r.diagnosticFor(code, err, false)
	ns.Diagnostic = &diagnostic
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node.errored", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "usage": ns.Usage, "diagnostic": ns.Diagnostic})
}

func (r *Runner) finishNodeFailure(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeFailed
	ns.ErrorCode = code
	ns.Error = err.Error()
	diagnostic := r.diagnosticFor(code, err, false)
	ns.Diagnostic = &diagnostic
	applyExecResult(ns, result)
	mergeRunArtifacts(state, result.Artifacts)

	state.CurrentNode = ""
	state.CurrentNodes = nil
	return r.commit(state, "node.failed", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "usage": ns.Usage, "diagnostic": ns.Diagnostic})
}

func (r *Runner) commit(state *store.RunState, eventType, nodeID string, data map[string]any) error {
	now := time.Now().UTC()
	state.ExecutorPID = os.Getpid()
	state.HeartbeatAt = &now
	for id, node := range state.Nodes {
		if node != nil && node.Path == "" {
			node.Path = canonicalNodePath(id)
		}
	}
	if nodeID != "" {
		if data == nil {
			data = map[string]any{}
		}
		if _, exists := data["node_path"]; !exists {
			data["node_path"] = canonicalNodePath(nodeID)
		}
	}
	if r.redactor == nil {
		return fmt.Errorf("runtime redactor dependency is required")
	}
	persisted, err := cloneRunStateForPersistence(state)
	if err != nil {
		return err
	}
	redactRunState(r.redactor, persisted)
	eventData := r.redactor.Map(data)
	if err := r.store.Commit(persisted, store.Event{Type: eventType, NodeID: nodeID, Data: eventData}); err != nil {
		return err
	}
	state.Revision = persisted.Revision
	state.UpdatedAt = persisted.UpdatedAt
	return nil
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
	if node.AlwaysRun {
		rule = "all_done"
	} else if rule == "" {
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

func (r *Runner) diagnosticFor(code string, err error, retryable bool) store.DiagnosticState {
	value := diagnostic.FromError(code, err, retryable, r.controlWorkspace, r.workspace)
	return store.DiagnosticState{Code: value.Code, Kind: value.Kind, Op: value.Op, Message: value.Message, Fingerprint: value.Fingerprint, Retryable: value.Retryable}
}

func cloneDiagnostic(value *store.DiagnosticState) *store.DiagnosticState {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func retryBackoff(policy spec.AttemptsSpec, nextAttempt int) (time.Duration, error) {
	if policy.Backoff == nil {
		return 0, nil
	}
	initial, err := time.ParseDuration(strings.TrimSpace(policy.Backoff.Initial))
	if err != nil || initial <= 0 {
		return 0, fmt.Errorf("attempts.backoff.initial must be a positive duration")
	}
	multiplier := policy.Backoff.Multiplier
	if multiplier == 0 {
		multiplier = 2
	}
	if multiplier < 1 {
		return 0, fmt.Errorf("attempts.backoff.multiplier must be >= 1")
	}
	maximum := time.Duration(0)
	if strings.TrimSpace(policy.Backoff.Max) != "" {
		maximum, err = time.ParseDuration(strings.TrimSpace(policy.Backoff.Max))
		if err != nil || maximum <= 0 {
			return 0, fmt.Errorf("attempts.backoff.max must be a positive duration")
		}
	}
	delay := float64(initial)
	for attempt := 2; attempt < nextAttempt; attempt++ {
		delay *= multiplier
		if maximum > 0 && delay >= float64(maximum) {
			delay = float64(maximum)
			break
		}
	}
	value := time.Duration(delay)
	if maximum > 0 && value > maximum {
		value = maximum
	}
	if policy.Backoff.Jitter && value > 0 {
		var b [1]byte
		if _, err := rand.Read(b[:]); err == nil {
			// Full decisions are persisted, so random jitter does not change after
			// restart. Keep the delay in [50%, 100%].
			factor := 0.5 + (float64(b[0])/255.0)*0.5
			value = time.Duration(float64(value) * factor)
		}
	}
	return value, nil
}

func (r *Runner) scheduleRetry(state *store.RunState, node spec.Node, kind, phase, feedback string) error {
	ns := state.Nodes[node.ID]
	delay, err := retryBackoff(node.Attempts, ns.Attempts+1)
	if err != nil {
		return err
	}
	fingerprint := ""
	if ns.Diagnostic != nil {
		fingerprint = ns.Diagnostic.Fingerprint
	}
	data := map[string]any{"feedback": feedback, "phase": phase, "kind": kind, "next_attempt": ns.Attempts + 1, "fingerprint": fingerprint}
	if delay > 0 {
		notBefore := time.Now().UTC().Add(delay)
		ns.Retry = &store.RetryState{NextAttempt: ns.Attempts + 1, NotBefore: notBefore, Delay: delay.String(), Kind: kind, Fingerprint: fingerprint}
		data["delay"] = delay.String()
		data["not_before"] = notBefore
		data["fingerprint"] = fingerprint
		return r.commit(state, "node.retry.scheduled", node.ID, data)
	}
	ns.Retry = nil
	return r.commit(state, "node.retry", node.ID, data)
}

func (r *Runner) awaitRetry(ctx context.Context, state *store.RunState, nodeID string) error {
	ns := state.Nodes[nodeID]
	if ns == nil || ns.Retry == nil {
		return nil
	}
	for {
		if r.pauseRequested(state.ID) {
			state.PauseRequested = true
			return ErrPaused
		}
		if state.CancelRequested || r.cancellationRequested(state.ID) {
			return context.Canceled
		}
		remaining := time.Until(ns.Retry.NotBefore)
		if remaining <= 0 {
			break
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	completed := ns.Retry
	ns.Retry = nil
	return r.commit(state, "node.retry.ready", nodeID, map[string]any{"next_attempt": completed.NextAttempt, "delay": completed.Delay, "fingerprint": completed.Fingerprint})
}

func canonicalNodePath(id string) string {
	parts := strings.Split(strings.TrimSpace(id), "__")
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	path := "/" + parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err == nil && n > 0 {
			path += fmt.Sprintf("[%d]", n)
		} else {
			path += "/" + part
		}
	}
	return path
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
