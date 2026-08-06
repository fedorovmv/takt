package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/profile"
	"takt/internal/store"
	"takt/internal/workflow"
)

type PlanRequest struct {
	Goal      string            `json:"goal"`
	Profile   string            `json:"profile,omitempty"`
	Candidate *dynamicplan.Plan `json:"candidate,omitempty"`
}

type PlanResult struct {
	PlanID               string              `json:"plan_id"`
	Decision             string              `json:"decision"`
	ExistingWorkflow     string              `json:"existing_workflow,omitempty"`
	Preview              string              `json:"preview"`
	RequiresConfirmation bool                `json:"requires_confirmation"`
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
	Preview       string              `json:"preview"`
	Phases        []PlanPhaseView     `json:"phases,omitempty"`
	Runs          []PlanRunView       `json:"runs,omitempty"`
	ArtifactCount int                 `json:"artifact_count"`
}

type ExecutePlanRequest struct {
	PlanID  string `json:"plan_id"`
	Confirm bool   `json:"confirm,omitempty"`
}

type SteerRequest struct {
	PlanID  string `json:"plan_id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message"`
}

func (s *Service) Plan(ctx context.Context, request PlanRequest) (*PlanResult, error) {
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
	var plan dynamicplan.Plan
	plannerRunID := ""
	if request.Candidate != nil {
		plan = *request.Candidate
		if strings.TrimSpace(plan.Goal) == "" {
			plan.Goal = goal
		}
	} else {
		plannerPath := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "dynamic-plan.yaml")
		started, startErr := s.Start(ctx, StartRequest{Selector: plannerPath, Input: goal, ConfigPath: resolved.ConfigPath})
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
	dynamicplan.Normalize(&plan)
	if plan.Goal == "" {
		plan.Goal = goal
	}
	if plan.Decision == "existing" && !strings.Contains(plan.ExistingWorkflow, ":") {
		plan.ExistingWorkflow = profileName + ":" + plan.ExistingWorkflow
	}
	if err := dynamicplan.Validate(plan); err != nil {
		return nil, err
	}
	if plan.Decision == "existing" {
		if _, err := s.DescribeWorkflow(plan.ExistingWorkflow); err != nil {
			return nil, fmt.Errorf("planned existing workflow: %w", err)
		}
	}
	id, err := newPlanID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	record := &dynamicplan.Record{
		ID: id, Status: "draft", Profile: profileName, ConfigPath: resolved.ConfigPath,
		CreatedAt: now, UpdatedAt: now, RequiresConfirmation: plan.Decision == "planned",
		PlannerRunID: plannerRunID, Results: map[string]string{},
		Revisions: []dynamicplan.Revision{{Number: 1, Reason: "initial plan", CreatedAt: now, Plan: plan}},
	}
	if plan.Decision == "planned" {
		record.PendingSegments = dynamicplan.Segments(plan.Phases)
	}
	if err := (dynamicplan.Store{Workspace: s.Workspace}).Save(record); err != nil {
		return nil, err
	}
	return &PlanResult{PlanID: id, Decision: plan.Decision, ExistingWorkflow: plan.ExistingWorkflow, Preview: dynamicplan.Preview(plan), RequiresConfirmation: record.RequiresConfirmation, Record: record}, nil
}

func (s *Service) GetPlan(planID string) (*PlanView, error) {
	record, err := (dynamicplan.Store{Workspace: s.Workspace}).Load(planID)
	if err != nil {
		return nil, err
	}
	plan := latestPlan(record)
	completed := map[string]bool{}
	for _, id := range record.CompletedPhases {
		completed[id] = true
	}
	view := &PlanView{Record: record, Preview: dynamicplan.Preview(plan)}
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
		run, runErr := s.GetRun(runID)
		if runErr != nil {
			view.Runs = append(view.Runs, PlanRunView{RunID: runID, Status: "unavailable", Error: runErr.Error()})
			continue
		}
		view.ArtifactCount += len(run.Artifacts)
		view.Runs = append(view.Runs, PlanRunView{RunID: run.ID, Status: run.Status, CurrentNode: run.CurrentNode, ArtifactCount: len(run.Artifacts), Usage: run.Usage, Error: run.Error})
	}
	return view, nil
}

func (s *Service) ExecutePlan(ctx context.Context, request ExecutePlanRequest) (*dynamicplan.Record, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	st := dynamicplan.Store{Workspace: s.Workspace}
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
	now := time.Now().UTC()
	if record.ConfirmedAt == nil {
		record.ConfirmedAt = &now
	}
	record.Status = "running"
	record.LastError = ""
	record.UpdatedAt = now
	plan := latestPlan(record)
	if plan.Decision == "existing" {
		started, err := s.Start(ctx, StartRequest{Selector: plan.ExistingWorkflow, Input: plan.Goal, Detached: true})
		if err != nil {
			record.Status = "failed"
			record.LastError = err.Error()
			_ = st.Save(record)
			return nil, err
		}
		record.CurrentRunID = started.RunID
		record.ExecutionRunIDs = append(record.ExecutionRunIDs, started.RunID)
		if err := st.Save(record); err != nil {
			return nil, err
		}
		return record, nil
	}
	if len(record.PendingSegments) == 0 {
		record.PendingSegments = dynamicplan.Segments(dynamicplan.PendingPhases(plan, record.CompletedPhases))
	}
	if err := s.startDynamicSegment(ctx, record); err != nil {
		record.Status = "failed"
		record.LastError = err.Error()
		_ = st.Save(record)
		return nil, err
	}
	if err := st.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) Steer(ctx context.Context, request SteerRequest) (*dynamicplan.Record, error) {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	if strings.TrimSpace(request.Message) == "" {
		return nil, fmt.Errorf("steering message is required")
	}
	record, err := s.resolvePlanRecord(request.PlanID, request.RunID)
	if err != nil {
		return nil, err
	}
	if record.Status != "running" && record.Status != "waiting" {
		return nil, fmt.Errorf("plan %s cannot be steered from status %s", record.ID, record.Status)
	}
	record.Steering = append(record.Steering, dynamicplan.Steering{Message: request.Message, CreatedAt: time.Now().UTC()})
	record.UpdatedAt = time.Now().UTC()
	if record.Status == "waiting" {
		record.Status = "running"
		record.LastError = ""
		if err := s.replanAtCheckpoint(ctx, record); err != nil {
			record.Status = "waiting"
			record.LastError = err.Error()
		}
	}
	if err := (dynamicplan.Store{Workspace: s.Workspace}).Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) PromotePlan(planID, name string) (*dynamicplan.Record, error) {
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
	name = dynamicplan.SafeWorkflowName(name)
	output := filepath.Join(s.Workspace, ".takt", "workflows", "generated", name+".yaml")
	blocks := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "blocks")
	wf, err := dynamicplan.Compile(plan.Phases, plan.Budget, dynamicplan.CompileOptions{WorkflowName: name, OutputPath: output, BlocksDir: blocks, Goal: plan.Goal, Promoted: true})
	if err != nil {
		return nil, err
	}
	if err := dynamicplan.WriteWorkflow(output, wf); err != nil {
		return nil, err
	}
	if _, err := workflow.Load(output); err != nil {
		_ = os.Remove(output)
		return nil, fmt.Errorf("validate promoted workflow: %w", err)
	}
	record.PromotedPath = output
	record.UpdatedAt = time.Now().UTC()
	if err := st.Save(record); err != nil {
		return nil, err
	}
	return record, nil
}

func (s *Service) AdvanceDynamicPlans(ctx context.Context) error {
	s.dynamicMu.Lock()
	defer s.dynamicMu.Unlock()
	st := dynamicplan.Store{Workspace: s.Workspace}
	records, err := st.List()
	if err != nil {
		return err
	}
	var failures []string
	for _, record := range records {
		if record.Status != "running" || record.CurrentRunID == "" {
			continue
		}
		if err := s.advanceDynamicRecord(ctx, record); err != nil {
			record.Status = "waiting"
			record.LastError = err.Error()
			record.UpdatedAt = time.Now().UTC()
			_ = st.Save(record)
			failures = append(failures, record.ID+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("advance dynamic plans: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (s *Service) advanceDynamicRecord(ctx context.Context, record *dynamicplan.Record) error {
	run, err := s.GetRun(record.CurrentRunID)
	if err != nil {
		return err
	}
	if run.Status == store.RunRunning || run.Status == store.RunWaiting {
		return nil
	}
	st := dynamicplan.Store{Workspace: s.Workspace}
	if run.Status != store.RunCompleted {
		record.Status = "failed"
		record.LastError = fmt.Sprintf("segment run %s ended with %s: %s", run.ID, run.Status, run.Error)
		record.UpdatedAt = time.Now().UTC()
		return st.Save(record)
	}
	if latestPlan(record).Decision == "existing" {
		record.Status = "completed"
		record.CurrentRunID = ""
		record.Results["workflow"] = run.Output
		record.UpdatedAt = time.Now().UTC()
		return st.Save(record)
	}
	if record.CurrentSegment >= len(record.PendingSegments) {
		return fmt.Errorf("current segment %d is outside pending segments", record.CurrentSegment)
	}
	segment := record.PendingSegments[record.CurrentSegment]
	for _, phase := range segment {
		if node := run.Nodes[phase.ID]; node != nil {
			record.Results[phase.ID] = node.Output
		}
		if !containsString(record.CompletedPhases, phase.ID) {
			record.CompletedPhases = append(record.CompletedPhases, phase.ID)
		}
	}
	if s.exceededTokenBudget(record, latestPlan(record).Budget.MaxTokens) {
		record.Status = "failed"
		record.LastError = "dynamic plan token budget exceeded at phase boundary"
		record.UpdatedAt = time.Now().UTC()
		return st.Save(record)
	}
	checkpoint := len(segment) > 0 && segment[len(segment)-1].Checkpoint
	if checkpoint && len(record.Revisions) < latestPlan(record).Budget.MaxIterations {
		if err := s.replanAtCheckpoint(ctx, record); err != nil {
			return err
		}
		if record.Status != "running" {
			return st.Save(record)
		}
		return st.Save(record)
	}
	record.CurrentSegment++
	if record.CurrentSegment >= len(record.PendingSegments) {
		record.Status = "completed"
		record.CurrentRunID = ""
		record.UpdatedAt = time.Now().UTC()
		return st.Save(record)
	}
	if err := s.startDynamicSegment(ctx, record); err != nil {
		return err
	}
	return st.Save(record)
}

func (s *Service) replanAtCheckpoint(ctx context.Context, record *dynamicplan.Record) error {
	plan := latestPlan(record)
	remaining := dynamicplan.PendingPhases(plan, record.CompletedPhases)
	payload := map[string]any{"goal": plan.Goal, "current_plan": plan, "completed_phases": record.CompletedPhases, "results": record.Results, "remaining_phases": remaining, "remaining_budget": plan.Budget, "steering": pendingSteering(record)}
	raw, _ := json.Marshal(payload)
	resolved, err := profile.Resolve(record.Profile, s.Workspace)
	if err != nil {
		return err
	}
	replanPath := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "dynamic-replan.yaml")
	started, err := s.Start(ctx, StartRequest{Selector: replanPath, Input: string(raw), ConfigPath: resolved.ConfigPath})
	if err != nil {
		return fmt.Errorf("dynamic replanner: %w", err)
	}
	if started.State == nil || started.State.Status != store.RunCompleted {
		return fmt.Errorf("dynamic replanner did not complete")
	}
	var decision dynamicplan.ReplanDecision
	if err := json.Unmarshal([]byte(started.State.Output), &decision); err != nil {
		return fmt.Errorf("decode replan decision: %w", err)
	}
	markSteeringApplied(record)
	switch decision.Action {
	case "continue":
		record.CurrentSegment++
		if record.CurrentSegment >= len(record.PendingSegments) {
			record.Status = "completed"
			record.CurrentRunID = ""
			return nil
		}
		return s.startDynamicSegment(ctx, record)
	case "replace_remaining":
		if len(decision.Phases) == 0 {
			return fmt.Errorf("replace_remaining requires phases")
		}
		combined := completedPhaseDefinitions(plan, record.CompletedPhases)
		combined = append(combined, decision.Phases...)
		revised := plan
		revised.Phases = combined
		dynamicplan.Normalize(&revised)
		if err := dynamicplan.Validate(revised); err != nil {
			return fmt.Errorf("validate revised plan: %w", err)
		}
		record.Revisions = append(record.Revisions, dynamicplan.Revision{Number: len(record.Revisions) + 1, Reason: decision.Reason, CreatedAt: time.Now().UTC(), Plan: revised})
		record.PendingSegments = dynamicplan.Segments(dynamicplan.PendingPhases(revised, record.CompletedPhases))
		record.CurrentSegment = 0
		if len(record.PendingSegments) == 0 {
			record.Status = "completed"
			record.CurrentRunID = ""
			return nil
		}
		return s.startDynamicSegment(ctx, record)
	case "finish":
		record.Status = "completed"
		record.CurrentRunID = ""
		return nil
	case "ask_user":
		record.Status = "waiting"
		record.CurrentRunID = ""
		record.LastError = decision.Reason
		return nil
	default:
		return fmt.Errorf("unsupported replan action %q", decision.Action)
	}
}

func (s *Service) startDynamicSegment(ctx context.Context, record *dynamicplan.Record) error {
	if record.CurrentSegment < 0 || record.CurrentSegment >= len(record.PendingSegments) {
		return fmt.Errorf("no dynamic segment %d", record.CurrentSegment)
	}
	plan := latestPlan(record)
	resolved, err := profile.Resolve(record.Profile, s.Workspace)
	if err != nil {
		return err
	}
	path := filepath.Join((dynamicplan.Store{Workspace: s.Workspace}).Dir(record.ID), fmt.Sprintf("revision-%03d-segment-%03d.yaml", len(record.Revisions), record.CurrentSegment+1))
	contextRaw, _ := json.Marshal(map[string]any{"goal": plan.Goal, "results": record.Results, "steering": pendingSteering(record)})
	blocks := filepath.Join(filepath.Dir(resolved.ManifestPath), "workflows", "blocks")
	segmentBudget := plan.Budget
	usedChildRuns := s.dynamicChildRunCount(record)
	segmentBudget.MaxChildRuns -= usedChildRuns
	if segmentBudget.MaxChildRuns < 1 {
		return fmt.Errorf("dynamic plan child-run budget exhausted: used %d of %d", usedChildRuns, plan.Budget.MaxChildRuns)
	}
	wf, err := dynamicplan.Compile(record.PendingSegments[record.CurrentSegment], segmentBudget, dynamicplan.CompileOptions{WorkflowName: fmt.Sprintf("dynamic-%s-r%d-s%d", strings.TrimPrefix(record.ID, "plan-"), len(record.Revisions), record.CurrentSegment+1), OutputPath: path, BlocksDir: blocks, Goal: plan.Goal, Context: string(contextRaw)})
	if err != nil {
		return err
	}
	if err := dynamicplan.WriteWorkflow(path, wf); err != nil {
		return err
	}
	if _, err := workflow.Load(path); err != nil {
		return fmt.Errorf("validate compiled dynamic workflow: %w", err)
	}
	started, err := s.Start(ctx, StartRequest{Selector: path, Input: string(contextRaw), ConfigPath: record.ConfigPath, Detached: true})
	if err != nil {
		return err
	}
	record.CurrentRunID = started.RunID
	record.ExecutionRunIDs = append(record.ExecutionRunIDs, started.RunID)
	record.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *Service) resolvePlanRecord(planID, runID string) (*dynamicplan.Record, error) {
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

func (s *Service) dynamicChildRunCount(record *dynamicplan.Record) int {
	seen := map[string]bool{}
	var visit func(string)
	visit = func(runID string) {
		state, err := s.GetRun(runID)
		if err != nil {
			return
		}
		for _, childID := range state.ChildRunIDs {
			if seen[childID] {
				continue
			}
			seen[childID] = true
			visit(childID)
		}
	}
	for _, runID := range record.ExecutionRunIDs {
		visit(runID)
	}
	return len(seen)
}

func (s *Service) exceededTokenBudget(record *dynamicplan.Record, limit int) bool {
	if limit <= 0 {
		return false
	}
	total := 0
	for _, runID := range record.ExecutionRunIDs {
		state, err := s.GetRun(runID)
		if err != nil || state.Usage == nil {
			continue
		}
		total += state.Usage.InputTokens + state.Usage.OutputTokens
	}
	return total > limit
}

func newPlanID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "plan-" + hex.EncodeToString(raw[:]), nil
}
