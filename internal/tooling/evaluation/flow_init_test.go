package evaluation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlowInitCreatesOnlySkeleton(t *testing.T) {
	output := filepath.Join(t.TempDir(), "flow")
	selector := " code:feature-development "
	if err := InitFlowSuite(selector, output); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"suite.yaml":                        "version: takt-flow-evaluation/v1alpha1\nworkflow:  code:feature-development \nconfig: config.yaml\ncases:\n  directory: cases\nvalidator:\n  id: replace-me\n  version: \"1\"\n  command: [./validator]\n  path: ./validator\n  timeout: 2m\n  max_output_bytes: 1048576\ngates:\n  validation_error_rate: {max: 0}\n",
		"cases/example/input.md":            "Describe the task for the selected production workflow.\n",
		"cases/example/expected.yaml":       "oracle: {}\n",
		"cases/example/workspace/README.md": "Replace this directory with the complete initial repository for this case.\n",
	}
	for path, content := range want {
		data, err := os.ReadFile(filepath.Join(output, path))
		if err != nil || string(data) != content {
			t.Fatalf("%s = %q err=%v", path, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "validator")); !os.IsNotExist(err) {
		t.Fatalf("validator exists: %v", err)
	}
	if err := InitFlowSuite("workflow", output); err == nil {
		t.Fatal("expected existing output rejection")
	}
}
