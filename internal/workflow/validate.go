package workflow

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"takt/internal/artifacttype"
	"takt/internal/spec"
)

func Validate(wf *spec.Workflow) error {
	if wf.APIVersion != "takt/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", wf.APIVersion)
	}
	if wf.Kind != "Workflow" {
		return fmt.Errorf("kind must be Workflow")
	}
	if strings.TrimSpace(wf.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if err := validateWorktree(wf.Worktree); err != nil {
		return err
	}
	if wf.Input != nil {
		switch wf.Input.Format {
		case "text", "markdown":
			if wf.Input.Schema != nil {
				return fmt.Errorf("input.schema requires input.format: json")
			}
		case "json":
			if wf.Input.Schema == nil {
				return fmt.Errorf("input.format json requires input.schema")
			}
			if err := validateOutputFormat(*wf.Input.Schema, "input.schema"); err != nil {
				return err
			}
		default:
			return fmt.Errorf("input.format must be text, markdown, or json")
		}
	}
	return validateNodes(wf.Nodes, "nodes", false)
}

func validateWorktree(value spec.WorktreeSpec) error {
	if !value.Enabled {
		if value.Base != "" || value.BranchPrefix != "" || value.Cleanup != "" || value.AllowDirty {
			return fmt.Errorf("worktree options require worktree.enabled: true")
		}
		return nil
	}
	if strings.TrimSpace(value.BranchPrefix) != value.BranchPrefix {
		return fmt.Errorf("worktree.branch_prefix must not have surrounding whitespace")
	}
	if value.Cleanup != "" && value.Cleanup != "on_success" && value.Cleanup != "manual" {
		return fmt.Errorf("worktree.cleanup must be on_success or manual")
	}
	return nil
}

func validateNodes(nodes []spec.Node, scope string, insideLoop bool) error {
	if len(nodes) == 0 {
		return fmt.Errorf("%s must not be empty", scope)
	}
	byID := map[string]spec.Node{}
	for _, n := range nodes {
		if strings.TrimSpace(n.ID) == "" {
			return fmt.Errorf("%s contains node without id", scope)
		}
		if _, exists := byID[n.ID]; exists {
			return fmt.Errorf("duplicate node id %q in %s", n.ID, scope)
		}
		byID[n.ID] = n
		kinds := 0
		if n.Command != "" {
			kinds++
		}
		if n.Prompt != "" {
			kinds++
		}
		if n.Bash != "" {
			kinds++
		}
		if n.Script != nil {
			kinds++
		}
		if n.Approval != nil {
			kinds++
		}
		if n.LoopGroup != nil {
			kinds++
		}
		if n.Subworkflow != nil {
			kinds++
		}
		if n.Foreach != nil {
			kinds++
		}
		if n.WorkflowRun != nil {
			kinds++
		}
		if n.Internal != nil {
			kinds++
		}
		if n.Adapter != nil {
			kinds++
		}
		if kinds != 1 {
			return fmt.Errorf("node %q must define exactly one action", n.ID)
		}
		if n.Subworkflow != nil || n.Foreach != nil {
			return fmt.Errorf("node %q contains an unexpanded workflow container", n.ID)
		}
		if n.WorkflowRun != nil {
			if strings.TrimSpace(n.WorkflowRun.Path) == "" {
				return fmt.Errorf("node %q workflow.path is required", n.ID)
			}
			if n.WorkflowRun.Policy != nil {
				if err := validatePolicySpec(*n.WorkflowRun.Policy, "node "+n.ID+".workflow.policy"); err != nil {
					return err
				}
			}
			switch n.WorkflowRun.Isolation {
			case "", "inherit", "worktree", "none":
			default:
				return fmt.Errorf("node %q workflow.isolation must be inherit, worktree, or none", n.ID)
			}
			if strings.TrimSpace(n.WorkflowRun.Repository) != "" {
				if filepath.IsAbs(n.WorkflowRun.Repository) {
					return fmt.Errorf("node %q workflow.repository must be relative to the control workspace", n.ID)
				}
				if n.WorkflowRun.Isolation == "inherit" {
					return fmt.Errorf("node %q workflow.repository cannot use isolation inherit", n.ID)
				}
				if n.WorkflowRun.FanOut != nil {
					return fmt.Errorf("node %q workflow.repository does not support fan_out in v0.1.43", n.ID)
				}
			}
			if fanOut := n.WorkflowRun.FanOut; fanOut != nil {
				if _, err := fanOutSourceNode(fanOut.ItemsFrom); err != nil {
					return fmt.Errorf("node %q workflow.fan_out.items_from: %w", n.ID, err)
				}
				if fanOut.As != "" && !fanOutNameRE.MatchString(fanOut.As) {
					return fmt.Errorf("node %q workflow.fan_out.as must be an identifier", n.ID)
				}
				if fanOut.MaxParallel < 0 || fanOut.MaxParallel > 64 {
					return fmt.Errorf("node %q workflow.fan_out.max_parallel must be 0 (default 1) or between 1 and 64", n.ID)
				}
				if fanOut.MaxItems < 0 || fanOut.MaxItems > 256 {
					return fmt.Errorf("node %q workflow.fan_out.max_items must be 0 (unlimited) or between 1 and 256", n.ID)
				}
				switch fanOut.Join {
				case "", "all_success", "all_done", "one_success":
				default:
					return fmt.Errorf("node %q workflow.fan_out.join must be all_success, all_done, or one_success", n.ID)
				}
			}
		}
		if n.Internal != nil && n.Internal.Mode != "noop" && n.Internal.Mode != "result" && n.Internal.Mode != "collect" && n.Internal.Mode != "worktree" {
			return fmt.Errorf("node %q has unsupported internal mode %q", n.ID, n.Internal.Mode)
		}
		if n.Internal != nil && n.Internal.Mode == "result" && strings.TrimSpace(n.Internal.ResultFrom) == "" {
			return fmt.Errorf("node %q internal result requires result source", n.ID)
		}
		if n.Internal != nil && n.Internal.Mode == "collect" && len(n.Internal.ResultsFrom) == 0 {
			return fmt.Errorf("node %q internal collect requires result sources", n.ID)
		}
		if n.Internal != nil && n.Internal.Mode == "worktree" && (n.Internal.Worktree == nil || !n.Internal.Worktree.Enabled) {
			return fmt.Errorf("node %q internal worktree action requires an enabled policy", n.ID)
		}
		if n.Script != nil {
			if err := validateScript(*n.Script, "node "+n.ID+".script"); err != nil {
				return err
			}
		}
		if n.Adapter != nil {
			if strings.TrimSpace(n.Adapter.Name) == "" {
				return fmt.Errorf("node %q adapter.name is required", n.ID)
			}
			if strings.TrimSpace(n.Adapter.Operation) == "" {
				return fmt.Errorf("node %q adapter.operation is required", n.ID)
			}
			parts := strings.Split(n.Adapter.Operation, ".")
			for _, part := range parts {
				if part == "" || !regexp.MustCompile(`^[a-z0-9_-]+$`).MatchString(part) {
					return fmt.Errorf("node %q adapter.operation must use lowercase dot-separated segments", n.ID)
				}
			}
		}
		if n.Executor != "" {
			if n.Executor != "external" {
				return fmt.Errorf("node %q executor must be external", n.ID)
			}
			if n.Command == "" && n.Prompt == "" {
				return fmt.Errorf("node %q external executor is supported only for command or prompt nodes", n.ID)
			}
		}
		if n.SideEffect != nil {
			if n.Executor != "external" && n.Adapter == nil {
				return fmt.Errorf("node %q side_effect requires executor: external or adapter node", n.ID)
			}
			switch n.SideEffect.Mode {
			case "idempotent", "reconcile":
			default:
				return fmt.Errorf("node %q side_effect.mode must be idempotent or reconcile", n.ID)
			}
		}
		if n.ToolApproval != nil {
			if n.Command == "" && n.Prompt == "" {
				return fmt.Errorf("node %q tool_approval is supported only for command or prompt nodes", n.ID)
			}
			if n.Executor != "external" {
				return fmt.Errorf("node %q tool_approval currently requires executor: external", n.ID)
			}
			if n.ToolApproval.Mode != "required" {
				return fmt.Errorf("node %q tool_approval.mode must be required", n.ID)
			}
			if err := validateStringSet(n.ToolApproval.Tools, "node "+n.ID+".tool_approval.tools"); err != nil {
				return err
			}
		}
		if n.Attempts.Max < 0 {
			return fmt.Errorf("node %q attempts.max cannot be negative", n.ID)
		}
		if len(n.Attempts.RetryOn) > 0 {
			if n.Attempts.Max < 2 {
				return fmt.Errorf("node %q attempts.retry_on requires attempts.max >= 2", n.ID)
			}
			if len(n.Hooks.OnFailure) > 0 {
				return fmt.Errorf("node %q cannot combine attempts.retry_on with hooks.on_failure", n.ID)
			}
			seenRetryKinds := map[string]bool{}
			for _, kind := range n.Attempts.RetryOn {
				if kind != "exit" && kind != "start" && kind != "protocol" && kind != "internal" && kind != "timed_out" {
					return fmt.Errorf("node %q attempts.retry_on contains unsupported kind %q", n.ID, kind)
				}
				if seenRetryKinds[kind] {
					return fmt.Errorf("node %q attempts.retry_on contains duplicate kind %q", n.ID, kind)
				}
				seenRetryKinds[kind] = true
			}
		}
		if n.Attempts.Backoff != nil {
			if n.Attempts.Max < 2 {
				return fmt.Errorf("node %q attempts.backoff requires attempts.max >= 2", n.ID)
			}
			initial, err := time.ParseDuration(n.Attempts.Backoff.Initial)
			if err != nil || initial <= 0 {
				return fmt.Errorf("node %q attempts.backoff.initial must be a positive duration", n.ID)
			}
			if n.Attempts.Backoff.Multiplier != 0 && n.Attempts.Backoff.Multiplier < 1 {
				return fmt.Errorf("node %q attempts.backoff.multiplier must be >= 1", n.ID)
			}
			if n.Attempts.Backoff.Max != "" {
				maximum, err := time.ParseDuration(n.Attempts.Backoff.Max)
				if err != nil || maximum <= 0 {
					return fmt.Errorf("node %q attempts.backoff.max must be a positive duration", n.ID)
				}
				if maximum < initial {
					return fmt.Errorf("node %q attempts.backoff.max must be >= attempts.backoff.initial", n.ID)
				}
			}
		}
		if n.Attempts.RetrySession != "" && n.Attempts.RetrySession != "fresh" && n.Attempts.RetrySession != "reuse" {
			return fmt.Errorf("node %q attempts.retry_session must be fresh or reuse", n.ID)
		}
		if n.Timeout != "" {
			duration, err := time.ParseDuration(n.Timeout)
			if err != nil || duration <= 0 {
				return fmt.Errorf("node %q has invalid timeout %q", n.ID, n.Timeout)
			}
		}
		if n.IdleTimeout != "" {
			duration, err := time.ParseDuration(n.IdleTimeout)
			if err != nil || duration <= 0 {
				return fmt.Errorf("node %q has invalid idle_timeout %q", n.ID, n.IdleTimeout)
			}
			if n.Command == "" && n.Prompt == "" {
				return fmt.Errorf("node %q idle_timeout is supported only for command or prompt nodes", n.ID)
			}
		}
		if n.AlwaysRun {
			if n.TriggerRule != "" && n.TriggerRule != "all_done" {
				return fmt.Errorf("node %q always_run is incompatible with trigger_rule %q; omit trigger_rule or use all_done", n.ID, n.TriggerRule)
			}
			if n.When != "" {
				return fmt.Errorf("node %q always_run is incompatible with when; use an explicit all_done node without always_run when conditional cleanup is required", n.ID)
			}
		}
		if n.AllowedTools != nil || len(n.DeniedTools) > 0 || n.Skills != nil || n.MCP != "" || len(n.Requires) > 0 {
			if n.Command == "" && n.Prompt == "" {
				return fmt.Errorf("node %q assistant policies are supported only for command or prompt nodes", n.ID)
			}
			if err := validateStringSet(optionalStrings(n.AllowedTools), "node "+n.ID+".allowed_tools"); err != nil {
				return err
			}
			if err := validateStringSet(n.DeniedTools, "node "+n.ID+".denied_tools"); err != nil {
				return err
			}
			if err := validateStringSet(optionalStrings(n.Skills), "node "+n.ID+".skills"); err != nil {
				return err
			}
			if err := validateStringSet(n.Requires, "node "+n.ID+".requires"); err != nil {
				return err
			}
			denied := map[string]bool{}
			for _, tool := range n.DeniedTools {
				denied[tool] = true
			}
			for _, tool := range optionalStrings(n.AllowedTools) {
				if denied[tool] {
					return fmt.Errorf("node %q tool %q appears in both allowed_tools and denied_tools", n.ID, tool)
				}
			}
		}
		if n.Sandbox != nil {
			if n.Sandbox.Filesystem != "" && n.Sandbox.Filesystem != "read_only" {
				return fmt.Errorf("node %q sandbox.filesystem must be read_only", n.ID)
			}
			if n.Sandbox.Network != "" && n.Sandbox.Network != "deny" {
				return fmt.Errorf("node %q sandbox.network must be deny", n.ID)
			}
			if n.Sandbox.Enforcement != "" && n.Sandbox.Enforcement != "required" && n.Sandbox.Enforcement != "optional" {
				return fmt.Errorf("node %q sandbox.enforcement must be required or optional", n.ID)
			}
			if n.Sandbox.Enforcement != "" && n.Bash == "" && n.Script == nil {
				return fmt.Errorf("node %q OS sandbox enforcement is supported only for bash or script nodes", n.ID)
			}
			if n.Sandbox.Enforcement == "" && n.Command == "" && n.Prompt == "" && n.Bash == "" && n.Script == nil {
				return fmt.Errorf("node %q sandbox is supported only for command, prompt, bash, or script nodes", n.ID)
			}
			if n.Sandbox.Filesystem == "read_only" && (n.Command != "" || n.Prompt != "") {
				mutating := map[string]bool{"bash": true, "edit": true, "write": true, "patch": true}
				for _, tool := range optionalStrings(n.AllowedTools) {
					if mutating[tool] {
						return fmt.Errorf("node %q read_only sandbox cannot allow tool %q", n.ID, tool)
					}
				}
			}
		}
		if n.OutputFormat != nil {
			if n.Command == "" && n.Prompt == "" && n.Script == nil && n.Adapter == nil {
				return fmt.Errorf("node %q output_format is supported only for command, prompt, script, or adapter nodes", n.ID)
			}
			if err := validateOutputFormat(*n.OutputFormat, "node "+n.ID+".output_format"); err != nil {
				return err
			}
		}
		if n.OutputType != "" || n.OutputMIME != "" || n.OutputPath != "" {
			if n.Command == "" && n.Prompt == "" && n.Bash == "" && n.Script == nil {
				return fmt.Errorf("node %q typed artifacts are supported only for command, prompt, bash, or script nodes", n.ID)
			}
			if !artifacttype.Valid(n.OutputType) {
				return fmt.Errorf("node %q output_type must match %s", n.ID, artifacttype.Pattern)
			}
			if strings.ContainsAny(n.OutputMIME, "\r\n") {
				return fmt.Errorf("node %q output_mime must be a single line", n.ID)
			}
			if n.OutputMIME != "" && !strings.Contains(n.OutputMIME, "/") {
				return fmt.Errorf("node %q output_mime must be a media type", n.ID)
			}
		}
		if n.Approval != nil && strings.TrimSpace(n.Approval.Message) == "" {
			return fmt.Errorf("approval node %q requires message", n.ID)
		}
		if n.LoopGroup != nil {
			if insideLoop {
				return fmt.Errorf("nested loop_group is not supported in v1alpha1: %s.%s", scope, n.ID)
			}
			if n.LoopGroup.MaxIterations <= 0 {
				return fmt.Errorf("loop_group node %q requires max_iterations > 0", n.ID)
			}
			if err := validateNodes(n.LoopGroup.Nodes, scope+"."+n.ID+".loop_group.nodes", true); err != nil {
				return err
			}
			found := false
			for _, child := range n.LoopGroup.Nodes {
				if child.ID == n.LoopGroup.Until.Node {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("loop_group %q until.node %q does not exist", n.ID, n.LoopGroup.Until.Node)
			}
			if n.LoopGroup.Until.ExitCode == nil && n.LoopGroup.Until.OutputContains == "" {
				return fmt.Errorf("loop_group %q requires until.exit_code or until.output_contains", n.ID)
			}
		}
	}
	for _, n := range nodes {
		if n.LoopGroup != nil {
			for _, child := range n.LoopGroup.Nodes {
				if _, collides := byID[child.ID]; collides {
					return fmt.Errorf("loop_group %q child id %q collides with a node in outer scope", n.ID, child.ID)
				}
			}
		}
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("node %q depends on unknown node %q", n.ID, dep)
			}
		}
		if n.TriggerRule != "" && n.TriggerRule != "all_success" && n.TriggerRule != "all_done" && n.TriggerRule != "none_failed_min_one_success" && n.TriggerRule != "one_success" {
			return fmt.Errorf("node %q has unsupported trigger_rule %q", n.ID, n.TriggerRule)
		}
	}
	for _, n := range nodes {
		if n.WorkflowRun == nil || n.WorkflowRun.FanOut == nil {
			continue
		}
		sourceID, err := fanOutSourceNode(n.WorkflowRun.FanOut.ItemsFrom)
		if err != nil {
			return fmt.Errorf("node %q workflow.fan_out.items_from: %w", n.ID, err)
		}
		if _, ok := byID[sourceID]; !ok {
			return fmt.Errorf("node %q workflow.fan_out references unknown source node %q", n.ID, sourceID)
		}
		if sourceID == n.ID || !nodeDependsOn(n.ID, sourceID, byID, map[string]bool{}) {
			return fmt.Errorf("node %q workflow.fan_out source %q must be an upstream dependency", n.ID, sourceID)
		}
	}

	visiting := map[string]bool{}
	visited := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("dependency cycle detected at node %q", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range byID[id].DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range byID {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

var fanOutNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var fanOutSourceRE = regexp.MustCompile(`^nodes\.([A-Za-z0-9_-]+)\.output(?:\.[A-Za-z0-9_.-]+)?$`)
var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validateScript(script spec.ScriptSpec, scope string) error {
	switch script.Runtime {
	case "command", "python", "node", "go", "validation":
	default:
		return fmt.Errorf("%s.runtime must be command, python, node, go, or validation", scope)
	}
	hasPath := strings.TrimSpace(script.Path) != ""
	hasInline := strings.TrimSpace(script.Inline) != ""
	if script.Runtime == "validation" {
		if hasPath || hasInline || len(script.Args) > 0 || len(script.Dependencies) > 0 {
			return fmt.Errorf("%s runtime validation reads validation_commands from workflow input and does not accept path, inline, args, or dependencies", scope)
		}
	} else {
		if hasPath == hasInline {
			return fmt.Errorf("%s must define exactly one of path or inline", scope)
		}
		if (script.Runtime == "command" || script.Runtime == "go") && hasInline {
			return fmt.Errorf("%s runtime command/go requires path", scope)
		}
	}
	for key := range script.Env {
		if !envNameRE.MatchString(key) {
			return fmt.Errorf("%s.env contains invalid variable name %q", scope, key)
		}
	}
	seen := map[string]bool{}
	for _, dependency := range script.Dependencies {
		if strings.TrimSpace(dependency) == "" {
			return fmt.Errorf("%s.dependencies contains an empty path", scope)
		}
		if seen[dependency] {
			return fmt.Errorf("%s.dependencies contains duplicate path %q", scope, dependency)
		}
		seen[dependency] = true
	}
	return nil
}

func fanOutSourceNode(path string) (string, error) {
	match := fanOutSourceRE.FindStringSubmatch(strings.TrimSpace(path))
	if len(match) != 2 {
		return "", fmt.Errorf("must be nodes.<id>.output or a nested output path")
	}
	return match[1], nil
}

func nodeDependsOn(nodeID, target string, byID map[string]spec.Node, seen map[string]bool) bool {
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	node, ok := byID[nodeID]
	if !ok {
		return false
	}
	for _, dep := range node.DependsOn {
		if dep == target || nodeDependsOn(dep, target, byID, seen) {
			return true
		}
	}
	return false
}

func validateOutputFormat(format spec.OutputFormat, path string) error {
	switch format.Type {
	case "object":
		if format.MinProperties < 0 || format.MaxProperties < 0 {
			return fmt.Errorf("%s minProperties/maxProperties must not be negative", path)
		}
		if format.MaxProperties > 0 && format.MinProperties > format.MaxProperties {
			return fmt.Errorf("%s minProperties must not exceed maxProperties", path)
		}
		for _, name := range format.Required {
			if _, ok := format.Properties[name]; !ok {
				return fmt.Errorf("%s requires unknown property %q", path, name)
			}
		}
		for name, child := range format.Properties {
			if err := validateOutputFormat(child, path+".properties."+name); err != nil {
				return err
			}
		}
	case "array":
		if format.Items == nil {
			return fmt.Errorf("%s array requires items", path)
		}
		if format.MinItems < 0 || format.MaxItems < 0 {
			return fmt.Errorf("%s minItems/maxItems must not be negative", path)
		}
		if format.MaxItems > 0 && format.MinItems > format.MaxItems {
			return fmt.Errorf("%s minItems must not exceed maxItems", path)
		}
		if err := validateOutputFormat(*format.Items, path+".items"); err != nil {
			return err
		}
	case "string":
		if format.MinLength < 0 || format.MaxLength < 0 {
			return fmt.Errorf("%s minLength/maxLength must not be negative", path)
		}
		if format.MaxLength > 0 && format.MinLength > format.MaxLength {
			return fmt.Errorf("%s minLength must not exceed maxLength", path)
		}
		if format.Pattern != "" {
			if _, err := regexp.Compile(format.Pattern); err != nil {
				return fmt.Errorf("%s pattern is invalid: %w", path, err)
			}
		}
	case "number", "integer":
		if format.Minimum != nil && format.Maximum != nil && *format.Minimum > *format.Maximum {
			return fmt.Errorf("%s minimum must not exceed maximum", path)
		}
	case "boolean":
	default:
		return fmt.Errorf("%s has unsupported type %q", path, format.Type)
	}
	if format.Type != "array" && (format.MinItems != 0 || format.MaxItems != 0 || format.UniqueItems || format.Items != nil) {
		return fmt.Errorf("%s array constraints require type array", path)
	}
	if format.Type != "string" && (format.MinLength != 0 || format.MaxLength != 0 || format.Pattern != "") {
		return fmt.Errorf("%s string constraints require type string", path)
	}
	if format.Type != "number" && format.Type != "integer" && (format.Minimum != nil || format.Maximum != nil) {
		return fmt.Errorf("%s numeric constraints require type number or integer", path)
	}
	if format.Type != "object" && (format.MinProperties != 0 || format.MaxProperties != 0 || len(format.Properties) > 0 || len(format.Required) > 0 || format.AdditionalProperties != nil) {
		return fmt.Errorf("%s object constraints require type object", path)
	}
	if len(format.Enum) > 0 && format.Type != "string" {
		return fmt.Errorf("%s enum is supported only for string", path)
	}
	return nil
}

func optionalStrings(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func validateStringSet(values []string, path string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s contains an empty value", path)
		}
		if seen[value] {
			return fmt.Errorf("%s contains duplicate value %q", path, value)
		}
		seen[value] = true
	}
	return nil
}

func validatePolicySpec(value spec.PolicySpec, path string) error {
	if err := validateStringSet(optionalStrings(value.AllowedTools), path+".allowed_tools"); err != nil {
		return err
	}
	if err := validateStringSet(value.DeniedTools, path+".denied_tools"); err != nil {
		return err
	}
	if err := validateStringSet(optionalStrings(value.Skills), path+".skills"); err != nil {
		return err
	}
	if err := validateStringSet(value.Requires, path+".requires"); err != nil {
		return err
	}
	denied := map[string]bool{}
	for _, tool := range value.DeniedTools {
		denied[tool] = true
	}
	for _, tool := range optionalStrings(value.AllowedTools) {
		if denied[tool] {
			return fmt.Errorf("%s tool %q appears in both allowed_tools and denied_tools", path, tool)
		}
	}
	if value.Sandbox != nil {
		if value.Sandbox.Enforcement != "" {
			return fmt.Errorf("%s sandbox.enforcement is not supported in assistant policy; use node-level bash/script sandbox enforcement", path)
		}
		if value.Sandbox.Filesystem != "" && value.Sandbox.Filesystem != "read_only" {
			return fmt.Errorf("%s sandbox.filesystem must be read_only", path)
		}
		if value.Sandbox.Network != "" && value.Sandbox.Network != "deny" {
			return fmt.Errorf("%s sandbox.network must be deny", path)
		}
		if value.Sandbox.Filesystem == "read_only" {
			mutating := map[string]bool{"bash": true, "edit": true, "write": true, "patch": true}
			for _, tool := range optionalStrings(value.AllowedTools) {
				if mutating[tool] {
					return fmt.Errorf("%s read_only sandbox cannot allow tool %q", path, tool)
				}
			}
		}
	}
	return nil
}
