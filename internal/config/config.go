package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"takt/internal/domainadapter"

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
	if cfg.DefaultAssistant != "" {
		if _, ok := cfg.Assistants[cfg.DefaultAssistant]; !ok {
			return nil, fmt.Errorf("default_assistant %q is not defined in assistants", cfg.DefaultAssistant)
		}
	}
	for name, model := range cfg.Models {
		if model.Provider == "" || model.ID == "" {
			return nil, fmt.Errorf("model %q requires provider and id", name)
		}
	}
	for name, source := range cfg.TaskSources {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("task source name cannot be empty")
		}
		if source.Transport != "process" {
			return nil, fmt.Errorf("task source %q transport must be process", name)
		}
		if len(source.Argv) == 0 || strings.TrimSpace(source.Argv[0]) == "" {
			return nil, fmt.Errorf("task source %q requires argv", name)
		}
		if source.Timeout != "" {
			value, err := time.ParseDuration(source.Timeout)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("task source %q timeout must be a positive duration", name)
			}
		}
		if source.MaxOutputBytes < 0 {
			return nil, fmt.Errorf("task source %q max_output_bytes cannot be negative", name)
		}
	}
	for name, adapter := range cfg.Adapters {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("adapter name cannot be empty")
		}
		switch adapter.Domain {
		case domainadapter.DomainSCM, domainadapter.DomainTracker, domainadapter.DomainCI:
		default:
			return nil, fmt.Errorf("adapter %q domain must be scm, tracker, or ci", name)
		}
		if adapter.Transport != "process" && adapter.Transport != "mcp" {
			return nil, fmt.Errorf("adapter %q transport must be process or mcp", name)
		}
		if len(adapter.Argv) == 0 || strings.TrimSpace(adapter.Argv[0]) == "" {
			return nil, fmt.Errorf("adapter %q requires argv", name)
		}
		if adapter.Timeout != "" {
			value, err := time.ParseDuration(adapter.Timeout)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("adapter %q timeout must be a positive duration", name)
			}
		}
		if adapter.MaxOutputBytes < 0 {
			return nil, fmt.Errorf("adapter %q max_output_bytes cannot be negative", name)
		}
		for operation, tool := range adapter.Operations {
			if err := domainadapter.ValidateOperation(operation); err != nil {
				return nil, fmt.Errorf("adapter %q operation %q: %w", name, operation, err)
			}
			if strings.TrimSpace(tool) == "" {
				return nil, fmt.Errorf("adapter %q operation %q has empty MCP tool mapping", name, operation)
			}
		}
		for operation, tool := range adapter.ReconcileOperations {
			if err := domainadapter.ValidateOperation(operation); err != nil {
				return nil, fmt.Errorf("adapter %q reconcile operation %q: %w", name, operation, err)
			}
			if strings.TrimSpace(tool) == "" {
				return nil, fmt.Errorf("adapter %q reconcile operation %q has empty MCP tool mapping", name, operation)
			}
		}
	}
	for name, assistant := range cfg.Assistants {
		if assistant.Type != "mock" && assistant.Type != "process" && assistant.Type != "pi" && assistant.Type != "opencode" {
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
			if assistant.Agent != "" || assistant.AutoApprove {
				return nil, fmt.Errorf("pi assistant %q does not support agent/auto_approve", name)
			}
		}
		if assistant.Type == "opencode" {
			if len(assistant.Argv) != 0 {
				return nil, fmt.Errorf("opencode assistant %q uses binary/args instead of argv", name)
			}
			if assistant.SessionDir != "" || assistant.ProjectTrust != "" {
				return nil, fmt.Errorf("opencode assistant %q does not support session_dir/project_trust", name)
			}
		}
		if assistant.Protocol != "" && assistant.Protocol != "takt-assistant/v1alpha1" && assistant.Protocol != "takt-assistant/v1alpha2" {
			return nil, fmt.Errorf("assistant %q has unsupported protocol %q", name, assistant.Protocol)
		}
		if assistant.Type != "process" && assistant.Protocol != "" {
			return nil, fmt.Errorf("assistant %q protocol is supported only for type process", name)
		}
		if assistant.Type != "pi" && assistant.Type != "opencode" && (assistant.Binary != "" || len(assistant.Args) != 0 || assistant.Agent != "" || assistant.AutoApprove || assistant.SessionDir != "" || assistant.ProjectTrust != "") {
			return nil, fmt.Errorf("assistant %q specialized fields are supported only for type pi or opencode", name)
		}
		if assistant.MaxOutputBytes < 0 {
			return nil, fmt.Errorf("assistant %q max_output_bytes cannot be negative", name)
		}
		builtinCapabilities := map[string]bool{}
		switch assistant.Type {
		case "pi":
			builtinCapabilities = map[string]bool{"tool_policy": true, "skills": true, "sandbox_filesystem": true}
		case "opencode":
			builtinCapabilities = map[string]bool{"tool_policy": true, "skills": true, "mcp": true, "sandbox_filesystem": true}
		}
		reservedCapabilities := map[string]bool{"tool_policy": true, "skills": true, "mcp": true, "sandbox_filesystem": true, "sandbox_network": true}
		seenCapabilities := map[string]bool{}
		for _, capability := range assistant.Capabilities {
			if strings.TrimSpace(capability) == "" {
				return nil, fmt.Errorf("assistant %q capabilities contains an empty value", name)
			}
			if seenCapabilities[capability] {
				return nil, fmt.Errorf("assistant %q capabilities contains duplicate %q", name, capability)
			}
			if assistant.Type != "process" && reservedCapabilities[capability] && !builtinCapabilities[capability] {
				return nil, fmt.Errorf("assistant %q type %s cannot declare unsupported built-in capability %q", name, assistant.Type, capability)
			}
			seenCapabilities[capability] = true
		}
	}
	return &cfg, nil
}
