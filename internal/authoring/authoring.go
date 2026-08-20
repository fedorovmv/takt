package authoring

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"takt/internal/command"
	"takt/internal/flowref"
	"takt/internal/spec"
)

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Hint     string `json:"hint,omitempty"`
}

func (d Diagnostic) Error() string {
	if d.Path != "" {
		return fmt.Sprintf("%s: %s", d.Path, d.Message)
	}
	return d.Message
}

type Error struct {
	Diagnostics []Diagnostic
}

func (e *Error) Error() string {
	if len(e.Diagnostics) == 0 {
		return "authoring validation failed"
	}
	return e.Diagnostics[0].Error()
}

func HasErrors(values []Diagnostic) bool {
	for _, value := range values {
		if value.Severity == "error" {
			return true
		}
	}
	return false
}

// Analyze performs static checks that are intentionally stricter and more
// helpful than runtime rendering. It never mutates the workflow.
func Analyze(wf *spec.Workflow, resolver command.Resolver) []Diagnostic {
	if wf == nil {
		return []Diagnostic{{Code: "workflow.required", Severity: "error", Message: "workflow is required"}}
	}
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, analyzeScope(wf.Nodes, "nodes", resolver, false, false, nil)...)
	sort.SliceStable(diagnostics, func(i, j int) bool {
		if diagnostics[i].Severity != diagnostics[j].Severity {
			return diagnostics[i].Severity == "error"
		}
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		return diagnostics[i].Code < diagnostics[j].Code
	})
	return diagnostics
}

func analyzeScope(nodes []spec.Node, scope string, resolver command.Resolver, insideLoop, insideMatrix bool, inherited map[string]spec.Node) []Diagnostic {
	local := make(map[string]spec.Node, len(nodes))
	byID := make(map[string]spec.Node, len(nodes)+len(inherited))
	for id, node := range inherited {
		byID[id] = node
	}
	for _, node := range nodes {
		local[node.ID] = node
		byID[node.ID] = node
	}
	var diagnostics []Diagnostic
	for index, node := range nodes {
		path := fmt.Sprintf("%s[%d]", scope, index)
		if reservedArchonNodeID(node.ID) {
			diagnostics = append(diagnostics, Diagnostic{Code: "node.id_reserved", Severity: "error", Path: path + ".id", Message: fmt.Sprintf("node id %q is reserved by the reference language", node.ID)})
		}
		if node.AlwaysRun && len(node.DependsOn) == 0 {
			diagnostics = append(diagnostics, Diagnostic{Code: "always_run.no_dependencies", Severity: "warning", Path: path + ".always_run", Message: "always_run has no effect on a node without dependencies", Hint: "remove always_run or add the cleanup dependencies"})
		}
		if node.IdleTimeout != "" && node.Timeout != "" {
			idle, idleErr := time.ParseDuration(node.IdleTimeout)
			total, totalErr := time.ParseDuration(node.Timeout)
			if idleErr == nil && totalErr == nil && idle >= total {
				diagnostics = append(diagnostics, Diagnostic{Code: "idle_timeout.ineffective", Severity: "warning", Path: path + ".idle_timeout", Message: "idle_timeout is greater than or equal to timeout and cannot fire first", Hint: "set idle_timeout below the total timeout"})
			}
		}
		if node.IdleTimeout != "" && node.Executor == "external" {
			diagnostics = append(diagnostics, Diagnostic{Code: "idle_timeout.daemon_required", Severity: "warning", Path: path + ".idle_timeout", Message: "external executor idle_timeout is enforced by takt daemon while the task is claimed", Hint: "run this workflow through takt daemon for automatic idle cancellation"})
		}
		if node.OutputFormat != nil && node.OutputFormat.Type == "object" && node.OutputFormat.AdditionalProperties == nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "schema.open_object", Severity: "warning", Path: path + ".output_format", Message: "object output_format accepts undeclared properties", Hint: "set additionalProperties: false for a closed machine-readable contract"})
		}

		fields := templateFields(node, resolver)
		for _, field := range fields {
			surface := flowref.NonShell
			if strings.HasSuffix(field.path, ".bash") {
				surface = flowref.Shell
			}
			diagnostics = append(diagnostics, analyzeTemplate(field.value, field.path, node, byID, local, insideLoop, insideMatrix, surface)...)
		}
		if strings.TrimSpace(node.When) != "" {
			diagnostics = append(diagnostics, analyzeWhen(node.When, path+".when", node, byID, local)...)
		}
		if node.LoopGroup != nil {
			childInherited := make(map[string]spec.Node)
			for id, candidate := range inherited {
				childInherited[id] = candidate
			}
			for id, candidate := range local {
				if id != node.ID && dependsOn(node.ID, id, local, map[string]bool{}) {
					childInherited[id] = candidate
				}
			}
			diagnostics = append(diagnostics, analyzeScope(node.LoopGroup.Nodes, path+".loop_group.nodes", resolver, true, insideMatrix, childInherited)...)
		}
		if node.Matrix != nil {
			childInherited := make(map[string]spec.Node)
			for id, candidate := range inherited {
				childInherited[id] = candidate
			}
			for id, candidate := range local {
				if id != node.ID && dependsOn(node.ID, id, local, map[string]bool{}) {
					childInherited[id] = candidate
				}
			}
			diagnostics = append(diagnostics, analyzeScope(node.Matrix.Nodes, path+".matrix.nodes", resolver, insideLoop, true, childInherited)...)
		}
	}
	return diagnostics
}

type templateField struct {
	path  string
	value string
}

func templateFields(node spec.Node, resolver command.Resolver) []templateField {
	base := "node." + node.ID
	var fields []templateField
	if node.Prompt != "" {
		fields = append(fields, templateField{base + ".prompt", node.Prompt})
	}
	if node.Command != "" {
		if definition, err := resolver.Resolve(node.Command); err == nil {
			fields = append(fields, templateField{base + ".command(" + node.Command + ")", definition.Body})
		}
	}
	if node.Bash != "" {
		fields = append(fields, templateField{base + ".bash", node.Bash})
	}
	if node.Approval != nil {
		fields = append(fields, templateField{base + ".approval.message", node.Approval.Message})
	}
	if node.ToolApproval != nil {
		fields = append(fields, templateField{base + ".tool_approval.message", node.ToolApproval.Message})
	}
	if node.Script != nil {
		fields = append(fields, templateField{base + ".script.path", node.Script.Path}, templateField{base + ".script.inline", node.Script.Inline}, templateField{base + ".script.working_directory", node.Script.WorkingDir})
		for index, value := range node.Script.Args {
			fields = append(fields, templateField{fmt.Sprintf("%s.script.args[%d]", base, index), value})
		}
		for key, value := range node.Script.Env {
			fields = append(fields, templateField{base + ".script.env." + key, value})
		}
	}
	if node.WorkflowRun != nil {
		fields = append(fields, templateField{base + ".workflow.input", node.WorkflowRun.Input})
	}
	if node.OutputPath != "" {
		fields = append(fields, templateField{base + ".output_path", node.OutputPath})
	}
	for _, group := range []struct {
		name  string
		hooks []spec.HookSpec
	}{{"before_node", node.Hooks.BeforeNode}, {"after_node", node.Hooks.AfterNode}, {"before_complete", node.Hooks.BeforeComplete}, {"on_failure", node.Hooks.OnFailure}} {
		for index, hook := range group.hooks {
			fields = append(fields, templateField{fmt.Sprintf("%s.hooks.%s[%d].bash", base, group.name, index), hook.Bash})
		}
	}
	return fields
}

type expression struct {
	path       string
	optional   bool
	hasDefault bool
}

func parseExpression(raw string) (expression, error) {
	raw = strings.TrimSpace(raw)
	result := expression{}
	if index := strings.Index(raw, ":-"); index >= 0 {
		result.path = strings.TrimSpace(raw[:index])
		result.hasDefault = true
	} else if strings.HasSuffix(raw, "?") {
		result.path = strings.TrimSpace(strings.TrimSuffix(raw, "?"))
		result.optional = true
	} else {
		result.path = raw
	}
	if result.path == "" {
		return expression{}, fmt.Errorf("empty expression path")
	}
	return result, nil
}

func analyzeTemplate(value, path string, current spec.Node, byID, local map[string]spec.Node, insideLoop, insideMatrix bool, surface flowref.Surface) []Diagnostic {
	var diagnostics []Diagnostic
	if strings.Contains(path, ".tool_approval.message") {
		// `${tool}` is an external-worker protocol placeholder, not a workflow
		// reference; the worker replaces it when presenting the approval prompt.
		value = strings.ReplaceAll(value, "${tool}", "")
	}
	if strings.Contains(value, "${") || strings.Contains(value, "$USER_MESSAGE") {
		return []Diagnostic{{Code: "template.reference_legacy", Severity: "error", Path: path, Message: "legacy Takt references are not accepted; use the Archon-first $... grammar"}}
	}
	refs, err := flowref.Scan(value, surface)
	if err != nil {
		return []Diagnostic{{Code: "template.invalid", Severity: "error", Path: path, Message: err.Error()}}
	}
	if strings.HasSuffix(path, ".script.inline") && len(refs) > 0 {
		return []Diagnostic{{Code: "script.inline_reference", Severity: "error", Path: path, Message: "inline script source cannot contain Takt references; pass values through script.args or script.env"}}
	}
	for _, ref := range refs {
		switch ref.Kind {
		case flowref.KindBare, flowref.KindInput, flowref.KindFanout:
			continue
		case flowref.KindMatrix:
			if !insideMatrix {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.matrix_outside_matrix", Severity: "error", Path: path, Message: "$MATRIX is available only inside matrix"})
			}
			continue
		case flowref.KindApproval:
			target, ok := byID[ref.NodeID]
			if !ok || target.Approval == nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.approval_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("template references unknown approval %q", ref.NodeID)})
			} else if _, inherited := local[target.ID]; inherited && !dependsOn(current.ID, target.ID, local, map[string]bool{}) {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("approval %q is not an upstream dependency of node %q", target.ID, current.ID)})
			}
			continue
		case flowref.KindLoopPrevious:
			if !insideLoop {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.loop_outside_loop", Severity: "error", Path: path, Message: "loop.previous is available only inside loop_group"})
			}
			continue
		case flowref.KindNode:
			source, ok := byID[ref.NodeID]
			if !ok {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.node_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("template references unknown node %q", ref.NodeID)})
				continue
			}
			if source.ID == current.ID || (local[source.ID].ID != "" && !dependsOn(current.ID, source.ID, local, map[string]bool{})) {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("node %q is not an upstream dependency of node %q", source.ID, current.ID), Hint: "add it to depends_on directly or transitively"})
				continue
			}
			diagnostics = append(diagnostics, validateNodePath(source, ref.Path, path, expression{})...)
		}
	}
	return diagnostics
}

func reservedArchonNodeID(id string) bool {
	switch id {
	case "ARGUMENTS", "ARTIFACTS_DIR", "BASE_BRANCH", "INPUTS", "LOOP_PREV", "FEEDBACK", "FANOUT", "MATRIX":
		return true
	default:
		return false
	}
}

func validateNodePath(source spec.Node, parts []string, path string, expression expression) []Diagnostic {
	if len(parts) == 0 {
		return []Diagnostic{{Code: "template.node_field_missing", Severity: "error", Path: path, Message: "node reference requires output, status, exit_code, child metadata, or artifacts"}}
	}
	switch parts[0] {
	case "status", "exit_code":
		if len(parts) != 1 {
			return []Diagnostic{{Code: "template.node_field_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("%s does not support nested fields", parts[0])}}
		}
	case "child_run_id", "child_control_workspace", "child_execution_workspace", "child_branch", "child_base_commit":
		if len(parts) != 1 {
			return []Diagnostic{{Code: "template.node_field_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("%s does not support nested fields", parts[0])}}
		}
		if source.WorkflowRun == nil {
			return []Diagnostic{{Code: "template.node_field_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("%s is only available on governed child workflow nodes", parts[0])}}
		}
	case "output":
		if len(parts) == 1 {
			return nil
		}
		if source.OutputFormat == nil {
			return []Diagnostic{{Code: "template.output_untyped", Severity: "warning", Path: path, Message: fmt.Sprintf("nested output reference to node %q cannot be checked because it has no output_format", source.ID), Hint: "declare output_format on the producer"}}
		}
		if ok, typed := schemaPath(*source.OutputFormat, parts[1:]); !ok {
			return []Diagnostic{{Code: "template.output_path_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("output path %q is not declared by node %q output_format", strings.Join(parts[1:], "."), source.ID)}}
		} else if !typed {
			return []Diagnostic{{Code: "template.output_path_open", Severity: "warning", Path: path, Message: fmt.Sprintf("output path %q passes through open additional properties and cannot be fully checked", strings.Join(parts[1:], "."))}}
		}
	case "artifacts":
		if len(parts) < 2 {
			return nil
		}
		if _, err := strconv.Atoi(parts[1]); err != nil && source.OutputType != "" && parts[1] != source.OutputType {
			return []Diagnostic{{Code: "template.artifact_type_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("node %q declares artifact type %q, not %q", source.ID, source.OutputType, parts[1])}}
		}
		if len(parts) >= 3 {
			valid := map[string]bool{"id": true, "type": true, "mime": true, "path": true, "sha256": true, "size": true, "producer_run_id": true, "producer_node_id": true, "attempt": true}
			if !valid[parts[2]] {
				return []Diagnostic{{Code: "template.artifact_field_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("unknown artifact field %q", parts[2])}}
			}
		}
	default:
		return []Diagnostic{{Code: "template.node_field_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("unknown node field %q", parts[0])}}
	}
	_ = expression
	return nil
}

func schemaPath(schema spec.OutputFormat, parts []string) (bool, bool) {
	current := schema
	for _, part := range parts {
		switch current.Type {
		case "object":
			next, ok := current.Properties[part]
			if !ok {
				if current.AdditionalProperties == nil || *current.AdditionalProperties {
					return true, false
				}
				return false, true
			}
			current = next
		case "array":
			if _, err := strconv.Atoi(part); err != nil || current.Items == nil {
				return false, true
			}
			current = *current.Items
		default:
			return false, true
		}
	}
	return true, true
}

func analyzeWhen(value, path string, current spec.Node, byID, local map[string]spec.Node) []Diagnostic {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if parts := splitWhenLogical(value, "||"); len(parts) > 1 {
		var diagnostics []Diagnostic
		for index, part := range parts {
			diagnostics = append(diagnostics, analyzeWhen(part, fmt.Sprintf("%s.or[%d]", path, index), current, byID, local)...)
		}
		return diagnostics
	}
	if parts := splitWhenLogical(value, "&&"); len(parts) > 1 {
		var diagnostics []Diagnostic
		for index, part := range parts {
			diagnostics = append(diagnostics, analyzeWhen(part, fmt.Sprintf("%s.and[%d]", path, index), current, byID, local)...)
		}
		return diagnostics
	}
	for _, operator := range []string{"==", "!="} {
		if index := strings.Index(value, operator); index >= 0 {
			left := strings.TrimSpace(value[:index])
			ref, err := flowref.Parse(left, flowref.When)
			if err != nil {
				return []Diagnostic{{Code: "when.reference_invalid", Severity: "error", Path: path, Message: err.Error()}}
			}
			if ref.Kind == flowref.KindNode {
				source, ok := byID[ref.NodeID]
				if !ok {
					return []Diagnostic{{Code: "when.node_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("when references unknown node %q", ref.NodeID)}}
				}
				if local[source.ID].ID != "" && !dependsOn(current.ID, source.ID, local, map[string]bool{}) {
					return []Diagnostic{{Code: "when.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("node %q is not upstream of node %q", source.ID, current.ID)}}
				}
				return validateNodePath(source, ref.Path, path, expression{})
			}
			if ref.Kind == flowref.KindInput || (ref.Kind == flowref.KindBare && ref.Name == "ARGUMENTS") {
				return nil
			}
			return []Diagnostic{{Code: "when.reference_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("unsupported when reference %q", left)}}
		}
	}
	return []Diagnostic{{Code: "when.expression_invalid", Severity: "error", Path: path, Message: "when supports == and != comparisons joined by && or ||"}}
}

func splitWhenLogical(expr, operator string) []string {
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

func dependsOn(nodeID, target string, byID map[string]spec.Node, seen map[string]bool) bool {
	if seen[nodeID] {
		return false
	}
	seen[nodeID] = true
	node, ok := byID[nodeID]
	if !ok {
		return false
	}
	for _, dependency := range node.DependsOn {
		if dependency == target || dependsOn(dependency, target, byID, seen) {
			return true
		}
	}
	return false
}
