package schemacontract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestSchemaRegistryIsOfflineAndDocumented(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	schemaDir := filepath.Join(root, "schemas")
	entries, err := filepath.Glob(filepath.Join(schemaDir, "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no schemas found")
	}
	readmeBytes, err := os.ReadFile(filepath.Join(schemaDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(readmeBytes)
	actual := map[string]bool{}
	for _, path := range entries {
		name := filepath.Base(path)
		actual[name] = true
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("%s: invalid JSON: %v", name, err)
		}
		rootObject, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s: root must be object", name)
		}
		if rootObject["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
			t.Fatalf("%s: expected Draft 2020-12 $schema", name)
		}
		walkRefs(t, name, value)
		if !strings.Contains(readme, "`"+name+"`") {
			t.Fatalf("schemas/README.md missing %s", name)
		}
	}
	re := regexp.MustCompile("`([^`]+\\.schema\\.json)`")
	var stale []string
	for _, match := range re.FindAllStringSubmatch(readme, -1) {
		if !actual[match[1]] {
			stale = append(stale, match[1])
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("schemas/README.md contains stale entries: %s", strings.Join(stale, ", "))
	}
}

func walkRefs(t *testing.T, schemaName string, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$ref" {
				ref, ok := child.(string)
				if !ok {
					t.Fatalf("%s: $ref must be string", schemaName)
				}
				if !strings.HasPrefix(ref, "#") {
					t.Fatalf("%s: external/cross-file $ref is forbidden: %s", schemaName, ref)
				}
			}
			walkRefs(t, schemaName, child)
		}
	case []any:
		for _, child := range v {
			walkRefs(t, schemaName, child)
		}
	}
}
