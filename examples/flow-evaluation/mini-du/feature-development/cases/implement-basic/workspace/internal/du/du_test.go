package du

import "testing"

func TestPublicContractIsDocumented(t *testing.T) {
	if ErrNotImplemented == nil {
		t.Fatal("missing implementation sentinel")
	}
}
