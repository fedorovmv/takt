package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"takt/internal/dynamicplan"
	"takt/internal/hostcontrol"
)

type HostBeginRequest struct {
	Host          string                   `json:"host"`
	HostSessionID string                   `json:"host_session_id"`
	Goal          string                   `json:"goal"`
	Profile       string                   `json:"profile,omitempty"`
	Enforcement   string                   `json:"enforcement,omitempty"`
	Capabilities  hostcontrol.Capabilities `json:"capabilities,omitempty"`
	Candidate     *dynamicplan.Plan        `json:"candidate,omitempty"`
}

type HostBeginResult struct {
	Session *hostcontrol.Session `json:"session"`
	Plan    *PlanResult          `json:"plan"`
}

type HostConfirmRequest struct {
	SessionID string `json:"session_id"`
	Confirm   bool   `json:"confirm"`
	// Detached is selected by the transport. Daemon-backed host integrations
	// set it to true; direct CLI calls may keep the process in the foreground.
	Detached bool `json:"-"`
}

type HostSessionView struct {
	Session *hostcontrol.Session `json:"session"`
	Plan    *PlanView            `json:"plan"`
}

type HostToolGuardRequest struct {
	SessionID string `json:"session_id"`
	Tool      string `json:"tool"`
	ReadOnly  bool   `json:"read_only,omitempty"`
}

type HostCompletionGuardRequest struct {
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
}

type HostGuardDecision struct {
	Allowed     bool   `json:"allowed"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	PlanID      string `json:"plan_id"`
	SessionID   string `json:"session_id"`
	RequiredOp  string `json:"required_operation,omitempty"`
	Enforcement string `json:"enforcement"`
}

func (s *HostService) BeginHostSession(ctx context.Context, request HostBeginRequest) (*HostBeginResult, error) {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	host := strings.TrimSpace(request.Host)
	hostSessionID := strings.TrimSpace(request.HostSessionID)
	if host == "" || hostSessionID == "" {
		return nil, fmt.Errorf("host and host_session_id are required")
	}
	enforcement := strings.ToLower(strings.TrimSpace(request.Enforcement))
	if enforcement == "" {
		enforcement = hostcontrol.EnforcementAdvisory
	}
	switch enforcement {
	case hostcontrol.EnforcementAdvisory, hostcontrol.EnforcementGuarded:
	case hostcontrol.EnforcementStrict:
		if !request.Capabilities.StrictReady() {
			return nil, fmt.Errorf("strict host control requires command, input, tool-call, completion, and recovery interception")
		}
	default:
		return nil, fmt.Errorf("unsupported host enforcement %q", enforcement)
	}
	store := hostcontrol.Store{Workspace: s.Workspace}
	release, err := acquireHostStoreLock(store)
	if err != nil {
		return nil, err
	}
	defer release()
	if existing, findErr := store.Find(host, hostSessionID); findErr == nil {
		if enforcement == hostcontrol.EnforcementStrict && (existing.Enforcement != hostcontrol.EnforcementStrict || !existing.Capabilities.StrictReady()) {
			return nil, fmt.Errorf("existing host session %s does not satisfy strict host control", existing.ID)
		}
		view, viewErr := s.Plans.GetPlan(existing.PlanID)
		if viewErr != nil {
			return nil, viewErr
		}
		plan := latestPlan(view.Record)
		return &HostBeginResult{Session: existing, Plan: &PlanResult{PlanID: existing.PlanID, Decision: plan.Decision, ExistingWorkflow: plan.ExistingWorkflow, Preview: view.Preview, RequiresConfirmation: view.Record.RequiresConfirmation, Record: view.Record}}, nil
	} else if !os.IsNotExist(findErr) {
		return nil, findErr
	}
	plan, err := s.Plans.Plan(ctx, PlanRequest{Goal: request.Goal, Profile: request.Profile, Candidate: request.Candidate})
	if err != nil {
		return nil, err
	}
	id, err := newHostSessionID()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := &hostcontrol.Session{ID: id, Host: host, HostSessionID: hostSessionID, PlanID: plan.PlanID, Status: hostcontrol.StatusPreview, Enforcement: enforcement, Capabilities: request.Capabilities, CreatedAt: now, UpdatedAt: now}
	if err := store.Save(session); err != nil {
		return nil, err
	}
	return &HostBeginResult{Session: session, Plan: plan}, nil
}

func acquireHostStoreLock(store hostcontrol.Store) (func() error, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		release, err := store.AcquireLock()
		if err == nil {
			return release, nil
		}
		if !strings.Contains(err.Error(), "locked by another process") || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (s *HostService) ConfirmHostSession(ctx context.Context, request HostConfirmRequest) (*HostSessionView, error) {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	store := hostcontrol.Store{Workspace: s.Workspace}
	session, err := store.Load(request.SessionID)
	if err != nil {
		return nil, err
	}
	if session.Status == hostcontrol.StatusReleased {
		return nil, fmt.Errorf("host session %s is released", session.ID)
	}
	record, err := s.Plans.ExecutePlan(ctx, ExecutePlanRequest{PlanID: session.PlanID, Confirm: request.Confirm, Detached: request.Detached})
	if err != nil {
		return nil, err
	}
	session.Status = hostStatusForPlan(record.Status)
	session.UpdatedAt = time.Now().UTC()
	if err := store.Save(session); err != nil {
		return nil, err
	}
	plan, err := s.Plans.GetPlan(session.PlanID)
	if err != nil {
		return nil, err
	}
	return &HostSessionView{Session: session, Plan: plan}, nil
}

func (s *HostService) GetHostSession(sessionID string) (*HostSessionView, error) {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	store := hostcontrol.Store{Workspace: s.Workspace}
	session, err := store.Load(sessionID)
	if err != nil {
		return nil, err
	}
	return s.refreshHostSession(store, session)
}

func (s *HostService) FindHostSession(host, hostSessionID string) (*HostSessionView, error) {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	store := hostcontrol.Store{Workspace: s.Workspace}
	session, err := store.Find(strings.TrimSpace(host), strings.TrimSpace(hostSessionID))
	if err != nil {
		return nil, err
	}
	return s.refreshHostSession(store, session)
}

func (s *HostService) GuardHostTool(request HostToolGuardRequest) (*HostGuardDecision, error) {
	view, err := s.GetHostSession(request.SessionID)
	if err != nil {
		return nil, err
	}
	status := view.Session.Status
	decision := &HostGuardDecision{Status: status, PlanID: view.Session.PlanID, SessionID: view.Session.ID, Enforcement: view.Session.Enforcement}
	if status == hostcontrol.StatusReleased || status == hostcontrol.StatusCompleted || status == hostcontrol.StatusFailed {
		decision.Allowed = true
		decision.Reason = "Takt managed mode is not active"
		return decision, nil
	}
	tool := strings.ToLower(strings.TrimSpace(request.Tool))
	if hostControlTool(tool) {
		decision.Allowed = true
		decision.Reason = "Takt control operation is allowed in managed mode"
		return decision, nil
	}
	if hostReadOnlyTool(tool) {
		decision.Allowed = true
		decision.Reason = "known read-only inspection tool is allowed in managed mode"
		return decision, nil
	}
	decision.Allowed = false
	decision.RequiredOp = "delegate_to_takt_worker"
	decision.Reason = fmt.Sprintf("tool %q is blocked while Takt plan %s is %s; mutating work must be performed by the current workflow phase", tool, view.Session.PlanID, status)
	return decision, nil
}

func hostControlTool(tool string) bool {
	normalized := strings.ReplaceAll(tool, "_", ".")
	switch normalized {
	case "takt.plan.get",
		"takt.run.get", "takt.run.list", "takt.run.events", "takt.run.children", "takt.run.artifacts", "takt.run.summary", "takt.run.attention",
		"takt.run.steer", "takt.run.answer", "takt.run.pause", "takt.run.resume.paused", "takt.run.retry", "takt.run.fork", "takt.run.abandon", "takt.run.recover", "takt.run.cancel",
		"takt.notify.list", "takt.notify.ack",
		"takt.host.get", "takt.host.find", "takt.host.guard.tool", "takt.host.guard.completion", "takt.host.release":
		return true
	default:
		return false
	}
}

func hostReadOnlyTool(tool string) bool {
	switch tool {
	case "read", "view", "grep", "glob", "find", "ls", "list", "diagnostics":
		return true
	default:
		return false
	}
}

func (s *HostService) GuardHostCompletion(request HostCompletionGuardRequest) (*HostGuardDecision, error) {
	view, err := s.GetHostSession(request.SessionID)
	if err != nil {
		return nil, err
	}
	status := view.Session.Status
	kind := strings.ToLower(strings.TrimSpace(request.Kind))
	decision := &HostGuardDecision{Status: status, PlanID: view.Session.PlanID, SessionID: view.Session.ID, Enforcement: view.Session.Enforcement}
	switch status {
	case hostcontrol.StatusCompleted, hostcontrol.StatusFailed, hostcontrol.StatusReleased:
		decision.Allowed = true
		decision.Reason = "Takt workflow reached a terminal state"
	case hostcontrol.StatusWaiting:
		decision.Allowed = kind == "question" || kind == "status"
		if decision.Allowed {
			decision.Reason = "workflow is waiting; a user question or status update is allowed"
		} else {
			decision.Reason = "final completion is blocked while the workflow waits for user input"
			decision.RequiredOp = "ask_user_or_steer"
		}
	default:
		decision.Allowed = kind == "status"
		if decision.Allowed {
			decision.Reason = "non-final progress update is allowed"
		} else {
			decision.Reason = "final completion is blocked while the Takt workflow is active"
			decision.RequiredOp = "observe_takt_run"
		}
	}
	return decision, nil
}

func (s *HostService) ReleaseHostSession(sessionID string) (*hostcontrol.Session, error) {
	s.hostMu.Lock()
	defer s.hostMu.Unlock()
	store := hostcontrol.Store{Workspace: s.Workspace}
	session, err := store.Load(sessionID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session.Status = hostcontrol.StatusReleased
	session.ReleasedAt = &now
	session.UpdatedAt = now
	if err := store.Save(session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *HostService) refreshHostSession(store hostcontrol.Store, session *hostcontrol.Session) (*HostSessionView, error) {
	plan, err := s.Plans.GetPlan(session.PlanID)
	if err != nil {
		return nil, err
	}
	if session.Status != hostcontrol.StatusReleased {
		status := hostStatusForPlan(plan.Record.Status)
		if session.Status != status {
			session.Status = status
			session.UpdatedAt = time.Now().UTC()
			if err := store.Save(session); err != nil {
				return nil, err
			}
		}
	}
	return &HostSessionView{Session: session, Plan: plan}, nil
}

func hostStatusForPlan(status string) string {
	switch status {
	case "draft":
		return hostcontrol.StatusPreview
	case "running":
		return hostcontrol.StatusManaged
	case "waiting":
		return hostcontrol.StatusWaiting
	case "pausing", "paused":
		return hostcontrol.StatusPaused
	case "abandoned":
		return hostcontrol.StatusFailed
	case "completed":
		return hostcontrol.StatusCompleted
	case "failed":
		return hostcontrol.StatusFailed
	default:
		return hostcontrol.StatusFailed
	}
}

func newHostSessionID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "host-" + hex.EncodeToString(raw[:]), nil
}
