package apperror

import (
	"errors"
	"testing"

	"takt/internal/assessment"
)

func TestDescribeAssessmentCorrupt(t *testing.T) {
	got := Describe(&assessment.CorruptError{ProducerRunID: "run-assessor", ArtifactID: "assessment-1", Err: errors.New("checksum")})
	if got.Code != "assessment_corrupt" || got.Details["producer_run_id"] != "run-assessor" || got.Details["artifact_id"] != "assessment-1" {
		t.Fatalf("descriptor = %+v", got)
	}
}
