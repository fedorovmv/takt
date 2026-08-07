package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func (r *Runner) runChildWorkflow(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (execResult, error) {
	definition := node.WorkflowRun
	if definition == nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "child workflow", Err: fmt.Errorf("node %q has no workflow definition", node.ID)}
	}
	if definition.FanOut != nil {
		return r.runChildWorkflowFanOut(ctx, state, node, local, feedback, artifacts)
	}
	childPath := definition.Path
	if !filepath.IsAbs(childPath) {
		childPath = filepath.Join(filepath.Dir(r.WorkflowPath), childPath)
	}
	childPath, err := filepath.Abs(childPath)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child workflow", Err: err}
	}
	childWorkflow, err := workflow.Load(childPath)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "load child workflow", Err: err}
	}

	if strings.TrimSpace(definition.OutputNode) != "" {
		found := false
		for _, childNode := range childWorkflow.Nodes {
			if childNode.ID == definition.OutputNode && !childNode.Hidden && childNode.PublicParent == "" {
				found = true
				break
			}
		}
		if !found {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child output", Err: fmt.Errorf("output_node %q does not exist in %s", definition.OutputNode, childPath)}
		}
	} else if singleTerminalNode(childWorkflow.Nodes) == "" {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child output", Err: fmt.Errorf("child workflow %s has multiple terminal nodes; set output_node", childPath)}
	}

	nodeState := state.Nodes[node.ID]
	childState, loadErr := loadCurrentChildRun(r.Store, nodeState.ChildRunID)
	if loadErr != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "load child run", Err: loadErr}
	}
	if nodeState.ChildRunID == "" || (nodeState.Attempts > 1 && childState != nil && terminalRunStatus(childState.Status)) {
		previousID := nodeState.ChildRunID
		childID, idErr := newID()
		if idErr != nil {
			return execResult{}, idErr
		}
		nodeState.ChildRunID = childID
		nodeState.ChildRunIDs = appendUniqueString(nodeState.ChildRunIDs, childID)
		state.ChildRunIDs = appendUniqueString(state.ChildRunIDs, childID)
		eventType := "child_run.linked"
		data := map[string]any{
			"child_run_id":  childID,
			"workflow":      childWorkflow.Metadata.Name,
			"workflow_path": childPath,
			"attempt":       nodeState.Attempts,
		}
		if previousID != "" {
			eventType = "child_run.restarted"
			data["previous_child_run_id"] = previousID
		}
		if err := r.commit(state, eventType, node.ID, data); err != nil {
			return execResult{}, err
		}
		childState = nil
	}

	childControlWorkspace, err := r.resolveChildControlWorkspace(definition.Repository)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child repository", Err: err}
	}
	childRunner := New(childWorkflow, r.Config, childPath, r.ConfigPath, childControlWorkspace)
	childRunner.Store = r.Store
	childRunner.Assistants = r.Assistants
	if childState == nil {
		input, renderErr := renderTemplate(definition.Input, state, local, feedback, artifacts)
		if renderErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "render child workflow input", Err: renderErr}
		}
		input, renderErr = ValidateWorkflowInput(input, childWorkflow.Input)
		if renderErr != nil {
			return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "validate child workflow input", Err: renderErr}
		}
		options := StartOptions{RunID: nodeState.ChildRunID, ParentRunID: state.ID, ParentNodeID: node.ID}
		childPolicy := r.inheritedPolicy
		if definition.Policy != nil {
			resolvedPolicy, policyErr := resolvePolicyFields(*definition.Policy, r.WorkflowPath)
			if policyErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child policy", Err: policyErr}
			}
			childPolicy, policyErr = mergePolicies(childPolicy, resolvedPolicy)
			if policyErr != nil {
				return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "merge child policy", Err: policyErr}
			}
		}
		if len(assistant.RequiredCapabilities(childPolicy)) > 0 {
			options.InheritedPolicy = &childPolicy
		}
		switch definition.Isolation {
		case "":
			// Use the child workflow's own worktree policy.
		case "inherit":
			value := false
			options.Worktree = &value
			childRunner.SetExecutionWorkspace(r.Workspace)
		case "none":
			value := false
			options.Worktree = &value
			childRunner.SetExecutionWorkspace(childControlWorkspace)
		case "worktree":
			value := true
			options.Worktree = &value
		}
		childState, err = childRunner.StartWithOptions(ctx, input, options)
	} else {
		childRunner.SetStartOptions(StartOptionsFromState(childState))
		if childState.ExecutionWorkspace != "" {
			childRunner.SetExecutionWorkspace(childState.ExecutionWorkspace)
		}
		childState, err = childRunner.Resume(ctx, childState)
	}

	captureChildRunMetadata(nodeState, childState, childControlWorkspace)
	if errors.Is(err, ErrPaused) || (childState != nil && childState.Status == store.RunPaused) {
		return childExecResult(childWorkflow, childState, definition.OutputNode), ErrPaused
	}
	if errors.Is(err, ErrWaiting) || childState.Status == store.RunWaiting {
		message := fmt.Sprintf("child run %s is waiting", childState.ID)
		if childState.Waiting != nil && childState.Waiting.Message != "" {
			message = childState.Waiting.Message
		}
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: message, Kind: "child_run", ChildRunID: childState.ID}
		nodeState.Status = store.NodeWaiting
		if commitErr := r.commit(state, "child_run.waiting", node.ID, map[string]any{"child_run_id": childState.ID, "message": message}); commitErr != nil {
			return execResult{}, commitErr
		}
		return execResult{}, ErrWaiting
	}
	if err != nil {
		kind := execution.KindExit
		if childState != nil && childState.Status == store.RunCancelled {
			kind = execution.KindCancelled
		}
		return childExecResult(childWorkflow, childState, definition.OutputNode), &execution.Error{Kind: kind, ExitCode: 1, Op: "child run " + nodeState.ChildRunID, Err: err}
	}
	if childState.Status != store.RunCompleted {
		return childExecResult(childWorkflow, childState, definition.OutputNode), &execution.Error{Kind: execution.KindInternal, Op: "child run", Err: fmt.Errorf("child run %s has non-terminal status %s", childState.ID, childState.Status)}
	}
	if commitErr := r.commit(state, "child_run.completed", node.ID, map[string]any{"child_run_id": childState.ID, "usage": childState.Usage}); commitErr != nil {
		return execResult{}, commitErr
	}
	return childExecResult(childWorkflow, childState, definition.OutputNode), nil
}

func (r *Runner) resolveChildControlWorkspace(repository string) (string, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" || repository == "." {
		return r.ControlWorkspace, nil
	}
	if filepath.IsAbs(repository) {
		return "", fmt.Errorf("repository %q must be relative to control workspace", repository)
	}
	root, err := filepath.Abs(r.ControlWorkspace)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(root, repository))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository %q escapes control workspace", repository)
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	candidateReal, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("repository %q: %w", repository, err)
	}
	rel, err = filepath.Rel(rootReal, candidateReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository %q resolves outside control workspace", repository)
	}
	info, err := os.Stat(candidateReal)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository %q is not a directory", repository)
	}
	return candidateReal, nil
}

func captureChildRunMetadata(node *store.NodeState, child *store.RunState, controlWorkspace string) {
	if node == nil || child == nil {
		return
	}
	node.ChildControlWorkspace = controlWorkspace
	node.ChildExecutionWorkspace = child.ExecutionWorkspace
	if child.Worktree != nil {
		node.ChildBranch = child.Worktree.Branch
		node.ChildBaseCommit = child.Worktree.BaseCommit
		if node.ChildExecutionWorkspace == "" {
			node.ChildExecutionWorkspace = child.Worktree.ExecutionWorkspace
		}
	}
}

func childExecResult(wf *spec.Workflow, state *store.RunState, outputNode string) execResult {
	if state == nil {
		return execResult{ExitCode: 1}
	}
	result := execResult{Output: state.Output, Stdout: state.Output, ExitCode: 0, Artifacts: cloneArtifacts(state.Artifacts)}
	if strings.TrimSpace(outputNode) != "" {
		if node := state.Nodes[outputNode]; node != nil {
			result.Output = node.Output
			result.Stdout = node.Stdout
			result.Stderr = node.Stderr
			result.ExitCode = node.ExitCode
			result.Truncated = node.OutputTruncated
		}
	} else if id := singleTerminalNode(wf.Nodes); id != "" {
		if node := state.Nodes[id]; node != nil {
			result.Output = node.Output
			result.Stdout = node.Stdout
			result.Stderr = node.Stderr
			result.ExitCode = node.ExitCode
			result.Truncated = node.OutputTruncated
		}
	}
	if state.Usage != nil {
		result.Usage = &assistant.ProtocolUsage{InputTokens: state.Usage.InputTokens, OutputTokens: state.Usage.OutputTokens, Cost: state.Usage.Cost}
	}
	return result
}

func aggregateRunUsage(nodes map[string]*store.NodeState) *store.Usage {
	usage := &store.Usage{}
	seen := false
	for _, node := range nodes {
		if node == nil || node.Usage == nil {
			continue
		}
		seen = true
		usage.InputTokens += node.Usage.InputTokens
		usage.OutputTokens += node.Usage.OutputTokens
		usage.Cost += node.Usage.Cost
	}
	if !seen {
		return nil
	}
	return usage
}

func defaultRunOutput(nodes []spec.Node, states map[string]*store.NodeState) string {
	terminal := terminalNodeIDs(nodes)
	if len(terminal) == 1 {
		if state := states[terminal[0]]; state != nil {
			return state.Output
		}
		return ""
	}

	// Conditional routing commonly leaves several structural terminal nodes in
	// the definition while only one branch completes in a concrete Run. Preserve
	// a useful top-level result when exactly one terminal branch completed.
	var completed string
	for _, id := range terminal {
		state := states[id]
		if state == nil || state.Status != store.NodeCompleted {
			continue
		}
		if completed != "" {
			return ""
		}
		completed = state.Output
	}
	return completed
}

func singleTerminalNode(nodes []spec.Node) string {
	terminal := terminalNodeIDs(nodes)
	if len(terminal) == 1 {
		return terminal[0]
	}
	return ""
}

func terminalNodeIDs(nodes []spec.Node) []string {
	public := map[string]bool{}
	depended := map[string]bool{}
	for _, node := range nodes {
		if node.Hidden || node.PublicParent != "" {
			continue
		}
		public[node.ID] = true
	}
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if public[dep] {
				depended[dep] = true
			}
		}
	}
	var terminal []string
	for id := range public {
		if !depended[id] {
			terminal = append(terminal, id)
		}
	}
	sort.Strings(terminal)
	return terminal
}

func loadCurrentChildRun(repository store.Repository, id string) (*store.RunState, error) {
	if id == "" {
		return nil, nil
	}
	state, err := repository.Load(id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return state, err
}

func terminalRunStatus(status string) bool {
	switch status {
	case store.RunCompleted, store.RunFailed, store.RunCancelled, store.RunAbandoned:
		return true
	default:
		return false
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
