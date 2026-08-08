package schemasubset

import (
	"reflect"
	"strings"
	"testing"

	"takt/internal/spec"
)

func TestDescriptionIsStable(t *testing.T) {
	got := Description()
	if got.Version != Version {
		t.Fatalf("version=%q", got.Version)
	}
	wantKeywords := []string{"type", "description", "properties", "required", "enum", "items", "minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum", "maximum", "minProperties", "maxProperties", "additionalProperties"}
	if !reflect.DeepEqual(got.Keywords, wantKeywords) {
		t.Fatalf("keywords=%v", got.Keywords)
	}
	for _, unsupported := range []string{"oneOf", "$ref", "default"} {
		if !contains(got.UnsupportedKeywords, unsupported) {
			t.Fatalf("missing unsupported keyword %q", unsupported)
		}
	}
}

func TestValidateDefinitionRejectsNonSubsetSemantics(t *testing.T) {
	format := spec.OutputFormat{Type: "number", Enum: []string{"1"}}
	if err := ValidateDefinition(format, "output_format"); err == nil || !strings.Contains(err.Error(), "enum is supported only for string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAndNormalizeUsesSameContract(t *testing.T) {
	closed := false
	schema := &spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"n": {Type: "integer"}}, Required: []string{"n"}, AdditionalProperties: &closed}
	got, err := ValidateAndNormalize(" { \"n\" : 1e0 } ", schema)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"n":1e0}` {
		t.Fatalf("normalized=%q", got)
	}
	if _, err := ValidateAndNormalize(`{"n":1,"extra":true}`, schema); err == nil {
		t.Fatal("expected closed-object error")
	}
}

func TestValidateDefinitionCoversSubsetKeywords(t *testing.T) {
	closed := false
	min := 2.0
	max := 5.0
	cases := []struct {
		name   string
		schema spec.OutputFormat
		valid  string
		bad    string
	}{
		{name: "string-length", schema: spec.OutputFormat{Type: "string", MinLength: 2, MaxLength: 4}, valid: `"abcd"`, bad: `"a"`},
		{name: "pattern", schema: spec.OutputFormat{Type: "string", Pattern: `^[a-z]+$`}, valid: `"abc"`, bad: `"A1"`},
		{name: "number-range", schema: spec.OutputFormat{Type: "number", Minimum: &min, Maximum: &max}, valid: `3.5`, bad: `7`},
		{name: "object-cardinality", schema: spec.OutputFormat{Type: "object", Properties: map[string]spec.OutputFormat{"a": {Type: "string"}, "b": {Type: "string"}}, MinProperties: 1, MaxProperties: 1, AdditionalProperties: &closed}, valid: `{"a":"x"}`, bad: `{"a":"x","b":"y"}`},
		{name: "array-max", schema: spec.OutputFormat{Type: "array", MinItems: 1, MaxItems: 2, Items: &spec.OutputFormat{Type: "string"}}, valid: `["a","b"]`, bad: `["a","b","c"]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateDefinition(tc.schema, "$schema"); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateAndNormalize(tc.valid, &tc.schema); err != nil {
				t.Fatalf("valid value: %v", err)
			}
			if _, err := ValidateAndNormalize(tc.bad, &tc.schema); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestValidateDefinitionRejectsCrossTypeConstraintsAndInvalidDefinitions(t *testing.T) {
	min := 1.0
	cases := []spec.OutputFormat{
		{Type: "string", MinItems: 1},
		{Type: "array", MinLength: 1, Items: &spec.OutputFormat{Type: "string"}},
		{Type: "boolean", Minimum: &min},
		{Type: "string", Properties: map[string]spec.OutputFormat{"x": {Type: "string"}}},
		{Type: "array"},
		{Type: "string", Pattern: "["},
	}
	for i, schema := range cases {
		if err := ValidateDefinition(schema, "$schema"); err == nil {
			t.Fatalf("case %d should fail", i)
		}
	}
}

func TestUniqueItemsUsesJSONNumericEquality(t *testing.T) {
	schema := &spec.OutputFormat{Type: "array", UniqueItems: true, Items: &spec.OutputFormat{Type: "number"}}
	for _, raw := range []string{`[1,1.0]`, `[1e0,1]`, `[0.10,0.1]`} {
		if _, err := ValidateAndNormalize(raw, schema); err == nil || !strings.Contains(err.Error(), "duplicates") {
			t.Fatalf("%s should contain duplicate numeric values, err=%v", raw, err)
		}
	}
	if _, err := ValidateAndNormalize(`[1,1.01]`, schema); err != nil {
		t.Fatalf("distinct numbers rejected: %v", err)
	}
}

func TestValidateDefinitionRejectsDuplicateRequiredAndEnum(t *testing.T) {
	for _, schema := range []spec.OutputFormat{
		{Type: "object", Properties: map[string]spec.OutputFormat{"x": {Type: "string"}}, Required: []string{"x", "x"}},
		{Type: "string", Enum: []string{"a", "a"}},
	} {
		if err := ValidateDefinition(schema, "$schema"); err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
