package workflow

import (
	"fmt"
	"strings"

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
	return validateNodes(wf.Nodes, "nodes")
}

func validateNodes(nodes []spec.Node, scope string) error {
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
		if kinds != 1 {
			return fmt.Errorf("node %q must define exactly one of command, prompt, bash, approval, loop_group", n.ID)
		}
		if n.Attempts.Max < 0 {
			return fmt.Errorf("node %q attempts.max cannot be negative", n.ID)
		}
		if n.Approval != nil && strings.TrimSpace(n.Approval.Message) == "" {
			return fmt.Errorf("approval node %q requires message", n.ID)
		}
		if n.LoopGroup != nil {
			if n.LoopGroup.MaxIterations <= 0 {
				return fmt.Errorf("loop_group node %q requires max_iterations > 0", n.ID)
			}
			if err := validateNodes(n.LoopGroup.Nodes, scope+"."+n.ID+".loop_group.nodes"); err != nil {
				return err
			}
			found := false
			for _, child := range n.LoopGroup.Nodes {
				if child.ID == n.LoopGroup.Until.Node {
					found = true
				}
				if child.Approval != nil {
					return fmt.Errorf("approval inside loop_group is not supported in v1alpha1 implementation: %s.%s", n.ID, child.ID)
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
		if n.TriggerRule != "" && n.TriggerRule != "all_success" && n.TriggerRule != "all_done" && n.TriggerRule != "none_failed_min_one_success" {
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
