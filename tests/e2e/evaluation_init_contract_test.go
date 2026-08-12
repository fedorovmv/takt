package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlowEvaluationInitContract(t *testing.T) {
	output := filepath.Join(t.TempDir(), "flow")
	resultObject(t, takt(t, nil, "eval", "flow", "init", "code:feature-development", "--output", output, "--json").RequireSuccess(t).JSON(t))
	for _, path := range []string{"suite.yaml", "cases/example/input.md", "cases/example/expected.yaml", "cases/example/workspace/README.md"} {
		if _, err := os.Stat(filepath.Join(output, path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "validator")); !os.IsNotExist(err) {
		t.Fatalf("validator exists: %v", err)
	}
	takt(t, nil, "eval", "flow", "init", "code:feature-development", "--output", output, "--json").RequireFailure(t).Contains(t, "flow evaluation output already exists")
}
