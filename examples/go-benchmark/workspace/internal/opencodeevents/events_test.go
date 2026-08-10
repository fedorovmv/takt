package opencodeevents

import (
	"strings"
	"testing"
)

func TestSummarizeCountsEachStepFinishOnce(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"step_finish","sessionID":"ses-1","part":{"id":"step-1","type":"step-finish","tokens":{"input":10,"output":4}}}`,
		`{"type":"step_finish","sessionID":"ses-1","part":{"id":"step-1","type":"step-finish","tokens":{"input":10,"output":4}}}`,
		`{"type":"step_finish","sessionID":"ses-1","part":{"id":"step-2","type":"step-finish","tokens":{"input":2,"output":1}}}`,
	}, "\n")

	got, err := Summarize(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.InputTokens != 12 || got.OutputTokens != 5 {
		t.Fatalf("Summarize() = %+v, want input=12 output=5", got)
	}
}

func TestSummarizeTreatsErrorEventAsFailure(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"error","sessionID":"ses-1","error":{"data":{"message":"provider failed"}}}`,
		`{"type":"step_finish","sessionID":"ses-1","part":{"id":"step-1","type":"step-finish","tokens":{"input":10,"output":4}}}`,
	}, "\n")

	_, err := Summarize(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("Summarize() error = %v, want provider failure", err)
	}
}
