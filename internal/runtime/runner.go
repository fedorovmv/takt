package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/store"
)

var ErrWaiting = fmt.Errorf("workflow is waiting for input")

type Runner struct {
	Workflow     *spec.Workflow
	Config       *spec.Config
	WorkflowPath string
	ConfigPath   string
	Workspace    string
	Commands     command.Resolver
	Store        store.FS
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
	now := time.Now().UTC()
	state := &store.RunState{
		ID: id, Status: "running", WorkflowPath: r.WorkflowPath, ConfigPath: r.ConfigPath,
		Workspace: r.Workspace, Input: input, Nodes: map[string]*store.NodeState{},
		Approvals: map[string]string{}, CreatedAt: now, UpdatedAt: now,
	}
	for _, n := range r.Workflow.Nodes {
		state.Nodes[n.ID] = &store.NodeState{Status: "pending"}
	}
	if err := r.Store.Save(state); err != nil {
		return nil, err
	}
	r.event(state, "run.started", "", map[string]any{"workflow": r.Workflow.Metadata.Name})
	return r.resume(ctx, state)
}

func (r *Runner) Resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	if state.Status == "completed" || state.Status == "failed" || state.Status == "cancelled" {
		return state, nil
	}
	state.Status = "running"
	state.Waiting = nil
	return r.resume(ctx, state)
}

func (r *Runner) resume(ctx context.Context, state *store.RunState) (*store.RunState, error) {
	for {
		progress := false
		for _, n := range r.Workflow.Nodes {
			ns := state.Nodes[n.ID]
			if ns.Status == "completed" || ns.Status == "skipped" {
				continue
			}
			ready, skip := dependenciesReady(n, state)
			if skip {
				ns.Status = "skipped"
				progress = true
				r.event(state, "node.skipped", n.ID, nil)
				_ = r.Store.Save(state)
				continue
			}
			if !ready {
				continue
			}
			ok, err := evalWhen(n.When, state)
			if err != nil {
				return r.fail(state, n.ID, err)
			}
			if !ok {
				ns.Status = "skipped"
				progress = true
				r.event(state, "node.skipped", n.ID, map[string]any{"reason": "when"})
				_ = r.Store.Save(state)
				continue
			}
			progress = true
			if err := r.runNode(ctx, state, n, nil); err != nil {
				if err == ErrWaiting {
					return state, ErrWaiting
				}
				return r.fail(state, n.ID, err)
			}
		}
		if allTerminal(state) {
			state.Status = "completed"
			state.CurrentNode = ""
			_ = r.Store.Save(state)
			r.event(state, "run.completed", "", nil)
			return state, nil
		}
		if !progress {
			return r.fail(state, "", fmt.Errorf("workflow made no progress; unresolved dependencies or failed trigger rules"))
		}
	}
}

func (r *Runner) runNode(ctx context.Context, state *store.RunState, node spec.Node, loopPrev map[string]store.NodeState) error {
	ns := state.Nodes[node.ID]
	if ns == nil {
		ns = &store.NodeState{Status: "pending"}
		state.Nodes[node.ID] = ns
	}
	max := node.Attempts.Max
	if max <= 0 {
		max = 1
	}
	hooks := mergeHooks(r.Workflow.Hooks, node.Hooks)
	for ns.Attempts < max {
		ns.Attempts++
		ns.Status = "running"
		state.CurrentNode = node.ID
		_ = r.Store.Save(state)
		r.event(state, "node.started", node.ID, map[string]any{"attempt": ns.Attempts})
		if decision, feedback, err := r.runHooks(ctx, state, node, hooks.BeforeNode, loopPrev); err != nil {
			return err
		} else if decision == "retry" {
			ns.Feedback = feedback
			_ = r.Store.Save(state)
			continue
		} else if decision == "fail" {
			return fmt.Errorf("before_node hook failed: %s", feedback)
		}

		result, err := r.execute(ctx, state, node, loopPrev)
		if err == ErrWaiting {
			// Waiting is a suspension point, not a failed/consumed attempt.
			ns.Attempts--
			if saveErr := r.Store.Save(state); saveErr != nil {
				return saveErr
			}
			return err
		}
		if err != nil && node.AllowFailure {
			// Preserve the command exit code as data for loop conditions or downstream nodes.
			err = nil
		}
		if err != nil {
			decision, feedback, hookErr := r.runHooks(ctx, state, node, hooks.OnFailure, loopPrev)
			if hookErr != nil {
				return hookErr
			}
			if decision == "retry" {
				ns.Feedback = joinFeedback(ns.Feedback, feedback, err.Error())
				_ = r.Store.Save(state)
				continue
			}
			return err
		}
		ns.Output, ns.ExitCode, ns.SessionID = result.Output, result.ExitCode, result.SessionID
		decision, feedback, err := r.runHooks(ctx, state, node, hooks.AfterNode, loopPrev)
		if err != nil {
			return err
		}
		if decision == "retry" {
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			_ = r.Store.Save(state)
			r.event(state, "node.retry", node.ID, map[string]any{"feedback": feedback})
			continue
		}
		if decision == "fail" {
			return fmt.Errorf("after_node hook failed: %s", feedback)
		}
		decision, feedback, err = r.runHooks(ctx, state, node, hooks.BeforeComplete, loopPrev)
		if err != nil {
			return err
		}
		if decision == "retry" {
			ns.Feedback = joinFeedback(ns.Feedback, feedback)
			_ = r.Store.Save(state)
			continue
		}
		if decision == "fail" {
			return fmt.Errorf("before_complete hook failed: %s", feedback)
		}
		ns.Status = "completed"
		state.CurrentNode = ""
		_ = r.Store.Save(state)
		r.event(state, "node.completed", node.ID, map[string]any{"attempts": ns.Attempts, "exit_code": ns.ExitCode})
		return nil
	}
	return fmt.Errorf("node %q exhausted %d attempts; feedback: %s", node.ID, max, ns.Feedback)
}

type execResult struct {
	Output    string
	ExitCode  int
	SessionID string
}

func (r *Runner) execute(ctx context.Context, state *store.RunState, node spec.Node, loopPrev map[string]store.NodeState) (execResult, error) {
	local := loopPrev
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
			out := ""
			if node.Approval.CaptureResponse {
				out = answer
			}
			return execResult{Output: out, ExitCode: 0}, nil
		}
		msg := renderTemplate(node.Approval.Message, state, local, feedback, artifacts)
		state.Status = "waiting"
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: msg}
		state.Nodes[node.ID].Status = "waiting"
		_ = r.Store.Save(state)
		r.event(state, "approval.requested", node.ID, map[string]any{"message": msg})
		return execResult{}, ErrWaiting
	case node.LoopGroup != nil:
		return r.runLoopGroup(ctx, state, node)
	case node.Command != "" || node.Prompt != "":
		prompt := node.Prompt
		assistantName, modelName := node.Assistant, node.Model
		if node.Command != "" {
			cmd, err := r.Commands.Resolve(node.Command)
			if err != nil {
				return execResult{}, err
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
			return execResult{}, fmt.Errorf("node %q does not resolve an assistant", node.ID)
		}
		model, ok := r.Config.Models[modelName]
		if !ok {
			return execResult{}, fmt.Errorf("node %q references unknown model %q", node.ID, modelName)
		}
		adapter, err := (assistant.Factory{Config: r.Config}).Resolve(assistantName)
		if err != nil {
			return execResult{}, err
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
		res, err := adapter.Run(ctx, assistant.Request{Prompt: prompt, Workspace: r.Workspace, ModelName: modelName, Model: model, SessionMode: sessionMode, SessionID: sessionID, NativeHooks: node.NativeHooks})
		if err != nil {
			return execResult{Output: res.Output, ExitCode: res.ExitCode, SessionID: res.SessionID}, err
		}
		return execResult{Output: res.Output, ExitCode: res.ExitCode, SessionID: res.SessionID}, nil
	}
	return execResult{}, fmt.Errorf("unsupported node %q", node.ID)
}

func (r *Runner) runLoopGroup(ctx context.Context, state *store.RunState, parent spec.Node) (execResult, error) {
	var previous map[string]store.NodeState
	for iteration := 1; iteration <= parent.LoopGroup.MaxIterations; iteration++ {
		local := map[string]store.NodeState{}
		for _, child := range parent.LoopGroup.Nodes {
			local[child.ID] = store.NodeState{Status: "pending"}
		}
		for {
			progress := false
			for _, child := range parent.LoopGroup.Nodes {
				cs := local[child.ID]
				if cs.Status == "completed" || cs.Status == "skipped" {
					continue
				}
				if !localDepsReady(child, local) {
					continue
				}
				progress = true
				// Temporarily expose child state through the parent node map.
				state.Nodes[child.ID] = &cs
				if err := r.runNode(ctx, state, child, previous); err != nil {
					return execResult{}, fmt.Errorf("loop %s iteration %d node %s: %w", parent.ID, iteration, child.ID, err)
				}
				local[child.ID] = *state.Nodes[child.ID]
				delete(state.Nodes, child.ID)
			}
			if localAllTerminal(local) {
				break
			}
			if !progress {
				return execResult{}, fmt.Errorf("loop %s made no progress", parent.ID)
			}
		}
		check := local[parent.LoopGroup.Until.Node]
		r.event(state, "loop.iteration.completed", parent.ID, map[string]any{"iteration": iteration, "until_node": parent.LoopGroup.Until.Node, "exit_code": check.ExitCode})
		if untilSatisfied(parent.LoopGroup.Until, check) {
			state.Nodes[parent.ID].LoopPrevious = local
			return execResult{Output: check.Output, ExitCode: check.ExitCode}, nil
		}
		previous = local
		state.Nodes[parent.ID].LoopPrevious = local
		_ = r.Store.Save(state)
	}
	return execResult{}, fmt.Errorf("loop_group %q exhausted %d iterations", parent.ID, parent.LoopGroup.MaxIterations)
}

func (r *Runner) runHooks(ctx context.Context, state *store.RunState, node spec.Node, hooks []spec.HookSpec, local map[string]store.NodeState) (string, string, error) {
	for _, h := range hooks {
		res, err := runBash(ctx, r.Workspace, renderTemplate(h.Bash, state, local, state.Nodes[node.ID].Feedback, r.Store.ArtifactsDir(state.ID)))
		if err == nil && res.ExitCode == 0 {
			continue
		}
		feedback := strings.TrimSpace(res.Output)
		if feedback == "" && err != nil {
			feedback = err.Error()
		}
		action := h.OnFailure.Action
		if action == "" {
			action = "fail"
		}
		r.event(state, "hook.failed", node.ID, map[string]any{"hook": h.ID, "action": action, "feedback": feedback})
		switch action {
		case "continue":
			continue
		case "retry":
			if h.OnFailure.Session == "fresh" && state.Nodes[node.ID] != nil {
				state.Nodes[node.ID].SessionID = ""
			}
			return "retry", feedback, nil
		case "fail":
			return "fail", feedback, nil
		default:
			return "", feedback, fmt.Errorf("hook %q has unsupported action %q", h.ID, action)
		}
	}
	return "continue", "", nil
}

func mergeHooks(global, local spec.HookSet) spec.HookSet {
	return spec.HookSet{
		BeforeNode:     append(append([]spec.HookSpec{}, global.BeforeNode...), local.BeforeNode...),
		AfterNode:      append(append([]spec.HookSpec{}, global.AfterNode...), local.AfterNode...),
		BeforeComplete: append(append([]spec.HookSpec{}, global.BeforeComplete...), local.BeforeComplete...),
		OnFailure:      append(append([]spec.HookSpec{}, global.OnFailure...), local.OnFailure...),
	}
}

func dependenciesReady(n spec.Node, state *store.RunState) (ready bool, skip bool) {
	if len(n.DependsOn) == 0 {
		return true, false
	}
	completed, failed, terminal := 0, 0, 0
	for _, dep := range n.DependsOn {
		s := state.Nodes[dep].Status
		if s == "completed" {
			completed++
			terminal++
		}
		if s == "failed" {
			failed++
			terminal++
		}
		if s == "skipped" {
			terminal++
		}
	}
	if terminal != len(n.DependsOn) {
		return false, false
	}
	rule := n.TriggerRule
	if rule == "" {
		rule = "all_success"
	}
	switch rule {
	case "all_done":
		return true, false
	case "none_failed_min_one_success":
		return failed == 0 && completed > 0, failed > 0 || completed == 0
	default:
		return completed == len(n.DependsOn), completed != len(n.DependsOn)
	}
}

func localDepsReady(n spec.Node, local map[string]store.NodeState) bool {
	for _, dep := range n.DependsOn {
		if local[dep].Status != "completed" {
			return false
		}
	}
	return true
}
func localAllTerminal(local map[string]store.NodeState) bool {
	for _, n := range local {
		if n.Status != "completed" && n.Status != "skipped" {
			return false
		}
	}
	return true
}
func allTerminal(state *store.RunState) bool {
	for _, n := range state.Nodes {
		if n.Status != "completed" && n.Status != "skipped" {
			return false
		}
	}
	return true
}
func untilSatisfied(u spec.UntilSpec, n store.NodeState) bool {
	if u.ExitCode != nil && n.ExitCode != *u.ExitCode {
		return false
	}
	if u.OutputContains != "" && !strings.Contains(n.Output, u.OutputContains) {
		return false
	}
	return true
}
func joinFeedback(values ...string) string {
	var clean []string
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			clean = append(clean, strings.TrimSpace(v))
		}
	}
	return strings.Join(clean, "\n\n")
}

func (r *Runner) fail(state *store.RunState, nodeID string, err error) (*store.RunState, error) {
	state.Status = "failed"
	state.Error = err.Error()
	state.CurrentNode = nodeID
	if nodeID != "" && state.Nodes[nodeID] != nil {
		state.Nodes[nodeID].Status = "failed"
	}
	_ = r.Store.Save(state)
	r.event(state, "run.failed", nodeID, map[string]any{"error": err.Error()})
	return state, err
}
func (r *Runner) event(state *store.RunState, typ, node string, data map[string]any) {
	_ = r.Store.AppendEvent(store.Event{Time: time.Now().UTC(), Type: typ, RunID: state.ID, NodeID: node, Data: data})
}
func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b), nil
}

// StableNodeOrder is exposed for tests and future parallel scheduling.
func StableNodeOrder(nodes []spec.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	sort.Strings(out)
	return out
}
