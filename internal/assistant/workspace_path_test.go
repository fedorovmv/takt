package assistant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateToolPathRejectsExternalWorkerEscape(t *testing.T) {
	workspace := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "artifacts")
	outside := filepath.Join(t.TempDir(), "control", "main.go")
	if err := ValidateToolPath("write", json.RawMessage(`{"path":"`+outside+`"}`), workspace, artifacts); err == nil {
		t.Fatal("external worker write escape was accepted")
	}
}

func TestValidateToolPathAllowsArtifacts(t *testing.T) {
	workspace := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "artifacts")
	path := filepath.Join(artifacts, "implementation.md")
	if err := ValidateToolPath("edit", json.RawMessage(`{"path":"`+path+`"}`), workspace, artifacts); err != nil {
		t.Fatalf("artifact path rejected: %v", err)
	}
}

func TestValidateToolPathRejectsEmptyWorkspace(t *testing.T) {
	if err := ValidateToolPath("write", json.RawMessage(`{"path":"relative.txt"}`), "", ""); err == nil {
		t.Fatal("empty workspace was resolved to process cwd")
	}
}

func TestValidateToolPathRejectsDanglingSymlinkSuffix(t *testing.T) {
	workspace := t.TempDir()
	control := t.TempDir()
	if err := os.Symlink(filepath.Join(control, "missing"), filepath.Join(workspace, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	path := filepath.Join(workspace, "escape", "new.go")
	raw, err := json.Marshal(map[string]string{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateToolPath("write", raw, workspace, ""); err == nil {
		t.Fatal("dangling symlink suffix was accepted")
	}
}
