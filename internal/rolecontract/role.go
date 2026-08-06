package rolecontract

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"takt/internal/spec"
)

const (
	ReactionDeny   = "deny"
	ReactionRepair = "repair"
	ReactionWarn   = "warn"

	CheckRequired  = "required"
	CheckPreferred = "preferred"
)

type ContextRecipe struct {
	Include  []string `json:"include,omitempty"`
	MaxChars int      `json:"max_chars,omitempty"`
}

type PathScope struct {
	Expected  []string `json:"expected,omitempty"`
	Allowed   []string `json:"allowed,omitempty"`
	Protected []string `json:"protected,omitempty"`
	Forbidden []string `json:"forbidden,omitempty"`
}

type Definition struct {
	Purpose      string          `json:"purpose,omitempty"`
	ModelProfile string          `json:"model_profile,omitempty"`
	Session      string          `json:"session,omitempty"`
	Context      ContextRecipe   `json:"context,omitempty"`
	Paths        PathScope       `json:"paths,omitempty"`
	Policy       spec.PolicySpec `json:"policy,omitempty"`
}

type Check struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Level    string `json:"level"`
	Reaction string `json:"reaction"`
}

type Brief struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Role       string         `json:"role"`
	Purpose    string         `json:"purpose,omitempty"`
	Goal       string         `json:"goal"`
	Objective  string         `json:"objective"`
	Signals    []string       `json:"signals,omitempty"`
	Scope      PathScope      `json:"scope"`
	Context    map[string]any `json:"context,omitempty"`
	Checks     []Check        `json:"checks,omitempty"`
}

type CheckResult struct {
	PhaseID      string `json:"phase_id,omitempty"`
	Block        string `json:"block,omitempty"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	Level        string `json:"level"`
	Reaction     string `json:"reaction"`
	Passed       bool   `json:"passed"`
	BaselineOnly bool   `json:"baseline_only,omitempty"`
	FailureCode  string `json:"failure_code,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

type ScopeResult struct {
	Changed        []string `json:"changed,omitempty"`
	Unexpected     []string `json:"unexpected,omitempty"`
	OutsideAllowed []string `json:"outside_allowed,omitempty"`
	Protected      []string `json:"protected,omitempty"`
	Forbidden      []string `json:"forbidden,omitempty"`
}

func ValidateDefinition(name string, value Definition) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("role name is required")
	}
	if value.Session != "" && value.Session != "fresh" && value.Session != "shared" {
		return fmt.Errorf("role %s session must be fresh or shared", name)
	}
	if value.Context.MaxChars < 0 || value.Context.MaxChars > 1_000_000 {
		return fmt.Errorf("role %s context.max_chars must be between 0 and 1000000", name)
	}
	return nil
}

func ValidateCheck(value Check) error {
	if strings.TrimSpace(value.Name) == "" || strings.TrimSpace(value.Path) == "" {
		return fmt.Errorf("check name and path are required")
	}
	if value.Level != CheckRequired && value.Level != CheckPreferred {
		return fmt.Errorf("check %s level must be required or preferred", value.Name)
	}
	if value.Reaction != ReactionDeny && value.Reaction != ReactionRepair && value.Reaction != ReactionWarn {
		return fmt.Errorf("check %s reaction must be deny, repair, or warn", value.Name)
	}
	return nil
}

var pathToken = regexp.MustCompile(`(?i)(?:^|[\s'"` + "`" + `])([a-z0-9_.-]+(?:/[a-z0-9_.{}*\[\]-]+)+)`)

func Compile(roleName string, role Definition, goal, objective string, signals []string, prior map[string]string, checks []Check) (Brief, error) {
	if err := ValidateDefinition(roleName, role); err != nil {
		return Brief{}, err
	}
	for _, check := range checks {
		if err := ValidateCheck(check); err != nil {
			return Brief{}, err
		}
	}
	scope := role.Paths
	scope.Expected = unique(append(scope.Expected, inferExpected(goal)...))
	if len(scope.Allowed) == 0 {
		scope.Allowed = []string{"**"}
	}
	if len(scope.Forbidden) == 0 {
		scope.Forbidden = []string{".git/**", ".takt/**"}
	}
	context := make(map[string]any)
	include := role.Context.Include
	if len(include) == 0 {
		include = []string{"prior_results"}
	}
	for _, key := range include {
		switch key {
		case "prior_results":
			context[key] = trimResults(prior, role.Context.MaxChars)
		case "signals":
			context[key] = unique(signals)
		case "scope":
			context[key] = scope
		}
	}
	return Brief{APIVersion: "takt/v1alpha1", Kind: "TaskBrief", Role: roleName, Purpose: role.Purpose, Goal: strings.TrimSpace(goal), Objective: strings.TrimSpace(objective), Signals: unique(signals), Scope: scope, Context: context, Checks: checks}, nil
}

func EncodeBrief(value Brief) string {
	raw, _ := json.MarshalIndent(value, "", "  ")
	return string(raw)
}

func Evaluate(output string, checks []Check) ([]CheckResult, error) {
	if len(checks) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return nil, fmt.Errorf("decode structured output for checks: %w", err)
	}
	results := make([]CheckResult, 0, len(checks))
	for _, check := range checks {
		actual, ok := lookup(value, check.Path)
		passed := ok && truthy(actual)
		detail := ""
		if !ok {
			detail = "field is missing"
		} else if !passed {
			detail = fmt.Sprintf("value=%v", actual)
		}
		results = append(results, CheckResult{Name: check.Name, Path: check.Path, Level: check.Level, Reaction: check.Reaction, Passed: passed, Detail: detail})
	}
	return results, nil
}

func ClassifyChanges(output string, scope PathScope) (ScopeResult, error) {
	var result ScopeResult
	if strings.TrimSpace(output) == "" {
		return result, nil
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(output), &value); err != nil {
		return result, nil
	}
	raw, ok := value["changed_files"].([]any)
	if !ok {
		return result, nil
	}
	for _, item := range raw {
		original, ok := item.(string)
		if !ok || strings.TrimSpace(original) == "" {
			continue
		}
		path, safe := normalizeRelativePath(original)
		if !safe {
			result.Forbidden = append(result.Forbidden, strings.TrimSpace(original))
			continue
		}
		result.Changed = append(result.Changed, path)
		if matchesAny(scope.Forbidden, path) {
			result.Forbidden = append(result.Forbidden, path)
			continue
		}
		if len(scope.Allowed) > 0 && !matchesAny(scope.Allowed, path) {
			result.OutsideAllowed = append(result.OutsideAllowed, path)
		}
		if matchesAny(scope.Protected, path) {
			result.Protected = append(result.Protected, path)
		}
		if len(scope.Expected) > 0 && !matchesAny(scope.Expected, path) {
			result.Unexpected = append(result.Unexpected, path)
		}
	}
	result.Changed = unique(result.Changed)
	result.Unexpected = unique(result.Unexpected)
	result.OutsideAllowed = unique(result.OutsideAllowed)
	result.Protected = unique(result.Protected)
	result.Forbidden = unique(result.Forbidden)
	return result, nil
}

func ForbiddenChanges(output string, scope PathScope) ([]string, error) {
	result, err := ClassifyChanges(output, scope)
	return result.Forbidden, err
}

func ProtectedChanges(output string, scope PathScope) ([]string, error) {
	result, err := ClassifyChanges(output, scope)
	return result.Protected, err
}

func normalizeRelativePath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return value, false
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(clean), false
	}
	clean = filepath.ToSlash(strings.TrimPrefix(clean, "./"))
	if clean == "." || clean == "" {
		return clean, false
	}
	return clean, true
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		if pathMatch(pattern, value) {
			return true
		}
	}
	return false
}

func lookup(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "pass") || strings.EqualFold(strings.TrimSpace(v), "passed") || strings.EqualFold(strings.TrimSpace(v), "approved")
	case float64:
		return v != 0
	default:
		return value != nil
	}
}

func trimResults(values map[string]string, maxChars int) map[string]string {
	if maxChars <= 0 {
		maxChars = 32000
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := map[string]string{}
	remaining := maxChars
	for _, key := range keys {
		if remaining <= 0 {
			break
		}
		value := values[key]
		if len(value) > remaining {
			value = value[:remaining]
		}
		out[key] = value
		remaining -= len(value)
	}
	return out
}

func inferExpected(goal string) []string {
	matches := pathToken.FindAllStringSubmatch(goal, -1)
	var out []string
	for _, match := range matches {
		if len(match) > 1 {
			out = append(out, filepath.ToSlash(strings.TrimSpace(match[1])))
		}
	}
	return unique(out)
}

func pathMatch(pattern, value string) bool {
	pattern = filepath.ToSlash(strings.TrimSpace(pattern))
	if pattern == "**" {
		return true
	}
	if strings.HasSuffix(pattern, "/**") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "**"))
	}
	if strings.HasPrefix(pattern, "**/") {
		return strings.Contains("/"+value, "/"+strings.TrimPrefix(pattern, "**/"))
	}
	matched, _ := filepath.Match(pattern, value)
	return matched
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
