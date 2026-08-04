package spec

import "encoding/json"

type Metadata struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type Workflow struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Defaults   Defaults `json:"defaults,omitempty"`
	Nodes      []Node   `json:"nodes"`
	Hooks      HookSet  `json:"hooks,omitempty"`
}

type Defaults struct {
	Assistant string `json:"assistant,omitempty"`
	Model     string `json:"model,omitempty"`
	Session   string `json:"session,omitempty"`
}

type Node struct {
	ID           string            `json:"id"`
	Hidden       bool              `json:"-"`
	PublicParent string            `json:"-"`
	DependsOn    []string          `json:"depends_on,omitempty"`
	When         string            `json:"when,omitempty"`
	TriggerRule  string            `json:"trigger_rule,omitempty"`
	Assistant    string            `json:"assistant,omitempty"`
	Model        string            `json:"model,omitempty"`
	Session      string            `json:"session,omitempty"`
	Command      string            `json:"command,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	Bash         string            `json:"bash,omitempty"`
	Approval     *ApprovalSpec     `json:"approval,omitempty"`
	LoopGroup    *LoopGroupSpec    `json:"loop_group,omitempty"`
	Subworkflow  *SubworkflowSpec  `json:"subworkflow,omitempty"`
	Foreach      *ForeachSpec      `json:"foreach,omitempty"`
	Internal     *InternalNodeSpec `json:"-"`
	Attempts     AttemptsSpec      `json:"attempts,omitempty"`
	AllowFailure bool              `json:"allow_failure,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	Hooks        HookSet           `json:"hooks,omitempty"`
	NativeHooks  json.RawMessage   `json:"native_hooks,omitempty"`
}

type SubworkflowSpec struct {
	Path       string            `json:"path"`
	Inputs     map[string]string `json:"inputs,omitempty"`
	OutputNode string            `json:"output_node,omitempty"`
}

type ForeachSpec struct {
	Items       []any               `json:"items,omitempty"`
	ItemsFrom   *ForeachItemsSource `json:"items_from,omitempty"`
	As          string              `json:"as,omitempty"`
	Subworkflow SubworkflowSpec     `json:"subworkflow"`
}

type ForeachItemsSource struct {
	Path string `json:"path"`
}

// InternalNodeSpec is produced by workflow expansion and is never accepted
// from user YAML. It keeps subworkflow/foreach execution on the ordinary DAG
// scheduler without adding a second runtime.
type InternalNodeSpec struct {
	Mode           string
	ResultFrom     string
	ResultsFrom    []string
	DefinitionHash string
}

type AttemptsSpec struct {
	Max int `json:"max,omitempty"`
}

type ApprovalSpec struct {
	Message         string `json:"message"`
	CaptureResponse bool   `json:"capture_response,omitempty"`
}

type LoopGroupSpec struct {
	MaxIterations int       `json:"max_iterations"`
	Nodes         []Node    `json:"nodes"`
	Until         UntilSpec `json:"until"`
}

type UntilSpec struct {
	Node           string `json:"node"`
	ExitCode       *int   `json:"exit_code,omitempty"`
	OutputContains string `json:"output_contains,omitempty"`
}

type HookSet struct {
	BeforeNode     []HookSpec `json:"before_node,omitempty"`
	AfterNode      []HookSpec `json:"after_node,omitempty"`
	BeforeComplete []HookSpec `json:"before_complete,omitempty"`
	OnFailure      []HookSpec `json:"on_failure,omitempty"`
}

type HookSpec struct {
	ID        string       `json:"id,omitempty"`
	Bash      string       `json:"bash"`
	OnFailure HookDecision `json:"on_failure,omitempty"`
}

type HookDecision struct {
	Action  string `json:"action,omitempty"`
	Session string `json:"session,omitempty"`
}

type Config struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Models     map[string]ModelSpec     `json:"models,omitempty"`
	Assistants map[string]AssistantSpec `json:"assistants,omitempty"`
}

type ModelSpec struct {
	Provider string         `json:"provider"`
	ID       string         `json:"id"`
	Params   map[string]any `json:"params,omitempty"`
}

type AssistantSpec struct {
	Type           string            `json:"type"`
	Argv           []string          `json:"argv,omitempty"`
	Binary         string            `json:"binary,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Agent          string            `json:"agent,omitempty"`
	AutoApprove    bool              `json:"auto_approve,omitempty"`
	SessionDir     string            `json:"session_dir,omitempty"`
	ProjectTrust   string            `json:"project_trust,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	Protocol       string            `json:"protocol,omitempty"`
	MaxOutputBytes int               `json:"max_output_bytes,omitempty"`
}
