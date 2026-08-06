package taskroute

import (
	"fmt"
	"sort"
	"strings"

	"takt/internal/blockcatalog"
	"takt/internal/dynamicplan"
)

const (
	APIVersion = "takt/v1alpha1"
	Kind       = "TaskRoute"

	RouteWorkflow = "workflow"
	RouteTemplate = "template"
	RouteDynamic  = "dynamic"

	TemplateSimpleReliable = "simple-reliable"
)

// Controls are progressive additions to the ordinary inspect/implement/verify
// path. They are selected by the router and then bounded by deterministic Takt
// policy before compilation.
type Controls struct {
	InspectFirst     bool `json:"inspect_first,omitempty"`
	Baseline         bool `json:"baseline,omitempty"`
	IndependentTests bool `json:"independent_tests,omitempty"`
	EnhancedReview   bool `json:"enhanced_review,omitempty"`
	MaxParallel      int  `json:"max_parallel,omitempty"`
}

// Decision is the provider-neutral result of routing one user task. It does not
// execute anything and cannot grant capabilities. Takt validates and compiles it
// into an ordinary WorkflowPlan or an existing workflow selector.
type Decision struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Route      string   `json:"route"`
	Workflow   string   `json:"workflow,omitempty"`
	Template   string   `json:"template,omitempty"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
	Signals    []string `json:"signals,omitempty"`
	Controls   Controls `json:"controls,omitempty"`
}

func Normalize(value *Decision, profile string) {
	if value.APIVersion == "" {
		value.APIVersion = APIVersion
	}
	if value.Kind == "" {
		value.Kind = Kind
	}
	if value.Route == RouteTemplate && value.Template == "" {
		value.Template = TemplateSimpleReliable
	}
	if value.Route == RouteWorkflow && value.Workflow != "" && !strings.Contains(value.Workflow, ":") {
		value.Workflow = strings.TrimSpace(profile) + ":" + strings.TrimSpace(value.Workflow)
	}
	value.Signals = uniqueSorted(value.Signals)
	if value.Controls.MaxParallel == 0 {
		value.Controls.MaxParallel = 2
	}
	ApplyMinimumControls(value)
}

// ApplyMinimumControls is monotonic: router output may request stronger checks,
// while deterministic signals can only add controls and never remove them.
func ApplyMinimumControls(value *Decision) {
	set := map[string]bool{}
	for _, signal := range value.Signals {
		set[signal] = true
	}
	if value.Confidence < 0.55 && value.Route != RouteWorkflow {
		value.Controls.InspectFirst = true
	}
	if set["regression"] || set["existing_failures"] || set["data_migration"] {
		value.Controls.Baseline = true
	}
	if set["public_api"] || set["security_sensitive"] || set["data_migration"] {
		value.Controls.Baseline = true
		value.Controls.IndependentTests = true
		value.Controls.EnhancedReview = true
	}
	if set["unclear_scope"] || set["multi_repository"] || set["large_change"] {
		value.Controls.InspectFirst = true
	}
}

func Validate(value Decision, workflows map[string]bool) error {
	if value.APIVersion != APIVersion || value.Kind != Kind {
		return fmt.Errorf("route must use apiVersion %s and kind %s", APIVersion, Kind)
	}
	if strings.TrimSpace(value.Reason) == "" {
		return fmt.Errorf("route reason is required")
	}
	if value.Confidence < 0 || value.Confidence > 1 {
		return fmt.Errorf("route confidence must be between 0 and 1")
	}
	if value.Controls.MaxParallel < 1 || value.Controls.MaxParallel > 64 {
		return fmt.Errorf("controls.max_parallel must be between 1 and 64")
	}
	switch value.Route {
	case RouteWorkflow:
		if strings.TrimSpace(value.Workflow) == "" {
			return fmt.Errorf("workflow route requires workflow")
		}
		if len(workflows) > 0 && !workflows[value.Workflow] {
			return fmt.Errorf("workflow route references unavailable selector %q", value.Workflow)
		}
		if value.Template != "" {
			return fmt.Errorf("workflow route cannot set template")
		}
	case RouteTemplate:
		if value.Template != TemplateSimpleReliable {
			return fmt.Errorf("unsupported task template %q", value.Template)
		}
		if value.Workflow != "" {
			return fmt.Errorf("template route cannot set workflow")
		}
	case RouteDynamic:
		if value.Workflow != "" || value.Template != "" {
			return fmt.Errorf("dynamic route cannot set workflow or template")
		}
	default:
		return fmt.Errorf("route must be workflow, template, or dynamic")
	}
	return nil
}

// InferSignals provides cheap, deterministic hints. They are intentionally
// conservative and do not choose a route; the semantic router receives them as
// evidence and Takt uses them only to raise minimum controls.
func InferSignals(goal string) []string {
	text := strings.ToLower(goal)
	checks := []struct {
		signal string
		terms  []string
	}{
		{"public_api", []string{"public api", "публичн", "openapi", "protobuf", "proto contract", "совместимост", "breaking change"}},
		{"security_sensitive", []string{"security", "безопасност", "auth", "авторизац", "аутентификац", "credential", "secret", "tls"}},
		{"data_migration", []string{"migration", "миграц", "schema change", "схемы данных", "database", "база данных"}},
		{"regression", []string{"regression", "регресс", "bug", "дефект", "ошибк", "исправ"}},
		{"multi_repository", []string{"multi-repo", "multiple repositories", "несколько репозитор", "между репозитория"}},
		{"existing_plan", []string{"existing plan", "готовый план", "по плану", "implementation plan"}},
		{"review", []string{"review", "ревью", "проверь pr", "pull request"}},
		{"unclear_scope", []string{"исследуй", "разберись", "investigate", "research", "непонятно", "найди причину"}},
	}
	var out []string
	for _, check := range checks {
		for _, term := range check.terms {
			if containsTerm(text, term) {
				out = append(out, check.signal)
				break
			}
		}
	}
	return uniqueSorted(out)
}

func containsTerm(text, term string) bool {
	if strings.ContainsAny(term, " -_/.") || containsNonASCII(term) {
		return strings.Contains(text, term)
	}
	// Short technical tokens such as "auth" and "bug" must not match
	// "author" or "debug". Treat plain ASCII words as lexical tokens while
	// retaining substring matching for Russian stems and multi-word phrases.
	start := 0
	for {
		index := strings.Index(text[start:], term)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !asciiWord(text[index-1])
		after := index + len(term)
		afterOK := after == len(text) || !asciiWord(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
}

func asciiWord(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9') || value == '_'
}

func containsNonASCII(value string) bool {
	for _, r := range value {
		if r > 127 {
			return true
		}
	}
	return false
}

func Compile(goal string, route Decision, catalog *blockcatalog.Catalog) (dynamicplan.Plan, error) {
	if route.Route != RouteTemplate || route.Template != TemplateSimpleReliable {
		return dynamicplan.Plan{}, fmt.Errorf("route %q cannot be compiled as template", route.Route)
	}
	required := []string{"investigate", "implement", "validate", "review"}
	if route.Controls.Baseline {
		required = append(required, "baseline")
	}
	if route.Controls.IndependentTests {
		required = append(required, "test-design")
	}
	if route.Controls.EnhancedReview {
		required = append(required, "adversarial-verify")
	}
	if catalog != nil {
		for _, name := range required {
			if _, ok := catalog.Block(name); !ok {
				return dynamicplan.Plan{}, fmt.Errorf("simple-reliable template requires trusted block %q", name)
			}
		}
	}

	phases := make([]dynamicplan.Phase, 0, len(required))
	previous := ""
	add := func(id, uses, objective string, checkpoint bool) {
		phase := dynamicplan.Phase{ID: id, Uses: uses, Objective: objective, Strategy: "task", Checkpoint: checkpoint}
		if previous != "" {
			phase.DependsOn = []string{previous}
		}
		phases = append(phases, phase)
		previous = id
	}
	if route.Controls.Baseline {
		add("baseline", "baseline", "Capture the unchanged repository baseline and distinguish pre-existing failures from regressions.", false)
	}
	add("inspect", "investigate", "Inspect the relevant code and establish a bounded implementation approach with evidence.", route.Controls.InspectFirst)
	if route.Controls.IndependentTests {
		add("test-design", "test-design", "Design and add independent regression or contract tests from the requested behavior before relying on the implementation.", false)
	}
	add("implement", "implement", "Implement the smallest complete change inside the established scope.", false)
	add("validate", "validate", "Run the deterministic checks required for the changed area and compare them with the baseline when present.", false)
	add("review", "review", "Independently review the completed change against the task and validation evidence.", false)
	if route.Controls.EnhancedReview {
		add("adversarial-verify", "adversarial-verify", "Challenge the result for missed edge cases, compatibility risks and unsupported claims.", false)
	}

	maxParallel := route.Controls.MaxParallel
	if maxParallel < 1 {
		maxParallel = 1
	}
	plan := dynamicplan.Plan{
		APIVersion: APIVersion,
		Kind:       dynamicplan.Kind,
		Decision:   "planned",
		Goal:       strings.TrimSpace(goal),
		Reason:     route.Reason,
		Budget: dynamicplan.Budget{
			MaxChildRuns:  24,
			MaxParallel:   maxParallel,
			MaxIterations: 3,
			MaxTokens:     500000,
		},
		Phases: phases,
	}
	dynamicplan.Normalize(&plan)
	return plan, nil
}

func WorkflowSet(profile string, selectors []string) map[string]bool {
	out := map[string]bool{}
	for _, selector := range selectors {
		selector = strings.TrimSpace(selector)
		if selector == "" {
			continue
		}
		if !strings.Contains(selector, ":") {
			selector = strings.TrimSpace(profile) + ":" + selector
		}
		out[selector] = true
	}
	return out
}

func uniqueSorted(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
