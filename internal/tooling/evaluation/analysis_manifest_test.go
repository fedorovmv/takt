package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/redact"
)

func TestBuildAnalysisEvidenceManifestUsesRepeatRelativePaths(t *testing.T) {
	root := t.TempDir()
	repeat := filepath.Join(root, "cases", "x", "repeat-001")
	if err := os.MkdirAll(repeat, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repeat, "run.json"), []byte(`{"states":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	item := &InspectionCase{CaseID: "x", Repeat: 1, Evidence: InspectionEvidence{Root: "cases/x/repeat-001", Run: "cases/x/repeat-001/run.json"}}
	m, err := buildAnalysisEvidenceManifest(root, repeat, item, RunRecord{CaseID: "x", Repeat: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 1 || m.Files[0].Path != "run.json" || m.Files[0].SHA256 == "" {
		t.Fatalf("manifest=%+v", m)
	}
}

func TestCopyAnalysisEvidenceRootRedactsAndBoundsFiles(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "validator.stderr"), []byte("known-secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "too-large.bin"), make([]byte, maxAnalysisEvidenceFileBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	missing, err := copyAnalysisEvidenceRoot(source, destination, r)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(destination, "validator.stderr"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "known-secret") {
		t.Fatal("secret leaked into analysis evidence")
	}
	if len(missing) != 1 || missing[0] != "too-large.bin" {
		t.Fatalf("missing=%v", missing)
	}
	if _, err := os.Stat(filepath.Join(destination, "too-large.bin")); !os.IsNotExist(err) {
		t.Fatal("oversized file copied")
	}
}

func TestBuildAnalysisEvidenceManifestRejectsSymlinkedEvidence(t *testing.T) {
	root := t.TempDir()
	repeat := filepath.Join(root, "cases", "x", "repeat-001")
	if err := os.MkdirAll(repeat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside.json"), filepath.Join(repeat, "validation-result.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := buildAnalysisEvidenceManifest(root, repeat, &InspectionCase{CaseID: "x", Repeat: 1, Evidence: InspectionEvidence{Root: "cases/x/repeat-001", Run: "cases/x/repeat-001/run.json", Validation: "cases/x/repeat-001/validation-result.json"}}, RunRecord{CaseID: "x", Repeat: 1})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error=%v", err)
	}
}
