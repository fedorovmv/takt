package spec

import "encoding/json"

type Metadata struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type Workflow struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Metadata       `json:"metadata"`
	Defaults   Defaults       `json:"defaults,omitempty"`
	Nodes      []Node         `json:"nodes"`
	Hooks      HookSet        `json:"hooks,omitempty"`
	Worktree   WorktreeSpec   `json:"worktree,omitempty"`
	Input      *InputContract `json:"input,omitempty"`
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
	Guard        string            `json:"-"`
	DependsOn    []string          `json:"depends_on,omitempty"`
	When         string            `json:"when,omitempty"`
	TriggerRule  string            `json:"trigger_rule,omitempty"`
	Assistant    string            `json:"assistant,omitempty"`
	Model        string            `json:"model,omitempty"`
	Session      string            `json:"session,omitempty"`
	Executor     string            `json:"executor,omitempty"`
	Command      string            `json:"command,omitempty"`
	Prompt       string            `json:"prompt,omitempty"`
	Bash         string            `json:"bash,omitempty"`
	Script       *ScriptSpec       `json:"script,omitempty"`
	Approval     *ApprovalSpec     `json:"approval,omitempty"`
	LoopGroup    *LoopGroupSpec    `json:"loop_group,omitempty"`
	Subworkflow  *SubworkflowSpec  `json:"subworkflow,omitempty"`
	Foreach      *ForeachSpec      `json:"foreach,omitempty"`
	WorkflowRun  *WorkflowRunSpec  `json:"workflow,omitempty"`
	Internal     *InternalNodeSpec `json:"-"`
	Attempts     AttemptsSpec      `json:"attempts,omitempty"`
	AllowFailure bool              `json:"allow_failure,omitempty"`
	AlwaysRun    bool              `json:"always_run,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	IdleTimeout  string            `json:"idle_timeout,omitempty"`
	Hooks        HookSet           `json:"hooks,omitempty"`
	NativeHooks  json.RawMessage   `json:"native_hooks,omitempty"`
	AllowedTools *[]string         `json:"allowed_tools,omitempty"`
	DeniedTools  []string          `json:"denied_tools,omitempty"`
	Skills       *[]string         `json:"skills,omitempty"`
	MCP          string            `json:"mcp,omitempty"`
	Sandbox      *SandboxSpec      `json:"sandbox,omitempty"`
	Requires     []string          `json:"requires,omitempty"`
	ToolApproval *ToolApprovalSpec `json:"tool_approval,omitempty"`
	SideEffect   *SideEffectSpec   `json:"side_effect,omitempty"`
	Adapter      *AdapterCallSpec  `json:"adapter,omitempty"`
	OutputFormat *OutputFormat     `json:"output_format,omitempty"`
	OutputType   string            `json:"output_type,omitempty"`
	OutputMIME   string            `json:"output_mime,omitempty"`
	OutputPath   string            `json:"output_path,omitempty"`
}

// InputContract gives a workflow an explicit machine-readable input boundary.
// JSON inputs use the same strict schema subset as structured node outputs.
type InputContract struct {
	Format string        `json:"format"`
	Schema *OutputFormat `json:"schema,omitempty"`
}

// ToolApprovalSpec requires a controllable assistant/external worker to stop
// before selected tool calls and wait for an explicit allow/deny decision.
type ToolApprovalSpec struct {
	Mode    string   `json:"mode"`
	Tools   []string `json:"tools,omitempty"`
	Message string   `json:"message,omitempty"`
}

// AdapterCallSpec invokes a provider-neutral engineering-system operation.
// The adapter name resolves from Config.adapters; operation names are domain
// local (for example change.create, item.get, run.start). Input is JSON with
// the ordinary Takt template syntax applied before invocation.
type AdapterCallSpec struct {
	Name      string `json:"name"`
	Operation string `json:"operation"`
	Input     string `json:"input,omitempty"`
}

// SideEffectSpec marks an external or domain-adapter node whose effects may outlive the process
// process. idempotent permits safe replay; reconcile requires an explicit
// external fact check after an expired/unknown claim before Takt can retry.
type SideEffectSpec struct {
	Mode           string `json:"mode"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// ScriptSpec runs deterministic project code without an assistant. Path and
// dependencies are resolved relative to the workflow definition and mapped to
// the execution worktree when they belong to the control checkout.
type ScriptSpec struct {
	Runtime      string            `json:"runtime"`
	Path         string            `json:"path,omitempty"`
	Inline       string            `json:"inline,omitempty"`
	Args         []string          `json:"args,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	WorkingDir   string            `json:"working_directory,omitempty"`
	Dependencies []string          `json:"dependencies,omitempty"`
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
	Parallel    bool                `json:"parallel,omitempty"`
	Subworkflow SubworkflowSpec     `json:"subworkflow"`
}

type ForeachItemsSource struct {
	Path string `json:"path"`
}

// WorkflowRunSpec starts another workflow as a separately persisted child Run.
// Unlike subworkflow, it is not expanded into the parent DAG.
type WorkflowRunSpec struct {
	Path       string              `json:"path"`
	Input      string              `json:"input,omitempty"`
	OutputNode string              `json:"output_node,omitempty"`
	Isolation  string              `json:"isolation,omitempty"`
	Repository string              `json:"repository,omitempty"`
	Policy     *PolicySpec         `json:"policy,omitempty"`
	FanOut     *WorkflowFanOutSpec `json:"fan_out,omitempty"`
}

// WorkflowFanOutSpec starts one governed child Run per item resolved from a
// structured upstream node output. Child links are persisted before execution,
// so a partially completed group can be resumed without recreating finished
// children.
type WorkflowFanOutSpec struct {
	ItemsFrom       string `json:"items_from"`
	As              string `json:"as,omitempty"`
	MaxParallel     int    `json:"max_parallel,omitempty"`
	MaxItems        int    `json:"max_items,omitempty"`
	Join            string `json:"join,omitempty"`
	AllowEmpty      bool   `json:"allow_empty,omitempty"`
	AllowDuplicates bool   `json:"allow_duplicates,omitempty"`
}

type PolicySpec struct {
	AllowedTools *[]string    `json:"allowed_tools,omitempty"`
	DeniedTools  []string     `json:"denied_tools,omitempty"`
	Skills       *[]string    `json:"skills,omitempty"`
	MCP          string       `json:"mcp,omitempty"`
	Sandbox      *SandboxSpec `json:"sandbox,omitempty"`
	Requires     []string     `json:"requires,omitempty"`
}

// InternalNodeSpec is produced by workflow expansion and is never accepted
// from user YAML. It keeps subworkflow/foreach execution on the ordinary DAG
// scheduler without adding a second runtime.
type InternalNodeSpec struct {
	Mode           string
	ResultFrom     string
	ResultsFrom    []string
	DefinitionHash string
	WorkflowName   string
	Worktree       *WorktreeSpec
}

// SandboxSpec is an assistant-enforced node policy. It is not an OS sandbox.
// Adapters must reject unsupported guarantees instead of silently ignoring them.
type SandboxSpec struct {
	Filesystem string `json:"filesystem,omitempty"`
	Network    string `json:"network,omitempty"`
}

// OutputFormat describes the JSON value an AI node must return. It is a
// deliberately small JSON-Schema-compatible subset used for routing and
// machine-readable workflow decisions.
type OutputFormat struct {
	Type                 string                  `json:"type"`
	Description          string                  `json:"description,omitempty"`
	Properties           map[string]OutputFormat `json:"properties,omitempty"`
	Required             []string                `json:"required,omitempty"`
	Enum                 []string                `json:"enum,omitempty"`
	Items                *OutputFormat           `json:"items,omitempty"`
	MinItems             int                     `json:"minItems,omitempty"`
	MaxItems             int                     `json:"maxItems,omitempty"`
	UniqueItems          bool                    `json:"uniqueItems,omitempty"`
	MinLength            int                     `json:"minLength,omitempty"`
	MaxLength            int                     `json:"maxLength,omitempty"`
	Pattern              string                  `json:"pattern,omitempty"`
	Minimum              *float64                `json:"minimum,omitempty"`
	Maximum              *float64                `json:"maximum,omitempty"`
	MinProperties        int                     `json:"minProperties,omitempty"`
	MaxProperties        int                     `json:"maxProperties,omitempty"`
	AdditionalProperties *bool                   `json:"additionalProperties,omitempty"`
}

type AttemptsSpec struct {
	Max          int      `json:"max,omitempty"`
	RetryOn      []string `json:"retry_on,omitempty"`
	RetrySession string   `json:"retry_session,omitempty"`
}

// WorktreeSpec isolates a Run in a dedicated Git branch and worktree. The
// control workspace still owns state and artifacts; only node execution moves
// to the worktree.
type WorktreeSpec struct {
	Enabled      bool   `json:"enabled,omitempty"`
	Base         string `json:"base,omitempty"`
	BranchPrefix string `json:"branch_prefix,omitempty"`
	Cleanup      string `json:"cleanup,omitempty"`
	AllowDirty   bool   `json:"allow_dirty,omitempty"`
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
	APIVersion       string                       `json:"apiVersion"`
	Kind             string                       `json:"kind"`
	DefaultAssistant string                       `json:"default_assistant,omitempty"`
	Models           map[string]ModelSpec         `json:"models,omitempty"`
	Assistants       map[string]AssistantSpec     `json:"assistants,omitempty"`
	Adapters         map[string]DomainAdapterSpec `json:"adapters,omitempty"`
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

// DomainAdapterSpec binds neutral SCM/tracker/CI operations to either a
// process protocol implementation or an MCP stdio server. Operation maps are
// transport details and never appear in workflows.
type DomainAdapterSpec struct {
	Domain              string            `json:"domain"`
	Transport           string            `json:"transport"`
	Argv                []string          `json:"argv"`
	Env                 map[string]string `json:"env,omitempty"`
	Operations          map[string]string `json:"operations,omitempty"`
	ReconcileOperations map[string]string `json:"reconcile_operations,omitempty"`
	Timeout             string            `json:"timeout,omitempty"`
	MaxOutputBytes      int               `json:"max_output_bytes,omitempty"`
}
