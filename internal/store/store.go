package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	RunWaiting   = "waiting"
	RunCompleted = "completed"
	RunFailed    = "failed"
	RunCancelled = "cancelled"
)

type ModelRef struct {
	Name     string         `json:"name,omitempty"`
	Provider string         `json:"provider,omitempty"`
	ID       string         `json:"id,omitempty"`
	Params   map[string]any `json:"params,omitempty"`
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

type NodeState struct {
	Status           string               `json:"status"`
	Output           string               `json:"output,omitempty"`
	Stdout           string               `json:"stdout,omitempty"`
	Stderr           string               `json:"stderr,omitempty"`
	OutputTruncated  bool                 `json:"output_truncated,omitempty"`
	Usage            *Usage               `json:"usage,omitempty"`
	Assistant        string               `json:"assistant,omitempty"`
	AssistantVersion string               `json:"assistant_version,omitempty"`
	RequestedModel   *ModelRef            `json:"requested_model,omitempty"`
	ResolvedModel    *ModelRef            `json:"resolved_model,omitempty"`
	ExitCode         int                  `json:"exit_code,omitempty"`
	Attempts         int                  `json:"attempts,omitempty"`
	Feedback         string               `json:"feedback,omitempty"`
	SessionID        string               `json:"session_id,omitempty"`
	Resumed          bool                 `json:"resumed,omitempty"`
	ErrorCode        string               `json:"error_code,omitempty"`
	Error            string               `json:"error,omitempty"`
	Executions       []ExecutionState     `json:"executions,omitempty"`
	LoopPrevious     map[string]NodeState `json:"loop_previous,omitempty"`
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

type RunState struct {
	ID                  string                `json:"id"`
	Status              string                `json:"status"`
	WorkflowPath        string                `json:"workflow_path"`
	ConfigPath          string                `json:"config_path"`
	Workspace           string                `json:"workspace"`
	Input               string                `json:"input"`
	CurrentNode         string                `json:"current_node,omitempty"`
	Waiting             *WaitingState         `json:"waiting,omitempty"`
	Nodes               map[string]*NodeState `json:"nodes"`
	Approvals           map[string]string     `json:"approvals"`
	WorkflowFingerprint string                `json:"workflow_fingerprint,omitempty"`
	ConfigFingerprint   string                `json:"config_fingerprint,omitempty"`
	CommandsFingerprint string                `json:"commands_fingerprint,omitempty"`
	Revision            uint64                `json:"revision"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
	ErrorCode           string                `json:"error_code,omitempty"`
	Error               string                `json:"error,omitempty"`
}

type WaitingState struct {
	NodeID  string `json:"node_id"`
	Message string `json:"message"`
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
	if err := writeFileSync(stateTmp, stateBytes, 0o644); err != nil {
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := writeFileSync(eventsTmp, existing, 0o644); err != nil {
		_ = os.Remove(stateTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
	}
	if err := os.Rename(eventsTmp, eventsPath); err != nil {
		_ = os.Remove(stateTmp)
		state.Revision, state.UpdatedAt = oldRevision, oldUpdated
		return err
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
	lastRevision, err := f.lastEventRevision(id)
	if err != nil {
		return nil, err
	}
	if state.Revision != lastRevision {
		return nil, &InconsistentError{RunID: id, Err: fmt.Errorf("state revision %d differs from event revision %d", state.Revision, lastRevision)}
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
