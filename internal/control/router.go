package control

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"takt/internal/blockcatalog"
	"takt/internal/profile"
	"takt/internal/store"
	"takt/internal/taskroute"
)

func (s *Service) routeTask(ctx context.Context, resolved *profile.Resolved, catalog *blockcatalog.Catalog, goal string, workflows []WorkflowListEntry) (*taskroute.Decision, string, error) {
	if resolved == nil || strings.TrimSpace(resolved.RouterPath) == "" {
		return nil, "", fmt.Errorf("semantic router is not configured")
	}
	routedWorkflows := make([]WorkflowListEntry, 0, len(workflows))
	selectors := make([]string, 0, len(workflows))
	for _, entry := range workflows {
		if entry.Default {
			continue
		}
		routedWorkflows = append(routedWorkflows, entry)
		selectors = append(selectors, entry.Selector)
	}
	payload, err := json.Marshal(map[string]any{
		"goal":                  goal,
		"deterministic_signals": taskroute.InferSignals(goal),
		"existing_workflows":    routedWorkflows,
		"trusted_catalog":       catalog.PlannerView(),
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode task router input: %w", err)
	}
	started, err := s.Start(ctx, StartRequest{Selector: resolved.RouterPath, Input: string(payload), ConfigPath: resolved.ConfigPath})
	if err != nil {
		return nil, "", fmt.Errorf("task router: %w", err)
	}
	if started.State == nil || started.State.Status != store.RunCompleted {
		return nil, "", fmt.Errorf("task router did not complete")
	}
	var decision taskroute.Decision
	if err := json.Unmarshal([]byte(started.State.Output), &decision); err != nil {
		return nil, "", fmt.Errorf("decode task router output: %w", err)
	}
	decision.Signals = append(decision.Signals, taskroute.InferSignals(goal)...)
	taskroute.Normalize(&decision, resolved.Name)
	if err := taskroute.Validate(decision, taskroute.WorkflowSet(resolved.Name, selectors)); err != nil {
		return nil, "", err
	}
	return &decision, started.RunID, nil
}
