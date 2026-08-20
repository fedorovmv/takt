package assessment

import (
	"strings"
	"testing"
)

const validPrimaryEnvelope = `{
  "protocol_version":"takt-assessment/v1alpha1",
  "type":"assessment",
  "id":"assessment-1",
  "role":"primary",
  "target":{"run_id":"run-target","revision":7,"status":"completed","workflow_fingerprint":"wf","config_fingerprint":"cfg"},
  "assessor":{"run_id":"run-assessor","node_id":"assess","revision":11},
  "scope":{"case_id":"case-a","repeat":1},
  "result":{"protocol_version":"takt-validation/v1alpha1","type":"validation_result","valid":false,"diagnostics":[{"code":"WRONG","severity":"error"}]},
  "outcome":"false_accept",
  "evidence":[{"producer_run_id":"run-assessor","artifact_id":"evidence:1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],
  "created_at":"2026-08-20T10:00:00Z"
}`

func TestDecodeStrictPrimaryAssessment(t *testing.T) {
	value, err := Decode([]byte(validPrimaryEnvelope))
	if err != nil {
		t.Fatal(err)
	}
	if value.ID != "assessment-1" || value.Role != RolePrimary || value.Target.RunID != "run-target" || value.Target.Revision != 7 {
		t.Fatalf("decoded assessment = %#v", value)
	}
	if value.Result.Valid || value.Outcome != OutcomeFalseAccept || len(value.Evidence) != 1 {
		t.Fatalf("decoded result = %#v", value)
	}
}

func TestDecodeRejectsInvalidAssessmentContracts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"unknown field", strings.Replace(validPrimaryEnvelope, `"created_at":`, `"extra":true,"created_at":`, 1), "unknown field"},
		{"wrong protocol", strings.Replace(validPrimaryEnvelope, "takt-assessment/v1alpha1", "wrong", 1), "protocol_version"},
		{"wrong type", strings.Replace(validPrimaryEnvelope, `"type":"assessment"`, `"type":"wrong"`, 1), "type"},
		{"empty id", strings.Replace(validPrimaryEnvelope, `"id":"assessment-1"`, `"id":""`, 1), "id"},
		{"unsupported role", strings.Replace(validPrimaryEnvelope, `"role":"primary"`, `"role":"judge"`, 1), "role"},
		{"zero target revision", strings.Replace(validPrimaryEnvelope, `"revision":7`, `"revision":0`, 1), "target revision"},
		{"missing primary case", strings.Replace(validPrimaryEnvelope, `"case_id":"case-a"`, `"case_id":""`, 1), "case_id"},
		{"zero primary repeat", strings.Replace(validPrimaryEnvelope, `"repeat":1`, `"repeat":0`, 1), "repeat"},
		{"missing primary evidence", strings.Replace(validPrimaryEnvelope, `"evidence":[{"producer_run_id":"run-assessor","artifact_id":"evidence:1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`, `"evidence":[]`, 1), "evidence"},
		{"outcome mismatch", strings.Replace(validPrimaryEnvelope, `"outcome":"false_accept"`, `"outcome":"true_accept"`, 1), "outcome"},
		{"trailing value", validPrimaryEnvelope + `{}`, "multiple JSON values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.raw))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Decode() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeAllowsAdvisoryWithoutPrimaryScopeOrEvidence(t *testing.T) {
	raw := strings.Replace(validPrimaryEnvelope, `"role":"primary"`, `"role":"advisory"`, 1)
	raw = strings.Replace(raw, `"scope":{"case_id":"case-a","repeat":1}`, `"scope":{}`, 1)
	raw = strings.Replace(raw, `"evidence":[{"producer_run_id":"run-assessor","artifact_id":"evidence:1","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`, `"evidence":[]`, 1)
	if _, err := Decode([]byte(raw)); err != nil {
		t.Fatal(err)
	}
}

func TestOutcomeMatrix(t *testing.T) {
	tests := []struct {
		status string
		valid  bool
		want   string
	}{
		{"completed", true, OutcomeTrueAccept},
		{"completed", false, OutcomeFalseAccept},
		{"failed", false, OutcomeTrueReject},
		{"failed", true, OutcomeFalseReject},
		{"cancelled", false, OutcomeTrueReject},
		{"abandoned", true, OutcomeFalseReject},
	}
	for _, test := range tests {
		got, err := Outcome(test.status, test.valid)
		if err != nil || got != test.want {
			t.Fatalf("Outcome(%q, %t) = %q, %v; want %q", test.status, test.valid, got, err, test.want)
		}
	}
	if _, err := Outcome("running", true); err == nil {
		t.Fatal("nonterminal target status accepted")
	}
}
