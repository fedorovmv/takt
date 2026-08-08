package application

import (
	"encoding/json"
	"fmt"
	"strings"

	"takt/internal/config"
	"takt/internal/dynamicplan"
	"takt/internal/redact"
	"takt/internal/store"
)

func (s *Context) persistenceRedactor(configPath string) (*redact.Redactor, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = strings.TrimSpace(s.ConfigPath)
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

func (s *Context) commitRedacted(st RunStore, state *store.RunState, event store.Event) error {
	r, err := s.persistenceRedactor(state.ConfigPath)
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

func (s *Context) savePlanRecord(record *dynamicplan.Record) error {
	if record == nil {
		return nil
	}
	r, err := s.persistenceRedactor(record.ConfigPath)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	redacted := r.Any(decoded)
	raw, err = json.Marshal(redacted)
	if err != nil {
		return err
	}
	var persisted dynamicplan.Record
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return err
	}
	return (dynamicplan.Store{Workspace: s.Workspace}).Save(&persisted)
}
