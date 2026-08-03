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
		if assistant.Type != "mock" && assistant.Type != "process" && assistant.Type != "pi" {
			return nil, fmt.Errorf("assistant %q has unsupported type %q", name, assistant.Type)
		}
		if assistant.Type == "process" && len(assistant.Argv) == 0 {
			return nil, fmt.Errorf("process assistant %q requires argv", name)
		}
		if assistant.Type == "pi" {
			if len(assistant.Argv) != 0 {
				return nil, fmt.Errorf("pi assistant %q uses binary/args instead of argv", name)
			}
			if assistant.ProjectTrust != "" && assistant.ProjectTrust != "default" && assistant.ProjectTrust != "approve" && assistant.ProjectTrust != "deny" {
				return nil, fmt.Errorf("pi assistant %q project_trust must be default, approve, or deny", name)
			}
		}
		if assistant.Protocol != "" && assistant.Protocol != "takt-assistant/v1alpha1" {
			return nil, fmt.Errorf("assistant %q has unsupported protocol %q", name, assistant.Protocol)
		}
		if assistant.Type != "process" && assistant.Protocol != "" {
			return nil, fmt.Errorf("assistant %q protocol is supported only for type process", name)
		}
		if assistant.Type != "pi" && (assistant.Binary != "" || len(assistant.Args) != 0 || assistant.SessionDir != "" || assistant.ProjectTrust != "") {
			return nil, fmt.Errorf("assistant %q binary/args/session_dir/project_trust are supported only for type pi", name)
		}
		if assistant.MaxOutputBytes < 0 {
			return nil, fmt.Errorf("assistant %q max_output_bytes cannot be negative", name)
		}
	}
	return &cfg, nil
}
