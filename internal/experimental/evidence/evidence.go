package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	FailureSpecGap             = "SPEC_GAP"
	FailureContextInsufficient = "CONTEXT_INSUFFICIENT"
	FailureImplementation      = "IMPLEMENTATION_FAILURE"
	FailureVerification        = "VERIFICATION_FAILURE"
	FailureBaseline            = "BASELINE_FAILURE"
	FailureBoundary            = "BOUNDARY_VIOLATION"
	FailureEnvironment         = "ENVIRONMENT_FAILURE"
	FailureSecurity            = "SECURITY_HALT"
	FailureBudget              = "BUDGET_EXCEEDED"
	FailureExternalUnknown     = "EXTERNAL_STATE_UNKNOWN"
	FailureOwnerDecision       = "OWNER_DECISION_REQUIRED"
)

const (
	VerdictPass    = "pass"
	VerdictFail    = "fail"
	VerdictPartial = "partial"
	VerdictStale   = "stale"
)

type Baseline struct {
	PhaseID             string    `json:"phase_id,omitempty"`
	BaseRef             string    `json:"base_ref,omitempty"`
	CandidateSHA        string    `json:"candidate_sha,omitempty"`
	PassedChecks        []string  `json:"passed_checks,omitempty"`
	KnownFailures       []string  `json:"known_failures,omitempty"`
	FailureFingerprints []string  `json:"failure_fingerprints,omitempty"`
	UnavailableChecks   []string  `json:"unavailable_checks,omitempty"`
	Evidence            []string  `json:"evidence,omitempty"`
	CapturedAt          time.Time `json:"captured_at,omitempty"`
}

type Acceptance struct {
	ID           string   `json:"id"`
	Block        string   `json:"block,omitempty"`
	Check        string   `json:"check,omitempty"`
	PhaseID      string   `json:"phase_id,omitempty"`
	Status       string   `json:"status"`
	Level        string   `json:"level,omitempty"`
	FailureCode  string   `json:"failure_code,omitempty"`
	Detail       string   `json:"detail,omitempty"`
	Evidence     []string `json:"evidence,omitempty"`
	CandidateSHA string   `json:"candidate_sha,omitempty"`
}

type Verdict struct {
	Status       string    `json:"status"`
	CandidateSHA string    `json:"candidate_sha"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Manifest struct {
	APIVersion   string                `json:"apiVersion"`
	Kind         string                `json:"kind"`
	CandidateSHA string                `json:"candidate_sha,omitempty"`
	Baseline     *Baseline             `json:"baseline,omitempty"`
	Acceptance   map[string]Acceptance `json:"acceptance,omitempty"`
	Verdict      *Verdict              `json:"verdict,omitempty"`
	UpdatedAt    time.Time             `json:"updated_at,omitempty"`
}

type Failure struct {
	Code           string    `json:"code"`
	Message        string    `json:"message"`
	Owner          string    `json:"owner,omitempty"`
	Retryable      bool      `json:"retryable,omitempty"`
	SafeNextAction string    `json:"safe_next_action,omitempty"`
	UnsafeToRepeat []string  `json:"unsafe_to_repeat,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

func NewManifest() *Manifest {
	return &Manifest{APIVersion: "takt/v1alpha1", Kind: "EvidenceManifest", Acceptance: map[string]Acceptance{}}
}

func AcceptanceID(block, check string) string {
	value := strings.ToLower(strings.TrimSpace(block + ":" + check))
	value = regexp.MustCompile(`[^a-z0-9_.:-]+`).ReplaceAllString(value, "-")
	return "check:" + strings.Trim(value, "-")
}

func FailureFingerprint(value string) string {
	normalized := NormalizeFailure(value)
	sum := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func NormalizeFailure(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}

func ClassifyAgainstBaseline(issues []string, baseline *Baseline) (known, fresh []string) {
	if baseline == nil || len(issues) == 0 {
		return nil, append([]string(nil), issues...)
	}
	fingerprints := map[string]bool{}
	for _, fp := range baseline.FailureFingerprints {
		fingerprints[fp] = true
	}
	if len(fingerprints) == 0 {
		for _, item := range baseline.KnownFailures {
			fingerprints[FailureFingerprint(item)] = true
		}
	}
	for _, issue := range issues {
		if fingerprints[FailureFingerprint(issue)] {
			known = append(known, issue)
		} else {
			fresh = append(fresh, issue)
		}
	}
	return known, fresh
}

func EvidenceStrings(output map[string]any) []string {
	var out []string
	for _, key := range []string{"evidence", "checks", "tests"} {
		raw, ok := output[key].([]any)
		if !ok {
			continue
		}
		for _, item := range raw {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
	}
	sort.Strings(out)
	return unique(out)
}

func MarkStale(manifest *Manifest, currentSHA string) bool {
	if manifest == nil || manifest.Verdict == nil || manifest.Verdict.Status == VerdictStale || manifest.Verdict.CandidateSHA == "" || currentSHA == "" || manifest.Verdict.CandidateSHA == currentSHA {
		return false
	}
	manifest.Verdict.Status = VerdictStale
	manifest.Verdict.Reason = fmt.Sprintf("candidate changed from %s to %s after verdict", manifest.Verdict.CandidateSHA, currentSHA)
	manifest.UpdatedAt = time.Now().UTC()
	return true
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
