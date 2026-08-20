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
	KindMatrix
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
	"ARGUMENTS": true, "ARTIFACTS_DIR": true, "BASE_BRANCH": true, "TAKT_WORKSPACE": true,
	"INPUTS": true, "LOOP_PREV": true, "FEEDBACK": true, "FANOUT": true, "MATRIX": true,
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
			case "ARGUMENTS", "ARTIFACTS_DIR", "BASE_BRANCH", "FEEDBACK", "TAKT_WORKSPACE":
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
	if parts[0] == "MATRIX" {
		if surface == When {
			return Reference{}, fmt.Errorf("$MATRIX is not valid in when; compute a value in a node first")
		}
		if len(parts) < 2 {
			return Reference{}, fmt.Errorf("$MATRIX requires a field")
		}
		if parts[1] != "item" && parts[1] != "index" && parts[1] != "total" {
			return Reference{}, fmt.Errorf("invalid matrix field %q", parts[1])
		}
		if (parts[1] == "index" || parts[1] == "total") && len(parts) != 2 {
			return Reference{}, fmt.Errorf("matrix %s does not accept a path", parts[1])
		}
		for _, part := range parts[2:] {
			if !segmentRE.MatchString(part) {
				return Reference{}, fmt.Errorf("invalid matrix path segment %q", part)
			}
		}
		return Reference{Kind: KindMatrix, Name: parts[1], Path: append([]string(nil), parts[2:]...), Optional: optional, Default: def}, nil
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
		if len(parts) < 3 {
			return Reference{}, fmt.Errorf("artifact reference requires type")
		}
		typeParts := parts[2:]
		meta := ""
		if len(parts) > 3 && metaFields[parts[len(parts)-1]] {
			meta = parts[len(parts)-1]
			typeParts = parts[2 : len(parts)-1]
		}
		artifactType := strings.Join(typeParts, ".")
		if artifactType == "" || !artifactRE.MatchString(artifactType) || allDigits(artifactType) {
			return Reference{}, fmt.Errorf("invalid artifact type %q", artifactType)
		}
		path := []string{"artifacts", artifactType}
		if meta != "" {
			path = append(path, meta)
		}
		return Reference{Kind: KindNode, NodeID: node, Path: path, Optional: optional, Default: def}, nil
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
	if surface == Shell {
		return renderShell(source, resolve)
	}
	return renderGeneric(source, surface, resolve)
}

func renderGeneric(source string, surface Surface, resolve func(Reference) (string, bool)) (string, error) {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(source); {
		if source[i] == '\\' && quote != '\'' && i+1 < len(source) {
			out.WriteByte(source[i])
			out.WriteByte(source[i+1])
			i += 2
			continue
		}
		if source[i] == '"' && quote != '\'' {
			if quote == '"' {
				quote = 0
			} else {
				quote = '"'
			}
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '\'' && quote != '"' {
			if quote == '\'' {
				quote = 0
			} else {
				quote = '\''
			}
			out.WriteByte(source[i])
			i++
			continue
		}
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
			if surface == Shell {
				if end, ok := nativeBracedShellVariable(source, i); ok {
					out.WriteString(source[i:end])
					i = end
					continue
				}
			}
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
		if surface == Shell && (ref.Kind != KindBare) && quote == '"' {
			return "", fmt.Errorf("shell reference %s must not be double quoted", token)
		}
		if surface == Shell && ref.Kind == KindBare {
			// Bare Takt variables are supplied through the process environment.
			// Resolve them here as a fail-closed existence check while retaining
			// native shell expansion in the command.
			value, ok := "", false
			if resolve != nil {
				value, ok = resolve(ref)
			}
			if !ok {
				if ref.Default != "" {
					value, ok = ref.Default, true
				} else if ref.Optional {
					value, ok = "", true
				}
			}
			if !ok {
				return "", fmt.Errorf("unresolved reference %q", token)
			}
			if ref.Optional || ref.Default != "" {
				out.WriteString(shellQuote(value))
			} else {
				out.WriteString(token)
			}
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
			if quote == '\'' {
				value = shellEscapeSingleQuoted(value)
			} else {
				value = shellQuote(value)
			}
		}
		out.WriteString(value)
		i = end
	}
	return out.String(), nil
}

type shellFrame struct {
	kind        byte // root, command substitution, arithmetic substitution, or backtick
	quote       byte
	parens      int
	comment     bool
	word        string
	casePending bool
	caseStack   []shellCaseState
}

type shellCaseState struct {
	pattern bool
}

type shellHeredoc struct {
	bodyStart  int
	bodyEnd    int
	contentEnd int
}

type shellLexer struct {
	frames   []shellFrame
	heredocs []shellHeredoc
}

func newShellLexer() *shellLexer {
	return &shellLexer{frames: []shellFrame{{kind: 'r'}}}
}

func (l *shellLexer) frame() *shellFrame {
	return &l.frames[len(l.frames)-1]
}

func (l *shellLexer) quote() byte {
	return l.frame().quote
}

func (l *shellLexer) push(kind byte, parens int) {
	l.frames = append(l.frames, shellFrame{kind: kind, parens: parens})
}

func (l *shellLexer) pop() {
	if len(l.frames) > 1 {
		l.frames = l.frames[:len(l.frames)-1]
	}
}

func (f *shellFrame) finishShellWord() {
	if f.word == "" {
		return
	}
	switch f.word {
	case "case":
		f.casePending = true
	case "in":
		if f.casePending {
			f.caseStack = append(f.caseStack, shellCaseState{pattern: true})
			f.casePending = false
		}
	case "esac":
		if len(f.caseStack) > 0 {
			f.caseStack = f.caseStack[:len(f.caseStack)-1]
		}
		f.casePending = false
	}
	f.word = ""
}

func isShellWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || strings.ContainsRune("_-.=/:", rune(value))
}

func (f *shellFrame) casePattern() bool {
	return len(f.caseStack) > 0 && f.caseStack[len(f.caseStack)-1].pattern
}

func (l *shellLexer) addHeredoc(value shellHeredoc) {
	if value.bodyStart < 0 {
		return
	}
	l.heredocs = append(l.heredocs, value)
}

func (l *shellLexer) takeHeredoc(start int) (shellHeredoc, bool) {
	for index, value := range l.heredocs {
		if value.bodyStart != start {
			continue
		}
		l.heredocs = append(l.heredocs[:index], l.heredocs[index+1:]...)
		return value, true
	}
	return shellHeredoc{}, false
}

func renderShell(source string, resolve func(Reference) (string, bool)) (string, error) {
	var out strings.Builder
	lexer := newShellLexer()
	for i := 0; i < len(source); {
		if heredoc, ok := lexer.takeHeredoc(i); ok {
			if err := validateHeredocBody(source[heredoc.bodyStart:heredoc.contentEnd]); err != nil {
				return "", err
			}
			out.WriteString(source[heredoc.bodyStart:heredoc.bodyEnd])
			i = heredoc.bodyEnd
			continue
		}

		frame := lexer.frame()
		if frame.comment {
			out.WriteByte(source[i])
			if source[i] == '\n' {
				frame.comment = false
			}
			i++
			continue
		}
		if source[i] == '\\' && frame.quote != '\'' && i+1 < len(source) {
			if frame.quote == 0 {
				frame.finishShellWord()
			}
			out.WriteByte(source[i])
			out.WriteByte(source[i+1])
			i += 2
			continue
		}
		if source[i] == '#' && shellCommentStart(source, i, frame.quote) {
			frame.finishShellWord()
			frame.comment = true
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '"' && frame.quote != '\'' {
			frame.finishShellWord()
			if frame.quote == '"' {
				frame.quote = 0
			} else {
				frame.quote = '"'
			}
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '\'' && frame.quote != '"' {
			frame.finishShellWord()
			if frame.quote == '\'' {
				frame.quote = 0
			} else {
				frame.quote = '\''
			}
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '`' && frame.quote != '\'' {
			frame.finishShellWord()
			if frame.kind == 'b' {
				lexer.pop()
			} else {
				lexer.push('b', 0)
			}
			out.WriteByte(source[i])
			i++
			continue
		}
		if frame.quote == 0 && isShellWordByte(source[i]) {
			frame.word += string(source[i])
			out.WriteByte(source[i])
			i++
			continue
		}
		frame.finishShellWord()
		if source[i] == ';' && frame.quote == 0 {
			if len(frame.caseStack) > 0 && i+1 < len(source) && source[i+1] == ';' {
				frame.caseStack[len(frame.caseStack)-1].pattern = true
			}
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == ')' && frame.quote == 0 && frame.kind == 'c' {
			if frame.casePattern() {
				frame.caseStack[len(frame.caseStack)-1].pattern = false
				out.WriteByte(source[i])
				i++
				continue
			}
			if len(frame.caseStack) > 0 && frame.parens <= 1 {
				out.WriteByte(source[i])
				i++
				continue
			}
		}
		if source[i] == ')' && frame.quote == 0 && (frame.kind == 'c' || frame.kind == 'a') {
			frame.parens--
			out.WriteByte(source[i])
			i++
			if frame.parens == 0 {
				lexer.pop()
			}
			continue
		}
		if source[i] == '(' && frame.quote == 0 && frame.kind == 'c' && frame.casePattern() {
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '(' && frame.quote == 0 && (frame.kind == 'c' || frame.kind == 'a') {
			frame.parens++
			out.WriteByte(source[i])
			i++
			continue
		}
		if source[i] == '<' && i+1 < len(source) && source[i+1] == '<' && frame.quote == 0 {
			if heredoc, ok := parseHeredoc(source, i); ok {
				lexer.addHeredoc(heredoc)
			}
		}
		if source[i] != '$' {
			out.WriteByte(source[i])
			i++
			continue
		}
		if frame.quote == '\'' && i+1 < len(source) && source[i+1] == '(' {
			out.WriteByte(source[i])
			i++
			continue
		}
		if i+1 < len(source) && source[i+1] == '$' {
			out.WriteString("$$")
			i += 2
			continue
		}
		if i+1 < len(source) && source[i+1] == '(' && frame.quote != '\'' {
			if i+2 < len(source) && source[i+2] == '(' {
				lexer.push('a', 2)
				out.WriteString("$((")
				i += 3
			} else {
				lexer.push('c', 1)
				out.WriteString("$(")
				i += 2
			}
			continue
		}
		if i+1 < len(source) && source[i+1] == '{' {
			if end, ok := nativeBracedShellVariable(source, i); ok {
				out.WriteString(source[i:end])
				i = end
				continue
			}
			return "", fmt.Errorf("legacy braced references are not supported")
		}
		end := referenceEnd(source, i+1)
		if end == i+1 {
			if i+1 < len(source) && (source[i+1] == '?' || source[i+1] == '!' || source[i+1] == '#' || source[i+1] == '@' || source[i+1] == '*' || (source[i+1] >= '0' && source[i+1] <= '9')) {
				out.WriteString(source[i : i+2])
				i += 2
				continue
			}
			return "", fmt.Errorf("invalid reference at byte %d", i)
		}
		token := source[i:end]
		ref, err := Parse(token, Shell)
		if err != nil {
			if isPreservedShellToken(token, source, i, end) {
				out.WriteString(token)
				i = end
				continue
			}
			return "", err
		}
		if frame.kind == 'a' {
			return "", fmt.Errorf("Takt references are not supported inside shell arithmetic")
		}
		if ref.Kind != KindBare && frame.quote == '"' {
			return "", fmt.Errorf("shell reference %s must not be double quoted", token)
		}
		if ref.Kind == KindBare {
			value, ok := "", false
			if resolve != nil {
				value, ok = resolve(ref)
			}
			if !ok {
				if ref.Default != "" {
					value, ok = ref.Default, true
				} else if ref.Optional {
					value, ok = "", true
				}
			}
			if !ok {
				return "", fmt.Errorf("unresolved reference %q", token)
			}
			if ref.Optional || ref.Default != "" {
				out.WriteString(shellQuote(value))
			} else {
				out.WriteString(token)
			}
			i = end
			continue
		}
		if frame.kind == 'c' && len(frame.caseStack) > 0 {
			return "", fmt.Errorf("Takt references are not supported inside shell case substitutions")
		}
		value, ok := "", false
		if resolve != nil {
			value, ok = resolve(ref)
		}
		if !ok || (value == "" && ref.Default != "") {
			if ref.Default != "" {
				value, ok = ref.Default, true
			} else if ref.Optional {
				value, ok = "", true
			}
		}
		if !ok {
			return "", fmt.Errorf("unresolved reference %q", token)
		}
		if frame.quote == '\'' {
			value = shellEscapeSingleQuoted(value)
		} else {
			value = shellQuote(value)
		}
		out.WriteString(value)
		i = end
	}
	return out.String(), nil
}

func shellCommentStart(source string, index int, quote byte) bool {
	if quote != 0 || index == 0 {
		return quote == 0
	}
	previous := source[index-1]
	return unicode.IsSpace(rune(previous)) || strings.ContainsRune(";|&()<>", rune(previous))
}

func parseHeredoc(source string, operator int) (shellHeredoc, bool) {
	headerEnd := strings.IndexByte(source[operator:], '\n')
	if headerEnd < 0 {
		return shellHeredoc{}, false
	}
	headerEnd += operator
	index := operator + 2
	stripTabs := false
	if index < headerEnd && source[index] == '-' {
		stripTabs = true
		index++
	}
	for index < headerEnd && (source[index] == ' ' || source[index] == '\t') {
		index++
	}
	if index >= headerEnd {
		return shellHeredoc{}, false
	}
	var delimiter strings.Builder
	if source[index] == '\'' || source[index] == '"' {
		quote := source[index]
		index++
		for index < headerEnd && source[index] != quote {
			delimiter.WriteByte(source[index])
			index++
		}
		if index >= headerEnd {
			return shellHeredoc{}, false
		}
		index++
	} else {
		for index < headerEnd && source[index] != ' ' && source[index] != '\t' {
			delimiter.WriteByte(source[index])
			index++
		}
	}
	value := delimiter.String()
	if value == "" {
		return shellHeredoc{}, false
	}
	bodyStart := headerEnd + 1
	for lineStart := bodyStart; lineStart <= len(source); {
		lineEnd := strings.IndexByte(source[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(source)
		} else {
			lineEnd += lineStart
		}
		line := source[lineStart:lineEnd]
		compare := line
		if stripTabs {
			compare = strings.TrimLeft(compare, "\t")
		}
		if compare == value {
			end := lineEnd
			if end < len(source) {
				end++
			}
			return shellHeredoc{bodyStart: bodyStart, bodyEnd: end, contentEnd: lineStart}, true
		}
		if lineEnd >= len(source) {
			break
		}
		lineStart = lineEnd + 1
	}
	return shellHeredoc{bodyStart: bodyStart, bodyEnd: len(source), contentEnd: len(source)}, true
}

func validateHeredocBody(body string) error {
	for index := 0; index < len(body); {
		if body[index] != '$' {
			index++
			continue
		}
		if index+1 >= len(body) {
			break
		}
		if body[index+1] == '$' {
			index += 2
			continue
		}
		if body[index+1] == '{' {
			if end, ok := nativeBracedShellVariable(body, index); ok {
				index = end
				continue
			}
			return fmt.Errorf("shell Takt references are not supported in heredoc bodies")
		}
		end := referenceEnd(body, index+1)
		if end == index+1 {
			index++
			continue
		}
		token := body[index:end]
		if _, err := Parse(token, Shell); err == nil {
			return fmt.Errorf("shell Takt references are not supported in heredoc bodies")
		}
		if isPreservedShellToken(token, body, index, end) {
			index = end
			continue
		}
		index++
	}
	return nil
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
	if start >= len(source) || source[start] == '?' {
		return start
	}
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
	if len(token) > 1 && reserved[token[1:]] {
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

func nativeBracedShellVariable(source string, start int) (int, bool) {
	if start+3 >= len(source) || source[start+1] != '{' {
		return 0, false
	}
	end := strings.IndexByte(source[start+2:], '}')
	if end < 0 {
		return 0, false
	}
	end += start + 2
	name := source[start+2 : end]
	if !isNativeShellVariable(name) || reserved[name] {
		return 0, false
	}
	return end + 1, true
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func shellEscapeSingleQuoted(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}
