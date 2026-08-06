package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/taskroute"
)

// TaskStartRequest is the compact user-facing entrypoint. Planning and routing
// always happen first; Go controls whether the preview is immediately accepted.
type TaskStartRequest struct {
	Goal     string `json:"goal"`
	Profile  string `json:"profile,omitempty"`
	Go       bool   `json:"go,omitempty"`
	Detached bool   `json:"-"`
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

func (s *Service) StartTask(ctx context.Context, request TaskStartRequest) (*TaskView, error) {
	plan, err := s.Plan(ctx, PlanRequest{Goal: request.Goal, Profile: request.Profile})
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
		NeedsInput: !request.Go,
	}
	if !request.Go {
		return view, nil
	}
	record, err := s.ExecutePlan(ctx, ExecutePlanRequest{PlanID: plan.PlanID, Confirm: true, Detached: request.Detached})
	if err != nil {
		return nil, err
	}
	view.Status = record.Status
	view.RunID = record.CurrentRunID
	view.NeedsInput = record.Status == "waiting" || record.Status == "paused"
	return view, nil
}

func (s *Service) TaskStatus(reference string) (*TaskView, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, fmt.Errorf("task reference is required")
	}
	if strings.HasPrefix(reference, "plan-") {
		plan, err := s.GetPlan(reference)
		if err != nil {
			return nil, err
		}
		view := &TaskView{Reference: reference, Kind: "plan", Status: plan.Record.Status, PlanID: reference, Route: plan.Route, Plan: plan, Preview: concisePlanView(plan)}
		view.RunID = plan.Record.CurrentRunID
		view.NeedsInput = plan.Record.Status == "draft" || plan.Record.Status == "waiting" || plan.Record.Status == "paused"
		return view, nil
	}
	summary, err := s.Summary(reference, true)
	if err != nil {
		return nil, err
	}
	return &TaskView{Reference: reference, Kind: "run", Status: summary.EffectiveStatus, RunID: reference, Run: summary, NeedsInput: summary.Attention.Required}, nil
}

func (s *Service) RespondTask(ctx context.Context, request TaskRespondRequest) (*TaskView, error) {
	reference := strings.TrimSpace(request.Reference)
	action := strings.ToLower(strings.TrimSpace(request.Action))
	if reference == "" || action == "" {
		return nil, fmt.Errorf("reference and action are required")
	}
	if strings.HasPrefix(reference, "plan-") {
		switch action {
		case "go", "continue":
			record, err := s.ExecutePlan(ctx, ExecutePlanRequest{PlanID: reference, Confirm: true, Detached: request.Detached})
			if err != nil {
				return nil, err
			}
			return &TaskView{Reference: reference, Kind: "plan", Status: record.Status, PlanID: reference, RunID: record.CurrentRunID, NeedsInput: record.Status == "waiting" || record.Status == "paused"}, nil
		case "steer", "answer":
			if strings.TrimSpace(request.Message) == "" {
				return nil, fmt.Errorf("message is required for %s", action)
			}
			if _, err := s.Steer(ctx, SteerRequest{PlanID: reference, Message: request.Message}); err != nil {
				return nil, err
			}
			return s.TaskStatus(reference)
		default:
			return nil, fmt.Errorf("action %q is not valid for a plan", action)
		}
	}

	switch action {
	case "pause":
		if _, err := s.Pause(reference); err != nil {
			return nil, err
		}
	case "resume", "continue":
		if _, err := s.ResumePaused(ctx, reference, request.Detached); err != nil {
			return nil, err
		}
	case "retry":
		if _, err := s.Retry(ctx, RetryRequest{RunID: reference, NodeID: request.NodeID, Detached: request.Detached}); err != nil {
			return nil, err
		}
	case "steer":
		if strings.TrimSpace(request.Message) == "" {
			return nil, fmt.Errorf("message is required for steer")
		}
		if _, err := s.Steer(ctx, SteerRequest{RunID: reference, Message: request.Message}); err != nil {
			return nil, err
		}
	case "answer", "go":
		if strings.TrimSpace(request.Message) == "" {
			request.Message = "approved"
		}
		state, err := s.GetRun(reference)
		if err != nil {
			return nil, err
		}
		nodeID := strings.TrimSpace(request.NodeID)
		if nodeID == "" && state.Waiting != nil {
			nodeID = state.Waiting.NodeID
		}
		if nodeID == "" {
			return nil, fmt.Errorf("run %s is not waiting for a user response", reference)
		}
		if _, err := s.Answer(ctx, reference, nodeID, request.Message); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported task action %q", action)
	}
	return s.TaskStatus(reference)
}

func (s *Service) StopTask(request TaskStopRequest) (*TaskView, error) {
	reference := strings.TrimSpace(request.Reference)
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "stopped by user"
	}
	if strings.HasPrefix(reference, "plan-") {
		st := dynamicplan.Store{Workspace: s.Workspace}
		record, err := st.Load(reference)
		if err != nil {
			return nil, err
		}
		if record.CurrentRunID != "" {
			if _, err := s.Abandon(record.CurrentRunID, reason); err != nil {
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
				if err := st.Save(record); err != nil {
					return nil, err
				}
			}
		}
		return s.TaskStatus(reference)
	}
	if _, err := s.Abandon(reference, reason); err != nil {
		return nil, err
	}
	return s.TaskStatus(reference)
}

func (s *Service) ExplainTask(reference string) (*TaskView, error) {
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
