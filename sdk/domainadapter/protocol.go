// Package domainadapter defines the public, provider-neutral contract used by
// Takt SCM, tracker and CI adapters. External adapter projects may depend on
// this package without importing Takt runtime internals.
package domainadapter

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const ProtocolV1Alpha1 = "takt-domain-adapter/v1alpha1"

const (
	DomainSCM     = "scm"
	DomainTracker = "tracker"
	DomainCI      = "ci"
)

// Core provider-neutral operations. Adapters may expose additional
// lowercase dot-separated capabilities; workflows should prefer these core
// names when the operation maps to the common SCM/tracker/CI contract.
const (
	SCMRepositoryGet = "repository.get"
	SCMChangeGet     = "change.get"
	SCMChangeCreate  = "change.create"
	SCMChangeComment = "change.comment"
	SCMChangeReview  = "change.review"
	SCMChecksGet     = "checks.get"

	TrackerItemGet        = "item.get"
	TrackerItemSearch     = "item.search"
	TrackerItemComment    = "item.comment"
	TrackerItemTransition = "item.transition"
	TrackerItemCreate     = "item.create"

	CIRunStart     = "run.start"
	CIRunGet       = "run.get"
	CIRunCancel    = "run.cancel"
	CILogsGet      = "logs.get"
	CIArtifactsGet = "artifacts.get"
)

type Declaration struct {
	APIVersion   string   `json:"apiVersion"`
	Kind         string   `json:"kind"`
	Domain       string   `json:"domain"`
	Capabilities []string `json:"capabilities"`
	Reconcile    []string `json:"reconcile,omitempty"`
}

type InvokeRequest struct {
	RunID          string          `json:"run_id"`
	NodeID         string          `json:"node_id"`
	Attempt        int             `json:"attempt"`
	Domain         string          `json:"domain"`
	Operation      string          `json:"operation"`
	Input          json.RawMessage `json:"input,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	SideEffectMode string          `json:"side_effect_mode,omitempty"`
}

type Result struct {
	Status    string          `json:"status"` // completed | failed | unknown
	Output    json.RawMessage `json:"output,omitempty"`
	Receipt   string          `json:"receipt,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type ReconcileRequest struct {
	RunID          string          `json:"run_id"`
	NodeID         string          `json:"node_id"`
	Domain         string          `json:"domain"`
	Operation      string          `json:"operation"`
	Input          json.RawMessage `json:"input,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Receipt        string          `json:"receipt,omitempty"`
}

type ReconcileResult struct {
	Outcome   string          `json:"outcome"` // applied | not_applied | unknown
	Output    json.RawMessage `json:"output,omitempty"`
	Receipt   string          `json:"receipt,omitempty"`
	ErrorCode string          `json:"error_code,omitempty"`
	Error     string          `json:"error,omitempty"`
}

func NormalizeDeclaration(value Declaration) Declaration {
	if value.APIVersion == "" {
		value.APIVersion = ProtocolV1Alpha1
	}
	if value.Kind == "" {
		value.Kind = "AdapterCapabilities"
	}
	value.Domain = strings.ToLower(strings.TrimSpace(value.Domain))
	value.Capabilities = uniqueSorted(value.Capabilities)
	value.Reconcile = uniqueSorted(value.Reconcile)
	return value
}

func ValidateDeclaration(value Declaration) error {
	value = NormalizeDeclaration(value)
	if value.APIVersion != ProtocolV1Alpha1 || value.Kind != "AdapterCapabilities" {
		return fmt.Errorf("domain adapter capabilities must use %s AdapterCapabilities", ProtocolV1Alpha1)
	}
	switch value.Domain {
	case DomainSCM, DomainTracker, DomainCI:
	default:
		return fmt.Errorf("unsupported domain %q", value.Domain)
	}
	if len(value.Capabilities) == 0 {
		return fmt.Errorf("domain adapter %s declares no capabilities", value.Domain)
	}
	available := map[string]bool{}
	for _, capability := range value.Capabilities {
		if err := ValidateOperation(capability); err != nil {
			return fmt.Errorf("capability %q: %w", capability, err)
		}
		available[capability] = true
	}
	for _, operation := range value.Reconcile {
		if !available[operation] {
			return fmt.Errorf("reconcile capability %q is not declared as an operation", operation)
		}
	}
	return nil
}

func ValidateOperation(operation string) error {
	operation = strings.TrimSpace(operation)
	if operation == "" {
		return fmt.Errorf("operation is required")
	}
	parts := strings.Split(operation, ".")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("operation contains an empty segment")
		}
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
				return fmt.Errorf("operation must use lowercase letters, digits, '_' or '-' segments")
			}
		}
	}
	return nil
}

func ValidateResult(value Result) error {
	switch value.Status {
	case "completed":
		if value.Error != "" || value.ErrorCode != "" {
			return fmt.Errorf("completed domain result cannot contain error")
		}
	case "failed":
		if strings.TrimSpace(value.Error) == "" {
			return fmt.Errorf("failed domain result requires error")
		}
	case "unknown":
	default:
		return fmt.Errorf("domain result status must be completed, failed, or unknown")
	}
	return nil
}

func ValidateReconcileResult(value ReconcileResult) error {
	switch value.Outcome {
	case "applied", "not_applied", "unknown":
	default:
		return fmt.Errorf("reconcile outcome must be applied, not_applied, or unknown")
	}
	if value.Outcome == "applied" && value.Receipt == "" {
		return fmt.Errorf("applied reconcile result requires receipt")
	}
	return nil
}

func CoreOperations(domain string) []string {
	var values []string
	switch domain {
	case DomainSCM:
		values = []string{SCMRepositoryGet, SCMChangeGet, SCMChangeCreate, SCMChangeComment, SCMChangeReview, SCMChecksGet}
	case DomainTracker:
		values = []string{TrackerItemGet, TrackerItemSearch, TrackerItemComment, TrackerItemTransition, TrackerItemCreate}
	case DomainCI:
		values = []string{CIRunStart, CIRunGet, CIRunCancel, CILogsGet, CIArtifactsGet}
	}
	return append([]string(nil), values...)
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}
