package runtime

import (
	"fmt"
	"path/filepath"
	"strings"

	"takt/internal/assistant"
	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/workflow"
)

// ValidateCapabilities resolves the effective assistant and node policy for all
// command/prompt nodes before a Run is created. Governed child workflows are
// checked recursively with inherited policy bounds.
func ValidateCapabilities(wf *spec.Workflow, cfg *spec.Config, workflowPath string, resolver command.Resolver, assistants assistant.Resolver) error {
	if wf == nil || cfg == nil {
		return fmt.Errorf("workflow and config are required")
	}
	return validateCapabilitiesRecursive(wf, cfg, workflowPath, resolver, assistants, assistant.Policy{}, map[string]bool{}, 0)
}

func validateCapabilitiesRecursive(wf *spec.Workflow, cfg *spec.Config, workflowPath string, resolver command.Resolver, assistants assistant.Resolver, inherited assistant.Policy, stack map[string]bool, depth int) error {
	if depth > 16 {
		return fmt.Errorf("capability validation exceeds governed workflow depth 16")
	}
	for _, node := range wf.Nodes {
		if node.Command != "" || node.Prompt != "" {
			assistantName := node.Provider
			if node.Command != "" {
				definition, err := resolver.Resolve(node.Command)
				if err != nil {
					return fmt.Errorf("node %q: %w", node.ID, err)
				}
				if assistantName == "" {
					assistantName = definition.Assistant
				}
			}
			if assistantName == "" {
				assistantName = wf.Provider
			}
			policy, err := resolveNodePolicy(node, workflowPath, inherited)
			if err != nil {
				return fmt.Errorf("node %q policy: %w", node.ID, err)
			}
			if node.Executor == "external" {
				// External workers attest capabilities at claim time. Validation can
				// still verify that the requested contract is internally coherent.
				if node.ToolApproval != nil && node.ToolApproval.Mode == "required" {
					_ = assistant.RequiredCapabilities(policy)
				}
			} else {
				adapter, err := assistants.Resolve(assistantName)
				if err != nil {
					return fmt.Errorf("node %q: %w", node.ID, err)
				}
				if _, err := validateAdapterPolicy(adapter, policy); err != nil {
					return fmt.Errorf("node %q assistant %q: %w", node.ID, assistantName, err)
				}
			}
		}
		if node.LoopGroup != nil {
			if err := validateCapabilitiesRecursive(&spec.Workflow{Provider: wf.Provider, Model: wf.Model, Nodes: node.LoopGroup.Nodes}, cfg, workflowPath, resolver, assistants, inherited, stack, depth); err != nil {
				return fmt.Errorf("loop_group %q: %w", node.ID, err)
			}
		}
		if node.Matrix != nil {
			if err := validateCapabilitiesRecursive(&spec.Workflow{Provider: wf.Provider, Model: wf.Model, Nodes: node.Matrix.Nodes}, cfg, workflowPath, resolver, assistants, inherited, stack, depth); err != nil {
				return fmt.Errorf("matrix %q: %w", node.ID, err)
			}
		}
		if node.WorkflowRun == nil {
			continue
		}
		if strings.Contains(node.WorkflowRun.Path, "$") || strings.Contains(node.WorkflowRun.Repository, "$") {
			continue
		}
		childPolicy := inherited
		if node.WorkflowRun.Policy != nil {
			local, err := resolvePolicyFields(*node.WorkflowRun.Policy, workflowPath)
			if err != nil {
				return fmt.Errorf("node %q child policy: %w", node.ID, err)
			}
			childPolicy, err = mergePolicies(childPolicy, local)
			if err != nil {
				return fmt.Errorf("node %q child policy: %w", node.ID, err)
			}
		}
		childPath := node.WorkflowRun.Path
		if !filepath.IsAbs(childPath) {
			childPath = filepath.Join(filepath.Dir(workflowPath), childPath)
		}
		childPath, err := filepath.Abs(childPath)
		if err != nil {
			return err
		}
		childPath = filepath.Clean(childPath)
		if stack[childPath] {
			return fmt.Errorf("recursive governed child workflow reference at %s", childPath)
		}
		child, err := workflow.Load(childPath)
		if err != nil {
			return fmt.Errorf("node %q child workflow: %w", node.ID, err)
		}
		childResolver := resolver
		childResolver.Dirs = append(workflow.CommandDirsForDefinition(childPath), childResolver.Dirs...)
		stack[childPath] = true
		err = validateCapabilitiesRecursive(child, cfg, childPath, childResolver, assistants, childPolicy, stack, depth+1)
		delete(stack, childPath)
		if err != nil {
			return fmt.Errorf("node %q child workflow %q: %w", node.ID, child.Name, err)
		}
	}
	return nil
}
