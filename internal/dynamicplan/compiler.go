package dynamicplan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"takt/internal/blockcatalog"
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
		if phase.Uses == "implement" {
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
		blockPath, blockPolicy, err := resolveBlock(options, phase.Uses)
		if err != nil {
			return nil, err
		}
		if options.OutputPath != "" {
			if rel, err := filepath.Rel(filepath.Dir(options.OutputPath), blockPath); err == nil {
				blockPath = filepath.ToSlash(rel)
			}
		}
		input := phaseInput(options.Goal, phase, options.Context, options.GovernanceContext, options.Promoted)
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
			node.WorkflowRun.Input = phaseFanOutInput(options.Goal, phase, options.Context, options.GovernanceContext, options.Promoted)
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

func resolveBlock(options CompileOptions, name string) (string, *spec.PolicySpec, error) {
	if options.Catalog != nil {
		block, ok := options.Catalog.Block(name)
		if !ok {
			return "", nil, fmt.Errorf("trusted block %q is not available", name)
		}
		policy := block.Policy
		return block.WorkflowPath, &policy, nil
	}
	blockFile, ok := AllowedBlocks[name]
	if !ok {
		return "", nil, fmt.Errorf("unsupported block %q", name)
	}
	return filepath.Join(options.BlocksDir, blockFile), nil, nil
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

func phaseInput(goal string, phase Phase, context, governance string, promoted bool) string {
	if promoted {
		goal = "${input}"
	}
	return fmt.Sprintf("Goal: %s\n\nPhase objective: %s\n\nTrusted package governance:\n%s\n\nPrior dynamic context:\n%s", goal, phase.Objective, governance, context)
}

func phaseFanOutInput(goal string, phase Phase, context, governance string, promoted bool) string {
	base := phaseInput(goal, phase, context, governance, promoted)
	return base + "\n\nCurrent item (${fanout.index}/${fanout.total}):\n${fanout.item}"
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
