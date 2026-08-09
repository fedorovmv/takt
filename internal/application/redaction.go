package application

import (
	"fmt"
	"strings"

	"takt/internal/config"
	"takt/internal/redact"
	"takt/internal/store"
)

func persistenceRedactor(defaultConfigPath, configPath string) (*redact.Redactor, error) {
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

func commitRedacted(defaultConfigPath string, st RunStateStore, state *store.RunState, event store.Event) error {
	r, err := persistenceRedactor(defaultConfigPath, state.ConfigPath)
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
