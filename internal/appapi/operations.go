package appapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type OperationStage string

const (
	StageStable       OperationStage = "stable"
	StageExtension    OperationStage = "extension"
	StageExperimental OperationStage = "experimental"
)

// OperationDescriptor is the single source of truth for a transport-neutral
// application operation and its MCP/documentation projection. Request decoding
// is bound to this descriptor through registerOperation in registry.go.
type OperationDescriptor struct {
	ID          string
	MCPTool     string
	Title       string
	Description string
	InputSchema map[string]any
	Annotations map[string]any
	Stage       OperationStage
}

func object(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}
func integerProp(description string, min, max int) map[string]any {
	return map[string]any{"type": "integer", "description": description, "minimum": min, "maximum": max}
}

var readOnly = map[string]any{"readOnlyHint": true, "destructiveHint": false}
var mutating = map[string]any{"readOnlyHint": false}

var canonicalOperationDescriptors = []OperationDescriptor{
	{ID: "notify.dispatch", Title: "Dispatch pending Takt notifications", Description: "Dispatch pending durable notifications through configured sinks.", InputSchema: object(map[string]any{}), Annotations: mutating, Stage: StageExtension},
	{ID: "task.start", MCPTool: "takt.task.start", Title: "Start a managed Takt task", Description: "Route a natural-language task or a configured structured task source to a specialized workflow, the stable simple-reliable template, or bounded dynamic composition. Provide goal, or source + source_ref. By default returns a preview; go=true confirms and starts it.", InputSchema: object(map[string]any{"goal": stringProp("Natural-language task; mutually exclusive with source"), "source": stringProp("Configured structured task source"), "source_ref": stringProp("External reference for source"), "profile": stringProp("Installed profile, defaults to code"), "go": boolProp("Confirm the preview and start immediately")}), Annotations: mutating, Stage: StageExperimental},
	{ID: "task.status", MCPTool: "takt.task.status", Title: "Get managed task status", Description: "Read a compact task view by plan_id or run_id, including whether user input is needed.", InputSchema: object(map[string]any{"reference": stringProp("Plan or Run ID")}, "reference"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "task.respond", MCPTool: "takt.task.respond", Title: "Respond to a managed task", Description: "Approve, answer, steer, pause, resume, continue or retry a task without exposing the internal state machine.", InputSchema: object(map[string]any{"reference": stringProp("Plan or Run ID"), "action": map[string]any{"type": "string", "enum": []string{"go", "continue", "answer", "steer", "pause", "resume", "retry"}}, "message": stringProp("Answer or steering text when required"), "node_id": stringProp("Optional waiting or failed node")}, "reference", "action"), Annotations: mutating, Stage: StageExperimental},
	{ID: "task.stop", MCPTool: "takt.task.stop", Title: "Stop a managed task", Description: "Abandon a plan or Run while preserving its durable history.", InputSchema: object(map[string]any{"reference": stringProp("Plan or Run ID"), "reason": stringProp("Optional stop reason")}, "reference"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}, Stage: StageExperimental},
	{ID: "task.explain", MCPTool: "takt.task.explain", Title: "Explain a managed task", Description: "Return detailed routing, controls, phases, child Runs and evidence only when deeper inspection is requested.", InputSchema: object(map[string]any{"reference": stringProp("Plan or Run ID")}, "reference"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "workflow.list", MCPTool: "takt.workflow.list", Title: "List Takt workflows", Description: "List deterministic workflow selectors published by an installed Takt profile.", InputSchema: object(map[string]any{"profile": stringProp("Installed profile name, for example code")}, "profile"), Annotations: readOnly, Stage: StageStable},
	{ID: "workflow.describe", MCPTool: "takt.workflow.describe", Title: "Describe a Takt workflow", Description: "Describe the public DAG of a profile selector before starting it.", InputSchema: object(map[string]any{"selector": stringProp("Profile selector such as code:plan-to-pr")}, "selector"), Annotations: readOnly, Stage: StageStable},
	{ID: "block.list", MCPTool: "takt.block.list", Title: "List trusted Dynamic Takt blocks", Description: "List explicitly trusted block packages, governance limits, templates and blocks available to a profile.", InputSchema: object(map[string]any{"profile": stringProp("Installed profile, defaults to code")}), Annotations: readOnly, Stage: StageExtension},
	{ID: "block.describe", MCPTool: "takt.block.describe", Title: "Describe trusted Dynamic Takt block", Description: "Describe one trusted block, its package scope, output paths, capabilities, integrations and policy.", InputSchema: object(map[string]any{"profile": stringProp("Installed profile, defaults to code"), "name": stringProp("Trusted block name")}, "name"), Annotations: readOnly, Stage: StageExtension},
	{ID: "host.begin", MCPTool: "takt.host.begin", Title: "Begin managed Takt session", Description: "Bind a coding-agent host session to a Takt plan before the main LLM handles the task. Strict mode requires interception and recovery capabilities.", InputSchema: object(map[string]any{
		"host": stringProp("Coding-agent host, for example pi or opencode"), "host_session_id": stringProp("Stable host session ID"), "goal": stringProp("User task"), "profile": stringProp("Planning profile"),
		"enforcement":  map[string]any{"type": "string", "enum": []string{"advisory", "guarded", "strict"}},
		"capabilities": object(map[string]any{"command_interception": boolProp("Host intercepts /takt before the LLM"), "input_interception": boolProp("Host intercepts later input"), "tool_call_blocking": boolProp("Host blocks tools before execution"), "completion_blocking": boolProp("Host blocks premature completion"), "session_recovery": boolProp("Host restores managed mode")}),
		"candidate":    map[string]any{"type": "object", "description": "Optional externally proposed WorkflowPlan; Takt still validates it"},
	}, "host", "host_session_id", "goal"), Annotations: mutating, Stage: StageExperimental},
	{ID: "host.confirm", MCPTool: "takt.host.confirm", Title: "Confirm managed Takt session", Description: "Confirm preview and start the bound Takt plan.", InputSchema: object(map[string]any{"session_id": stringProp("Managed host session ID"), "confirm": boolProp("Confirm preview and budgets")}, "session_id", "confirm"), Annotations: mutating, Stage: StageExperimental},
	{ID: "host.get", MCPTool: "takt.host.get", Title: "Get managed Takt session", Description: "Read managed host session and bound plan state.", InputSchema: object(map[string]any{"session_id": stringProp("Managed host session ID")}, "session_id"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "host.find", MCPTool: "takt.host.find", Title: "Find managed Takt session", Description: "Recover a durable managed session by coding-agent host and session ID.", InputSchema: object(map[string]any{"host": stringProp("Coding-agent host"), "host_session_id": stringProp("Stable host session ID")}, "host", "host_session_id"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "host.guard_tool", MCPTool: "takt.host.guard_tool", Title: "Guard coding-agent tool", Description: "Fail closed on a host tool call while a Takt-managed workflow is active.", InputSchema: object(map[string]any{"session_id": stringProp("Managed host session ID"), "tool": stringProp("Host tool name"), "read_only": boolProp("Host advisory read-only declaration; never overrides the Takt allowlist")}, "session_id", "tool"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "host.guard_completion", MCPTool: "takt.host.guard_completion", Title: "Guard coding-agent completion", Description: "Block a final response while the bound Takt plan is active.", InputSchema: object(map[string]any{"session_id": stringProp("Managed host session ID"), "kind": map[string]any{"type": "string", "enum": []string{"final", "status", "question"}}}, "session_id", "kind"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "host.release", MCPTool: "takt.host.release", Title: "Release managed Takt session", Description: "Explicitly leave managed mode without cancelling the underlying Takt plan.", InputSchema: object(map[string]any{"session_id": stringProp("Managed host session ID")}, "session_id"), Annotations: mutating, Stage: StageExperimental},
	{ID: "plan.create", MCPTool: "takt.plan", Title: "Plan with Dynamic Takt", Description: "Choose an existing workflow or create a bounded task-specific WorkflowPlan from approved blocks. Returns preview, budget and confirmation requirement.", InputSchema: object(map[string]any{
		"goal": stringProp("Natural-language engineering goal"), "profile": stringProp("Installed profile, defaults to code"), "candidate": map[string]any{"type": "object", "description": "Optional externally proposed WorkflowPlan; Takt still validates it"},
	}, "goal"), Annotations: mutating, Stage: StageExperimental},
	{ID: "plan.get", MCPTool: "takt.plan.get", Title: "Get Dynamic Takt plan", Description: "Read plan revisions, current phase segment, execution Runs, steering and promotion state.", InputSchema: object(map[string]any{"plan_id": stringProp("Durable plan ID")}, "plan_id"), Annotations: readOnly, Stage: StageExperimental},
	{ID: "plan.execute", MCPTool: "takt.execute", Title: "Execute Dynamic Takt plan", Description: "Execute a previewed existing or planned workflow. Planned workflows require explicit confirm=true.", InputSchema: object(map[string]any{"plan_id": stringProp("Durable plan ID"), "confirm": boolProp("Confirm the displayed preview and hard limits")}, "plan_id"), Annotations: mutating, Stage: StageExperimental},
	{ID: "plan.steer", MCPTool: "takt.run.steer", Title: "Steer Dynamic Takt run", Description: "Queue an instruction for the next replanning checkpoint, or continue a plan waiting for user input.", InputSchema: object(map[string]any{"plan_id": stringProp("Plan ID"), "run_id": stringProp("Any execution Run ID owned by the plan"), "message": stringProp("Concrete steering instruction")}, "message"), Annotations: mutating, Stage: StageExperimental},
	{ID: "plan.promote", MCPTool: "takt.plan.promote", Title: "Promote successful dynamic plan", Description: "Compile the latest successful plan revision into a validated project workflow under .takt/workflows/generated.", InputSchema: object(map[string]any{"plan_id": stringProp("Completed plan ID"), "name": stringProp("Project workflow name"), "force": boolProp("Replace an existing generated workflow")}, "plan_id", "name"), Annotations: mutating, Stage: StageExperimental},
	{ID: "run.start", MCPTool: "takt.run.start", Title: "Start a Takt Run", Description: "Validate definitions and start a local Takt Run. Detached mode is the default and returns a durable run_id for polling.", InputSchema: object(map[string]any{
		"selector": stringProp("Profile selector or workflow file path"), "input": stringProp("Input text or a readable input file path"),
		"config_path": stringProp("Optional config override"), "worktree": boolProp("Force or disable managed Git worktree isolation"),
		"worktree_base": stringProp("Optional Git base revision"), "keep_worktree": boolProp("Keep a successful worktree"),
		"allow_dirty_worktree": boolProp("Allow a dirty control checkout and start from committed state"), "detached": boolProp("Return after the Run is durably started; defaults to true"),
	}, "selector"), Annotations: mutating, Stage: StageStable},
	{ID: "run.get", MCPTool: "takt.run.get", Title: "Get Takt Run", Description: "Read the current public Run state, including waiting approval, nodes, usage and durable child links.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID")}, "run_id"), Annotations: readOnly, Stage: StageStable},
	{ID: "run.list", MCPTool: "takt.run.list", Title: "List Takt Runs", Description: "List durable local Runs with effective state, attention reason, current phase, usage and artifact counts.", InputSchema: object(map[string]any{
		"status": stringProp("Optional status filter"), "active_only": boolProp("Return only non-terminal Runs"),
		"attention_only": boolProp("Return only Runs requiring operator attention"), "root_only": boolProp("Exclude governed child Runs"),
		"limit": integerProp("Maximum number of Runs", 1, 10000),
	}), Annotations: readOnly, Stage: StageStable},
	{ID: "run.attention", MCPTool: "takt.run.attention", Title: "List Runs requiring attention", Description: "Return approvals, questions, tool approvals, failures and paused Runs that require an operator action.", InputSchema: object(map[string]any{}), Annotations: readOnly, Stage: StageStable},
	{ID: "run.summary", MCPTool: "takt.run.summary", Title: "Summarize Takt Run", Description: "Return an operator-oriented result projection with progress, descendants, usage, artifacts, output and remaining attention.", InputSchema: object(map[string]any{"run_id": stringProp("Run ID"), "recursive": boolProp("Aggregate descendant Runs")}, "run_id"), Annotations: readOnly, Stage: StageStable},
	{ID: "run.pause", MCPTool: "takt.run.pause", Title: "Pause Takt Run", Description: "Request a safe pause at node boundaries for the Run and active descendants. Running attempts finish before the pause takes effect.", InputSchema: object(map[string]any{"run_id": stringProp("Run ID")}, "run_id"), Annotations: mutating, Stage: StageStable},
	{ID: "run.resume_paused", MCPTool: "takt.run.resume_paused", Title: "Resume paused Takt Run", Description: "Clear pause requests and continue a paused Run. A Run paused while waiting returns to the same waiting state.", InputSchema: object(map[string]any{"run_id": stringProp("Run ID")}, "run_id"), Annotations: mutating, Stage: StageStable},
	{ID: "run.retry", MCPTool: "takt.run.retry", Title: "Retry failed Takt node", Description: "Reset one failed node and its dependent remainder, preserving completed prerequisites and operator retry history.", InputSchema: object(map[string]any{"run_id": stringProp("Run ID"), "node_id": stringProp("Failed node; defaults to the first failed node")}, "run_id"), Annotations: mutating, Stage: StageStable},
	{ID: "run.fork", MCPTool: "takt.run.fork", Title: "Fork Takt Run", Description: "Create a new Run from the same workflow and options, or a new Dynamic Plan when the source belongs to Dynamic Takt.", InputSchema: object(map[string]any{"run_id": stringProp("Source Run ID"), "input": stringProp("Optional replacement input or Dynamic Plan goal")}, "run_id"), Annotations: mutating, Stage: StageStable},
	{ID: "run.abandon", MCPTool: "takt.run.abandon", Title: "Abandon Takt Run", Description: "Stop servicing a Run and active descendants while preserving history with an abandoned terminal state.", InputSchema: object(map[string]any{"run_id": stringProp("Run ID"), "reason": stringProp("Operator reason")}, "run_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}, Stage: StageStable},
	{ID: "run.recover", MCPTool: "takt.run.recover", Title: "Recover interrupted Runs", Description: "Detect Runs whose executor process disappeared, mark active attempts as worker_lost and continue them from durable state.", InputSchema: object(map[string]any{}), Annotations: mutating, Stage: StageStable},
	{ID: "notify.list", MCPTool: "takt.notify.list", Title: "List Takt notifications", Description: "Read durable local notifications produced by autonomous Runs; supports an unread-only view for coding-agent hosts.", InputSchema: object(map[string]any{"unread_only": boolProp("Only unacknowledged notifications"), "limit": integerProp("Maximum notifications", 1, 10000)}), Annotations: readOnly, Stage: StageExtension},
	{ID: "notify.ack", MCPTool: "takt.notify.ack", Title: "Acknowledge Takt notification", Description: "Mark one durable notification as acknowledged.", InputSchema: object(map[string]any{"id": stringProp("Notification ID")}, "id"), Annotations: mutating, Stage: StageExtension},
	{ID: "notify.test", MCPTool: "takt.notify.test", Title: "Test Takt notifications", Description: "Create and deliver a local test notification through configured sinks.", InputSchema: object(map[string]any{"message": stringProp("Optional test message")}), Annotations: mutating, Stage: StageExtension},
	{ID: "run.resume", MCPTool: "takt.run.resume", Title: "Resume Takt Run", Description: "Resume a failed or otherwise resumable Run after external correction. Definitions and fingerprints are verified first.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID")}, "run_id"), Annotations: mutating, Stage: StageStable},
	{ID: "run.answer", MCPTool: "takt.run.answer", Title: "Answer Takt approval", Description: "Submit an approval response and continue the waiting child and parent Run chain.", InputSchema: object(map[string]any{"run_id": stringProp("Root or direct child Run ID"), "node_id": stringProp("Public approval node ID"), "value": stringProp("Approval response")}, "run_id", "node_id", "value"), Annotations: mutating, Stage: StageStable},
	{ID: "run.cancel", MCPTool: "takt.run.cancel", Title: "Cancel Takt Run", Description: "Request durable cancellation of a Run and its active child tree.", InputSchema: object(map[string]any{"run_id": stringProp("Durable Takt Run ID"), "reason": stringProp("Cancellation reason")}, "run_id"), Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": true}, Stage: StageStable},
	{ID: "run.children", MCPTool: "takt.run.children", Title: "List child Runs", Description: "List direct governed child Runs and fan-out item metadata.", InputSchema: object(map[string]any{"run_id": stringProp("Parent Run ID")}, "run_id"), Annotations: readOnly, Stage: StageStable},
	{ID: "run.artifacts", MCPTool: "takt.run.artifacts", Title: "List Takt artifacts", Description: "List typed artifacts with checksum and provenance; optionally include bounded local file content.", InputSchema: object(map[string]any{
		"run_id": stringProp("Run ID"), "node_id": stringProp("Optional producer node filter"), "type": stringProp("Optional semantic type filter"),
		"recursive": boolProp("Include descendant Runs"), "include_content": boolProp("Include bounded artifact content"), "max_bytes": integerProp("Maximum bytes per included artifact; defaults to 65536", 1, 1048576),
	}, "run_id"), Annotations: readOnly, Stage: StageStable},
	{ID: "run.events", MCPTool: "takt.run.events", Title: "Read Takt Run events", Description: "Read events after a durable revision cursor. wait_ms enables bounded long polling for incremental monitoring.", InputSchema: object(map[string]any{
		"run_id": stringProp("Run ID"), "after_revision": integerProp("Return events with a greater revision", 0, int(^uint32(0))),
		"limit": integerProp("Maximum events, defaults to 200", 1, 1000), "wait_ms": integerProp("Long-poll wait, 0 to 30000 milliseconds", 0, 30000),
	}, "run_id"), Annotations: readOnly, Stage: StageStable},
}

var operationByMCPTool = func() map[string]string {
	out := make(map[string]string, len(canonicalOperationDescriptors))
	for _, descriptor := range canonicalOperationDescriptors {
		if descriptor.MCPTool == "" {
			continue
		}
		if prior, exists := out[descriptor.MCPTool]; exists {
			panic(fmt.Sprintf("duplicate canonical MCP tool %q for %s and %s", descriptor.MCPTool, prior, descriptor.ID))
		}
		out[descriptor.MCPTool] = descriptor.ID
	}
	return out
}()
var operationByID = func() map[string]OperationDescriptor {
	out := make(map[string]OperationDescriptor, len(canonicalOperationDescriptors))
	for _, descriptor := range canonicalOperationDescriptors {
		if descriptor.ID == "" {
			panic("canonical operation id is required")
		}
		if _, exists := out[descriptor.ID]; exists {
			panic("duplicate canonical operation id: " + descriptor.ID)
		}
		out[descriptor.ID] = descriptor
	}
	return out
}()

func CanonicalOperationForMCP(tool string) (string, bool) {
	id, ok := operationByMCPTool[tool]
	return id, ok
}
func CanonicalOperations() []OperationDescriptor {
	out := make([]OperationDescriptor, 0, len(canonicalOperationDescriptors))
	for _, descriptor := range canonicalOperationDescriptors {
		out = append(out, cloneDescriptor(descriptor))
	}
	return out
}
func Descriptor(id string) (OperationDescriptor, bool) {
	value, ok := operationByID[id]
	if !ok {
		return OperationDescriptor{}, false
	}
	return cloneDescriptor(value), true
}

func cloneDescriptor(value OperationDescriptor) OperationDescriptor {
	value.InputSchema = cloneMap(value.InputSchema)
	value.Annotations = cloneMap(value.Annotations)
	return value
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		switch typed := item.(type) {
		case map[string]any:
			out[key] = cloneMap(typed)
		case []string:
			out[key] = append([]string(nil), typed...)
		case []any:
			copyItems := make([]any, len(typed))
			for i, child := range typed {
				if childMap, ok := child.(map[string]any); ok {
					copyItems[i] = cloneMap(childMap)
				} else {
					copyItems[i] = child
				}
			}
			out[key] = copyItems
		default:
			out[key] = item
		}
	}
	return out
}

// RenderOperationDocs renders generated operation documentation from the same
// descriptors used by appapi and MCP. The checked-in document is protected by
// a regression test so documentation cannot silently drift from the API.
func RenderOperationDocs() string {
	descriptors := CanonicalOperations()
	sort.Slice(descriptors, func(i, j int) bool {
		if stageRank(descriptors[i].Stage) != stageRank(descriptors[j].Stage) {
			return stageRank(descriptors[i].Stage) < stageRank(descriptors[j].Stage)
		}
		return descriptors[i].ID < descriptors[j].ID
	})
	var b strings.Builder
	b.WriteString("# Canonical operation contracts\n\n")
	b.WriteString("Generated from `internal/appapi` operation descriptors. Do not edit by hand.\n\n")
	var stage OperationStage
	for _, d := range descriptors {
		if d.Stage != stage {
			stage = d.Stage
			b.WriteString("## " + stageHeading(stage) + "\n\n")
		}
		mcp := d.MCPTool
		if mcp == "" {
			mcp = "—"
		}
		fmt.Fprintf(&b, "### `%s` — %s\n\n", d.ID, d.Title)
		fmt.Fprintf(&b, "- MCP tool: `%s`\n", mcp)
		fmt.Fprintf(&b, "- Stage: `%s`\n\n", d.Stage)
		b.WriteString(d.Description + "\n\n")
		schema, err := json.MarshalIndent(d.InputSchema, "", "  ")
		if err != nil {
			panic(fmt.Sprintf("render operation %s input schema: %v", d.ID, err))
		}
		b.WriteString("Input schema:\n\n```json\n")
		b.Write(schema)
		b.WriteString("\n```\n\n")
	}
	return b.String()
}

func stageRank(stage OperationStage) int {
	switch stage {
	case StageStable:
		return 0
	case StageExtension:
		return 1
	case StageExperimental:
		return 2
	default:
		return 3
	}
}
func stageHeading(stage OperationStage) string {
	switch stage {
	case StageStable:
		return "Stable"
	case StageExtension:
		return "Extensions"
	case StageExperimental:
		return "Experimental"
	default:
		return string(stage)
	}
}
