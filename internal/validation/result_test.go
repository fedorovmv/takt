package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeValidationResult(t *testing.T) {
	score := 92.0
	result, err := Decode([]byte(`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":92,"checks":{"syntax":{"passed":true,"score":100}},"diagnostics":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid || result.Score == nil || *result.Score != score {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDecodeValidationResultRejectsInvalidContract(t *testing.T) {
	cases := []string{
		`{}`,
		`{"protocol_version":"wrong","type":"validation_result","valid":true}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"wrong","valid":true}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result"}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":null}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":null}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"checks":{"syntax":{"passed":null}}}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"diagnostics":[null]}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true,"score":101}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"diagnostics":[{"code":"X","severity":"fatal"}]}`,
		`{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":true} {}`,
	}
	for _, value := range cases {
		if _, err := Decode([]byte(value)); err == nil {
			t.Fatalf("expected error for %s", value)
		}
	}
}

func TestValidationSchemaMatchesDecoderContract(t *testing.T) {
	path := filepath.Join("..", "..", "schemas", "validation-result.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	if properties["protocol_version"].(map[string]any)["const"] != ProtocolV1Alpha1 {
		t.Fatal("validation schema protocol version differs from decoder")
	}
	if properties["type"].(map[string]any)["const"] != "validation_result" {
		t.Fatal("validation schema result type differs from decoder")
	}
	required := schema["required"].([]any)
	hasValid := false
	for _, field := range required {
		if field == "valid" {
			hasValid = true
		}
	}
	if !hasValid {
		t.Fatal("validation schema must require valid")
	}
	checks := properties["checks"].(map[string]any)
	if checks["propertyNames"].(map[string]any)["minLength"] != float64(1) {
		t.Fatal("validation schema must reject empty check names")
	}
}
