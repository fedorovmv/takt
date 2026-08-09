package dynamicflow

import (
	"encoding/json"
	"fmt"
	"strings"

	"takt/internal/config"
	"takt/internal/experimental/dynamicplan"
	"takt/internal/redact"
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

func savePlanRecord(defaultConfigPath string, planStore PlanStore, record *dynamicplan.Record) error {
	if record == nil {
		return nil
	}
	r, err := persistenceRedactor(defaultConfigPath, record.ConfigPath)
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
	return planStore.Save(&persisted)
}

func (s *PlanService) savePlanRecord(record *dynamicplan.Record) error {
	return savePlanRecord(s.configPath, s.store, record)
}
