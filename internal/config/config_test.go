package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"takt/internal/spec"
)

func TestLoadModelPresets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
model_preset: local
model_presets:
  local:
    implementation: aihub/Qwen/Qwen3-Coder-Next
    review: anthropic/claude-sonnet-4
    tester: openai/gpt-5-mini
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ModelPresets["local"]["implementation"]; got != "aihub/Qwen/Qwen3-Coder-Next" {
		t.Fatalf("implementation = %q", got)
	}
}

func TestBuiltInAnalysisModelAliasLoadsInBothConfigModes(t *testing.T) {
	root := filepath.Join("..", "profile", "builtin")
	for _, tc := range []struct {
		name   string
		path   string
		preset string
	}{
		{name: "legacy", path: filepath.Join(root, "code", "config.example.yaml")},
		{name: "preset", path: filepath.Join(root, "evaluation", "config.example.yaml"), preset: "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			materialized, _, err := MaterializeModels(cfg, ModelSelection{Preset: tc.preset})
			if err != nil {
				t.Fatal(err)
			}
			model, ok := materialized.Models["takt_analyze"]
			if !ok || model.Provider == "" || model.ID == "" {
				t.Fatalf("missing materialized takt_analyze alias: %+v", materialized.Models)
			}
		})
	}
}

func TestLoadRejectsInvalidModelPresets(t *testing.T) {
	for _, tc := range []struct {
		name, defaults, preset string
	}{
		{name: "unknown preset", defaults: "model_preset: missing\n", preset: validPresetYAML()},
		{name: "missing selected preset", preset: "model_presets:\n  p:\n    tester: aihub/model\n"},
		{name: "missing provider", defaults: "model_preset: p\n", preset: "model_presets:\n  p:\n    tester: /model\n"},
		{name: "missing model", defaults: "model_preset: p\n", preset: "model_presets:\n  p:\n    tester: aihub/\n"},
		{name: "surrounding whitespace", defaults: "model_preset: p\n", preset: "model_presets:\n  p:\n    tester: ' aihub/model'\n"},
		{name: "embedded whitespace", defaults: "model_preset: p\n", preset: "model_presets:\n  p:\n    tester: 'ai hub/model'\n"},
		{name: "mixed source modes", defaults: "model_preset: p\nmodels:\n  tester: {provider: base, id: model}\n", preset: validPresetYAML()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "apiVersion: takt/v1alpha1\nkind: Config\n" + tc.defaults + tc.preset
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected invalid model preset to fail")
			}
		})
	}
}

func TestMaterializeModelsPrecedenceAndIsolation(t *testing.T) {
	cfg := &spec.Config{
		Models: map[string]spec.ModelSpec{
			"implementation": {Provider: "base", ID: "implementation", Params: map[string]any{"temperature": 1}},
			"review":         {Provider: "base", ID: "review"},
			"extra":          {Provider: "base", ID: "extra"},
		},
		ModelPreset: "default",
		ModelPresets: map[string]spec.ModelPreset{
			"default": {"implementation": "default/impl", "review": "default/review", "tester": "default/tester"},
			"chosen":  {"implementation": "aihub/Qwen/Qwen3-Coder-Next", "review": "anthropic/claude-sonnet-4", "tester": "openai/gpt-5-mini"},
		},
	}
	wantSource := *cfg
	wantModels := cfg.Models
	materialized, selected, err := MaterializeModels(cfg, ModelSelection{
		Preset: "chosen",
		Overrides: map[string]string{
			"tester": "override/vendor/model",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != "chosen" {
		t.Fatalf("selected preset = %q", selected)
	}
	want := map[string]spec.ModelSpec{
		"implementation": {Provider: "aihub", ID: "Qwen/Qwen3-Coder-Next"},
		"review":         {Provider: "anthropic", ID: "claude-sonnet-4"},
		"tester":         {Provider: "override", ID: "vendor/model"},
	}
	if !reflect.DeepEqual(materialized.Models, want) {
		t.Fatalf("models = %#v, want %#v", materialized.Models, want)
	}
	if materialized.ModelPreset != "" || materialized.ModelPresets != nil {
		t.Fatalf("materialized config retained preset metadata: %+v", materialized)
	}
	if !reflect.DeepEqual(cfg.Models, wantModels) || cfg.ModelPreset != wantSource.ModelPreset || len(cfg.ModelPresets) != len(wantSource.ModelPresets) {
		t.Fatal("source config was mutated")
	}
}

func TestMaterializeModelsUsesDefaultAndSplitsFirstSlash(t *testing.T) {
	cfg := &spec.Config{ModelPreset: "p", ModelPresets: map[string]spec.ModelPreset{
		"p": {"implementation": "aihub/org/model", "tester": "aihub/review"},
	}}
	materialized, selected, err := MaterializeModels(cfg, ModelSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if selected != "p" || materialized.Models["implementation"].Provider != "aihub" || materialized.Models["implementation"].ID != "org/model" || materialized.Models["tester"].ID != "review" {
		t.Fatalf("unexpected materialization: selected=%q models=%+v", selected, materialized.Models)
	}
}

func TestMaterializeModelsRejectsUnknownSelectionAndOverrideRole(t *testing.T) {
	cfg := &spec.Config{ModelPresets: map[string]spec.ModelPreset{"p": {"implementation": "a/i", "tester": "a/t"}}}
	if _, _, err := MaterializeModels(cfg, ModelSelection{Preset: "missing"}); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown preset error = %v", err)
	}
	base := &spec.Config{Models: map[string]spec.ModelSpec{"tester": {Provider: "base", ID: "tester"}}}
	if materialized, _, err := MaterializeModels(base, ModelSelection{Overrides: map[string]string{"tester": "a/model"}}); err != nil || materialized.Models["tester"].ID != "model" {
		t.Fatalf("expected arbitrary defined alias override, got cfg=%+v err=%v", materialized, err)
	}
	if _, _, err := MaterializeModels(cfg, ModelSelection{Preset: "p", Overrides: map[string]string{"oracle": "a/model"}}); err == nil || !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("unknown alias error = %v", err)
	}
	if _, _, err := MaterializeModels(base, ModelSelection{Overrides: map[string]string{"oracle": "a/model"}}); err == nil || !strings.Contains(err.Error(), "oracle") {
		t.Fatalf("unknown base alias error = %v", err)
	}
}

func validPresetYAML() string {
	return "model_preset: p\nmodel_presets:\n  p:\n    tester: aihub/model\n"
}

func TestRejectsNegativeOutputLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	src := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  bad:
    type: process
    argv: [bash]
    max_output_bytes: -1
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestLoadProcessProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
models:
  m:
    provider: test
    id: model
assistants:
  fake:
    type: process
    protocol: takt-assistant/v1alpha1
    argv: [fake]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Assistants["fake"].Protocol != "takt-assistant/v1alpha1" {
		t.Fatalf("protocol not loaded: %+v", cfg.Assistants["fake"])
	}
}

func TestLoadProcessProtocolV1Alpha2(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  generic-session:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [agent-session-wrapper]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Assistants["generic-session"].Protocol != "takt-assistant/v1alpha2" {
		t.Fatalf("protocol not loaded: %+v", cfg.Assistants["generic-session"])
	}
}

func TestLoadRejectsUnknownProcessProtocol(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  fake:
    type: process
    protocol: unknown/v1
    argv: [fake]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown protocol error")
	}
}

func TestLoadRejectsProtocolOnMockAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  fake:
    type: mock
    protocol: takt-assistant/v1alpha1
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected protocol on mock assistant to be rejected")
	}
}

func TestLoadPiAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  pi:
    type: pi
    binary: /usr/local/bin/pi
    args: [--offline]
    session_dir: .takt/pi-sessions
    project_trust: deny
    max_output_bytes: 1048576
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Assistants["pi"]
	if got.Type != "pi" || got.Binary != "/usr/local/bin/pi" || got.ProjectTrust != "deny" || len(got.Args) != 1 {
		t.Fatalf("unexpected Pi config: %+v", got)
	}
}

func TestLoadRejectsInvalidPiOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "invalid trust", body: "type: pi\n    project_trust: sometimes"},
		{name: "argv", body: "type: pi\n    argv: [pi]"},
		{name: "Pi fields on process", body: "type: process\n    argv: [echo]\n    binary: pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  bad:\n    " + tc.body + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadOpenCodeAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  opencode:
    type: opencode
    binary: /usr/local/bin/opencode
    args: [--fake-case, success]
    agent: build
    auto_approve: true
    max_output_bytes: 1048576
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.Assistants["opencode"]
	if got.Type != "opencode" || got.Binary != "/usr/local/bin/opencode" || got.Agent != "build" || !got.AutoApprove || len(got.Args) != 2 {
		t.Fatalf("unexpected OpenCode config: %+v", got)
	}
}

func TestLoadRejectsInvalidOpenCodeOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "argv", body: "type: opencode\n    argv: [opencode]"},
		{name: "protocol", body: "type: opencode\n    protocol: takt-assistant/v1alpha1"},
		{name: "session dir", body: "type: opencode\n    session_dir: .sessions"},
		{name: "project trust", body: "type: opencode\n    project_trust: approve"},
		{name: "OpenCode fields on Pi", body: "type: pi\n    agent: build"},
		{name: "OpenCode fields on process", body: "type: process\n    argv: [echo]\n    auto_approve: true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  bad:\n    " + tc.body + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestLoadRejectsInvalidAssistantCapabilities(t *testing.T) {
	for _, capabilities := range []string{"[tool_policy, tool_policy]", `[tool_policy, ""]`} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		content := "apiVersion: takt/v1alpha1\nkind: Config\nassistants:\n  bad:\n    type: process\n    argv: [echo]\n    capabilities: " + capabilities + "\n"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("expected invalid capabilities %s to fail", capabilities)
		}
	}
}

func TestLoadRejectsUnsupportedBuiltinCapabilityDeclaration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
assistants:
  pi:
    type: pi
    capabilities: [mcp]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected Pi MCP capability declaration to fail")
	}
}

func TestLoadDefaultAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
default_assistant: codex
assistants:
  codex:
    type: process
    protocol: takt-assistant/v1alpha2
    argv: [codex-takt-adapter]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultAssistant != "codex" {
		t.Fatalf("default assistant = %q", cfg.DefaultAssistant)
	}
}

func TestLoadRejectsUnknownDefaultAssistant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
default_assistant: missing
assistants:
  codex:
    type: process
    argv: [codex]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown default assistant to fail")
	}
}

func TestLoadDomainAdapters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `apiVersion: takt/v1alpha1
kind: Config
adapters:
  scm:
    domain: scm
    transport: mcp
    argv: [corp-scm]
    operations:
      change.create: create_change
    reconcile_operations:
      change.create: reconcile_change
    timeout: 5s
  tracker:
    domain: tracker
    transport: process
    argv: [corp-tracker]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Adapters["scm"].Operations["change.create"]; got != "create_change" {
		t.Fatalf("operation mapping = %q", got)
	}
	if got := cfg.Adapters["tracker"].Domain; got != "tracker" {
		t.Fatalf("tracker domain = %q", got)
	}
}

func TestLoadRejectsInvalidDomainAdapter(t *testing.T) {
	for name, body := range map[string]string{
		"domain":    "domain: github\n    transport: process\n    argv: [adapter]",
		"transport": "domain: scm\n    transport: http\n    argv: [adapter]",
		"operation": "domain: scm\n    transport: mcp\n    argv: [adapter]\n    operations:\n      Change.Create: create",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "apiVersion: takt/v1alpha1\nkind: Config\nadapters:\n  bad:\n    " + body + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("expected invalid domain adapter to fail")
			}
		})
	}
}
