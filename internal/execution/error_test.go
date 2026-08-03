package execution

import (
	"errors"
	"testing"
)

func TestKindOfWrappedError(t *testing.T) {
	err := &Error{Kind: KindStart, Op: "test", Err: errors.New("boom")}
	if KindOf(err) != KindStart || IsExit(err) {
		t.Fatalf("unexpected classification: %s", KindOf(err))
	}
	if !errors.Is(err, err.Err) {
		t.Fatal("unwrap is not preserved")
	}
}
