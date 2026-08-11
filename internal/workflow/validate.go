package workflow

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"takt/internal/artifacttype"
	"takt/internal/schemasubset"
	"takt/internal/spec"
	"takt/internal/whenexpr"
)

func Validate(wf *spec.Workflow) error {
	if wf == nil {
		return fmt.Errorf("workflow is required")
	}
	name := strings.TrimSpace(wf.Name)
	if name == "" {
		return fmt.Errorf("name is required")
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
			if err := schemasubset.ValidateDefinition(*wf.Input.Schema, "input.schema"); err != nil {
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
	byID, err := indexNodes(nodes, scope)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := validateNode(node, scope, insideLoop); err != nil {
			return err
		}
	}
	if err := validateLoopCollisions(nodes, byID); err != nil {
		return err
	}
	if err := validateDependencies(nodes, byID); err != nil {
		return err
	}
	if err := validateFanOutDependencies(nodes, byID); err != nil {
		return err
	}
	if err := validateSharedContexts(nodes, byID); err != nil {
		return err
	}
	return validateAcyclic(byID)
}

func validateSharedContexts(nodes []spec.Node, byID map[string]spec.Node) error {
	consumers := map[string][]string{}
	for _, node := range nodes {
		if node.Context != "shared" {
			continue
		}
		source, err := nearestSharedSource(node, byID)
		if err != nil {
			return err
		}
		consumers[source] = append(consumers[source], node.ID)
	}
	for source, ids := range consumers {
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				if !nodeDependsOn(ids[i], ids[j], byID, map[string]bool{}) && !nodeDependsOn(ids[j], ids[i], byID, map[string]bool{}) {
					return fmt.Errorf("shared session source %q is consumed concurrently by nodes %q and %q", source, ids[i], ids[j])
				}
			}
		}
	}
	return nil
}

func nearestSharedSource(node spec.Node, byID map[string]spec.Node) (string, error) {
	ancestors := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if ancestors[id] {
			return
		}
		candidate, ok := byID[id]
		if !ok {
			return
		}
		ancestors[id] = true
		for _, dep := range candidate.DependsOn {
			visit(dep)
		}
	}
	for _, dep := range node.DependsOn {
		visit(dep)
	}
	provider := node.Provider
	candidates := make([]string, 0, len(ancestors))
	for id := range ancestors {
		candidate := byID[id]
		if candidate.Command == "" && candidate.Prompt == "" {
			continue
		}
		candidateProvider := candidate.Provider
		if provider != "" && candidateProvider != "" && provider != candidateProvider {
			continue
		}
		if node.Model != "" && candidate.Model != "" && node.Model != candidate.Model {
			continue
		}
		candidates = append(candidates, id)
	}
	nearest := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		shadowed := false
		for _, other := range candidates {
			if candidate == other {
				continue
			}
			if nodeDependsOn(other, candidate, byID, map[string]bool{}) {
				shadowed = true
				break
			}
		}
		if !shadowed {
			nearest = append(nearest, candidate)
		}
	}
	if len(nearest) == 0 {
		return "", fmt.Errorf("node %q context: shared requires one explicit upstream assistant ancestor", node.ID)
	}
	if len(nearest) > 1 {
		return "", fmt.Errorf("node %q context: shared has ambiguous upstream assistant ancestors", node.ID)
	}
	return nearest[0], nil
}

func indexNodes(nodes []spec.Node, scope string) (map[string]spec.Node, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s must not be empty", scope)
	}
	byID := make(map[string]spec.Node, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			return nil, fmt.Errorf("%s contains node without id", scope)
		}
		if !nodeIDRE.MatchString(node.ID) {
			return nil, fmt.Errorf("%s contains invalid node id %q", scope, node.ID)
		}
		if reservedNodeIDs[node.ID] {
			return nil, fmt.Errorf("%s contains reserved node id %q", scope, node.ID)
		}
		if _, exists := byID[node.ID]; exists {
			return nil, fmt.Errorf("duplicate node id %q in %s", node.ID, scope)
		}
		byID[node.ID] = node
	}
	return byID, nil
}

func validateNode(node spec.Node, scope string, insideLoop bool) error {
	checks := []func(spec.Node) error{
		validateActionShape,
		validateWorkflowAction,
		validateInternalAction,
		validateScriptAndAdapter,
		validateExternalExecution,
		validateAttempts,
		validateTiming,
		validateWhen,
		validateAssistantPolicy,
		validateSandbox,
		validateOutputs,
		validateApproval,
		validateArchonA0Fields,
	}
	for _, check := range checks {
		if err := check(node); err != nil {
			return err
		}
	}
	return validateLoop(node, scope, insideLoop)
}

var nodeIDRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

var reservedNodeIDs = map[string]bool{
	"ARGUMENTS": true, "ARTIFACTS_DIR": true, "BASE_BRANCH": true,
	"INPUTS": true, "LOOP_PREV": true, "FEEDBACK": true, "FANOUT": true,
}

func validateArchonA0Fields(node spec.Node) error {
	if node.Context != "" && node.Context != "fresh" && node.Context != "shared" {
		return fmt.Errorf("node %q context must be fresh or shared", node.ID)
	}
	return nil
}

func validateActionShape(node spec.Node) error {
	kinds := 0
	for _, present := range []bool{
		node.Command != "", node.Prompt != "", node.Bash != "", node.Script != nil,
		node.Approval != nil, node.LoopGroup != nil, node.Loop != nil, node.Cancel != "", node.Subworkflow != nil,
		node.Foreach != nil, node.WorkflowRun != nil, node.Internal != nil, node.Adapter != nil,
	} {
		if present {
			kinds++
		}
	}
	if kinds != 1 {
		return fmt.Errorf("node %q must define exactly one action", node.ID)
	}
	if node.Subworkflow != nil || node.Foreach != nil {
		return fmt.Errorf("node %q contains an unexpanded workflow container", node.ID)
	}
	return nil
}

func validateWorkflowAction(node spec.Node) error {
	value := node.WorkflowRun
	if value == nil {
		return nil
	}
	if strings.TrimSpace(value.Path) == "" {
		return fmt.Errorf("node %q workflow.path is required", node.ID)
	}
	if value.Policy != nil {
		if err := validatePolicySpec(*value.Policy, "node "+node.ID+".workflow.policy"); err != nil {
			return err
		}
	}
	switch value.Isolation {
	case "", "inherit", "worktree", "none":
	default:
		return fmt.Errorf("node %q workflow.isolation must be inherit, worktree, or none", node.ID)
	}
	if strings.TrimSpace(value.Repository) != "" {
		if filepath.IsAbs(value.Repository) {
			return fmt.Errorf("node %q workflow.repository must be relative to the control workspace", node.ID)
		}
		if value.Isolation == "inherit" {
			return fmt.Errorf("node %q workflow.repository cannot use isolation inherit", node.ID)
		}
		if value.FanOut != nil {
			return fmt.Errorf("node %q workflow.repository does not support fan_out in v0.1.43", node.ID)
		}
	}
	if value.FanOut == nil {
		return nil
	}
	fanOut := value.FanOut
	if _, err := fanOutSourceNode(fanOut.ItemsFrom); err != nil {
		return fmt.Errorf("node %q workflow.fan_out.items_from: %w", node.ID, err)
	}
	if fanOut.As != "" && !fanOutNameRE.MatchString(fanOut.As) {
		return fmt.Errorf("node %q workflow.fan_out.as must be an identifier", node.ID)
	}
	if fanOut.MaxParallel < 0 || fanOut.MaxParallel > 64 {
		return fmt.Errorf("node %q workflow.fan_out.max_parallel must be 0 (default 1) or between 1 and 64", node.ID)
	}
	if fanOut.MaxItems < 0 || fanOut.MaxItems > 256 {
		return fmt.Errorf("node %q workflow.fan_out.max_items must be 0 (unlimited) or between 1 and 256", node.ID)
	}
	switch fanOut.Join {
	case "", "all_success", "all_done", "one_success":
		return nil
	default:
		return fmt.Errorf("node %q workflow.fan_out.join must be all_success, all_done, or one_success", node.ID)
	}
}

func validateInternalAction(node spec.Node) error {
	value := node.Internal
	if value == nil {
		return nil
	}
	if value.Mode != "noop" && value.Mode != "result" && value.Mode != "collect" && value.Mode != "worktree" {
		return fmt.Errorf("node %q has unsupported internal mode %q", node.ID, value.Mode)
	}
	if value.Mode == "result" && strings.TrimSpace(value.ResultFrom) == "" {
		return fmt.Errorf("node %q internal result requires result source", node.ID)
	}
	if value.Mode == "collect" && len(value.ResultsFrom) == 0 {
		return fmt.Errorf("node %q internal collect requires result sources", node.ID)
	}
	if value.Mode == "worktree" && (value.Worktree == nil || !value.Worktree.Enabled) {
		return fmt.Errorf("node %q internal worktree action requires an enabled policy", node.ID)
	}
	return nil
}

var adapterOperationSegmentRE = regexp.MustCompile(`^[a-z0-9_-]+$`)

func validateScriptAndAdapter(node spec.Node) error {
	if node.Script != nil {
		if err := validateScript(*node.Script, "node "+node.ID+".script"); err != nil {
			return err
		}
	}
	if node.Adapter == nil {
		return nil
	}
	if strings.TrimSpace(node.Adapter.Name) == "" {
		return fmt.Errorf("node %q adapter.name is required", node.ID)
	}
	if strings.TrimSpace(node.Adapter.Operation) == "" {
		return fmt.Errorf("node %q adapter.operation is required", node.ID)
	}
	for _, part := range strings.Split(node.Adapter.Operation, ".") {
		if part == "" || !adapterOperationSegmentRE.MatchString(part) {
			return fmt.Errorf("node %q adapter.operation must use lowercase dot-separated segments", node.ID)
		}
	}
	return nil
}

func validateExternalExecution(node spec.Node) error {
	if node.Executor != "" {
		if node.Executor != "external" {
			return fmt.Errorf("node %q executor must be external", node.ID)
		}
		if node.Command == "" && node.Prompt == "" {
			return fmt.Errorf("node %q external executor is supported only for command or prompt nodes", node.ID)
		}
	}
	if node.SideEffect != nil {
		if node.Executor != "external" && node.Adapter == nil {
			return fmt.Errorf("node %q side_effect requires executor: external or adapter node", node.ID)
		}
		switch node.SideEffect.Mode {
		case "idempotent", "reconcile":
		default:
			return fmt.Errorf("node %q side_effect.mode must be idempotent or reconcile", node.ID)
		}
	}
	if node.ToolApproval == nil {
		return nil
	}
	if node.Command == "" && node.Prompt == "" {
		return fmt.Errorf("node %q tool_approval is supported only for command or prompt nodes", node.ID)
	}
	if node.Executor != "external" {
		return fmt.Errorf("node %q tool_approval currently requires executor: external", node.ID)
	}
	if node.ToolApproval.Mode != "required" {
		return fmt.Errorf("node %q tool_approval.mode must be required", node.ID)
	}
	return validateStringSet(node.ToolApproval.Tools, "node "+node.ID+".tool_approval.tools")
}

func validateAttempts(node spec.Node) error {
	if node.Attempts.Max < 0 {
		return fmt.Errorf("node %q attempts.max cannot be negative", node.ID)
	}
	if len(node.Attempts.RetryOn) > 0 {
		if node.Attempts.Max < 2 {
			return fmt.Errorf("node %q attempts.retry_on requires attempts.max >= 2", node.ID)
		}
		if len(node.Hooks.OnFailure) > 0 {
			return fmt.Errorf("node %q cannot combine attempts.retry_on with hooks.on_failure", node.ID)
		}
		seen := map[string]bool{}
		for _, kind := range node.Attempts.RetryOn {
			if kind != "exit" && kind != "start" && kind != "protocol" && kind != "internal" && kind != "timed_out" {
				return fmt.Errorf("node %q attempts.retry_on contains unsupported kind %q", node.ID, kind)
			}
			if seen[kind] {
				return fmt.Errorf("node %q attempts.retry_on contains duplicate kind %q", node.ID, kind)
			}
			seen[kind] = true
		}
	}
	if node.Attempts.Backoff != nil {
		if node.Attempts.Max < 2 {
			return fmt.Errorf("node %q attempts.backoff requires attempts.max >= 2", node.ID)
		}
		initial, err := time.ParseDuration(node.Attempts.Backoff.Initial)
		if err != nil || initial <= 0 {
			return fmt.Errorf("node %q attempts.backoff.initial must be a positive duration", node.ID)
		}
		if node.Attempts.Backoff.Multiplier != 0 && node.Attempts.Backoff.Multiplier < 1 {
			return fmt.Errorf("node %q attempts.backoff.multiplier must be >= 1", node.ID)
		}
		if node.Attempts.Backoff.Max != "" {
			maximum, err := time.ParseDuration(node.Attempts.Backoff.Max)
			if err != nil || maximum <= 0 {
				return fmt.Errorf("node %q attempts.backoff.max must be a positive duration", node.ID)
			}
			if maximum < initial {
				return fmt.Errorf("node %q attempts.backoff.max must be >= attempts.backoff.initial", node.ID)
			}
		}
	}
	if node.Attempts.RetrySession != "" && node.Attempts.RetrySession != "fresh" && node.Attempts.RetrySession != "reuse" {
		return fmt.Errorf("node %q attempts.retry_session must be fresh or reuse", node.ID)
	}
	return nil
}

func validateTiming(node spec.Node) error {
	if node.Timeout != "" {
		duration, err := time.ParseDuration(node.Timeout)
		if err != nil || duration <= 0 {
			return fmt.Errorf("node %q has invalid timeout %q", node.ID, node.Timeout)
		}
	}
	if node.IdleTimeout != "" {
		duration, err := time.ParseDuration(node.IdleTimeout)
		if err != nil || duration <= 0 {
			return fmt.Errorf("node %q has invalid idle_timeout %q", node.ID, node.IdleTimeout)
		}
		if node.Command == "" && node.Prompt == "" {
			return fmt.Errorf("node %q idle_timeout is supported only for command or prompt nodes", node.ID)
		}
	}
	if node.AlwaysRun {
		if node.TriggerRule != "" && node.TriggerRule != "all_done" {
			return fmt.Errorf("node %q always_run is incompatible with trigger_rule %q; omit trigger_rule or use all_done", node.ID, node.TriggerRule)
		}
		if node.When != "" {
			return fmt.Errorf("node %q always_run is incompatible with when; use an explicit all_done node without always_run when conditional cleanup is required", node.ID)
		}
	}
	return nil
}

func validateWhen(node spec.Node) error {
	if node.When == "" {
		return nil
	}
	if err := whenexpr.Validate(node.When); err != nil {
		return fmt.Errorf("node %q when: %w", node.ID, err)
	}
	return nil
}

func validateAssistantPolicy(node spec.Node) error {
	if node.AllowedTools == nil && len(node.DeniedTools) == 0 && node.Skills == nil && node.MCP == "" && len(node.Requires) == 0 {
		return nil
	}
	if node.Command == "" && node.Prompt == "" {
		return fmt.Errorf("node %q assistant policies are supported only for command or prompt nodes", node.ID)
	}
	sets := []struct {
		values []string
		name   string
	}{
		{optionalStrings(node.AllowedTools), "allowed_tools"},
		{node.DeniedTools, "denied_tools"},
		{optionalStrings(node.Skills), "skills"},
		{node.Requires, "requires"},
	}
	for _, set := range sets {
		if err := validateStringSet(set.values, "node "+node.ID+"."+set.name); err != nil {
			return err
		}
	}
	denied := map[string]bool{}
	for _, tool := range node.DeniedTools {
		denied[tool] = true
	}
	for _, tool := range optionalStrings(node.AllowedTools) {
		if denied[tool] {
			return fmt.Errorf("node %q tool %q appears in both allowed_tools and denied_tools", node.ID, tool)
		}
	}
	return nil
}

func validateSandbox(node spec.Node) error {
	value := node.Sandbox
	if value == nil {
		return nil
	}
	if value.Filesystem != "" && value.Filesystem != "read_only" {
		return fmt.Errorf("node %q sandbox.filesystem must be read_only", node.ID)
	}
	if value.Network != "" && value.Network != "deny" {
		return fmt.Errorf("node %q sandbox.network must be deny", node.ID)
	}
	if value.Enforcement != "" && value.Enforcement != "required" && value.Enforcement != "optional" {
		return fmt.Errorf("node %q sandbox.enforcement must be required or optional", node.ID)
	}
	if value.Enforcement != "" && node.Bash == "" && node.Script == nil {
		return fmt.Errorf("node %q OS sandbox enforcement is supported only for bash or script nodes", node.ID)
	}
	if value.Enforcement == "" && node.Command == "" && node.Prompt == "" && node.Bash == "" && node.Script == nil {
		return fmt.Errorf("node %q sandbox is supported only for command, prompt, bash, or script nodes", node.ID)
	}
	if value.Filesystem == "read_only" && (node.Command != "" || node.Prompt != "") {
		mutating := map[string]bool{"bash": true, "edit": true, "write": true, "patch": true}
		for _, tool := range optionalStrings(node.AllowedTools) {
			if mutating[tool] {
				return fmt.Errorf("node %q read_only sandbox cannot allow tool %q", node.ID, tool)
			}
		}
	}
	return nil
}

func validateOutputs(node spec.Node) error {
	if node.OutputFormat != nil {
		if node.Command == "" && node.Prompt == "" && node.Script == nil && node.Adapter == nil {
			return fmt.Errorf("node %q output_format is supported only for command, prompt, script, or adapter nodes", node.ID)
		}
		if err := schemasubset.ValidateDefinition(*node.OutputFormat, "node "+node.ID+".output_format"); err != nil {
			return err
		}
	}
	if node.OutputType == "" && node.OutputMIME == "" && node.OutputPath == "" {
		return nil
	}
	if node.Command == "" && node.Prompt == "" && node.Bash == "" && node.Script == nil {
		return fmt.Errorf("node %q typed artifacts are supported only for command, prompt, bash, or script nodes", node.ID)
	}
	if !artifacttype.Valid(node.OutputType) {
		return fmt.Errorf("node %q output_type must match %s", node.ID, artifacttype.Pattern)
	}
	if strings.ContainsAny(node.OutputMIME, "\r\n") {
		return fmt.Errorf("node %q output_mime must be a single line", node.ID)
	}
	if node.OutputMIME != "" && !strings.Contains(node.OutputMIME, "/") {
		return fmt.Errorf("node %q output_mime must be a media type", node.ID)
	}
	return nil
}

func validateApproval(node spec.Node) error {
	if node.Approval != nil && strings.TrimSpace(node.Approval.Message) == "" {
		return fmt.Errorf("approval node %q requires message", node.ID)
	}
	return nil
}

func validateLoop(node spec.Node, scope string, insideLoop bool) error {
	value := node.LoopGroup
	if value == nil {
		return nil
	}
	if insideLoop {
		return fmt.Errorf("nested loop_group is not supported in v1alpha1: %s.%s", scope, node.ID)
	}
	if value.MaxIterations <= 0 {
		return fmt.Errorf("loop_group node %q requires max_iterations > 0", node.ID)
	}
	if value.MaxIterations > spec.MaxLoopGroupIterations {
		return fmt.Errorf("loop_group node %q max_iterations must be <= %d", node.ID, spec.MaxLoopGroupIterations)
	}
	if value.Until.Node == "" {
		return fmt.Errorf("loop_group %q until.node is required; scalar until needs one terminal node", node.ID)
	}
	if value.Until.Signal != "" && !signalRE.MatchString(value.Until.Signal) {
		return fmt.Errorf("loop_group %q until.signal must match %s", node.ID, signalRE.String())
	}
	if len(value.Until.Requires) > 64 {
		return fmt.Errorf("loop_group %q until.requires must contain at most 64 entries", node.ID)
	}
	body := make(map[string]spec.Node, len(value.Nodes))
	for _, child := range value.Nodes {
		body[child.ID] = child
	}
	seenRequirements := map[string]bool{}
	for _, requirement := range value.Until.Requires {
		if requirement.Node == "" || seenRequirements[requirement.Node] {
			return fmt.Errorf("loop_group %q until.requires contains duplicate or empty node", node.ID)
		}
		seenRequirements[requirement.Node] = true
		if _, ok := body[requirement.Node]; !ok {
			return fmt.Errorf("loop_group %q until.requires references node %q outside body", node.ID, requirement.Node)
		}
		if requirement.ExitCode == nil && requirement.OutputContains == "" {
			return fmt.Errorf("loop_group %q until.requires[%q] needs exit_code or output_contains", node.ID, requirement.Node)
		}
		if requirement.Node == value.Until.Node {
			return fmt.Errorf("loop_group %q until.requires repeats primary node %q", node.ID, requirement.Node)
		}
	}
	if err := validateNodes(value.Nodes, scope+"."+node.ID+".loop_group.nodes", true); err != nil {
		return err
	}
	found := false
	for _, child := range value.Nodes {
		if child.ID == value.Until.Node {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("loop_group %q until.node %q does not exist", node.ID, value.Until.Node)
	}
	if value.Until.ExitCode == nil && value.Until.OutputContains == "" && value.Until.Signal == "" {
		return fmt.Errorf("loop_group %q requires until.exit_code or until.output_contains", node.ID)
	}
	return nil
}

var signalRE = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,63}$`)

func validateLoopCollisions(nodes []spec.Node, byID map[string]spec.Node) error {
	for _, node := range nodes {
		if node.LoopGroup == nil {
			continue
		}
		for _, child := range node.LoopGroup.Nodes {
			if _, collides := byID[child.ID]; collides {
				return fmt.Errorf("loop_group %q child id %q collides with a node in outer scope", node.ID, child.ID)
			}
		}
	}
	return nil
}

func validateDependencies(nodes []spec.Node, byID map[string]spec.Node) error {
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if _, ok := byID[dep]; !ok {
				return fmt.Errorf("node %q depends on unknown node %q", node.ID, dep)
			}
		}
		switch node.TriggerRule {
		case "", "all_success", "all_done", "none_failed_min_one_success", "one_success":
		default:
			return fmt.Errorf("node %q has unsupported trigger_rule %q", node.ID, node.TriggerRule)
		}
	}
	return nil
}

func validateFanOutDependencies(nodes []spec.Node, byID map[string]spec.Node) error {
	for _, node := range nodes {
		if node.WorkflowRun == nil || node.WorkflowRun.FanOut == nil {
			continue
		}
		sourceID, err := fanOutSourceNode(node.WorkflowRun.FanOut.ItemsFrom)
		if err != nil {
			return fmt.Errorf("node %q workflow.fan_out.items_from: %w", node.ID, err)
		}
		if _, ok := byID[sourceID]; !ok {
			return fmt.Errorf("node %q workflow.fan_out references unknown source node %q", node.ID, sourceID)
		}
		if sourceID == node.ID || !nodeDependsOn(node.ID, sourceID, byID, map[string]bool{}) {
			return fmt.Errorf("node %q workflow.fan_out source %q must be an upstream dependency", node.ID, sourceID)
		}
	}
	return nil
}

func validateAcyclic(byID map[string]spec.Node) error {
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
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "$") {
		parts := strings.Split(path, ".")
		if len(parts) >= 2 && parts[1] == "output" && nodeIDRE.MatchString(strings.TrimPrefix(parts[0], "$")) {
			return strings.TrimPrefix(parts[0], "$"), nil
		}
		return "", fmt.Errorf("must be $<id>.output or a nested output path")
	}
	match := fanOutSourceRE.FindStringSubmatch(path)
	if len(match) != 2 {
		return "", fmt.Errorf("must be $<id>.output or a nested output path")
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
