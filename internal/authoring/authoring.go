package authoring

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"takt/internal/command"
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

var templateRE = regexp.MustCompile(`\$\{([^}]+)\}`)

// Analyze performs static checks that are intentionally stricter and more
// helpful than runtime rendering. It never mutates the workflow.
func Analyze(wf *spec.Workflow, resolver command.Resolver) []Diagnostic {
	if wf == nil {
		return []Diagnostic{{Code: "workflow.required", Severity: "error", Message: "workflow is required"}}
	}
	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, analyzeScope(wf.Nodes, "nodes", resolver, false, nil)...)
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

func analyzeScope(nodes []spec.Node, scope string, resolver command.Resolver, insideLoop bool, inherited map[string]spec.Node) []Diagnostic {
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
			diagnostics = append(diagnostics, analyzeTemplate(field.value, field.path, node, byID, local, insideLoop)...)
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
			diagnostics = append(diagnostics, analyzeScope(node.LoopGroup.Nodes, path+".loop_group.nodes", resolver, true, childInherited)...)
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

func analyzeTemplate(value, path string, current spec.Node, byID, local map[string]spec.Node, insideLoop bool) []Diagnostic {
	var diagnostics []Diagnostic
	for _, match := range templateRE.FindAllStringSubmatch(value, -1) {
		expression, err := parseExpression(match[1])
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "template.invalid", Severity: "error", Path: path, Message: err.Error()})
			continue
		}
		key := expression.path
		if key == "input" || key == "feedback" || (key == "tool" && strings.Contains(path, ".tool_approval.message")) {
			continue
		}
		parts := strings.Split(key, ".")
		if len(parts) == 2 && parts[0] == "approvals" {
			target, ok := byID[parts[1]]
			if !ok || target.Approval == nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.approval_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("template references unknown approval %q", parts[1])})
			} else if _, inherited := local[target.ID]; inherited && !dependsOn(current.ID, target.ID, local, map[string]bool{}) {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("approval %q is not an upstream dependency of node %q", target.ID, current.ID)})
			}
			continue
		}
		if len(parts) >= 4 && parts[0] == "loop" && parts[1] == "previous" {
			if !insideLoop {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.loop_outside_loop", Severity: "error", Path: path, Message: "loop.previous is available only inside loop_group"})
			}
			continue
		}
		if len(parts) >= 2 && (parts[0] == "fanout" || (current.WorkflowRun != nil && current.WorkflowRun.FanOut != nil && parts[0] == current.WorkflowRun.FanOut.As)) {
			if current.WorkflowRun == nil || current.WorkflowRun.FanOut == nil {
				diagnostics = append(diagnostics, Diagnostic{Code: "template.fanout_outside_fanout", Severity: "error", Path: path, Message: "fanout variables are available only in workflow.fan_out input"})
			}
			continue
		}
		if len(parts) < 3 || parts[0] != "nodes" {
			diagnostics = append(diagnostics, Diagnostic{Code: "template.unknown_root", Severity: "error", Path: path, Message: fmt.Sprintf("unsupported template expression %q", key), Hint: "use input, feedback, approvals.<id>, nodes.<id>.<field>, optional ${path?}, or default ${path:-value}"})
			continue
		}
		source, ok := byID[parts[1]]
		if !ok {
			diagnostics = append(diagnostics, Diagnostic{Code: "template.node_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("template references unknown node %q", parts[1])})
			continue
		}
		if source.ID == current.ID || (local[source.ID].ID != "" && !dependsOn(current.ID, source.ID, local, map[string]bool{})) {
			diagnostics = append(diagnostics, Diagnostic{Code: "template.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("node %q is not an upstream dependency of node %q", source.ID, current.ID), Hint: "add it to depends_on directly or transitively"})
			continue
		}
		diagnostics = append(diagnostics, validateNodePath(source, parts[2:], path, expression)...)
	}
	return diagnostics
}

func validateNodePath(source spec.Node, parts []string, path string, expression expression) []Diagnostic {
	if len(parts) == 0 {
		return []Diagnostic{{Code: "template.node_field_missing", Severity: "error", Path: path, Message: "node reference requires output, status, exit_code, or artifacts"}}
	}
	switch parts[0] {
	case "status", "exit_code":
		if len(parts) != 1 {
			return []Diagnostic{{Code: "template.node_field_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("%s does not support nested fields", parts[0])}}
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
	for _, operator := range []string{"==", "!="} {
		if index := strings.Index(value, operator); index >= 0 {
			left := strings.TrimSpace(value[:index])
			if strings.HasPrefix(left, "nodes.") {
				parts := strings.Split(left, ".")
				if len(parts) < 3 {
					return []Diagnostic{{Code: "when.reference_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("invalid when reference %q", left)}}
				}
				source, ok := byID[parts[1]]
				if !ok {
					return []Diagnostic{{Code: "when.node_unknown", Severity: "error", Path: path, Message: fmt.Sprintf("when references unknown node %q", parts[1])}}
				}
				if local[source.ID].ID != "" && !dependsOn(current.ID, source.ID, local, map[string]bool{}) {
					return []Diagnostic{{Code: "when.reference_not_upstream", Severity: "error", Path: path, Message: fmt.Sprintf("node %q is not upstream of node %q", source.ID, current.ID)}}
				}
				return validateNodePath(source, parts[2:], path, expression{})
			}
			if left == "inputs.message" || left == "inputs.input" {
				return nil
			}
			return []Diagnostic{{Code: "when.reference_invalid", Severity: "error", Path: path, Message: fmt.Sprintf("unsupported when reference %q", left)}}
		}
	}
	return []Diagnostic{{Code: "when.expression_invalid", Severity: "error", Path: path, Message: "when supports only == and != comparisons"}}
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
