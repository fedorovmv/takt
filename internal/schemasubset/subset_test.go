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

func contains(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}
