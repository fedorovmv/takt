package compatibility

import (
	"reflect"
	"sort"
	"strings"

	"takt/internal/extensions/blockcatalog"
	"takt/internal/spec"
)

type FieldMatrix struct {
	APIVersion  string          `json:"apiVersion"`
	Kind        string          `json:"kind"`
	TaktVersion string          `json:"takt_version"`
	Scope       string          `json:"scope"`
	Fields      []FieldDecision `json:"fields"`
}

type FieldDecision struct {
	Contract     string `json:"contract"`
	Field        string `json:"field"`
	Support      string `json:"support"`
	Decision     string `json:"decision"`
	V1Beta1Field string `json:"v1beta1_field,omitempty"`
	Migration    string `json:"migration,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

type auditedContract struct {
	Name            string
	Type            reflect.Type
	DefaultSupport  string
	DefaultDecision string
	Expected        []string
}

func CurrentFieldMatrix() FieldMatrix {
	contracts := auditedContracts()
	var fields []FieldDecision
	for _, contract := range contracts {
		actual := jsonFields(contract.Type)
		for _, field := range actual {
			decision := FieldDecision{Contract: contract.Name, Field: field, Support: contract.DefaultSupport, Decision: contract.DefaultDecision, V1Beta1Field: field}
			applyFieldOverride(&decision)
			fields = append(fields, decision)
		}
	}
	sort.Slice(fields, func(i, j int) bool {
		if fields[i].Contract == fields[j].Contract {
			return fields[i].Field < fields[j].Field
		}
		return fields[i].Contract < fields[j].Contract
	})
	return FieldMatrix{
		APIVersion:  MatrixVersion,
		Kind:        "V1Beta1FieldMatrix",
		TaktVersion: CurrentMatrix().TaktVersion,
		Scope:       "stable-candidate authoring/config contracts plus explicitly deferred external seams",
		Fields:      fields,
	}
}

func auditedContracts() []auditedContract {
	return []auditedContract{
		{Name: "Workflow", Type: reflect.TypeOf(spec.Workflow{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"name", "description", "labels", "provider", "model", "nodes", "hooks", "worktree", "input"}},
		{Name: "Node", Type: reflect.TypeOf(spec.Node{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"id", "depends_on", "when", "trigger_rule", "provider", "context", "cancel", "loop", "model", "executor", "command", "prompt", "bash", "script", "approval", "loop_group", "subworkflow", "foreach", "workflow", "attempts", "allow_failure", "always_run", "timeout", "idle_timeout", "hooks", "native_hooks", "allowed_tools", "denied_tools", "skills", "mcp", "sandbox", "requires", "tool_approval", "side_effect", "adapter", "output_format", "output_type", "output_mime", "output_path"}},
		{Name: "Config", Type: reflect.TypeOf(spec.Config{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"apiVersion", "kind", "default_assistant", "models", "assistants", "adapters", "task_sources"}},
		{Name: "AssistantSpec", Type: reflect.TypeOf(spec.AssistantSpec{}), DefaultSupport: "supported-alpha", DefaultDecision: "defer", Expected: []string{"type", "argv", "binary", "args", "agent", "auto_approve", "session_dir", "project_trust", "env", "capabilities", "protocol", "max_output_bytes"}},
		{Name: "DomainAdapterSpec", Type: reflect.TypeOf(spec.DomainAdapterSpec{}), DefaultSupport: "supported-alpha", DefaultDecision: "defer", Expected: []string{"domain", "transport", "argv", "env", "operations", "reconcile_operations", "timeout", "max_output_bytes"}},
		{Name: "TaskSourceSpec", Type: reflect.TypeOf(spec.TaskSourceSpec{}), DefaultSupport: "supported-alpha", DefaultDecision: "defer", Expected: []string{"transport", "argv", "env", "timeout", "max_output_bytes"}},
		{Name: "BlockPackage", Type: reflect.TypeOf(blockcatalog.Package{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"apiVersion", "kind", "metadata", "dependencies", "requirements", "blocks", "roles", "templates", "governance"}},
		{Name: "OutputFormat", Type: reflect.TypeOf(spec.OutputFormat{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"type", "description", "properties", "required", "enum", "items", "minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum", "maximum", "minProperties", "maxProperties", "additionalProperties"}},
		{Name: "WorkflowRunSpec", Type: reflect.TypeOf(spec.WorkflowRunSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"path", "input", "output_node", "isolation", "repository", "policy", "fan_out"}},
		{Name: "PolicySpec", Type: reflect.TypeOf(spec.PolicySpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"allowed_tools", "denied_tools", "skills", "mcp", "sandbox", "requires"}},
		{Name: "InputContract", Type: reflect.TypeOf(spec.InputContract{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"format", "schema"}},
		{Name: "AttemptsSpec", Type: reflect.TypeOf(spec.AttemptsSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"max", "retry_on", "retry_session", "backoff"}},
		{Name: "BackoffSpec", Type: reflect.TypeOf(spec.BackoffSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"initial", "multiplier", "max", "jitter"}},
		{Name: "SandboxSpec", Type: reflect.TypeOf(spec.SandboxSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"filesystem", "network", "enforcement"}},
		{Name: "HookSet", Type: reflect.TypeOf(spec.HookSet{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"before_node", "after_node", "before_complete", "on_failure"}},
		{Name: "HookSpec", Type: reflect.TypeOf(spec.HookSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"id", "bash", "on_failure"}},
		{Name: "ApprovalSpec", Type: reflect.TypeOf(spec.ApprovalSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"message", "capture_response"}},
		{Name: "LoopGroupSpec", Type: reflect.TypeOf(spec.LoopGroupSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"max_iterations", "nodes", "until", "until_bash", "fresh_context"}},
		{Name: "UntilSpec", Type: reflect.TypeOf(spec.UntilSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"node", "exit_code", "output_contains", "signal", "requires"}},
		{Name: "SubworkflowSpec", Type: reflect.TypeOf(spec.SubworkflowSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"path", "inputs", "output_node"}},
		{Name: "ForeachSpec", Type: reflect.TypeOf(spec.ForeachSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"items", "items_from", "as", "parallel", "subworkflow"}},
		{Name: "WorkflowFanOutSpec", Type: reflect.TypeOf(spec.WorkflowFanOutSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"items_from", "as", "max_parallel", "max_items", "join", "allow_empty", "allow_duplicates"}},
		{Name: "ToolApprovalSpec", Type: reflect.TypeOf(spec.ToolApprovalSpec{}), DefaultSupport: "supported-alpha", DefaultDecision: "defer", Expected: []string{"mode", "tools", "message"}},
		{Name: "AdapterCallSpec", Type: reflect.TypeOf(spec.AdapterCallSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"name", "operation", "input"}},
		{Name: "SideEffectSpec", Type: reflect.TypeOf(spec.SideEffectSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"mode", "idempotency_key"}},
		{Name: "ScriptSpec", Type: reflect.TypeOf(spec.ScriptSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"runtime", "path", "inline", "args", "env", "working_directory", "dependencies"}},
		{Name: "WorktreeSpec", Type: reflect.TypeOf(spec.WorktreeSpec{}), DefaultSupport: "stable-candidate", DefaultDecision: "keep", Expected: []string{"enabled", "base", "branch_prefix", "cleanup", "allow_dirty"}},
	}
}

func applyFieldOverride(value *FieldDecision) {
	key := value.Contract + "." + value.Field
	switch key {
	case "Workflow.apiVersion", "Config.apiVersion", "BlockPackage.apiVersion":
		value.Decision = "migrate-value"
		value.Migration = "takt/v1alpha1 -> takt/v1beta1 when v1beta1 is released; v0.2 continues to read v1alpha1"
	case "Node.executor":
		value.Support = "supported-alpha"
		value.Decision = "defer"
		value.Notes = "external executor selection remains alpha until an external wrapper proves the public seam"
	case "Node.native_hooks":
		value.Support = "supported-alpha"
		value.Decision = "defer"
		value.Notes = "provider-specific host/assistant hook payload; not part of the portable v1beta1 core"
	case "Node.tool_approval":
		value.Support = "supported-alpha"
		value.Decision = "defer"
		value.Notes = "requires controllable tool lifecycle and external seam evidence"
	case "Config.assistants":
		value.Notes = "field stays; nested AssistantSpec remains supported-alpha and capability-driven"
	case "Config.task_sources":
		value.Support = "supported-alpha"
		value.Decision = "defer"
		value.Notes = "structured task ingestion stays alpha until external source evidence is collected"
	case "Config.adapters":
		value.Notes = "field stays; nested DomainAdapterSpec remains supported-alpha while neutral adapter node semantics stay stable-candidate"
	case "OutputFormat.type":
		value.Notes = "part of takt-schema-subset/v1; this is not arbitrary JSON Schema"
	}
}

func jsonFields(t reflect.Type) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out = append(out, name)
	}
	return out
}

func auditedFieldMismatches() []string {
	var mismatches []string
	for _, contract := range auditedContracts() {
		actual := jsonFields(contract.Type)
		expected := append([]string(nil), contract.Expected...)
		sort.Strings(actual)
		sort.Strings(expected)
		if !reflect.DeepEqual(actual, expected) {
			mismatches = append(mismatches, contract.Name+": expected="+strings.Join(expected, ",")+" actual="+strings.Join(actual, ","))
		}
	}
	return mismatches
}
