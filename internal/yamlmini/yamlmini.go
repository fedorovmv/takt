package yamlmini

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type line struct {
	indent int
	text   string
	num    int
}

// Unmarshal parses a deliberately small YAML 1.2 subset sufficient for the
// workflow files shipped with this prototype. JSON is accepted as well.
func Unmarshal(data []byte, out any) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return fmt.Errorf("empty document")
	}
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.DisallowUnknownFields()
		return dec.Decode(out)
	}
	ls, err := tokenize(string(data))
	if err != nil {
		return err
	}
	p := parser{lines: ls}
	v, err := p.parseBlock(ls[0].indent)
	if err != nil {
		return err
	}
	if p.pos != len(p.lines) {
		return fmt.Errorf("unexpected content at line %d", p.lines[p.pos].num)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode YAML document: %w", err)
	}
	return nil
}

func tokenize(src string) ([]line, error) {
	var out []line
	raw := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, s := range raw {
		if strings.Contains(s, "\t") {
			return nil, fmt.Errorf("tabs are not allowed in YAML indentation, line %d", i+1)
		}
		trim := strings.TrimSpace(s)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		indent := len(s) - len(strings.TrimLeft(s, " "))
		text := stripComment(strings.TrimSpace(s))
		if text == "" {
			continue
		}
		out = append(out, line{indent: indent, text: text, num: i + 1})
	}
	if len(out) == 0 {
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

func (p *parser) parseBlock(indent int) (any, error) {
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
	for p.pos < len(p.lines) {
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
		switch rest {
		case "|", ">":
			value = p.parseBlockScalar(indent, rest == ">")
		case "":
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				value, err = p.parseBlock(p.lines[p.pos].indent)
			} else {
				value = nil
			}
		default:
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
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent < indent {
			break
		}
		if ln.indent != indent || !strings.HasPrefix(ln.text, "-") {
			break
		}
		rest := strings.TrimSpace(strings.TrimPrefix(ln.text, "-"))
		p.pos++
		if rest == "" {
			if p.pos >= len(p.lines) || p.lines[p.pos].indent <= indent {
				out = append(out, nil)
				continue
			}
			v, err := p.parseBlock(p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
			continue
		}
		if key, val, ok := splitKeyValue(rest); ok {
			item := map[string]any{}
			var parsed any
			var err error
			if val == "" {
				if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
					parsed, err = p.parseBlock(p.lines[p.pos].indent)
				}
			} else {
				parsed, err = parseScalar(val)
			}
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", ln.num, err)
			}
			item[key] = parsed
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
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
		v, err := parseScalar(rest)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", ln.num, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func (p *parser) parseBlockScalar(parentIndent int, folded bool) string {
	if p.pos >= len(p.lines) || p.lines[p.pos].indent <= parentIndent {
		return ""
	}
	base := p.lines[p.pos].indent
	var parts []string
	for p.pos < len(p.lines) && p.lines[p.pos].indent > parentIndent {
		ln := p.lines[p.pos]
		text := ln.text
		if ln.indent > base {
			text = strings.Repeat(" ", ln.indent-base) + text
		}
		parts = append(parts, text)
		p.pos++
	}
	if folded {
		return strings.Join(parts, " ")
	}
	return strings.Join(parts, "\n") + "\n"
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
			v, err := parseScalar(part)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	if strings.HasPrefix(s, "{") {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("inline objects must use JSON syntax: %w", err)
		}
		return v, nil
	}
	if (strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"")) || (strings.HasPrefix(s, "'") && strings.HasSuffix(s, "'")) {
		if s[0] == '\'' {
			return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), nil
		}
		v, err := strconv.Unquote(s)
		if err != nil {
			return nil, err
		}
		return v, nil
	}
	switch strings.ToLower(s) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "~":
		return nil, nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && strings.ContainsAny(s, ".eE") {
		return f, nil
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
