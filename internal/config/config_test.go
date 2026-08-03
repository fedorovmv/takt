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
