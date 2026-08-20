package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/spec"
	"takt/internal/store"
	"takt/internal/workflow"
)

type fanOutRunResult struct {
	position int
	state    *store.RunState
	err      error
}

type fanOutExecution struct {
	definition    *spec.WorkflowRunSpec
	fanOut        *spec.WorkflowFanOutSpec
	childPath     string
	childWorkflow *spec.Workflow
	items         []any
	nodeState     *store.NodeState
	attempt       int
	pending       []int
	maxParallel   int
	join          string
}

func (r *Runner) runChildWorkflowFanOut(ctx context.Context, state *store.RunState, node spec.Node, local map[string]store.NodeState, feedback, artifacts string) (execResult, error) {
	executionState, err := r.prepareFanOutExecution(state, node)
	if err != nil {
		return execResult{}, err
	}
	if err := r.runFanOutBatches(ctx, state, node, executionState, local, feedback, artifacts); err != nil {
		return execResult{}, err
	}
	return r.finishFanOut(state, node, executionState)
}

func (r *Runner) prepareFanOutExecution(state *store.RunState, node spec.Node) (*fanOutExecution, error) {
	definition := node.WorkflowRun
	if definition == nil || definition.FanOut == nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "child fan-out", Err: fmt.Errorf("node %q has no fan_out definition", node.ID)}
	}
	fanOut := definition.FanOut
	childPath := definition.Path
	if !filepath.IsAbs(childPath) {
		childPath = filepath.Join(filepath.Dir(r.workflowPath), childPath)
	}
	childPath, err := filepath.Abs(childPath)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "resolve fan-out child workflow", Err: err}
	}
	childWorkflow, err := workflow.Load(childPath)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "load fan-out child workflow", Err: err}
	}
	if err := validateChildOutputSelection(childWorkflow, childPath, definition.OutputNode); err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "resolve fan-out child output", Err: err}
	}
	items, encodedItems, fingerprint, err := resolveFanOutItems(fanOut.ItemsFrom, state)
	if err != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "resolve fan-out items", Err: err}
	}
	if err := validateFanOutItems(node.ID, fanOut, items, encodedItems); err != nil {
		return nil, err
	}
	nodeState := state.Nodes[node.ID]
	attempt := nodeState.Attempts
	if err := r.ensureFanOutLinks(state, node.ID, nodeState, attempt, fanOut, encodedItems, fingerprint); err != nil {
		return nil, err
	}
	positions := currentFanOutPositions(nodeState.ChildRuns, attempt)
	if len(positions) != len(items) {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "resume child fan-out", Err: fmt.Errorf("fan-out state has %d items for attempt %d, expected %d", len(positions), attempt, len(items))}
	}
	pending, err := r.pendingFanOutPositions(nodeState, positions, childWorkflow, definition.OutputNode)
	if err != nil {
		return nil, err
	}
	join := normalizedFanOutJoin(fanOut.Join)
	if decided, _ := fanOutJoinDecision(nodeState.ChildRuns, attempt, len(items), join); decided {
		if err := r.cancelPendingFanOut(state, node.ID, nodeState, pending, attempt, join); err != nil {
			return nil, err
		}
		pending = nil
	}
	return &fanOutExecution{
		definition: definition, fanOut: fanOut, childPath: childPath, childWorkflow: childWorkflow,
		items: items, nodeState: nodeState, attempt: attempt, pending: pending,
		maxParallel: normalizedMaxParallel(fanOut.MaxParallel), join: join,
	}, nil
}

func validateFanOutItems(nodeID string, fanOut *spec.WorkflowFanOutSpec, items []any, encodedItems []json.RawMessage) error {
	if fanOut.MaxItems > 0 && len(items) > fanOut.MaxItems {
		return &execution.Error{Kind: execution.KindProtocol, Op: "resolve fan-out items", Err: fmt.Errorf("fan-out source for node %q has %d items, exceeding max_items %d", nodeID, len(items), fanOut.MaxItems)}
	}
	if len(items) == 0 && !fanOut.AllowEmpty {
		return &execution.Error{Kind: execution.KindInternal, Op: "resolve fan-out items", Err: fmt.Errorf("fan-out source for node %q is empty; set allow_empty: true to accept it", nodeID)}
	}
	if fanOut.AllowDuplicates {
		return nil
	}
	seen := map[string]int{}
	for index, raw := range encodedItems {
		key := string(raw)
		if previous, exists := seen[key]; exists {
			return &execution.Error{Kind: execution.KindInternal, Op: "resolve fan-out items", Err: fmt.Errorf("fan-out source for node %q contains duplicate items at indexes %d and %d; set allow_duplicates: true to run both", nodeID, previous, index)}
		}
		seen[key] = index
	}
	return nil
}

func (r *Runner) ensureFanOutLinks(state *store.RunState, nodeID string, nodeState *store.NodeState, attempt int, fanOut *spec.WorkflowFanOutSpec, encodedItems []json.RawMessage, fingerprint string) error {
	if nodeState.FanOutAttempt == attempt {
		if nodeState.FanOutFingerprint != fingerprint {
			return &execution.Error{Kind: execution.KindInternal, Op: "resume child fan-out", Err: fmt.Errorf("fan-out items changed for node %q: stored=%s actual=%s", nodeID, nodeState.FanOutFingerprint, fingerprint)}
		}
		return nil
	}
	for index, raw := range encodedItems {
		childID, err := newID()
		if err != nil {
			return err
		}
		itemHash := sha256.Sum256(raw)
		record := store.ChildRunItemState{
			Attempt: attempt, Index: index, Item: append(json.RawMessage(nil), raw...),
			ItemFingerprint: hex.EncodeToString(itemHash[:]), RunID: childID, Status: store.NodePending,
		}
		nodeState.ChildRuns = append(nodeState.ChildRuns, record)
		nodeState.ChildRunIDs = appendUniqueString(nodeState.ChildRunIDs, childID)
		state.ChildRunIDs = appendUniqueString(state.ChildRunIDs, childID)
	}
	nodeState.FanOutAttempt = attempt
	nodeState.FanOutFingerprint = fingerprint
	return r.commit(state, "child_run.fan_out.linked", nodeID, map[string]any{
		"attempt": attempt, "items": len(encodedItems), "items_fingerprint": fingerprint,
		"max_parallel": normalizedMaxParallel(fanOut.MaxParallel), "join": normalizedFanOutJoin(fanOut.Join),
	})
}

func (r *Runner) pendingFanOutPositions(nodeState *store.NodeState, positions []int, childWorkflow *spec.Workflow, outputNode string) ([]int, error) {
	pending := make([]int, 0, len(positions))
	for _, position := range positions {
		record := &nodeState.ChildRuns[position]
		childState, err := loadCurrentChildRun(r.store, record.RunID)
		if err != nil {
			return nil, &execution.Error{Kind: execution.KindInternal, Op: "load fan-out child run", Err: err}
		}
		if childState != nil && terminalRunStatus(childState.Status) {
			updateFanOutRecord(record, childWorkflow, outputNode, childState, nil)
			continue
		}
		pending = append(pending, position)
	}
	return pending, nil
}

func (r *Runner) cancelPendingFanOut(state *store.RunState, nodeID string, nodeState *store.NodeState, positions []int, attempt int, join string) error {
	cancelled := cancelFanOutPositions(nodeState.ChildRuns, positions, attempt, "fanout_result_decided")
	if cancelled == 0 {
		return nil
	}
	return r.commit(state, "child_run.fan_out.cancelled", nodeID, map[string]any{
		"attempt": attempt, "cancelled": cancelled, "reason": "fanout_result_decided", "join": join,
	})
}

func (r *Runner) runFanOutBatches(ctx context.Context, state *store.RunState, node spec.Node, executionState *fanOutExecution, local map[string]store.NodeState, feedback, artifacts string) error {
	for start := 0; start < len(executionState.pending); start += executionState.maxParallel {
		if r.pauseRequested(state.ID) {
			state.PauseRequested = true
			return ErrPaused
		}
		end := start + executionState.maxParallel
		if end > len(executionState.pending) {
			end = len(executionState.pending)
		}
		batch := executionState.pending[start:end]
		decided := r.runFanOutBatch(ctx, state, node, executionState, batch, local, feedback, artifacts)
		if err := r.commitFanOutProgress(state, node.ID, executionState); err != nil {
			return err
		}
		if fanOutStatusCount(executionState.nodeState.ChildRuns, executionState.attempt, store.RunPaused) > 0 || r.pauseRequested(state.ID) {
			state.PauseRequested = true
			return ErrPaused
		}
		if decided {
			return r.cancelPendingFanOut(state, node.ID, executionState.nodeState, executionState.pending[end:], executionState.attempt, executionState.join)
		}
	}
	return nil
}

func (r *Runner) runFanOutBatch(ctx context.Context, state *store.RunState, node spec.Node, executionState *fanOutExecution, batch []int, local map[string]store.NodeState, feedback, artifacts string) bool {
	batchCtx, cancelBatch := context.WithCancel(ctx)
	defer cancelBatch()
	results := make(chan fanOutRunResult, len(batch))
	var wg sync.WaitGroup
	for _, position := range batch {
		position := position
		wg.Add(1)
		go func() {
			defer wg.Done()
			record := executionState.nodeState.ChildRuns[position]
			childState, runErr := r.runFanOutChild(batchCtx, state, node, executionState.childWorkflow, executionState.childPath, record, executionState.items[record.Index], len(executionState.items), local, feedback, artifacts)
			results <- fanOutRunResult{position: position, state: childState, err: runErr}
		}()
	}
	decided := false
	for range batch {
		result := <-results
		updateFanOutRecord(&executionState.nodeState.ChildRuns[result.position], executionState.childWorkflow, executionState.definition.OutputNode, result.state, result.err)
		if !decided {
			if done, _ := fanOutJoinDecision(executionState.nodeState.ChildRuns, executionState.attempt, len(executionState.items), executionState.join); done && executionState.join != "all_done" {
				decided = true
				cancelBatch()
			}
		}
	}
	cancelBatch()
	wg.Wait()
	close(results)
	if decided {
		for _, position := range batch {
			record := &executionState.nodeState.ChildRuns[position]
			if record.Attempt == executionState.attempt && record.Status == store.RunCancelled && record.CancelReason == "" {
				record.CancelReason = "fanout_result_decided"
			}
		}
	}
	return decided
}

func (r *Runner) commitFanOutProgress(state *store.RunState, nodeID string, executionState *fanOutExecution) error {
	return r.commit(state, "child_run.fan_out.progress", nodeID, map[string]any{
		"attempt":   executionState.attempt,
		"completed": fanOutStatusCount(executionState.nodeState.ChildRuns, executionState.attempt, store.RunCompleted),
		"waiting":   fanOutStatusCount(executionState.nodeState.ChildRuns, executionState.attempt, store.RunWaiting),
		"paused":    fanOutStatusCount(executionState.nodeState.ChildRuns, executionState.attempt, store.RunPaused),
		"join":      executionState.join,
	})
}

func (r *Runner) finishFanOut(state *store.RunState, node spec.Node, executionState *fanOutExecution) (execResult, error) {
	output, usage, err := fanOutAggregate(executionState.nodeState.ChildRuns, executionState.attempt)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "aggregate child fan-out", Err: err}
	}
	waitingIDs := fanOutWaitingIDs(executionState.nodeState.ChildRuns, executionState.attempt)
	if len(waitingIDs) > 0 {
		message := fmt.Sprintf("%d child runs are waiting for input", len(waitingIDs))
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: node.ID, Message: message, Kind: "child_run", ChildRunIDs: waitingIDs}
		if len(waitingIDs) == 1 {
			state.Waiting.ChildRunID = waitingIDs[0]
		}
		executionState.nodeState.Status = store.NodeWaiting
		if err := r.commit(state, "child_run.fan_out.waiting", node.ID, map[string]any{"child_run_ids": waitingIDs, "message": message}); err != nil {
			return execResult{}, err
		}
		return execResult{}, ErrWaiting
	}
	result := execResult{Output: output, Stdout: output, ExitCode: 0, Usage: usage, Artifacts: fanOutArtifacts(executionState.nodeState.ChildRuns, executionState.attempt)}
	completed, failed := fanOutTerminalCounts(executionState.nodeState.ChildRuns, executionState.attempt)
	if err := validateFanOutJoin(executionState.join, completed, failed); err != nil {
		result.ExitCode = 1
		return result, &execution.Error{Kind: fanOutFailureKind(executionState.nodeState.ChildRuns, executionState.attempt), ExitCode: 1, Op: "child fan-out join", Err: err}
	}
	if err := r.commit(state, "child_run.fan_out.completed", node.ID, map[string]any{
		"attempt": executionState.attempt, "children": completed + failed, "completed": completed, "failed": failed, "join": executionState.join,
	}); err != nil {
		return execResult{}, err
	}
	return result, nil
}

func fanOutFailureKind(records []store.ChildRunItemState, attempt int) execution.Kind {
	for _, record := range records {
		if record.Attempt != attempt || record.Status == store.RunCompleted || record.CancelReason == "fanout_result_decided" || record.ErrorCode == "" || record.ErrorCode == string(execution.KindExit) {
			continue
		}
		return execution.Kind(record.ErrorCode)
	}
	return execution.KindExit
}

func validateFanOutJoin(join string, completed, failed int) error {
	switch join {
	case "all_success":
		if failed > 0 {
			return fmt.Errorf("%d of %d child runs did not complete successfully", failed, completed+failed)
		}
	case "one_success":
		if completed == 0 {
			return fmt.Errorf("none of %d child runs completed successfully", failed)
		}
	}
	return nil
}

func (r *Runner) runFanOutChild(ctx context.Context, parent *store.RunState, node spec.Node, childWorkflow *spec.Workflow, childPath string, record store.ChildRunItemState, item any, total int, local map[string]store.NodeState, feedback, artifacts string) (*store.RunState, error) {
	definition := node.WorkflowRun
	childRunner := r.childRunner(childWorkflow, childPath, r.controlWorkspace)
	childState, loadErr := loadCurrentChildRun(r.store, record.RunID)
	if loadErr != nil {
		return nil, loadErr
	}
	if childState != nil {
		childRunner.SetStartOptions(StartOptionsFromState(childState))
		if childState.ExecutionWorkspace != "" {
			childRunner.SetExecutionWorkspace(childState.ExecutionWorkspace)
		}
		return childRunner.Resume(ctx, childState)
	}

	input, renderErr := renderFanOutTemplate(definition.Input, parent, local, feedback, artifacts, item, record.Index, total, definition.FanOut.As)
	if renderErr != nil {
		return nil, &execution.Error{Kind: execution.KindInternal, Op: "render fan-out child input", Err: renderErr}
	}
	input, renderErr = ValidateWorkflowInput(input, childWorkflow.Input)
	if renderErr != nil {
		return nil, &execution.Error{Kind: execution.KindProtocol, Op: "validate fan-out child workflow input", Err: renderErr}
	}
	options := StartOptions{RunID: record.RunID, ParentRunID: parent.ID, ParentNodeID: fmt.Sprintf("%s[%d]", node.ID, record.Index), KeepWorktree: definition.KeepWorktree, ModelPreset: r.startOptions.ModelPreset, ModelOverrides: cloneStringMap(r.startOptions.ModelOverrides)}
	childPolicy := r.inheritedPolicy
	if definition.Policy != nil {
		resolvedPolicy, policyErr := resolvePolicyFields(*definition.Policy, r.workflowPath)
		if policyErr != nil {
			return nil, policyErr
		}
		childPolicy, policyErr = mergePolicies(childPolicy, resolvedPolicy)
		if policyErr != nil {
			return nil, policyErr
		}
	}
	if len(assistant.RequiredCapabilities(childPolicy)) > 0 {
		options.InheritedPolicy = &childPolicy
	}
	switch definition.Isolation {
	case "inherit":
		value := false
		options.Worktree = &value
		childRunner.SetExecutionWorkspace(r.workspace)
	case "none":
		value := false
		options.Worktree = &value
		childRunner.SetExecutionWorkspace(r.controlWorkspace)
	case "worktree":
		value := true
		options.Worktree = &value
	}
	return childRunner.StartWithOptions(ctx, input, options)
}

func resolveFanOutItems(path string, state *store.RunState) ([]any, []json.RawMessage, string, error) {
	parts := strings.Split(strings.TrimSpace(path), ".")
	if len(parts) >= 2 && strings.HasPrefix(parts[0], "$") {
		parts[0] = strings.TrimPrefix(parts[0], "$")
		parts = append([]string{"nodes", parts[0]}, parts[1:]...)
	}
	if len(parts) < 3 || parts[0] != "nodes" || parts[2] != "output" {
		return nil, nil, "", fmt.Errorf("items_from must be $<id>.output or a nested output path")
	}
	node := state.Nodes[parts[1]]
	if node == nil {
		return nil, nil, "", fmt.Errorf("source node %q is missing", parts[1])
	}
	var current any
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(node.Output)))
	decoder.UseNumber()
	if err := decoder.Decode(&current); err != nil {
		return nil, nil, "", fmt.Errorf("source node %q output is not JSON: %w", parts[1], err)
	}
	for _, part := range parts[3:] {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return nil, nil, "", fmt.Errorf("items_from path component %q does not exist", part)
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, nil, "", fmt.Errorf("items_from array index %q is invalid", part)
			}
			current = value[index]
		default:
			return nil, nil, "", fmt.Errorf("items_from path reaches non-container before %q", part)
		}
	}
	items, ok := current.([]any)
	if !ok {
		return nil, nil, "", fmt.Errorf("items_from resolves to %T, expected array", current)
	}
	encoded := make([]json.RawMessage, len(items))
	for index, item := range items {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, nil, "", fmt.Errorf("encode item %d: %w", index, err)
		}
		encoded[index] = raw
	}
	all, err := json.Marshal(items)
	if err != nil {
		return nil, nil, "", err
	}
	hash := sha256.Sum256(all)
	return items, encoded, hex.EncodeToString(hash[:]), nil
}

func renderFanOutTemplate(src string, state *store.RunState, local map[string]store.NodeState, feedback, artifacts string, item any, index, total int, alias string) (string, error) {
	if alias == "" {
		alias = "item"
	}
	extra := func(key string) (string, bool) {
		switch key {
		case "fanout.index":
			return strconv.Itoa(index), true
		case "fanout.total":
			return strconv.Itoa(total), true
		case "fanout.item", alias:
			return fanOutValueString(item), true
		}
		if strings.HasPrefix(key, "fanout.item.") {
			return fanOutPathLookup(item, strings.Split(strings.TrimPrefix(key, "fanout.item."), "."))
		}
		if strings.HasPrefix(key, alias+".") {
			return fanOutPathLookup(item, strings.Split(strings.TrimPrefix(key, alias+"."), "."))
		}
		return "", false
	}
	return renderTemplateWithResolver(src, state, local, feedback, artifacts, extra)
}

func fanOutPathString(value any, path []string) string {
	result, _ := fanOutPathLookup(value, path)
	return result
}

func fanOutPathLookup(value any, path []string) (string, bool) {
	current := value
	for _, part := range path {
		switch typed := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = typed[part]
			if !ok {
				return "", false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(typed) {
				return "", false
			}
			current = typed[index]
		default:
			return "", false
		}
	}
	return fanOutValueString(current), true
}

func fanOutValueString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func currentFanOutPositions(records []store.ChildRunItemState, attempt int) []int {
	positions := make([]int, 0)
	for position := range records {
		if records[position].Attempt == attempt {
			positions = append(positions, position)
		}
	}
	return positions
}

func updateFanOutRecord(record *store.ChildRunItemState, childWorkflow *spec.Workflow, outputNode string, child *store.RunState, runErr error) {
	if child != nil {
		record.Status = child.Status
		record.Output = childExecResult(childWorkflow, child, outputNode).Output
		record.ErrorCode = child.ErrorCode
		record.Error = child.Error
		record.Usage = child.Usage
		record.Artifacts = cloneArtifacts(child.Artifacts)
		return
	}
	if runErr != nil {
		record.Status = store.RunFailed
		record.ErrorCode = string(execution.KindOf(runErr))
		record.Error = runErr.Error()
	}
}

func fanOutArtifacts(records []store.ChildRunItemState, attempt int) []store.ArtifactRef {
	var artifacts []store.ArtifactRef
	for _, record := range records {
		if record.Attempt != attempt {
			continue
		}
		for _, artifact := range record.Artifacts {
			artifacts = appendArtifactUnique(artifacts, artifact)
		}
	}
	return artifacts
}

func fanOutAggregate(records []store.ChildRunItemState, attempt int) (string, *assistant.ProtocolUsage, error) {
	values := make([]map[string]any, 0)
	usage := &assistant.ProtocolUsage{}
	hasUsage := false
	for _, record := range records {
		if record.Attempt != attempt {
			continue
		}
		var item any
		decoder := json.NewDecoder(strings.NewReader(string(record.Item)))
		decoder.UseNumber()
		if err := decoder.Decode(&item); err != nil {
			return "", nil, err
		}
		var output any = record.Output
		if strings.TrimSpace(record.Output) != "" {
			var parsed any
			outDecoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(record.Output)))
			outDecoder.UseNumber()
			if err := outDecoder.Decode(&parsed); err == nil {
				output = parsed
			}
		}
		value := map[string]any{"index": record.Index, "item": item, "run_id": record.RunID, "status": record.Status, "output": output}
		if record.ErrorCode != "" {
			value["error_code"] = record.ErrorCode
		}
		if record.Error != "" {
			value["error"] = record.Error
		}
		if record.CancelReason != "" {
			value["cancel_reason"] = record.CancelReason
		}
		if record.Usage != nil {
			value["usage"] = record.Usage
			usage.InputTokens += record.Usage.InputTokens
			usage.OutputTokens += record.Usage.OutputTokens
			usage.Cost += record.Usage.Cost
			hasUsage = true
		}
		values = append(values, value)
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", nil, err
	}
	if !hasUsage {
		usage = nil
	}
	return string(raw), usage, nil
}

func fanOutJoinDecision(records []store.ChildRunItemState, attempt, total int, join string) (decided, success bool) {
	completed, terminal := 0, 0
	for _, record := range records {
		if record.Attempt != attempt {
			continue
		}
		if record.Status == store.RunCompleted {
			completed++
			terminal++
			continue
		}
		switch record.Status {
		case store.RunFailed, store.RunCancelled, store.RunAbandoned:
			terminal++
		}
	}
	switch join {
	case "one_success":
		if completed > 0 {
			return true, true
		}
		if terminal == total {
			return true, false
		}
	case "all_success":
		if terminal-completed > 0 {
			return true, false
		}
		if completed == total {
			return true, true
		}
	case "all_done":
		if terminal == total {
			return true, true
		}
	}
	return false, false
}

func cancelFanOutPositions(records []store.ChildRunItemState, positions []int, attempt int, reason string) int {
	count := 0
	for _, position := range positions {
		if position < 0 || position >= len(records) {
			continue
		}
		record := &records[position]
		if record.Attempt != attempt || record.Status != store.NodePending {
			continue
		}
		record.Status = store.RunCancelled
		record.ErrorCode = string(execution.KindCancelled)
		record.Error = reason
		record.CancelReason = reason
		count++
	}
	return count
}

func fanOutWaitingIDs(records []store.ChildRunItemState, attempt int) []string {
	var ids []string
	for _, record := range records {
		if record.Attempt == attempt && record.Status == store.RunWaiting {
			ids = append(ids, record.RunID)
		}
	}
	return ids
}

func fanOutTerminalCounts(records []store.ChildRunItemState, attempt int) (completed, failed int) {
	for _, record := range records {
		if record.Attempt != attempt {
			continue
		}
		if record.Status == store.RunCompleted {
			completed++
		} else {
			failed++
		}
	}
	return completed, failed
}

func fanOutStatusCount(records []store.ChildRunItemState, attempt int, status string) int {
	count := 0
	for _, record := range records {
		if record.Attempt == attempt && record.Status == status {
			count++
		}
	}
	return count
}

func normalizedMaxParallel(value int) int {
	if value <= 0 {
		return 1
	}
	return value
}

func normalizedFanOutJoin(value string) string {
	if value == "" {
		return "all_success"
	}
	return value
}

func validateChildOutputSelection(childWorkflow *spec.Workflow, childPath, outputNode string) error {
	if strings.TrimSpace(outputNode) != "" {
		for _, childNode := range childWorkflow.Nodes {
			if childNode.ID == outputNode && !childNode.Hidden && childNode.PublicParent == "" {
				return nil
			}
		}
		return fmt.Errorf("output_node %q does not exist in %s", outputNode, childPath)
	}
	if singleTerminalNode(childWorkflow.Nodes) == "" {
		return fmt.Errorf("child workflow %s has multiple terminal nodes; set output_node", childPath)
	}
	return nil
}
