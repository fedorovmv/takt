package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"takt/internal/workflow"
	"takt/internal/yamlcodec"
)

func TestArchonFlowLanguageInventory(t *testing.T) {
	t.Parallel()
	roots := []string{
		filepath.Join(repoRoot, "internal", "profile", "builtin"),
		filepath.Join(repoRoot, "internal", "workflow", "testdata"),
		filepath.Join(repoRoot, "examples"),
		filepath.Join(repoRoot, "skills", "takt", "assets"),
		// tests/e2e keeps most fixtures as Go string literals; there are no YAML
		// files there to inventory, and those contracts remain covered by Go tests.
		filepath.Join(repoRoot, "tests", "e2e"),
	}
	type definition struct {
		path     string
		document map[string]any
	}
	var definitions []definition
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || (filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml") {
				return err
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			var value any
			if err := yamlcodec.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("decode %s: %w", path, err)
			}
			document, ok := value.(map[string]any)
			if !ok {
				return nil
			}
			if _, hasNodes := document["nodes"].([]any); !hasNodes {
				return nil
			}
			kind, hasKind := document["kind"].(string)
			if hasKind && kind != "Workflow" {
				return nil
			}
			if !hasKind {
				if _, target := document["name"].(string); !target {
					return nil
				}
			}
			definitions = append(definitions, definition{path: filepath.Clean(path), document: document})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(definitions) == 0 {
		t.Fatal("workflow inventory is empty")
	}
	referenced := map[string]bool{}
	for _, item := range definitions {
		collectArchonSubworkflowPaths(item.document, filepath.Dir(item.path), referenced)
	}
	for _, item := range definitions {
		if !referenced[item.path] {
			if _, err := workflow.Load(item.path); err != nil {
				t.Errorf("Load(%s): %v", item.path, err)
			}
		}
		for _, field := range []string{"apiVersion", "kind", "metadata", "defaults"} {
			if _, legacy := item.document[field]; legacy {
				t.Errorf("%s still uses legacy root field %q", item.path, field)
			}
		}
	}
}

func collectArchonSubworkflowPaths(value any, base string, paths map[string]bool) {
	switch value := value.(type) {
	case map[string]any:
		if subworkflow, ok := value["subworkflow"].(map[string]any); ok {
			if path, ok := subworkflow["path"].(string); ok {
				paths[filepath.Clean(filepath.Join(base, filepath.FromSlash(path)))] = true
			}
		}
		for _, child := range value {
			collectArchonSubworkflowPaths(child, base, paths)
		}
	case []any:
		for _, child := range value {
			collectArchonSubworkflowPaths(child, base, paths)
		}
	}
}
