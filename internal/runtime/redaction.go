package runtime

import (
	"encoding/json"

	"takt/internal/redact"
	"takt/internal/store"
)

func cloneRunStateForPersistence(state *store.RunState) (*store.RunState, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	var out store.RunState
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func redactRunState(r *redact.Redactor, state *store.RunState) {
	if r == nil || state == nil {
		return
	}
	state.Input = r.String(state.Input)
	state.Output = r.String(state.Output)
	state.Error = r.String(state.Error)
	state.AbandonReason = r.String(state.AbandonReason)
	if state.Waiting != nil {
		state.Waiting.Message = r.String(state.Waiting.Message)
	}
	for key, value := range state.Approvals {
		state.Approvals[key] = r.String(value)
	}
	for _, node := range state.Nodes {
		redactNodeState(r, node)
	}
}

func redactNodeState(r *redact.Redactor, node *store.NodeState) {
	if node == nil {
		return
	}
	node.Output = r.String(node.Output)
	node.Stdout = r.String(node.Stdout)
	node.Stderr = r.String(node.Stderr)
	node.Feedback = r.String(node.Feedback)
	node.Error = r.String(node.Error)
	if node.Diagnostic != nil {
		node.Diagnostic.Message = r.String(node.Diagnostic.Message)
	}
	for i := range node.Executions {
		node.Executions[i].Error = r.String(node.Executions[i].Error)
		if node.Executions[i].Diagnostic != nil {
			node.Executions[i].Diagnostic.Message = r.String(node.Executions[i].Diagnostic.Message)
		}
	}
	for key, previous := range node.LoopPrevious {
		copy := previous
		redactNodeState(r, &copy)
		node.LoopPrevious[key] = copy
	}
	for i := range node.ChildRuns {
		node.ChildRuns[i].Output = r.String(node.ChildRuns[i].Output)
		node.ChildRuns[i].Error = r.String(node.ChildRuns[i].Error)
	}
	if node.External != nil {
		node.External.Prompt = r.String(node.External.Prompt)
		for _, call := range node.External.ToolCalls {
			if call == nil {
				continue
			}
			if redacted, ok := r.Any(call.Input).(json.RawMessage); ok {
				call.Input = redacted
			}
			if redacted, ok := r.Any(call.Output).(json.RawMessage); ok {
				call.Output = redacted
			}
			call.Reason = r.String(call.Reason)
		}
		if node.External.Result != nil {
			node.External.Result.Output = r.String(node.External.Result.Output)
			node.External.Result.Stdout = r.String(node.External.Result.Stdout)
			node.External.Result.Stderr = r.String(node.External.Result.Stderr)
			node.External.Result.Error = r.String(node.External.Result.Error)
			if redacted, ok := r.Any(node.External.Result.Structured).(json.RawMessage); ok {
				node.External.Result.Structured = redacted
			}
		}
	}
}
