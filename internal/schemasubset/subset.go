package schemasubset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"takt/internal/spec"
)

const Version = "takt-schema-subset/v1"

type Contract struct {
	Version                   string   `json:"version"`
	Types                     []string `json:"types"`
	Keywords                  []string `json:"keywords"`
	EnumTypes                 []string `json:"enum_types"`
	AdditionalPropertiesModes []string `json:"additional_properties_modes"`
	PatternDialect            string   `json:"pattern_dialect"`
	UnsupportedKeywords       []string `json:"unsupported_keywords"`
	Notes                     []string `json:"notes,omitempty"`
}

func Description() Contract {
	return Contract{
		Version:                   Version,
		Types:                     []string{"object", "array", "string", "number", "integer", "boolean"},
		Keywords:                  []string{"type", "description", "properties", "required", "enum", "items", "minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum", "maximum", "minProperties", "maxProperties", "additionalProperties"},
		EnumTypes:                 []string{"string"},
		AdditionalPropertiesModes: []string{"true", "false"},
		PatternDialect:            "Go RE2-compatible regexp",
		UnsupportedKeywords:       []string{"$ref", "$defs", "allOf", "anyOf", "oneOf", "not", "const", "default", "examples", "format", "if", "then", "else", "dependentRequired", "dependentSchemas", "patternProperties", "additionalItems", "prefixItems", "contains", "minContains", "maxContains", "multipleOf", "exclusiveMinimum", "exclusiveMaximum"},
		Notes: []string{
			"The subset is used identically for workflow JSON input schemas and node output_format.",
			"enum is intentionally restricted to strings.",
			"additionalProperties accepts only a boolean; schema-valued additionalProperties is not supported.",
			"A successful value is canonicalized as compact JSON after validation.",
		},
	}
}

func ValidateDefinition(format spec.OutputFormat, path string) error {
	if err := rejectDuplicateStrings(format.Required, path+".required"); err != nil {
		return err
	}
	if err := rejectDuplicateStrings(format.Enum, path+".enum"); err != nil {
		return err
	}
	switch format.Type {
	case "object":
		if format.MinProperties < 0 || format.MaxProperties < 0 {
			return fmt.Errorf("%s minProperties/maxProperties must not be negative", path)
		}
		if format.MaxProperties > 0 && format.MinProperties > format.MaxProperties {
			return fmt.Errorf("%s minProperties must not exceed maxProperties", path)
		}
		for _, name := range format.Required {
			if _, ok := format.Properties[name]; !ok {
				return fmt.Errorf("%s requires unknown property %q", path, name)
			}
		}
		for name, child := range format.Properties {
			if err := ValidateDefinition(child, path+".properties."+name); err != nil {
				return err
			}
		}
	case "array":
		if format.Items == nil {
			return fmt.Errorf("%s array requires items", path)
		}
		if format.MinItems < 0 || format.MaxItems < 0 {
			return fmt.Errorf("%s minItems/maxItems must not be negative", path)
		}
		if format.MaxItems > 0 && format.MinItems > format.MaxItems {
			return fmt.Errorf("%s minItems must not exceed maxItems", path)
		}
		if err := ValidateDefinition(*format.Items, path+".items"); err != nil {
			return err
		}
	case "string":
		if format.MinLength < 0 || format.MaxLength < 0 {
			return fmt.Errorf("%s minLength/maxLength must not be negative", path)
		}
		if format.MaxLength > 0 && format.MinLength > format.MaxLength {
			return fmt.Errorf("%s minLength must not exceed maxLength", path)
		}
		if format.Pattern != "" {
			if _, err := regexp.Compile(format.Pattern); err != nil {
				return fmt.Errorf("%s pattern is invalid: %w", path, err)
			}
		}
	case "number", "integer":
		if format.Minimum != nil && format.Maximum != nil && *format.Minimum > *format.Maximum {
			return fmt.Errorf("%s minimum must not exceed maximum", path)
		}
	case "boolean":
	default:
		return fmt.Errorf("%s has unsupported type %q", path, format.Type)
	}
	if format.Type != "array" && (format.MinItems != 0 || format.MaxItems != 0 || format.UniqueItems || format.Items != nil) {
		return fmt.Errorf("%s array constraints require type array", path)
	}
	if format.Type != "string" && (format.MinLength != 0 || format.MaxLength != 0 || format.Pattern != "") {
		return fmt.Errorf("%s string constraints require type string", path)
	}
	if format.Type != "number" && format.Type != "integer" && (format.Minimum != nil || format.Maximum != nil) {
		return fmt.Errorf("%s numeric constraints require type number or integer", path)
	}
	if format.Type != "object" && (format.MinProperties != 0 || format.MaxProperties != 0 || len(format.Properties) > 0 || len(format.Required) > 0 || format.AdditionalProperties != nil) {
		return fmt.Errorf("%s object constraints require type object", path)
	}
	if len(format.Enum) > 0 && format.Type != "string" {
		return fmt.Errorf("%s enum is supported only for string", path)
	}
	return nil
}

func rejectDuplicateStrings(values []string, path string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s contains duplicate %q", path, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func ValidateAndNormalize(raw string, schema *spec.OutputFormat) (string, error) {
	if schema == nil {
		return raw, nil
	}
	if err := ValidateDefinition(*schema, "$schema"); err != nil {
		return "", err
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
	if err := validateValue(value, *schema, "$", true); err != nil {
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

func uniquenessKey(value any) (string, error) {
	var b strings.Builder
	if err := writeUniquenessKey(&b, value); err != nil {
		return "", err
	}
	return b.String(), nil
}

func writeUniquenessKey(b *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		b.WriteString("null;")
	case bool:
		b.WriteString("bool:")
		b.WriteString(strconv.FormatBool(v))
		b.WriteByte(';')
	case string:
		b.WriteString("str:")
		encoded, _ := json.Marshal(v)
		b.Write(encoded)
		b.WriteByte(';')
	case json.Number:
		rat, ok := new(big.Rat).SetString(v.String())
		if !ok {
			return fmt.Errorf("invalid JSON number %q", v.String())
		}
		b.WriteString("num:")
		b.WriteString(rat.RatString())
		b.WriteByte(';')
	case float64:
		rat, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
		if !ok {
			return fmt.Errorf("invalid number %v", v)
		}
		b.WriteString("num:")
		b.WriteString(rat.RatString())
		b.WriteByte(';')
	case []any:
		b.WriteString("array[")
		for _, item := range v {
			if err := writeUniquenessKey(b, item); err != nil {
				return err
			}
		}
		b.WriteString("];")
	case map[string]any:
		b.WriteString("object{")
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			encoded, _ := json.Marshal(key)
			b.Write(encoded)
			b.WriteByte(':')
			if err := writeUniquenessKey(b, v[key]); err != nil {
				return err
			}
		}
		b.WriteString("};")
	default:
		return fmt.Errorf("unsupported JSON value type %T", value)
	}
	return nil
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

func validateValue(value any, schema spec.OutputFormat, path string, requireType bool) error {
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
			if err := validateValue(child, propertySchema, path+"."+name, true); err != nil {
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
				key, err := uniquenessKey(child)
				if err != nil {
					return fmt.Errorf("%s[%d] cannot be compared for uniqueness: %w", path, i, err)
				}
				if previous, exists := seen[key]; exists {
					return fmt.Errorf("%s[%d] duplicates %s[%d]", path, i, path, previous)
				}
				seen[key] = i
			}
		}
		if schema.Items != nil {
			for i, child := range array {
				if err := validateValue(child, *schema.Items, fmt.Sprintf("%s[%d]", path, i), true); err != nil {
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
