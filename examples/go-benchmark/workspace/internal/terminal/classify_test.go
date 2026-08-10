package terminal

import (
	"context"
	"testing"
)

func TestClassifyGivesContextPriorityOverOverflow(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Kind
	}{
		{name: "timeout", err: context.DeadlineExceeded, want: KindTimedOut},
		{name: "cancel", err: context.Canceled, want: KindCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err, 1, true); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyOrdinaryFailures(t *testing.T) {
	if got := Classify(nil, 1, true); got != KindOverflow {
		t.Fatalf("overflow Classify() = %q", got)
	}
	if got := Classify(nil, 7, false); got != KindExit {
		t.Fatalf("exit Classify() = %q", got)
	}
}
