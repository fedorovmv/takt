// Package whenexpr owns Takt's deliberately small conditional language.
//
// The language is a governance surface, not a general expression language:
// comparisons are limited to == and !=, combined with && and ||. Computation
// belongs in script/command/prompt nodes and can expose a structured value for
// a later when gate. Do not grow this package one operator at a time.
package whenexpr

import (
	"fmt"
	"regexp"
	"strings"
)

type Resolver func(path string) (string, error)

func Validate(expr string) error {
	_, err := parse(strings.TrimSpace(expr))
	return err
}

func Evaluate(expr string, resolve Resolver) (bool, error) {
	parsed, err := parse(strings.TrimSpace(expr))
	if err != nil {
		return false, err
	}
	if parsed == nil {
		return true, nil
	}
	return parsed.eval(resolve)
}

type node interface{ eval(Resolver) (bool, error) }

type logical struct {
	op    string
	parts []node
}

type comparison struct {
	left  string
	op    string
	right string
}

func (n logical) eval(resolve Resolver) (bool, error) {
	switch n.op {
	case "||":
		for _, part := range n.parts {
			ok, err := part.eval(resolve)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "&&":
		for _, part := range n.parts {
			ok, err := part.eval(resolve)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, fmt.Errorf("unsupported logical operator %q", n.op)
	}
}

func (n comparison) eval(resolve Resolver) (bool, error) {
	if resolve == nil {
		return false, fmt.Errorf("when resolver is required")
	}
	value, err := resolve(n.left)
	if err != nil {
		return false, err
	}
	if n.op == "==" {
		return value == n.right, nil
	}
	return value != n.right, nil
}

func parse(expr string) (node, error) {
	if expr == "" {
		return nil, nil
	}
	if hasUnquotedAny(expr, "()") {
		return nil, constitutionError(expr, "parentheses are not part of the language")
	}
	if parts := split(expr, "||"); len(parts) > 1 {
		nodes := make([]node, 0, len(parts))
		for _, part := range parts {
			child, err := parse(part)
			if err != nil {
				return nil, err
			}
			if child == nil {
				return nil, constitutionError(expr, "empty operand")
			}
			nodes = append(nodes, child)
		}
		return logical{op: "||", parts: nodes}, nil
	}
	if parts := split(expr, "&&"); len(parts) > 1 {
		nodes := make([]node, 0, len(parts))
		for _, part := range parts {
			child, err := parse(part)
			if err != nil {
				return nil, err
			}
			if child == nil {
				return nil, constitutionError(expr, "empty operand")
			}
			nodes = append(nodes, child)
		}
		return logical{op: "&&", parts: nodes}, nil
	}
	left, op, right, err := parseComparison(expr)
	if err != nil {
		return nil, err
	}
	return comparison{left: left, op: op, right: right}, nil
}

var pathRE = regexp.MustCompile(`^(nodes\.[A-Za-z0-9_-]+\.[A-Za-z0-9_.-]+|inputs\.(message|input))$`)

func parseComparison(expr string) (string, string, string, error) {
	idx, op, count := findComparators(expr)
	if count != 1 {
		if count == 0 {
			return "", "", "", constitutionError(expr, "only == and != comparisons are supported")
		}
		return "", "", "", constitutionError(expr, "each clause must contain exactly one comparison")
	}
	left := strings.TrimSpace(expr[:idx])
	rightRaw := strings.TrimSpace(expr[idx+len(op):])
	if left == "" || rightRaw == "" {
		return "", "", "", constitutionError(expr, "comparison operands must not be empty")
	}
	if !pathRE.MatchString(left) {
		return "", "", "", constitutionError(expr, "the left operand must be a nodes.* or inputs.* path")
	}
	if hasUnquotedAny(rightRaw, "+*/%<>") {
		return "", "", "", constitutionError(expr, "arithmetic and ordering operators belong in a script node")
	}
	right, err := literal(rightRaw)
	if err != nil {
		return "", "", "", constitutionError(expr, err.Error())
	}
	return left, op, right, nil
}

func literal(value string) (string, error) {
	if strings.ContainsAny(value, `"'`) {
		if len(value) < 2 || (value[0] != '"' && value[0] != '\'') || value[len(value)-1] != value[0] {
			return "", fmt.Errorf("quoted string literals must use matching delimiters")
		}
		return value[1 : len(value)-1], nil
	}
	if strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("string literals containing whitespace must be quoted")
	}
	if strings.ContainsAny(value, "()") {
		return "", fmt.Errorf("functions and parentheses are not part of the language")
	}
	return value, nil
}

func findComparators(expr string) (index int, op string, count int) {
	index = -1
	var quote byte
	for i := 0; i < len(expr)-1; i++ {
		c := expr[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		pair := expr[i : i+2]
		if pair == "==" || pair == "!=" {
			if count == 0 {
				index, op = i, pair
			}
			count++
			i++
		}
	}
	return index, op, count
}

func split(expr, operator string) []string {
	var parts []string
	start := 0
	var quote byte
	for i := 0; i <= len(expr)-len(operator); i++ {
		c := expr[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if expr[i:i+len(operator)] == operator {
			parts = append(parts, strings.TrimSpace(expr[start:i]))
			start = i + len(operator)
			i += len(operator) - 1
		}
	}
	if len(parts) == 0 {
		return []string{expr}
	}
	parts = append(parts, strings.TrimSpace(expr[start:]))
	return parts
}

func hasUnquotedAny(expr, chars string) bool {
	var quote byte
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if strings.ContainsRune(chars, rune(c)) {
			return true
		}
	}
	return false
}

func constitutionError(expr, reason string) error {
	return fmt.Errorf("unsupported when expression %q: %s; Takt when is intentionally limited to ==, !=, && and ||; compute richer decisions in a script/command/prompt node", expr, reason)
}
