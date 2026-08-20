package assessment

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"takt/internal/store"
	"takt/internal/validation"
)

const (
	ProtocolV1Alpha1 = "takt-assessment/v1alpha1"
	TypeAssessment   = "assessment"
	MIMEAssessment   = "application/vnd.takt.assessment+json"
	RolePrimary      = "primary"
	RoleAdvisory     = "advisory"

	OutcomeTrueAccept  = "true_accept"
	OutcomeFalseAccept = "false_accept"
	OutcomeTrueReject  = "true_reject"
	OutcomeFalseReject = "false_reject"
)

type Target struct {
	RunID               string `json:"run_id"`
	Revision            uint64 `json:"revision"`
	Status              string `json:"status"`
	WorkflowFingerprint string `json:"workflow_fingerprint"`
	ConfigFingerprint   string `json:"config_fingerprint"`
}

type Assessor struct {
	RunID    string `json:"run_id"`
	NodeID   string `json:"node_id"`
	Revision uint64 `json:"revision"`
}

type Scope struct {
	CaseID string `json:"case_id,omitempty"`
	Repeat int    `json:"repeat,omitempty"`
}

type EvidenceRef struct {
	ProducerRunID string `json:"producer_run_id"`
	ArtifactID    string `json:"artifact_id"`
	SHA256        string `json:"sha256"`
}

type CorruptError struct {
	ProducerRunID string
	ArtifactID    string
	Err           error
}

func (e *CorruptError) Error() string {
	return fmt.Sprintf("assessment_corrupt: producer Run %s artifact %s: %v", e.ProducerRunID, e.ArtifactID, e.Err)
}

func (e *CorruptError) Unwrap() error { return e.Err }

type Envelope struct {
	ProtocolVersion string            `json:"protocol_version"`
	Type            string            `json:"type"`
	ID              string            `json:"id"`
	Role            string            `json:"role"`
	Target          Target            `json:"target"`
	Assessor        Assessor          `json:"assessor"`
	Scope           Scope             `json:"scope"`
	Result          validation.Result `json:"result"`
	Outcome         string            `json:"outcome"`
	Evidence        []EvidenceRef     `json:"evidence"`
	CreatedAt       time.Time         `json:"created_at"`
}

type wireEnvelope struct {
	ProtocolVersion string          `json:"protocol_version"`
	Type            string          `json:"type"`
	ID              string          `json:"id"`
	Role            string          `json:"role"`
	Target          Target          `json:"target"`
	Assessor        Assessor        `json:"assessor"`
	Scope           Scope           `json:"scope"`
	Result          json.RawMessage `json:"result"`
	Outcome         string          `json:"outcome"`
	Evidence        []EvidenceRef   `json:"evidence"`
	CreatedAt       time.Time       `json:"created_at"`
}

func Decode(data []byte) (*Envelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire wireEnvelope
	if err := decoder.Decode(&wire); err != nil {
		return nil, fmt.Errorf("decode assessment: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("decode assessment: multiple JSON values")
		}
		return nil, fmt.Errorf("decode assessment trailing data: %w", err)
	}
	result, err := validation.Decode(wire.Result)
	if err != nil {
		return nil, fmt.Errorf("assessment result: %w", err)
	}
	value := &Envelope{
		ProtocolVersion: wire.ProtocolVersion, Type: wire.Type, ID: wire.ID, Role: wire.Role,
		Target: wire.Target, Assessor: wire.Assessor, Scope: wire.Scope, Result: *result,
		Outcome: wire.Outcome, Evidence: wire.Evidence, CreatedAt: wire.CreatedAt,
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func (value Envelope) Validate() error {
	if value.ProtocolVersion != ProtocolV1Alpha1 {
		return fmt.Errorf("assessment protocol_version must be %q", ProtocolV1Alpha1)
	}
	if value.Type != TypeAssessment {
		return fmt.Errorf("assessment type must be %q", TypeAssessment)
	}
	if strings.TrimSpace(value.ID) == "" {
		return fmt.Errorf("assessment id is required")
	}
	if value.Role != RolePrimary && value.Role != RoleAdvisory {
		return fmt.Errorf("assessment role must be primary or advisory")
	}
	if strings.TrimSpace(value.Target.RunID) == "" {
		return fmt.Errorf("assessment target run_id is required")
	}
	if value.Target.Revision == 0 {
		return fmt.Errorf("assessment target revision must be positive")
	}
	if _, err := Outcome(value.Target.Status, value.Result.Valid); err != nil {
		return err
	}
	if strings.TrimSpace(value.Assessor.RunID) == "" || strings.TrimSpace(value.Assessor.NodeID) == "" || value.Assessor.Revision == 0 {
		return fmt.Errorf("assessment assessor run_id, node_id, and revision are required")
	}
	if value.CreatedAt.IsZero() {
		return fmt.Errorf("assessment created_at is required")
	}
	if value.Role == RolePrimary {
		if strings.TrimSpace(value.Scope.CaseID) == "" {
			return fmt.Errorf("primary assessment scope case_id is required")
		}
		if value.Scope.Repeat <= 0 {
			return fmt.Errorf("primary assessment scope repeat must be positive")
		}
		if len(value.Evidence) == 0 {
			return fmt.Errorf("primary assessment evidence is required")
		}
	}
	for index, evidence := range value.Evidence {
		if strings.TrimSpace(evidence.ProducerRunID) == "" || strings.TrimSpace(evidence.ArtifactID) == "" {
			return fmt.Errorf("assessment evidence %d requires producer_run_id and artifact_id", index)
		}
		decoded, err := hex.DecodeString(evidence.SHA256)
		if err != nil || len(decoded) != 32 {
			return fmt.Errorf("assessment evidence %d sha256 must contain 64 hexadecimal characters", index)
		}
	}
	expected, err := Outcome(value.Target.Status, value.Result.Valid)
	if err != nil {
		return err
	}
	if value.Outcome != expected {
		return fmt.Errorf("assessment outcome %q does not match target status and validation result; want %q", value.Outcome, expected)
	}
	return nil
}

func Outcome(status string, valid bool) (string, error) {
	switch status {
	case store.RunCompleted:
		if valid {
			return OutcomeTrueAccept, nil
		}
		return OutcomeFalseAccept, nil
	case store.RunFailed, store.RunCancelled, store.RunAbandoned:
		if valid {
			return OutcomeFalseReject, nil
		}
		return OutcomeTrueReject, nil
	default:
		return "", fmt.Errorf("assessment target status %q is not terminal", status)
	}
}
