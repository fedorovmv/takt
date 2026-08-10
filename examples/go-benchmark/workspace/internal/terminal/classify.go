package terminal

import (
	"context"
	"errors"
)

type Kind string

const (
	KindExit      Kind = "exit"
	KindOverflow  Kind = "overflow"
	KindTimedOut  Kind = "timed_out"
	KindCancelled Kind = "cancelled"
)

func Classify(ctxErr error, _ int, overflow bool) Kind {
	if overflow {
		return KindOverflow
	}
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return KindTimedOut
	}
	if errors.Is(ctxErr, context.Canceled) {
		return KindCancelled
	}
	return KindExit
}
