package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/blockcatalog"
	cfgpkg "takt/internal/config"
	"takt/internal/dynamicplan"
	"takt/internal/evidence"
	"takt/internal/profile"
	"takt/internal/rolecontract"
	"takt/internal/store"
	"takt/internal/taskroute"
	"takt/internal/workflow"
	"takt/internal/workspacecatalog"
	tasksource "takt/sdk/tasksource"
)

type PlanRequest struct {
	Goal       string            `json:"goal"`
	Profile    string            `json:"profile,omitempty"`
	Candidate  *dynamicplan.Plan `json:"candidate,omitempty"`
	TaskSource *tasksource.Task  `json:"task_source,omitempty"`
}

type PlanResult struct {
	PlanID               string              `json:"plan_id"`
	Decision             string              `json:"decision"`
	ExistingWorkflow     string              `json:"existing_workflow,omitempty"`
	Preview              string              `json:"preview"`
	RequiresConfirmation bool                `json:"requires_confirmation"`
	Route                *taskroute.Decision `json:"route,omitempty"`
	Record               *dynamicplan.Record `json:"record"`
}

type PlanPhaseView struct {
	ID         string `json:"id"`
	Uses       string `json:"uses"`
	Objective  string `json:"objective"`
	Status     string `json:"status"`
	Checkpoint bool   `json:"checkpoint,omitempty"`
	Output     string `json:"output,omitempty"`
}

type PlanRunView struct {
	RunID         string       `json:"run_id"`
	Status        string       `json:"status"`
	CurrentNode   string       `json:"current_node,omitempty"`
	ArtifactCount int          `json:"artifact_count"`
	Usage         *store.Usage `json:"usage,omitempty"`
	Error         string       `json:"error,omitempty"`
}

type PlanView struct {
	Record        *dynamicplan.Record `json:"record"`
	Route         *taskroute.Decision `json:"route,omitempty"`
	Preview       string              `json:"preview"`
	Phases        []PlanPhaseView     `json:"phases,omitempty"`
	Runs          []PlanRunView       `json:"runs,omitempty"`
	ArtifactCount int                 `json:"artifact_count"`
}

type ExecutePlanRequest struct {
	PlanID  string `json:"plan_id"`
	Confirm bool   `json:"confirm,omitempty"`
	// Detached is selected by the transport: daemon-backed callers set it to
	// true, while direct CLI/stdio callers keep foreground execution.
	Detached bool `json:"-"`
}

type SteerRequest struct {
	PlanID  string `json:"plan_id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message"`
}

func (s *PlanService) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	goal := strings.TrimSpace(request.Goal)
	if goal == "" {
		return nil, fmt.Errorf("goal is required")
	}
	profileName := strings.TrimSpace(request.Profile)
	if profileName == "" {
		profileName = "code"
	}
	resolved, err := profile.Resolve(profileName, s.Workspace)
	if err != nil {
		return nil, err
	}
	catalog, err := catalogForResolved(resolved)
	if err != nil {
		return nil, err
	}
	repositories, err := workspacecatalog.Load(ctx, s.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace repositories: %w", err)
	}
	preflightConfig, err := cfgpkg.Load(resolved.ConfigPath)
	if err != nil {
		return nil, err
	}
	adapterPreflight, err := preflightCatalogAdapters(ctx, catalog, preflightConfig)
	if err != nil {
		return nil, err
	}
	workflows, err := s.Catalogs.ListWorkflows(profileName)
	if err != nil {
		return nil, fmt.Errorf("list workflows for task router: %w", err)
	}

	var plan dynamicplan.Plan
	var route *taskroute.Decision
	routerRunID := ""
	routerError := ""
	plannerRunID := ""
	if request.Candidate != nil {
		plan = *request.Candidate
		if strings.TrimSpace(plan.Goal) == "" {
			plan.Goal = goal
		}
	} else {
		route, routerRunID, err = s.routeTask(ctx, resolved, catalog, repositories, goal, request.TaskSource, workflows, adapterPreflight)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			routerError = err.Error()
			// Routing is an optimization, not a new availability dependency. An
			// unavailable or invalid semantic router falls back to the stable
			// inspect-first template and records the reason in the durable route.
			route = &taskroute.Decision{
				Route:      taskroute.RouteTemplate,
				Template:   taskroute.TemplateSimpleReliable,
				Reason:     "semantic router unavailable; stable inspect-first fallback selected",
				Confidence: 0,
				Signals:    append(taskroute.InferSignals(goal), "router_fallback"),
				Controls:   taskroute.Controls{InspectFirst: true, MaxParallel: 1},
			}
			taskroute.Normalize(route, resolved.Name)
			err = nil
		}
		switch {
		case route != nil && route.Route == taskroute.RouteWorkflow:
			plan = dynamicplan.Plan{
				APIVersion:       dynamicplan.APIVersion,
				Kind:             dynamicplan.Kind,
				Decision:         "existing",
				Goal:             goal,
				ExistingWorkflow: route.Workflow,
				Reason:           route.Reason,
			}
		case route != nil && route.Route == taskroute.RouteTemplate:
			plan, err = taskroute.Compile(goal, *route, catalog)
			if err != nil {
				return nil, err
			}
		default:
			plannerPath := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "dynamic-plan.yaml")
			plannerInput, encodeErr := json.Marshal(map[string]any{"goal": goal, "task_source": request.TaskSource, "existing_workflows": workflows, "trusted_catalog": catalog.PlannerView(), "adapter_preflight": adapterPreflight, "repositories": repositories.PlannerView()})
			if encodeErr != nil {
				return nil, fmt.Errorf("encode dynamic planner input: %w", encodeErr)
			}
			started, startErr := s.Runs.Start(ctx, StartRequest{Selector: plannerPath, Input: string(plannerInput), ConfigPath: resolved.ConfigPath})
			if startErr != nil {
				return nil, fmt.Errorf("dynamic planner: %w", startErr)
			}
			if started.State == nil || started.State.Status != store.RunCompleted {
				return nil, fmt.Errorf("dynamic planner did not complete")
			}
			plannerRunID = started.RunID
			if err := json.Unmarshal([]byte(started.State.Output), &plan); err != nil {
				return nil, fmt.Errorf("decode dynamic planner output: %w", err)
			}
		}
	}
	dynamicplan.Normalize(&plan)
	if plan.Goal == "" {
		plan.Goal = goal
	}
	if plan.Decision == "existing" && !strings.Contains(plan.ExistingWorkflow, ":") {
		plan.ExistingWorkflow = profileName + ":" + plan.ExistingWorkflow
	}
	if err := dynamicplan.ValidateWithCatalogAndRepositories(plan, catalog, repositories); err != nil {
		return nil, err
	}
	if plan.Decision == "existing" {
		if _, err := s.Catalogs.DescribeWorkflow(plan.ExistingWorkflow); err != nil {
			return nil, fmt.Errorf("planned existing workflow: %w", err)
		}
	}
	id, err := newPlanID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var routeRaw json.RawMessage
	if route != nil {
		routeRaw, err = json.Marshal(route)
		if err != nil {
			return nil, fmt.Errorf("encode task route: %w", err)
		}
	}
	record := &dynamicplan.Record{
		ID: id, Status: "draft", Profile: profileName, ConfigPath: resolved.ConfigPath,
		CreatedAt: now, UpdatedAt: now, RequiresConfirmation: plan.Decision == "planned",
		RouterRunID: routerRunID, RouterError: routerError, Route: routeRaw, TaskSource: request.TaskSource, PlannerRunID: plannerRunID, Results: map[string]string{},
		BlockPackagePaths: append([]string(nil), resolved.BlockPackagePaths...), BlockCatalogFingerprint: catalog.Fingerprint,
		RepositoryCatalogFingerprint: repositories.Fingerprint, RepositoryExecutions: map[string]dynamicplan.RepositoryExecution{}, MergeOrder: dynamicplan.RepositoryMergeOrder(plan),
		Revisions: []dynamicplan.Revision{{Number: 1, Reason: "initial plan", CreatedAt: now, Plan: plan}},
	}
	if plan.Decision == "planned" {
		record.PendingSegments = dynamicplan.Segments(plan.Phases)
	}
	if err := s.savePlanRecord(record); err != nil {
		return nil, err
	}
	return &PlanResult{PlanID: id, Decision: plan.Decision, ExistingWorkflow: plan.ExistingWorkflow, Preview: dynamicplan.PreviewWithCatalog(plan, catalog), RequiresConfirmation: record.RequiresConfirmation, Route: route, Record: record}, nil
}

func (s *PlanService) GetPlan(planID string) (*PlanView, error) {
	record, err := (dynamicplan.Store{Workspace: s.Workspace}).Load(planID)
	if err != nil {
		return nil, err
	}
	plan := latestPlan(record)
	var route *taskroute.Decision
	if len(record.Route) > 0 {
		var decoded taskroute.Decision
		if err := json.Unmarshal(record.Route, &decoded); err != nil {
			return nil, fmt.Errorf("decode persisted task route: %w", err)
		}
		route = &decoded
	}
	catalog, err := s.catalogForRecord(record)
	if err != nil {
		return nil, err
	}
	completed := map[string]bool{}
	for _, id := range record.CompletedPhases {
		completed[id] = true
	}
	view := &PlanView{Record: record, Route: route, Preview: dynamicplan.PreviewWithCatalog(plan, catalog)}
	for _, phase := range plan.Phases {
		status := "pending"
		if completed[phase.ID] {
			status = "completed"
		} else if record.Status == "running" && record.CurrentSegment < len(record.PendingSegments) {
			for _, current := range record.PendingSegments[record.CurrentSegment] {
				if current.ID == phase.ID {
					status = "running"
				}
			}
		}
		view.Phases = append(view.Phases, PlanPhaseView{ID: phase.ID, Uses: phase.Uses, Objective: phase.Objective, Status: status, Checkpoint: phase.Checkpoint, Output: record.Results[phase.ID]})
	}
	for _, runID := range record.ExecutionRunIDs {
		run, runErr := s.Runs.GetRun(runID)
		if runErr != nil {
			view.Runs = append(view.Runs, PlanRunView{RunID: runID, Status: "unavailable", Error: runErr.Error()})
			continue
		}
		view.ArtifactCount += len(run.Artifacts)
		view.Runs = append(view.Runs, PlanRunView{RunID: run.ID, Status: run.Status, CurrentNode: run.CurrentNode, ArtifactCount: len(run.Artifacts), Usage: run.Usage, Error: run.Error})
	}
	return view, nil
}

func (s *PlanService) ExecutePlan(ctx context.Context, request ExecutePlanRequest) (*dynamicplan.Record, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	st := dynamicplan.Store{Workspace: s.Workspace}
	advanceLock, err := st.AcquireAdvanceLock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if advanceLock != nil {
			_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
		}
	}()
	record, err := st.Load(request.PlanID)
	if err != nil {
		return nil, err
	}
	if record.Status != "draft" && record.Status != "waiting" {
		return nil, fmt.Errorf("plan %s cannot execute from status %s", record.ID, record.Status)
	}
	if record.RequiresConfirmation && !request.Confirm && record.ConfirmedAt == nil {
		return nil, fmt.Errorf("plan %s requires confirmation after preview", record.ID)
	}
	plan := latestPlan(record)
	if _, err := s.catalogForRecord(record); err != nil {
		return nil, err
	}
	if len(record.Revisions) >= plan.Budget.MaxIterations {
		return nil, fmt.Errorf("plan %s has %d revisions, exceeding limit %d", record.ID, len(record.Revisions), plan.Budget.MaxIterations)
	}
	if s.exceededTokenBudget(record, plan.Budget.MaxTokens) {
		return nil, fmt.Errorf("dynamic plan token budget is already exhausted")
	}
	now := time.Now().UTC()
	if record.ConfirmedAt == nil {
		record.ConfirmedAt = &now
	}
	record.Detached = request.Detached
	record.Status = "running"
	record.LastError = ""
	record.UpdatedAt = now
	if plan.Decision == "existing" {
		if s.dynamicRunCount(record)+1 > plan.Budget.MaxChildRuns {
			return nil, fmt.Errorf("dynamic plan run budget exhausted before existing workflow start")
		}
		// The underlying Run is always launched asynchronously. Foreground mode
		// keeps this process alive in advanceForegroundPlan; daemon mode returns
		// after the durable plan-to-Run link has been persisted.
		started, startErr := s.Runs.Start(ctx, StartRequest{Selector: plan.ExistingWorkflow, Input: plan.Goal, Detached: true})
		if startErr != nil {
			record.Status = "failed"
			record.LastError = startErr.Error()
			return nil, s.saveDynamicFailure(record, startErr)
		}
		record.CurrentRunID = started.RunID
		record.ExecutionRunIDs = append(record.ExecutionRunIDs, started.RunID)
		if err := s.savePlanRecord(record); err != nil {
			return nil, err
		}
	} else {
		if len(record.PendingSegments) == 0 {
			record.PendingSegments = dynamicplan.Segments(dynamicplan.PendingPhases(plan, record.CompletedPhases))
		}
		if err := s.startDynamicSegment(ctx, record); err != nil {
			record.Status = "failed"
			record.LastError = err.Error()
			return nil, s.saveDynamicFailure(record, err)
		}
		if err := s.savePlanRecord(record); err != nil {
			return nil, err
		}
	}
	if err := dynamicplan.ReleaseAdvanceLock(advanceLock); err != nil {
		return nil, err
	}
	advanceLock = nil
	if !record.Detached {
		return s.advanceForegroundPlan(ctx, record, st)
	}
	return record, nil
}

func (s *PlanService) advanceForegroundPlan(ctx context.Context, record *dynamicplan.Record, st dynamicplan.Store) (*dynamicplan.Record, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		advanceLock, err := st.AcquireAdvanceLock(ctx)
		if err != nil {
			return nil, err
		}
		current, err := st.Load(record.ID)
		if err != nil {
			_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
			return nil, err
		}
		record = current
		if (record.Status != "running" && record.Status != "pausing") || record.CurrentRunID == "" {
			if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
				return nil, releaseErr
			}
			return record, nil
		}
		run, err := s.Runs.GetRun(record.CurrentRunID)
		if err != nil {
			_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
			return nil, err
		}
		if run.Status == store.RunRunning || run.Status == store.RunPausing {
			if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
				return nil, releaseErr
			}
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if run.Status == store.RunPaused {
			record.Status = "paused"
			record.LastError = ""
			record.UpdatedAt = time.Now().UTC()
			if err := s.savePlanRecord(record); err != nil {
				_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
				return nil, err
			}
			if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
				return nil, releaseErr
			}
			return record, nil
		}
		if run.Status == store.RunWaiting {
			record.Status = "waiting"
			record.LastError = fmt.Sprintf("segment run %s is waiting for input", run.ID)
			record.UpdatedAt = time.Now().UTC()
			if err := s.savePlanRecord(record); err != nil {
				_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
				return nil, err
			}
			if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
				return nil, releaseErr
			}
			return record, nil
		}
		if err := s.advanceDynamicRecord(ctx, record); err != nil {
			record.Status = "waiting"
			record.LastError = err.Error()
			record.UpdatedAt = time.Now().UTC()
			if saveErr := s.savePlanRecord(record); saveErr != nil {
				_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
				return nil, fmt.Errorf("%v; persist plan failure: %w", err, saveErr)
			}
			if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
				return nil, fmt.Errorf("%v; release plan lock: %w", err, releaseErr)
			}
			return nil, err
		}
		if releaseErr := dynamicplan.ReleaseAdvanceLock(advanceLock); releaseErr != nil {
			return nil, releaseErr
		}
	}
}

func (s *PlanService) saveDynamicFailure(record *dynamicplan.Record, cause error) error {
	if err := s.savePlanRecord(record); err != nil {
		return fmt.Errorf("%v; persist plan failure: %w", cause, err)
	}
	return cause
}

func (s *PlanService) Steer(ctx context.Context, request SteerRequest) (*dynamicplan.Record, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	if strings.TrimSpace(request.Message) == "" {
		return nil, fmt.Errorf("steering message is required")
	}
	st := dynamicplan.Store{Workspace: s.Workspace}
	advanceLock, err := st.AcquireAdvanceLock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if advanceLock != nil {
			_ = dynamicplan.ReleaseAdvanceLock(advanceLock)
		}
	}()
	record, err := s.resolvePlanRecord(request.PlanID, request.RunID)
	if err != nil {
		return nil, err
	}
	if record.Status != "running" && record.Status != "waiting" && record.Status != "parked" {
		return nil, fmt.Errorf("plan %s cannot be steered from status %s", record.ID, record.Status)
	}
	plan := latestPlan(record)
	if len(record.Revisions) >= plan.Budget.MaxIterations {
		return nil, fmt.Errorf("plan revision limit reached: %d of %d", len(record.Revisions), plan.Budget.MaxIterations)
	}
	record.Steering = append(record.Steering, dynamicplan.Steering{Message: request.Message, CreatedAt: time.Now().UTC()})
	record.UpdatedAt = time.Now().UTC()
	if record.Status == "waiting" || record.Status == "parked" {
		previousStatus := record.Status
		record.Status = "running"
		record.LastError = ""
		if err := s.replanAtCheckpoint(ctx, record); err != nil {
			// Steering is tentative until the replacement plan is accepted. Keep the
			// durable parking record intact so a failed replanner cannot erase the
			// reason, timestamp or safe continuation advice for the operator.
			record.Status = previousStatus
			record.LastError = err.Error()
			if saveErr := s.savePlanRecord(record); saveErr != nil {
				return nil, fmt.Errorf("%v; persist steering failure: %w", err, saveErr)
			}
			return nil, err
		}
		clearPlanFailure(record)
	}
	if err := s.savePlanRecord(record); err != nil {
		return nil, err
	}
	if err := dynamicplan.ReleaseAdvanceLock(advanceLock); err != nil {
		return nil, err
	}
	advanceLock = nil
	if !record.Detached && record.Status == "running" {
		return s.advanceForegroundPlan(ctx, record, st)
	}
	return record, nil
}

type PromotePlanOptions struct {
	Force bool
}

func (s *PlanService) PromotePlan(planID, name string) (*dynamicplan.Record, error) {
	return s.PromotePlanWithOptions(planID, name, PromotePlanOptions{})
}

func (s *PlanService) PromotePlanWithOptions(planID, name string, options PromotePlanOptions) (*dynamicplan.Record, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	st := dynamicplan.Store{Workspace: s.Workspace}
	record, err := st.Load(planID)
	if err != nil {
		return nil, err
	}
	if record.Status != "completed" {
		return nil, fmt.Errorf("only a completed plan can be promoted")
	}
	plan := latestPlan(record)
	if plan.Decision != "planned" {
		return nil, fmt.Errorf("an existing workflow plan does not need promotion")
	}
	resolved, err := profile.Resolve(record.Profile, s.Workspace)
	if err != nil {
		return nil, err
	}
	catalog, err := s.catalogForRecord(record)
	if err != nil {
		return nil, err
	}
	repositories, err := s.repositoriesForRecord(context.Background(), record)
	if err != nil {
		return nil, err
	}
	name = dynamicplan.SafeWorkflowName(name)
	output := filepath.Join(s.Workspace, ".takt", "workflows", "generated", name+".yaml")
	if !options.Force {
		if _, err := os.Stat(output); err == nil {
			return nil, fmt.Errorf("promoted workflow already exists: %s; use force to replace it", output)
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	blocks := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "blocks")
	wf, err := dynamicplan.Compile(plan.Phases, plan.Budget, dynamicplan.CompileOptions{WorkflowName: name, OutputPath: output, BlocksDir: blocks, Goal: plan.Goal, Promoted: true, Catalog: catalog, GovernanceContext: catalog.GovernanceJSON(), Signals: routeSignals(record), Repositories: repositories})
	if err != nil {
		return nil, err
	}
	var previous []byte
	if options.Force {
		previous, err = os.ReadFile(output)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read previous promoted workflow: %w", err)
		}
		if os.IsNotExist(err) {
			previous = nil
		}
	}
	if err := dynamicplan.WriteWorkflow(output, wf); err != nil {
		return nil, err
	}
	if _, err := workflow.Load(output); err != nil {
		var rollbackErr error
		if len(previous) > 0 {
			rollbackErr = os.WriteFile(output, previous, 0o600)
		} else {
			rollbackErr = os.Remove(output)
			if os.IsNotExist(rollbackErr) {
				rollbackErr = nil
			}
		}
		if rollbackErr != nil {
			return nil, fmt.Errorf("validate promoted workflow: %v; rollback failed: %w", err, rollbackErr)
		}
		return nil, fmt.Errorf("validate promoted workflow: %w", err)
	}
	record.PromotedPath = output
	record.UpdatedAt = time.Now().UTC()
	if err := s.savePlanRecord(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *PlanService) AdvanceDynamicPlans(ctx context.Context) error {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	st := dynamicplan.Store{Workspace: s.Workspace}
	lock, acquired, err := st.TryAdvanceLock()
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	defer dynamicplan.ReleaseAdvanceLock(lock)
	records, err := st.List()
	if err != nil {
		return err
	}
	var failures []string
	for _, record := range records {
		if (record.Status != "running" && record.Status != "pausing") || record.CurrentRunID == "" {
			continue
		}
		if err := s.advanceDynamicRecord(ctx, record); err != nil {
			record.Status = "waiting"
			record.LastError = err.Error()
			record.UpdatedAt = time.Now().UTC()
			if saveErr := s.savePlanRecord(record); saveErr != nil {
				failures = append(failures, record.ID+": "+err.Error()+"; persist: "+saveErr.Error())
				continue
			}
			failures = append(failures, record.ID+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("advance dynamic plans: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *PlanService) advanceDynamicRecord(ctx context.Context, record *dynamicplan.Record) error {
	run, err := s.Runs.GetRun(record.CurrentRunID)
	if err != nil {
		return err
	}
	if run.Status == store.RunRunning || run.Status == store.RunPausing || run.Status == store.RunWaiting {
		return nil
	}
	if run.Status == store.RunPaused {
		record.Status = "paused"
		record.LastError = ""
		record.UpdatedAt = time.Now().UTC()
		return s.savePlanRecord(record)
	}
	if run.Status == store.RunAbandoned {
		record.Status = "abandoned"
		record.LastError = run.Error
		record.UpdatedAt = time.Now().UTC()
		return s.savePlanRecord(record)
	}
	if run.Status != store.RunCompleted {
		record.Status = "failed"
		record.LastError = fmt.Sprintf("segment run %s ended with %s: %s", run.ID, run.Status, run.Error)
		record.UpdatedAt = time.Now().UTC()
		return s.savePlanRecord(record)
	}
	if latestPlan(record).Decision == "existing" {
		record.Status = "completed"
		record.CurrentRunID = ""
		record.Results["workflow"] = run.Output
		record.UpdatedAt = time.Now().UTC()
		return s.savePlanRecord(record)
	}
	if record.CurrentSegment >= len(record.PendingSegments) {
		return fmt.Errorf("current segment %d is outside pending segments", record.CurrentSegment)
	}
	segment := record.PendingSegments[record.CurrentSegment]
	if run.Worktree != nil && run.Worktree.Enabled && strings.TrimSpace(run.ExecutionWorkspace) != "" {
		if record.ExecutionWorkspace == "" {
			record.ExecutionWorkspace = run.ExecutionWorkspace
			record.ExecutionBaseCommit = run.Worktree.BaseCommit
			record.ExecutionWorktreeRunID = run.ID
		} else if filepath.Clean(record.ExecutionWorkspace) != filepath.Clean(run.ExecutionWorkspace) {
			return fmt.Errorf("dynamic plan execution workspace changed from %s to %s", record.ExecutionWorkspace, run.ExecutionWorkspace)
		}
	}
	for _, phase := range segment {
		if node := run.Nodes[phase.ID]; node != nil {
			record.Results[phase.ID] = node.Output
			if phase.Repository != "" {
				execution := record.RepositoryExecutions[phase.Repository]
				execution.Repository = phase.Repository
				execution.RunID = node.ChildRunID
				execution.ControlWorkspace = node.ChildControlWorkspace
				execution.ExecutionWorkspace = node.ChildExecutionWorkspace
				execution.Branch = node.ChildBranch
				execution.BaseCommit = node.ChildBaseCommit
				execution.Status = "completed"
				if execution.ExecutionWorkspace != "" && execution.BaseCommit != "" {
					sha, shaErr := candidateSHAForWorkspace(ctx, execution.ExecutionWorkspace, execution.BaseCommit)
					if shaErr != nil {
						return shaErr
					}
					execution.CandidateSHA = sha
					execution.Evidence = repositoryEvidence(phase, node.Output, sha)
				}
				if publish := run.Nodes[phase.ID+"-publish"]; publish != nil {
					execution.ChangeOutput = publish.Output
				}
				record.RepositoryExecutions[phase.Repository] = execution
			}
		}
		if !containsString(record.CompletedPhases, phase.ID) {
			record.CompletedPhases = append(record.CompletedPhases, phase.ID)
		}
	}
	catalog, err := s.catalogForRecord(record)
	if err != nil {
		return err
	}
	actualChanges, err := s.dynamicActualChanges(ctx, record)
	if err != nil {
		return err
	}
	candidateSHA, err := s.dynamicCandidateSHA(ctx, record)
	if err != nil {
		return err
	}
	if record.Evidence != nil {
		evidence.MarkStale(record.Evidence, candidateSHA)
	}
	controlOutcome, err := evaluateSegmentControls(record, segment, catalog, actualChanges, candidateSHA)
	if err != nil {
		return err
	}
	if applyControlDeny(record, controlOutcome) {
		return s.savePlanRecord(record)
	}
	if len(controlOutcome.RepairFailures) > 0 {
		handled, err := s.scheduleAutomaticRepair(ctx, record, segment, controlOutcome.RepairFailures, catalog)
		if err != nil {
			return err
		}
		if handled {
			record.UpdatedAt = time.Now().UTC()
			return s.savePlanRecord(record)
		}
	}
	if s.exceededTokenBudget(record, latestPlan(record).Budget.MaxTokens) {
		parkPlan(record, evidence.FailureBudget, "dynamic plan token budget exceeded at phase boundary", "task-owner", "fork the task with an explicitly larger trusted budget or stop it", false)
		return s.savePlanRecord(record)
	}
	checkpoint := len(segment) > 0 && segment[len(segment)-1].Checkpoint
	if checkpoint && len(record.Revisions) < latestPlan(record).Budget.MaxIterations {
		if err := s.replanAtCheckpoint(ctx, record); err != nil {
			return err
		}
		if record.Status != "running" {
			return s.savePlanRecord(record)
		}
		return s.savePlanRecord(record)
	}
	record.CurrentSegment++
	if record.CurrentSegment >= len(record.PendingSegments) {
		if len(record.DeferredSegments) > 0 {
			record.PendingSegments = record.DeferredSegments
			record.DeferredSegments = nil
			record.CurrentSegment = 0
			record.CurrentRunID = ""
			if err := s.startDynamicSegment(ctx, record); err != nil {
				return err
			}
			record.UpdatedAt = time.Now().UTC()
			return s.savePlanRecord(record)
		}
		record.Status = "completed"
		record.CurrentRunID = ""
		clearPlanFailure(record)
		finalizeEvidence(record, candidateSHA)
		record.UpdatedAt = time.Now().UTC()
		return s.savePlanRecord(record)
	}
	if err := s.startDynamicSegment(ctx, record); err != nil {
		return err
	}
	return s.savePlanRecord(record)
}

func repositoryEvidence(phase dynamicplan.Phase, output, candidateSHA string) *evidence.Manifest {
	manifest := evidence.NewManifest()
	manifest.CandidateSHA = candidateSHA
	passed := true
	var value map[string]any
	if json.Unmarshal([]byte(output), &value) == nil {
		if approved, ok := value["approved"].(bool); ok {
			passed = approved
		}
		if validated, ok := value["passed"].(bool); ok {
			passed = passed && validated
		}
	}
	status := evidence.VerdictPass
	reason := "repository child Run completed and its current candidate is captured"
	if !passed {
		status = evidence.VerdictFail
		reason = "repository child Run reported an unsuccessful verification result"
	}
	manifest.Acceptance[evidence.AcceptanceID(phase.Uses, "repository-result")] = evidence.Acceptance{
		ID: evidence.AcceptanceID(phase.Uses, "repository-result"), Block: phase.Uses, Check: "repository-result", PhaseID: phase.ID,
		Status: map[bool]string{true: "passed", false: "failed"}[passed], Level: rolecontract.CheckRequired, CandidateSHA: candidateSHA, Evidence: outputEvidence(output),
	}
	manifest.Verdict = &evidence.Verdict{Status: status, CandidateSHA: candidateSHA, Reason: reason, CreatedAt: time.Now().UTC()}
	manifest.UpdatedAt = time.Now().UTC()
	return manifest
}

func applyControlDeny(record *dynamicplan.Record, outcome segmentControlOutcome) bool {
	if outcome.DenyReason == "" {
		return false
	}
	parkPlan(record, evidence.FailureBoundary, outcome.DenyReason, "policy", "adjust the task or trusted scope; do not retry the same out-of-scope mutation", false, "repeat the same out-of-scope mutation")
	return true
}

type segmentControlOutcome struct {
	DenyReason     string
	RepairFailures []controlFailure
}

type controlFailure struct {
	PhaseID string
	Block   string
	Check   string
	Detail  string
}

func evaluateSegmentControls(record *dynamicplan.Record, segment []dynamicplan.Phase, catalog *blockcatalog.Catalog, actualChanges []string, candidateSHA string) (segmentControlOutcome, error) {
	var outcome segmentControlOutcome
	if len(actualChanges) > 0 {
		declared := declaredChanges(record)
		var undeclared []string
		for _, path := range actualChanges {
			if !declared[path] {
				undeclared = append(undeclared, path)
			}
		}
		if len(undeclared) > 0 {
			outcome.DenyReason = fmt.Sprintf("execution workspace contains changes not declared by mutating worker outputs: %s", strings.Join(undeclared, ", "))
			return outcome, nil
		}
	}
	for _, phase := range segment {
		block, ok := catalog.Block(phase.Uses)
		if !ok {
			return outcome, fmt.Errorf("trusted block %q disappeared while evaluating phase %s", phase.Uses, phase.ID)
		}
		output := record.Results[phase.ID]
		if phase.Uses == "baseline" {
			if err := captureBaselineEvidence(record, phase.ID, output, candidateSHA); err != nil {
				return outcome, err
			}
		}
		if block.RoleDefinition != nil {
			scopeResult, err := rolecontract.ClassifyChanges(output, block.RoleDefinition.Paths)
			if err != nil {
				return outcome, err
			}
			if len(scopeResult.Forbidden) > 0 {
				outcome.DenyReason = fmt.Sprintf("phase %s attempted changes in forbidden scope: %s", phase.ID, strings.Join(scopeResult.Forbidden, ", "))
				return outcome, nil
			}
			if len(scopeResult.OutsideAllowed) > 0 {
				outcome.DenyReason = fmt.Sprintf("phase %s attempted changes outside the role's allowed scope: %s", phase.ID, strings.Join(scopeResult.OutsideAllowed, ", "))
				return outcome, nil
			}
			if len(scopeResult.Protected) > 0 {
				record.Warnings = appendUniqueString(record.Warnings, fmt.Sprintf("phase %s touched protected scope %s; verification remains mandatory", phase.ID, strings.Join(scopeResult.Protected, ", ")))
			}
			if len(scopeResult.Unexpected) > 0 {
				record.Warnings = appendUniqueString(record.Warnings, fmt.Sprintf("phase %s changed files outside the expected scope: %s", phase.ID, strings.Join(scopeResult.Unexpected, ", ")))
			}
		}
		results, err := rolecontract.Evaluate(output, block.Checks)
		if err != nil {
			return outcome, fmt.Errorf("phase %s checks: %w", phase.ID, err)
		}
		for _, result := range results {
			result.PhaseID = phase.ID
			result.Block = phase.Uses
			issues := outputIssues(output)
			if !result.Passed && len(issues) > 0 && record.Evidence != nil && record.Evidence.Baseline != nil {
				known, fresh := evidence.ClassifyAgainstBaseline(issues, record.Evidence.Baseline)
				if len(known) > 0 && len(fresh) == 0 {
					result.Passed = true
					result.BaselineOnly = true
					result.FailureCode = evidence.FailureBaseline
					result.Detail = "unchanged baseline failures: " + strings.Join(known, "; ")
					record.Warnings = appendUniqueString(record.Warnings, fmt.Sprintf("phase %s reports only failures already present in the captured baseline", phase.ID))
				}
			}
			if !result.Passed && result.FailureCode == "" {
				if phase.Uses == "validate" {
					result.FailureCode = evidence.FailureImplementation
				} else {
					result.FailureCode = evidence.FailureVerification
				}
			}
			status := "passed"
			if result.BaselineOnly {
				status = "baseline"
			} else if !result.Passed {
				status = "failed"
			}
			recordAcceptance(record, phase.ID, phase.Uses, result.Name, result.Level, status, result.FailureCode, result.Detail, candidateSHA, outputEvidence(output))
			record.CheckResults = append(record.CheckResults, result)
			if result.Passed {
				continue
			}
			if result.Level == rolecontract.CheckPreferred {
				record.Warnings = appendUniqueString(record.Warnings, fmt.Sprintf("preferred check %s failed in phase %s (%s)", result.Name, phase.ID, result.Detail))
				continue
			}
			switch result.Reaction {
			case rolecontract.ReactionWarn:
				record.Warnings = appendUniqueString(record.Warnings, fmt.Sprintf("required check %s failed in phase %s (%s)", result.Name, phase.ID, result.Detail))
			case rolecontract.ReactionDeny:
				outcome.DenyReason = fmt.Sprintf("required check %s denied completion in phase %s (%s)", result.Name, phase.ID, result.Detail)
				return outcome, nil
			case rolecontract.ReactionRepair:
				outcome.RepairFailures = append(outcome.RepairFailures, controlFailure{PhaseID: phase.ID, Block: phase.Uses, Check: result.Name, Detail: result.Detail})
			}
		}
	}
	return outcome, nil
}

func (s *PlanService) scheduleAutomaticRepair(ctx context.Context, record *dynamicplan.Record, segment []dynamicplan.Phase, failures []controlFailure, catalog *blockcatalog.Catalog) (bool, error) {
	if len(failures) == 0 {
		return false, nil
	}
	if record.RepairAttempts == nil {
		record.RepairAttempts = map[string]int{}
	}
	for _, failure := range failures {
		key := failure.Block + ":" + failure.Check
		if record.RepairAttempts[key] >= 1 {
			parkPlan(record, evidence.FailureImplementation, fmt.Sprintf("Technical check %s still fails after one automatic repair.", failure.Check), "task-owner", "choose a different implementation approach, explicitly accept the remaining risk through a new plan, or stop the task", false)
			return true, nil
		}
	}
	if _, ok := catalog.Block("implement"); !ok {
		parkPlan(record, evidence.FailureOwnerDecision, "A required technical check failed, but the trusted catalog has no implement block for automatic repair.", "task-owner", "select a trusted implementation path or stop the task", false)
		return true, nil
	}
	for _, failure := range failures {
		record.RepairAttempts[failure.Block+":"+failure.Check]++
	}
	record.RepairGeneration++
	gen := record.RepairGeneration
	var details []string
	for _, failure := range failures {
		details = append(details, fmt.Sprintf("%s/%s: %s", failure.PhaseID, failure.Check, failure.Detail))
	}
	repair := dynamicplan.Phase{
		ID:        fmt.Sprintf("auto-repair-%d", gen),
		Uses:      "implement",
		Objective: "Repair only the concrete technical failures reported by Takt while preserving the task goal and already successful work. Failures: " + strings.Join(details, "; "),
		Strategy:  "task",
	}
	phases := []dynamicplan.Phase{repair}
	previous := repair.ID
	for _, phase := range segment {
		block, ok := catalog.Block(phase.Uses)
		if !ok || len(block.Checks) == 0 {
			continue
		}
		id := fmt.Sprintf("recheck-%s-%d", phase.Uses, gen)
		if len(id) > 63 {
			id = fmt.Sprintf("recheck-%d-%d", len(phases), gen)
		}
		phases = append(phases, dynamicplan.Phase{ID: id, Uses: phase.Uses, Objective: "Re-run this verification independently after the automatic repair: " + phase.Objective, DependsOn: []string{previous}, Strategy: "task"})
		previous = id
	}
	if len(phases) == 1 {
		parkPlan(record, evidence.FailureOwnerDecision, "A required check failed, but no trusted verification block is available for a bounded automatic repair.", "task-owner", "select a trusted verification path or stop the task", false)
		return true, nil
	}
	remaining := record.CurrentSegment + 1
	if remaining < len(record.PendingSegments) {
		record.DeferredSegments = append([][]dynamicplan.Phase(nil), record.PendingSegments[remaining:]...)
	} else {
		record.DeferredSegments = nil
	}
	record.PendingSegments = dynamicplan.Segments(phases)
	record.CurrentSegment = 0
	record.CurrentRunID = ""
	record.Status = "running"
	record.LastError = ""
	if err := s.startDynamicSegment(ctx, record); err != nil {
		return true, err
	}
	return true, nil
}

func declaredChanges(record *dynamicplan.Record) map[string]bool {
	out := map[string]bool{}
	repositories := map[string]string{}
	if record != nil && len(record.Revisions) > 0 {
		for _, phase := range latestPlan(record).Phases {
			if phase.Repository != "" {
				repositories[phase.ID] = phase.Repository
			}
		}
	}
	if record == nil {
		return out
	}
	for phaseID, output := range record.Results {
		var value map[string]any
		if err := json.Unmarshal([]byte(output), &value); err != nil {
			continue
		}
		raw, ok := value["changed_files"].([]any)
		if !ok {
			continue
		}
		for _, item := range raw {
			path, ok := item.(string)
			if !ok {
				continue
			}
			path = filepath.ToSlash(strings.TrimSpace(path))
			path = strings.TrimPrefix(path, "./")
			if path != "" {
				if repo := strings.TrimSpace(repositories[phaseID]); repo != "" {
					path = filepath.ToSlash(filepath.Join(repo, filepath.FromSlash(path)))
				}
				out[path] = true
			}
		}
	}
	return out
}

func (s *PlanService) dynamicActualChanges(ctx context.Context, record *dynamicplan.Record) ([]string, error) {
	if record != nil && len(record.RepositoryExecutions) > 0 {
		changed := map[string]bool{}
		var repos []string
		for repo := range record.RepositoryExecutions {
			repos = append(repos, repo)
		}
		sort.Strings(repos)
		for _, repo := range repos {
			execution := record.RepositoryExecutions[repo]
			if execution.ExecutionWorkspace == "" || execution.BaseCommit == "" {
				continue
			}
			paths, err := gitWorkspaceChanges(ctx, execution.ExecutionWorkspace, execution.BaseCommit)
			if err != nil {
				return nil, fmt.Errorf("inspect repository %s changes: %w", repo, err)
			}
			for _, path := range paths {
				changed[filepath.ToSlash(filepath.Join(repo, filepath.FromSlash(path)))] = true
			}
		}
		out := make([]string, 0, len(changed))
		for path := range changed {
			out = append(out, path)
		}
		sort.Strings(out)
		return out, nil
	}
	if record == nil || strings.TrimSpace(record.ExecutionWorkspace) == "" || strings.TrimSpace(record.ExecutionBaseCommit) == "" {
		return nil, nil
	}
	return gitWorkspaceChanges(ctx, record.ExecutionWorkspace, record.ExecutionBaseCommit)
}

func gitWorkspaceChanges(ctx context.Context, executionWorkspace, baseCommit string) ([]string, error) {
	workspace := filepath.Clean(executionWorkspace)
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("dynamic execution workspace is unavailable at %s", workspace)
	}
	changed := map[string]bool{}
	commands := [][]string{
		{"-C", workspace, "diff", "--name-only", "-z", baseCommit, "--"},
		{"-C", workspace, "ls-files", "--others", "--exclude-standard", "-z"},
	}
	for _, args := range commands {
		cmd := exec.CommandContext(ctx, "git", args...)
		raw, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("inspect dynamic execution changes with git %s: %w", strings.Join(args, " "), err)
		}
		for _, item := range strings.Split(string(raw), "\x00") {
			path := filepath.ToSlash(strings.TrimSpace(item))
			path = strings.TrimPrefix(path, "./")
			if path != "" {
				changed[path] = true
			}
		}
	}
	out := make([]string, 0, len(changed))
	for path := range changed {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func buildReplannerPayload(plan dynamicplan.Plan, record *dynamicplan.Record, remaining []dynamicplan.Phase, catalog *blockcatalog.Catalog, repositories *workspacecatalog.Catalog) map[string]any {
	payload := map[string]any{
		"goal":                  plan.Goal,
		"task_source":           record.TaskSource,
		"current_plan":          plan,
		"completed_phases":      record.CompletedPhases,
		"results":               record.Results,
		"remaining_phases":      remaining,
		"remaining_budget":      plan.Budget,
		"steering":              pendingSteering(record),
		"trusted_catalog":       catalog.PlannerView(),
		"repositories":          repositories.PlannerView(),
		"repository_executions": record.RepositoryExecutions,
	}
	return payload
}

func (s *PlanService) replanAtCheckpoint(ctx context.Context, record *dynamicplan.Record) error {
	plan := latestPlan(record)
	if len(record.Revisions) >= plan.Budget.MaxIterations {
		return fmt.Errorf("plan revision limit reached: %d of %d", len(record.Revisions), plan.Budget.MaxIterations)
	}
	if s.dynamicRunCount(record)+1 > plan.Budget.MaxChildRuns {
		return fmt.Errorf("dynamic plan run budget exhausted before replanning")
	}
	remaining := dynamicplan.PendingPhases(plan, record.CompletedPhases)
	catalog, err := s.catalogForRecord(record)
	if err != nil {
		return err
	}
	repositories, err := s.repositoriesForRecord(ctx, record)
	if err != nil {
		return err
	}
	payload := buildReplannerPayload(plan, record, remaining, catalog, repositories)
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode dynamic replanner input: %w", err)
	}
	resolved, err := profile.Resolve(record.Profile, s.Workspace)
	if err != nil {
		return err
	}
	replanPath := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "dynamic-replan.yaml")
	started, err := s.Runs.Start(ctx, StartRequest{Selector: replanPath, Input: string(raw), ConfigPath: resolved.ConfigPath})
	if err != nil {
		return fmt.Errorf("dynamic replanner: %w", err)
	}
	record.ReplannerRunIDs = append(record.ReplannerRunIDs, started.RunID)
	if started.State == nil || started.State.Status != store.RunCompleted {
		return fmt.Errorf("dynamic replanner did not complete")
	}
	if s.exceededTokenBudget(record, plan.Budget.MaxTokens) {
		return fmt.Errorf("dynamic plan token budget exceeded by replanner")
	}
	var decision dynamicplan.ReplanDecision
	if err := json.Unmarshal([]byte(started.State.Output), &decision); err != nil {
		return fmt.Errorf("decode replan decision: %w", err)
	}
	switch decision.Action {
	case "continue":
		if len(decision.Phases) != 0 {
			return fmt.Errorf("continue requires no replacement phases")
		}
		record.CurrentSegment++
		if record.CurrentSegment >= len(record.PendingSegments) {
			record.Status = "completed"
			record.CurrentRunID = ""
			markSteeringApplied(record)
			return nil
		}
		if err := s.startDynamicSegment(ctx, record); err != nil {
			return err
		}
		markSteeringApplied(record)
		return nil
	case "replace_remaining":
		if len(decision.Phases) == 0 {
			return fmt.Errorf("replace_remaining requires phases")
		}
		combined := completedPhaseDefinitions(plan, record.CompletedPhases)
		combined = append(combined, decision.Phases...)
		revised := plan
		revised.Phases = combined
		dynamicplan.Normalize(&revised)
		if err := dynamicplan.ValidateWithCatalogAndRepositories(revised, catalog, repositories); err != nil {
			return fmt.Errorf("validate revised plan: %w", err)
		}
		if len(record.Revisions) >= revised.Budget.MaxIterations {
			return fmt.Errorf("plan revision limit reached: %d of %d", len(record.Revisions), revised.Budget.MaxIterations)
		}
		record.Revisions = append(record.Revisions, dynamicplan.Revision{Number: len(record.Revisions) + 1, Reason: decision.Reason, CreatedAt: time.Now().UTC(), Plan: revised})
		record.MergeOrder = dynamicplan.RepositoryMergeOrder(revised)
		record.PendingSegments = dynamicplan.Segments(dynamicplan.PendingPhases(revised, record.CompletedPhases))
		record.CurrentSegment = 0
		if len(record.PendingSegments) == 0 {
			record.Status = "completed"
			record.CurrentRunID = ""
			markSteeringApplied(record)
			return nil
		}
		if err := s.startDynamicSegment(ctx, record); err != nil {
			return err
		}
		markSteeringApplied(record)
		return nil
	case "finish":
		if len(decision.Phases) != 0 {
			return fmt.Errorf("finish requires no phases")
		}
		record.Status = "completed"
		record.CurrentRunID = ""
		markSteeringApplied(record)
		return nil
	case "ask_user":
		if len(decision.Phases) != 0 {
			return fmt.Errorf("ask_user requires no phases")
		}
		record.Status = "waiting"
		record.CurrentRunID = ""
		record.LastError = decision.Reason
		markSteeringApplied(record)
		return nil
	default:
		return fmt.Errorf("unsupported replan action %q", decision.Action)
	}
}

func (s *PlanService) startDynamicSegment(ctx context.Context, record *dynamicplan.Record) error {
	if record.CurrentSegment < 0 || record.CurrentSegment >= len(record.PendingSegments) {
		return fmt.Errorf("no dynamic segment %d", record.CurrentSegment)
	}
	plan := latestPlan(record)
	resolved, err := profile.Resolve(record.Profile, s.Workspace)
	if err != nil {
		return err
	}
	catalog, err := s.catalogForRecord(record)
	if err != nil {
		return err
	}
	repositories, err := s.repositoriesForRecord(ctx, record)
	if err != nil {
		return err
	}
	segmentName := fmt.Sprintf("execution-%03d-revision-%03d-segment-%03d.yaml", len(record.ExecutionRunIDs)+1, len(record.Revisions), record.CurrentSegment+1)
	path := filepath.Join((dynamicplan.Store{Workspace: s.Workspace}).Dir(record.ID), segmentName)
	contextRaw, _ := json.Marshal(map[string]any{"goal": plan.Goal, "task_source": record.TaskSource, "results": record.Results, "steering": pendingSteering(record)})
	blocks := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "blocks")
	segmentBudget := plan.Budget
	usedRuns := s.dynamicRunCount(record)
	// Reserve one Run for the generated segment wrapper itself. The remaining
	// budget is available to governed phase and fan-out child Runs.
	segmentBudget.MaxChildRuns -= usedRuns + 1
	if segmentBudget.MaxChildRuns < 1 {
		return fmt.Errorf("dynamic plan run budget exhausted: used %d of %d, segment wrapper requires one additional Run", usedRuns, plan.Budget.MaxChildRuns)
	}
	wf, err := dynamicplan.Compile(record.PendingSegments[record.CurrentSegment], segmentBudget, dynamicplan.CompileOptions{WorkflowName: fmt.Sprintf("dynamic-%s-r%d-s%d", strings.TrimPrefix(record.ID, "plan-"), len(record.Revisions), record.CurrentSegment+1), OutputPath: path, BlocksDir: blocks, Goal: plan.Goal, Context: string(contextRaw), Catalog: catalog, GovernanceContext: catalog.GovernanceJSON(), Signals: routeSignals(record), Repositories: repositories})
	if err != nil {
		return err
	}
	if err := dynamicplan.WriteWorkflow(path, wf); err != nil {
		return err
	}
	if _, err := workflow.Load(path); err != nil {
		return fmt.Errorf("validate compiled dynamic workflow: %w", err)
	}
	startRequest := StartRequest{Selector: path, Input: string(contextRaw), ConfigPath: record.ConfigPath, Detached: true}
	if strings.TrimSpace(record.ExecutionWorkspace) != "" {
		worktree := false
		startRequest.Worktree = &worktree
		startRequest.ExecutionWorkspace = record.ExecutionWorkspace
	}
	started, err := s.Runs.Start(ctx, startRequest)
	if err != nil {
		return err
	}
	record.CurrentRunID = started.RunID
	record.ExecutionRunIDs = append(record.ExecutionRunIDs, started.RunID)
	record.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *PlanService) repositoriesForRecord(ctx context.Context, record *dynamicplan.Record) (*workspacecatalog.Catalog, error) {
	repositories, err := workspacecatalog.Load(ctx, s.Workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace repositories: %w", err)
	}
	if record != nil && record.RepositoryCatalogFingerprint != "" && repositories.Fingerprint != record.RepositoryCatalogFingerprint {
		return nil, fmt.Errorf("workspace repository catalog changed since planning: stored=%s current=%s", record.RepositoryCatalogFingerprint, repositories.Fingerprint)
	}
	return repositories, nil
}

func (s *PlanService) resolvePlanRecord(planID, runID string) (*dynamicplan.Record, error) {
	st := dynamicplan.Store{Workspace: s.Workspace}
	if strings.TrimSpace(planID) != "" {
		return st.Load(planID)
	}
	if strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("plan_id or run_id is required")
	}
	records, err := st.List()
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.CurrentRunID == runID || containsString(record.ExecutionRunIDs, runID) {
			return record, nil
		}
	}
	return nil, fmt.Errorf("no dynamic plan owns run %s", runID)
}

func latestPlan(record *dynamicplan.Record) dynamicplan.Plan {
	return record.Revisions[len(record.Revisions)-1].Plan
}

func completedPhaseDefinitions(plan dynamicplan.Plan, completed []string) []dynamicplan.Phase {
	set := map[string]bool{}
	for _, id := range completed {
		set[id] = true
	}
	out := make([]dynamicplan.Phase, 0, len(completed))
	for _, phase := range plan.Phases {
		if set[phase.ID] {
			out = append(out, phase)
		}
	}
	return out
}

func routeSignals(record *dynamicplan.Record) []string {
	if record == nil || len(record.Route) == 0 {
		return nil
	}
	var route taskroute.Decision
	if err := json.Unmarshal(record.Route, &route); err != nil {
		return nil
	}
	return append([]string(nil), route.Signals...)
}

func pendingSteering(record *dynamicplan.Record) []string {
	var out []string
	for _, item := range record.Steering {
		if !item.Applied {
			out = append(out, item.Message)
		}
	}
	return out
}
func markSteeringApplied(record *dynamicplan.Record) {
	for i := range record.Steering {
		record.Steering[i].Applied = true
	}
}
func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func (s *PlanService) dynamicRunIDs(record *dynamicplan.Record) []string {
	seen := map[string]bool{}
	var ordered []string
	var visit func(string)
	visit = func(runID string) {
		if strings.TrimSpace(runID) == "" || seen[runID] {
			return
		}
		seen[runID] = true
		ordered = append(ordered, runID)
		state, err := s.Runs.GetRun(runID)
		if err != nil {
			return
		}
		for _, childID := range state.ChildRunIDs {
			visit(childID)
		}
	}
	visit(record.PlannerRunID)
	for _, runID := range record.ReplannerRunIDs {
		visit(runID)
	}
	for _, runID := range record.ExecutionRunIDs {
		visit(runID)
	}
	return ordered
}

func (s *PlanService) dynamicRunCount(record *dynamicplan.Record) int {
	return len(s.dynamicRunIDs(record))
}

func (s *PlanService) exceededTokenBudget(record *dynamicplan.Record, limit int) bool {
	if limit < 1 {
		return true
	}
	total := 0
	for _, runID := range s.dynamicRunIDs(record) {
		state, err := s.Runs.GetRun(runID)
		if err != nil || state.Usage == nil {
			continue
		}
		total += state.Usage.InputTokens + state.Usage.OutputTokens
	}
	return total > limit
}

func catalogForResolved(resolved *profile.Resolved) (*blockcatalog.Catalog, error) {
	if resolved == nil || len(resolved.BlockPackagePaths) == 0 {
		return nil, fmt.Errorf("profile %q does not declare trusted block packages", resolved.Name)
	}
	return blockcatalog.Load(resolved.BlockPackagePaths)
}

func (s *PlanService) catalogForRecord(record *dynamicplan.Record) (*blockcatalog.Catalog, error) {
	paths := append([]string(nil), record.BlockPackagePaths...)
	if len(paths) == 0 {
		resolved, err := profile.Resolve(record.Profile, s.Workspace)
		if err != nil {
			return nil, err
		}
		paths = resolved.BlockPackagePaths
	}
	catalog, err := blockcatalog.Load(paths)
	if err != nil {
		return nil, err
	}
	if record.BlockCatalogFingerprint != "" && catalog.Fingerprint != record.BlockCatalogFingerprint {
		return nil, fmt.Errorf("trusted block catalog changed since plan creation: expected %s, got %s", record.BlockCatalogFingerprint, catalog.Fingerprint)
	}
	return catalog, nil
}

func newPlanID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "plan-" + hex.EncodeToString(raw[:]), nil
}
