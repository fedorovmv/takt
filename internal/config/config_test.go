package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
