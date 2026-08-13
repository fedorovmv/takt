package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

func TestFlowEvidenceRedactsSCMAndStructuredJSON(t *testing.T) {
	root, source := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "receipt.txt"), []byte("quote\"secret"), 0644); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret(`quote"secret`)
	item := FlowEvidence{
		CaseID: "case", Repeat: 1, SCMDir: source,
		States: []*store.RunState{{ID: "run", Error: `quote"secret`}},
	}
	if err := WriteFlowEvidence(root, item, r); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "cases", "case", "repeat-001", "run.json"),
		filepath.Join(root, "cases", "case", "repeat-001", "scm", "receipt.txt"),
	} {
		data, err := os.ReadFile(path)
		if err != nil || strings.Contains(string(data), `quote"secret`) {
			t.Fatalf("%s: data=%q err=%v", path, data, err)
		}
		if strings.HasSuffix(path, ".json") && !strings.Contains(string(data), `\u003credacted\u003e`) {
			t.Fatalf("json was not redacted: %s", data)
		}
		if strings.HasSuffix(path, ".txt") && !strings.Contains(string(data), "<redacted>") {
			t.Fatalf("text was not redacted: %s", data)
		}
	}
}

func TestFlowEvidenceAllowsAbsentSCMState(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(t.TempDir(), "not-created")
	err := WriteFlowEvidence(root, FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: missing}, &redact.Redactor{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cases", "case", "repeat-001", "scm")); !os.IsNotExist(err) {
		t.Fatalf("scm evidence unexpectedly exists: %v", err)
	}
}

func TestFlowEvidenceRejectsUnsafeSCMAndArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		setup      func(t *testing.T, dir string)
		item       func(dir string) FlowEvidence
	}{
		{"scm symlink", "link", func(t *testing.T, dir string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence { return FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: dir} }},
		{"artifact symlink", "link", func(t *testing.T, dir string) {
			if err := os.Symlink(t.TempDir(), filepath.Join(dir, "link")); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence {
			return FlowEvidence{CaseID: "case", Repeat: 1, ArtifactDirs: map[string]string{"run": dir}}
		}},
		{"artifact fifo", "pipe", func(t *testing.T, dir string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0600); err != nil {
				t.Skip(err)
			}
		}, func(dir string) FlowEvidence {
			return FlowEvidence{CaseID: "case", Repeat: 1, ArtifactDirs: map[string]string{"run": dir}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if err := WriteFlowEvidence(t.TempDir(), tc.item(dir), &redact.Redactor{}); err == nil {
				t.Fatal("unsafe input accepted")
			}
		})
	}
}

func TestFlowEvidenceRejectsBinarySecretInSCM(t *testing.T) {
	scm := t.TempDir()
	if err := os.WriteFile(filepath.Join(scm, "receipt.bin"), append([]byte{0}, []byte("known-secret")...), 0600); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	err := WriteFlowEvidence(t.TempDir(), FlowEvidence{CaseID: "case", Repeat: 1, SCMDir: scm}, r)
	if err == nil || !strings.Contains(err.Error(), "known secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestFlowEvidenceRejectsArtifactProvenanceMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "artifact.txt"), []byte("actual"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []store.ArtifactRef{
		{Path: "artifact.txt", MIME: "text/plain", SHA256: strings.Repeat("0", 64), Size: 6},
		{Path: "artifact.txt", MIME: "text/plain", SHA256: evidenceSHA256Hex([]byte("actual")), Size: 7},
	} {
		err := WriteFlowEvidence(t.TempDir(), FlowEvidence{CaseID: "case", Repeat: 1, Artifacts: []store.ArtifactRef{artifact}, ArtifactDirs: map[string]string{"run": dir}}, &redact.Redactor{})
		if err == nil || !strings.Contains(err.Error(), "provenance mismatch") {
			t.Fatalf("err=%v", err)
		}
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
	suite := filepath.Join(root, "suite")
	caseRoot := filepath.Join(root, "cases", "case")
	invocation := filepath.Join(root, "invocation")
	for _, path := range []string{suite, caseRoot, invocation} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name    string
		paths   FlowCleanupPaths
		wantErr bool
	}{
		{"keep", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Keep: true}, false},
		{"paused", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}, Paused: true}, false},
		{"root", FlowCleanupPaths{ControlWorkspace: root, Created: []string{root}}, true},
		{"outside", FlowCleanupPaths{ControlWorkspace: outside, Created: []string{outside}}, true},
		{"suite root", FlowCleanupPaths{ControlWorkspace: suite, Created: []string{suite}}, true},
		{"case root", FlowCleanupPaths{ControlWorkspace: caseRoot, Created: []string{caseRoot}}, true},
		{"invocation root", FlowCleanupPaths{ControlWorkspace: invocation, Created: []string{invocation}}, true},
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

func TestCleanupFlowRepeatRejectsNonCanonicalRepeatDirectory(t *testing.T) {
	root := t.TempDir()
	for _, repeat := range []string{"repeat-x", "repeat-1", "repeat-0000"} {
		target := filepath.Join(root, "workspaces", "case", repeat, "control")
		if err := os.MkdirAll(target, 0755); err != nil {
			t.Fatal(err)
		}
		if err := CleanupFlowRepeat(root, FlowCleanupPaths{ControlWorkspace: target, Created: []string{target}}); err == nil {
			t.Fatalf("accepted non-canonical repeat path %q", repeat)
		}
	}
}

func evidenceSHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
