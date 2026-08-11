package workflow

import (
	"fmt"
	"path/filepath"
	"sort"

	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/spec"
)

// ValidateReferences resolves commands, assistants, models and governed child
// workflow outputs after the structural workflow has been loaded and expanded.
func ValidateReferences(wf *spec.Workflow, cfg *spec.Config, resolver command.Resolver) error {
	if wf == nil {
		return fmt.Errorf("workflow is required")
	}
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	defaults := wf.Defaults
	if defaults.Assistant == "" {
		defaults.Assistant = wf.Provider
	}
	if defaults.Model == "" {
		defaults.Model = wf.Model
	}
	return validateReferencesRecursive(wf.Nodes, defaults, cfg, resolver, map[string]bool{}, 0)
}

func validateReferencesRecursive(nodes []spec.Node, defaults spec.Defaults, cfg *spec.Config, resolver command.Resolver, stack map[string]bool, depth int) error {
	if depth > maxGovernedWorkflowDepth {
		return fmt.Errorf("governed child workflow validation exceeds depth %d", maxGovernedWorkflowDepth)
	}
	for _, n := range nodes {
		assistantName, modelName := n.Assistant, n.Model
		if assistantName == "" {
			assistantName = n.Provider
		}
		if n.Command != "" {
			cmd, err := resolver.Resolve(n.Command)
			if err != nil {
				return fmt.Errorf("node %q: %w", n.ID, err)
			}
			if assistantName == "" {
				assistantName = cmd.Provider
				if assistantName == "" {
					assistantName = cmd.Assistant
				}
			}
			if modelName == "" {
				modelName = cmd.Model
			}
		}
		if n.Command != "" || n.Prompt != "" {
			if assistantName == "" {
				assistantName = defaults.Assistant
			}
			if modelName == "" {
				modelName = defaults.Model
			}
			if !assistantConfigured(cfg, assistantName) {
				return fmt.Errorf("node %q references unknown assistant %q", n.ID, assistantName)
			}
			if _, ok := cfg.Models[modelName]; !ok {
				return fmt.Errorf("node %q references unknown model %q", n.ID, modelName)
			}
		}
		if n.Adapter != nil {
			adapter, ok := cfg.Adapters[n.Adapter.Name]
			if !ok {
				return fmt.Errorf("node %q references unknown domain adapter %q", n.ID, n.Adapter.Name)
			}
			if adapter.Transport == "mcp" && len(adapter.Operations) > 0 {
				if _, ok := adapter.Operations[n.Adapter.Operation]; !ok {
					return fmt.Errorf("node %q operation %q is not mapped by MCP adapter %q", n.ID, n.Adapter.Operation, n.Adapter.Name)
				}
			}
		}
		if n.LoopGroup != nil {
			if err := validateReferencesRecursive(n.LoopGroup.Nodes, defaults, cfg, resolver, stack, depth); err != nil {
				return fmt.Errorf("loop_group %q: %w", n.ID, err)
			}
		}
		if n.WorkflowRun == nil {
			continue
		}
		path := n.WorkflowRun.Path
		if !filepath.IsAbs(path) {
			return fmt.Errorf("node %q child workflow path was not resolved: %s", n.ID, path)
		}
		path = filepath.Clean(path)
		if stack[path] {
			return fmt.Errorf("recursive governed child workflow reference at %s", path)
		}
		child, err := Load(path)
		if err != nil {
			return fmt.Errorf("node %q child workflow: %w", n.ID, err)
		}
		if n.WorkflowRun.OutputNode != "" {
			found := false
			for _, childNode := range child.Nodes {
				if childNode.ID == n.WorkflowRun.OutputNode && !childNode.Hidden && childNode.PublicParent == "" {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("node %q child output_node %q does not exist", n.ID, n.WorkflowRun.OutputNode)
			}
		} else if terminals := PublicTerminalIDs(child.Nodes); len(terminals) != 1 {
			return fmt.Errorf("node %q child workflow %q has %d terminal nodes; set output_node", n.ID, child.Metadata.Name, len(terminals))
		}
		childResolver := resolver
		childResolver.Dirs = append(CommandDirsForDefinition(path), childResolver.Dirs...)
		stack[path] = true
		err = validateReferencesRecursive(child.Nodes, child.Defaults, cfg, childResolver, stack, depth+1)
		delete(stack, path)
		if err != nil {
			return fmt.Errorf("node %q child workflow %q: %w", n.ID, child.Metadata.Name, err)
		}
	}
	return nil
}

func assistantConfigured(cfg *spec.Config, name string) bool {
	_, _, ok := assistant.ResolveConfigured(name, cfg)
	return ok
}

// CommandDirsForDefinition returns command directories beside a workflow and
// its ancestors. Installed profiles keep workflows under workflows/ while their
// shared commands live at the profile root, so governed child validation must
// follow the same lookup model as runtime execution.
func CommandDirsForDefinition(path string) []string {
	dir := filepath.Dir(filepath.Clean(path))
	seen := map[string]bool{}
	var dirs []string
	for {
		commandDir := filepath.Join(dir, "commands")
		if !seen[commandDir] {
			dirs = append(dirs, commandDir)
			seen[commandDir] = true
		}
		if filepath.Base(dir) == ".takt" || dir == filepath.Dir(dir) {
			break
		}
		dir = filepath.Dir(dir)
	}
	return dirs
}

// PublicTerminalIDs returns terminal node IDs visible through the public Run view.
func PublicTerminalIDs(nodes []spec.Node) []string {
	public := map[string]bool{}
	depended := map[string]bool{}
	for _, node := range nodes {
		if !node.Hidden && node.PublicParent == "" {
			public[node.ID] = true
		}
	}
	for _, node := range nodes {
		for _, dep := range node.DependsOn {
			if public[dep] {
				depended[dep] = true
			}
		}
	}
	out := make([]string, 0)
	for id := range public {
		if !depended[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}
