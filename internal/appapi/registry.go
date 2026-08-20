package appapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"takt/internal/application"
	"takt/internal/experimental/dynamicflow"
	"takt/internal/extensions"
	"takt/internal/store"
)

// Registry is the canonical JSON boundary for application operations shared by
// local transports. It owns argument decoding/defaults, never transport I/O.
type Handler func(context.Context, json.RawMessage) (any, error)

type registeredOperation struct {
	descriptor  OperationDescriptor
	requestType reflect.Type
	schema      *jsonschema.Schema
	handler     Handler
}

type Registry struct {
	operations    map[string]registeredOperation
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
	r := &Registry{operations: map[string]registeredOperation{}}
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

func compileInputSchema(descriptor OperationDescriptor) *jsonschema.Schema {
	raw, err := json.Marshal(descriptor.InputSchema)
	if err != nil {
		panic(fmt.Sprintf("encode operation %s input schema: %v", descriptor.ID, err))
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("decode operation %s input schema: %v", descriptor.ID, err))
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	resource := "takt://operation/" + descriptor.ID + ".json"
	if err := compiler.AddResource(resource, doc); err != nil {
		panic(fmt.Sprintf("register operation %s input schema: %v", descriptor.ID, err))
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		panic(fmt.Sprintf("compile operation %s input schema: %v", descriptor.ID, err))
	}
	return compiled
}

func validateOperationInput(schema *jsonschema.Schema, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := schema.Validate(value); err != nil {
		return fmt.Errorf("operation input schema validation failed: %w", err)
	}
	return nil
}

func validateTypedRequestContract(descriptor OperationDescriptor, requestType reflect.Type) error {
	for requestType.Kind() == reflect.Pointer {
		requestType = requestType.Elem()
	}
	if requestType.Kind() != reflect.Struct {
		return fmt.Errorf("operation %s request type must be a struct, got %s", descriptor.ID, requestType)
	}
	properties, ok := descriptor.InputSchema["properties"].(map[string]any)
	if !ok {
		return fmt.Errorf("operation %s input schema properties must be an object", descriptor.ID)
	}
	fields := map[string]bool{}
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = true
		if _, exists := properties[name]; !exists {
			return fmt.Errorf("operation %s typed request exposes JSON field %q missing from descriptor schema", descriptor.ID, name)
		}
	}
	for name := range properties {
		if !fields[name] {
			return fmt.Errorf("operation %s descriptor schema exposes %q missing from typed request", descriptor.ID, name)
		}
	}
	return nil
}

func registerOperation[T any](r *Registry, id string, handle func(context.Context, T) (any, error)) {
	descriptor, ok := Descriptor(id)
	if !ok {
		panic("missing operation descriptor: " + id)
	}
	if _, exists := r.operations[id]; exists {
		panic("duplicate operation handler: " + id)
	}
	requestType := reflect.TypeOf((*T)(nil)).Elem()
	if err := validateTypedRequestContract(descriptor, requestType); err != nil {
		panic(err)
	}
	schema := compileInputSchema(descriptor)
	handler := func(ctx context.Context, raw json.RawMessage) (any, error) {
		if err := validateOperationInput(schema, raw); err != nil {
			return nil, err
		}
		var params T
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return handle(ctx, params)
	}
	r.operations[descriptor.ID] = registeredOperation{
		descriptor:  descriptor,
		requestType: requestType,
		schema:      schema,
		handler:     handler,
	}
}

func (r *Registry) CallMap(ctx context.Context, method string, params map[string]any) (any, error) {
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return r.Call(ctx, method, raw)
}

func (r *Registry) Call(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	operation, ok := r.operations[method]
	if !ok {
		return nil, fmt.Errorf("unknown application operation %q", method)
	}
	return operation.handler(ctx, raw)
}

func (r *Registry) registerTaskOperations() {
	registerOperation(r, "task.start", func(ctx context.Context, params dynamicflow.TaskStartRequest) (any, error) {
		params.Detached = true
		return r.tasks.StartTask(ctx, params)
	})
	registerOperation(r, "task.status", func(ctx context.Context, params struct {
		Reference string `json:"reference"`
	}) (any, error) {
		return r.tasks.TaskStatus(params.Reference)
	})
	registerOperation(r, "task.respond", func(ctx context.Context, params dynamicflow.TaskRespondRequest) (any, error) {
		params.Detached = true
		return r.tasks.RespondTask(ctx, params)
	})
	registerOperation(r, "task.stop", func(ctx context.Context, params dynamicflow.TaskStopRequest) (any, error) {
		return r.tasks.StopTask(ctx, params)
	})
	registerOperation(r, "task.explain", func(ctx context.Context, params struct {
		Reference string `json:"reference"`
	}) (any, error) {
		return r.tasks.ExplainTask(params.Reference)
	})
}

func (r *Registry) registerCatalogOperations() {
	registerOperation(r, "workflow.list", func(ctx context.Context, params struct {
		Profile string `json:"profile"`
	}) (any, error) {
		return r.catalog.ListWorkflows(params.Profile)
	})
	registerOperation(r, "workflow.describe", func(ctx context.Context, params struct {
		Selector string `json:"selector"`
	}) (any, error) {
		return r.catalog.DescribeWorkflow(params.Selector)
	})
	registerOperation(r, "block.list", func(ctx context.Context, params struct {
		Profile string `json:"profile,omitempty"`
	}) (any, error) {
		return r.blocks.List(params.Profile)
	})
	registerOperation(r, "block.describe", func(ctx context.Context, params struct {
		Profile string `json:"profile,omitempty"`
		Name    string `json:"name"`
	}) (any, error) {
		return r.blocks.Describe(params.Profile, params.Name)
	})
}

func (r *Registry) registerHostOperations() {
	registerOperation(r, "host.begin", func(ctx context.Context, params dynamicflow.HostBeginRequest) (any, error) {
		return r.hosts.BeginHostSession(ctx, params)
	})
	registerOperation(r, "host.confirm", func(ctx context.Context, params dynamicflow.HostConfirmRequest) (any, error) {
		params.Detached = true
		return r.hosts.ConfirmHostSession(ctx, params)
	})
	registerOperation(r, "host.get", func(ctx context.Context, params struct {
		SessionID string `json:"session_id"`
	}) (any, error) {
		return r.hosts.GetHostSession(params.SessionID)
	})
	registerOperation(r, "host.find", func(ctx context.Context, params struct {
		Host          string `json:"host"`
		HostSessionID string `json:"host_session_id"`
	}) (any, error) {
		return r.hosts.FindHostSession(params.Host, params.HostSessionID)
	})
	registerOperation(r, "host.guard_tool", func(ctx context.Context, params dynamicflow.HostToolGuardRequest) (any, error) {
		return r.hosts.GuardHostTool(params)
	})
	registerOperation(r, "host.guard_completion", func(ctx context.Context, params dynamicflow.HostCompletionGuardRequest) (any, error) {
		return r.hosts.GuardHostCompletion(params)
	})
	registerOperation(r, "host.release", func(ctx context.Context, params struct {
		SessionID string `json:"session_id"`
	}) (any, error) {
		return r.hosts.ReleaseHostSession(params.SessionID)
	})
}

func (r *Registry) registerPlanOperations() {
	registerOperation(r, "plan.create", func(ctx context.Context, params dynamicflow.PlanRequest) (any, error) {
		return r.plans.Plan(ctx, params)
	})
	registerOperation(r, "plan.get", func(ctx context.Context, params struct {
		PlanID string `json:"plan_id"`
	}) (any, error) {
		return r.plans.GetPlan(params.PlanID)
	})
	registerOperation(r, "plan.execute", func(ctx context.Context, params dynamicflow.ExecutePlanRequest) (any, error) {
		params.Detached = true
		return r.plans.ExecutePlan(ctx, params)
	})
	registerOperation(r, "plan.steer", func(ctx context.Context, params dynamicflow.SteerRequest) (any, error) {
		return r.plans.Steer(ctx, params)
	})
	registerOperation(r, "plan.promote", func(ctx context.Context, params struct {
		PlanID string `json:"plan_id"`
		Name   string `json:"name"`
		Force  bool   `json:"force,omitempty"`
	}) (any, error) {
		return r.plans.PromotePlanWithOptions(ctx, params.PlanID, params.Name, dynamicflow.PromotePlanOptions{Force: params.Force})
	})
}

func (r *Registry) registerRunOperations() {
	registerOperation(r, "run.start", func(ctx context.Context, params struct {
		Selector       string            `json:"selector"`
		Input          string            `json:"input,omitempty"`
		ConfigPath     string            `json:"config_path,omitempty"`
		ModelPreset    string            `json:"model_preset,omitempty"`
		ModelOverrides map[string]string `json:"model_overrides,omitempty"`
		Worktree       *bool             `json:"worktree,omitempty"`
		WorktreeBase   string            `json:"worktree_base,omitempty"`
		KeepWorktree   bool              `json:"keep_worktree,omitempty"`
		AllowDirty     bool              `json:"allow_dirty_worktree,omitempty"`
		Detached       *bool             `json:"detached,omitempty"`
	}) (any, error) {
		detached := true
		if params.Detached != nil {
			detached = *params.Detached
		}
		return r.runs.Start(ctx, application.StartRequest{Selector: params.Selector, Input: params.Input, ConfigPath: params.ConfigPath, ModelPreset: params.ModelPreset, ModelOverrides: params.ModelOverrides, Worktree: params.Worktree, WorktreeBase: params.WorktreeBase, KeepWorktree: params.KeepWorktree, AllowDirty: params.AllowDirty, Detached: detached})
	})
	registerOperation(r, "run.get", func(ctx context.Context, params struct {
		RunID string `json:"run_id"`
	}) (any, error) {
		return r.runs.GetRun(params.RunID)
	})
	registerOperation(r, "run.list", func(ctx context.Context, params application.RunListRequest) (any, error) {
		return r.runs.ListRuns(params)
	})
	registerOperation(r, "run.attention", func(ctx context.Context, params struct{}) (any, error) {
		return r.runs.Attention()
	})
	registerOperation(r, "run.summary", func(ctx context.Context, params struct {
		RunID     string `json:"run_id"`
		Recursive bool   `json:"recursive,omitempty"`
	}) (any, error) {
		return r.runs.Summary(params.RunID, params.Recursive)
	})
	registerOperation(r, "run.pause", func(ctx context.Context, params struct {
		RunID string `json:"run_id"`
	}) (any, error) {
		return r.runs.Pause(ctx, params.RunID)
	})
	registerOperation(r, "run.resume_paused", func(ctx context.Context, params struct {
		RunID string `json:"run_id"`
	}) (any, error) {
		return r.runs.ResumePaused(ctx, params.RunID, true)
	})
	registerOperation(r, "run.retry", func(ctx context.Context, params application.RetryRequest) (any, error) {
		params.Detached = true
		return r.runs.Retry(ctx, params)
	})
	registerOperation(r, "run.fork", func(ctx context.Context, params dynamicflow.ForkRequest) (any, error) {
		params.Detached = true
		return r.forks.Fork(ctx, params)
	})
	registerOperation(r, "run.abandon", func(ctx context.Context, params struct {
		RunID  string `json:"run_id"`
		Reason string `json:"reason,omitempty"`
	}) (any, error) {
		return r.runs.Abandon(ctx, params.RunID, params.Reason)
	})
	registerOperation(r, "run.recover", func(ctx context.Context, params struct{}) (any, error) {
		return r.runs.RecoverInterruptedRuns(ctx)
	})
	registerOperation(r, "run.resume", func(ctx context.Context, params struct {
		RunID string `json:"run_id"`
	}) (any, error) {
		return r.runs.Resume(ctx, params.RunID)
	})
	registerOperation(r, "run.answer", func(ctx context.Context, params struct {
		RunID  string `json:"run_id"`
		NodeID string `json:"node_id"`
		Value  string `json:"value"`
	}) (any, error) {
		return r.runs.Answer(ctx, params.RunID, params.NodeID, params.Value)
	})
	registerOperation(r, "run.cancel", func(ctx context.Context, params struct {
		RunID  string `json:"run_id"`
		Reason string `json:"reason"`
	}) (any, error) {
		return r.runs.Cancel(ctx, params.RunID, params.Reason)
	})
	registerOperation(r, "run.children", func(ctx context.Context, params struct {
		RunID string `json:"run_id"`
	}) (any, error) {
		return r.runs.Children(params.RunID)
	})
	registerOperation(r, "run.artifacts", func(ctx context.Context, params struct {
		RunID          string `json:"run_id"`
		NodeID         string `json:"node_id,omitempty"`
		Type           string `json:"type,omitempty"`
		Recursive      bool   `json:"recursive,omitempty"`
		IncludeContent bool   `json:"include_content,omitempty"`
		MaxBytes       int    `json:"max_bytes,omitempty"`
	}) (any, error) {
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
	})
	registerOperation(r, "run.assessment", func(ctx context.Context, params application.AssessmentQuery) (any, error) {
		return r.runs.Assessments(params)
	})
	registerOperation(r, "run.events", func(ctx context.Context, params struct {
		RunID         string `json:"run_id"`
		AfterRevision uint64 `json:"after_revision"`
		Limit         int    `json:"limit"`
		WaitMS        int    `json:"wait_ms"`
	}) (any, error) {
		return r.runs.Events(ctx, params.RunID, params.AfterRevision, params.Limit, time.Duration(params.WaitMS)*time.Millisecond)
	})
}

func (r *Registry) registerNotificationOperations() {
	registerOperation(r, "notify.list", func(ctx context.Context, params struct {
		UnreadOnly bool `json:"unread_only,omitempty"`
		Limit      int  `json:"limit,omitempty"`
	}) (any, error) {
		return r.notifications.List(params.UnreadOnly, params.Limit)
	})
	registerOperation(r, "notify.ack", func(ctx context.Context, params struct {
		ID string `json:"id"`
	}) (any, error) {
		return r.notifications.Ack(params.ID)
	})
	registerOperation(r, "notify.test", func(ctx context.Context, params struct {
		Message string `json:"message,omitempty"`
	}) (any, error) {
		return r.notifications.Test(params.Message)
	})
	registerOperation(r, "notify.dispatch", func(ctx context.Context, params struct{}) (any, error) {
		return r.notifications.Dispatch()
	})
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
