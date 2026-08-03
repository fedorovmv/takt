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
