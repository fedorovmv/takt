package evidence

import "testing"

func TestClassifyAgainstBaselineUsesStableFingerprints(t *testing.T) {
	baseline := &Baseline{KnownFailures: []string{"TestLegacyConnection: timeout"}}
	known, fresh := ClassifyAgainstBaseline([]string{"  testlegacyconnection:   timeout ", "TestNewFailure: mismatch"}, baseline)
	if len(known) != 1 || len(fresh) != 1 {
		t.Fatalf("known=%v fresh=%v", known, fresh)
	}
}

func TestMarkStaleInvalidatesVerdictWhenCandidateChanges(t *testing.T) {
	manifest := NewManifest()
	manifest.Verdict = &Verdict{Status: VerdictPass, CandidateSHA: "sha256:a"}
	if !MarkStale(manifest, "sha256:b") {
		t.Fatal("verdict was not invalidated")
	}
	if manifest.Verdict.Status != VerdictStale {
		t.Fatalf("status=%q", manifest.Verdict.Status)
	}
}
