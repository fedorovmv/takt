package compatibility

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"takt/internal/schemasubset"
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

func TestSchemaSubsetMetaSchemaMatchesDescription(t *testing.T) {
	schema := readSchemaForTest(t, "schema-subset-v1.schema.json")
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("meta-schema $defs missing")
	}
	definition, ok := defs["schema"].(map[string]any)
	if !ok {
		t.Fatal("meta-schema $defs.schema missing")
	}
	props, ok := definition["properties"].(map[string]any)
	if !ok {
		t.Fatal("meta-schema properties missing")
	}
	gotKeywords := make([]string, 0, len(props))
	for name := range props {
		gotKeywords = append(gotKeywords, name)
	}
	sort.Strings(gotKeywords)
	wantKeywords := append([]string(nil), schemasubset.Description().Keywords...)
	sort.Strings(wantKeywords)
	if !reflect.DeepEqual(gotKeywords, wantKeywords) {
		t.Fatalf("meta-schema keywords=%v Description keywords=%v", gotKeywords, wantKeywords)
	}
	typeSchema, ok := props["type"].(map[string]any)
	if !ok {
		t.Fatal("meta-schema type definition missing")
	}
	rawEnum, ok := typeSchema["enum"].([]any)
	if !ok {
		t.Fatal("meta-schema type enum missing")
	}
	gotTypes := make([]string, 0, len(rawEnum))
	for _, raw := range rawEnum {
		gotTypes = append(gotTypes, fmt.Sprint(raw))
	}
	sort.Strings(gotTypes)
	wantTypes := append([]string(nil), schemasubset.Description().Types...)
	sort.Strings(wantTypes)
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("meta-schema types=%v Description types=%v", gotTypes, wantTypes)
	}
}

func TestCompatibilitySchemaValidatorRejectsUnknownKeywordsAndUsesJSONNumericEquality(t *testing.T) {
	root := map[string]any{"type": "string", "pattern": "^ok$"}
	if err := validateSchemaForTest(root, root, "bad", "$", "#"); err == nil {
		t.Fatal("pattern was silently ignored")
	}
	unknown := map[string]any{"type": "string", "madeUpKeyword": true}
	if err := validateSchemaForTest(unknown, unknown, "x", "$", "#"); err == nil {
		t.Fatal("unknown schema keyword was silently ignored")
	}
	unique := map[string]any{"type": "array", "uniqueItems": true}
	value := []any{json.Number("1"), json.Number("1.0")}
	if err := validateSchemaForTest(unique, unique, value, "$", "#"); err == nil {
		t.Fatal("1 and 1.0 must be equal for JSON Schema uniqueItems")
	}
}

func readSchemaForTest(t *testing.T, name string) map[string]any {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "schemas", name)
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
	if err := rejectUnknownSchemaKeywordsForTest(schema, schemaPath); err != nil {
		return err
	}
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
		if max, ok := numberIntForTest(schema["maxLength"]); ok && len([]rune(text)) > max {
			return fmt.Errorf("%s: %s longer than maxLength", schemaPath, path)
		}
		if pattern, ok := schema["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("%s: invalid pattern: %w", schemaPath, err)
			}
			if !re.MatchString(text) {
				return fmt.Errorf("%s: %s does not match pattern", schemaPath, path)
			}
		}
	}
	if number, ok := value.(json.Number); ok {
		if minimum, ok := numberRatForTest(schema["minimum"]); ok {
			actual, _ := new(big.Rat).SetString(number.String())
			if actual == nil || actual.Cmp(minimum) < 0 {
				return fmt.Errorf("%s: %s is below minimum", schemaPath, path)
			}
		}
		if maximum, ok := numberRatForTest(schema["maximum"]); ok {
			actual, _ := new(big.Rat).SetString(number.String())
			if actual == nil || actual.Cmp(maximum) > 0 {
				return fmt.Errorf("%s: %s is above maximum", schemaPath, path)
			}
		}
	}
	if array, ok := value.([]any); ok {
		if min, ok := numberIntForTest(schema["minItems"]); ok && len(array) < min {
			return fmt.Errorf("%s: %s shorter than minItems", schemaPath, path)
		}
		if max, ok := numberIntForTest(schema["maxItems"]); ok && len(array) > max {
			return fmt.Errorf("%s: %s longer than maxItems", schemaPath, path)
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, item := range array {
				key, err := semanticJSONKeyForTest(item)
				if err != nil {
					return err
				}
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
		if min, ok := numberIntForTest(schema["minProperties"]); ok && len(object) < min {
			return fmt.Errorf("%s: %s has fewer than minProperties", schemaPath, path)
		}
		if max, ok := numberIntForTest(schema["maxProperties"]); ok && len(object) > max {
			return fmt.Errorf("%s: %s has more than maxProperties", schemaPath, path)
		}
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

func rejectUnknownSchemaKeywordsForTest(schema map[string]any, path string) error {
	allowed := map[string]bool{
		"$schema": true, "$id": true, "$defs": true, "$ref": true,
		"title": true, "description": true,
		"type": true, "const": true, "enum": true,
		"properties": true, "required": true, "additionalProperties": true,
		"items": true, "minItems": true, "maxItems": true, "uniqueItems": true,
		"minLength": true, "maxLength": true, "pattern": true,
		"minimum": true, "maximum": true, "minProperties": true, "maxProperties": true,
	}
	for key := range schema {
		if !allowed[key] {
			return fmt.Errorf("%s: unsupported test schema keyword %q", path, key)
		}
	}
	return nil
}

func semanticJSONKeyForTest(value any) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case bool:
		return "bool:" + strconv.FormatBool(v), nil
	case string:
		raw, _ := json.Marshal(v)
		return "str:" + string(raw), nil
	case json.Number:
		rat, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return "", fmt.Errorf("invalid JSON number %q", v.String())
		}
		return "num:" + rat.RatString(), nil
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			key, err := semanticJSONKeyForTest(item)
			if err != nil {
				return "", err
			}
			parts[i] = key
		}
		return "arr:[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			child, err := semanticJSONKeyForTest(v[key])
			if err != nil {
				return "", err
			}
			parts = append(parts, strconv.Quote(key)+":"+child)
		}
		return "obj:{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("unsupported JSON value type %T", value)
	}
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

func numberRatForTest(raw any) (*big.Rat, bool) {
	switch v := raw.(type) {
	case json.Number:
		n, ok := new(big.Rat).SetString(v.String())
		return n, ok
	case float64:
		n, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
		return n, ok
	default:
		return nil, false
	}
}
