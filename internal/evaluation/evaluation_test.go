package evaluation

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"takt/internal/store"
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

func TestRunRejectsCaseIDCollisionsBeforeCreatingOutput(t *testing.T) {
	root := t.TempDir()
	casesDir := filepath.Join(root, "cases")
	templateDir := filepath.Join(root, "template")
	outputDir := filepath.Join(root, "output")
	for _, dir := range []string{casesDir, templateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(casesDir, "a b.md"), "first", 0o644)
	mustWrite(t, filepath.Join(casesDir, "a+b.md"), "second", 0o644)

	_, err := Run(context.Background(), RunOptions{
		WorkflowPath: filepath.Join(root, "workflow.yaml"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
		CasesDir:     casesDir, WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), `case id collision "a-b"`) {
		t.Fatalf("expected normalized case id collision, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("output directory was created despite preflight failure: %v", statErr)
	}
}

func TestRunRejectsOverlappingWorkspaceTemplateAndOutput(t *testing.T) {
	root := t.TempDir()
	templateDir := filepath.Join(root, "template")
	casesDir := filepath.Join(root, "cases")
	for _, dir := range []string{templateDir, casesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)
	outputDir := filepath.Join(templateDir, "results")

	_, err := Run(context.Background(), RunOptions{
		WorkflowPath: filepath.Join(root, "workflow.yaml"),
		ConfigPath:   filepath.Join(root, "config.yaml"),
		CasesDir:     casesDir, WorkspaceTemplate: templateDir, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("expected workspace/output overlap error, got %v", err)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("nested output directory was created despite preflight failure: %v", statErr)
	}
}

func TestRecordFromStatePreservesResumeAndDiagnostics(t *testing.T) {
	state := &store.RunState{
		ID: "run-1", Status: store.RunFailed,
		CreatedAt: time.Unix(100, 0), UpdatedAt: time.Unix(102, 0),
		Nodes: map[string]*store.NodeState{
			"validate": {
				Status: store.NodeFailed, Attempts: 2, SessionID: "session-1", Resumed: true,
				ExitCode: 7, ErrorCode: "exit", Error: "validator failed",
				Feedback: "ROUTE_INVALID", Output: `{"valid":false}`,
			},
		},
	}
	record := recordFromState("case", 1, "/workspace", state)
	node := record.Nodes["validate"]
	if record.Resumed != 1 || !node.Resumed || node.Feedback != "ROUTE_INVALID" || node.Error != "validator failed" || node.DiagnosticOutput != `{"valid":false}` {
		t.Fatalf("diagnostic fields were not preserved: record=%+v node=%+v", record, node)
	}
}

func TestRunRejectsOutputNestedThroughWorkspaceTemplateSymlink(t *testing.T) {
	root := t.TempDir()
	realTemplate := filepath.Join(root, "real-template")
	linkedTemplate := filepath.Join(root, "template-link")
	casesDir := filepath.Join(root, "cases")
	for _, dir := range []string{realTemplate, casesDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(realTemplate, linkedTemplate); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	mustWrite(t, filepath.Join(casesDir, "case.md"), "case", 0o644)
	outputDir := filepath.Join(linkedTemplate, "results")
	_, err := Run(context.Background(), RunOptions{
		WorkflowPath: filepath.Join(root, "workflow.yaml"), ConfigPath: filepath.Join(root, "config.yaml"),
		CasesDir: casesDir, WorkspaceTemplate: linkedTemplate, OutputDir: outputDir,
	})
	if err == nil || !strings.Contains(err.Error(), "must not overlap") {
		t.Fatalf("expected canonical overlap error, got %v", err)
	}
}
