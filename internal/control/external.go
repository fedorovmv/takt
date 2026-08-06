package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"takt/internal/assistant"
	"takt/internal/runtime"
	"takt/internal/store"
)

type ExternalTask struct {
	RunID          string                 `json:"run_id"`
	NodeID         string                 `json:"node_id"`
	Status         string                 `json:"status"`
	Attempt        int                    `json:"attempt"`
	Prompt         string                 `json:"prompt"`
	Workspace      string                 `json:"workspace"`
	Assistant      string                 `json:"assistant,omitempty"`
	RequestedModel *store.ModelRef        `json:"requested_model,omitempty"`
	SessionMode    string                 `json:"session_mode,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"`
	Policy         *store.NodePolicyState `json:"policy,omitempty"`
	OutputFormat   json.RawMessage        `json:"output_format,omitempty"`
	ClaimedBy      string                 `json:"claimed_by,omitempty"`
	LeaseExpiresAt time.Time              `json:"lease_expires_at,omitempty"`
	ClaimToken     string                 `json:"claim_token,omitempty"`
}

type ExternalClaimRequest struct {
	RunID        string
	NodeID       string
	WorkerID     string
	Capabilities []string
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
			if node == nil || node.External == nil {
				continue
			}
			external := node.External
			if external.Status != "pending" && !(external.Status == "claimed" && !external.LeaseExpiresAt.After(now)) {
				continue
			}
			tasks = append(tasks, externalTask(state.ID, nodeID, external, ""))
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
	release, err := st.AcquireLock(request.RunID)
	if err != nil {
		return nil, err
	}
	defer release()
	state, node, external, err := loadExternalNode(st, request.RunID, request.NodeID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if external.Status == "claimed" && external.LeaseExpiresAt.After(now) {
		return nil, fmt.Errorf("external node %s/%s is already claimed by %s until %s", state.ID, request.NodeID, external.ClaimedBy, external.LeaseExpiresAt.Format(time.RFC3339))
	}
	if external.Status != "pending" && external.Status != "claimed" {
		return nil, fmt.Errorf("external node %s/%s cannot be claimed in status %s", state.ID, request.NodeID, external.Status)
	}
	if err := requireExternalCapabilities(external.Policy, request.Capabilities); err != nil {
		return nil, err
	}
	token, err := newClaimToken()
	if err != nil {
		return nil, err
	}
	external.Status = "claimed"
	external.ClaimedBy = request.WorkerID
	external.ClaimToken = token
	external.LeaseExpiresAt = now.Add(request.Lease)
	node.Status = store.NodeWaiting
	if err := st.Commit(state, store.Event{Type: "external_node.claimed", NodeID: request.NodeID, Data: map[string]any{
		"worker_id": request.WorkerID, "lease_expires_at": external.LeaseExpiresAt, "capabilities": request.Capabilities,
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
	return ExternalTask{
		RunID: runID, NodeID: nodeID, Status: external.Status, Attempt: external.Attempt,
		Prompt: external.Prompt, Workspace: external.Workspace, Assistant: external.Assistant,
		RequestedModel: external.RequestedModel, SessionMode: external.SessionMode, SessionID: external.SessionID,
		Policy: external.Policy, OutputFormat: external.OutputFormat, ClaimedBy: external.ClaimedBy,
		LeaseExpiresAt: external.LeaseExpiresAt, ClaimToken: token,
	}
}

func (s *Service) AppendExternalEvent(runID, nodeID, claimToken string, event assistant.Event) (uint64, error) {
	if err := assistant.ValidateEvent(event); err != nil {
		return 0, err
	}
	st := store.FS{Workspace: s.Workspace}
	release, err := st.AcquireLock(runID)
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
	external.LastEventSequence++
	data := assistant.EventData(event)
	data["sequence"] = external.LastEventSequence
	data["source"] = "external"
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if err := st.Commit(state, store.Event{Time: event.Time, Type: "assistant." + event.Type, NodeID: nodeID, Data: data}); err != nil {
		return 0, err
	}
	return external.LastEventSequence, nil
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

func (s *Service) submitExternal(ctx context.Context, submission ExternalSubmission, failed bool) (*store.RunState, error) {
	st := store.FS{Workspace: s.Workspace}
	release, err := st.AcquireLock(submission.RunID)
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
	finalEvents := make([]assistant.Event, 0, 3)
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
		external.LastEventSequence++
		data := assistant.EventData(event)
		data["sequence"] = external.LastEventSequence
		data["source"] = "external"
		if err := st.Commit(state, store.Event{Type: "assistant." + event.Type, NodeID: submission.NodeID, Data: data}); err != nil {
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
