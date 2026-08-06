package dynamicplan

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"takt/internal/blockcatalog"
)

const (
	APIVersion = "takt/v1alpha1"
	Kind       = "WorkflowPlan"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

var AllowedBlocks = map[string]string{
	"discover":           "dynamic-discover.yaml",
	"investigate":        "dynamic-investigate.yaml",
	"implement":          "dynamic-implement.yaml",
	"validate":           "dynamic-validate.yaml",
	"review":             "dynamic-review.yaml",
	"adversarial-verify": "dynamic-adversarial-verify.yaml",
	"synthesize":         "dynamic-synthesize.yaml",
}

type Budget struct {
	MaxChildRuns  int `json:"max_child_runs"`
	MaxParallel   int `json:"max_parallel"`
	MaxIterations int `json:"max_iterations"`
	MaxTokens     int `json:"max_tokens"`
}

type Phase struct {
	ID          string   `json:"id"`
	Uses        string   `json:"uses"`
	Objective   string   `json:"objective"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Strategy    string   `json:"strategy,omitempty"`
	Source      string   `json:"source,omitempty"`
	MaxParallel int      `json:"max_parallel,omitempty"`
	Checkpoint  bool     `json:"checkpoint,omitempty"`
}

type Plan struct {
	APIVersion       string  `json:"apiVersion"`
	Kind             string  `json:"kind"`
	Decision         string  `json:"decision"`
	Goal             string  `json:"goal"`
	ExistingWorkflow string  `json:"existing_workflow,omitempty"`
	Reason           string  `json:"reason"`
	Budget           Budget  `json:"budget"`
	Phases           []Phase `json:"phases,omitempty"`
}

type Revision struct {
	Number    int       `json:"number"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	Plan      Plan      `json:"plan"`
}

type Steering struct {
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
	Applied   bool      `json:"applied,omitempty"`
}

type Record struct {
	ID                      string            `json:"id"`
	Status                  string            `json:"status"`
	Profile                 string            `json:"profile"`
	ConfigPath              string            `json:"config_path"`
	BlockPackagePaths       []string          `json:"block_package_paths,omitempty"`
	BlockCatalogFingerprint string            `json:"block_catalog_fingerprint,omitempty"`
	CreatedAt               time.Time         `json:"created_at"`
	UpdatedAt               time.Time         `json:"updated_at"`
	RequiresConfirmation    bool              `json:"requires_confirmation"`
	ConfirmedAt             *time.Time        `json:"confirmed_at,omitempty"`
	PlannerRunID            string            `json:"planner_run_id,omitempty"`
	ReplannerRunIDs         []string          `json:"replanner_run_ids,omitempty"`
	ExecutionRunIDs         []string          `json:"execution_run_ids,omitempty"`
	Detached                bool              `json:"detached,omitempty"`
	CurrentRunID            string            `json:"current_run_id,omitempty"`
	CurrentSegment          int               `json:"current_segment,omitempty"`
	PendingSegments         [][]Phase         `json:"pending_segments,omitempty"`
	CompletedPhases         []string          `json:"completed_phases,omitempty"`
	Results                 map[string]string `json:"results,omitempty"`
	Steering                []Steering        `json:"steering,omitempty"`
	LastError               string            `json:"last_error,omitempty"`
	PromotedPath            string            `json:"promoted_path,omitempty"`
	Revisions               []Revision        `json:"revisions"`
}

type ReplanDecision struct {
	Action string  `json:"action"`
	Reason string  `json:"reason"`
	Phases []Phase `json:"phases,omitempty"`
}

func Normalize(plan *Plan) {
	if plan.APIVersion == "" {
		plan.APIVersion = APIVersion
	}
	if plan.Kind == "" {
		plan.Kind = Kind
	}
	if plan.Budget.MaxChildRuns == 0 {
		plan.Budget.MaxChildRuns = 24
	}
	if plan.Budget.MaxParallel == 0 {
		plan.Budget.MaxParallel = 4
	}
	if plan.Budget.MaxIterations == 0 {
		plan.Budget.MaxIterations = 3
	}
	if plan.Budget.MaxTokens == 0 {
		plan.Budget.MaxTokens = 500000
	}
	for i := range plan.Phases {
		if plan.Phases[i].Strategy == "" {
			plan.Phases[i].Strategy = "task"
		}
		if plan.Phases[i].MaxParallel == 0 {
			plan.Phases[i].MaxParallel = plan.Budget.MaxParallel
		}
	}
}

func Validate(plan Plan) error {
	return ValidateWithCatalog(plan, nil)
}

func ValidateWithCatalog(plan Plan, catalog *blockcatalog.Catalog) error {
	if plan.APIVersion != APIVersion || plan.Kind != Kind {
		return fmt.Errorf("plan must use apiVersion %s and kind %s", APIVersion, Kind)
	}
	if strings.TrimSpace(plan.Goal) == "" {
		return fmt.Errorf("plan goal is required")
	}
	if plan.Decision != "existing" && plan.Decision != "planned" {
		return fmt.Errorf("plan decision must be existing or planned")
	}
	if plan.Budget.MaxChildRuns < 1 || plan.Budget.MaxChildRuns > 256 {
		return fmt.Errorf("budget.max_child_runs must be between 1 and 256")
	}
	if plan.Budget.MaxParallel < 1 || plan.Budget.MaxParallel > 64 {
		return fmt.Errorf("budget.max_parallel must be between 1 and 64")
	}
	if plan.Budget.MaxIterations < 1 || plan.Budget.MaxIterations > 16 {
		return fmt.Errorf("budget.max_iterations must be between 1 and 16")
	}
	if plan.Budget.MaxTokens < 1 || plan.Budget.MaxTokens > 100000000 {
		return fmt.Errorf("budget.max_tokens must be between 1 and 100000000")
	}
	if strings.TrimSpace(plan.Reason) == "" {
		return fmt.Errorf("plan reason is required")
	}
	if plan.Decision == "existing" {
		if strings.TrimSpace(plan.ExistingWorkflow) == "" {
			return fmt.Errorf("existing_workflow is required for existing decision")
		}
		if len(plan.Phases) != 0 {
			return fmt.Errorf("existing decision cannot contain phases")
		}
		return nil
	}
	if len(plan.Phases) == 0 {
		return fmt.Errorf("planned decision requires at least one phase")
	}
	if len(plan.Phases) > plan.Budget.MaxChildRuns {
		return fmt.Errorf("plan has %d phases, exceeding max_child_runs %d", len(plan.Phases), plan.Budget.MaxChildRuns)
	}
	seen := map[string]int{}
	for i, phase := range plan.Phases {
		if !idPattern.MatchString(phase.ID) {
			return fmt.Errorf("phase %d has invalid id %q", i, phase.ID)
		}
		if _, ok := seen[phase.ID]; ok {
			return fmt.Errorf("duplicate phase id %q", phase.ID)
		}
		seen[phase.ID] = i
		if catalog != nil {
			if _, ok := catalog.Block(phase.Uses); !ok {
				return fmt.Errorf("phase %q uses block %q outside the trusted catalog", phase.ID, phase.Uses)
			}
		} else if _, ok := AllowedBlocks[phase.Uses]; !ok {
			return fmt.Errorf("phase %q uses unsupported block %q", phase.ID, phase.Uses)
		}
		if strings.TrimSpace(phase.Objective) == "" {
			return fmt.Errorf("phase %q objective is required", phase.ID)
		}
		if phase.Strategy != "task" && phase.Strategy != "map" {
			return fmt.Errorf("phase %q strategy must be task or map", phase.ID)
		}
		if phase.Strategy == "map" {
			if strings.TrimSpace(phase.Source) == "" {
				return fmt.Errorf("phase %q map strategy requires source", phase.ID)
			}
			if phase.MaxParallel < 1 || phase.MaxParallel > plan.Budget.MaxParallel {
				return fmt.Errorf("phase %q max_parallel must be between 1 and plan max_parallel %d", phase.ID, plan.Budget.MaxParallel)
			}
		}
	}
	uses := make([]string, 0, len(plan.Phases))
	for _, phase := range plan.Phases {
		uses = append(uses, phase.Uses)
		for _, dep := range phase.DependsOn {
			position, ok := seen[dep]
			if !ok {
				return fmt.Errorf("phase %q depends on unknown phase %q", phase.ID, dep)
			}
			if position >= seen[phase.ID] {
				return fmt.Errorf("phase %q dependency %q must appear earlier", phase.ID, dep)
			}
		}
		if phase.Strategy == "map" {
			sourceID, err := SourcePhaseID(phase.Source)
			if err != nil {
				return fmt.Errorf("phase %q: %w", phase.ID, err)
			}
			if _, ok := seen[sourceID]; !ok || seen[sourceID] >= seen[phase.ID] {
				return fmt.Errorf("phase %q source references unavailable phase %q", phase.ID, sourceID)
			}
			if catalog != nil {
				sourcePhase := plan.Phases[seen[sourceID]]
				path := SourceOutputPath(phase.Source)
				outputType, ok := catalog.OutputPathType(sourcePhase.Uses, path)
				if !ok {
					return fmt.Errorf("phase %q source path %q is not declared by trusted block %q", phase.ID, path, sourcePhase.Uses)
				}
				if outputType != "array" {
					return fmt.Errorf("phase %q source path %q from block %q has type %s, want array", phase.ID, path, sourcePhase.Uses, outputType)
				}
			}
		}
	}
	if catalog != nil {
		if err := catalog.ValidateBudget(plan.Budget.MaxChildRuns, plan.Budget.MaxParallel, plan.Budget.MaxIterations, plan.Budget.MaxTokens); err != nil {
			return err
		}
		if err := catalog.ValidateRequiredBlocks(uses); err != nil {
			return err
		}
	}
	return nil
}

func SourcePhaseID(source string) (string, error) {
	parts := strings.Split(strings.TrimSpace(source), ".")
	if len(parts) < 4 || parts[0] != "phases" || parts[2] != "output" {
		return "", fmt.Errorf("source must be phases.<id>.output.<path>")
	}
	return parts[1], nil
}

func SourceOutputPath(source string) string {
	parts := strings.Split(strings.TrimSpace(source), ".")
	if len(parts) < 4 {
		return ""
	}
	return strings.Join(parts[3:], ".")
}

func RuntimeSource(source string) (string, error) {
	if _, err := SourcePhaseID(source); err != nil {
		return "", err
	}
	return "nodes." + strings.TrimPrefix(source, "phases."), nil
}

func Segments(phases []Phase) [][]Phase {
	var result [][]Phase
	var current []Phase
	for _, phase := range phases {
		current = append(current, phase)
		if phase.Checkpoint {
			result = append(result, current)
			current = nil
		}
	}
	if len(current) > 0 {
		result = append(result, current)
	}
	return result
}

func PendingPhases(plan Plan, completed []string) []Phase {
	set := map[string]bool{}
	for _, id := range completed {
		set[id] = true
	}
	out := make([]Phase, 0, len(plan.Phases))
	for _, phase := range plan.Phases {
		if !set[phase.ID] {
			out = append(out, phase)
		}
	}
	return out
}

func Preview(plan Plan) string {
	return PreviewWithCatalog(plan, nil)
}

func PreviewWithCatalog(plan Plan, catalog *blockcatalog.Catalog) string {
	var b strings.Builder
	if plan.Decision == "existing" {
		fmt.Fprintf(&b, "Existing workflow: %s\nReason: %s\n", plan.ExistingWorkflow, plan.Reason)
		return b.String()
	}
	fmt.Fprintf(&b, "Dynamic workflow for: %s\n\n", plan.Goal)
	for i, phase := range plan.Phases {
		strategy := phase.Strategy
		if strategy == "map" {
			strategy += fmt.Sprintf(" (parallel <= %d)", phase.MaxParallel)
		}
		checkpoint := ""
		if phase.Checkpoint {
			checkpoint = " [replanning checkpoint]"
		}
		fmt.Fprintf(&b, "%d. %s — %s: %s%s\n", i+1, phase.ID, strategy, phase.Objective, checkpoint)
	}
	if catalog != nil {
		uses := make([]string, 0, len(plan.Phases))
		for _, phase := range plan.Phases {
			uses = append(uses, phase.Uses)
		}
		capabilities := catalog.RequiredCapabilities(uses)
		if len(capabilities) > 0 {
			fmt.Fprintf(&b, "\nRequired capabilities: %s\n", strings.Join(capabilities, ", "))
		}
	}
	fmt.Fprintf(&b, "\nBudget: total runs <= %d, parallel phase nodes <= %d, plan revisions <= %d", plan.Budget.MaxChildRuns, plan.Budget.MaxParallel, plan.Budget.MaxIterations)
	if plan.Budget.MaxTokens > 0 {
		fmt.Fprintf(&b, ", tokens <= %d", plan.Budget.MaxTokens)
	}
	b.WriteByte('\n')
	return b.String()
}

func PlanJSON(plan Plan) string {
	raw, _ := json.Marshal(plan)
	return string(raw)
}

func SortedResultKeys(results map[string]string) []string {
	keys := make([]string, 0, len(results))
	for key := range results {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
