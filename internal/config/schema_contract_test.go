package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigSchemaRejectsProtocolForMockAssistant(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "schemas", "config.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("config schema has no $defs")
	}
	assistant, ok := defs["assistant"].(map[string]any)
	if !ok {
		t.Fatal("config schema has no assistant definition")
	}
	allOf, ok := assistant["allOf"].([]any)
	if !ok {
		t.Fatal("assistant schema has no allOf")
	}
	for _, raw := range allOf {
		rule, _ := raw.(map[string]any)
		ifPart, _ := rule["if"].(map[string]any)
		properties, _ := ifPart["properties"].(map[string]any)
		typeRule, _ := properties["type"].(map[string]any)
		if typeRule["const"] != "mock" {
			continue
		}
		thenPart, _ := rule["then"].(map[string]any)
		thenProperties, _ := thenPart["properties"].(map[string]any)
		if forbidden, exists := thenProperties["protocol"]; exists && forbidden == false {
			return
		}
	}
	t.Fatal("config schema does not forbid protocol for mock assistants")
}

func TestConfigSchemaDefinesPiAssistantContract(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "schemas", "config.schema.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	assistant := defs["assistant"].(map[string]any)
	properties := assistant["properties"].(map[string]any)
	for _, field := range []string{"binary", "args", "session_dir", "project_trust", "settings"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("Pi field %q is missing from config schema", field)
		}
	}
	allOf := assistant["allOf"].([]any)
	for _, raw := range allOf {
		rule := raw.(map[string]any)
		ifPart := rule["if"].(map[string]any)
		typeRule := ifPart["properties"].(map[string]any)["type"].(map[string]any)
		if typeRule["const"] != "pi" {
			continue
		}
		thenProperties := rule["then"].(map[string]any)["properties"].(map[string]any)
		for _, forbidden := range []string{"argv", "protocol"} {
			if value, exists := thenProperties[forbidden]; !exists || value != false {
				t.Fatalf("config schema does not forbid %s for Pi assistants", forbidden)
			}
		}
		return
	}
	t.Fatal("config schema has no Pi conditional rule")
}
