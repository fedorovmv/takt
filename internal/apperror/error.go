package apperror

import (
	"errors"

	"takt/internal/authoring"
	"takt/internal/definition"
	"takt/internal/runtime"
	"takt/internal/store"
)

type Descriptor struct {
	Code      string
	Retryable bool
	Details   map[string]any
}

// Describe converts concrete domain/runtime failures into the stable local API
// error envelope. Transports should not need to know which package defined an error.
func Describe(err error) Descriptor {
	result := Descriptor{Code: "internal_error", Details: map[string]any{}}
	var runErr *runtime.RunFailedError
	if errors.As(err, &runErr) {
		result.Code = runErr.Code
		if result.Code == "" {
			result.Code = "run_failed"
		}
		result.Details["run_id"] = runErr.RunID
		result.Details["node_id"] = runErr.NodeID
	}
	var changed *definition.ChangedError
	if errors.As(err, &changed) {
		result.Code = "definition_changed"
		result.Details["definition"] = changed.Kind
	}
	var inconsistent *store.InconsistentError
	if errors.As(err, &inconsistent) {
		result.Code = "store_inconsistent"
		result.Details["run_id"] = inconsistent.RunID
	}
	var authoringErr *authoring.Error
	if errors.As(err, &authoringErr) {
		result.Code = "authoring_validation_failed"
		result.Details["diagnostics"] = authoringErr.Diagnostics
	}
	return result
}
