// Package flowref implements the single reference grammar shared by workflow
// templates, when expressions and deterministic shell/script surfaces.
package flowref

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type Surface uint8

const (
	NonShell Surface = iota
	Shell
	ScriptArg
	ScriptEnv
	When
)

type Kind uint8

const (
	KindNode Kind = iota
	KindInput
	KindFanout
	KindLoopPrevious
	KindBare
	KindApproval
)

type Reference struct {
	Kind     Kind
	NodeID   string
	Path     []string
	Optional bool
	Default  string
	Name     string
}

var (
	nameRE     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
	segmentRE  = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_-]*|[0-9]+)$`)
	artifactRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	metaFields = map[string]bool{
		"id": true, "type": true, "mime": true, "path": true, "sha256": true,
		"size": true, "producer_run_id": true, "producer_node_id": true, "attempt": true,
	}
	stateFields = map[string]bool{
		"status": true, "exit_code": true, "child_run_id": true,
		"child_control_workspace": true, "child_execution_workspace": true,
		"child_branch": true, "child_base_commit": true,
	}
)

var reserved = map[string]bool{
	"ARGUMENTS": true, "ARTIFACTS_DIR": true, "BASE_BRANCH": true,
	"INPUTS": true, "LOOP_PREV": true, "FEEDBACK": true, "FANOUT": true,
}

// Parse parses one complete Takt reference. Literal text should be handled by
// Render, not passed to Parse.
func Parse(source string, surface Surface) (Reference, error) {
	if source == "" {
		return Reference{}, fmt.Errorf("empty reference")
	}
	if source == "$$" && surface != Shell {
		return Reference{}, nil
	}
	if !strings.HasPrefix(source, "$") {
		return Reference{}, fmt.Errorf("reference must start with $")
	}
	if strings.HasPrefix(source, "${") {
		return Reference{}, fmt.Errorf("legacy braced references are not supported")
	}
	if strings.HasPrefix(source, "$$") {
		return Reference{}, fmt.Errorf("literal $$ is only valid inside rendered text")
	}
	body, optional, def, err := splitSuffix(source[1:])
	if err != nil {
		return Reference{}, err
	}
	if body == "" {
		return Reference{}, fmt.Errorf("empty reference")
	}
	parts := strings.Split(body, ".")
	for _, part := range parts {
		if part == "" {
			return Reference{}, fmt.Errorf("reference contains an empty segment")
		}
	}
	if len(parts) == 1 {
		if reserved[parts[0]] {
			switch parts[0] {
			case "ARGUMENTS", "ARTIFACTS_DIR", "BASE_BRANCH", "FEEDBACK":
				return Reference{Kind: KindBare, Name: parts[0], Optional: optional, Default: def}, nil
			default:
				return Reference{}, fmt.Errorf("%s requires a suffix", parts[0])
			}
		}
		if parts[0] == "USER_MESSAGE" {
			return Reference{}, fmt.Errorf("legacy reference $USER_MESSAGE is not supported")
		}
		if surface == Shell && isNativeShellVariable(parts[0]) {
			return Reference{}, fmt.Errorf("%s is a shell variable, not a Takt reference", parts[0])
		}
		return Reference{}, fmt.Errorf("unknown bare reference %q", parts[0])
	}

	if parts[0] == "INPUTS" {
		if len(parts) < 2 || !nameRE.MatchString(parts[1]) {
			return Reference{}, fmt.Errorf("invalid $INPUTS reference")
		}
		for _, part := range parts[2:] {
			if !segmentRE.MatchString(part) {
				return Reference{}, fmt.Errorf("invalid input path segment %q", part)
			}
		}
		return Reference{Kind: KindInput, Name: parts[1], Path: append([]string(nil), parts[2:]...), Optional: optional, Default: def}, nil
	}
	if parts[0] == "FANOUT" {
		if len(parts) < 2 {
			return Reference{}, fmt.Errorf("$FANOUT requires a field")
		}
		if parts[1] != "item" && parts[1] != "index" && parts[1] != "total" && !nameRE.MatchString(parts[1]) {
			return Reference{}, fmt.Errorf("invalid fan-out field %q", parts[1])
		}
		if (parts[1] == "index" || parts[1] == "total") && len(parts) != 2 {
			return Reference{}, fmt.Errorf("fan-out %s does not accept a path", parts[1])
		}
		for _, part := range parts[2:] {
			if !segmentRE.MatchString(part) {
				return Reference{}, fmt.Errorf("invalid fan-out path segment %q", part)
			}
		}
		return Reference{Kind: KindFanout, Name: parts[1], Path: append([]string(nil), parts[2:]...), Optional: optional, Default: def}, nil
	}
	if parts[0] == "LOOP_PREV" {
		if len(parts) < 3 || !nameRE.MatchString(parts[1]) || parts[2] != "output" {
			return Reference{}, fmt.Errorf("invalid $LOOP_PREV reference")
		}
		for _, part := range parts[3:] {
			if !segmentRE.MatchString(part) {
				return Reference{}, fmt.Errorf("invalid previous-output path segment %q", part)
			}
		}
		return Reference{Kind: KindLoopPrevious, NodeID: parts[1], Path: append([]string(nil), parts[2:]...), Optional: optional, Default: def}, nil
	}

	node := parts[0]
	if !nameRE.MatchString(node) {
		return Reference{}, fmt.Errorf("invalid node id %q", node)
	}
	if reserved[node] {
		return Reference{}, fmt.Errorf("reserved node id %q", node)
	}
	if len(parts) < 2 {
		return Reference{}, fmt.Errorf("node reference %q requires a field", node)
	}
	if parts[1] == "artifacts" {
		if len(parts) < 4 {
			return Reference{}, fmt.Errorf("artifact reference requires type and metadata field")
		}
		meta := parts[len(parts)-1]
		if !metaFields[meta] {
			return Reference{}, fmt.Errorf("unknown artifact metadata field %q", meta)
		}
		typeParts := parts[2 : len(parts)-1]
		artifactType := strings.Join(typeParts, ".")
		if artifactType == "" || !artifactRE.MatchString(artifactType) || allDigits(artifactType) {
			return Reference{}, fmt.Errorf("invalid artifact type %q", artifactType)
		}
		return Reference{Kind: KindNode, NodeID: node, Path: []string{"artifacts", artifactType, meta}, Optional: optional, Default: def}, nil
	}
	if parts[1] == "output" {
		for _, part := range parts[2:] {
			if !segmentRE.MatchString(part) {
				return Reference{}, fmt.Errorf("invalid output path segment %q", part)
			}
		}
		return Reference{Kind: KindNode, NodeID: node, Path: append([]string(nil), parts[1:]...), Optional: optional, Default: def}, nil
	}
	if len(parts) == 2 && stateFields[parts[1]] {
		return Reference{Kind: KindNode, NodeID: node, Path: []string{parts[1]}, Optional: optional, Default: def}, nil
	}
	if len(parts) == 2 && parts[1] == "output" {
		return Reference{Kind: KindApproval, NodeID: node, Path: []string{"output"}, Optional: optional, Default: def}, nil
	}
	return Reference{}, fmt.Errorf("unknown node reference field %q", strings.Join(parts[1:], "."))
}

func splitSuffix(body string) (string, bool, string, error) {
	if i := strings.Index(body, ":-"); i >= 0 {
		if i == 0 || strings.ContainsAny(body[i+2:], " \t\r\n") || strings.Contains(body[i+2:], ":-") {
			return "", false, "", fmt.Errorf("invalid default suffix")
		}
		return body[:i], false, body[i+2:], nil
	}
	if strings.HasSuffix(body, "?") {
		return strings.TrimSuffix(body, "?"), true, "", nil
	}
	return body, false, "", nil
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isNativeShellVariable(value string) bool {
	for _, r := range value {
		if !(unicode.IsUpper(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return value != ""
}

// Render resolves all Takt references in source in one pass. Substituted
// values are never scanned again.
func Render(source string, surface Surface, resolve func(Reference) (string, bool)) (string, error) {
	var out strings.Builder
	for i := 0; i < len(source); {
		if source[i] != '$' {
			out.WriteByte(source[i])
			i++
			continue
		}
		if i+1 < len(source) && source[i+1] == '$' {
			if surface == Shell {
				out.WriteString("$$")
			} else {
				out.WriteByte('$')
			}
			i += 2
			continue
		}
		if source[i] == '$' && i+1 < len(source) && source[i+1] == '{' {
			return "", fmt.Errorf("legacy braced references are not supported")
		}
		end := referenceEnd(source, i+1)
		if end == i+1 {
			if surface == Shell && (i+1 < len(source) && (source[i+1] == '?' || source[i+1] == '(')) {
				out.WriteByte('$')
				i++
				continue
			}
			if surface == Shell && i+1 < len(source) && (source[i+1] == '!' || source[i+1] == '#' || source[i+1] == '@' || source[i+1] == '*') {
				out.WriteString(source[i : i+2])
				i += 2
				continue
			}
			return "", fmt.Errorf("invalid reference at byte %d", i)
		}
		token := source[i:end]
		ref, err := Parse(token, surface)
		if err != nil {
			if surface == Shell && isPreservedShellToken(token, source, i, end) {
				out.WriteString(token)
				i = end
				continue
			}
			return "", err
		}
		if surface == Shell && (ref.Kind != KindBare) && isQuoted(source, i, end) {
			return "", fmt.Errorf("shell reference %s must not be double quoted", token)
		}
		if surface == Shell && ref.Kind == KindBare {
			// Bare shell contexts are supplied through the process environment.
			out.WriteString(token)
			i = end
			continue
		}
		value, ok := "", false
		if resolve != nil {
			value, ok = resolve(ref)
		}
		if !ok || (value == "" && ref.Default != "") {
			if ref.Default != "" {
				value = ref.Default
				ok = true
			} else if ref.Optional {
				value = ""
				ok = true
			}
		}
		if !ok {
			return "", fmt.Errorf("unresolved reference %q", token)
		}
		if surface == Shell {
			value = shellQuote(value)
		}
		out.WriteString(value)
		i = end
	}
	return out.String(), nil
}

// Scan returns references in source without resolving their values.
func Scan(source string, surface Surface) ([]Reference, error) {
	refs := make([]Reference, 0)
	_, err := Render(source, surface, func(ref Reference) (string, bool) {
		refs = append(refs, ref)
		return "", true
	})
	return refs, err
}

func referenceEnd(source string, start int) int {
	i := start
	for i < len(source) {
		c := source[i]
		if c == ':' {
			if i+1 < len(source) && source[i+1] == '-' {
				i++
				continue
			}
			break
		}
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == '?' || c == ':' {
			i++
			continue
		}
		break
	}
	// Include the default marker's second byte and token; the scanner above
	// already consumes ':' and the remainder, so no special case is needed.
	return i
}

func isPreservedShellToken(token, source string, start, end int) bool {
	if token == "$USER_MESSAGE" {
		return false
	}
	if strings.HasPrefix(token, "${") {
		return false
	}
	if len(token) > 1 && token[1] >= '0' && token[1] <= '9' {
		return true
	}
	if len(token) > 1 && nameRE.MatchString(token[1:]) && !strings.Contains(token, ".") {
		if token == "$USER_MESSAGE" {
			return false
		}
		if end < len(source) && (source[end] == '.' || source[end] == '-' || source[end] == '_') {
			return false
		}
		return true
	}
	return false
}

func isQuoted(source string, start, end int) bool {
	return start > 0 && end < len(source) && source[start-1] == '"' && source[end] == '"'
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
