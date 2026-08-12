package evaluation

import (
	"fmt"
	"os"
	"path/filepath"
)

func InitFlowSuite(workflowSelector, output string) error {
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("flow evaluation output already exists: %s", output)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(output, "cases", "example", "workspace"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"suite.yaml":                        "version: takt-flow-evaluation/v1alpha1\nworkflow: " + workflowSelector + "\nconfig: config.yaml\ncases:\n  directory: cases\nvalidator:\n  id: replace-me\n  version: \"1\"\n  command: [./validator]\n  path: ./validator\n  timeout: 2m\n  max_output_bytes: 1048576\ngates:\n  validation_error_rate: {max: 0}\n",
		"cases/example/input.md":            "Describe the task for the selected production workflow.\n",
		"cases/example/expected.yaml":       "oracle: {}\n",
		"cases/example/workspace/README.md": "Replace this directory with the complete initial repository for this case.\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(output, name), []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}
