package dynamicplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/blockcatalog"
	"takt/internal/rolecontract"
	"takt/internal/spec"
)

type CompileOptions struct {
	WorkflowName      string
	OutputPath        string
	BlocksDir         string
	Goal              string
	Context           string
	Promoted          bool
	Catalog           *blockcatalog.Catalog
	GovernanceContext string
	Signals           []string
}

func Compile(phases []Phase, budget Budget, options CompileOptions) (*spec.Workflow, error) {
	if len(phases) == 0 {
		return nil, fmt.Errorf("cannot compile an empty phase segment")
	}
	wf := &spec.Workflow{
		APIVersion: APIVersion,
		Kind:       "Workflow",
		Metadata:   spec.Metadata{Name: options.WorkflowName, Description: "Generated Dynamic Takt segment compiled from a validated WorkflowPlan."},
		Defaults:   spec.Defaults{Assistant: "opencode", Model: "implementation", Session: "fresh"},
	}
	for _, phase := range phases {
		if phaseRequiresWorktree(options, phase) {
			wf.Worktree = spec.WorktreeSpec{Enabled: true, Cleanup: "manual"}
			break
		}
	}
	phaseSet := map[string]bool{}
	nonMapRuns := 0
	mapRuns := 0
	for _, phase := range phases {
		phaseSet[phase.ID] = true
		if phase.Strategy == "map" {
			mapRuns++
		} else {
			nonMapRuns++
		}
	}
	remainingMapItems := budget.MaxChildRuns - nonMapRuns
	if remainingMapItems < mapRuns {
		return nil, fmt.Errorf("segment needs at least %d child runs but budget allows %d", nonMapRuns+mapRuns, budget.MaxChildRuns)
	}
	mapIndex := 0
	for _, phase := range phases {
		blockPath, blockPolicy, resolvedBlock, err := resolveBlock(options, phase.Uses)
		if err != nil {
			return nil, err
		}
		if options.OutputPath != "" {
			if rel, err := filepath.Rel(filepath.Dir(options.OutputPath), blockPath); err == nil {
				blockPath = filepath.ToSlash(rel)
			}
		}
		input, err := phaseInput(options.Goal, phase, options.Context, options.GovernanceContext, options.Promoted, options.Signals, resolvedBlock)
		if err != nil {
			return nil, err
		}
		node := spec.Node{ID: phase.ID, DependsOn: internalDependencies(phase.DependsOn, phaseSet), WorkflowRun: &spec.WorkflowRunSpec{Path: blockPath, Input: input, Isolation: "inherit", Policy: blockPolicy}}
		if phase.Strategy == "map" {
			source, err := RuntimeSource(phase.Source)
			if err != nil {
				return nil, err
			}
			sourceID, _ := SourcePhaseID(phase.Source)
			if !phaseSet[sourceID] {
				return nil, fmt.Errorf("map phase %q source %q crosses a replanning checkpoint; keep producer and map phase in the same segment", phase.ID, sourceID)
			}
			fanInput, inputErr := phaseFanOutInput(options.Goal, phase, options.Context, options.GovernanceContext, options.Promoted, options.Signals, resolvedBlock)
			if inputErr != nil {
				return nil, inputErr
			}
			node.WorkflowRun.Input = fanInput
			mapIndex++
			remainingMaps := mapRuns - mapIndex + 1
			maxItems := remainingMapItems / remainingMaps
			remainingMapItems -= maxItems
			node.WorkflowRun.FanOut = &spec.WorkflowFanOutSpec{ItemsFrom: source, As: "item", MaxParallel: phase.MaxParallel, MaxItems: maxItems, Join: "all_success"}
		}
		wf.Nodes = append(wf.Nodes, node)
	}
	applySegmentParallelLimit(wf.Nodes, budget.MaxParallel)
	return wf, nil
}

func phaseRequiresWorktree(options CompileOptions, phase Phase) bool {
	if options.Catalog != nil {
		if block, ok := options.Catalog.Block(phase.Uses); ok {
			for _, capability := range block.Capabilities {
				if capability == "repository.write" {
					return true
				}
			}
			return false
		}
	}
	// Backward-compatible fallback for legacy dynamic plans without a catalog.
	return phase.Uses == "implement"
}

func applySegmentParallelLimit(nodes []spec.Node, maxParallel int) {
	if maxParallel < 1 {
		maxParallel = 1
	}
	for index := maxParallel; index < len(nodes); index++ {
		predecessor := nodes[index-maxParallel].ID
		if !containsDependency(nodes[index].DependsOn, predecessor) {
			nodes[index].DependsOn = append(nodes[index].DependsOn, predecessor)
		}
	}
}

func containsDependency(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func resolveBlock(options CompileOptions, name string) (string, *spec.PolicySpec, *blockcatalog.ResolvedBlock, error) {
	if options.Catalog != nil {
		block, ok := options.Catalog.Block(name)
		if !ok {
			return "", nil, nil, fmt.Errorf("trusted block %q is not available", name)
		}
		policy := block.Policy
		return block.WorkflowPath, &policy, &block, nil
	}
	blockFile, ok := AllowedBlocks[name]
	if !ok {
		return "", nil, nil, fmt.Errorf("unsupported block %q", name)
	}
	return filepath.Join(options.BlocksDir, blockFile), nil, nil, nil
}

func WriteWorkflow(path string, wf *spec.Workflow) error {
	raw, err := json.MarshalIndent(wf, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func internalDependencies(deps []string, segment map[string]bool) []string {
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		if segment[dep] {
			out = append(out, dep)
		}
	}
	return out
}

func phaseInput(goal string, phase Phase, context, governance string, promoted bool, signals []string, block *blockcatalog.ResolvedBlock) (string, error) {
	if promoted {
		goal = "${input}"
	}
	if block == nil || block.RoleDefinition == nil || block.Role == "" {
		return fmt.Sprintf("Goal: %s\n\nPhase objective: %s\n\nTrusted package governance:\n%s\n\nPrior dynamic context:\n%s", goal, phase.Objective, governance, context), nil
	}
	prior := map[string]string{}
	if strings.TrimSpace(context) != "" {
		var decoded struct {
			Results map[string]string `json:"results"`
		}
		if err := json.Unmarshal([]byte(context), &decoded); err == nil && decoded.Results != nil {
			prior = decoded.Results
		}
	}
	brief, err := rolecontract.Compile(block.Role, *block.RoleDefinition, goal, phase.Objective, signals, prior, block.Checks)
	if err != nil {
		return "", fmt.Errorf("compile brief for phase %s: %w", phase.ID, err)
	}
	if brief.Context == nil {
		brief.Context = map[string]any{}
	}
	if strings.TrimSpace(governance) != "" {
		var value any
		if err := json.Unmarshal([]byte(governance), &value); err == nil {
			brief.Context["trusted_governance"] = value
		}
	}
	return rolecontract.EncodeBrief(brief), nil
}

func phaseFanOutInput(goal string, phase Phase, context, governance string, promoted bool, signals []string, block *blockcatalog.ResolvedBlock) (string, error) {
	base, err := phaseInput(goal, phase, context, governance, promoted, signals, block)
	if err != nil {
		return "", err
	}
	if block != nil && block.RoleDefinition != nil && block.Role != "" {
		var brief rolecontract.Brief
		if err := json.Unmarshal([]byte(base), &brief); err != nil {
			return "", err
		}
		if brief.Context == nil {
			brief.Context = map[string]any{}
		}
		brief.Context["current_item"] = "${fanout.item}"
		brief.Context["fanout_index"] = "${fanout.index}"
		brief.Context["fanout_total"] = "${fanout.total}"
		return rolecontract.EncodeBrief(brief), nil
	}
	return base + "\n\nCurrent item (${fanout.index}/${fanout.total}):\n${fanout.item}", nil
}

func SafeWorkflowName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == ' ':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "generated-workflow"
	}
	if len(name) > 63 {
		name = strings.Trim(name[:63], "-")
	}
	return name
}
