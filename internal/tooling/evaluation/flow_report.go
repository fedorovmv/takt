package evaluation

import (
	"fmt"
	"sort"

	"takt/internal/store"
	"takt/internal/validation"
)

type FlowValidationRecord struct {
	Status     string             `json:"status"`
	ErrorCode  string             `json:"error_code,omitempty"`
	Error      string             `json:"error,omitempty"`
	Result     *validation.Result `json:"result,omitempty"`
	DurationMS int64              `json:"duration_ms"`
}

type FlowCleanupRecord struct {
	Status string   `json:"status"`
	Error  string   `json:"error,omitempty"`
	Paths  []string `json:"paths,omitempty"`
}

type FlowSummary struct {
	EvaluatedRuns        int      `json:"evaluated_runs"`
	FlowCompleted        int      `json:"flow_completed"`
	TrueAccept           int      `json:"true_accept"`
	FalseAccept          int      `json:"false_accept"`
	TrueReject           int      `json:"true_reject"`
	FalseReject          int      `json:"false_reject"`
	ValidationErrors     int      `json:"validation_errors"`
	InfrastructureErrors int      `json:"infrastructure_errors"`
	ValidRate            *float64 `json:"valid_rate"`
	FalseAcceptRate      *float64 `json:"false_accept_rate"`
	FalseRejectRate      *float64 `json:"false_reject_rate"`
	FlowCompletionRate   *float64 `json:"flow_completion_rate"`
	ValidationErrorRate  *float64 `json:"validation_error_rate"`
}

func ClassifyFlowRecord(record *RunRecord) {
	if record == nil {
		return
	}
	if isProviderUnavailableRecord(*record) {
		record.Outcome, record.RunPassed = "infrastructure_error", nil
		record.QualityExpected, record.Quality = false, nil
		return
	}
	if record.Validation == nil || record.Validation.Status != "completed" || record.Validation.Result == nil {
		return
	}
	record.QualityExpected = true
	record.Quality = record.Validation.Result
	valid := record.Validation.Result.Valid
	switch {
	case record.Status == store.RunCompleted && valid:
		record.Outcome, record.RunPassed = "true_accept", boolPointer(true)
	case record.Status == store.RunCompleted:
		record.Outcome, record.RunPassed = "false_accept", boolPointer(false)
	case !valid:
		record.Outcome, record.RunPassed = "true_reject", boolPointer(false)
	default:
		record.Outcome, record.RunPassed = "false_reject", boolPointer(false)
	}
}

func isProviderUnavailableRecord(record RunRecord) bool {
	if record.Status == store.RunFailed && providerUnavailable(record.ErrorCode, nil) {
		return true
	}
	for _, node := range record.Nodes {
		// Node-level fields describe the terminal node outcome. Historical
		// execution records are evidence, but an earlier provider outage must
		// not relabel a node that later completed successfully.
		if !nodeProviderFailureStatus(node.Status) {
			continue
		}
		if providerUnavailable(node.ErrorCode, node.Diagnostic) {
			return true
		}
		if len(node.Executions) > 0 {
			execution := node.Executions[len(node.Executions)-1]
			if nodeProviderFailureStatus(execution.Status) && providerUnavailable(execution.ErrorCode, execution.Diagnostic) {
				return true
			}
		}
	}
	return false
}

func nodeProviderFailureStatus(status string) bool {
	switch status {
	case string(store.NodeFailed), string(store.NodeErrored), string(store.NodeBlocked):
		return true
	default:
		return false
	}
}

func providerUnavailable(errorCode string, diagnostic *store.DiagnosticState) bool {
	if errorCode == "provider_unavailable" {
		return true
	}
	return diagnostic != nil && (diagnostic.Code == "provider_unavailable" || diagnostic.Kind == "provider_unavailable")
}

func ApplyFlowGates(gates FlowGates, summary Summary) []GateResult {
	flow := summary.Flow
	if flow == nil {
		flow = &FlowSummary{}
	}
	var results []GateResult
	for _, item := range []struct {
		name  string
		value *float64
		gate  FlowThreshold
	}{
		{"validation_error_rate", flow.ValidationErrorRate, gates.ValidationErrorRate},
		{"valid_rate", flow.ValidRate, gates.ValidRate},
		{"false_accept_rate", flow.FalseAcceptRate, gates.FalseAcceptRate},
		{"false_reject_rate", flow.FalseRejectRate, gates.FalseRejectRate},
		{"flow_completion_rate", flow.FlowCompletionRate, gates.FlowCompletionRate},
	} {
		if item.gate.Min != nil {
			passed := item.value != nil && *item.value >= *item.gate.Min
			results = append(results, GateResult{Passed: passed, Message: fmt.Sprintf("%s actual=%s min=%.6g", item.name, formatMetric(item.value), *item.gate.Min)})
		}
		if item.gate.Max != nil {
			passed := item.value != nil && *item.value <= *item.gate.Max
			results = append(results, GateResult{Passed: passed, Message: fmt.Sprintf("%s actual=%s max=%.6g", item.name, formatMetric(item.value), *item.gate.Max)})
		}
	}
	if gates.UnstableCases.Max != nil {
		passed := summary.UnstableCases <= *gates.UnstableCases.Max
		results = append(results, GateResult{Passed: passed, Message: fmt.Sprintf("unstable_cases actual=%d max=%d", summary.UnstableCases, *gates.UnstableCases.Max)})
	}
	return results
}

func boolPointer(value bool) *bool { return &value }

// recordFromFlowStates keeps root usage authoritative: expanded nodes and
// governed children retain identity details but must not be added twice.
func recordFromFlowStates(caseID string, repeat int, workspace string, state *store.RunState, repository store.Repository) RunRecord {
	record := RunRecord{CaseID: caseID, Repeat: repeat, RunID: state.ID, Status: state.Status, Workspace: workspace, Mode: "flow", DurationMS: state.UpdatedAt.Sub(state.CreatedAt).Milliseconds(), Answers: len(state.Approvals), ErrorCode: state.ErrorCode, Error: state.Error, Nodes: map[string]NodeRecord{}}
	if state.Usage != nil {
		record.InputTokens, record.OutputTokens, record.Cost = state.Usage.InputTokens, state.Usage.OutputTokens, state.Usage.Cost
	}
	addFlowStateNodes(&record, state.ID, state.Nodes)
	if repository == nil {
		return record
	}
	loadFlowChildren(&record, state.ID, state, repository, map[string]bool{state.ID: true})
	return record
}

func loadFlowChildren(record *RunRecord, prefix string, state *store.RunState, repository store.Repository, seen map[string]bool) {
	for _, id := range flowChildRunIDs(state.Nodes) {
		if seen[id] {
			continue
		}
		seen[id] = true
		child, err := repository.Load(id)
		if err != nil || child == nil {
			continue
		}
		childPrefix := prefix + "/" + id
		addFlowStateNodes(record, childPrefix, child.Nodes)
		loadFlowChildren(record, childPrefix, child, repository, seen)
	}
}

func flowChildRunIDs(nodes map[string]*store.NodeState) []string {
	ids, seen := []string{}, map[string]bool{}
	var visit func(map[string]*store.NodeState)
	visit = func(values map[string]*store.NodeState) {
		for _, node := range values {
			if node == nil {
				continue
			}
			for _, id := range append(append([]string(nil), node.ChildRunIDs...), node.ChildRunID) {
				if id != "" && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			for _, child := range node.ChildRuns {
				if child.RunID != "" && !seen[child.RunID] {
					seen[child.RunID] = true
					ids = append(ids, child.RunID)
				}
			}
			for _, iteration := range node.LoopIterations {
				loopNodes := make(map[string]*store.NodeState, len(iteration.Nodes))
				for id, item := range iteration.Nodes {
					copy := item
					loopNodes[id] = &copy
				}
				visit(loopNodes)
			}
		}
	}
	visit(nodes)
	sort.Strings(ids)
	return ids
}

func addFlowStateNodes(record *RunRecord, prefix string, nodes map[string]*store.NodeState) {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		node := nodes[id]
		if node == nil {
			continue
		}
		if flowActionNode(node) {
			addFlowNode(record, prefix+"/"+id, *node)
		}
		for _, iteration := range node.LoopIterations {
			loopNodes := make(map[string]*store.NodeState, len(iteration.Nodes))
			for childID, child := range iteration.Nodes {
				copy := child
				loopNodes[childID] = &copy
			}
			addFlowStateNodes(record, fmt.Sprintf("%s/%s[%03d]", prefix, id, iteration.Iteration), loopNodes)
		}
	}
}

func flowActionNode(node *store.NodeState) bool {
	if len(node.Executions) > 0 {
		return true
	}
	return !node.Hidden && node.PublicParent == "" && len(node.LoopIterations) == 0 && len(node.LoopPrevious) == 0 && len(node.ChildRuns) == 0 && node.ChildRunID == "" && len(node.ChildRunIDs) == 0
}

func addFlowNode(record *RunRecord, id string, node store.NodeState) {
	executions := make([]ExecutionRecord, 0, len(node.Executions))
	identities := map[string]struct{}{}
	for _, execution := range node.Executions {
		item := executionRecordFromState(execution)
		executions = append(executions, item)
		if key := executionIdentityKey(item.Assistant, item.AssistantVersion, item.RequestedModel, item.ResolvedModel); key != "" {
			identities[key] = struct{}{}
		}
	}
	mixed := len(identities) > 1
	record.Attempts += node.Attempts
	record.ProviderAttempts += node.ProviderAttempts
	if node.OutputTruncated {
		record.Truncated++
	}
	if node.Resumed {
		record.Resumed++
	}
	if mixed {
		record.MixedIdentityNodes++
	}
	record.Nodes[id] = NodeRecord{Status: node.Status, Attempts: node.Attempts, ProviderAttempts: node.ProviderAttempts, Assistant: node.Assistant, AssistantVersion: node.AssistantVersion, RequestedModel: node.RequestedModel, ResolvedModel: node.ResolvedModel, SessionID: node.SessionID, Resumed: node.Resumed, ExitCode: node.ExitCode, ErrorCode: node.ErrorCode, Error: node.Error, Feedback: node.Feedback, DiagnosticOutput: node.Output, Stdout: node.Stdout, Stderr: node.Stderr, OutputTruncated: node.OutputTruncated, Usage: node.Usage, Diagnostic: node.Diagnostic, MixedIdentity: mixed, Executions: executions}
}
