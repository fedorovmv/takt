package control

import (
	"encoding/json"

	"takt/internal/config"
	"takt/internal/dynamicplan"
	"takt/internal/redact"
	"takt/internal/store"
)

func (s *Service) persistenceRedactor(configPath string) *redact.Redactor {
	if configPath == "" {
		configPath = s.ConfigPath
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return redact.NewFromEnvironment()
	}
	return redact.NewFromConfig(cfg)
}

func (s *Service) commitRedacted(st store.FS, state *store.RunState, event store.Event) error {
	r := s.persistenceRedactor(state.ConfigPath)
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

func (s *Service) savePlanRecord(record *dynamicplan.Record) error {
	if record == nil {
		return nil
	}
	r := s.persistenceRedactor(record.ConfigPath)
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
