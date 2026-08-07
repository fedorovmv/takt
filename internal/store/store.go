package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	NodePending   = "pending"
	NodeRunning   = "running"
	NodeCompleted = "completed"
	NodeFailed    = "failed"
	NodeErrored   = "errored"
	NodeCancelled = "cancelled"
	NodeTimedOut  = "timed_out"
	NodeSkipped   = "skipped"
	NodeBlocked   = "blocked"
	NodeWaiting   = "waiting"
)

const (
	RunRunning   = "running"
	RunPausing   = "pausing"
	RunPaused    = "paused"
	RunWaiting   = "waiting"
	RunCompleted = "completed"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
	RunAbandoned = "abandoned"
)

type ModelRef struct {
	Name     string         `json:"name,omitempty"`
	Provider string         `json:"provider,omitempty"`
	ID       string         `json:"id,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
}

type NodePolicyState struct {
	AllowedTools     []string `json:"allowed_tools,omitempty"`
	DeniedTools      []string `json:"denied_tools,omitempty"`
	ToolsRestricted  bool     `json:"tools_restricted,omitempty"`
	Skills           []string `json:"skills,omitempty"`
	SkillsRestricted bool     `json:"skills_restricted,omitempty"`
	MCPPath          string   `json:"mcp_path,omitempty"`
	Filesystem       string   `json:"filesystem,omitempty"`
	Network          string   `json:"network,omitempty"`
	Requires         []string `json:"requires,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
}

type Usage struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	Cost         float64 `json:"cost,omitempty"`
}

// ExecutionState records one actual node action invocation. NodeState keeps
// aggregate fields for compatibility and quick inspection, while Executions
// preserves the per-attempt identity needed to attribute usage when retries
// resolve to different assistants or models.
type ExecutionState struct {
	Attempt          int       `json:"attempt"`
	Status           string    `json:"status"`
	Assistant        string    `json:"assistant,omitempty"`
	AssistantVersion string    `json:"assistant_version,omitempty"`
	RequestedModel   *ModelRef `json:"requested_model,omitempty"`
	ResolvedModel    *ModelRef `json:"resolved_model,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	Resumed          bool      `json:"resumed,omitempty"`
	ExitCode         int       `json:"exit_code,omitempty"`
	ErrorCode        string    `json:"error_code,omitempty"`
	Error            string    `json:"error,omitempty"`
	OutputTruncated  bool      `json:"output_truncated,omitempty"`
	Usage            *Usage    `json:"usage,omitempty"`
}

type ToolApprovalState struct {
	Mode    string   `json:"mode"`
	Tools   []string `json:"tools,omitempty"`
	Message string   `json:"message,omitempty"`
}

type ToolCallState struct {
	CallID          string          `json:"call_id"`
	Tool            string          `json:"tool"`
	Input           json.RawMessage `json:"input,omitempty"`
	Output          json.RawMessage `json:"output,omitempty"`
	Status          string          `json:"status"`
	Decision        string          `json:"decision,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	ApprovalNeeded  bool            `json:"approval_needed,omitempty"`
	CancelRequested bool            `json:"cancel_requested,omitempty"`
	RequestedAt     time.Time       `json:"requested_at,omitempty"`
	DecidedAt       time.Time       `json:"decided_at,omitempty"`
	StartedAt       time.Time       `json:"started_at,omitempty"`
	CompletedAt     time.Time       `json:"completed_at,omitempty"`
}

// DomainOperationState records provider-neutral SCM/tracker/CI execution and
// the reconciliation facts needed to prevent blind retries of side effects.
type DomainOperationState struct {
	Adapter            string   `json:"adapter"`
	Domain             string   `json:"domain"`
	Operation          string   `json:"operation"`
	Capabilities       []string `json:"capabilities,omitempty"`
	ReconcileSupported bool     `json:"reconcile_supported,omitempty"`
	SideEffectMode     string   `json:"side_effect_mode,omitempty"`
	IdempotencyKey     string   `json:"idempotency_key,omitempty"`
	Receipt            string   `json:"receipt,omitempty"`
	ReconcileStatus    string   `json:"reconcile_status,omitempty"`
}

// ExternalExecutionState is a durable hand-off of one command/prompt node to a
// local MCP client. The claim token is intentionally omitted from PublicView.
type ExternalExecutionState struct {
	Status                string                    `json:"status"`
	Attempt               int                       `json:"attempt"`
	Prompt                string                    `json:"prompt"`
	Workspace             string                    `json:"workspace"`
	Assistant             string                    `json:"assistant,omitempty"`
	RequestedModel        *ModelRef                 `json:"requested_model,omitempty"`
	SessionMode           string                    `json:"session_mode,omitempty"`
	SessionID             string                    `json:"session_id,omitempty"`
	Policy                *NodePolicyState          `json:"policy,omitempty"`
	CapabilityDeclaration json.RawMessage           `json:"capability_declaration,omitempty"`
	ToolApproval          *ToolApprovalState        `json:"tool_approval,omitempty"`
	ToolCalls             map[string]*ToolCallState `json:"tool_calls,omitempty"`
	OutputFormat          json.RawMessage           `json:"output_format,omitempty"`
	ClaimedBy             string                    `json:"claimed_by,omitempty"`
	ClaimedAt             time.Time                 `json:"claimed_at,omitempty"`
	ClaimToken            string                    `json:"claim_token,omitempty"`
	LeaseExpiresAt        time.Time                 `json:"lease_expires_at,omitempty"`
	IdleTimeout           string                    `json:"idle_timeout,omitempty"`
	LastActivityAt        time.Time                 `json:"last_activity_at,omitempty"`
	LastEventSequence     uint64                    `json:"last_event_sequence,omitempty"`
	Result                *ExternalResultState      `json:"result,omitempty"`
	SideEffectMode        string                    `json:"side_effect_mode,omitempty"`
	IdempotencyKey        string                    `json:"idempotency_key,omitempty"`
	Receipt               string                    `json:"receipt,omitempty"`
	ReconcileStatus       string                    `json:"reconcile_status,omitempty"`
}

type ExternalResultState struct {
	Output           string          `json:"output,omitempty"`
	Structured       json.RawMessage `json:"structured,omitempty"`
	Stdout           string          `json:"stdout,omitempty"`
	Stderr           string          `json:"stderr,omitempty"`
	ExitCode         int             `json:"exit_code,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	Resumed          bool            `json:"resumed,omitempty"`
	AssistantVersion string          `json:"assistant_version,omitempty"`
	ResolvedModel    *ModelRef       `json:"resolved_model,omitempty"`
	Usage            *Usage          `json:"usage,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
	Error            string          `json:"error,omitempty"`
}

type ChildRunItemState struct {
	Attempt         int             `json:"attempt"`
	Index           int             `json:"index"`
	Item            json.RawMessage `json:"item"`
	ItemFingerprint string          `json:"item_fingerprint"`
	RunID           string          `json:"run_id"`
	Status          string          `json:"status"`
	Output          string          `json:"output,omitempty"`
	ErrorCode       string          `json:"error_code,omitempty"`
	Error           string          `json:"error,omitempty"`
	Usage           *Usage          `json:"usage,omitempty"`
	Artifacts       []ArtifactRef   `json:"artifacts,omitempty"`
}

// ArtifactRef is immutable metadata for a file copied into a Run artifact
// store. Path is absolute for direct local consumption; producer fields retain
// provenance when a child artifact is exposed by a parent node.
type ArtifactRef struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	MIME           string    `json:"mime"`
	Path           string    `json:"path"`
	SHA256         string    `json:"sha256"`
	Size           int64     `json:"size"`
	ProducerRunID  string    `json:"producer_run_id"`
	ProducerNodeID string    `json:"producer_node_id"`
	Attempt        int       `json:"attempt"`
	CreatedAt      time.Time `json:"created_at"`
	CallID         string    `json:"call_id,omitempty"`
}

type NodeState struct {
	Status                  string                  `json:"status"`
	Output                  string                  `json:"output,omitempty"`
	Stdout                  string                  `json:"stdout,omitempty"`
	Stderr                  string                  `json:"stderr,omitempty"`
	OutputTruncated         bool                    `json:"output_truncated,omitempty"`
	Usage                   *Usage                  `json:"usage,omitempty"`
	Assistant               string                  `json:"assistant,omitempty"`
	AssistantVersion        string                  `json:"assistant_version,omitempty"`
	RequestedModel          *ModelRef               `json:"requested_model,omitempty"`
	ResolvedModel           *ModelRef               `json:"resolved_model,omitempty"`
	ExitCode                int                     `json:"exit_code,omitempty"`
	Attempts                int                     `json:"attempts,omitempty"`
	Feedback                string                  `json:"feedback,omitempty"`
	SessionID               string                  `json:"session_id,omitempty"`
	Resumed                 bool                    `json:"resumed,omitempty"`
	ErrorCode               string                  `json:"error_code,omitempty"`
	Error                   string                  `json:"error,omitempty"`
	Executions              []ExecutionState        `json:"executions,omitempty"`
	LoopPrevious            map[string]NodeState    `json:"loop_previous,omitempty"`
	LoopIteration           int                     `json:"loop_iteration,omitempty"`
	Hidden                  bool                    `json:"internal,omitempty"`
	PublicParent            string                  `json:"public_parent,omitempty"`
	ChildRunID              string                  `json:"child_run_id,omitempty"`
	ChildRunIDs             []string                `json:"child_run_ids,omitempty"`
	ChildControlWorkspace   string                  `json:"child_control_workspace,omitempty"`
	ChildExecutionWorkspace string                  `json:"child_execution_workspace,omitempty"`
	ChildBranch             string                  `json:"child_branch,omitempty"`
	ChildBaseCommit         string                  `json:"child_base_commit,omitempty"`
	ChildRuns               []ChildRunItemState     `json:"child_runs,omitempty"`
	FanOutAttempt           int                     `json:"fan_out_attempt,omitempty"`
	FanOutFingerprint       string                  `json:"fan_out_fingerprint,omitempty"`
	Policy                  *NodePolicyState        `json:"policy,omitempty"`
	Artifacts               []ArtifactRef           `json:"artifacts,omitempty"`
	External                *ExternalExecutionState `json:"external,omitempty"`
	DomainOperation         *DomainOperationState   `json:"domain_operation,omitempty"`
}

func (n NodeState) Terminal() bool {
	switch n.Status {
	case NodeCompleted, NodeFailed, NodeErrored, NodeCancelled, NodeTimedOut, NodeSkipped, NodeBlocked:
		return true
	default:
		return false
	}
}

func (n NodeState) Successful() bool { return n.Status == NodeCompleted }
func (n NodeState) FailedLike() bool {
	switch n.Status {
	case NodeFailed, NodeErrored, NodeCancelled, NodeTimedOut, NodeBlocked:
		return true
	default:
		return false
	}
}

type WorktreeState struct {
	Enabled            bool      `json:"enabled"`
	RepositoryRoot     string    `json:"repository_root,omitempty"`
	ControlWorkspace   string    `json:"control_workspace,omitempty"`
	ExecutionWorkspace string    `json:"execution_workspace,omitempty"`
	Path               string    `json:"path,omitempty"`
	Branch             string    `json:"branch,omitempty"`
	BaseRef            string    `json:"base_ref,omitempty"`
	BaseCommit         string    `json:"base_commit,omitempty"`
	Cleanup            string    `json:"cleanup,omitempty"`
	BaseDirty          bool      `json:"base_dirty,omitempty"`
	Dirty              bool      `json:"dirty,omitempty"`
	Removed            bool      `json:"removed,omitempty"`
	BranchRemoved      bool      `json:"branch_removed,omitempty"`
	BranchCleanupError string    `json:"branch_cleanup_error,omitempty"`
	RetainedReason     string    `json:"retained_reason,omitempty"`
	CleanupError       string    `json:"cleanup_error,omitempty"`
	RemovedAt          time.Time `json:"removed_at,omitempty"`
}

type RunOptionsState struct {
	WorktreeMode string `json:"worktree_mode,omitempty"`
	WorktreeBase string `json:"worktree_base,omitempty"`
	KeepWorktree bool   `json:"keep_worktree,omitempty"`
	AllowDirty   bool   `json:"allow_dirty_worktree,omitempty"`
}

type RunState struct {
	ID                    string                `json:"id"`
	Status                string                `json:"status"`
	ParentRunID           string                `json:"parent_run_id,omitempty"`
	ForkedFromRunID       string                `json:"forked_from_run_id,omitempty"`
	ForkSourceFingerprint string                `json:"fork_source_fingerprint,omitempty"`
	ParentNodeID          string                `json:"parent_node_id,omitempty"`
	ChildRunIDs           []string              `json:"child_run_ids,omitempty"`
	WorkflowPath          string                `json:"workflow_path"`
	ConfigPath            string                `json:"config_path"`
	Workspace             string                `json:"workspace"`
	ExecutionWorkspace    string                `json:"execution_workspace,omitempty"`
	Worktree              *WorktreeState        `json:"worktree,omitempty"`
	RunOptions            RunOptionsState       `json:"run_options,omitempty"`
	InheritedPolicy       *NodePolicyState      `json:"inherited_policy,omitempty"`
	Input                 string                `json:"input"`
	Output                string                `json:"output,omitempty"`
	Usage                 *Usage                `json:"usage,omitempty"`
	Artifacts             []ArtifactRef         `json:"artifacts,omitempty"`
	CancelRequested       bool                  `json:"cancel_requested,omitempty"`
	PauseRequested        bool                  `json:"pause_requested,omitempty"`
	PausedAt              *time.Time            `json:"paused_at,omitempty"`
	PausedFrom            string                `json:"paused_from,omitempty"`
	AbandonedAt           *time.Time            `json:"abandoned_at,omitempty"`
	AbandonReason         string                `json:"abandon_reason,omitempty"`
	RecoveryCount         int                   `json:"recovery_count,omitempty"`
	LastRecoveredAt       *time.Time            `json:"last_recovered_at,omitempty"`
	ExecutorPID           int                   `json:"executor_pid,omitempty"`
	HeartbeatAt           *time.Time            `json:"heartbeat_at,omitempty"`
	OperatorRetries       []OperatorRetryState  `json:"operator_retries,omitempty"`
	CurrentNode           string                `json:"current_node,omitempty"`
	CurrentNodes          []string              `json:"current_nodes,omitempty"`
	Waiting               *WaitingState         `json:"waiting,omitempty"`
	Nodes                 map[string]*NodeState `json:"nodes"`
	Approvals             map[string]string     `json:"approvals"`
	WorkflowFingerprint   string                `json:"workflow_fingerprint,omitempty"`
	ConfigFingerprint     string                `json:"config_fingerprint,omitempty"`
	CommandsFingerprint   string                `json:"commands_fingerprint,omitempty"`
	Revision              uint64                `json:"revision"`
	CreatedAt             time.Time             `json:"created_at"`
	UpdatedAt             time.Time             `json:"updated_at"`
	ErrorCode             string                `json:"error_code,omitempty"`
	Error                 string                `json:"error,omitempty"`
}

type OperatorRetryState struct {
	NodeID           string    `json:"node_id"`
	RequestedAt      time.Time `json:"requested_at"`
	PreviousStatus   string    `json:"previous_status"`
	PreviousAttempts int       `json:"previous_attempts"`
	PreviousError    string    `json:"previous_error,omitempty"`
}

type WaitingState struct {
	NodeID      string   `json:"node_id"`
	Message     string   `json:"message"`
	Kind        string   `json:"kind,omitempty"`
	ChildRunID  string   `json:"child_run_id,omitempty"`
	ChildRunIDs []string `json:"child_run_ids,omitempty"`
}

func (s *RunState) PublicView() *RunState {
	if s == nil {
		return nil
	}
	out := *s
	out.Nodes = make(map[string]*NodeState, len(s.Nodes))
	for id, node := range s.Nodes {
		if node == nil || node.Hidden || node.PublicParent != "" {
			continue
		}
		clone := *node
		clone.Hidden = false
		clone.PublicParent = ""
		clone.LoopPrevious = publicLoopPrevious(id, node.LoopPrevious)
		if node.External != nil {
			external := *node.External
			external.ClaimToken = ""
			clone.External = &external
		}
		out.Nodes[id] = &clone
	}
	if s.Waiting != nil {
		waiting := *s.Waiting
		if node := s.Nodes[waiting.NodeID]; node != nil && node.PublicParent != "" {
			waiting.NodeID = node.PublicParent
		}
		out.Waiting = &waiting
	}
	if node := s.Nodes[s.CurrentNode]; node != nil && node.PublicParent != "" {
		out.CurrentNode = node.PublicParent
	}
	if len(s.CurrentNodes) > 0 {
		seen := map[string]bool{}
		out.CurrentNodes = make([]string, 0, len(s.CurrentNodes))
		for _, id := range s.CurrentNodes {
			publicID := id
			if node := s.Nodes[id]; node != nil && node.PublicParent != "" {
				publicID = node.PublicParent
			}
			if !seen[publicID] {
				seen[publicID] = true
				out.CurrentNodes = append(out.CurrentNodes, publicID)
			}
		}
	}
	out.Approvals = make(map[string]string, len(s.Approvals))
	for id, value := range s.Approvals {
		publicID := id
		if node := s.Nodes[id]; node != nil && node.PublicParent != "" {
			publicID = node.PublicParent
		}
		out.Approvals[publicID] = value
	}
	return &out
}

func publicLoopPrevious(parentID string, values map[string]NodeState) map[string]NodeState {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]NodeState, len(values))
	for id, node := range values {
		if node.Hidden {
			continue
		}
		node.Hidden = false
		node.PublicParent = ""
		node.LoopPrevious = publicLoopPrevious(id, node.LoopPrevious)
		publicID := strings.TrimPrefix(id, parentID+"__")
		out[publicID] = node
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

type Event struct {
	Time     time.Time      `json:"time"`
	Type     string         `json:"type"`
	RunID    string         `json:"run_id"`
	NodeID   string         `json:"node_id,omitempty"`
	Revision uint64         `json:"revision"`
	Data     map[string]any `json:"data,omitempty"`
}

type Repository interface {
	RunDir(id string) string
	ArtifactsDir(id string) string
	Save(state *RunState) error
	Commit(state *RunState, event Event) error
	Load(id string) (*RunState, error)
}

type FS struct{ Workspace string }

var safeRunID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func ValidateRunID(id string) error {
	if !safeRunID.MatchString(id) || id == "." || id == ".." {
		return fmt.Errorf("unsafe run id %q", id)
	}
	return nil
}

func (f FS) RunDir(id string) string       { return filepath.Join(f.Workspace, ".takt", "runs", id) }
func (f FS) ArtifactsDir(id string) string { return filepath.Join(f.RunDir(id), "artifacts") }

func (f FS) Save(state *RunState) error {
	if err := ValidateRunID(state.ID); err != nil {
		return err
	}
	state.UpdatedAt = time.Now().UTC()
	return f.writeStateAtomic(state)
}

// Commit persists one state transition and its matching event. Both records use
// the same revision. The event file is replaced before state.json; Load detects
// an interrupted second rename as an inconsistent store instead of silently
// returning stale state.
func (f FS) Commit(state *RunState, event Event) error {
	if err := ValidateRunID(state.ID); err != nil {
		return err
	}
	dir := f.RunDir(state.ID)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		return err
	}
	oldRevision, oldUpdated := state.Revision, state.UpdatedAt
	state.Revision++
	state.UpdatedAt = time.Now().UTC()
	event.RunID = state.ID
	event.Revision = state.Revision
	if event.Time.IsZero() {
		event.Time = state.UpdatedAt
	}

	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	eventBytes, err := json.Marshal(event)
	if err != nil {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	eventsPath := filepath.Join(dir, "events.jsonl")
	existing, err := os.ReadFile(eventsPath)
	if err != nil && !os.IsNotExist(err) {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	existing = append(existing, eventBytes...)
	existing = append(existing, '\n')

	stateTmp := filepath.Join(dir, "state.json.tmp")
	eventsTmp := filepath.Join(dir, "events.jsonl.tmp")
	indexTmp := filepath.Join(dir, eventIndexFile+".tmp")
	indexBytes := buildEventIndex(existing)
	if err := writeFileSync(stateTmp, stateBytes, 0o644); err != nil {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := writeFileSync(eventsTmp, existing, 0o644); err != nil {
		_ = os.Remove(stateTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := writeFileSync(indexTmp, indexBytes, 0o644); err != nil {
		_ = os.Remove(stateTmp)
		_ = os.Remove(eventsTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := os.Rename(eventsTmp, eventsPath); err != nil {
		_ = os.Remove(stateTmp)
		_ = os.Remove(indexTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := os.Rename(indexTmp, filepath.Join(dir, eventIndexFile)); err != nil {
		_ = os.Remove(stateTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return &InconsistentError{RunID: state.ID, Err: fmt.Errorf("event revision was committed but event index rename failed: %w", err)}
	}
	if err := os.Rename(stateTmp, filepath.Join(dir, "state.json")); err != nil {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return &InconsistentError{RunID: state.ID, Err: fmt.Errorf("event revision was committed but state rename failed: %w", err)}
	}
	return syncDir(dir)
}

func (f FS) writeStateAtomic(state *RunState) error {
	dir := f.RunDir(state.ID)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "state.json.tmp")
	if err := writeFileSync(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, "state.json")); err != nil {
		return err
	}
	return syncDir(dir)
}

func (f FS) Load(id string) (*RunState, error) {
	if err := ValidateRunID(id); err != nil {
		return nil, err
	}
	const attempts = 8
	var mismatch error
	for attempt := 0; attempt < attempts; attempt++ {
		before, err := f.readState(id)
		if err != nil {
			return nil, err
		}
		lastRevision, err := f.lastEventRevision(id)
		if err != nil {
			return nil, err
		}
		if before.Revision == lastRevision {
			return before, nil
		}
		// A writer replaces events before state. Re-reading state closes the
		// common torn-read window without waiting when the second rename has
		// already completed.
		after, err := f.readState(id)
		if err != nil {
			return nil, err
		}
		if after.Revision == lastRevision {
			return after, nil
		}
		mismatch = fmt.Errorf("state revision %d differs from event revision %d", after.Revision, lastRevision)
		if attempt+1 < attempts {
			time.Sleep(time.Duration(attempt+1) * time.Millisecond)
		}
	}
	return nil, &InconsistentError{RunID: id, Err: mismatch}
}

func (f FS) readState(id string) (*RunState, error) {
	b, err := os.ReadFile(filepath.Join(f.RunDir(id), "state.json"))
	if err != nil {
		return nil, err
	}
	var state RunState
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("decode run state: %w", err)
	}
	if state.ID != id {
		return nil, &InconsistentError{RunID: id, Err: fmt.Errorf("state contains run id %q", state.ID)}
	}
	return &state, nil
}

func (f FS) lastEventRevision(id string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(f.RunDir(id), "events.jsonl"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	if len(lines) == 0 || len(bytes.TrimSpace(lines[len(lines)-1])) == 0 {
		return 0, nil
	}
	var event Event
	if err := json.Unmarshal(lines[len(lines)-1], &event); err != nil {
		return 0, &InconsistentError{RunID: id, Err: fmt.Errorf("decode last event: %w", err)}
	}
	return event.Revision, nil
}

func (f FS) AcquireLock(id string) (func() error, error) {
	if err := ValidateRunID(id); err != nil {
		return nil, err
	}
	dir := f.RunDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("run %s is locked by another process", id)
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return func() error { return os.Remove(path) }, nil
}

// RequestCancel writes an out-of-band cancellation marker. A running process
// polls this marker, so cancellation does not depend on rewriting state.json
// concurrently with the executor.
func (f FS) RequestCancel(id string) error {
	if err := ValidateRunID(id); err != nil {
		return err
	}
	dir := f.RunDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileSync(filepath.Join(dir, "cancel.requested"), []byte("cancel\n"), 0o644)
}

func (f FS) CancelRequested(id string) (bool, error) {
	if err := ValidateRunID(id); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(f.RunDir(id), "cancel.requested"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (f FS) ClearCancel(id string) error {
	if err := ValidateRunID(id); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(f.RunDir(id), "cancel.requested"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (f FS) RequestPause(id string) error {
	return f.writeOperatorMarker(id, "pause.requested", "pause\n")
}

func (f FS) PauseRequested(id string) (bool, error) {
	return f.operatorMarkerExists(id, "pause.requested")
}

func (f FS) ClearPause(id string) error {
	return f.clearOperatorMarker(id, "pause.requested")
}

func (f FS) RequestAbandon(id, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "abandoned by operator"
	}
	return f.writeOperatorMarker(id, "abandon.requested", reason+"\n")
}

func (f FS) AbandonRequested(id string) (bool, string, error) {
	if err := ValidateRunID(id); err != nil {
		return false, "", err
	}
	raw, err := os.ReadFile(filepath.Join(f.RunDir(id), "abandon.requested"))
	if err == nil {
		return true, strings.TrimSpace(string(raw)), nil
	}
	if os.IsNotExist(err) {
		return false, "", nil
	}
	return false, "", err
}

func (f FS) ClearAbandon(id string) error {
	return f.clearOperatorMarker(id, "abandon.requested")
}

func (f FS) writeOperatorMarker(id, name, value string) error {
	if err := ValidateRunID(id); err != nil {
		return err
	}
	dir := f.RunDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeFileSync(filepath.Join(dir, name), []byte(value), 0o600)
}

func (f FS) operatorMarkerExists(id, name string) (bool, error) {
	if err := ValidateRunID(id); err != nil {
		return false, err
	}
	_, err := os.Stat(filepath.Join(f.RunDir(id), name))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (f FS) clearOperatorMarker(id, name string) error {
	if err := ValidateRunID(id); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(f.RunDir(id), name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

type InconsistentError struct {
	RunID string
	Err   error
}

func (e *InconsistentError) Error() string {
	return fmt.Sprintf("run %s store is inconsistent: %v", e.RunID, e.Err)
}
func (e *InconsistentError) Unwrap() error { return e.Err }

func writeFileSync(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
