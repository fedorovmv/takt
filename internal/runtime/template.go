package runtime

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"takt/internal/flowref"
	"takt/internal/store"
	"takt/internal/whenexpr"
)

type templateExpression struct {
	Path       string
	Optional   bool
	HasDefault bool
	Default    string
}

func parseTemplateExpression(raw string) (templateExpression, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return templateExpression{}, fmt.Errorf("empty template expression")
	}
	expression := templateExpression{}
	if index := strings.Index(raw, ":-"); index >= 0 {
		expression.Path = strings.TrimSpace(raw[:index])
		expression.HasDefault = true
		expression.Default = raw[index+2:]
	} else if strings.HasSuffix(raw, "?") {
		expression.Path = strings.TrimSpace(strings.TrimSuffix(raw, "?"))
		expression.Optional = true
	} else {
		expression.Path = raw
	}
	if expression.Path == "" {
		return templateExpression{}, fmt.Errorf("template expression path is empty")
	}
	return expression, nil
}

type templateResolver func(string) (string, bool)

func renderTemplate(src string, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string) (string, error) {
	return renderTemplateSurface(src, flowref.NonShell, state, local, feedback, artifactsDir, nil)
}

func renderTemplateWithResolver(src string, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string, extra templateResolver) (string, error) {
	return renderTemplateSurface(src, flowref.NonShell, state, local, feedback, artifactsDir, extra)
}

func renderTemplateSurface(src string, surface flowref.Surface, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string, extra templateResolver) (string, error) {
	return flowref.Render(src, surface, func(ref flowref.Reference) (string, bool) {
		return resolveFlowReference(ref, state, local, feedback, artifactsDir, extra)
	})
}

func resolveFlowReference(ref flowref.Reference, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string, extra templateResolver) (string, bool) {
	key := flowReferenceKey(ref)
	if extra != nil {
		if value, ok := extra(key); ok {
			return value, true
		}
	}
	switch ref.Kind {
	case flowref.KindBare:
		switch ref.Name {
		case "ARGUMENTS":
			return state.Input, true
		case "ARTIFACTS_DIR":
			return artifactsDir, true
		case "FEEDBACK":
			return feedback, true
		case "BASE_BRANCH":
			if state.Worktree != nil && state.Worktree.BaseRef != "" {
				return state.Worktree.BaseRef, true
			}
			return "", false
		}
	case flowref.KindInput:
		if ref.Name == "input" || ref.Name == "message" {
			return state.Input, true
		}
		return "", false
	case flowref.KindFanout:
		return "", false
	case flowref.KindLoopPrevious:
		if n, ok := local[ref.NodeID]; ok {
			return nodePathLookup(n, ref.Path)
		}
		if len(local) == 0 {
			// The first loop iteration has no prior snapshot. This is the one
			// implicit-empty reference permitted by the workflow contract.
			return "", true
		}
		return "", false
	case flowref.KindNode, flowref.KindApproval:
		if ref.Kind == flowref.KindApproval {
			value, ok := state.Approvals[ref.NodeID]
			return value, ok
		}
		if len(ref.Path) == 1 && ref.Path[0] == "output" {
			if value, ok := state.Approvals[ref.NodeID]; ok {
				return value, true
			}
		}
		if n, ok := state.Nodes[ref.NodeID]; ok && n != nil {
			return nodePathLookup(*n, ref.Path)
		}
		if n, ok := local[ref.NodeID]; ok {
			return nodePathLookup(n, ref.Path)
		}
	}
	return "", false
}

func flowReferenceKey(ref flowref.Reference) string {
	switch ref.Kind {
	case flowref.KindBare:
		return strings.ToLower(ref.Name)
	case flowref.KindInput:
		return "inputs." + strings.Join(append([]string{ref.Name}, ref.Path...), ".")
	case flowref.KindFanout:
		return "fanout." + strings.Join(append([]string{ref.Name}, ref.Path...), ".")
	case flowref.KindLoopPrevious:
		return "loop.previous." + strings.Join(append([]string{ref.NodeID}, ref.Path...), ".")
	case flowref.KindApproval:
		return "approvals." + ref.NodeID
	default:
		return "nodes." + strings.Join(append([]string{ref.NodeID}, ref.Path...), ".")
	}
}

func nodePath(n store.NodeState, parts []string) string {
	value, _ := nodePathLookup(n, parts)
	return value
}

func nodePathLookup(n store.NodeState, parts []string) (string, bool) {
	if len(parts) == 0 {
		return "", false
	}
	if parts[0] == "artifacts" {
		return artifactPathLookup(n.Artifacts, parts[1:])
	}
	value, found := nodeFieldLookup(n, parts[0])
	if !found {
		return "", false
	}
	if len(parts) == 1 || parts[0] != "output" {
		return value, true
	}
	return jsonPathLookup(value, parts[1:])
}

func artifactPathString(artifacts []store.ArtifactRef, parts []string) string {
	value, _ := artifactPathLookup(artifacts, parts)
	return value
}

func artifactPathLookup(artifacts []store.ArtifactRef, parts []string) (string, bool) {
	if len(parts) == 0 {
		raw, _ := json.Marshal(artifacts)
		return string(raw), true
	}
	var artifact *store.ArtifactRef
	if index, err := strconv.Atoi(parts[0]); err == nil {
		if index < 0 || index >= len(artifacts) {
			return "", false
		}
		artifact = &artifacts[index]
	} else {
		for index := range artifacts {
			if artifacts[index].Type == parts[0] {
				artifact = &artifacts[index]
				break
			}
		}
	}
	if artifact == nil {
		return "", false
	}
	if len(parts) == 1 {
		raw, _ := json.Marshal(artifact)
		return string(raw), true
	}
	switch parts[1] {
	case "id":
		return artifact.ID, true
	case "type":
		return artifact.Type, true
	case "mime":
		return artifact.MIME, true
	case "path":
		return artifact.Path, true
	case "sha256":
		return artifact.SHA256, true
	case "size":
		return strconv.FormatInt(artifact.Size, 10), true
	case "producer_run_id":
		return artifact.ProducerRunID, true
	case "producer_node_id":
		return artifact.ProducerNodeID, true
	case "attempt":
		return strconv.Itoa(artifact.Attempt), true
	default:
		return "", false
	}
}

func jsonPathString(raw string, path []string) string {
	value, _ := jsonPathLookup(raw, path)
	return value
}

func jsonPathLookup(raw string, path []string) (string, bool) {
	if len(path) == 0 {
		return raw, true
	}
	var current any
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.UseNumber()
	if err := dec.Decode(&current); err != nil {
		return "", false
	}
	for _, part := range path {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[part]
			if !ok {
				return "", false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return "", false
			}
			current = value[index]
		default:
			return "", false
		}
	}
	switch value := current.(type) {
	case string:
		return value, true
	case nil:
		return "", true
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

func nodeField(n store.NodeState, field string) string {
	value, _ := nodeFieldLookup(n, field)
	return value
}

func nodeFieldLookup(n store.NodeState, field string) (string, bool) {
	switch field {
	case "output":
		return n.Output, true
	case "exit_code":
		return strconv.Itoa(n.ExitCode), true
	case "status":
		return n.Status, true
	case "child_run_id":
		return n.ChildRunID, true
	case "child_control_workspace":
		return n.ChildControlWorkspace, true
	case "child_execution_workspace":
		return n.ChildExecutionWorkspace, true
	case "child_branch":
		return n.ChildBranch, true
	case "child_base_commit":
		return n.ChildBaseCommit, true
	default:
		return "", false
	}
}

func evalWhen(expr string, state *store.RunState) (bool, error) {
	return whenexpr.Evaluate(expr, func(path string) (string, error) {
		return resolveExprPath(path, state)
	})
}

func resolveExprPath(path string, state *store.RunState) (string, error) {
	ref, err := flowref.Parse(path, flowref.When)
	if err != nil {
		return "", err
	}
	if ref.Kind == flowref.KindBare && ref.Name == "ARGUMENTS" {
		return state.Input, nil
	}
	if ref.Kind == flowref.KindNode {
		n, ok := state.Nodes[ref.NodeID]
		if !ok || n == nil {
			return "", fmt.Errorf("when references unknown node %q", ref.NodeID)
		}
		value, found := nodePathLookup(*n, ref.Path)
		if !found {
			return "", fmt.Errorf("when references unknown node field %q", strings.Join(ref.Path, "."))
		}
		return value, nil
	}
	if ref.Kind == flowref.KindInput && (ref.Name == "input" || ref.Name == "message") {
		return state.Input, nil
	}
	return "", fmt.Errorf("unsupported expression path %q", path)
}
