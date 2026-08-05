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

func renderTemplate(src string, state *store.RunState, local map[string]store.NodeState, feedback, artifactsDir string) string {
	src = strings.ReplaceAll(src, "$USER_MESSAGE", state.Input)
	src = strings.ReplaceAll(src, "$ARTIFACTS_DIR", artifactsDir)
	return variableRE.ReplaceAllStringFunc(src, func(token string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(token, "${"), "}")
		switch key {
		case "input":
			return state.Input
		case "feedback":
			return feedback
		}
		parts := strings.Split(key, ".")
		if len(parts) >= 3 && parts[0] == "nodes" {
			if n, ok := state.Nodes[parts[1]]; ok {
				return nodePath(*n, parts[2:])
			}
			if n, ok := local[parts[1]]; ok {
				return nodePath(n, parts[2:])
			}
		}
		if len(parts) >= 4 && parts[0] == "loop" && parts[1] == "previous" {
			if n, ok := local[parts[2]]; ok {
				return nodePath(n, parts[3:])
			}
		}
		if len(parts) == 2 && parts[0] == "approvals" {
			return state.Approvals[parts[1]]
		}
		return token
	})
}

func nodePath(n store.NodeState, parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	value := nodeField(n, parts[0])
	if len(parts) == 1 || parts[0] != "output" {
		return value
	}
	return jsonPathString(value, parts[1:])
}

func jsonPathString(raw string, path []string) string {
	if len(path) == 0 {
		return raw
	}
	var current any
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.UseNumber()
	if err := dec.Decode(&current); err != nil {
		return ""
	}
	for _, part := range path {
		switch value := current.(type) {
		case map[string]any:
			current = value[part]
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return ""
			}
			current = value[index]
		default:
			return ""
		}
	}
	switch value := current.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		b, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func nodeField(n store.NodeState, field string) string {
	switch field {
	case "output":
		return n.Output
	case "exit_code":
		return strconv.Itoa(n.ExitCode)
	case "status":
		return n.Status
	default:
		return ""
	}
}

func evalWhen(expr string, state *store.RunState) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
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

func resolveExprPath(path string, state *store.RunState) (string, error) {
	parts := strings.Split(path, ".")
	if len(parts) >= 3 && parts[0] == "nodes" {
		n, ok := state.Nodes[parts[1]]
		if !ok {
			return "", fmt.Errorf("when references unknown node %q", parts[1])
		}
		return nodePath(*n, parts[2:]), nil
	}
	if len(parts) == 2 && parts[0] == "inputs" {
		if parts[1] == "message" || parts[1] == "input" {
			return state.Input, nil
		}
	}
	return "", fmt.Errorf("unsupported expression path %q", path)
}
