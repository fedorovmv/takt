package evaluation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesIsolatedWorkspacesAndReport(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workflow := `apiVersion: takt/v1alpha1
kind: Workflow
metadata:
  name: evaluation-test
defaults:
  assistant: fake
  model: m
nodes:
  - id: implement
    prompt: |
      Process this input:
      ${input}
`
	config := `apiVersion: takt/v1alpha1
kind: Config
models:
  m:
    provider: test
    id: model
assistants:
  fake:
    type: mock
`
	mustWrite(t, workflowPath, workflow, 0o644)
	mustWrite(t, configPath, config, 0o644)
	mustWrite(t, filepath.Join(casesDir, "a.md"), "first", 0o644)
	mustWrite(t, filepath.Join(casesDir, "b.md"), "second", 0o644)
	mustWrite(t, filepath.Join(templateDir, "marker.txt"), "template", 0o644)
	if err := os.MkdirAll(filepath.Join(templateDir, ".takt", "runs"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(templateDir, ".takt", "runs", "stale"), "stale", 0o644)

	report, err := Run(context.Background(), RunOptions{
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir, Repeat: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 4 || report.Summary.ByStatus["completed"] != 4 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
	for _, record := range report.Runs {
		if record.RunID == "" || record.Status != "completed" || record.Attempts != 1 {
			t.Fatalf("unexpected record: %+v", record)
		}
		if _, err := os.Stat(filepath.Join(record.Workspace, "marker.txt")); err != nil {
			t.Fatalf("template was not copied: %v", err)
		}
		if _, err := os.Stat(filepath.Join(record.Workspace, ".takt", "runs", "stale")); !os.IsNotExist(err) {
			t.Fatalf("template runtime state leaked into workspace: %v", err)
		}
	}
	loaded, err := LoadReport(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Summary.Total != report.Summary.Total {
		t.Fatalf("loaded report differs: %+v", loaded.Summary)
	}
}

func TestRunRejectsExistingWorkspaceWithoutReplace(t *testing.T) {
	root := t.TempDir()
	workflowPath := filepath.Join(root, "workflow.yaml")
	configPath := filepath.Join(root, "config.yaml")
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir, filepath.Join(outputDir, "workspaces", "case-001")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, workflowPath, "apiVersion: takt/v1alpha1\nkind: Workflow\nmetadata:\n  name: x\nnodes: []\n", 0o644)
	mustWrite(t, configPath, "apiVersion: takt/v1alpha1\nkind: Config\nmodels: {}\nassistants: {}\n", 0o644)
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)

	report, err := Run(context.Background(), RunOptions{
		WorkflowPath: workflowPath, ConfigPath: configPath, CasesDir: casesDir,
		WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil {
		t.Fatal("expected existing workspace error")
	}
	if report == nil || len(report.Runs) != 1 || report.Runs[0].Status != "infrastructure_error" {
		t.Fatalf("unexpected partial report: %+v", report)
	}
}

func mustWrite(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}
