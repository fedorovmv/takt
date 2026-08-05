package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"takt/internal/spec"
)

func validateAndNormalizeOutput(raw string, schema *spec.OutputFormat) (string, error) {
	if schema == nil {
		return raw, nil
	}
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("output is not valid JSON: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return "", err
	}
	if err := validateOutputValue(value, *schema, "$", true); err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("output contains invalid trailing JSON: %w", err)
	}
	return fmt.Errorf("output contains more than one JSON value")
}

func validateOutputValue(value any, schema spec.OutputFormat, path string, requireType bool) error {
	typeName := strings.TrimSpace(schema.Type)
	if requireType && typeName == "" {
		return fmt.Errorf("%s schema type is required", path)
	}
	if len(schema.Enum) > 0 {
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string from enum", path)
		}
		matched := false
		for _, candidate := range schema.Enum {
			if text == candidate {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s value %q is not in enum %v", path, text, schema.Enum)
		}
	}
	switch typeName {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, name := range schema.Required {
			if _, exists := object[name]; !exists {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		for name, child := range object {
			propertySchema, exists := schema.Properties[name]
			if !exists {
				if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			if err := validateOutputValue(child, propertySchema, path+"."+name, true); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if schema.Items != nil {
			for i, child := range array {
				if err := validateOutputValue(child, *schema.Items, fmt.Sprintf("%s[%d]", path, i), true); err != nil {
					return err
				}
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "number":
		if _, ok := value.(json.Number); !ok {
			return fmt.Errorf("%s must be a number", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || !jsonNumberIsInteger(number.String()) {
			return fmt.Errorf("%s must be an integer", path)
		}
	default:
		return fmt.Errorf("%s has unsupported schema type %q", path, typeName)
	}
	return nil
}

func jsonNumberIsInteger(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if value[0] == '-' {
		value = value[1:]
	}
	mantissa, exponentText := value, ""
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		mantissa, exponentText = value[:index], value[index+1:]
	}
	exponent := 0
	if exponentText != "" {
		sign := 1
		if exponentText[0] == '+' || exponentText[0] == '-' {
			if exponentText[0] == '-' {
				sign = -1
			}
			exponentText = exponentText[1:]
		}
		if exponentText == "" {
			return false
		}
		for _, digit := range exponentText {
			if digit < '0' || digit > '9' {
				return false
			}
			if exponent < 1_000_000 {
				exponent = exponent*10 + int(digit-'0')
			}
		}
		exponent *= sign
	}
	fractionDigits := 0
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		fractionDigits = len(mantissa) - dot - 1
		mantissa = mantissa[:dot] + mantissa[dot+1:]
	}
	allZero := true
	for _, digit := range mantissa {
		if digit < '0' || digit > '9' {
			return false
		}
		if digit != '0' {
			allZero = false
		}
	}
	if allZero {
		return true
	}
	scale := fractionDigits - exponent
	if scale <= 0 {
		return true
	}
	if scale > len(mantissa) {
		return false
	}
	for _, digit := range mantissa[len(mantissa)-scale:] {
		if digit != '0' {
			return false
		}
	}
	return true
}
