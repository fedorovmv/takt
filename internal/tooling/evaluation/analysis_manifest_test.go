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

func TestBuildAnalysisEvidenceManifestCarriesDeterministicContext(t *testing.T) {
	root := t.TempDir()
	repeat := filepath.Join(root, "cases", "x", "repeat-001")
	if err := os.MkdirAll(repeat, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"run.json":               `{"status":"failed"}`,
		"validation-result.json": `{"status":"completed"}`,
		"validator.stderr":       "validator warning\n",
	} {
		if err := os.WriteFile(filepath.Join(repeat, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inspection := &InspectionCase{
		CaseID: "x", Repeat: 1, Outcome: "false_accept",
		Cause:        InspectionCause{Source: "validator", Message: "invalid output"},
		CausalChain:  []InspectionObservation{{Code: "validator_invalid", Message: "validator rejected", Evidence: "validation-result.json#/status"}},
		Observations: []InspectionObservation{{Code: "stderr", Message: "warning", Evidence: "validator.stderr#line:1"}},
		Nodes:        []InspectionNode{{ID: "analyze", Status: "failed", ErrorCode: "exit", Error: "failed"}},
		Evidence:     InspectionEvidence{Root: "cases/x/repeat-001", Run: "cases/x/repeat-001/run.json", Validation: "cases/x/repeat-001/validation-result.json"},
	}
	report := &SuiteReport{Strategy: StrategyIdentity{Fingerprint: "strategy-fp"}, Benchmark: BenchmarkIdentity{Fingerprint: "benchmark-fp"}}
	m, err := buildAnalysisEvidenceManifest(root, repeat, inspection, RunRecord{CaseID: "x", Repeat: 1, Status: "failed", Outcome: "false_accept", RunID: "run"}, report)
	if err != nil {
		t.Fatal(err)
	}
	if m.Outcome != "false_accept" || m.StrategyFingerprint != "strategy-fp" || m.BenchmarkFingerprint != "benchmark-fp" || m.DeterministicVerdict.Outcome != "false_accept" {
		t.Fatalf("deterministic context=%+v", m)
	}
	if len(m.CausalChain) != 1 || len(m.Observations) != 1 || len(m.NonCompletedNodes) != 1 || m.ValidatorStderrPath != "validator.stderr" {
		t.Fatalf("inspection context=%+v", m)
	}
	seen := false
	for _, file := range m.Files {
		if file.Path == "validator.stderr" {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("validator stderr missing from files: %+v", m.Files)
	}
}

func TestBuildAnalysisEvidenceManifestNormalizesInspectionMissingPaths(t *testing.T) {
	root := t.TempDir()
	repeat := filepath.Join(root, "evidence")
	if err := os.MkdirAll(repeat, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"run.json":               `{"states":[]}`,
		"validation-result.json": `{"valid":false}`,
	} {
		if err := os.WriteFile(filepath.Join(repeat, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	inspection := &InspectionCase{
		CaseID: "problem", Repeat: 1,
		Evidence:        InspectionEvidence{Root: "cases/problem/repeat-001", Validation: "cases/problem/repeat-001/validation-result.json"},
		MissingEvidence: []string{"cases/problem/repeat-001/validation-result.json", "executor-manifest.json"},
	}
	manifest, err := buildAnalysisEvidenceManifest(root, repeat, inspection, RunRecord{CaseID: "problem", Repeat: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, missing := range manifest.MissingEvidence {
		if strings.HasPrefix(missing, "cases/problem/repeat-001/") || missing == "validation-result.json" {
			t.Fatalf("available copied evidence reported missing: %q (all=%v)", missing, manifest.MissingEvidence)
		}
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
	if err := os.WriteFile(filepath.Join(source, "secret.bin"), append([]byte{0}, []byte("known-secret")...), 0644); err != nil {
		t.Fatal(err)
	}
	r := &redact.Redactor{}
	r.AddSecret("known-secret")
	missing, err := copyAnalysisEvidenceRoot(source, destination, r)
	if err == nil || !strings.Contains(err.Error(), "binary evidence") {
		t.Fatalf("binary secret error=%v", err)
	}
	if err := os.Remove(filepath.Join(source, "secret.bin")); err != nil {
		t.Fatal(err)
	}
	// The fail-closed binary is checked before the bounded-file assertions.
	missing, err = copyAnalysisEvidenceRoot(source, destination, r)
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

func TestCopyAnalysisEvidenceRootBoundsPostRedactionExpansion(t *testing.T) {
	source, destination := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "expand.txt"), []byte(strings.Repeat("s", maxAnalysisEvidenceFileBytes)), 0o644); err != nil {
		t.Fatal(err)
	}
	redactor := &redact.Redactor{}
	redactor.AddSecret("s")
	missing, err := copyAnalysisEvidenceRoot(source, destination, redactor)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "expand.txt" {
		t.Fatalf("missing=%v", missing)
	}
	if _, err := os.Stat(filepath.Join(destination, "expand.txt")); !os.IsNotExist(err) {
		t.Fatalf("post-redaction oversized file was copied: %v", err)
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
