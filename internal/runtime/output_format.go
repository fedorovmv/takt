package runtime

import (
	"fmt"

	"takt/internal/schemasubset"
	"takt/internal/spec"
)

// validateAndNormalizeOutput remains the runtime-local compatibility seam for
// existing tests and call sites. The contract implementation lives in
// schemasubset so workflow input and output_format cannot drift apart.
func validateAndNormalizeOutput(raw string, schema *spec.OutputFormat) (string, error) {
	return schemasubset.ValidateAndNormalize(raw, schema)
}

// ValidateWorkflowInput verifies and canonicalizes one workflow input using
// takt-schema-subset/v1. Text and Markdown inputs are returned unchanged.
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
	normalized, err := schemasubset.ValidateAndNormalize(raw, contract.Schema)
	if err != nil {
		return "", fmt.Errorf("workflow input: %w", err)
	}
	return normalized, nil
}
