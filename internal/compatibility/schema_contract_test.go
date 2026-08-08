package compatibility

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"takt/internal/spec"
)

func TestPublishedSchemasValidateCompatibilityPayloads(t *testing.T) {
	cfg := &spec.Config{Models: map[string]spec.ModelSpec{}, Assistants: map[string]spec.AssistantSpec{}, Adapters: map[string]spec.DomainAdapterSpec{}, TaskSources: map[string]spec.TaskSourceSpec{}}
	cases := []struct {
		name, schema string
		value        any
	}{
		{"matrix", "compatibility-matrix.schema.json", CurrentMatrix()},
		{"fields", "v1beta1-field-matrix.schema.json", CurrentFieldMatrix()},
		{"schema", "schema-subset-description.schema.json", CurrentMatrix().SchemaSubset},
		{"check", "compatibility-check.schema.json", Check(context.Background(), cfg, CheckOptions{})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schema := readSchemaForTest(t, tc.schema)
			value := jsonValueForTest(t, tc.value)
			if err := validateSchemaForTest(schema, schema, value, "$", "#"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func readSchemaForTest(t *testing.T, name string) map[string]any {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "schemas", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonValueForTest(t *testing.T, value any) any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// validateSchemaForTest is deliberately small and test-only. It implements the
// Draft 2020-12 keywords used by the published compatibility payload schemas so
// schema drift is caught without a network or third-party validator.
func validateSchemaForTest(root, schema map[string]any, value any, path, schemaPath string) error {
	if ref, ok := schema["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/") {
			return fmt.Errorf("%s: external $ref %q is not supported", schemaPath, ref)
		}
		resolved, err := resolveRefForTest(root, ref)
		if err != nil {
			return err
		}
		return validateSchemaForTest(root, resolved, value, path, ref)
	}
	if c, ok := schema["const"]; ok && !reflect.DeepEqual(c, value) {
		return fmt.Errorf("%s: value at %s does not match const", schemaPath, path)
	}
	if enum, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range enum {
			if reflect.DeepEqual(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s: value at %s is outside enum", schemaPath, path)
		}
	}
	if rawType, ok := schema["type"]; ok && !matchesTypeForTest(rawType, value) {
		return fmt.Errorf("%s: value at %s has wrong type (%T), want %v", schemaPath, path, value, rawType)
	}
	if text, ok := value.(string); ok {
		if min, ok := numberIntForTest(schema["minLength"]); ok && len([]rune(text)) < min {
			return fmt.Errorf("%s: %s shorter than minLength", schemaPath, path)
		}
	}
	if array, ok := value.([]any); ok {
		if min, ok := numberIntForTest(schema["minItems"]); ok && len(array) < min {
			return fmt.Errorf("%s: %s shorter than minItems", schemaPath, path)
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, item := range array {
				raw, _ := json.Marshal(item)
				key := string(raw)
				if seen[key] {
					return fmt.Errorf("%s: duplicate item at %s", schemaPath, path)
				}
				seen[key] = true
			}
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range array {
				if err := validateSchemaForTest(root, itemSchema, item, fmt.Sprintf("%s[%d]", path, i), schemaPath+"/items"); err != nil {
					return err
				}
			}
		}
	}
	if object, ok := value.(map[string]any); ok {
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				name, _ := raw.(string)
				if _, exists := object[name]; !exists {
					return fmt.Errorf("%s: %s.%s is required", schemaPath, path, name)
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		if closed, ok := schema["additionalProperties"].(bool); ok && !closed {
			for name := range object {
				if _, exists := props[name]; !exists {
					return fmt.Errorf("%s: %s.%s is not allowed", schemaPath, path, name)
				}
			}
		}
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, name := range keys {
			raw, exists := object[name]
			if !exists {
				continue
			}
			child, ok := props[name].(map[string]any)
			if !ok {
				continue
			}
			if err := validateSchemaForTest(root, child, raw, path+"."+name, schemaPath+"/properties/"+name); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveRefForTest(root map[string]any, ref string) (map[string]any, error) {
	var current any = root
	for _, token := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid local $ref %q", ref)
		}
		current, ok = object[token]
		if !ok {
			return nil, fmt.Errorf("unknown local $ref %q", ref)
		}
	}
	out, ok := current.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("local $ref %q does not reference an object", ref)
	}
	return out, nil
}

func matchesTypeForTest(raw any, value any) bool {
	var types []string
	switch v := raw.(type) {
	case string:
		types = []string{v}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				types = append(types, s)
			}
		}
	}
	for _, typ := range types {
		switch typ {
		case "object":
			if _, ok := value.(map[string]any); ok {
				return true
			}
		case "array":
			if _, ok := value.([]any); ok {
				return true
			}
		case "string":
			if _, ok := value.(string); ok {
				return true
			}
		case "boolean":
			if _, ok := value.(bool); ok {
				return true
			}
		case "number":
			if _, ok := value.(json.Number); ok {
				return true
			}
		case "integer":
			if n, ok := value.(json.Number); ok && !strings.ContainsAny(n.String(), ".eE") {
				return true
			}
		case "null":
			if value == nil {
				return true
			}
		}
	}
	return len(types) == 0
}

func numberIntForTest(raw any) (int, bool) {
	switch v := raw.(type) {
	case json.Number:
		n, e := v.Int64()
		return int(n), e == nil
	case float64:
		return int(v), v == float64(int(v))
	default:
		return 0, false
	}
}
