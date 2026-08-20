package evaluation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"takt/internal/workflow"
)

func TestFlowInitCreatesAuthoredEvaluationScaffold(t *testing.T) {
	output := filepath.Join(t.TempDir(), "flow")
	selector := " code:feature-development "
	if err := InitEvaluationWorkflow(selector, output); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.Load(filepath.Join(output, "workflows", "evaluate.yaml")); err != nil {
		t.Fatalf("authored scaffold is not a valid workflow: %v", err)
	}
	want := map[string]string{
		"cases/example/input.md":            "Describe the task for the selected production workflow.\n",
		"cases/example/expected.yaml":       "oracle: {}\n",
		"cases/example/workspace/README.md": "Replace this directory with the complete initial repository for this case.\n",
	}
	evaluationWorkflow, err := os.ReadFile(filepath.Join(output, "workflows", "evaluate.yaml"))
	if err != nil || string(evaluationWorkflow[:len("name: evaluation\n")]) != "name: evaluation\n" {
		t.Fatalf("evaluation workflow = %q err=%v", evaluationWorkflow, err)
	}
	workflowSource := string(evaluationWorkflow)
	for _, want := range []string{
		`"case_id": "$MATRIX.item.case_id"`,
		`"repeat": $MATRIX.item.repeat`,
		`"workspace": "$candidate.child_execution_workspace"`,
		`"baseline_workspace": "$MATRIX.item.baseline_path"`,
		`"expected_path": "$MATRIX.item.expected_path"`,
		`"run_id": "$candidate.child_run_id"`,
		`"run_status": "$candidate.status"`,
		"- --workspace\n              - $candidate.child_execution_workspace",
		"- --base\n              - $candidate.child_base_commit",
	} {
		if !strings.Contains(workflowSource, want) {
			t.Errorf("evaluation workflow missing %q", want)
		}
	}
	for path, content := range want {
		data, err := os.ReadFile(filepath.Join(output, path))
		if err != nil || string(data) != content {
			t.Fatalf("%s = %q err=%v", path, data, err)
		}
	}
	for _, name := range []string{"validate.sh", "collect-evidence.sh"} {
		data, err := os.ReadFile(filepath.Join(output, "tools", name))
		if err != nil || len(data) < len("#!/bin/sh") || string(data[:len("#!/bin/sh")]) != "#!/bin/sh" {
			t.Fatalf("tool %s = %q err=%v", name, data, err)
		}
	}
	readme, err := os.ReadFile(filepath.Join(output, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "Create or copy config.yaml") {
		t.Fatalf("README does not explain the required config: %s", readme)
	}
	for _, name := range []string{"validate.sh", "collect-evidence.sh"} {
		info, err := os.Stat(filepath.Join(output, "tools", name))
		if err != nil || info.Mode()&0o111 == 0 {
			t.Fatalf("tool %s is not executable: info=%v err=%v", name, info, err)
		}
	}
	if err := InitEvaluationWorkflow("workflow", output); err == nil {
		t.Fatal("expected existing output rejection")
	}
}

func TestFlowInitLegacyStillCreatesCompatibilitySuite(t *testing.T) {
	output := filepath.Join(t.TempDir(), "flow")
	if err := InitFlowSuite("code:feature-development", output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "suite.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(output, "workflows", "evaluate.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy scaffold unexpectedly created authored workflow: %v", err)
	}
}
