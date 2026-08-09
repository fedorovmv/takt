package dynamicflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"takt/internal/experimental/taskroute"
	"takt/internal/experimental/workspacecatalog"
	"takt/internal/extensions/blockcatalog"
	"takt/internal/profile"
	"takt/internal/store"
	tasksource "takt/sdk/tasksource"
)

func (s *PlanService) routeTask(ctx context.Context, resolved *profile.Resolved, catalog *blockcatalog.Catalog, repositories *workspacecatalog.Catalog, goal string, source *tasksource.Task, workflows []WorkflowListEntry, adapterPreflight []AdapterPreflightStatus) (*taskroute.Decision, string, error) {
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
		"task_source":           source,
		"deterministic_signals": taskroute.InferSignals(goal),
		"existing_workflows":    routedWorkflows,
		"trusted_catalog":       catalog.PlannerView(),
		"adapter_preflight":     adapterPreflight,
		"repositories":          repositories.PlannerView(),
	})
	if err != nil {
		return nil, "", fmt.Errorf("encode task router input: %w", err)
	}
	started, err := s.runs.Start(ctx, StartRequest{Selector: resolved.RouterPath, Input: string(payload), ConfigPath: resolved.ConfigPath})
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
