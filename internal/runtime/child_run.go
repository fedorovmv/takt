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
	"takt/internal/definition"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

func (r *Runner) childRunner(workflowDef *spec.Workflow, workflowPath, controlWorkspace string) *Runner {
	def := Definition{Workflow: workflowDef, Config: r.config, WorkflowPath: workflowPath, ConfigPath: r.configPath, ControlWorkspace: controlWorkspace}
	deps := Dependencies{
		Commands: NewCommandResolver(workflowPath, controlWorkspace, controlWorkspace),
		Store:    r.store, Assistants: r.assistants, Adapters: r.adapters, Redactor: r.redactor,
		AssistantEvents:      r.assistantEvents,
		AssistantActivity:    r.assistantActivity,
		AssistantIdleTimeout: r.assistantIdleTimeout,
	}
	return NewWithDependencies(def, deps)
}

func (r *Runner) runChildWorkflow(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (execResult, error) {
	definition := node.WorkflowRun
	if definition == nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "child workflow", Err: fmt.Errorf("node %q has no workflow definition", node.ID)}
	}
	if definition.FanOut != nil {
		return r.runChildWorkflowFanOut(ctx, state, node, local, feedback, artifacts)
	}
	nodeState := state.Nodes[node.ID]
	childPath, childControlWorkspace, childWorkflow, identity, err := r.resolveChildWorkflowDefinition(state, node, local, feedback, artifacts)
	if err != nil {
		return execResult{}, err
	}
	if identity != nil {
		if nodeState.ChildWorkflowHash == "" {
			nodeState.ChildWorkflowPath = identity.Path
			nodeState.ChildControlWorkspace = identity.Repository
			nodeState.ChildWorkflowHash = identity.Fingerprint
			if err := r.commit(state, "child_workflow.resolved", node.ID, map[string]any{"path": identity.Path, "repository": identity.Repository, "fingerprint": identity.Fingerprint}); err != nil {
				return execResult{}, err
			}
		} else if nodeState.ChildWorkflowPath != identity.Path || nodeState.ChildControlWorkspace != identity.Repository || nodeState.ChildWorkflowHash != identity.Fingerprint {
			return execResult{}, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: fmt.Errorf("child workflow definition changed since preflight")}
		}
	}
	childState, err := r.ensureChildRunLink(state, node.ID, nodeState, childWorkflow, childPath)
	if err != nil {
		return execResult{}, err
	}
	childRunner := r.childRunner(childWorkflow, childPath, childControlWorkspace)
	childState, runErr := r.startOrResumeChild(ctx, state, node, nodeState, childState, childRunner, childWorkflow, childControlWorkspace, local, feedback, artifacts)
	captureChildRunMetadata(nodeState, childState, childControlWorkspace)
	return r.finishChildRun(state, node.ID, nodeState, childWorkflow, definition.OutputNode, childState, runErr)
}

func dynamicChildWorkflow(definition *spec.WorkflowRunSpec) bool {
	return definition != nil && (strings.Contains(definition.Path, "$") || strings.Contains(definition.Repository, "$"))
}

func (r *Runner) resolveChildWorkflowDefinition(state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (string, string, *spec.Workflow, *store.ChildWorkflowIdentityState, error) {
	definition := node.WorkflowRun
	if !dynamicChildWorkflow(definition) {
		path, child, err := r.loadChildWorkflow(definition)
		if err != nil {
			return "", "", nil, nil, err
		}
		repository, err := r.resolveChildControlWorkspace(definition.Repository)
		if err != nil {
			return "", "", nil, nil, &execution.Error{Kind: execution.KindInternal, Op: "resolve child repository", Err: err}
		}
		if nodeState := state.Nodes[node.ID]; nodeState != nil && nodeState.ChildWorkflowHash != "" {
			identity, err := r.staticChildWorkflowIdentity(path, repository, child)
			if err != nil {
				return "", "", nil, nil, err
			}
			return path, repository, child, &identity, nil
		}
		return path, repository, child, nil, nil
	}
	path, err := renderTemplate(definition.Path, state, local, feedback, artifacts)
	if err != nil {
		return "", "", nil, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "render child workflow path", Err: err}
	}
	repository, err := renderTemplate(definition.Repository, state, local, feedback, artifacts)
	if err != nil {
		return "", "", nil, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "render child repository", Err: err}
	}
	identity, child, err := r.resolveDynamicChildWorkflow(path, repository, definition.OutputNode)
	if err != nil {
		return "", "", nil, nil, err
	}
	return identity.Path, identity.Repository, child, &identity, nil
}

func (r *Runner) staticChildWorkflowIdentity(path, repository string, child *spec.Workflow) (store.ChildWorkflowIdentityState, error) {
	resolver := NewCommandResolver(path, repository, repository)
	fingerprint, err := definition.ContentClosureFingerprint(child, path, resolver)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, &execution.Error{Kind: execution.KindConfiguration, Op: "fingerprint child workflow", Err: err}
	}
	return store.ChildWorkflowIdentityState{Path: filepath.Clean(path), Repository: filepath.Clean(repository), Fingerprint: fingerprint}, nil
}

func (r *Runner) resolveDynamicChildWorkflow(path, repository, outputNode string) (store.ChildWorkflowIdentityState, *spec.Workflow, error) {
	controlWorkspace, err := r.resolveChildControlWorkspace(repository)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child repository", Err: err}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: fmt.Errorf("path is empty")}
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(controlWorkspace, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: err}
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: err}
	}
	if !pathWithin(controlWorkspace, path) {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: fmt.Errorf("workflow %q resolves outside repository %q", path, controlWorkspace)}
	}
	info, err := os.Stat(path)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: err}
	}
	if !info.Mode().IsRegular() {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "resolve child workflow", Err: fmt.Errorf("workflow %q is not a regular file", path)}
	}
	child, err := workflow.Load(path)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "load child workflow", Err: err}
	}
	if err := validateChildOutput(child, path, outputNode); err != nil {
		return store.ChildWorkflowIdentityState{}, nil, err
	}
	resolver := NewCommandResolver(path, controlWorkspace, controlWorkspace)
	if err := workflow.ValidateReferences(child, r.config, resolver); err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "validate child workflow references", Err: err}
	}
	if err := ValidateCapabilities(child, r.config, path, resolver, r.assistants); err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "validate child workflow capabilities", Err: err}
	}
	fingerprint, err := definition.ContentClosureFingerprint(child, path, resolver)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, &execution.Error{Kind: execution.KindConfiguration, Op: "fingerprint child workflow", Err: err}
	}
	return store.ChildWorkflowIdentityState{Path: filepath.Clean(path), Repository: filepath.Clean(controlWorkspace), Fingerprint: fingerprint}, child, nil
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (r *Runner) loadChildWorkflow(definition *spec.WorkflowRunSpec) (string, *spec.Workflow, error) {
	childPath := definition.Path
	if !filepath.IsAbs(childPath) {
		childPath = filepath.Join(filepath.Dir(r.workflowPath), childPath)
	}
	childPath, err := filepath.Abs(childPath)
	if err != nil {
		return "", nil, &execution.Error{Kind: execution.KindInternal, Op: "resolve child workflow", Err: err}
	}
	childWorkflow, err := workflow.Load(childPath)
	if err != nil {
		return "", nil, &execution.Error{Kind: execution.KindInternal, Op: "load child workflow", Err: err}
	}
	if err := validateChildOutput(childWorkflow, childPath, definition.OutputNode); err != nil {
		return "", nil, err
	}
	return childPath, childWorkflow, nil
}

func validateChildOutput(childWorkflow *spec.Workflow, childPath, outputNode string) error {
	if strings.TrimSpace(outputNode) == "" {
		if singleTerminalNode(childWorkflow.Nodes) == "" {
			return &execution.Error{Kind: execution.KindInternal, Op: "resolve child output", Err: fmt.Errorf("child workflow %s has multiple terminal nodes; set output_node", childPath)}
		}
		return nil
	}
	for _, childNode := range childWorkflow.Nodes {
		if childNode.ID == outputNode && !childNode.Hidden && childNode.PublicParent == "" {
			return nil
		}
	}
	return &execution.Error{Kind: execution.KindInternal, Op: "resolve child output", Err: fmt.Errorf("output_node %q does not exist in %s", outputNode, childPath)}
}

func (r *Runner) ensureChildRunLink(state *store.RunState, nodeID string, nodeState *store.NodeState, childWorkflow *spec.Workflow, childPath string) (*store.RunState, error) {
	childState, err := loadCurrentChildRun(r.store, nodeState.ChildRunID)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "load child run", Err: err}
	}
	needsNew := nodeState.ChildRunID == "" || (nodeState.Attempts > 1 && childState != nil && terminalRunStatus(childState.Status) && childState.Status != store.RunCompleted)
	if !needsNew {
		return childState, nil
	}
	previousID := nodeState.ChildRunID
	childID, err := newID()
	if err != nil {
		return nil, err
	}
	nodeState.ChildRunID = childID
	nodeState.ChildRunIDs = appendUniqueString(nodeState.ChildRunIDs, childID)
	state.ChildRunIDs = appendUniqueString(state.ChildRunIDs, childID)
	eventType := "child_run.linked"
	data := map[string]any{"child_run_id": childID, "workflow": childWorkflow.Name, "workflow_path": childPath, "attempt": nodeState.Attempts}
	if previousID != "" {
		eventType = "child_run.restarted"
		data["previous_child_run_id"] = previousID
	}
	if err := r.commit(state, eventType, nodeID, data); err != nil {
		return nil, err
	}
	return nil, nil
}

func (r *Runner) startOrResumeChild(ctx context.Context, state *store.RunState, node spec.Node, nodeState *store.NodeState, childState *store.RunState, childRunner *Runner, childWorkflow *spec.Workflow, childControlWorkspace string, local map[string]store.NodeState, feedback, artifacts string) (*store.RunState, error) {
	if childState != nil {
		childRunner.SetStartOptions(StartOptionsFromState(childState))
		if childState.ExecutionWorkspace != "" {
			childRunner.SetExecutionWorkspace(childState.ExecutionWorkspace)
		}
		return childRunner.Resume(ctx, childState)
	}
	input, err := renderTemplate(node.WorkflowRun.Input, state, local, feedback, artifacts)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "render child workflow input", Err: err}
	}
	input, err = ValidateWorkflowInput(input, childWorkflow.Input)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindProtocol, Op: "validate child workflow input", Err: err}
	}
	options, err := r.childStartOptions(node, nodeState.ChildRunID, state.ID)
	if err != nil {
		return nil, err
	}
	configureChildIsolation(childRunner, node.WorkflowRun.Isolation, childControlWorkspace, r.workspace, &options)
	return childRunner.StartWithOptions(ctx, input, options)
}

func (r *Runner) childStartOptions(node spec.Node, childRunID, parentRunID string) (StartOptions, error) {
	options := StartOptions{RunID: childRunID, ParentRunID: parentRunID, ParentNodeID: node.ID, KeepWorktree: node.WorkflowRun.KeepWorktree, ModelPreset: r.startOptions.ModelPreset, ModelOverrides: cloneStringMap(r.startOptions.ModelOverrides)}
	childPolicy := r.inheritedPolicy
	if node.WorkflowRun.Policy != nil {
		resolved, err := resolvePolicyFields(*node.WorkflowRun.Policy, r.workflowPath)
		if err != nil {
			return StartOptions{}, &execution.Error{Kind: execution.KindInternal, Op: "resolve child policy", Err: err}
		}
		childPolicy, err = mergePolicies(childPolicy, resolved)
		if err != nil {
			return StartOptions{}, &execution.Error{Kind: execution.KindInternal, Op: "merge child policy", Err: err}
		}
	}
	if len(assistant.RequiredCapabilities(childPolicy)) > 0 {
		options.InheritedPolicy = &childPolicy
	}
	return options, nil
}

func configureChildIsolation(childRunner *Runner, isolation, childControlWorkspace, parentWorkspace string, options *StartOptions) {
	switch isolation {
	case "":
		// Use the child workflow's own worktree policy.
	case "inherit":
		value := false
		options.Worktree = &value
		childRunner.SetExecutionWorkspace(parentWorkspace)
	case "none":
		value := false
		options.Worktree = &value
		childRunner.SetExecutionWorkspace(childControlWorkspace)
	case "worktree":
		value := true
		options.Worktree = &value
	}
}

func (r *Runner) finishChildRun(state *store.RunState, nodeID string, nodeState *store.NodeState, childWorkflow *spec.Workflow, outputNode string, childState *store.RunState, runErr error) (execResult, error) {
	result := childExecResult(childWorkflow, childState, outputNode)
	if errors.Is(runErr, ErrPaused) || (childState != nil && childState.Status == store.RunPaused) {
		return result, ErrPaused
	}
	if errors.Is(runErr, ErrWaiting) || (childState != nil && childState.Status == store.RunWaiting) {
		if childState == nil {
			return result, &execution.Error{Kind: execution.KindInternal, Op: "child run", Err: fmt.Errorf("waiting child state is missing")}
		}
		message := fmt.Sprintf("child run %s is waiting", childState.ID)
		if childState.Waiting != nil && childState.Waiting.Message != "" {
			message = childState.Waiting.Message
		}
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: nodeID, Message: message, Kind: "child_run", ChildRunID: childState.ID}
		nodeState.Status = store.NodeWaiting
		if err := r.commit(state, "child_run.waiting", nodeID, map[string]any{"child_run_id": childState.ID, "message": message}); err != nil {
			return execResult{}, err
		}
		return execResult{}, ErrWaiting
	}
	if runErr != nil {
		kind := childFailureKind(childState, runErr)
		exitCode := result.ExitCode
		if kind == execution.KindExit && exitCode == 0 {
			exitCode = 1
		}
		return result, &execution.Error{Kind: kind, ExitCode: exitCode, Op: "child run " + nodeState.ChildRunID, Err: runErr}
	}
	if childState == nil || childState.Status != store.RunCompleted {
		status := ""
		id := nodeState.ChildRunID
		if childState != nil {
			status, id = childState.Status, childState.ID
		}
		return result, &execution.Error{Kind: execution.KindInternal, Op: "child run", Err: fmt.Errorf("child run %s has non-terminal status %s", id, status)}
	}
	if err := r.commit(state, "child_run.completed", nodeID, map[string]any{"child_run_id": childState.ID, "usage": childState.Usage}); err != nil {
		return execResult{}, err
	}
	return result, nil
}

func childFailureKind(state *store.RunState, err error) execution.Kind {
	if state != nil {
		if state.Status == store.RunCancelled {
			return execution.KindCancelled
		}
		switch execution.Kind(state.ErrorCode) {
		case execution.KindExit, execution.KindStart, execution.KindCancelled, execution.KindTimedOut, execution.KindProtocol, execution.KindConfiguration, execution.KindProviderUnavailable, execution.KindInternal, execution.KindExternalUnknown:
			return execution.Kind(state.ErrorCode)
		}
	}
	var failed *RunFailedError
	if errors.As(err, &failed) {
		kind := execution.Kind(failed.Code)
		if kind != "" {
			return kind
		}
	}
	return execution.KindInternal
}

func (r *Runner) resolveChildControlWorkspace(repository string) (string, error) {
	repository = strings.TrimSpace(repository)
	if repository == "" || repository == "." {
		return r.controlWorkspace, nil
	}
	if filepath.IsAbs(repository) {
		return "", fmt.Errorf("repository %q must be relative to control workspace", repository)
	}
	root, err := filepath.Abs(r.controlWorkspace)
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
