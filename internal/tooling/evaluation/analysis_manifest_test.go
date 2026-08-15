package evaluation

import (
	"os"
	"path/filepath"
	"testing"
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
