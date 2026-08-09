package yamlcodec

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// Unmarshal decodes YAML or JSON into out while preserving Takt's strict
// public-contract checks and JSON-tag semantics. YAML syntax itself is owned by
// the maintained yaml/go-yaml library; this package only adapts decoded values
// to Takt's JSON-shaped contracts and diagnostics.
func Unmarshal(data []byte, out any) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return fmt.Errorf("empty document")
	}
	if reflect.TypeOf(out) == nil || reflect.TypeOf(out).Kind() != reflect.Pointer || reflect.ValueOf(out).IsNil() {
		return fmt.Errorf("output must be a non-nil pointer")
	}

	var value any
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		dec := json.NewDecoder(strings.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			return err
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				return fmt.Errorf("decode JSON document: multiple documents")
			}
			return fmt.Errorf("decode JSON document trailing data: %w", err)
		}
	} else {
		dec := yaml.NewDecoder(strings.NewReader(string(data)))
		var document any
		if err := dec.Decode(&document); err != nil {
			return fmt.Errorf("decode YAML document: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				return fmt.Errorf("decode YAML document: multiple documents")
			}
			return fmt.Errorf("decode YAML document trailing data: %w", err)
		}
		if err := yaml.Unmarshal(data, &value); err != nil {
			return fmt.Errorf("decode YAML document: %w", err)
		}
		value = normalizeYAML(value)
	}

	target := reflect.TypeOf(out).Elem()
	if err := validateKnownFields(value, target, "$"); err != nil {
		return err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("normalize YAML document: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(encoded)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decode YAML document: %w", err)
	}
	return nil
}

func normalizeYAML(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = normalizeYAML(child)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[fmt.Sprint(key)] = normalizeYAML(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = normalizeYAML(child)
		}
		return out
	default:
		return value
	}
}
