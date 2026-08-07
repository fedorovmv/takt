package runtime

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"takt/internal/store"
)

var variableRE = regexp.MustCompile(`\$\{([^}]+)\}`)

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
	return renderTemplateWithResolver(src, state, local, feedback, artifactsDir, nil)
}

func renderTemplateWithResolver(src string, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string, extra templateResolver) (string, error) {
	src = strings.ReplaceAll(src, "$USER_MESSAGE", state.Input)
	src = strings.ReplaceAll(src, "$ARTIFACTS_DIR", artifactsDir)
	var renderErr error
	out := variableRE.ReplaceAllStringFunc(src, func(token string) string {
		if renderErr != nil {
			return token
		}
		raw := strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}")
		expression, err := parseTemplateExpression(raw)
		if err != nil {
			renderErr = err
			return token
		}
		value, found := resolveTemplatePath(expression.Path, state, local, feedback, extra)
		if expression.HasDefault && (!found || value == "") {
			return expression.Default
		}
		if expression.Optional && !found {
			return ""
		}
		if !found {
			renderErr = fmt.Errorf("unresolved template expression %q", expression.Path)
			return token
		}
		return value
	})
	if renderErr != nil {
		return "", renderErr
	}
	return out, nil
}

func resolveTemplatePath(key string, state *store.RunState, local map[string]store.NodeState, feedback string, extra templateResolver) (string, bool) {
	if extra != nil {
		if value, ok := extra(key); ok {
			return value, true
		}
	}
	switch key {
	case "input":
		return state.Input, true
	case "feedback":
		return feedback, true
	}
	parts := strings.Split(key, ".")
	if len(parts) >= 3 && parts[0] == "nodes" {
		if n, ok := state.Nodes[parts[1]]; ok && n != nil {
			return nodePathLookup(*n, parts[2:])
		}
		if n, ok := local[parts[1]]; ok {
			return nodePathLookup(n, parts[2:])
		}
		return "", false
	}
	if len(parts) >= 4 && parts[0] == "loop" && parts[1] == "previous" {
		if n, ok := local[parts[2]]; ok {
			return nodePathLookup(n, parts[3:])
		}
		return "", false
	}
	if len(parts) == 2 && parts[0] == "approvals" {
		value, ok := state.Approvals[parts[1]]
		return value, ok
	}
	return "", false
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
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	if parts := splitLogicalExpression(expr, "||"); len(parts) > 1 {
		for _, part := range parts {
			ok, err := evalWhen(part, state)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}
	if parts := splitLogicalExpression(expr, "&&"); len(parts) > 1 {
		for _, part := range parts {
			ok, err := evalWhen(part, state)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}
	ops := []string{"==", "!="}
	for _, op := range ops {
		if idx := strings.Index(expr, op); idx >= 0 {
			left := strings.TrimSpace(expr[:idx])
			right := strings.Trim(strings.TrimSpace(expr[idx+len(op):]), `"'`)
			value, err := resolveExprPath(left, state)
			if err != nil {
				return false, err
			}
			if op == "==" {
				return value == right, nil
			}
			return value != right, nil
		}
	}
	return false, fmt.Errorf("unsupported when expression %q", expr)
}

func splitLogicalExpression(expr, operator string) []string {
	var parts []string
	start := 0
	var quote rune
	for index, r := range expr {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if index+len(operator) <= len(expr) && expr[index:index+len(operator)] == operator {
			parts = append(parts, strings.TrimSpace(expr[start:index]))
			start = index + len(operator)
		}
	}
	if start == 0 {
		return []string{expr}
	}
	parts = append(parts, strings.TrimSpace(expr[start:]))
	return parts
}

func resolveExprPath(path string, state *store.RunState) (string, error) {
	parts := strings.Split(path, ".")
	if len(parts) >= 3 && parts[0] == "nodes" {
		n, ok := state.Nodes[parts[1]]
		if !ok {
			return "", fmt.Errorf("when references unknown node %q", parts[1])
		}
		value, found := nodePathLookup(*n, parts[2:])
		if !found {
			return "", fmt.Errorf("when references unknown node field %q", strings.Join(parts[2:], "."))
		}
		return value, nil
	}
	if len(parts) == 2 && parts[0] == "inputs" {
		if parts[1] == "message" || parts[1] == "input" {
			return state.Input, nil
		}
	}
	return "", fmt.Errorf("unsupported expression path %q", path)
}
