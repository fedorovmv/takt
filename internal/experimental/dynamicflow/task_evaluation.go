package dynamicflow

import (
	"context"
	"encoding/json"

	"takt/internal/application"
	"takt/internal/experimental/dynamicplan"
)

// TaskEvaluationSnapshot is the application-level observation used by the
// task benchmark. The evaluation package depends on this behavior through an
// injected runner rather than constructing Takt's application graph itself.
type TaskEvaluationSnapshot struct {
	PlanID         string
	RunID          string
	Status         string
	Route          string
	Template       string
	Workflow       string
	PlanRevisions  int
	ReplannerRuns  int
	ExecutionRuns  int
	RouterFallback bool
	InputTokens    int
	OutputTokens   int
	Cost           float64
}

func (s *Services) EvaluateTaskCase(ctx context.Context, goal, profileName string) (TaskEvaluationSnapshot, error) {
	var result TaskEvaluationSnapshot
	planned, planErr := s.PlanService.Plan(ctx, PlanRequest{Goal: goal, Profile: profileName})
	if planned != nil {
		result.PlanID = planned.PlanID
		result.Status = planned.Record.Status
		if planned.Route != nil {
			result.Route = planned.Route.Route
			result.Template = planned.Route.Template
			result.Workflow = planned.Route.Workflow
		}
	}
	if planErr != nil {
		return s.observeTaskEvaluation(result), planErr
	}

	executed, executeErr := s.PlanService.ExecutePlan(ctx, ExecutePlanRequest{PlanID: result.PlanID, Confirm: true})
	if executed != nil {
		result.Status = executed.Status
		result.RunID = executed.CurrentRunID
	}
	return s.observeTaskEvaluation(result), executeErr
}

func (s *Services) observeTaskEvaluation(result TaskEvaluationSnapshot) TaskEvaluationSnapshot {
	if result.PlanID == "" {
		return result
	}
	plan, err := s.PlanService.store.Load(result.PlanID)
	if err != nil {
		return result
	}
	result.Status = plan.Status
	result.RunID = plan.CurrentRunID
	result.PlanRevisions = len(plan.Revisions)
	result.ReplannerRuns = len(plan.ReplannerRunIDs)
	result.ExecutionRuns = len(plan.ExecutionRunIDs)
	result.RouterFallback = plan.RouterError != "" || routeHasSignal(plan.Route, "router_fallback")
	accumulatePlanUsage(s.RunService, plan, &result)
	return result
}

func routeHasSignal(raw json.RawMessage, signal string) bool {
	var value struct {
		Signals []string `json:"signals"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	for _, item := range value.Signals {
		if item == signal {
			return true
		}
	}
	return false
}

func accumulatePlanUsage(runs *application.RunService, plan *dynamicplan.Record, result *TaskEvaluationSnapshot) {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(runID string) {
		if runID == "" || seen[runID] {
			return
		}
		seen[runID] = true
		state, err := runs.GetRun(runID)
		if err != nil {
			return
		}
		if state.Usage != nil {
			result.InputTokens += state.Usage.InputTokens
			result.OutputTokens += state.Usage.OutputTokens
			result.Cost += state.Usage.Cost
		}
		for _, child := range state.ChildRunIDs {
			visit(child)
		}
	}
	visit(plan.RouterRunID)
	visit(plan.PlannerRunID)
	for _, id := range plan.ReplannerRunIDs {
		visit(id)
	}
	for _, id := range plan.ExecutionRunIDs {
		visit(id)
	}
}
