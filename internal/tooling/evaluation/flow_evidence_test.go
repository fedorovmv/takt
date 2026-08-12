package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/redact"
	"takt/internal/store"
	"takt/internal/validation"
)

func TestFlowEvidenceWritesRedactedAtomicRecordsAndArtifacts(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "run-a")
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	text := []byte("token=known-secret\n")
	if err := os.WriteFile(filepath.Join(artifacts, "summary.md"), text, 0640); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(text)
	item := FlowEvidence{
		CaseID: "case-a", Repeat: 1,
		States:     []*store.RunState{{ID: "run-a", Status: store.RunCompleted, Error: "known-secret", Artifacts: []store.ArtifactRef{{ID: "summary", MIME: "text/markdown", Path: "summary.md", SHA256: hex.EncodeToString(h[:]), Size: int64(len(text)), ProducerRunID: "run-a"}}}},
		Request:    FlowValidationRequest{Workspace: "/local/known-secret", Run: FlowValidationRun{ID: "run-a", Status: "completed"}},
		Validation: FlowValidationExecution{Status: "completed", Stderr: []byte("known-secret"), Result: &validation.Result{Valid: true}},
		Diff:       []byte("known-secret"), Artifacts: []store.ArtifactRef{{ID: "summary", MIME: "text/markdown", Path: "summary.md", SHA256: hex.EncodeToString(h[:]), Size: int64(len(text)), ProducerRunID: "run-a"}},
		ArtifactDirs: map[string]string{"run-a": artifacts},
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	if err := WriteFlowEvidence(root, item, r); err != nil {
		t.Fatal(err)
	}
	repeat := filepath.Join(root, "cases", "case-a", "repeat-001")
	for _, name := range []string{"run.json", "validation-request.json", "validation-result.json", "validator.stderr", "diff.patch", "artifacts/manifest.json", "artifacts/files/run-a/summary.md"} {
		data, err := os.ReadFile(filepath.Join(repeat, name))
		if err != nil {
			t.Fatal(name, err)
		}
		if strings.Contains(string(data), "known-secret") {
			t.Fatalf("%s leaked secret: %s", name, data)
		}
	}
	for _, name := range []string{"run.json", "validation-request.json", "validation-result.json", "artifacts/manifest.json"} {
		data, _ := os.ReadFile(filepath.Join(repeat, name))
		if !json.Valid(data) {
			t.Fatalf("invalid json: %s", name)
		}
		if _, err := os.Stat(filepath.Join(repeat, name+".tmp")); !os.IsNotExist(err) {
			t.Fatalf("temporary file remained: %s", name)
		}
	}
	manifest, err := os.ReadFile(filepath.Join(repeat, "artifacts/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"registered": true`) || !strings.Contains(string(manifest), `"redacted": true`) || !strings.Contains(string(manifest), `"source_path": "run-a/summary.md"`) {
		t.Fatalf("manifest=%s", manifest)
	}
	persisted, err := os.ReadFile(filepath.Join(repeat, "artifacts/files/run-a/summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	persistedHash := sha256.Sum256(persisted)
	if !strings.Contains(string(manifest), hex.EncodeToString(persistedHash[:])) {
		t.Fatalf("manifest missing persisted hash: %s", manifest)
	}
}

func TestFlowEvidenceRejectsBinarySecretBeforeCopy(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(t.TempDir(), "run-a")
	if err := os.MkdirAll(artifacts, 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte{0, 'k', 'n', 'o', 'w', 'n', '-', 's', 'e', 'c', 'r', 'e', 't'}
	if err := os.WriteFile(filepath.Join(artifacts, "secret.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, States: []*store.RunState{{ID: "run-a"}}, ArtifactDirs: map[string]string{"run-a": artifacts}}, r)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "cases", "case", "repeat-001", "artifacts", "files", "run-a", "secret.bin")); !os.IsNotExist(err) {
		t.Fatal("binary artifact was persisted")
	}
}

func TestCleanupFlowRepeatRequiresContainedExactCreatedTargets(t *testing.T) {
	root := t.TempDir()
	control := filepath.Join(root, "workspaces", "case", "repeat-001", "control")
	baseline := filepath.Join(root, "workspaces", "case", "repeat-001", "baseline")
	if err := os.MkdirAll(control, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(baseline, 0755); err != nil {
		t.Fatal(err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: control, BaselineWorkspace: baseline, Created: []string{control}}); err == nil {
		t.Fatal("uncreated baseline was removed")
	}
	if _, err := os.Stat(control); err != nil {
		t.Fatal(err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: control, Created: []string{control}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(control); !os.IsNotExist(err) {
		t.Fatal("control not removed")
	}
}

func TestCleanupFlowRepeatPreservesKeepPausedAndRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	for _, tc := range []struct {
		name    string
		paths   FlowCleanupPaths
		wantErr bool
	}{
		{"keep", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Keep: true}, false},
		{"paused", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Paused: true}, false},
		{"root", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}}, true},
		{"outside", FlowCleanupPaths{ControlWorkspace: outside, Created: []string{outside}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CleanupFlowRepeat(root, tc.paths); (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
		})
	}
	escape := filepath.Join(root, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: escape, Created: []string{escape}}); err == nil {
		t.Fatal("symlink escape accepted")
	}
}
