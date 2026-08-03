package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	Workflow     *spec.Workflow
	Config       *spec.Config
	WorkflowPath string
	ConfigPath   string
	Workspace    string
	Commands     command.Resolver
	Store        store.Repository
}

func New(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath, workspace string) *Runner {
	workflowDir := filepath.Dir(workflowPath)
	home, _ := os.UserHomeDir()
	dirs := []string{
		filepath.Join(workspace, ".takt", "commands"),
		filepath.Join(workflowDir, "commands"),
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".takt", "commands"))
	}
	return &Runner{wf, cfg, workflowPath, configPath, workspace, command.Resolver{Dirs: dirs}, store.FS{Workspace: workspace}}
}

func (r *Runner) Start(ctx context.Context, input string) (*store.RunState, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	fingerprints, err := definition.Compute(r.Workflow, r.Config, r.WorkflowPath, r.ConfigPath, r.Commands)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	state := &store.RunState{
		ID: id, Status: store.RunRunning, WorkflowPath: r.WorkflowPath, ConfigPath: r.ConfigPath,
		Workspace: r.Workspace, Input: input, Nodes: map[string]*store.NodeState{},
		Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now,
		WorkflowFingerprint: fingerprints.Workflow,
		ConfigFingerprint:   fingerprints.Config,
		CommandsFingerprint: fingerprints.Commands,
	}
	for _, node := range r.Workflow.Nodes {
		state.Nodes[node.ID] = &store.NodeState{Status: store.NodePending}
	}
	if err := r.commit(state, "run.started", "", map[string]any{"workflow": r.Workflow.Metadata.Name}); err != nil {
		return nil, err
	}
	return r.resume(ctx, state)
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
		return state, ErrWaiting
	}
	state.Status = store.RunRunning
	state.Error, state.ErrorCode = "", ""
	if err := r.commit(state, "run.resumed", "", nil); err != nil {
		return state, err
	}
	return r.resume(ctx, state)
}

func (r *Runner) resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	err := r.executeGraph(ctx, state, r.Workflow.Nodes, nil)
	if errors.Is(err, ErrWaiting) {
		return state, ErrWaiting
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			state.Status = store.RunCancelled
			state.ErrorCode = string(execution.KindOf(err))
			state.Error = err.Error()
			state.CurrentNode = ""
			if commitErr := r.commit(state, "run.cancelled", "", map[string]any{"error": err.Error()}); commitErr != nil {
				return state, commitErr
			}
			return state, err
		}
		return state, err
	}

	status, nodeID, code, cause := graphResult(r.Workflow.Nodes, state.Nodes)
	state.CurrentNode = ""
	state.Status = status
	state.ErrorCode = code
	state.Error = cause
	switch status {
	case store.RunCompleted:
		if err := r.commit(state, "run.completed", "", nil); err != nil {
			return state, err
		}
		return state, nil
	case store.RunCancelled:
		if err := r.commit(state, "run.cancelled", nodeID, map[string]any{"error": cause, "code": code}); err != nil {
			return state, err
		}
		return state, &RunFailedError{RunID: state.ID, NodeID: nodeID, Code: code, Cause: cause}
	default:
		if err := r.commit(state, "run.failed", nodeID, map[string]any{"error": cause, "code": code}); err != nil {
			return state, err
		}
		return state, &RunFailedError{RunID: state.ID, NodeID: nodeID, Code: code, Cause: cause}
	}
}

func (r *Runner) executeGraph(ctx context.Context, state *store.RunState, nodes []spec.Node, previous map[string]store.NodeState) error {
	for {
		progress := false
		for _, node := range nodes {
			ns := state.Nodes[node.ID]
			if ns == nil {
				ns = &store.NodeState{Status: store.NodePending}
				state.Nodes[node.ID] = ns
			}
			if ns.Terminal() {
				continue
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
	for ns.Attempts < max {
		ns.Attempts++
		ns.Status = store.NodeRunning
		ns.Error, ns.ErrorCode = "", ""
		state.CurrentNode = node.ID
		if err := r.commit(state, "node.started", node.ID, map[string]any{"attempt": ns.Attempts}); err != nil {
			return err
		}

		attemptCtx, cancel, err := nodeContext(ctx, node.Timeout)
		if err != nil {
			return r.finishNodeError(state, node.ID, "invalid_timeout", err, execResult{})
		}

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
			if err := r.commit(state, "node.suspended", node.ID, map[string]any{"reason": "approval"}); err != nil {
				return err
			}
			return ErrWaiting
		}
		ns.Output, ns.ExitCode, ns.SessionID, ns.OutputTruncated = result.Output, result.ExitCode, result.SessionID, result.Truncated
		if execErr != nil && node.AllowFailure && execution.IsExit(execErr) {
			execErr = nil
			if err := r.commit(state, "node.failure_allowed", node.ID, map[string]any{"exit_code": result.ExitCode}); err != nil {
				cancel()
				return err
			}
		}
		if execErr != nil {
			kind := execution.KindOf(execErr)
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

		cancel()
		ns.Status = store.NodeCompleted
		state.CurrentNode = ""
		if err := r.commit(state, "node.completed", node.ID, map[string]any{"attempts": ns.Attempts, "exit_code": ns.ExitCode, "output_truncated": ns.OutputTruncated}); err != nil {
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
	Output    string
	ExitCode  int
	SessionID string
	Truncated bool
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
	case node.Approval != nil:
		if answer, ok := state.Approvals[node.ID]; ok {
			output := ""
			if node.Approval.CaptureResponse {
				output = answer
			}
			return execResult{Output: output, ExitCode: 0}, nil
		}
		message := renderTemplate(node.Approval.Message, state, local, feedback, artifacts)
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: message}
		state.Nodes[node.ID].Status = store.NodeWaiting
		if err := r.commit(state, "approval.requested", node.ID, map[string]any{"message": message}); err != nil {
			return execResult{}, err
		}
		return execResult{}, ErrWaiting
	case node.LoopGroup != nil:
		return r.runLoopGroup(ctx, state, node)
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
		adapter, err := (assistant.Factory{Config: r.Config}).Resolve(assistantName)
		if err != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve assistant", Err: err}
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
		result, err := adapter.Run(ctx, assistant.Request{Prompt: prompt, Workspace: r.Workspace, ModelName: modelName, Model: model, SessionMode: sessionMode, SessionID: sessionID, NativeHooks: node.NativeHooks})
		return execResult{Output: result.Output, ExitCode: result.ExitCode, SessionID: result.SessionID, Truncated: result.Truncated}, err
	default:
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "execute node", Err: fmt.Errorf("unsupported node %q", node.ID)}
	}
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
	var previous map[string]store.NodeState
	for iteration := 1; iteration <= parent.LoopGroup.MaxIterations; iteration++ {
		for _, child := range parent.LoopGroup.Nodes {
			if _, exists := state.Nodes[child.ID]; exists {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("child node id %q collides with existing runtime state", child.ID)}
			}
		}
		for _, child := range parent.LoopGroup.Nodes {
			state.Nodes[child.ID] = &store.NodeState{Status: store.NodePending}
		}
		if err := r.commit(state, "loop.iteration.started", parent.ID, map[string]any{"iteration": iteration}); err != nil {
			return execResult{}, err
		}
		if err := r.executeGraph(ctx, state, parent.LoopGroup.Nodes, previous); err != nil {
			return execResult{}, err
		}
		local := make(map[string]store.NodeState, len(parent.LoopGroup.Nodes))
		for _, child := range parent.LoopGroup.Nodes {
			local[child.ID] = *state.Nodes[child.ID]
			delete(state.Nodes, child.ID)
		}
		check, exists := local[parent.LoopGroup.Until.Node]
		if !exists {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "loop_group", Err: fmt.Errorf("until node %q missing", parent.LoopGroup.Until.Node)}
		}
		state.Nodes[parent.ID].LoopPrevious = local
		if err := r.commit(state, "loop.iteration.completed", parent.ID, map[string]any{
			"iteration": iteration, "until_node": parent.LoopGroup.Until.Node,
			"exit_code": check.ExitCode, "status": check.Status,
		}); err != nil {
			return execResult{}, err
		}
		if untilSatisfied(parent.LoopGroup.Until, check) {
			return execResult{Output: check.Output, ExitCode: check.ExitCode, SessionID: check.SessionID, Truncated: check.OutputTruncated}, nil
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
	ns.Output, ns.ExitCode, ns.SessionID, ns.OutputTruncated = result.Output, result.ExitCode, result.SessionID, result.Truncated
	state.CurrentNode = ""
	return r.commit(state, "node."+status, nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode, "exit_code": ns.ExitCode})
}

func (r *Runner) finishNodeError(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeErrored
	ns.ErrorCode = code
	ns.Error = err.Error()
	ns.Output, ns.ExitCode, ns.SessionID, ns.OutputTruncated = result.Output, result.ExitCode, result.SessionID, result.Truncated
	state.CurrentNode = ""
	return r.commit(state, "node.errored", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode})
}

func (r *Runner) finishNodeFailure(state *store.RunState, nodeID, code string, err error, result execResult) error {
	ns := state.Nodes[nodeID]
	ns.Status = store.NodeFailed
	ns.ErrorCode = code
	ns.Error = err.Error()
	ns.Output, ns.ExitCode, ns.SessionID, ns.OutputTruncated = result.Output, result.ExitCode, result.SessionID, result.Truncated
	state.CurrentNode = ""
	return r.commit(state, "node.failed", nodeID, map[string]any{"error": ns.Error, "code": ns.ErrorCode})
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

func joinFeedback(values ...string) string {
	var clean []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			clean = append(clean, strings.TrimSpace(value))
		}
	}
	return strings.Join(clean, "\n\n")
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
