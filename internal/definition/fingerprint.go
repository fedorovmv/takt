package definition

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"takt/internal/command"
	"takt/internal/spec"
	"takt/internal/workflow"
)

type Fingerprints struct {
	Workflow string
	Config   string
	Commands string
}

func Compute(wf *spec.Workflow, cfg *spec.Config, workflowPath, configPath string, resolver command.Resolver) (Fingerprints, error) {
	workflowBytes, err := workflowDefinitionBytes(workflowPath, wf)
	if err != nil {
		return Fingerprints{}, fmt.Errorf("fingerprint workflow: %w", err)
	}
	configBytes, err := configDefinitionBytes(configPath, cfg)
	if err != nil {
		return Fingerprints{}, fmt.Errorf("fingerprint config: %w", err)
	}
	names := collectCommands(wf.Nodes)
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		cmd, err := resolver.Resolve(name)
		if err != nil {
			return Fingerprints{}, fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		b, err := os.ReadFile(cmd.Path)
		if err != nil {
			return Fingerprints{}, fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(b)
		_, _ = h.Write([]byte{0})
	}
	return Fingerprints{
		Workflow: sum(workflowBytes),
		Config:   sum(configBytes),
		Commands: hex.EncodeToString(h.Sum(nil)),
	}, nil
}

func configDefinitionBytes(path string, cfg *spec.Config) ([]byte, error) {
	if cfg != nil && cfg.ModelsMaterialized {
		return json.Marshal(cfg)
	}
	return bytesOrJSON(path, cfg)
}

// ContentClosureFingerprint returns one fingerprint for the executable content
// addressed by a workflow: the expanded workflow definition, nested workflow
// resources, scripts/dependencies, path skills/MCP files, and resolved Markdown
// commands. It is used when a trusted block must remain immutable between
// preview and execution.
func ContentClosureFingerprint(wf *spec.Workflow, workflowPath string, resolver command.Resolver) (string, error) {
	definitionBytes, err := workflowDefinitionBytes(workflowPath, wf)
	if err != nil {
		return "", err
	}
	out := append([]byte(nil), definitionBytes...)
	names := collectCommands(wf.Nodes)
	sort.Strings(names)
	for _, name := range names {
		cmd, err := resolver.Resolve(name)
		if err != nil {
			return "", fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		raw, err := os.ReadFile(cmd.Path)
		if err != nil {
			return "", fmt.Errorf("fingerprint command %q: %w", name, err)
		}
		out = append(out, 0)
		out = append(out, []byte(cmd.Path)...)
		out = append(out, 0)
		out = append(out, raw...)
	}
	return sum(out), nil
}

func Verify(expected Fingerprints, actual Fingerprints) error {
	if expected.Workflow != "" && expected.Workflow != actual.Workflow {
		return &ChangedError{Kind: "workflow", Expected: expected.Workflow, Actual: actual.Workflow}
	}
	if expected.Config != "" && expected.Config != actual.Config {
		return &ChangedError{Kind: "config", Expected: expected.Config, Actual: actual.Config}
	}
	if expected.Commands != "" && expected.Commands != actual.Commands {
		return &ChangedError{Kind: "commands", Expected: expected.Commands, Actual: actual.Commands}
	}
	return nil
}

type ChangedError struct {
	Kind     string
	Expected string
	Actual   string
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("%s definition changed since run start", e.Kind)
}

func workflowDefinitionBytes(path string, wf *spec.Workflow) ([]byte, error) {
	return workflowDefinitionBytesSeen(path, wf, map[string]bool{})
}

func workflowDefinitionBytesSeen(path string, wf *spec.Workflow, stack map[string]bool) ([]byte, error) {
	var source []byte
	if path != "" && path[0] != '<' {
		b, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		source = b
	}
	canonical, err := json.Marshal(struct {
		Workflow *spec.Workflow                   `json:"workflow"`
		Internal map[string]spec.InternalNodeSpec `json:"internal,omitempty"`
	}{
		Workflow: wf,
		Internal: collectInternalNodes(wf.Nodes),
	})
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(source)+len(canonical)+1)
	out = append(out, source...)
	out = append(out, 0)
	out = append(out, canonical...)

	resources, err := collectDefinitionResourcePaths(path, wf.Nodes)
	if err != nil {
		return nil, err
	}
	for _, resource := range resources {
		content, err := policyResourceBytes(resource)
		if err != nil {
			return nil, err
		}
		out = append(out, 0)
		out = append(out, []byte(resource)...)
		out = append(out, 0)
		out = append(out, content...)
	}

	paths := collectGovernedWorkflowPaths(wf.Nodes)
	sort.Strings(paths)
	for _, childPath := range paths {
		if !filepath.IsAbs(childPath) {
			childPath = filepath.Join(filepath.Dir(path), childPath)
		}
		childPath = filepath.Clean(childPath)
		if stack[childPath] {
			return nil, fmt.Errorf("recursive governed child workflow reference: %s", childPath)
		}
		stack[childPath] = true
		child, err := workflow.Load(childPath)
		if err != nil {
			return nil, err
		}
		childBytes, err := workflowDefinitionBytesSeen(childPath, child, stack)
		delete(stack, childPath)
		if err != nil {
			return nil, err
		}
		out = append(out, 0)
		out = append(out, []byte(childPath)...)
		out = append(out, 0)
		out = append(out, childBytes...)
	}
	return out, nil
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
			if node.Matrix != nil {
				visit(node.Matrix.Nodes)
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

func collectInternalNodes(nodes []spec.Node) map[string]spec.InternalNodeSpec {
	out := map[string]spec.InternalNodeSpec{}
	var visit func([]spec.Node)
	visit = func(items []spec.Node) {
		for _, node := range items {
			if node.Internal != nil {
				out[node.ID] = *node.Internal
			}
			if node.LoopGroup != nil {
				visit(node.LoopGroup.Nodes)
			}
			if node.Matrix != nil {
				visit(node.Matrix.Nodes)
			}
		}
	}
	visit(nodes)
	if len(out) == 0 {
		return nil
	}
	return out
}

func bytesOrJSON(path string, value any) ([]byte, error) {
	if path != "" && path[0] != '<' {
		if b, err := os.ReadFile(path); err == nil {
			return b, nil
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return json.Marshal(value)
}

func sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func collectCommands(nodes []spec.Node) []string {
	set := map[string]struct{}{}
	var visit func([]spec.Node)
	visit = func(items []spec.Node) {
		for _, node := range items {
			if node.Command != "" {
				set[node.Command] = struct{}{}
			}
			if node.LoopGroup != nil {
				visit(node.LoopGroup.Nodes)
			}
			if node.Matrix != nil {
				visit(node.Matrix.Nodes)
			}
		}
	}
	visit(nodes)
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	return out
}

func optionalStringValues(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}

func optionalStringCount(value *[]string) int {
	return len(optionalStringValues(value))
}

func collectDefinitionResourcePaths(workflowPath string, nodes []spec.Node) ([]string, error) {
	set := map[string]bool{}
	base := filepath.Dir(workflowPath)
	type resourceRef struct {
		value     string
		mandatory bool
	}
	var visit func([]spec.Node) error
	visit = func(items []spec.Node) error {
		for _, node := range items {
			refs := make([]resourceRef, 0, optionalStringCount(node.Skills)+4)
			if node.Script != nil {
				if node.Script.Path != "" {
					refs = append(refs, resourceRef{value: node.Script.Path, mandatory: true})
				}
				for _, dependency := range node.Script.Dependencies {
					refs = append(refs, resourceRef{value: dependency, mandatory: true})
				}
			}
			for _, skill := range optionalStringValues(node.Skills) {
				refs = append(refs, resourceRef{value: skill})
			}
			if node.MCP != "" {
				refs = append(refs, resourceRef{value: node.MCP, mandatory: true})
			}
			if node.WorkflowRun != nil && node.WorkflowRun.Policy != nil {
				for _, skill := range optionalStringValues(node.WorkflowRun.Policy.Skills) {
					refs = append(refs, resourceRef{value: skill})
				}
				if node.WorkflowRun.Policy.MCP != "" {
					refs = append(refs, resourceRef{value: node.WorkflowRun.Policy.MCP, mandatory: true})
				}
			}
			for _, ref := range refs {
				candidate := ref.value
				if !filepath.IsAbs(candidate) {
					candidate = filepath.Join(base, candidate)
				}
				info, err := os.Stat(candidate)
				if err != nil {
					if ref.mandatory || !os.IsNotExist(err) {
						return fmt.Errorf("fingerprint policy resource %q: %w", ref.value, err)
					}
					continue // a non-path skill name is adapter-resolved
				}
				if info.Mode()&os.ModeSymlink != 0 {
					if candidate, err = filepath.EvalSymlinks(candidate); err != nil {
						return err
					}
				}
				abs, err := filepath.Abs(candidate)
				if err != nil {
					return err
				}
				set[filepath.Clean(abs)] = true
			}
			if node.LoopGroup != nil {
				if err := visit(node.LoopGroup.Nodes); err != nil {
					return err
				}
			}
			if node.Matrix != nil {
				if err := visit(node.Matrix.Nodes); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(nodes); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

func policyResourceBytes(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return os.ReadFile(path)
	}
	var files []string
	if err := filepath.Walk(path, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, current)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []byte
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(path, file)
		out = append(out, []byte(rel)...)
		out = append(out, 0)
		out = append(out, raw...)
		out = append(out, 0)
	}
	return out, nil
}
