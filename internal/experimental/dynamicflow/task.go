package dynamicflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	cfgpkg "takt/internal/config"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/experimental/taskroute"
	sourceresolver "takt/internal/tasksource"
	tasksource "takt/sdk/tasksource"
)

// TaskStartRequest is the compact user-facing entrypoint. Planning and routing
// always happen first; Go controls whether the preview is immediately accepted.
type TaskStartRequest struct {
	Goal      string `json:"goal,omitempty"`
	Source    string `json:"source,omitempty"`
	SourceRef string `json:"source_ref,omitempty"`
	Profile   string `json:"profile,omitempty"`
	Go        bool   `json:"go,omitempty"`
	Detached  bool   `json:"-"`
}

type TaskView struct {
	Reference  string              `json:"reference"`
	Kind       string              `json:"kind"`
	Status     string              `json:"status"`
	NeedsInput bool                `json:"needs_input,omitempty"`
	Preview    string              `json:"preview,omitempty"`
	PlanID     string              `json:"plan_id,omitempty"`
	RunID      string              `json:"run_id,omitempty"`
	Route      *taskroute.Decision `json:"route,omitempty"`
	TaskSource *tasksource.Task    `json:"task_source,omitempty"`
	Plan       *PlanView           `json:"plan,omitempty"`
	Run        *RunSummary         `json:"run,omitempty"`
}

type TaskRespondRequest struct {
	Reference string `json:"reference"`
	Action    string `json:"action"`
	Message   string `json:"message,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	Detached  bool   `json:"-"`
}

type TaskStopRequest struct {
	Reference string `json:"reference"`
	Reason    string `json:"reason,omitempty"`
}

func (s *TaskService) StartTask(ctx context.Context, request TaskStartRequest) (*TaskView, error) {
	goal, source, err := s.resolveTaskStart(ctx, request)
	if err != nil {
		return nil, err
	}
	plan, err := s.plans.Plan(ctx, PlanRequest{Goal: goal, Profile: request.Profile, TaskSource: source})
	if err != nil {
		return nil, err
	}
	view := &TaskView{
		Reference:  plan.PlanID,
		Kind:       "plan",
		Status:     plan.Record.Status,
		Preview:    conciseTaskPreview(plan),
		PlanID:     plan.PlanID,
		Route:      plan.Route,
		TaskSource: plan.Record.TaskSource,
		NeedsInput: !request.Go,
	}
	if !request.Go {
		return view, nil
	}
	record, err := s.plans.ExecutePlan(ctx, ExecutePlanRequest{PlanID: plan.PlanID, Confirm: true, Detached: request.Detached})
	if err != nil {
		return nil, err
	}
	view.Status = record.Status
	view.RunID = record.CurrentRunID
	view.NeedsInput = record.Status == "waiting" || record.Status == "paused" || record.Status == "parked"
	return view, nil
}

func (s *TaskService) TaskStatus(reference string) (*TaskView, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("task reference is required")
	}
	if strings.HasPrefix(reference, "plan-") {
		plan, err := s.plans.GetPlan(reference)
		if err != nil {
			return nil, err
		}
		view := &TaskView{Reference: reference, Kind: "plan", Status: plan.Record.Status, PlanID: reference, Route: plan.Route, TaskSource: plan.Record.TaskSource, Plan: plan, Preview: concisePlanView(plan)}
		view.RunID = plan.Record.CurrentRunID
		view.NeedsInput = plan.Record.Status == "draft" || plan.Record.Status == "waiting" || plan.Record.Status == "paused" || plan.Record.Status == "parked"
		return view, nil
	}
	summary, err := s.runs.Summary(reference, true)
	if err != nil {
		return nil, err
	}
	return &TaskView{Reference: reference, Kind: "run", Status: summary.EffectiveStatus, RunID: reference, Run: summary, NeedsInput: summary.Attention.Required}, nil
}

func (s *TaskService) resolveTaskStart(ctx context.Context, request TaskStartRequest) (string, *tasksource.Task, error) {
	goal := strings.TrimSpace(request.Goal)
	sourceName := strings.TrimSpace(request.Source)
	ref := strings.TrimSpace(request.SourceRef)
	if sourceName == "" {
		if ref != "" {
			return "", nil, fmt.Errorf("source_ref requires source")
		}
		if goal == "" {
			return "", nil, fmt.Errorf("goal or source is required")
		}
		return goal, nil, nil
	}
	if goal != "" {
		return "", nil, fmt.Errorf("goal and source are mutually exclusive")
	}
	if ref == "" {
		return "", nil, fmt.Errorf("source_ref is required with source")
	}
	cfg, err := cfgpkg.Load(s.configPath)
	if err != nil {
		return "", nil, fmt.Errorf("load task source config: %w", err)
	}
	specification, ok := cfg.TaskSources[sourceName]
	if !ok {
		return "", nil, fmt.Errorf("task source %q is not configured", sourceName)
	}
	task, err := (sourceresolver.Resolver{Name: sourceName, Spec: specification}).Resolve(ctx, ref, s.workspace)
	if err != nil {
		return "", nil, err
	}
	return tasksource.GoalText(*task), task, nil
}

func (s *TaskService) RespondTask(ctx context.Context, request TaskRespondRequest) (*TaskView, error) {
	reference := strings.TrimSpace(request.Reference)
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if reference == "" || action == "" {
		return nil, fmt.Errorf("reference and action are required")
	}
	request.Reference = reference
	request.Action = action
	if strings.HasPrefix(reference, "plan-") {
		return s.respondPlanTask(ctx, request)
	}
	return s.respondRunTask(ctx, request)
}

func (s *TaskService) respondPlanTask(ctx context.Context, request TaskRespondRequest) (*TaskView, error) {
	reference := request.Reference
	switch request.Action {
	case "go", "continue":
		current, err := s.planStore.Load(reference)
		if err != nil {
			return nil, err
		}
		if current.Status == "parked" {
			return nil, fmt.Errorf("plan %s is parked: provide steering with a safe alternative or stop the task", reference)
		}
		record, err := s.plans.ExecutePlan(ctx, ExecutePlanRequest{PlanID: reference, Confirm: true, Detached: request.Detached})
		if err != nil {
			return nil, err
		}
		return taskViewForPlan(record), nil
	case "steer":
		if err := requireTaskMessage(request); err != nil {
			return nil, err
		}
		if _, err := s.plans.Steer(ctx, SteerRequest{PlanID: reference, Message: request.Message}); err != nil {
			return nil, err
		}
		return s.TaskStatus(reference)
	case "answer":
		return s.answerPlanTask(ctx, request)
	default:
		return nil, fmt.Errorf("action %q is not valid for a plan", request.Action)
	}
}

func (s *TaskService) answerPlanTask(ctx context.Context, request TaskRespondRequest) (*TaskView, error) {
	if err := requireTaskMessage(request); err != nil {
		return nil, err
	}
	record, err := s.planStore.Load(request.Reference)
	if err != nil {
		return nil, err
	}
	if record.CurrentRunID != "" {
		run, err := s.runs.GetRun(record.CurrentRunID)
		if err != nil {
			return nil, err
		}
		if run.Waiting != nil || run.Status == "paused" {
			nodeID := strings.TrimSpace(request.NodeID)
			if nodeID == "" && run.Waiting != nil {
				nodeID = run.Waiting.NodeID
			}
			if nodeID == "" {
				return nil, fmt.Errorf("run %s is not waiting for a user response", record.CurrentRunID)
			}
			if _, err := s.runs.Answer(ctx, record.CurrentRunID, nodeID, request.Message); err != nil {
				return nil, err
			}
			// Foreground execution has no daemon monitor, so reconcile the plan
			// explicitly after its waiting child Run continues.
			if err := s.plans.AdvanceDynamicPlans(ctx); err != nil {
				return nil, err
			}
			return s.TaskStatus(request.Reference)
		}
	}
	// A plan-level ask_user checkpoint has no waiting Run; the answer is
	// steering input for the replanner rather than a Run-level response.
	if _, err := s.plans.Steer(ctx, SteerRequest{PlanID: request.Reference, Message: request.Message}); err != nil {
		return nil, err
	}
	return s.TaskStatus(request.Reference)
}

func (s *TaskService) respondRunTask(ctx context.Context, request TaskRespondRequest) (*TaskView, error) {
	switch request.Action {
	case "pause":
		if _, err := s.runs.Pause(ctx, request.Reference); err != nil {
			return nil, err
		}
	case "resume", "continue":
		if _, err := s.runs.ResumePaused(ctx, request.Reference, request.Detached); err != nil {
			return nil, err
		}
	case "retry":
		if _, err := s.runs.Retry(ctx, RetryRequest{RunID: request.Reference, NodeID: request.NodeID, Detached: request.Detached}); err != nil {
			return nil, err
		}
	case "steer":
		if err := requireTaskMessage(request); err != nil {
			return nil, err
		}
		if _, err := s.plans.Steer(ctx, SteerRequest{RunID: request.Reference, Message: request.Message}); err != nil {
			return nil, err
		}
	case "answer", "go":
		if err := s.answerRunTask(ctx, request); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported task action %q", request.Action)
	}
	return s.TaskStatus(request.Reference)
}

func (s *TaskService) answerRunTask(ctx context.Context, request TaskRespondRequest) error {
	if err := requireTaskMessage(request); err != nil {
		return err
	}
	state, err := s.runs.GetRun(request.Reference)
	if err != nil {
		return err
	}
	nodeID := strings.TrimSpace(request.NodeID)
	if nodeID == "" && state.Waiting != nil {
		nodeID = state.Waiting.NodeID
	}
	if nodeID == "" {
		return fmt.Errorf("run %s is not waiting for a user response", request.Reference)
	}
	_, err = s.runs.Answer(ctx, request.Reference, nodeID, request.Message)
	return err
}

func requireTaskMessage(request TaskRespondRequest) error {
	if strings.TrimSpace(request.Message) == "" {
		return fmt.Errorf("message is required for %s", request.Action)
	}
	return nil
}

func taskViewForPlan(record *dynamicplan.Record) *TaskView {
	return &TaskView{
		Reference: record.ID, Kind: "plan", Status: record.Status, PlanID: record.ID,
		RunID: record.CurrentRunID, NeedsInput: record.Status == "waiting" || record.Status == "paused" || record.Status == "parked",
	}
}

func (s *TaskService) StopTask(ctx context.Context, request TaskStopRequest) (*TaskView, error) {
	reference := strings.TrimSpace(request.Reference)
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "stopped by user"
	}
	if strings.HasPrefix(reference, "plan-") {
		st := s.planStore
		record, err := st.Load(reference)
		if err != nil {
			return nil, err
		}
		if record.CurrentRunID != "" {
			if _, err := s.runs.Abandon(ctx, record.CurrentRunID, reason); err != nil {
				return nil, err
			}
			// Reconcile explicitly. This path must also work without a daemon
			// monitor and must not leave the compact task reference permanently
			// reporting "running" after the underlying Run was abandoned.
			record, err = st.Load(reference)
			if err != nil {
				return nil, err
			}
			record.Status = "abandoned"
			record.LastError = reason
			record.UpdatedAt = time.Now().UTC()
			if err := savePlanRecord(s.configPath, s.planStore, record); err != nil {
				return nil, err
			}
		} else {
			switch record.Status {
			case "completed", "failed", "abandoned", "cancelled":
				return nil, fmt.Errorf("cannot stop terminal plan %s with status %s", reference, record.Status)
			default:
				record.Status = "abandoned"
				record.LastError = reason
				record.UpdatedAt = time.Now().UTC()
				if err := savePlanRecord(s.configPath, s.planStore, record); err != nil {
					return nil, err
				}
			}
		}
		return s.TaskStatus(reference)
	}
	if _, err := s.runs.Abandon(ctx, reference, reason); err != nil {
		return nil, err
	}
	return s.TaskStatus(reference)
}

func (s *TaskService) ExplainTask(reference string) (*TaskView, error) {
	return s.TaskStatus(reference)
}

func conciseTaskPreview(result *PlanResult) string {
	if result == nil {
		return ""
	}
	lines := []string{}
	if result.Route != nil {
		switch result.Route.Route {
		case taskroute.RouteWorkflow:
			lines = append(lines, "Route: existing workflow "+result.Route.Workflow)
		case taskroute.RouteTemplate:
			lines = append(lines, "Route: simple reliable development")
		case taskroute.RouteDynamic:
			lines = append(lines, "Route: task-specific dynamic workflow")
		}
		lines = append(lines, "Reason: "+result.Route.Reason)
	}
	if strings.TrimSpace(result.Preview) != "" {
		lines = append(lines, "", strings.TrimSpace(result.Preview))
	}
	return strings.Join(lines, "\n")
}

func concisePlanView(view *PlanView) string {
	if view == nil {
		return ""
	}
	result := &PlanResult{Preview: view.Preview, Route: view.Route}
	return conciseTaskPreview(result)
}
