package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"time"

	"takt/internal/assessment"
	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/flowref"
	"takt/internal/spec"
	"takt/internal/store"
)

const maxMatrixItems = 1024

func (r *Runner) runMatrix(ctx context.Context, state *store.RunState, parent spec.Node) (execResult, error) {
	items, rawItems, fingerprint, err := resolveMatrixItems(parent.Matrix.ItemsFrom, state)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindProtocol, Op: "resolve matrix items", Err: err}
	}
	parentState := state.Nodes[parent.ID]
	if err := r.prepareMatrix(state, parent, parentState, rawItems, fingerprint); err != nil {
		return execResult{}, err
	}
	if err := r.preflightMatrixChildren(state, parent, items); err != nil {
		return execResult{}, err
	}
	for index := 0; index < len(items); index++ {
		branch := &parentState.MatrixBranches[index]
		if branch.Status == store.NodeCompleted {
			continue
		}
		if parentState.MatrixActiveIndex == nil {
			if err := r.startMatrixBranch(state, parent, parentState, index); err != nil {
				return execResult{}, err
			}
		} else if *parentState.MatrixActiveIndex != index {
			return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "resume matrix", Err: fmt.Errorf("active index %d does not match pending branch %d", *parentState.MatrixActiveIndex, index)}
		}
		if err := r.executeGraph(ctx, state, parent.Matrix.Nodes, nil); err != nil {
			return execResult{}, err
		}
		local := make(map[string]store.NodeState, len(parent.Matrix.Nodes))
		for _, child := range parent.Matrix.Nodes {
			local[child.ID] = *state.Nodes[child.ID]
		}
		output := local[parent.Matrix.OutputNode].Output
		primaryID, cardinalityErr := matrixPrimaryAssessment(parent.Matrix.Nodes, local)
		bodyErr := loopBodyFailure(local)
		if bodyErr == nil {
			bodyErr = cardinalityErr
		}
		if err := r.completeMatrixBranch(state, parent, index, local, output, primaryID, bodyErr); err != nil {
			return execResult{}, err
		}
		if bodyErr != nil {
			return execResult{}, bodyErr
		}
	}
	output, err := matrixOutput(parentState.MatrixBranches)
	if err != nil {
		return execResult{}, &execution.Error{Kind: execution.KindInternal, Op: "encode matrix output", Err: err}
	}
	return execResult{Output: output, Stdout: output, ExitCode: 0, Usage: matrixUsage(parentState.MatrixBranches), Artifacts: matrixArtifacts(parentState.MatrixBranches)}, nil
}

func resolveMatrixItems(source string, state *store.RunState) ([]any, []json.RawMessage, string, error) {
	ref, err := flowref.Parse(source, flowref.NonShell)
	if err != nil || ref.Optional || ref.Default != "" {
		return nil, nil, "", fmt.Errorf("items_from must be one exact reference")
	}
	var raw string
	switch ref.Kind {
	case flowref.KindInput:
		if state.InputFormat != "json" {
			return nil, nil, "", fmt.Errorf("workflow input must declare format json")
		}
		inputDecoder := json.NewDecoder(strings.NewReader(state.Input))
		inputDecoder.UseNumber()
		var input any
		if err := inputDecoder.Decode(&input); err != nil {
			return nil, nil, "", fmt.Errorf("decode workflow JSON input: %w", err)
		}
		if err := inputDecoder.Decode(&struct{}{}); err != io.EOF {
			return nil, nil, "", fmt.Errorf("decode workflow JSON input: trailing JSON value")
		}
		var found bool
		raw, found = jsonPathLookup(state.Input, append([]string{ref.Name}, ref.Path...))
		if !found {
			return nil, nil, "", fmt.Errorf("items_from reference did not resolve")
		}
		if raw == "" {
			raw = "null"
		}
	case flowref.KindNode:
		node := state.Nodes[ref.NodeID]
		if node != nil {
			var found bool
			raw, found = nodePathLookup(*node, ref.Path)
			if found && raw == "" {
				raw = "null"
			}
		}
	default:
		return nil, nil, "", fmt.Errorf("items_from must reference workflow JSON input or node output")
	}
	if strings.TrimSpace(raw) == "" {
		return nil, nil, "", fmt.Errorf("items_from reference did not resolve")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, nil, "", fmt.Errorf("items_from must resolve to a JSON array: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, nil, "", fmt.Errorf("items_from contains trailing JSON value")
	}
	items, ok := value.([]any)
	if !ok {
		return nil, nil, "", fmt.Errorf("items_from must resolve to a JSON array")
	}
	if len(items) > maxMatrixItems {
		return nil, nil, "", fmt.Errorf("matrix has %d items, maximum is %d", len(items), maxMatrixItems)
	}
	rawItems := make([]json.RawMessage, len(items))
	seen := map[string]int{}
	for index, item := range items {
		canonical, err := canonicalMatrixValue(item)
		if err != nil {
			return nil, nil, "", err
		}
		items[index] = canonical
		raw, err := json.Marshal(canonical)
		if err != nil {
			return nil, nil, "", err
		}
		key := string(raw)
		if previous, ok := seen[key]; ok {
			return nil, nil, "", fmt.Errorf("matrix contains duplicate items at indexes %d and %d", previous, index)
		}
		seen[key] = index
		rawItems[index] = raw
	}
	all, err := json.Marshal(rawItems)
	if err != nil {
		return nil, nil, "", err
	}
	sum := sha256.Sum256(all)
	return items, rawItems, hex.EncodeToString(sum[:]), nil
}

func canonicalMatrixValue(value any) (any, error) {
	switch typed := value.(type) {
	case json.Number:
		return canonicalMatrixNumber(typed)
	case []any:
		for index := range typed {
			canonical, err := canonicalMatrixValue(typed[index])
			if err != nil {
				return nil, err
			}
			typed[index] = canonical
		}
	case map[string]any:
		for key := range typed {
			canonical, err := canonicalMatrixValue(typed[key])
			if err != nil {
				return nil, err
			}
			typed[key] = canonical
		}
	}
	return value, nil
}

func canonicalMatrixNumber(value json.Number) (json.Number, error) {
	raw := value.String()
	negative := strings.HasPrefix(raw, "-")
	if negative {
		raw = raw[1:]
	}
	exponent := new(big.Int)
	if index := strings.IndexAny(raw, "eE"); index >= 0 {
		if _, ok := exponent.SetString(raw[index+1:], 10); !ok {
			return "", fmt.Errorf("invalid matrix number %q", value)
		}
		raw = raw[:index]
	}
	integer, fraction := raw, ""
	if index := strings.IndexByte(raw, '.'); index >= 0 {
		integer, fraction = raw[:index], raw[index+1:]
	}
	exponent.Sub(exponent, big.NewInt(int64(len(fraction))))
	digits := strings.TrimLeft(integer+fraction, "0")
	if digits == "" {
		return "0", nil
	}
	trimmed := strings.TrimRight(digits, "0")
	exponent.Add(exponent, big.NewInt(int64(len(digits)-len(trimmed))))
	prefix := ""
	if negative {
		prefix = "-"
	}
	if exponent.Sign() == 0 {
		return json.Number(prefix + trimmed), nil
	}
	return json.Number(prefix + trimmed + "e" + exponent.String()), nil
}

func (r *Runner) prepareMatrix(state *store.RunState, parent spec.Node, parentState *store.NodeState, items []json.RawMessage, fingerprint string) error {
	if parentState.MatrixFingerprint != "" {
		if parentState.MatrixFingerprint != fingerprint || len(parentState.MatrixBranches) != len(items) {
			return &execution.Error{Kind: execution.KindProtocol, Op: "resume matrix", Err: fmt.Errorf("matrix_items_changed")}
		}
		return nil
	}
	parentState.MatrixFingerprint = fingerprint
	parentState.MatrixBranches = make([]store.MatrixBranchState, len(items))
	for index, raw := range items {
		sum := sha256.Sum256(raw)
		parentState.MatrixBranches[index] = store.MatrixBranchState{Index: index, Item: append(json.RawMessage(nil), raw...), ItemFingerprint: hex.EncodeToString(sum[:]), Status: store.NodePending}
	}
	return r.commit(state, "matrix.prepared", parent.ID, map[string]any{"items": len(items), "fingerprint": fingerprint})
}

func (r *Runner) startMatrixBranch(state *store.RunState, parent spec.Node, parentState *store.NodeState, index int) error {
	for _, child := range parent.Matrix.Nodes {
		if _, exists := state.Nodes[child.ID]; exists {
			return &execution.Error{Kind: execution.KindInternal, Op: "start matrix branch", Err: fmt.Errorf("child node state %q already exists", child.ID)}
		}
	}
	for _, child := range parent.Matrix.Nodes {
		childState := &store.NodeState{Status: store.NodePending, Path: matrixChildNodePath(parent.ID, index, child.ID), Hidden: child.Hidden, PublicParent: child.PublicParent}
		if identity, ok := parentState.MatrixBranches[index].ChildWorkflows[child.ID]; ok {
			childState.ChildWorkflowPath = identity.Path
			childState.ChildControlWorkspace = identity.Repository
			childState.ChildWorkflowHash = identity.Fingerprint
		}
		state.Nodes[child.ID] = childState
	}
	active := index
	parentState.MatrixActiveIndex = &active
	parentState.MatrixBranches[index].Status = store.NodeRunning
	return r.commit(state, "matrix.branch.started", parent.ID, map[string]any{"index": index, "item_fingerprint": parentState.MatrixBranches[index].ItemFingerprint})
}

func (r *Runner) completeMatrixBranch(state *store.RunState, parent spec.Node, index int, local map[string]store.NodeState, output, primaryID string, branchErr error) error {
	parentState := state.Nodes[parent.ID]
	branch := &parentState.MatrixBranches[index]
	branch.Nodes = cloneLoopNodes(local)
	branch.Output = output
	branch.PrimaryAssessmentID = primaryID
	branch.CompletedAt = time.Now().UTC()
	branch.Status = store.NodeCompleted
	if branchErr != nil {
		branch.Status = store.NodeFailed
	}
	parentState.MatrixActiveIndex = nil
	for _, child := range parent.Matrix.Nodes {
		delete(state.Nodes, child.ID)
		delete(state.Approvals, child.ID)
	}
	data := map[string]any{"index": index, "status": branch.Status, "primary_assessment_id": primaryID}
	if branchErr != nil {
		data["error"] = branchErr.Error()
		data["error_code"] = string(execution.KindOf(branchErr))
	}
	return r.commit(state, "matrix.branch.completed", parent.ID, data)
}

func matrixPrimaryAssessment(nodes []spec.Node, states map[string]store.NodeState) (string, error) {
	declared := false
	var ids []string
	for _, node := range nodes {
		if node.Assessment == nil || node.Assessment.Role != assessment.RolePrimary {
			continue
		}
		declared = true
		for _, artifact := range states[node.ID].Artifacts {
			if artifact.Type == assessment.TypeAssessment {
				ids = append(ids, artifact.ID)
			}
		}
	}
	if !declared {
		return "", nil
	}
	if len(ids) != 1 {
		return "", &execution.Error{Kind: execution.Kind("assessment_ambiguous"), Op: "matrix primary assessment", Err: fmt.Errorf("branch produced %d primary assessments, expected 1", len(ids))}
	}
	return ids[0], nil
}

func matrixOutput(branches []store.MatrixBranchState) (string, error) {
	values := make([]any, len(branches))
	for index, branch := range branches {
		values[index] = branch.Output
		if strings.TrimSpace(branch.Output) != "" {
			var value any
			decoder := json.NewDecoder(strings.NewReader(branch.Output))
			decoder.UseNumber()
			if decoder.Decode(&value) == nil {
				values[index] = value
			}
		}
	}
	raw, err := json.Marshal(values)
	return string(raw), err
}

func matrixUsage(branches []store.MatrixBranchState) *assistant.ProtocolUsage {
	usage := &assistant.ProtocolUsage{}
	seen := false
	for _, branch := range branches {
		for _, node := range branch.Nodes {
			if node.Usage == nil {
				continue
			}
			seen = true
			usage.InputTokens += node.Usage.InputTokens
			usage.OutputTokens += node.Usage.OutputTokens
			usage.Cost += node.Usage.Cost
		}
	}
	if !seen {
		return nil
	}
	return usage
}

func matrixArtifacts(branches []store.MatrixBranchState) []store.ArtifactRef {
	var artifacts []store.ArtifactRef
	for _, branch := range branches {
		for _, node := range branch.Nodes {
			for _, artifact := range node.Artifacts {
				artifacts = appendArtifactUnique(artifacts, artifact)
			}
		}
	}
	return artifacts
}

func matrixChildNodePath(parentID string, index int, childID string) string {
	suffix := strings.TrimPrefix(childID, parentID+"__")
	path := fmt.Sprintf("/%s[%d]", parentID, index)
	for _, part := range strings.Split(suffix, "__") {
		if part != "" {
			path += "/" + part
		}
	}
	return path
}

func activeMatrixValue(state *store.RunState, name string, path []string) (string, bool) {
	for _, node := range state.Nodes {
		if node == nil || node.MatrixActiveIndex == nil {
			continue
		}
		index := *node.MatrixActiveIndex
		if index < 0 || index >= len(node.MatrixBranches) {
			return "", false
		}
		switch name {
		case "index":
			return strconv.Itoa(index), true
		case "total":
			return strconv.Itoa(len(node.MatrixBranches)), true
		case "item":
			if len(path) == 0 {
				var item any
				decoder := json.NewDecoder(strings.NewReader(string(node.MatrixBranches[index].Item)))
				decoder.UseNumber()
				if decoder.Decode(&item) != nil {
					return "", false
				}
				return fanOutValueString(item), true
			}
			return jsonPathLookup(string(node.MatrixBranches[index].Item), path)
		}
	}
	return "", false
}

func (r *Runner) preflightMatrixChildren(state *store.RunState, parent spec.Node, items []any) error {
	hasChild := false
	for _, node := range parent.Matrix.Nodes {
		if node.WorkflowRun != nil && node.WorkflowRun.FanOut == nil {
			hasChild = true
			break
		}
	}
	if !hasChild {
		return nil
	}
	parentState := state.Nodes[parent.ID]
	changed := false
	for index, item := range items {
		resolved := map[string]store.ChildWorkflowIdentityState{}
		for _, node := range parent.Matrix.Nodes {
			if node.WorkflowRun == nil || node.WorkflowRun.FanOut != nil {
				continue
			}
			identity, child, err := r.resolveMatrixChildWorkflow(state, node, item, index, len(items))
			if err != nil {
				return matrixPreflightError(index, node.ID, err)
			}
			input, err := renderMatrixItemTemplate(node.WorkflowRun.Input, state, item, index, len(items))
			if err != nil {
				return matrixPreflightError(index, node.ID, err)
			}
			if _, err := ValidateWorkflowInput(input, child.Input); err != nil {
				return matrixPreflightError(index, node.ID, fmt.Errorf("validate child workflow input: %w", err))
			}
			resolved[node.ID] = identity
		}
		branch := &parentState.MatrixBranches[index]
		if branch.ChildWorkflows == nil {
			branch.ChildWorkflows = resolved
			changed = true
			continue
		}
		if !sameChildWorkflowIdentities(branch.ChildWorkflows, resolved) {
			return matrixPreflightError(index, parent.ID, fmt.Errorf("child workflow definition changed since matrix preflight"))
		}
	}
	if !changed {
		return nil
	}
	return r.commit(state, "matrix.preflighted", parent.ID, map[string]any{"items": len(items)})
}

func (r *Runner) resolveMatrixChildWorkflow(state *store.RunState, node spec.Node, item any, index, total int) (store.ChildWorkflowIdentityState, *spec.Workflow, error) {
	definition := node.WorkflowRun
	if dynamicChildWorkflow(definition) {
		path, err := renderMatrixItemTemplate(definition.Path, state, item, index, total)
		if err != nil {
			return store.ChildWorkflowIdentityState{}, nil, err
		}
		repository, err := renderMatrixItemTemplate(definition.Repository, state, item, index, total)
		if err != nil {
			return store.ChildWorkflowIdentityState{}, nil, err
		}
		return r.resolveDynamicChildWorkflow(path, repository, definition.OutputNode)
	}
	path, child, err := r.loadChildWorkflow(definition)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, err
	}
	repository, err := r.resolveChildControlWorkspace(definition.Repository)
	if err != nil {
		return store.ChildWorkflowIdentityState{}, nil, err
	}
	identity, err := r.staticChildWorkflowIdentity(path, repository, child)
	return identity, child, err
}

func matrixPreflightError(index int, nodeID string, err error) error {
	return &execution.Error{Kind: execution.KindConfiguration, Op: fmt.Sprintf("preflight matrix item %d child %s", index, nodeID), Err: err}
}

func sameChildWorkflowIdentities(left, right map[string]store.ChildWorkflowIdentityState) bool {
	if len(left) != len(right) {
		return false
	}
	for id, value := range left {
		if right[id] != value {
			return false
		}
	}
	return true
}

func renderMatrixItemTemplate(source string, state *store.RunState, item any, index, total int) (string, error) {
	extra := func(key string) (string, bool) {
		switch key {
		case "matrix.index":
			return strconv.Itoa(index), true
		case "matrix.total":
			return strconv.Itoa(total), true
		case "matrix.item":
			return fanOutValueString(item), true
		}
		if strings.HasPrefix(key, "matrix.item.") {
			return fanOutPathLookup(item, strings.Split(strings.TrimPrefix(key, "matrix.item."), "."))
		}
		return "", false
	}
	return renderTemplateWithResolver(source, state, nil, "", "", extra)
}
