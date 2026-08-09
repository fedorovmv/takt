// Package runcontrol contains small persistence helpers shared by stable
// control-plane use cases. It owns no workflow or execution policy.
package runcontrol

import (
	"fmt"
	"strings"
	"time"

	"takt/internal/config"
	"takt/internal/redact"
	"takt/internal/store"
)

type StateStore interface {
	Load(string) (*store.RunState, error)
	Commit(*store.RunState, store.Event) error
}

type LockStore interface {
	AcquireLock(string) (func() error, error)
}

// AcquireLock serializes short control-plane mutations from concurrent CLI,
// MCP, daemon and worker clients. The Store boundary remains non-blocking;
// this helper owns the bounded retry policy.
func AcquireLock(st LockStore, runID string) (func() error, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		release, err := st.AcquireLock(runID)
		if err == nil {
			return release, nil
		}
		if !strings.Contains(err.Error(), "locked by another process") || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func PersistenceRedactor(defaultConfigPath, configPath string) (*redact.Redactor, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = strings.TrimSpace(defaultConfigPath)
	}
	if configPath == "" {
		return redact.NewFromEnvironment(), nil
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("load persistence redaction config %s: %w", configPath, err)
	}
	return redact.NewFromConfig(cfg), nil
}

func CommitRedacted(defaultConfigPath string, st StateStore, state *store.RunState, event store.Event) error {
	r, err := PersistenceRedactor(defaultConfigPath, state.ConfigPath)
	if err != nil {
		return err
	}
	persisted, err := redact.CloneRunState(state)
	if err != nil {
		return err
	}
	redact.RedactRunState(r, persisted)
	event.Data = redact.EventData(r, event.Data)
	if err := st.Commit(persisted, event); err != nil {
		return err
	}
	state.Revision = persisted.Revision
	state.UpdatedAt = persisted.UpdatedAt
	return nil
}

func DurablePublicRun(st interface {
	Load(string) (*store.RunState, error)
}, state *store.RunState) (*store.RunState, error) {
	if state == nil {
		return nil, nil
	}
	persisted, err := st.Load(state.ID)
	if err != nil {
		return nil, err
	}
	return persisted.PublicView(), nil
}
