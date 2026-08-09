package appapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"takt/internal/application"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/extensions"
	"takt/internal/store"
)

// Registry is the canonical JSON boundary for application operations shared by
// local transports. It owns argument decoding/defaults, never transport I/O.
type Handler func(context.Context, json.RawMessage) (any, error)

type Registry struct {
	handlers      map[string]Handler
	tasks         *dynamicflow.TaskService
	catalog       *application.CatalogService
	blocks        *extensions.BlockService
	hosts         *dynamicflow.HostService
	plans         *dynamicflow.PlanService
	runs          *application.RunService
	forks         *dynamicflow.ForkService
	notifications *extensions.NotificationService
}

type Dependencies struct {
	Core          *application.Services
	Dynamic       *dynamicflow.Services
	Blocks        *extensions.BlockService
	Notifications *extensions.NotificationService
}

func New(deps Dependencies) *Registry {
	r := &Registry{handlers: map[string]Handler{}}
	if deps.Core != nil {
		r.catalog = deps.Core.CatalogService
		r.runs = deps.Core.RunService
	}
	if deps.Dynamic != nil {
		r.tasks = deps.Dynamic.TaskService
		r.hosts = deps.Dynamic.HostService
		r.plans = deps.Dynamic.PlanService
		r.forks = deps.Dynamic.ForkService
	}
	r.blocks = deps.Blocks
	r.notifications = deps.Notifications
	r.registerTaskOperations()
	r.registerCatalogOperations()
	r.registerHostOperations()
	r.registerPlanOperations()
	r.registerRunOperations()
	r.registerNotificationOperations()
	return r
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (r *Registry) CallMap(ctx context.Context, method string, params map[string]any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return r.Call(ctx, method, raw)
}

func (r *Registry) Call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	handler, ok := r.handlers[method]
	if !ok {
		return nil, fmt.Errorf("unknown application operation %q", method)
	}
	return handler(ctx, raw)
}

func (r *Registry) registerTaskOperations() {
	r.handlers["task.start"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.TaskStartRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.tasks.StartTask(ctx, params)
	}
	r.handlers["task.status"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Reference string `json:"reference"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.tasks.TaskStatus(params.Reference)
	}
	r.handlers["task.respond"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.TaskRespondRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.tasks.RespondTask(ctx, params)
	}
	r.handlers["task.stop"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.TaskStopRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.tasks.StopTask(ctx, params)
	}
	r.handlers["task.explain"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Reference string `json:"reference"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.tasks.ExplainTask(params.Reference)
	}
}

func (r *Registry) registerCatalogOperations() {
	r.handlers["workflow.list"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Profile string `json:"profile"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.catalog.ListWorkflows(params.Profile)
	}
	r.handlers["workflow.describe"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Selector string `json:"selector"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.catalog.DescribeWorkflow(params.Selector)
	}
	r.handlers["block.list"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Profile string `json:"profile,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.blocks.List(params.Profile)
	}
	r.handlers["block.describe"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Profile string `json:"profile,omitempty"`
			Name    string `json:"name"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.blocks.Describe(params.Profile, params.Name)
	}
}

func (r *Registry) registerHostOperations() {
	r.handlers["host.begin"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.HostBeginRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.BeginHostSession(ctx, params)
	}
	r.handlers["host.confirm"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.HostConfirmRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.hosts.ConfirmHostSession(ctx, params)
	}
	r.handlers["host.get"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			SessionID string `json:"session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.GetHostSession(params.SessionID)
	}
	r.handlers["host.find"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Host          string `json:"host"`
			HostSessionID string `json:"host_session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.FindHostSession(params.Host, params.HostSessionID)
	}
	r.handlers["host.guard_tool"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.HostToolGuardRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.GuardHostTool(params)
	}
	r.handlers["host.guard_completion"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.HostCompletionGuardRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.GuardHostCompletion(params)
	}
	r.handlers["host.release"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			SessionID string `json:"session_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.hosts.ReleaseHostSession(params.SessionID)
	}
}

func (r *Registry) registerPlanOperations() {
	r.handlers["plan.create"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.PlanRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.plans.Plan(ctx, params)
	}
	r.handlers["plan.get"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			PlanID string `json:"plan_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.plans.GetPlan(params.PlanID)
	}
	r.handlers["plan.execute"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.ExecutePlanRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.plans.ExecutePlan(ctx, params)
	}
	r.handlers["plan.steer"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.SteerRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.plans.Steer(ctx, params)
	}
	r.handlers["plan.promote"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			PlanID string `json:"plan_id"`
			Name   string `json:"name"`
			Force  bool   `json:"force,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.plans.PromotePlanWithOptions(ctx, params.PlanID, params.Name, dynamicflow.PromotePlanOptions{Force: params.Force})
	}
}

func (r *Registry) registerRunOperations() {
	r.handlers["run.start"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Selector     string `json:"selector"`
			Input        string `json:"input,omitempty"`
			ConfigPath   string `json:"config_path,omitempty"`
			Worktree     *bool  `json:"worktree,omitempty"`
			WorktreeBase string `json:"worktree_base,omitempty"`
			KeepWorktree bool   `json:"keep_worktree,omitempty"`
			AllowDirty   bool   `json:"allow_dirty_worktree,omitempty"`
			Detached     *bool  `json:"detached,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		detached := true
		if params.Detached != nil {
			detached = *params.Detached
		}
		return r.runs.Start(ctx, application.StartRequest{Selector: params.Selector, Input: params.Input, ConfigPath: params.ConfigPath, Worktree: params.Worktree, WorktreeBase: params.WorktreeBase, KeepWorktree: params.KeepWorktree, AllowDirty: params.AllowDirty, Detached: detached})
	}
	r.handlers["run.get"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.GetRun(params.RunID)
	}
	r.handlers["run.list"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params application.RunListRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.ListRuns(params)
	}
	r.handlers["run.attention"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		return r.runs.Attention()
	}
	r.handlers["run.summary"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID     string `json:"run_id"`
			Recursive bool   `json:"recursive,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Summary(params.RunID, params.Recursive)
	}
	r.handlers["run.pause"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Pause(ctx, params.RunID)
	}
	r.handlers["run.resume_paused"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.ResumePaused(ctx, params.RunID, true)
	}
	r.handlers["run.retry"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params application.RetryRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.runs.Retry(ctx, params)
	}
	r.handlers["run.fork"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params dynamicflow.ForkRequest
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		params.Detached = true
		return r.forks.Fork(ctx, params)
	}
	r.handlers["run.abandon"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID  string `json:"run_id"`
			Reason string `json:"reason,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Abandon(ctx, params.RunID, params.Reason)
	}
	r.handlers["run.recover"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		return r.runs.RecoverInterruptedRuns(ctx)
	}
	r.handlers["run.resume"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Resume(ctx, params.RunID)
	}
	r.handlers["run.answer"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID  string `json:"run_id"`
			NodeID string `json:"node_id"`
			Value  string `json:"value"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Answer(ctx, params.RunID, params.NodeID, params.Value)
	}
	r.handlers["run.cancel"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID  string `json:"run_id"`
			Reason string `json:"reason"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Cancel(ctx, params.RunID, params.Reason)
	}
	r.handlers["run.children"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID string `json:"run_id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Children(params.RunID)
	}
	r.handlers["run.artifacts"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID          string `json:"run_id"`
			NodeID         string `json:"node_id,omitempty"`
			Type           string `json:"type,omitempty"`
			Recursive      bool   `json:"recursive,omitempty"`
			IncludeContent bool   `json:"include_content,omitempty"`
			MaxBytes       int    `json:"max_bytes,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		result, err := r.runs.Artifacts(params.RunID, application.ArtifactQuery{NodeID: params.NodeID, Type: params.Type, Recursive: params.Recursive})
		if err != nil {
			return nil, err
		}
		if params.IncludeContent {
			if err := attachArtifactContent(result, params.MaxBytes); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	r.handlers["run.events"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			RunID         string `json:"run_id"`
			AfterRevision uint64 `json:"after_revision"`
			Limit         int    `json:"limit"`
			WaitMS        int    `json:"wait_ms"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.runs.Events(ctx, params.RunID, params.AfterRevision, params.Limit, time.Duration(params.WaitMS)*time.Millisecond)
	}
}

func (r *Registry) registerNotificationOperations() {
	r.handlers["notify.list"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			UnreadOnly bool `json:"unread_only,omitempty"`
			Limit      int  `json:"limit,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.notifications.List(params.UnreadOnly, params.Limit)
	}
	r.handlers["notify.ack"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.notifications.Ack(params.ID)
	}
	r.handlers["notify.test"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params struct {
			Message string `json:"message,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return r.notifications.Test(params.Message)
	}
	r.handlers["notify.dispatch"] = func(ctx context.Context, raw json.RawMessage) (any, error) {
		return r.notifications.Dispatch()
	}
}

func attachArtifactContent(result map[string]any, maxBytes int) error {
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	if maxBytes > 1024*1024 {
		return fmt.Errorf("max_bytes must not exceed 1048576")
	}
	artifacts, ok := result["artifacts"].([]store.ArtifactRef)
	if !ok {
		return fmt.Errorf("unexpected artifact result")
	}
	values := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		raw, err := os.ReadFile(artifact.Path)
		if err != nil {
			return fmt.Errorf("read artifact %s: %w", artifact.ID, err)
		}
		truncated := len(raw) > maxBytes
		if truncated {
			raw = raw[:maxBytes]
		}
		encoded, _ := json.Marshal(artifact)
		var value map[string]any
		_ = json.Unmarshal(encoded, &value)
		if isTextMIME(artifact.MIME) {
			value["content"] = string(raw)
			value["content_encoding"] = "utf-8"
		} else {
			value["content"] = base64.StdEncoding.EncodeToString(raw)
			value["content_encoding"] = "base64"
		}
		if truncated {
			value["content_truncated"] = true
		}
		values = append(values, value)
	}
	result["artifacts"] = values
	return nil
}

func isTextMIME(value string) bool {
	value = strings.ToLower(value)
	return strings.HasPrefix(value, "text/") || strings.Contains(value, "json") || strings.Contains(value, "yaml") || strings.Contains(value, "xml") || value == ""
}
