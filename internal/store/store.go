package store

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type NodeState struct {
	Status       string               `json:"status"`
	Output       string               `json:"output,omitempty"`
	ExitCode     int                  `json:"exit_code,omitempty"`
	Attempts     int                  `json:"attempts,omitempty"`
	Feedback     string               `json:"feedback,omitempty"`
	SessionID    string               `json:"session_id,omitempty"`
	LoopPrevious map[string]NodeState `json:"loop_previous,omitempty"`
}

type RunState struct {
	ID           string                `json:"id"`
	Status       string                `json:"status"`
	WorkflowPath string                `json:"workflow_path"`
	ConfigPath   string                `json:"config_path"`
	Workspace    string                `json:"workspace"`
	Input        string                `json:"input"`
	CurrentNode  string                `json:"current_node,omitempty"`
	Waiting      *WaitingState         `json:"waiting,omitempty"`
	Nodes        map[string]*NodeState `json:"nodes"`
	Approvals    map[string]string     `json:"approvals"`
	CreatedAt    time.Time             `json:"created_at"`
	UpdatedAt    time.Time             `json:"updated_at"`
	Error        string                `json:"error,omitempty"`
}

type WaitingState struct {
	NodeID  string `json:"node_id"`
	Message string `json:"message"`
}

type Event struct {
	Time   time.Time      `json:"time"`
	Type   string         `json:"type"`
	RunID  string         `json:"run_id"`
	NodeID string         `json:"node_id,omitempty"`
	Data   map[string]any `json:"data,omitempty"`
}

type FS struct{ Workspace string }

func (f FS) RunDir(id string) string       { return filepath.Join(f.Workspace, ".takt", "runs", id) }
func (f FS) ArtifactsDir(id string) string { return filepath.Join(f.RunDir(id), "artifacts") }

func (f FS) Save(state *RunState) error {
	state.UpdatedAt = time.Now().UTC()
	dir := f.RunDir(state.ID)
	if err := os.MkdirAll(filepath.Join(dir, "artifacts"), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "state.json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "state.json"))
}

func (f FS) Load(id string) (*RunState, error) {
	b, err := os.ReadFile(filepath.Join(f.RunDir(id), "state.json"))
	if err != nil {
		return nil, err
	}
	var s RunState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (f FS) AppendEvent(e Event) error {
	dir := f.RunDir(e.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	w := bufio.NewWriter(file)
	b, _ := json.Marshal(e)
	if _, err := fmt.Fprintln(w, string(b)); err != nil {
		return err
	}
	return w.Flush()
}
