package compatibility

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"takt/internal/assistant"
	assistantproviders "takt/internal/extensions/assistants"

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
		{"check", "compatibility-check.schema.json", Check(context.Background(), cfg, CheckOptions{Providers: assistant.MustRegistry(assistantproviders.Registrations()...)})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compilePublishedSchemaForTest(t, tc.schema)
			value := jsonValueForTest(t, tc.value)
			if err := compiled.Validate(value); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchemaSubsetMetaSchemaMatchesDescription(t *testing.T) {
	schema := readSchemaObjectForTest(t, "schema-subset-v1.schema.json")
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

func TestCompatibilitySchemasUseDraft2020NumericEquality(t *testing.T) {
	compiled := compileInlineSchemaForTest(t, `{"type":"array","uniqueItems":true}`)
	value, err := jsonschema.UnmarshalJSON(bytes.NewBufferString(`[1,1.0]`))
	if err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(value); err == nil {
		t.Fatal("JSON Schema uniqueItems must treat 1 and 1.0 as equal")
	}
}

func compilePublishedSchemaForTest(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "schemas", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return compileSchemaBytesForTest(t, raw, "takt://schemas/"+name)
}

func compileInlineSchemaForTest(t *testing.T, raw string) *jsonschema.Schema {
	t.Helper()
	return compileSchemaBytesForTest(t, []byte(raw), "takt://tests/schema.json")
}

func compileSchemaBytesForTest(t *testing.T, raw []byte, resource string) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(resource, doc); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func readSchemaObjectForTest(t *testing.T, name string) map[string]any {
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
	out, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
