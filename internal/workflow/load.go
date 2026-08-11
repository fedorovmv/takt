package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"takt/internal/spec"
	"takt/internal/yamlcodec"
)

const maxGovernedWorkflowDepth = 16

func Load(path string) (*spec.Workflow, error) {
	wf, err := loadOne(path)
	if err != nil {
		return nil, err
	}
	canonical, err := canonicalWorkflowPath(path)
	if err != nil {
		return nil, err
	}
	stack := map[string]bool{canonical: true}
	if err := validateGovernedWorkflowGraph(canonical, wf, stack, 0); err != nil {
		return nil, err
	}
	return wf, nil
}

func loadOne(path string) (*spec.Workflow, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow: %w", err)
	}
	var wf spec.Workflow
	if err := yamlcodec.Unmarshal(b, &wf); err != nil {
		return nil, fmt.Errorf("parse workflow %s: %w", path, err)
	}
	// Keep the internal expansion/runtime compatibility view populated while
	// exposing only the Archon-first root in YAML.
	if wf.Name != "" {
		wf.Metadata = spec.Metadata{Name: wf.Name, Description: wf.Description, Labels: wf.Labels}
	}
	wf.Defaults = spec.Defaults{Assistant: wf.Provider, Model: wf.Model, Session: "fresh"}
	expanded, err := Expand(path, &wf)
	if err != nil {
		return nil, fmt.Errorf("expand workflow %s: %w", path, err)
	}
	if err := Validate(expanded); err != nil {
		return nil, err
	}
	if err := validateWorkflowResources(path, expanded); err != nil {
		return nil, err
	}
	return expanded, nil
}

func validateGovernedWorkflowGraph(path string, wf *spec.Workflow, stack map[string]bool, depth int) error {
	if depth >= maxGovernedWorkflowDepth {
		return fmt.Errorf("governed workflow depth exceeds %d at %s", maxGovernedWorkflowDepth, path)
	}
	paths := collectGovernedWorkflowPaths(wf.Nodes)
	sort.Strings(paths)
	for _, ref := range paths {
		childPath := ref
		if !filepath.IsAbs(childPath) {
			childPath = filepath.Join(filepath.Dir(path), childPath)
		}
		childPath, err := canonicalWorkflowPath(childPath)
		if err != nil {
			return err
		}
		if stack[childPath] {
			return fmt.Errorf("recursive governed child workflow reference: %s", childPath)
		}
		child, err := loadOne(childPath)
		if err != nil {
			return err
		}
		stack[childPath] = true
		err = validateGovernedWorkflowGraph(childPath, child, stack, depth+1)
		delete(stack, childPath)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowResources(path string, wf *spec.Workflow) error {
	base := filepath.Dir(path)
	var visit func([]spec.Node) error
	visit = func(nodes []spec.Node) error {
		for _, node := range nodes {
			if node.Script != nil {
				paths := append([]string(nil), node.Script.Dependencies...)
				if node.Script.Path != "" {
					paths = append([]string{node.Script.Path}, paths...)
				}
				for _, resource := range paths {
					resolved := resource
					if !filepath.IsAbs(resolved) {
						resolved = filepath.Join(base, resolved)
					}
					if _, err := os.Stat(resolved); err != nil {
						return fmt.Errorf("node %q script resource %s: %w", node.ID, resolved, err)
					}
				}
			}
			resources := []string{}
			if node.MCP != "" {
				resources = append(resources, node.MCP)
			}
			if node.WorkflowRun != nil && node.WorkflowRun.Policy != nil && node.WorkflowRun.Policy.MCP != "" {
				resources = append(resources, node.WorkflowRun.Policy.MCP)
			}
			for _, resource := range resources {
				if !filepath.IsAbs(resource) {
					resource = filepath.Join(base, resource)
				}
				raw, err := os.ReadFile(resource)
				if err != nil {
					return fmt.Errorf("node %q read MCP config %s: %w", node.ID, resource, err)
				}
				var value any
				if err := json.Unmarshal(raw, &value); err != nil {
					return fmt.Errorf("node %q parse MCP config %s: %w", node.ID, resource, err)
				}
				if _, ok := value.(map[string]any); !ok {
					return fmt.Errorf("node %q MCP config %s must contain a JSON object", node.ID, resource)
				}
			}

			if node.LoopGroup != nil {
				if err := visit(node.LoopGroup.Nodes); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(wf.Nodes)
}

func canonicalWorkflowPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve workflow path %s: %w", path, err)
	}
	return filepath.Clean(resolved), nil
}

func collectGovernedWorkflowPaths(nodes []spec.Node) []string {
	set := map[string]bool{}
	var visit func([]spec.Node)
	visit = func(items []spec.Node) {
		for _, node := range items {
			if node.WorkflowRun != nil && node.WorkflowRun.Path != "" {
				set[node.WorkflowRun.Path] = true
			}
			if node.LoopGroup != nil {
				visit(node.LoopGroup.Nodes)
			}
		}
	}
	visit(nodes)
	out := make([]string, 0, len(set))
	for path := range set {
		out = append(out, path)
	}
	return out
}
