package schemasubset

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

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
			"Runtime validation is delegated to a Draft 2020-12 JSON Schema implementation.",
			"A successful value is canonicalized as compact JSON after validation.",
		},
	}
}

// ValidateDefinition enforces Takt's intentionally small authoring contract.
// JSON Schema execution itself is delegated to the upstream Draft 2020-12
// validator; this function only keeps product-specific subset rules stable.
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

	value, err := decodeJSONValue(raw)
	if err != nil {
		return "", err
	}
	compiled, err := compile(schema)
	if err != nil {
		return "", err
	}
	if err := compiled.Validate(value); err != nil {
		return "", fmt.Errorf("JSON Schema validation failed: %w", err)
	}
	return encodeCanonical(value)
}

func compile(schema *spec.OutputFormat) (*jsonschema.Schema, error) {
	rawSchema, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("encode JSON Schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(rawSchema))
	if err != nil {
		return nil, fmt.Errorf("decode JSON Schema: %w", err)
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	const resource = "takt://schema/subset.json"
	if err := compiler.AddResource(resource, doc); err != nil {
		return nil, fmt.Errorf("register JSON Schema: %w", err)
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("compile JSON Schema: %w", err)
	}
	return compiled, nil
}

func decodeJSONValue(raw string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, fmt.Errorf("output is not valid JSON: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return nil, err
	}
	return value, nil
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

func encodeCanonical(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}
