package workflow

import (
	"fmt"
	"strings"
	"time"

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
	return validateNodes(wf.Nodes, "nodes", false)
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
		if n.Internal != nil {
			kinds++
		}
		if kinds != 1 {
			return fmt.Errorf("node %q must define exactly one action", n.ID)
		}
		if n.Subworkflow != nil || n.Foreach != nil {
			return fmt.Errorf("node %q contains an unexpanded workflow container", n.ID)
		}
		if n.Internal != nil && n.Internal.Mode != "noop" && n.Internal.Mode != "result" && n.Internal.Mode != "collect" {
			return fmt.Errorf("node %q has unsupported internal mode %q", n.ID, n.Internal.Mode)
		}
		if n.Internal != nil && n.Internal.Mode == "result" && strings.TrimSpace(n.Internal.ResultFrom) == "" {
			return fmt.Errorf("node %q internal result requires result source", n.ID)
		}
		if n.Internal != nil && n.Internal.Mode == "collect" && len(n.Internal.ResultsFrom) == 0 {
			return fmt.Errorf("node %q internal collect requires result sources", n.ID)
		}
		if n.Attempts.Max < 0 {
			return fmt.Errorf("node %q attempts.max cannot be negative", n.ID)
		}
		if n.Timeout != "" {
			duration, err := time.ParseDuration(n.Timeout)
			if err != nil || duration <= 0 {
				return fmt.Errorf("node %q has invalid timeout %q", n.ID, n.Timeout)
			}
		}
		if n.OutputFormat != nil {
			if n.Command == "" && n.Prompt == "" {
				return fmt.Errorf("node %q output_format is supported only for command or prompt nodes", n.ID)
			}
			if err := validateOutputFormat(*n.OutputFormat, "node "+n.ID+".output_format"); err != nil {
				return err
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

func validateOutputFormat(format spec.OutputFormat, path string) error {
	switch format.Type {
	case "object":
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
		if err := validateOutputFormat(*format.Items, path+".items"); err != nil {
			return err
		}
	case "string", "boolean", "number", "integer":
	default:
		return fmt.Errorf("%s has unsupported type %q", path, format.Type)
	}
	if len(format.Enum) > 0 && format.Type != "string" {
		return fmt.Errorf("%s enum is supported only for string", path)
	}
	return nil
}
