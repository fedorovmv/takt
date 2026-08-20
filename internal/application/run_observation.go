package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"takt/internal/assessment"
	"takt/internal/store"
)

type RunMatrixProgress struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type RunAssessmentSummary struct {
	Primary  int            `json:"primary"`
	Advisory int            `json:"advisory"`
	Outcomes map[string]int `json:"outcomes,omitempty"`
}

type RunStatusResult struct {
	RunID       string               `json:"run_id"`
	Status      string               `json:"status"`
	ErrorCode   string               `json:"error_code,omitempty"`
	Error       string               `json:"error,omitempty"`
	CurrentNode string               `json:"current_node,omitempty"`
	Matrix      RunMatrixProgress    `json:"matrix"`
	Attempts    int                  `json:"attempts"`
	Executions  int                  `json:"executions"`
	Usage       *store.Usage         `json:"usage,omitempty"`
	Assessment  RunAssessmentSummary `json:"assessment"`
}

type MetricRatio struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value"`
}

type RunStatsQuery struct {
	RunID      string `json:"run_id"`
	CheckGates bool   `json:"check_gates,omitempty"`
}

type AssessmentGateResult struct {
	Metric      string   `json:"metric"`
	Passed      bool     `json:"passed"`
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Actual      *float64 `json:"actual"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	Message     string   `json:"message"`
}

type RunStatsResult struct {
	RunID               string                 `json:"run_id"`
	Status              string                 `json:"status"`
	Total               int                    `json:"total"`
	Evaluated           int                    `json:"evaluated"`
	Attempts            int                    `json:"attempts"`
	Executions          int                    `json:"executions"`
	Usage               *store.Usage           `json:"usage,omitempty"`
	Outcomes            map[string]int         `json:"outcomes"`
	ValidRate           MetricRatio            `json:"valid_rate"`
	FalseAcceptRate     MetricRatio            `json:"false_accept_rate"`
	FalseRejectRate     MetricRatio            `json:"false_reject_rate"`
	FlowCompletionRate  MetricRatio            `json:"flow_completion_rate"`
	ValidationErrorRate MetricRatio            `json:"validation_error_rate"`
	Gates               []AssessmentGateResult `json:"gates,omitempty"`
	GatesPassed         bool                   `json:"gates_passed"`
}

type AssessmentGateFailureError struct{ Results []AssessmentGateResult }

func (e *AssessmentGateFailureError) Error() string {
	messages := make([]string, 0, len(e.Results))
	for _, result := range e.Results {
		if !result.Passed {
			messages = append(messages, result.Message)
		}
	}
	return "assessment gates failed: " + strings.Join(messages, "; ")
}

func (r *RunStatsResult) GateFailure() error {
	if r == nil || r.GatesPassed {
		return nil
	}
	return &AssessmentGateFailureError{Results: append([]AssessmentGateResult(nil), r.Gates...)}
}

type RunInspectQuery struct {
	RunID  string `json:"run_id"`
	CaseID string `json:"case_id,omitempty"`
	Repeat int    `json:"repeat,omitempty"`
	NodeID string `json:"node_id,omitempty"`
}

type RunCause struct {
	Source  string `json:"source"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type RunCaseInspection struct {
	CaseID       string                   `json:"case_id"`
	Repeat       int                      `json:"repeat"`
	TargetRunID  string                   `json:"target_run_id,omitempty"`
	TargetStatus string                   `json:"target_status,omitempty"`
	Outcome      string                   `json:"outcome,omitempty"`
	Valid        *bool                    `json:"valid,omitempty"`
	Cause        RunCause                 `json:"cause"`
	Evidence     []assessment.EvidenceRef `json:"evidence,omitempty"`
}

type RunNodeInspection struct {
	RunID       string `json:"run_id"`
	NodeID      string `json:"node_id"`
	Path        string `json:"path,omitempty"`
	BranchIndex *int   `json:"branch_index,omitempty"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	Executions  int    `json:"executions"`
	ErrorCode   string `json:"error_code,omitempty"`
	Error       string `json:"error,omitempty"`
}

type RunInspectResult struct {
	RunID      string              `json:"run_id"`
	Status     string              `json:"status"`
	Attempts   int                 `json:"attempts"`
	Executions int                 `json:"executions"`
	Usage      *store.Usage        `json:"usage,omitempty"`
	Cause      RunCause            `json:"cause"`
	Cases      []RunCaseInspection `json:"cases"`
	Nodes      []RunNodeInspection `json:"nodes,omitempty"`
}

type runObservation struct {
	snapshot       *EvaluationSnapshot
	assessments    []AssessmentRecord
	allAssessments []AssessmentRecord
	matrix         RunMatrixProgress
	attempts       int
	executions     int
}

func (s *RunService) Status(runID string) (*RunStatusResult, error) {
	facts, err := s.observeRun(runID)
	if err != nil {
		return nil, err
	}
	root := facts.snapshot.Root
	result := &RunStatusResult{RunID: root.ID, Status: root.Status, ErrorCode: root.ErrorCode, Error: root.Error, CurrentNode: root.CurrentNode, Matrix: facts.matrix, Attempts: facts.attempts, Executions: facts.executions, Usage: cloneUsage(root.Usage), Assessment: RunAssessmentSummary{Outcomes: map[string]int{}}}
	for _, record := range facts.assessments {
		switch record.Assessment.Role {
		case assessment.RolePrimary:
			result.Assessment.Primary++
			result.Assessment.Outcomes[record.Assessment.Outcome]++
		case assessment.RoleAdvisory:
			result.Assessment.Advisory++
		}
	}
	return result, nil
}

func (s *RunService) Stats(query RunStatsQuery) (*RunStatsResult, error) {
	facts, err := s.observeRun(query.RunID)
	if err != nil {
		return nil, err
	}
	root := facts.snapshot.Root
	outcomes := map[string]int{}
	evaluated, targetsCompleted := 0, 0
	for _, record := range facts.assessments {
		if record.Assessment.Role != assessment.RolePrimary {
			continue
		}
		evaluated++
		outcomes[record.Assessment.Outcome]++
	}
	currentStatus := map[string]string{}
	for _, state := range facts.snapshot.States {
		currentStatus[state.ID] = state.Status
	}
	seenTarget := map[string]bool{}
	for _, record := range facts.allAssessments {
		value := record.Assessment
		if value.Role != assessment.RolePrimary || seenTarget[value.Target.RunID] {
			continue
		}
		seenTarget[value.Target.RunID] = true
		status := currentStatus[value.Target.RunID]
		if status == "" {
			status = value.Target.Status
		}
		if status == store.RunCompleted {
			targetsCompleted++
		}
	}
	total := facts.matrix.Total
	result := &RunStatsResult{
		RunID: root.ID, Status: root.Status, Total: total, Evaluated: evaluated, Attempts: facts.attempts, Executions: facts.executions, Usage: cloneUsage(root.Usage), Outcomes: outcomes,
		ValidRate: ratio(outcomes[assessment.OutcomeTrueAccept], evaluated), FalseAcceptRate: ratio(outcomes[assessment.OutcomeFalseAccept], evaluated), FalseRejectRate: ratio(outcomes[assessment.OutcomeFalseReject], evaluated),
		FlowCompletionRate: ratio(targetsCompleted, total), ValidationErrorRate: ratio(maxInt(total-evaluated, 0), total), GatesPassed: true,
	}
	if query.CheckGates {
		if root.InputFormat == "json" {
			result.Gates, err = evaluateAssessmentGates(root.Input, result)
			if err != nil {
				return nil, err
			}
		}
		for _, gate := range result.Gates {
			if !gate.Passed {
				result.GatesPassed = false
			}
		}
	}
	return result, nil
}

func (s *RunService) Inspect(query RunInspectQuery) (*RunInspectResult, error) {
	facts, err := s.observeRun(query.RunID)
	if err != nil {
		return nil, err
	}
	root := facts.snapshot.Root
	result := &RunInspectResult{RunID: root.ID, Status: root.Status, Attempts: facts.attempts, Executions: facts.executions, Usage: cloneUsage(root.Usage), Cause: runCause(root), Cases: []RunCaseInspection{}}
	for _, record := range facts.assessments {
		value := record.Assessment
		if value.Role != assessment.RolePrimary || (query.CaseID != "" && value.Scope.CaseID != query.CaseID) || (query.Repeat > 0 && value.Scope.Repeat != query.Repeat) {
			continue
		}
		valid := value.Result.Valid
		item := RunCaseInspection{CaseID: value.Scope.CaseID, Repeat: value.Scope.Repeat, TargetRunID: value.Target.RunID, TargetStatus: value.Target.Status, Outcome: value.Outcome, Valid: &valid, Cause: assessmentCause(value), Evidence: append([]assessment.EvidenceRef(nil), value.Evidence...)}
		result.Cases = append(result.Cases, item)
		if root.Status == store.RunCompleted && result.Cause.Source == "run" && !valid {
			result.Cause = item.Cause
		}
	}
	for _, state := range facts.snapshot.States {
		for id, node := range state.Nodes {
			if node == nil || node.Hidden || node.PublicParent != "" {
				continue
			}
			if query.NodeID == "" || query.NodeID == id {
				result.Nodes = append(result.Nodes, RunNodeInspection{RunID: state.ID, NodeID: id, Status: node.Status, Attempts: node.Attempts, Executions: len(node.Executions), ErrorCode: node.ErrorCode, Error: node.Error})
			}
			for branchIndex, branch := range node.MatrixBranches {
				for childID, child := range branch.Nodes {
					publicID := strings.TrimPrefix(childID, id+"__")
					if query.NodeID != "" && query.NodeID != childID && query.NodeID != publicID {
						continue
					}
					index := branchIndex
					result.Nodes = append(result.Nodes, RunNodeInspection{RunID: state.ID, NodeID: publicID, Path: child.Path, BranchIndex: &index, Status: child.Status, Attempts: child.Attempts, Executions: len(child.Executions), ErrorCode: child.ErrorCode, Error: child.Error})
				}
			}
		}
	}
	sort.SliceStable(result.Nodes, func(i, j int) bool {
		if result.Nodes[i].RunID != result.Nodes[j].RunID {
			return result.Nodes[i].RunID < result.Nodes[j].RunID
		}
		return result.Nodes[i].NodeID < result.Nodes[j].NodeID
	})
	return result, nil
}

func (s *RunService) observeRun(runID string) (*runObservation, error) {
	snapshot, err := s.EvaluationSnapshot(runID)
	if err != nil {
		return nil, err
	}
	assessments, err := s.Assessments(AssessmentQuery{RunID: runID, IncludeStale: true})
	if err != nil {
		return nil, err
	}
	facts := &runObservation{snapshot: snapshot, allAssessments: assessments.Assessments}
	for _, record := range assessments.Assessments {
		if !record.Stale {
			facts.assessments = append(facts.assessments, record)
		}
	}
	for _, state := range snapshot.States {
		for _, node := range state.Nodes {
			if node == nil || node.Hidden || node.PublicParent != "" {
				continue
			}
			facts.attempts += node.Attempts
			facts.executions += len(node.Executions)
			for _, branch := range node.MatrixBranches {
				if state.ID == snapshot.Root.ID {
					facts.matrix.Total++
					switch branch.Status {
					case store.NodePending:
						facts.matrix.Pending++
					case store.NodeRunning:
						facts.matrix.Running++
					case store.NodeCompleted:
						facts.matrix.Completed++
					case store.NodeFailed:
						facts.matrix.Failed++
					}
				}
				for _, child := range branch.Nodes {
					facts.attempts += child.Attempts
					facts.executions += len(child.Executions)
				}
			}
		}
	}
	return facts, nil
}

func ratio(numerator, denominator int) MetricRatio {
	result := MetricRatio{Numerator: numerator, Denominator: denominator}
	if denominator > 0 {
		value := float64(numerator) / float64(denominator)
		result.Value = &value
	}
	return result
}

func assessmentCause(value assessment.Envelope) RunCause {
	if len(value.Result.Diagnostics) > 0 {
		diagnostic := value.Result.Diagnostics[0]
		return RunCause{Source: "validator", Code: diagnostic.Code, Message: diagnostic.Message}
	}
	if !value.Result.Valid {
		return RunCause{Source: "validator", Code: "invalid", Message: "validation result is invalid"}
	}
	return RunCause{Source: "assessment", Code: value.Outcome}
}

func runCause(root *store.RunState) RunCause {
	if root.ErrorCode != "" || root.Error != "" {
		return RunCause{Source: "run", Code: root.ErrorCode, Message: root.Error}
	}
	return RunCause{Source: "run", Code: root.Status}
}

type gateThreshold struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

func evaluateAssessmentGates(input string, stats *RunStatsResult) ([]AssessmentGateResult, error) {
	var envelope struct {
		Gates map[string]json.RawMessage `json:"gates"`
	}
	if err := json.Unmarshal([]byte(input), &envelope); err != nil {
		return nil, fmt.Errorf("decode Run gates: %w", err)
	}
	metrics := map[string]MetricRatio{"valid_rate": stats.ValidRate, "false_accept_rate": stats.FalseAcceptRate, "false_reject_rate": stats.FalseRejectRate, "flow_completion_rate": stats.FlowCompletionRate, "validation_error_rate": stats.ValidationErrorRate}
	names := make([]string, 0, len(envelope.Gates))
	for name := range envelope.Gates {
		if _, ok := metrics[name]; !ok {
			return nil, fmt.Errorf("unsupported assessment gate %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	results := make([]AssessmentGateResult, 0, len(names))
	for _, name := range names {
		decoder := json.NewDecoder(bytes.NewReader(envelope.Gates[name]))
		decoder.DisallowUnknownFields()
		var threshold gateThreshold
		if err := decoder.Decode(&threshold); err != nil {
			return nil, fmt.Errorf("decode assessment gate %q: %w", name, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("decode assessment gate %q: trailing JSON value", name)
		}
		if (threshold.Min == nil) == (threshold.Max == nil) {
			return nil, fmt.Errorf("assessment gate %q must define exactly one of min or max", name)
		}
		limit := threshold.Min
		if limit == nil {
			limit = threshold.Max
		}
		if *limit < 0 || *limit > 1 {
			return nil, fmt.Errorf("assessment gate %q threshold must be between 0 and 1", name)
		}
		metric := metrics[name]
		passed := metric.Value != nil
		if threshold.Min != nil {
			passed = passed && *metric.Value >= *threshold.Min
		}
		if threshold.Max != nil {
			passed = passed && *metric.Value <= *threshold.Max
		}
		message := fmt.Sprintf("%s actual=%d/%d", name, metric.Numerator, metric.Denominator)
		if metric.Value != nil {
			message += " (" + strconv.FormatFloat(*metric.Value, 'g', -1, 64) + ")"
		}
		if threshold.Min != nil {
			message += " min=" + strconv.FormatFloat(*threshold.Min, 'g', -1, 64)
		}
		if threshold.Max != nil {
			message += " max=" + strconv.FormatFloat(*threshold.Max, 'g', -1, 64)
		}
		results = append(results, AssessmentGateResult{Metric: name, Passed: passed, Numerator: metric.Numerator, Denominator: metric.Denominator, Actual: metric.Value, Minimum: threshold.Min, Maximum: threshold.Max, Message: message})
	}
	return results, nil
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
