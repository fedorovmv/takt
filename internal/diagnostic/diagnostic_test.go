package diagnostic

import (
	"fmt"
	"testing"

	"takt/internal/execution"
)

func TestFingerprintNormalizesWorkspaceAndVolatileNumbers(t *testing.T) {
	a := FromError("TEST_FAILURE", &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "validate", Err: fmt.Errorf("/tmp/a/project/test.log pid=12345")}, false, "/tmp/a/project")
	b := FromError("TEST_FAILURE", &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "validate", Err: fmt.Errorf("/tmp/b/project/test.log pid=67890")}, false, "/tmp/b/project")
	if a.Fingerprint != b.Fingerprint {
		t.Fatalf("fingerprints differ: %s %s", a.Fingerprint, b.Fingerprint)
	}
	if a.Fingerprint == "" || a.Kind != "exit" || a.Op != "validate" {
		t.Fatalf("unexpected diagnostic: %+v", a)
	}
}

func TestFingerprintDistinguishesDifferentFailures(t *testing.T) {
	a := FromError("TEST_FAILURE", &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "validate", Err: fmt.Errorf("unknown endpoint alpha")}, false)
	b := FromError("TEST_FAILURE", &execution.Error{Kind: execution.KindExit, ExitCode: 1, Op: "validate", Err: fmt.Errorf("type mismatch beta")}, false)
	if a.Fingerprint == b.Fingerprint {
		t.Fatalf("different failures share fingerprint %s", a.Fingerprint)
	}
}
