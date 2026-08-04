package validation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const ProtocolV1Alpha1 = "takt-validation/v1alpha1"

type Result struct {
	ProtocolVersion string                     `json:"protocol_version"`
	Type            string                     `json:"type"`
	Valid           bool                       `json:"valid"`
	Score           *float64                   `json:"score,omitempty"`
	Checks          map[string]Check           `json:"checks,omitempty"`
	Diagnostics     []Diagnostic               `json:"diagnostics,omitempty"`
	Metadata        map[string]json.RawMessage `json:"metadata,omitempty"`
}

type Check struct {
	Passed  *bool    `json:"passed,omitempty"`
	Score   *float64 `json:"score,omitempty"`
	Weight  *float64 `json:"weight,omitempty"`
	Message string   `json:"message,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message,omitempty"`
}

type wireResult struct {
	ProtocolVersion string                     `json:"protocol_version"`
	Type            string                     `json:"type"`
	Valid           *bool                      `json:"valid"`
	Score           *float64                   `json:"score,omitempty"`
	Checks          map[string]Check           `json:"checks,omitempty"`
	Diagnostics     []Diagnostic               `json:"diagnostics,omitempty"`
	Metadata        map[string]json.RawMessage `json:"metadata,omitempty"`
}

func Decode(data []byte) (*Result, error) {
	if err := rejectExplicitNulls(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var wire wireResult
	if err := dec.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode validation result: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode validation result: multiple JSON values")
		}
		return nil, fmt.Errorf("decode validation result trailing data: %w", err)
	}
	if wire.ProtocolVersion != ProtocolV1Alpha1 {
		return nil, fmt.Errorf("unsupported validation protocol_version %q", wire.ProtocolVersion)
	}
	if wire.Type != "validation_result" {
		return nil, fmt.Errorf("validation result type must be %q", "validation_result")
	}
	if wire.Valid == nil {
		return nil, fmt.Errorf("validation result requires valid")
	}
	if wire.Score != nil && (*wire.Score < 0 || *wire.Score > 100) {
		return nil, fmt.Errorf("validation score must be between 0 and 100")
	}
	for name, check := range wire.Checks {
		if name == "" {
			return nil, fmt.Errorf("validation check name cannot be empty")
		}
		if check.Score != nil && (*check.Score < 0 || *check.Score > 100) {
			return nil, fmt.Errorf("validation check %q score must be between 0 and 100", name)
		}
		if check.Weight != nil && *check.Weight < 0 {
			return nil, fmt.Errorf("validation check %q weight cannot be negative", name)
		}
	}
	for i, diagnostic := range wire.Diagnostics {
		if diagnostic.Code == "" {
			return nil, fmt.Errorf("validation diagnostic %d requires code", i)
		}
		switch diagnostic.Severity {
		case "info", "warning", "error":
		default:
			return nil, fmt.Errorf("validation diagnostic %q has unsupported severity %q", diagnostic.Code, diagnostic.Severity)
		}
	}
	return &Result{
		ProtocolVersion: wire.ProtocolVersion,
		Type:            wire.Type,
		Valid:           *wire.Valid,
		Score:           wire.Score,
		Checks:          wire.Checks,
		Diagnostics:     wire.Diagnostics,
		Metadata:        wire.Metadata,
	}, nil
}

// rejectExplicitNulls keeps the Go decoder aligned with the JSON Schema. Fields
// that are optional may be absent, but when present they must have their declared
// JSON type rather than null. Metadata values remain unconstrained and may be null.
func rejectExplicitNulls(data []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil // the strict decoder below returns the authoritative syntax error
	}
	for _, name := range []string{"protocol_version", "type", "valid", "score", "checks", "diagnostics", "metadata"} {
		if raw, ok := top[name]; ok && isNull(raw) {
			return fmt.Errorf("validation result field %q cannot be null", name)
		}
	}
	if raw, ok := top["checks"]; ok {
		var checks map[string]json.RawMessage
		if err := json.Unmarshal(raw, &checks); err == nil {
			for name, checkRaw := range checks {
				if isNull(checkRaw) {
					return fmt.Errorf("validation check %q cannot be null", name)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(checkRaw, &fields); err == nil {
					for field, value := range fields {
						if isNull(value) {
							return fmt.Errorf("validation check %q field %q cannot be null", name, field)
						}
					}
				}
			}
		}
	}
	if raw, ok := top["diagnostics"]; ok {
		var diagnostics []json.RawMessage
		if err := json.Unmarshal(raw, &diagnostics); err == nil {
			for i, diagnosticRaw := range diagnostics {
				if isNull(diagnosticRaw) {
					return fmt.Errorf("validation diagnostic %d cannot be null", i)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(diagnosticRaw, &fields); err == nil {
					for field, value := range fields {
						if isNull(value) {
							return fmt.Errorf("validation diagnostic %d field %q cannot be null", i, field)
						}
					}
				}
			}
		}
	}
	return nil
}

func isNull(value json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}
