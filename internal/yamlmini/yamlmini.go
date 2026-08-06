package yamlmini

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

type line struct {
	indent  int
	text    string
	raw     string
	num     int
	blank   bool
	comment bool
	content bool
}

// Unmarshal parses the documented Takt YAML subset and JSON. The parser keeps
// block-scalar whitespace exactly enough for prompts and shell scripts,
// including blank lines and the |, |-, |+, >, >-, and >+ chomp modes.
func Unmarshal(data []byte, out any) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return fmt.Errorf("empty document")
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var value any
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			return err
		}
		if err := validateKnownFields(value, reflect.TypeOf(out).Elem(), "$"); err != nil {
			return err
		}
		typed := json.NewDecoder(strings.NewReader(trimmed))
		typed.DisallowUnknownFields()
		return typed.Decode(out)
	}
	lines, err := tokenize(string(data))
	if err != nil {
		return err
	}
	p := parser{lines: lines}
	p.skipIgnorable()
	if p.pos >= len(p.lines) {
		return fmt.Errorf("empty YAML document")
	}
	value, err := p.parseBlock(p.lines[p.pos].indent)
	if err != nil {
		return err
	}
	p.skipIgnorable()
	if p.pos != len(p.lines) {
		return fmt.Errorf("unexpected content at line %d", p.lines[p.pos].num)
	}
	if err := validateKnownFields(value, reflect.TypeOf(out).Elem(), "$"); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(encoded)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode YAML document: %w", err)
	}
	return nil
}

func tokenize(src string) ([]line, error) {
	rawLines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	out := make([]line, 0, len(rawLines))
	hasContent := false
	for i, raw := range rawLines {
		if strings.Contains(raw, "\t") {
			return nil, fmt.Errorf("tabs are not allowed in YAML, line %d", i+1)
		}
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		trimmed := strings.TrimSpace(raw)
		ln := line{indent: indent, raw: raw, num: i + 1}
		switch {
		case trimmed == "":
			ln.blank = true
		case strings.HasPrefix(trimmed, "#"):
			ln.comment = true
		default:
			ln.text = stripComment(trimmed)
			if ln.text == "" {
				ln.comment = true
			} else {
				ln.content = true
				hasContent = true
			}
		}
		out = append(out, ln)
	}
	if !hasContent {
		return nil, fmt.Errorf("empty YAML document")
	}
	return out, nil
}

func stripComment(s string) string {
	var quote rune
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote == '"' {
			escaped = true
			continue
		}
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
		if r == '#' && (i == 0 || s[i-1] == ' ') {
			return strings.TrimSpace(s[:i])
		}
	}
	return strings.TrimSpace(s)
}

type parser struct {
	lines []line
	pos   int
}

func (p *parser) skipIgnorable() {
	for p.pos < len(p.lines) && !p.lines[p.pos].content {
		p.pos++
	}
}

func (p *parser) parseBlock(indent int) (any, error) {
	p.skipIgnorable()
	if p.pos >= len(p.lines) {
		return nil, fmt.Errorf("unexpected end of YAML")
	}
	if p.lines[p.pos].indent != indent {
		return nil, fmt.Errorf("unexpected indentation at line %d", p.lines[p.pos].num)
	}
	if strings.HasPrefix(p.lines[p.pos].text, "-") {
		return p.parseSeq(indent)
	}
	return p.parseMap(indent)
}

func (p *parser) parseMap(indent int) (map[string]any, error) {
	out := map[string]any{}
	for {
		p.skipIgnorable()
		if p.pos >= len(p.lines) {
			break
		}
		ln := p.lines[p.pos]
		if ln.indent < indent {
			break
		}
		if ln.indent > indent {
			return nil, fmt.Errorf("unexpected indentation at line %d", ln.num)
		}
		if strings.HasPrefix(ln.text, "-") {
			break
		}
		key, rest, ok := splitKeyValue(ln.text)
		if !ok {
			return nil, fmt.Errorf("expected key: value at line %d", ln.num)
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf("duplicate key %q at line %d", key, ln.num)
		}
		p.pos++
		var value any
		var err error
		if style, chomp, ok := blockHeader(rest); ok {
			value = p.parseBlockScalar(indent, style, chomp)
		} else if rest == "" {
			next := p.nextContent()
			if next >= 0 && p.lines[next].indent > indent {
				p.pos = next
				value, err = p.parseBlock(p.lines[p.pos].indent)
			} else {
				value = nil
			}
		} else {
			value, err = parseScalar(rest)
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", ln.num, err)
		}
		out[key] = value
	}
	return out, nil
}

func (p *parser) parseSeq(indent int) ([]any, error) {
	var out []any
	for {
		p.skipIgnorable()
		if p.pos >= len(p.lines) {
			break
		}
		ln := p.lines[p.pos]
		if ln.indent < indent || ln.indent != indent || !strings.HasPrefix(ln.text, "-") {
			break
		}
		rest := strings.TrimSpace(strings.TrimPrefix(ln.text, "-"))
		p.pos++
		if rest == "" {
			next := p.nextContent()
			if next < 0 || p.lines[next].indent <= indent {
				out = append(out, nil)
				continue
			}
			p.pos = next
			value, err := p.parseBlock(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
			continue
		}
		if key, val, ok := splitKeyValue(rest); ok {
			item := map[string]any{}
			var parsed any
			var err error
			if style, chomp, scalar := blockHeader(val); scalar {
				parsed = p.parseBlockScalar(indent, style, chomp)
			} else if val == "" {
				next := p.nextContent()
				if next >= 0 && p.lines[next].indent > indent {
					p.pos = next
					parsed, err = p.parseBlock(p.lines[p.pos].indent)
				}
			} else {
				parsed, err = parseScalar(val)
			}
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", ln.num, err)
			}
			item[key] = parsed
			next := p.nextContent()
			if next >= 0 && p.lines[next].indent > indent {
				p.pos = next
				more, err := p.parseMap(p.lines[p.pos].indent)
				if err != nil {
					return nil, err
				}
				for k, v := range more {
					if _, exists := item[k]; exists {
						return nil, fmt.Errorf("duplicate key %q in list item", k)
					}
					item[k] = v
				}
			}
			out = append(out, item)
			continue
		}
		value, err := parseScalar(rest)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", ln.num, err)
		}
		out = append(out, value)
	}
	return out, nil
}

func (p *parser) nextContent() int {
	for i := p.pos; i < len(p.lines); i++ {
		if p.lines[i].content {
			return i
		}
	}
	return -1
}

func blockHeader(value string) (style byte, chomp byte, ok bool) {
	switch value {
	case "|", "|-", "|+", ">", ">-", ">+":
		style = value[0]
		if len(value) == 2 {
			chomp = value[1]
		}
		return style, chomp, true
	default:
		return 0, 0, false
	}
}

func (p *parser) parseBlockScalar(parentIndent int, style, chomp byte) string {
	start := p.pos
	end := start
	base := -1
	for end < len(p.lines) {
		ln := p.lines[end]
		if ln.content && ln.indent <= parentIndent {
			break
		}
		if ln.comment && ln.indent <= parentIndent {
			break
		}
		if ln.content || ln.comment {
			if ln.indent <= parentIndent {
				break
			}
			if base < 0 || ln.indent < base {
				base = ln.indent
			}
		}
		end++
	}
	if base < 0 {
		p.pos = end
		return ""
	}
	parts := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		ln := p.lines[i]
		if ln.blank {
			parts = append(parts, "")
			continue
		}
		raw := ln.raw
		if len(raw) >= base {
			raw = raw[base:]
		} else {
			raw = ""
		}
		parts = append(parts, raw)
	}
	p.pos = end
	var value string
	if style == '>' {
		value = foldLines(parts)
	} else {
		value = strings.Join(parts, "\n")
	}
	switch chomp {
	case '-':
		return strings.TrimRight(value, "\n")
	case '+':
		return value + "\n"
	default:
		return strings.TrimRight(value, "\n") + "\n"
	}
}

func foldLines(parts []string) string {
	var out strings.Builder
	for i, part := range parts {
		if i > 0 {
			if parts[i-1] == "" || part == "" {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		}
		out.WriteString(part)
	}
	return out.String()
}

func splitKeyValue(s string) (string, string, bool) {
	var quote rune
	escaped := false
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote == '"' {
			escaped = true
			continue
		}
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
		if r == ':' {
			key := strings.TrimSpace(s[:i])
			if key == "" {
				return "", "", false
			}
			return key, strings.TrimSpace(s[i+1:]), true
		}
	}
	return "", "", false
}

func parseScalar(s string) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if strings.HasPrefix(s, "[") {
		if !strings.HasSuffix(s, "]") {
			return nil, fmt.Errorf("unterminated inline list")
		}
		body := strings.TrimSpace(s[1 : len(s)-1])
		if body == "" {
			return []any{}, nil
		}
		parts, err := splitCSV(body)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, len(parts))
		for _, part := range parts {
			value, err := parseScalar(part)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	}
	if strings.HasPrefix(s, "{") {
		var value any
		if err := json.Unmarshal([]byte(s), &value); err != nil {
			return nil, fmt.Errorf("inline objects must use JSON syntax: %w", err)
		}
		return value, nil
	}
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) || (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		if s[0] == '\'' {
			return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
		}
		value, err := strconv.Unquote(s)
		if err != nil {
			return nil, err
		}
		return value, nil
	}
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if integer, err := strconv.ParseInt(s, 10, 64); err == nil {
		return integer, nil
	}
	if float, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return float, nil
	}
	return s, nil
}

func splitCSV(s string) ([]string, error) {
	var out []string
	start := 0
	var quote rune
	escaped := false
	depth := 0
	for i, r := range s {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' && quote == '"' {
			escaped = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if quote != 0 || depth != 0 {
		return nil, fmt.Errorf("unterminated inline value")
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out, nil
}
