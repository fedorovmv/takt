package execution

import (
	"errors"
	"fmt"
	"testing"
	"time"
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

func TestProviderUnavailableClassification(t *testing.T) {
	err := &Error{Kind: KindProviderUnavailable, RetryAfter: 3 * time.Second}
	if KindOf(err) != KindProviderUnavailable || !ProviderUnavailable(err) {
		t.Fatalf("classification = %s", KindOf(err))
	}
	if ProviderUnavailable(&Error{Kind: KindExit}) {
		t.Fatal("ordinary exit classified as provider outage")
	}
	if ProviderUnavailable(&Error{Kind: KindProtocol}) {
		t.Fatal("protocol error classified as provider outage")
	}

	wrapped := fmt.Errorf("wrapped: %w", err)
	var classified *Error
	if !errors.As(wrapped, &classified) || classified.RetryAfter != 3*time.Second {
		t.Fatalf("retry-after not preserved through wrapping: %#v", classified)
	}
}
