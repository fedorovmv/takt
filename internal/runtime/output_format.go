package runtime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

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
		if len(object) < schema.MinProperties {
			return fmt.Errorf("%s must contain at least %d properties", path, schema.MinProperties)
		}
		if schema.MaxProperties > 0 && len(object) > schema.MaxProperties {
			return fmt.Errorf("%s must contain at most %d properties", path, schema.MaxProperties)
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
		if len(array) < schema.MinItems {
			return fmt.Errorf("%s must contain at least %d items", path, schema.MinItems)
		}
		if schema.MaxItems > 0 && len(array) > schema.MaxItems {
			return fmt.Errorf("%s must contain at most %d items", path, schema.MaxItems)
		}
		if schema.UniqueItems {
			seen := map[string]int{}
			for i, child := range array {
				encoded, err := json.Marshal(child)
				if err != nil {
					return fmt.Errorf("%s[%d] cannot be compared for uniqueness: %w", path, i, err)
				}
				key := string(encoded)
				if previous, exists := seen[key]; exists {
					return fmt.Errorf("%s[%d] duplicates %s[%d]", path, i, path, previous)
				}
				seen[key] = i
			}
		}
		if schema.Items != nil {
			for i, child := range array {
				if err := validateOutputValue(child, *schema.Items, fmt.Sprintf("%s[%d]", path, i), true); err != nil {
					return err
				}
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		length := utf8.RuneCountInString(text)
		if length < schema.MinLength {
			return fmt.Errorf("%s must contain at least %d characters", path, schema.MinLength)
		}
		if schema.MaxLength > 0 && length > schema.MaxLength {
			return fmt.Errorf("%s must contain at most %d characters", path, schema.MaxLength)
		}
		if schema.Pattern != "" {
			matched, err := regexp.MatchString(schema.Pattern, text)
			if err != nil {
				return fmt.Errorf("%s has invalid schema pattern: %w", path, err)
			}
			if !matched {
				return fmt.Errorf("%s does not match pattern %q", path, schema.Pattern)
			}
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		if err := validateNumberRange(number, schema, path); err != nil {
			return err
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok || !jsonNumberIsInteger(number.String()) {
			return fmt.Errorf("%s must be an integer", path)
		}
		if err := validateNumberRange(number, schema, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s has unsupported schema type %q", path, typeName)
	}
	return nil
}

func validateNumberRange(number json.Number, schema spec.OutputFormat, path string) error {
	if schema.Minimum == nil && schema.Maximum == nil {
		return nil
	}
	value, err := number.Float64()
	if err != nil {
		return fmt.Errorf("%s must be a finite JSON number: %w", path, err)
	}
	if schema.Minimum != nil && value < *schema.Minimum {
		return fmt.Errorf("%s must be >= %v", path, *schema.Minimum)
	}
	if schema.Maximum != nil && value > *schema.Maximum {
		return fmt.Errorf("%s must be <= %v", path, *schema.Maximum)
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

// ValidateWorkflowInput verifies and canonicalizes one workflow input using the
// same strict JSON subset as output_format. Text and Markdown inputs are
// returned unchanged.
func ValidateWorkflowInput(raw string, contract *spec.InputContract) (string, error) {
	if contract == nil || contract.Format == "" || contract.Format == "text" || contract.Format == "markdown" {
		return raw, nil
	}
	if contract.Format != "json" {
		return "", fmt.Errorf("unsupported workflow input format %q", contract.Format)
	}
	if contract.Schema == nil {
		return "", fmt.Errorf("JSON workflow input requires schema")
	}
	normalized, err := validateAndNormalizeOutput(raw, contract.Schema)
	if err != nil {
		return "", fmt.Errorf("workflow input: %w", err)
	}
	return normalized, nil
}
