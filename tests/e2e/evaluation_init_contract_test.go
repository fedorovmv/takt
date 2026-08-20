package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFlowEvaluationInitContract(t *testing.T) {
	t.Parallel()
	output := filepath.Join(t.TempDir(), "flow")
	resultObject(t, takt(t, nil, "eval", "flow", "init", "code:feature-development", "--output", output, "--json").RequireSuccess(t).JSON(t))
	for _, path := range []string{"workflows/evaluate.yaml", "README.md", "tools/validate.sh", "tools/collect-evidence.sh", "cases/example/input.md", "cases/example/expected.yaml", "cases/example/workspace/README.md"} {
		if _, err := os.Stat(filepath.Join(output, path)); err != nil {
			t.Fatalf("missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "suite.yaml")); !os.IsNotExist(err) {
		t.Fatalf("legacy suite unexpectedly exists: %v", err)
	}
	takt(t, nil, "eval", "flow", "init", "code:feature-development", "--output", output, "--json").RequireFailure(t).Contains(t, "flow evaluation output already exists")

	legacy := filepath.Join(t.TempDir(), "legacy")
	resultObject(t, takt(t, nil, "eval", "flow", "init", "code:feature-development", "--output", legacy, "--legacy", "--json").RequireSuccess(t).JSON(t))
	if _, err := os.Stat(filepath.Join(legacy, "suite.yaml")); err != nil {
		t.Fatalf("missing legacy suite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacy, "workflows", "evaluate.yaml")); !os.IsNotExist(err) {
		t.Fatalf("authored workflow unexpectedly exists in legacy scaffold: %v", err)
	}
}
