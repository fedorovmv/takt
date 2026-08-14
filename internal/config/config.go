package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"takt/internal/domainadapter"
	"takt/internal/spec"
	"takt/internal/yamlcodec"
)

func Load(path string) (*spec.Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg spec.Config
	if err := yamlcodec.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if err := validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validate(cfg *spec.Config) error {
	if err := validateHeader(cfg); err != nil {
		return err
	}
	if err := validateModels(cfg); err != nil {
		return err
	}
	if err := validateTaskSources(cfg); err != nil {
		return err
	}
	if err := validateAdapters(cfg); err != nil {
		return err
	}
	return validateAssistants(cfg)
}

func validateHeader(cfg *spec.Config) error {
	if cfg.APIVersion != "takt/v1alpha1" || cfg.Kind != "Config" {
		return fmt.Errorf("config must use apiVersion takt/v1alpha1 and kind Config")
	}
	if cfg.DefaultAssistant != "" {
		if _, ok := cfg.Assistants[cfg.DefaultAssistant]; !ok {
			return fmt.Errorf("default_assistant %q is not defined in assistants", cfg.DefaultAssistant)
		}
	}
	return nil
}

func validateModels(cfg *spec.Config) error {
	for name, model := range cfg.Models {
		if model.Provider == "" || model.ID == "" {
			return fmt.Errorf("model %q requires provider and id", name)
		}
	}
	if len(cfg.Models) > 0 && (cfg.ModelPreset != "" || len(cfg.ModelPresets) > 0) {
		return fmt.Errorf("models and model preset mode are mutually exclusive")
	}
	if len(cfg.ModelPresets) > 0 && cfg.ModelPreset == "" {
		return fmt.Errorf("model_preset is required with model_presets")
	}
	if cfg.ModelPreset != "" {
		if _, ok := cfg.ModelPresets[cfg.ModelPreset]; !ok {
			return fmt.Errorf("model_preset %q is not defined in model_presets", cfg.ModelPreset)
		}
	}
	for name, preset := range cfg.ModelPresets {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("model preset name cannot be empty")
		}
		if len(preset) == 0 {
			return fmt.Errorf("model preset %q cannot be empty", name)
		}
		for role, value := range preset {
			if err := validateModelAlias(role); err != nil {
				return fmt.Errorf("model preset %q: %w", name, err)
			}
			if _, err := parseModelReference(value); err != nil {
				return fmt.Errorf("model preset %q role %q: %w", name, role, err)
			}
		}
	}
	return nil
}

type ModelSelection struct {
	Preset    string
	Overrides map[string]string
}

func MaterializeModels(cfg *spec.Config, selection ModelSelection) (*spec.Config, string, error) {
	if cfg == nil {
		return nil, "", fmt.Errorf("config is required")
	}
	selected := selection.Preset
	if selected == "" {
		selected = cfg.ModelPreset
	}
	models := make(map[string]spec.ModelSpec, len(cfg.Models)+3)
	if selected == "" {
		for role, model := range cfg.Models {
			models[role] = model
		}
	}
	if selected != "" {
		preset, ok := cfg.ModelPresets[selected]
		if !ok {
			return nil, "", fmt.Errorf("model preset %q is not defined", selected)
		}
		for role, value := range preset {
			model, err := parseModelReference(value)
			if err != nil {
				return nil, "", fmt.Errorf("model preset %q role %q: %w", selected, role, err)
			}
			models[role] = model
		}
	}
	for role, value := range selection.Overrides {
		if err := validateModelAlias(role); err != nil {
			return nil, "", err
		}
		if selected != "" {
			if _, ok := cfg.ModelPresets[selected][role]; !ok {
				return nil, "", fmt.Errorf("model alias %q is not defined in preset %q", role, selected)
			}
		} else if _, ok := cfg.Models[role]; !ok {
			return nil, "", fmt.Errorf("model alias %q is not defined in models", role)
		}
		model, err := parseModelReference(value)
		if err != nil {
			return nil, "", fmt.Errorf("model role override %q: %w", role, err)
		}
		models[role] = model
	}
	materialized := *cfg
	materialized.Models = models
	materialized.ModelPreset = ""
	materialized.ModelPresets = nil
	materialized.ModelsMaterialized = selected != "" || len(selection.Overrides) > 0
	return &materialized, selected, nil
}

func parseModelReference(value string) (spec.ModelSpec, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.IndexFunc(value, func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' }) >= 0 {
		return spec.ModelSpec{}, fmt.Errorf("model reference must be provider/model-id without surrounding whitespace")
	}
	provider, id, ok := strings.Cut(value, "/")
	if !ok || provider == "" || id == "" {
		return spec.ModelSpec{}, fmt.Errorf("model reference %q must be provider/model-id", value)
	}
	return spec.ModelSpec{Provider: provider, ID: id}, nil
}

func validateModelAlias(value string) error {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return fmt.Errorf("model alias %q must match [a-z][a-z0-9_]*", value)
	}
	for _, char := range value[1:] {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return fmt.Errorf("model alias %q must match [a-z][a-z0-9_]*", value)
		}
	}
	return nil
}

func validateTaskSources(cfg *spec.Config) error {
	for name, source := range cfg.TaskSources {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("task source name cannot be empty")
		}
		if source.Transport != "process" {
			return fmt.Errorf("task source %q transport must be process", name)
		}
		if len(source.Argv) == 0 || strings.TrimSpace(source.Argv[0]) == "" {
			return fmt.Errorf("task source %q requires argv", name)
		}
		if err := validatePositiveDuration(source.Timeout); err != nil {
			return fmt.Errorf("task source %q timeout must be a positive duration", name)
		}
		if source.MaxOutputBytes < 0 {
			return fmt.Errorf("task source %q max_output_bytes cannot be negative", name)
		}
	}
	return nil
}

func validateAdapters(cfg *spec.Config) error {
	for name, adapter := range cfg.Adapters {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("adapter name cannot be empty")
		}
		switch adapter.Domain {
		case domainadapter.DomainSCM, domainadapter.DomainTracker, domainadapter.DomainCI:
		default:
			return fmt.Errorf("adapter %q domain must be scm, tracker, or ci", name)
		}
		if adapter.Transport != "process" && adapter.Transport != "mcp" {
			return fmt.Errorf("adapter %q transport must be process or mcp", name)
		}
		if len(adapter.Argv) == 0 || strings.TrimSpace(adapter.Argv[0]) == "" {
			return fmt.Errorf("adapter %q requires argv", name)
		}
		if err := validatePositiveDuration(adapter.Timeout); err != nil {
			return fmt.Errorf("adapter %q timeout must be a positive duration", name)
		}
		if adapter.MaxOutputBytes < 0 {
			return fmt.Errorf("adapter %q max_output_bytes cannot be negative", name)
		}
		if err := validateOperationMap(name, "operation", adapter.Operations); err != nil {
			return err
		}
		if err := validateOperationMap(name, "reconcile operation", adapter.ReconcileOperations); err != nil {
			return err
		}
	}
	return nil
}

func validateOperationMap(adapterName, label string, operations map[string]string) error {
	for operation, tool := range operations {
		if err := domainadapter.ValidateOperation(operation); err != nil {
			return fmt.Errorf("adapter %q %s %q: %w", adapterName, label, operation, err)
		}
		if strings.TrimSpace(tool) == "" {
			if label == "operation" {
				return fmt.Errorf("adapter %q operation %q has empty MCP tool mapping", adapterName, operation)
			}
			return fmt.Errorf("adapter %q reconcile operation %q has empty MCP tool mapping", adapterName, operation)
		}
	}
	return nil
}

func validateAssistants(cfg *spec.Config) error {
	for name, assistant := range cfg.Assistants {
		if err := validateAssistantShape(name, assistant); err != nil {
			return err
		}
		if err := validateAssistantCapabilities(name, assistant); err != nil {
			return err
		}
	}
	return nil
}

func validateAssistantShape(name string, assistant spec.AssistantSpec) error {
	if assistant.Type != "mock" && assistant.Type != "process" && assistant.Type != "pi" && assistant.Type != "opencode" {
		return fmt.Errorf("assistant %q has unsupported type %q", name, assistant.Type)
	}
	if assistant.Type == "process" && len(assistant.Argv) == 0 {
		return fmt.Errorf("process assistant %q requires argv", name)
	}
	if assistant.Type == "pi" {
		if len(assistant.Argv) != 0 {
			return fmt.Errorf("pi assistant %q uses binary/args instead of argv", name)
		}
		if assistant.ProjectTrust != "" && assistant.ProjectTrust != "default" && assistant.ProjectTrust != "approve" && assistant.ProjectTrust != "deny" {
			return fmt.Errorf("pi assistant %q project_trust must be default, approve, or deny", name)
		}
		if assistant.Agent != "" || assistant.AutoApprove {
			return fmt.Errorf("pi assistant %q does not support agent/auto_approve", name)
		}
	}
	if assistant.Type == "opencode" {
		if len(assistant.Argv) != 0 {
			return fmt.Errorf("opencode assistant %q uses binary/args instead of argv", name)
		}
		if assistant.SessionDir != "" || assistant.ProjectTrust != "" {
			return fmt.Errorf("opencode assistant %q does not support session_dir/project_trust", name)
		}
	}
	if assistant.Protocol != "" && assistant.Protocol != "takt-assistant/v1alpha1" && assistant.Protocol != "takt-assistant/v1alpha2" {
		return fmt.Errorf("assistant %q has unsupported protocol %q", name, assistant.Protocol)
	}
	if assistant.Type != "process" && assistant.Protocol != "" {
		return fmt.Errorf("assistant %q protocol is supported only for type process", name)
	}
	if assistant.Type != "pi" && assistant.Type != "opencode" && (assistant.Binary != "" || len(assistant.Args) != 0 || assistant.Agent != "" || assistant.AutoApprove || assistant.SessionDir != "" || assistant.ProjectTrust != "") {
		return fmt.Errorf("assistant %q specialized fields are supported only for type pi or opencode", name)
	}
	if assistant.MaxOutputBytes < 0 {
		return fmt.Errorf("assistant %q max_output_bytes cannot be negative", name)
	}
	return nil
}

func validateAssistantCapabilities(name string, assistant spec.AssistantSpec) error {
	builtin := map[string]bool{}
	switch assistant.Type {
	case "pi":
		builtin = map[string]bool{"tool_policy": true, "skills": true, "sandbox_filesystem": true}
	case "opencode":
		builtin = map[string]bool{"tool_policy": true, "skills": true, "mcp": true, "sandbox_filesystem": true}
	}
	reserved := map[string]bool{"tool_policy": true, "skills": true, "mcp": true, "sandbox_filesystem": true, "sandbox_network": true}
	seen := map[string]bool{}
	for _, capability := range assistant.Capabilities {
		if strings.TrimSpace(capability) == "" {
			return fmt.Errorf("assistant %q capabilities contains an empty value", name)
		}
		if seen[capability] {
			return fmt.Errorf("assistant %q capabilities contains duplicate %q", name, capability)
		}
		if assistant.Type != "process" && reserved[capability] && !builtin[capability] {
			return fmt.Errorf("assistant %q type %s cannot declare unsupported built-in capability %q", name, assistant.Type, capability)
		}
		seen[capability] = true
	}
	return nil
}

func validatePositiveDuration(raw string) error {
	if raw == "" {
		return nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	return nil
}
