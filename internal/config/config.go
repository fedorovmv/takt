package config

import (
	"fmt"
	"os"

	"takt/internal/spec"
	"takt/internal/yamlmini"
)

func Load(path string) (*spec.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg spec.Config
	if err := yamlmini.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.APIVersion != "takt/v1alpha1" || cfg.Kind != "Config" {
		return nil, fmt.Errorf("config must use apiVersion takt/v1alpha1 and kind Config")
	}
	for name, model := range cfg.Models {
		if model.Provider == "" || model.ID == "" {
			return nil, fmt.Errorf("model %q requires provider and id", name)
		}
	}
	for name, assistant := range cfg.Assistants {
		if assistant.Type != "mock" && assistant.Type != "process" {
			return nil, fmt.Errorf("assistant %q has unsupported type %q", name, assistant.Type)
		}
		if assistant.Type == "process" && len(assistant.Argv) == 0 {
			return nil, fmt.Errorf("process assistant %q requires argv", name)
		}
	}
	return &cfg, nil
}
