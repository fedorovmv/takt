package control

import (
	"takt/internal/config"
	"takt/internal/redact"
	"takt/internal/store"
)

func (s *Service) persistenceRedactor() *redact.Redactor {
	r := redact.NewFromEnvironment()
	cfg, err := config.Load(s.ConfigPath)
	if err != nil || cfg == nil {
		return r
	}
	for _, assistant := range cfg.Assistants {
		for _, value := range assistant.Env {
			r.RegisterReferences(value)
		}
	}
	for _, adapter := range cfg.Adapters {
		for _, value := range adapter.Env {
			r.RegisterReferences(value)
		}
	}
	return r
}

func (s *Service) commitRedacted(st store.FS, state *store.RunState, event store.Event) error {
	r := s.persistenceRedactor()
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
