package evaluation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPrepareEvaluationInputOrdersCasesAndPinsPreparedIdentity(t *testing.T) {
	root := t.TempDir()
	root, err := canonicalPath(root)
	if err != nil {
		t.Fatal(err)
	}
	workflowPath := filepath.Join(root, "evaluate.yaml")
	configPath := filepath.Join(root, "config.yaml")
	writeEvaluationInputFile(t, workflowPath, "name: evaluate\ninput: {format: json}\nnodes:\n  - id: done\n    bash: 'true'\n")
	writeEvaluationInputFile(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\n")
	for _, id := range []string{"b", "a"} {
		caseRoot := filepath.Join(root, "cases", id)
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "input.md"), "implement "+id)
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "expected.yaml"), "oracle: {expected: true}\n")
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "workspace", "target.yaml"), "name: target\nnodes:\n  - id: done\n    bash: 'true'\n")
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "workspace", "main.txt"), id)
	}

	minimum := 1.0
	prepared, err := PrepareEvaluationInput(context.Background(), EvaluationInputOptions{
		WorkflowPath: workflowPath,
		Target:       "target.yaml",
		ConfigPath:   configPath,
		CasesDir:     filepath.Join(root, "cases"),
		OutputDir:    filepath.Join(root, ".takt", "evals", "input-test"),
		Workspace:    root,
		Repeat:       2,
		Gates:        map[string]EvaluationGate{"valid_rate": {Min: &minimum}},
		Now:          func() time.Time { return time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC) },
		HostPATH:     "host-path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ConfigPath == "" || !filepath.IsAbs(prepared.ConfigPath) || len(prepared.JSON) == 0 {
		t.Fatalf("prepared=%+v", prepared)
	}
	if got, want := evaluationCaseOrder(prepared.Input.Cases), []string{"a#1", "a#2", "b#1", "b#2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order=%v want=%v", got, want)
	}
	for _, item := range prepared.Input.Cases {
		if !filepath.IsAbs(item.InputPath) || !filepath.IsAbs(item.ExpectedPath) || !filepath.IsAbs(item.WorkflowPath) || !filepath.IsAbs(item.BaselinePath) {
			t.Fatalf("paths are not absolute: %+v", item)
		}
		repository := filepath.Join(root, filepath.FromSlash(item.Repository))
		if filepath.IsAbs(item.Repository) || !pathInside(root, repository) || !pathInside(repository, item.WorkflowPath) {
			t.Fatalf("repository escaped workspace: %+v", item)
		}
		if item.CaseFingerprint == "" || item.PreparedFingerprint == "" || item.Input == "" {
			t.Fatalf("identity is incomplete: %+v", item)
		}
	}
	if prepared.Input.ProtocolVersion != EvaluationInputProtocol || prepared.Input.Type != EvaluationInputType || prepared.Input.Identity.Fingerprint == "" || prepared.Input.Identity.DatasetFingerprint == "" || prepared.Input.Identity.ConfigFingerprint == "" || prepared.Input.Identity.WorkflowFingerprint == "" {
		t.Fatalf("input identity=%+v", prepared.Input)
	}
	decoded, err := DecodeEvaluationInput(prepared.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Cases) != 4 || decoded.Gates["valid_rate"].Min == nil || *decoded.Gates["valid_rate"].Min != 1 {
		t.Fatalf("decoded=%+v", decoded)
	}
	decoded.Identity.Fingerprint = "short"
	malformed, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEvaluationInput(malformed); err == nil {
		t.Fatal("invalid identity fingerprint was accepted")
	}
	decoded.Identity.Fingerprint = prepared.Input.Identity.Fingerprint
	decoded.Cases[0].Repository = "../outside"
	malformed, err = json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeEvaluationInput(malformed); err == nil {
		t.Fatal("escaping repository was accepted")
	}
	second, err := PrepareEvaluationInput(context.Background(), EvaluationInputOptions{
		WorkflowPath: workflowPath, Target: "target.yaml", ConfigPath: configPath, CasesDir: filepath.Join(root, "cases"),
		OutputDir: filepath.Join(root, ".takt", "evals", "input-test-2"), Workspace: root, Repeat: 2,
		Gates: map[string]EvaluationGate{"valid_rate": {Min: &minimum}}, Now: func() time.Time { return time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC) }, HostPATH: "host-path",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Input.Identity.Fingerprint != prepared.Input.Identity.Fingerprint {
		t.Fatalf("identity depends on output path: %s != %s", second.Input.Identity.Fingerprint, prepared.Input.Identity.Fingerprint)
	}
}

func TestDecodeEvaluationInputRejectsUnknownFields(t *testing.T) {
	_, err := DecodeEvaluationInput([]byte(`{"protocol_version":"takt-evaluation-input/v1alpha1","type":"evaluation_input","cases":[],"gates":{},"identity":{"fingerprint":"x","workflow_fingerprint":"x","config_fingerprint":"x","dataset_fingerprint":"x"},"extra":true}`))
	if err == nil {
		t.Fatal("unknown field was accepted")
	}
}

func TestPrepareEvaluationInputRejectsMixedSCMFixtures(t *testing.T) {
	root := t.TempDir()
	workflow := filepath.Join(root, "evaluate.yaml")
	config := filepath.Join(root, "config.yaml")
	writeEvaluationInputFile(t, workflow, "name: evaluate\ninput: {format: json, schema: {type: object}}\nnodes:\n  - id: done\n    bash: 'true'\n")
	writeEvaluationInputFile(t, config, "apiVersion: takt/v1alpha1\nkind: Config\n")
	for _, id := range []string{"a", "b"} {
		caseRoot := filepath.Join(root, "cases", id)
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "input.md"), id)
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "expected.yaml"), "oracle: {}\n")
		writeEvaluationInputFile(t, filepath.Join(caseRoot, "workspace", "target.yaml"), "name: target\nnodes:\n  - id: done\n    bash: 'true'\n")
	}
	writeEvaluationInputFile(t, filepath.Join(root, "cases", "b", "scm", "repository.yaml"), "repository: owner/repo\nbase_branch: main\nhead_branch: change\n")
	_, err := PrepareEvaluationInput(context.Background(), EvaluationInputOptions{WorkflowPath: workflow, Target: "target.yaml", ConfigPath: config, CasesDir: filepath.Join(root, "cases"), OutputDir: filepath.Join(root, ".takt", "evals", "mixed"), Workspace: root, Repeat: 1, HostPATH: "host"})
	if err == nil || !strings.Contains(err.Error(), "inconsistent SCM") {
		t.Fatalf("error=%v", err)
	}
}

func evaluationCaseOrder(items []EvaluationCaseInput) []string {
	result := make([]string, len(items))
	for index, item := range items {
		result[index] = item.CaseID + "#" + string(rune('0'+item.Repeat))
	}
	return result
}

func writeEvaluationInputFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
