package control

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/execution"
	"takt/internal/runtime"
	"takt/internal/store"
)

type ExternalTask struct {
	RunID                 string                          `json:"run_id"`
	NodeID                string                          `json:"node_id"`
	Status                string                          `json:"status"`
	Attempt               int                             `json:"attempt"`
	Prompt                string                          `json:"prompt"`
	Workspace             string                          `json:"workspace"`
	Assistant             string                          `json:"assistant,omitempty"`
	RequestedModel        *store.ModelRef                 `json:"requested_model,omitempty"`
	SessionMode           string                          `json:"session_mode,omitempty"`
	SessionID             string                          `json:"session_id,omitempty"`
	Policy                *store.NodePolicyState          `json:"policy,omitempty"`
	OutputFormat          json.RawMessage                 `json:"output_format,omitempty"`
	ClaimedBy             string                          `json:"claimed_by,omitempty"`
	ClaimedAt             time.Time                       `json:"claimed_at,omitempty"`
	LeaseExpiresAt        time.Time                       `json:"lease_expires_at,omitempty"`
	ClaimToken            string                          `json:"claim_token,omitempty"`
	CapabilityDeclaration assistant.CapabilityDeclaration `json:"capability_declaration"`
	ToolApproval          *store.ToolApprovalState        `json:"tool_approval,omitempty"`
	ToolCalls             map[string]*store.ToolCallState `json:"tool_calls,omitempty"`
	SideEffectMode        string                          `json:"side_effect_mode,omitempty"`
	IdempotencyKey        string                          `json:"idempotency_key,omitempty"`
	Receipt               string                          `json:"receipt,omitempty"`
	ReconcileStatus       string                          `json:"reconcile_status,omitempty"`
}

type ExternalClaimRequest struct {
	RunID        string
	NodeID       string
	WorkerID     string
	Capabilities []string
	Declaration  assistant.CapabilityDeclaration
	Lease        time.Duration
}

type ExternalSubmission struct {
	RunID            string
	NodeID           string
	ClaimToken       string
	Output           string
	Structured       json.RawMessage
	Stdout           string
	Stderr           string
	ExitCode         int
	SessionID        string
	Resumed          bool
	AssistantVersion string
	ResolvedModel    *store.ModelRef
	Usage            *store.Usage
	ErrorCode        string
	Error            string
}

func (s *Service) PendingExternal(runID string, recursive bool) ([]ExternalTask, error) {
	st := store.FS{Workspace: s.Workspace}
	ids, err := externalRunIDs(st, runID, recursive)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tasks := make([]ExternalTask, 0)
	for _, id := range ids {
		state, loadErr := st.Load(id)
		if loadErr != nil {
			continue
		}
		for nodeID, node := range state.Nodes {
			if node == nil || node.External == nil || !externalReadyForClaim(node) {
				continue
			}
			external := node.External
			if external.Status != "pending" && external.Status != "reconcile_required" && !(external.Status == "claimed" && !external.LeaseExpiresAt.After(now)) {
				continue
			}
			task := externalTask(state.ID, nodeID, external, "")
			if external.Status == "claimed" && !external.LeaseExpiresAt.After(now) && external.SideEffectMode == "reconcile" {
				task.Status = "reconcile_required"
			}
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].RunID != tasks[j].RunID {
			return tasks[i].RunID < tasks[j].RunID
		}
		return tasks[i].NodeID < tasks[j].NodeID
	})
	return tasks, nil
}

func externalRunIDs(st store.FS, runID string, recursive bool) ([]string, error) {
	if strings.TrimSpace(runID) == "" {
		return st.ListRunIDs()
	}
	root, err := st.Load(runID)
	if err != nil {
		return nil, err
	}
	ids := []string{root.ID}
	if !recursive {
		return ids, nil
	}
	seen := map[string]bool{root.ID: true}
	queue := append([]string(nil), root.ChildRunIDs...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		child, err := st.Load(id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
		queue = append(queue, child.ChildRunIDs...)
	}
	return ids, nil
}

func externalReadyForClaim(node *store.NodeState) bool {
	if node == nil || node.External == nil {
		return false
	}
	// The runtime commits external_node.requested before it commits the
	// suspension checkpoint. Expose the task only after the attempt rollback is
	// durable; otherwise a fast worker can race node.suspended and create two
	// writers for the same Run.
	return node.Status == store.NodeWaiting && node.Attempts+1 == node.External.Attempt
}

func (s *Service) ClaimExternal(request ExternalClaimRequest) (*ExternalTask, error) {
	if strings.TrimSpace(request.WorkerID) == "" {
		return nil, fmt.Errorf("worker_id is required")
	}
	if request.Lease <= 0 {
		request.Lease = 15 * time.Minute
	}
	if request.Lease > time.Hour {
		request.Lease = time.Hour
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, node, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	if !externalReadyForClaim(node) {
		return nil, fmt.Errorf("external node %s/%s is still being suspended; retry after it appears in takt.node.pending", state.ID, request.NodeID)
	}
	now := time.Now().UTC()
	if external.Status == "reconcile_required" || (external.Status == "claimed" && !external.LeaseExpiresAt.After(now) && external.SideEffectMode == "reconcile" && external.ReconcileStatus != "not_applied") {
		external.Status = "reconcile_required"
		external.ReconcileStatus = "required"
		node.Status = store.NodeWaiting
		state.Status = store.RunWaiting
		state.Waiting = &store.WaitingState{NodeID: request.NodeID, Message: "external side effect must be reconciled before retry", Kind: "external_reconcile"}
		if err := st.Commit(state, store.Event{Type: "external_node.reconciliation_required", NodeID: request.NodeID, Data: map[string]any{"idempotency_key": external.IdempotencyKey}}); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("external node %s/%s requires side-effect reconciliation before retry", state.ID, request.NodeID)
	}
	if external.Status == "claimed" && external.LeaseExpiresAt.After(now) {
		return nil, fmt.Errorf("external node %s/%s is already claimed by %s until %s", state.ID, request.NodeID, external.ClaimedBy, external.LeaseExpiresAt.Format(time.RFC3339))
	}
	if external.Status != "pending" && external.Status != "claimed" {
		return nil, fmt.Errorf("external node %s/%s cannot be claimed in status %s", state.ID, request.NodeID, external.Status)
	}
	declaration := request.Declaration
	if len(declaration.Capabilities) == 0 {
		declaration.Capabilities = append([]string(nil), request.Capabilities...)
	}
	declaration = assistant.NormalizeDeclaration(declaration)
	if err := requireExternalCapabilities(external.Policy, declaration.Capabilities); err != nil {
		return nil, err
	}
	if external.ToolApproval != nil && !declaration.ToolControl {
		return nil, fmt.Errorf("external executor must declare tool_control for node %s/%s", state.ID, request.NodeID)
	}
	token, err := newClaimToken()
	if err != nil {
		return nil, err
	}
	external.Status = "claimed"
	external.ReconcileStatus = ""
	external.ClaimedBy = request.WorkerID
	encodedDeclaration, _ := json.Marshal(declaration)
	external.CapabilityDeclaration = encodedDeclaration
	external.ClaimToken = token
	external.LeaseExpiresAt = now.Add(request.Lease)
	external.LastActivityAt = now
	node.Status = store.NodeWaiting
	if err := st.Commit(state, store.Event{Type: "external_node.claimed", NodeID: request.NodeID, Data: map[string]any{
		"worker_id": request.WorkerID, "lease_expires_at": external.LeaseExpiresAt, "capability_declaration": declaration,
	}}); err != nil {
		return nil, err
	}
	task := externalTask(state.ID, request.NodeID, external, token)
	return &task, nil
}

func requireExternalCapabilities(policy *store.NodePolicyState, capabilities []string) error {
	if policy == nil || len(policy.Capabilities) == 0 {
		return nil
	}
	available := map[string]bool{}
	for _, capability := range capabilities {
		available[capability] = true
	}
	var missing []string
	for _, capability := range policy.Capabilities {
		if !available[capability] {
			missing = append(missing, capability)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("external executor lacks required capabilities: %s", strings.Join(missing, ", "))
	}
	return nil
}

func newClaimToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func externalTask(runID, nodeID string, external *store.ExternalExecutionState, token string) ExternalTask {
	var declaration assistant.CapabilityDeclaration
	if len(external.CapabilityDeclaration) > 0 {
		_ = json.Unmarshal(external.CapabilityDeclaration, &declaration)
	}
	return ExternalTask{
		RunID: runID, NodeID: nodeID, Status: external.Status, Attempt: external.Attempt,
		Prompt: external.Prompt, Workspace: external.Workspace, Assistant: external.Assistant,
		RequestedModel: external.RequestedModel, SessionMode: external.SessionMode, SessionID: external.SessionID,
		Policy: external.Policy, OutputFormat: external.OutputFormat, ClaimedBy: external.ClaimedBy,
		LeaseExpiresAt: external.LeaseExpiresAt, ClaimToken: token, CapabilityDeclaration: declaration,
		ToolApproval: external.ToolApproval, ToolCalls: cloneToolCalls(external.ToolCalls),
		SideEffectMode: external.SideEffectMode, IdempotencyKey: external.IdempotencyKey, Receipt: external.Receipt, ReconcileStatus: external.ReconcileStatus,
	}
}

type ExternalReconcileRequest struct {
	RunID      string
	NodeID     string
	Outcome    string
	Receipt    string
	Submission ExternalSubmission
}

func (s *Service) ReconcileExternal(ctx context.Context, request ExternalReconcileRequest) (*store.RunState, error) {
	outcome := strings.ToLower(strings.TrimSpace(request.Outcome))
	if outcome != "applied" && outcome != "not_applied" && outcome != "unknown" {
		return nil, fmt.Errorf("reconcile outcome must be applied, not_applied, or unknown")
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	state, node, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		_ = release()
		return nil, err
	}
	now := time.Now().UTC()
	if external.SideEffectMode != "reconcile" {
		_ = release()
		return nil, fmt.Errorf("external node %s/%s does not require reconciliation", request.RunID, request.NodeID)
	}
	if external.Status == "claimed" && external.LeaseExpiresAt.After(now) {
		_ = release()
		return nil, fmt.Errorf("external node %s/%s still has an active claim", request.RunID, request.NodeID)
	}
	if external.Status != "claimed" && external.Status != "reconcile_required" {
		_ = release()
		return nil, fmt.Errorf("external node %s/%s cannot reconcile from status %s", request.RunID, request.NodeID, external.Status)
	}
	external.ReconcileStatus = outcome
	external.Receipt = strings.TrimSpace(request.Receipt)
	switch outcome {
	case "unknown":
		external.Status = "reconcile_required"
		state.Status = store.RunWaiting
		node.Status = store.NodeWaiting
		state.Waiting = &store.WaitingState{NodeID: request.NodeID, Message: "external side effect state remains unknown; operator decision required", Kind: "external_reconcile"}
		err = st.Commit(state, store.Event{Type: "external_node.reconciled", NodeID: request.NodeID, Data: map[string]any{"outcome": outcome, "receipt": external.Receipt}})
		_ = release()
		if err != nil {
			return nil, err
		}
		return state.PublicView(), nil
	case "not_applied":
		external.Status = "pending"
		external.ClaimToken = ""
		external.ClaimedBy = ""
		external.LeaseExpiresAt = time.Time{}
		state.Status = store.RunWaiting
		node.Status = store.NodeWaiting
		state.Waiting = &store.WaitingState{NodeID: request.NodeID, Message: "external executor must claim and complete this node", Kind: "external_node"}
		err = st.Commit(state, store.Event{Type: "external_node.reconciled", NodeID: request.NodeID, Data: map[string]any{"outcome": outcome, "receipt": external.Receipt}})
		_ = release()
		if err != nil {
			return nil, err
		}
		return state.PublicView(), nil
	case "applied":
		if external.Receipt == "" {
			_ = release()
			return nil, fmt.Errorf("receipt is required when reconciliation confirms an applied side effect")
		}
		token, tokenErr := newClaimToken()
		if tokenErr != nil {
			_ = release()
			return nil, tokenErr
		}
		external.Status = "claimed"
		external.ClaimToken = token
		external.ClaimedBy = "reconciler"
		external.LeaseExpiresAt = now.Add(time.Minute)
		err = st.Commit(state, store.Event{Type: "external_node.reconciled", NodeID: request.NodeID, Data: map[string]any{"outcome": outcome, "receipt": external.Receipt}})
		_ = release()
		if err != nil {
			return nil, err
		}
		sub := request.Submission
		sub.RunID, sub.NodeID, sub.ClaimToken = request.RunID, request.NodeID, token
		return s.submitExternal(ctx, sub, false)
	}
	_ = release()
	return nil, fmt.Errorf("unreachable reconcile outcome")
}

func (s *Service) AppendExternalEvent(runID, nodeID, claimToken string, event assistant.Event) (uint64, error) {
	switch event.Type {
	case assistant.EventToolRequested, assistant.EventToolAllowed, assistant.EventToolDenied, assistant.EventToolStarted, assistant.EventToolCompleted:
		return 0, fmt.Errorf("tool lifecycle events must use takt.node.tool.* controls")
	case assistant.EventArtifactDeclared:
		return 0, fmt.Errorf("artifact declarations must use takt.node.artifact.declare")
	}
	if err := assistant.ValidateEvent(event); err != nil {
		return 0, err
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, runID)
	if err != nil {
		return 0, err
	}
	defer release()
	state, _, external, err := loadExternalNode(st, runID, nodeID)
	if err != nil {
		return 0, err
	}
	if err := verifyExternalClaim(external, claimToken); err != nil {
		return 0, err
	}
	return appendExternalAssistantEvent(st, state, nodeID, external, event)
}

func (s *Service) CompleteExternal(ctx context.Context, submission ExternalSubmission) (*store.RunState, error) {
	return s.submitExternal(ctx, submission, false)
}

func (s *Service) FailExternal(ctx context.Context, submission ExternalSubmission) (*store.RunState, error) {
	if submission.ExitCode == 0 {
		submission.ExitCode = 1
	}
	if strings.TrimSpace(submission.Error) == "" {
		submission.Error = "external executor reported failure"
	}
	if submission.ErrorCode == "" {
		submission.ErrorCode = "exit"
	}
	return s.submitExternal(ctx, submission, true)
}

// ExpireIdleExternal fails claimed external nodes whose worker has not
// produced any normalized activity within the configured idle timeout. The
// durable state is rechecked under the Run lock before any transition.
func (s *Service) ExpireIdleExternal(ctx context.Context, now time.Time) ([]string, error) {
	st := store.FS{Workspace: s.Workspace}
	runIDs, err := st.ListRunIDs()
	if err != nil {
		return nil, err
	}
	var expired []string
	var failures []string
	for _, runID := range runIDs {
		state, loadErr := st.Load(runID)
		if loadErr != nil {
			continue
		}
		for nodeID, node := range state.Nodes {
			if node == nil || node.External == nil || node.External.Status != "claimed" || strings.TrimSpace(node.External.IdleTimeout) == "" || externalAwaitingApproval(node.External) {
				continue
			}
			duration, parseErr := time.ParseDuration(node.External.IdleTimeout)
			if parseErr != nil || duration <= 0 {
				continue
			}
			last := node.External.LastActivityAt
			if last.IsZero() {
				last = node.External.ClaimedAt
			}
			// Legacy claimed states did not persist claimed_at. Their original lease
			// duration cannot be recovered safely, so leave them for normal lease
			// expiry/reclaim instead of inventing a 15-minute start time.
			if last.IsZero() {
				continue
			}
			if now.Sub(last) < duration {
				continue
			}
			if _, timeoutErr := s.timeoutExternalIdle(ctx, runID, nodeID, now); timeoutErr != nil {
				failures = append(failures, fmt.Sprintf("%s/%s: %v", runID, nodeID, timeoutErr))
				continue
			}
			expired = append(expired, runID+"/"+nodeID)
		}
	}
	if len(failures) > 0 {
		return expired, fmt.Errorf("expire idle external nodes: %s", strings.Join(failures, "; "))
	}
	return expired, nil
}

func externalAwaitingApproval(external *store.ExternalExecutionState) bool {
	if external == nil {
		return false
	}
	for _, call := range external.ToolCalls {
		if call != nil && call.Status == "waiting_approval" {
			return true
		}
	}
	return false
}

func (s *Service) timeoutExternalIdle(ctx context.Context, runID, nodeID string, now time.Time) (*store.RunState, error) {
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, runID)
	if err != nil {
		return nil, err
	}
	state, node, external, err := loadExternalNode(st, runID, nodeID)
	if err != nil {
		_ = release()
		return nil, err
	}
	if external.Status != "claimed" || strings.TrimSpace(external.IdleTimeout) == "" || externalAwaitingApproval(external) {
		_ = release()
		return state.PublicView(), nil
	}
	duration, err := time.ParseDuration(external.IdleTimeout)
	if err != nil || duration <= 0 || now.Sub(external.LastActivityAt) < duration {
		_ = release()
		return state.PublicView(), nil
	}
	reason := fmt.Sprintf("external node idle for %s", external.IdleTimeout)
	for _, call := range external.ToolCalls {
		if call == nil {
			continue
		}
		switch call.Status {
		case "completed", "failed", "denied", "cancelled":
			continue
		}
		call.Status = "cancelled"
		call.Decision = "cancel"
		call.Reason = reason
		call.CancelRequested = true
		call.CompletedAt = now
		if _, eventErr := appendExternalAssistantEvent(st, state, nodeID, external, assistant.Event{
			Time: now, Type: assistant.EventToolCompleted, Tool: call.Tool, CallID: call.CallID,
			Reason: reason, SessionID: external.SessionID, Data: map[string]any{"status": "cancelled", "idle_timeout": true},
		}); eventErr != nil {
			_ = release()
			return nil, eventErr
		}
	}
	if _, err := appendExternalAssistantEvent(st, state, nodeID, external, assistant.Event{
		Time: now, Type: assistant.EventDiagnostic, Message: reason, SessionID: external.SessionID,
		Data: map[string]any{"code": "idle_timeout", "idle_timeout": external.IdleTimeout},
	}); err != nil {
		_ = release()
		return nil, err
	}
	if _, err := appendExternalAssistantEvent(st, state, nodeID, external, assistant.Event{
		Time: now, Type: assistant.EventFailed, Message: reason, SessionID: external.SessionID,
	}); err != nil {
		_ = release()
		return nil, err
	}
	external.Status = "failed"
	external.Result = &store.ExternalResultState{ExitCode: 124, ErrorCode: string(execution.KindTimedOut), Error: reason, SessionID: external.SessionID}
	external.ClaimToken = ""
	external.LeaseExpiresAt = time.Time{}
	node.Status = store.NodePending
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := st.Commit(state, store.Event{Time: now, Type: "external_node.idle_timeout", NodeID: nodeID, Data: map[string]any{
		"idle_timeout": external.IdleTimeout, "last_activity_at": external.LastActivityAt,
	}}); err != nil {
		_ = release()
		return nil, err
	}
	runner, err := runnerForState(state)
	if err != nil {
		_ = release()
		return nil, err
	}
	state, runErr := runner.Resume(ctx, state)
	_ = release()
	var failedRun *runtime.RunFailedError
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) && !errors.As(runErr, &failedRun) {
		return nil, runErr
	}
	root, cascadeErr := resumeParentChain(ctx, st, state)
	failedRun = nil
	if cascadeErr != nil && !errors.Is(cascadeErr, runtime.ErrWaiting) && !errors.As(cascadeErr, &failedRun) {
		return nil, cascadeErr
	}
	return root.PublicView(), nil
}

func (s *Service) submitExternal(ctx context.Context, submission ExternalSubmission, failed bool) (*store.RunState, error) {
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, submission.RunID)
	if err != nil {
		return nil, err
	}
	state, node, external, err := loadExternalNode(st, submission.RunID, submission.NodeID)
	if err != nil {
		_ = release()
		return nil, err
	}
	if err := verifyExternalClaim(external, submission.ClaimToken); err != nil {
		_ = release()
		return nil, err
	}
	if err := ensureExternalToolsTerminal(external.ToolCalls); err != nil {
		_ = release()
		return nil, err
	}
	if len(submission.Structured) > 0 && strings.TrimSpace(submission.Output) == "" {
		submission.Output = string(submission.Structured)
	}
	status := "completed"
	if failed {
		status = "failed"
	}
	external.Status = status
	external.Result = &store.ExternalResultState{
		Output: submission.Output, Structured: append(json.RawMessage(nil), submission.Structured...),
		Stdout: submission.Stdout, Stderr: submission.Stderr, ExitCode: submission.ExitCode,
		SessionID: submission.SessionID, Resumed: submission.Resumed, AssistantVersion: submission.AssistantVersion,
		ResolvedModel: submission.ResolvedModel, Usage: submission.Usage,
		ErrorCode: submission.ErrorCode, Error: submission.Error,
	}
	finalEvents := make([]assistant.Event, 0, 4)
	sessionEvent := assistant.EventSessionStarted
	if submission.Resumed {
		sessionEvent = assistant.EventSessionResumed
	}
	finalEvents = append(finalEvents, assistant.Event{Type: sessionEvent, SessionID: submission.SessionID})
	if strings.TrimSpace(submission.Output) != "" {
		message := submission.Output
		if len(message) > 64*1024 {
			message = message[:64*1024]
		}
		finalEvents = append(finalEvents, assistant.Event{Type: assistant.EventMessage, Message: message, SessionID: submission.SessionID, Data: map[string]any{"final": true}})
	}
	if submission.Usage != nil {
		finalEvents = append(finalEvents, assistant.Event{Type: assistant.EventUsage, Usage: &assistant.ProtocolUsage{InputTokens: submission.Usage.InputTokens, OutputTokens: submission.Usage.OutputTokens, Cost: submission.Usage.Cost}, SessionID: submission.SessionID})
	}
	terminalEvent := assistant.Event{Type: assistant.EventCompleted, SessionID: submission.SessionID}
	if failed {
		terminalEvent.Type = assistant.EventFailed
		terminalEvent.Message = submission.Error
	}
	finalEvents = append(finalEvents, terminalEvent)
	for _, event := range finalEvents {
		if _, err := appendExternalAssistantEvent(st, state, submission.NodeID, external, event); err != nil {
			_ = release()
			return nil, err
		}
	}
	external.ClaimToken = ""
	external.LeaseExpiresAt = time.Time{}
	node.Status = store.NodePending
	state.Status = store.RunRunning
	state.Waiting = nil
	if err := st.Commit(state, store.Event{Type: "external_node.result.submitted", NodeID: submission.NodeID, Data: map[string]any{
		"status": status, "exit_code": submission.ExitCode, "worker_id": external.ClaimedBy,
	}}); err != nil {
		_ = release()
		return nil, err
	}
	runner, err := runnerForState(state)
	if err != nil {
		_ = release()
		return nil, err
	}
	state, runErr := runner.Resume(ctx, state)
	_ = release()
	if runErr != nil && !errors.Is(runErr, runtime.ErrWaiting) {
		return nil, runErr
	}
	root, cascadeErr := resumeParentChain(ctx, st, state)
	if cascadeErr != nil && !errors.Is(cascadeErr, runtime.ErrWaiting) {
		return nil, cascadeErr
	}
	return root.PublicView(), nil
}

func loadExternalNode(st store.FS, runID, nodeID string) (*store.RunState, *store.NodeState, *store.ExternalExecutionState, error) {
	state, err := st.Load(runID)
	if err != nil {
		return nil, nil, nil, err
	}
	node := state.Nodes[nodeID]
	if node == nil || node.External == nil {
		return nil, nil, nil, fmt.Errorf("run %s has no external node %q", runID, nodeID)
	}
	return state, node, node.External, nil
}

func verifyExternalClaim(external *store.ExternalExecutionState, token string) error {
	if external.Status != "claimed" {
		return fmt.Errorf("external node is not claimed")
	}
	if strings.TrimSpace(token) == "" || token != external.ClaimToken {
		return fmt.Errorf("invalid external claim token")
	}
	if !external.LeaseExpiresAt.IsZero() && !external.LeaseExpiresAt.After(time.Now().UTC()) {
		return fmt.Errorf("external claim lease expired")
	}
	return nil
}

type ExternalToolRequest struct {
	RunID      string
	NodeID     string
	ClaimToken string
	CallID     string
	Tool       string
	Input      json.RawMessage
	Message    string
	Wait       time.Duration
}

type ExternalToolDecisionRequest struct {
	RunID    string
	NodeID   string
	CallID   string
	Decision string
	Reason   string
}

type ExternalToolUpdate struct {
	RunID      string
	NodeID     string
	ClaimToken string
	CallID     string
	Output     json.RawMessage
	Failed     bool
	Reason     string
}

type ExternalArtifactRequest struct {
	RunID      string
	NodeID     string
	ClaimToken string
	CallID     string
	Type       string
	MIME       string
	Path       string
}

func ensureExternalToolsTerminal(values map[string]*store.ToolCallState) error {
	for callID, call := range values {
		if call == nil {
			continue
		}
		switch call.Status {
		case "completed", "failed", "denied", "cancelled":
			continue
		default:
			return fmt.Errorf("tool call %q is not terminal (status %s)", callID, call.Status)
		}
	}
	return nil
}

func cloneToolCalls(values map[string]*store.ToolCallState) map[string]*store.ToolCallState {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]*store.ToolCallState, len(values))
	for id, value := range values {
		if value == nil {
			continue
		}
		clone := *value
		clone.Input = append(json.RawMessage(nil), value.Input...)
		clone.Output = append(json.RawMessage(nil), value.Output...)
		out[id] = &clone
	}
	return out
}

func (s *Service) RequestExternalTool(ctx context.Context, request ExternalToolRequest) (*store.ToolCallState, error) {
	if strings.TrimSpace(request.CallID) == "" || strings.TrimSpace(request.Tool) == "" {
		return nil, fmt.Errorf("call_id and tool are required")
	}
	if request.Wait < 0 || request.Wait > 30*time.Second {
		return nil, fmt.Errorf("wait must be between 0 and 30s")
	}
	st := store.FS{Workspace: s.Workspace}
	state, err := s.createOrReadToolRequest(st, request)
	if err != nil {
		return nil, err
	}
	if toolDecisionTerminal(state.Status) || request.Wait == 0 {
		return state, nil
	}
	deadline := time.NewTimer(request.Wait)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return state, nil
		case <-ticker.C:
			_, _, external, loadErr := loadExternalNode(st, request.RunID, request.NodeID)
			if loadErr != nil {
				return nil, loadErr
			}
			current := external.ToolCalls[request.CallID]
			if current == nil {
				return nil, fmt.Errorf("tool call %q disappeared", request.CallID)
			}
			state = cloneToolCall(current)
			if toolDecisionTerminal(state.Status) {
				return state, nil
			}
		}
	}
}

func (s *Service) createOrReadToolRequest(st store.FS, request ExternalToolRequest) (*store.ToolCallState, error) {
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, _, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	if err := verifyExternalClaim(external, request.ClaimToken); err != nil {
		return nil, err
	}
	if external.ToolCalls == nil {
		external.ToolCalls = map[string]*store.ToolCallState{}
	}
	if existing := external.ToolCalls[request.CallID]; existing != nil {
		if existing.Tool != request.Tool || string(existing.Input) != string(request.Input) {
			return nil, fmt.Errorf("tool call %q was already requested with different content", request.CallID)
		}
		return cloneToolCall(existing), nil
	}
	now := time.Now().UTC()
	call := &store.ToolCallState{
		CallID: request.CallID, Tool: request.Tool, Input: append(json.RawMessage(nil), request.Input...),
		Status: "requested", RequestedAt: now,
	}
	external.ToolCalls[request.CallID] = call
	if _, err := appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
		Type: assistant.EventToolRequested, Tool: request.Tool, CallID: request.CallID, Input: request.Input,
		Message: request.Message, SessionID: external.SessionID,
	}); err != nil {
		return nil, err
	}
	if allowed, reason := externalToolPolicy(external.Policy, request.Tool); !allowed {
		call.Status, call.Decision, call.Reason, call.DecidedAt = "denied", "deny", reason, time.Now().UTC()
		if _, err := appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
			Type: assistant.EventToolDenied, Tool: request.Tool, CallID: request.CallID,
			Decision: "deny", Reason: reason, SessionID: external.SessionID,
		}); err != nil {
			return nil, err
		}
		return cloneToolCall(call), nil
	}
	call.ApprovalNeeded = toolApprovalRequired(external.ToolApproval, request.Tool)
	if call.ApprovalNeeded {
		call.Status = "waiting_approval"
		if err := st.Commit(state, store.Event{Type: "tool.approval.requested", NodeID: request.NodeID, Data: map[string]any{
			"call_id": request.CallID, "tool": request.Tool, "message": toolApprovalMessage(external.ToolApproval, request.Tool),
		}}); err != nil {
			return nil, err
		}
		return cloneToolCall(call), nil
	}
	call.Status, call.Decision, call.DecidedAt = "allowed", "allow", time.Now().UTC()
	if _, err := appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
		Type: assistant.EventToolAllowed, Tool: request.Tool, CallID: request.CallID,
		Decision: "allow", Reason: "allowed by node policy", SessionID: external.SessionID,
	}); err != nil {
		return nil, err
	}
	return cloneToolCall(call), nil
}

func (s *Service) DecideExternalTool(request ExternalToolDecisionRequest) (*store.ToolCallState, error) {
	if request.Decision != "allow" && request.Decision != "deny" {
		return nil, fmt.Errorf("decision must be allow or deny")
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, _, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	call := external.ToolCalls[request.CallID]
	if call == nil {
		return nil, fmt.Errorf("unknown tool call %q", request.CallID)
	}
	if call.Status != "waiting_approval" {
		return nil, fmt.Errorf("tool call %q is not waiting for approval (status %s)", request.CallID, call.Status)
	}
	call.Decision, call.Reason, call.DecidedAt = request.Decision, request.Reason, time.Now().UTC()
	if request.Decision == "allow" {
		call.Status = "allowed"
		_, err = appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
			Type: assistant.EventToolAllowed, Tool: call.Tool, CallID: call.CallID,
			Decision: "allow", Reason: request.Reason, SessionID: external.SessionID,
		})
	} else {
		call.Status = "denied"
		_, err = appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
			Type: assistant.EventToolDenied, Tool: call.Tool, CallID: call.CallID,
			Decision: "deny", Reason: request.Reason, SessionID: external.SessionID,
		})
	}
	if err != nil {
		return nil, err
	}
	return cloneToolCall(call), nil
}

func (s *Service) StartExternalTool(request ExternalToolUpdate) (*store.ToolCallState, error) {
	return s.updateExternalTool(request, "start")
}

func (s *Service) CompleteExternalTool(request ExternalToolUpdate) (*store.ToolCallState, error) {
	return s.updateExternalTool(request, "complete")
}

func (s *Service) updateExternalTool(request ExternalToolUpdate, action string) (*store.ToolCallState, error) {
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, _, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	if err := verifyExternalClaim(external, request.ClaimToken); err != nil {
		return nil, err
	}
	call := external.ToolCalls[request.CallID]
	if call == nil {
		return nil, fmt.Errorf("unknown tool call %q", request.CallID)
	}
	now := time.Now().UTC()
	switch action {
	case "start":
		if call.CancelRequested {
			return nil, fmt.Errorf("tool call %q was cancelled", request.CallID)
		}
		if call.Status != "allowed" {
			return nil, fmt.Errorf("tool call %q cannot start in status %s", request.CallID, call.Status)
		}
		call.Status, call.StartedAt = "running", now
		_, err = appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
			Type: assistant.EventToolStarted, Tool: call.Tool, CallID: call.CallID, Input: call.Input, SessionID: external.SessionID,
		})
	case "complete":
		if call.Status != "running" && call.Status != "cancel_requested" {
			return nil, fmt.Errorf("tool call %q cannot complete in status %s", request.CallID, call.Status)
		}
		call.Output = append(json.RawMessage(nil), request.Output...)
		call.CompletedAt = now
		if call.CancelRequested {
			call.Status, call.Decision = "cancelled", "cancel"
		} else if request.Failed {
			call.Status, call.Reason = "failed", request.Reason
		} else {
			call.Status = "completed"
		}
		_, err = appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
			Type: assistant.EventToolCompleted, Tool: call.Tool, CallID: call.CallID, Output: call.Output,
			Reason: call.Reason, SessionID: external.SessionID, Data: map[string]any{"status": call.Status},
		})
	}
	if err != nil {
		return nil, err
	}
	return cloneToolCall(call), nil
}

func (s *Service) CancelExternalTool(runID, nodeID, callID, reason string) (*store.ToolCallState, error) {
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, runID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, _, external, err := loadExternalNode(st, runID, nodeID)
	if err != nil {
		return nil, err
	}
	call := external.ToolCalls[callID]
	if call == nil {
		return nil, fmt.Errorf("unknown tool call %q", callID)
	}
	if reason == "" {
		reason = "tool call cancelled by controller"
	}
	switch call.Status {
	case "requested", "waiting_approval", "allowed":
		call.Status, call.Decision, call.Reason, call.CancelRequested = "cancelled", "cancel", reason, true
		call.DecidedAt = time.Now().UTC()
		_, err = appendExternalAssistantEvent(st, state, nodeID, external, assistant.Event{
			Type: assistant.EventToolDenied, Tool: call.Tool, CallID: call.CallID,
			Decision: "cancel", Reason: reason, SessionID: external.SessionID,
		})
	case "running", "cancel_requested":
		call.Status, call.CancelRequested, call.Reason = "cancel_requested", true, reason
		_, err = appendExternalAssistantEvent(st, state, nodeID, external, assistant.Event{
			Type: assistant.EventDiagnostic, Message: reason, CallID: call.CallID,
			SessionID: external.SessionID, Data: map[string]any{"tool": call.Tool, "tool_cancel_requested": true},
		})
	default:
		return nil, fmt.Errorf("tool call %q is terminal with status %s", callID, call.Status)
	}
	if err != nil {
		return nil, err
	}
	return cloneToolCall(call), nil
}

func (s *Service) GetExternalTool(runID, nodeID, callID string) (*store.ToolCallState, error) {
	st := store.FS{Workspace: s.Workspace}
	_, _, external, err := loadExternalNode(st, runID, nodeID)
	if err != nil {
		return nil, err
	}
	call := external.ToolCalls[callID]
	if call == nil {
		return nil, fmt.Errorf("unknown tool call %q", callID)
	}
	return cloneToolCall(call), nil
}

func (s *Service) DeclareExternalArtifact(request ExternalArtifactRequest) (*store.ArtifactRef, error) {
	if strings.TrimSpace(request.Type) == "" || strings.TrimSpace(request.Path) == "" || strings.TrimSpace(request.CallID) == "" {
		return nil, fmt.Errorf("call_id, type and path are required")
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := acquireRunLock(st, request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, node, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	if err := verifyExternalClaim(external, request.ClaimToken); err != nil {
		return nil, err
	}
	call := external.ToolCalls[request.CallID]
	if call == nil {
		return nil, fmt.Errorf("artifact references unknown tool call %q", request.CallID)
	}
	if call.Status != "running" && call.Status != "completed" {
		return nil, fmt.Errorf("artifact tool call %q must be running or completed", request.CallID)
	}
	source, err := secureExternalArtifactPath(request.Path, external.Workspace, st.ArtifactsDir(state.ID))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("artifact path is not a regular file")
	}
	destinationDir := filepath.Join(st.ArtifactsDir(state.ID), "nodes", safeControlPart(request.NodeID), fmt.Sprintf("%d", external.Attempt), "external")
	if err := os.MkdirAll(destinationDir, 0o755); err != nil {
		return nil, err
	}
	destination := filepath.Join(destinationDir, safeControlPart(request.CallID)+"-"+filepath.Base(source))
	if err := copyControlFile(source, destination); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	mime := request.MIME
	if mime == "" {
		mime = "application/octet-stream"
	}
	artifact := store.ArtifactRef{
		ID:   fmt.Sprintf("%s:%s:%d:%s", request.NodeID, request.Type, external.Attempt, request.CallID),
		Type: request.Type, MIME: mime, Path: destination, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data)),
		ProducerRunID: state.ID, ProducerNodeID: request.NodeID, Attempt: external.Attempt, CreatedAt: time.Now().UTC(), CallID: request.CallID,
	}
	node.Artifacts = appendControlArtifact(node.Artifacts, artifact)
	state.Artifacts = appendControlArtifact(state.Artifacts, artifact)
	_, err = appendExternalAssistantEvent(st, state, request.NodeID, external, assistant.Event{
		Type: assistant.EventArtifactDeclared, CallID: request.CallID, SessionID: external.SessionID,
		Artifact: &assistant.ArtifactDeclaration{ID: artifact.ID, Type: artifact.Type, MIME: artifact.MIME, Path: artifact.Path, SHA256: artifact.SHA256, Size: artifact.Size, CallID: request.CallID},
	})
	if err != nil {
		return nil, err
	}
	return &artifact, nil
}

func appendExternalAssistantEvent(st store.FS, state *store.RunState, nodeID string, external *store.ExternalExecutionState, event assistant.Event) (uint64, error) {
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if err := assistant.ValidateEvent(event); err != nil {
		return 0, err
	}
	external.LastActivityAt = event.Time
	external.LastEventSequence++
	data := assistant.EventData(event)
	data["sequence"] = external.LastEventSequence
	data["source"] = "external"
	if err := st.Commit(state, store.Event{Time: event.Time, Type: "assistant." + event.Type, NodeID: nodeID, Data: data}); err != nil {
		return 0, err
	}
	return external.LastEventSequence, nil
}

func externalToolPolicy(policy *store.NodePolicyState, tool string) (bool, string) {
	if policy == nil {
		return true, ""
	}
	for _, denied := range policy.DeniedTools {
		if denied == tool {
			return false, "tool is denied by node policy"
		}
	}
	if policy.ToolsRestricted {
		allowed := false
		for _, candidate := range policy.AllowedTools {
			if candidate == tool {
				allowed = true
				break
			}
		}
		if !allowed {
			return false, "tool is outside allowed_tools"
		}
	}
	if policy.Filesystem == "read_only" {
		switch tool {
		case "write", "edit", "patch", "bash", "shell", "task":
			return false, "tool conflicts with read_only filesystem policy"
		}
	}
	if policy.Network == "deny" {
		switch tool {
		case "web", "http", "fetch", "network", "browser":
			return false, "tool conflicts with network deny policy"
		}
	}
	return true, ""
}

func toolApprovalRequired(policy *store.ToolApprovalState, tool string) bool {
	if policy == nil || policy.Mode != "required" {
		return false
	}
	if len(policy.Tools) == 0 {
		return true
	}
	for _, candidate := range policy.Tools {
		if candidate == tool {
			return true
		}
	}
	return false
}

func toolApprovalMessage(policy *store.ToolApprovalState, tool string) string {
	if policy != nil && strings.TrimSpace(policy.Message) != "" {
		return strings.ReplaceAll(policy.Message, "${tool}", tool)
	}
	return fmt.Sprintf("Allow tool %s?", tool)
}

func toolDecisionTerminal(status string) bool {
	switch status {
	case "allowed", "denied", "cancelled", "running", "completed", "failed", "cancel_requested":
		return true
	default:
		return false
	}
}

func cloneToolCall(value *store.ToolCallState) *store.ToolCallState {
	if value == nil {
		return nil
	}
	clone := *value
	clone.Input = append(json.RawMessage(nil), value.Input...)
	clone.Output = append(json.RawMessage(nil), value.Output...)
	return &clone
}

func secureExternalArtifactPath(value, workspace, artifactRoot string) (string, error) {
	candidate := value
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(workspace, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	resolved := filepath.Clean(abs)
	if evaluated, evalErr := filepath.EvalSymlinks(resolved); evalErr == nil {
		resolved = evaluated
	}
	for _, root := range []string{workspace, artifactRoot} {
		rootAbs, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		if evaluated, evalErr := filepath.EvalSymlinks(rootAbs); evalErr == nil {
			rootAbs = evaluated
		}
		rel, relErr := filepath.Rel(rootAbs, resolved)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("artifact path %q is outside execution workspace and Run artifacts", value)
}

func copyControlFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = output.Close()
		if !ok {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func safeControlPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "artifact"
	}
	return b.String()
}

func appendControlArtifact(values []store.ArtifactRef, artifact store.ArtifactRef) []store.ArtifactRef {
	for i := range values {
		if values[i].ID == artifact.ID && values[i].ProducerRunID == artifact.ProducerRunID {
			values[i] = artifact
			return values
		}
	}
	return append(values, artifact)
}
