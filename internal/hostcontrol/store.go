package hostcontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	EnforcementAdvisory = "advisory"
	EnforcementGuarded  = "guarded"
	EnforcementStrict   = "strict"

	StatusPreview   = "preview"
	StatusManaged   = "managed"
	StatusWaiting   = "waiting"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusReleased  = "released"
)

type Capabilities struct {
	CommandInterception bool `json:"command_interception"`
	InputInterception   bool `json:"input_interception"`
	ToolCallBlocking    bool `json:"tool_call_blocking"`
	CompletionBlocking  bool `json:"completion_blocking"`
	SessionRecovery     bool `json:"session_recovery"`
}

func (c Capabilities) StrictReady() bool {
	return c.CommandInterception && c.InputInterception && c.ToolCallBlocking && c.CompletionBlocking && c.SessionRecovery
}

type Session struct {
	ID            string       `json:"id"`
	Host          string       `json:"host"`
	HostSessionID string       `json:"host_session_id"`
	PlanID        string       `json:"plan_id"`
	Status        string       `json:"status"`
	Enforcement   string       `json:"enforcement"`
	Capabilities  Capabilities `json:"capabilities"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	ReleasedAt    *time.Time   `json:"released_at,omitempty"`
}

type Store struct{ Workspace string }

func (s Store) Root() string          { return filepath.Join(s.Workspace, ".takt", "host-sessions") }
func (s Store) Path(id string) string { return filepath.Join(s.Root(), id+".json") }

func ValidateID(id string) error {
	if !strings.HasPrefix(id, "host-") || len(id) > 96 || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid host session id %q", id)
	}
	return nil
}

func (s Store) Save(session *Session) error {
	if session == nil {
		return fmt.Errorf("host session is required")
	}
	if err := ValidateID(session.ID); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(s.Root(), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Root(), ".host-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.Path(session.ID))
}

func (s Store) Load(id string) (*Session, error) {
	if err := ValidateID(id); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(s.Path(id))
	if err != nil {
		return nil, err
	}
	var session Session
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&session); err != nil {
		return nil, err
	}
	if session.Enforcement == "" {
		session.Enforcement = EnforcementAdvisory
	}
	return &session, nil
}

func (s Store) List() ([]*Session, error) {
	entries, err := os.ReadDir(s.Root())
	if errors.Is(err, os.ErrNotExist) {
		return []*Session{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]*Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		session, loadErr := s.Load(id)
		if loadErr == nil {
			out = append(out, session)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (s Store) Find(host, hostSessionID string) (*Session, error) {
	sessions, err := s.List()
	if err != nil {
		return nil, err
	}
	for i := len(sessions) - 1; i >= 0; i-- {
		item := sessions[i]
		if item.Host == host && item.HostSessionID == hostSessionID && item.Status != StatusReleased {
			return item, nil
		}
	}
	return nil, os.ErrNotExist
}
