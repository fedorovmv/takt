package redact

import (
	"encoding/json"
	"strings"

	"takt/internal/store"
)

// CloneRunState creates the persistence copy used by runtime and control
// paths. Call RedactRunState before committing the returned value.
func CloneRunState(state *store.RunState) (*store.RunState, error) {
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

// RedactRunState removes known secrets from every durable textual field that
// can contain model, tool, approval, adapter or reconciliation data.
func RedactRunState(r *Redactor, state *store.RunState) {
	if r == nil || state == nil {
		return
	}
	state.Input = r.String(state.Input)
	state.Output = r.String(state.Output)
	state.Error = r.String(state.Error)
	state.AbandonReason = r.String(state.AbandonReason)
	state.CancelReason = r.String(state.CancelReason)
	if state.Waiting != nil {
		state.Waiting.Message = r.String(state.Waiting.Message)
	}
	for key, value := range state.Approvals {
		state.Approvals[key] = r.String(value)
	}
	for _, node := range state.Nodes {
		RedactNodeState(r, node)
	}
}

func RedactNodeState(r *Redactor, node *store.NodeState) {
	if r == nil || node == nil {
		return
	}
	node.Output = r.String(node.Output)
	node.Stdout = r.String(node.Stdout)
	node.Stderr = r.String(node.Stderr)
	node.Prompt = r.String(node.Prompt)
	node.Feedback = r.String(node.Feedback)
	node.Error = r.String(node.Error)
	if node.Diagnostic != nil {
		node.Diagnostic.Message = r.String(node.Diagnostic.Message)
	}
	for i := range node.Executions {
		node.Executions[i].Prompt = r.String(node.Executions[i].Prompt)
		node.Executions[i].Error = r.String(node.Executions[i].Error)
		if node.Executions[i].Diagnostic != nil {
			node.Executions[i].Diagnostic.Message = r.String(node.Executions[i].Diagnostic.Message)
		}
	}
	for key, previous := range node.LoopPrevious {
		copy := previous
		RedactNodeState(r, &copy)
		node.LoopPrevious[key] = copy
	}
	for i := range node.LoopIterations {
		if node.LoopIterations[i].UntilBash != nil {
			node.LoopIterations[i].UntilBash.Stdout = r.String(node.LoopIterations[i].UntilBash.Stdout)
			node.LoopIterations[i].UntilBash.Stderr = r.String(node.LoopIterations[i].UntilBash.Stderr)
		}
		for key, previous := range node.LoopIterations[i].Nodes {
			copy := previous
			RedactNodeState(r, &copy)
			node.LoopIterations[i].Nodes[key] = copy
		}
	}
	for i := range node.ChildRuns {
		node.ChildRuns[i].Output = r.String(node.ChildRuns[i].Output)
		node.ChildRuns[i].Error = r.String(node.ChildRuns[i].Error)
		node.ChildRuns[i].CancelReason = r.String(node.ChildRuns[i].CancelReason)
	}
	if node.DomainOperation != nil {
		node.DomainOperation.Receipt = r.String(node.DomainOperation.Receipt)
	}
	if node.External != nil {
		node.External.Prompt = r.String(node.External.Prompt)
		node.External.Receipt = r.String(node.External.Receipt)
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

func EventData(r *Redactor, data map[string]any) map[string]any {
	if r == nil || data == nil {
		return data
	}
	return r.Map(data)
}

func TextualMIME(mime string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.Split(mime, ";")[0]))
	return strings.HasPrefix(base, "text/") || base == "application/json" || base == "application/xml" || strings.HasSuffix(base, "+json") || strings.HasSuffix(base, "+xml")
}
