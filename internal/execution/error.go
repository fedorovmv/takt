package execution

import (
	"errors"
	"fmt"
	"time"
)

type Kind string

const (
	KindExit                Kind = "exit"
	KindStart               Kind = "start"
	KindCancelled           Kind = "cancelled"
	KindTimedOut            Kind = "timed_out"
	KindProtocol            Kind = "protocol"
	KindProviderUnavailable Kind = "provider_unavailable"
	KindInternal            Kind = "internal"
	KindExternalUnknown     Kind = "external_state_unknown"
)

type Error struct {
	Kind       Kind
	ExitCode   int
	Op         string
	Err        error
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		if e.Kind == KindExit {
			return fmt.Sprintf("%s exited with code %d", e.Op, e.ExitCode)
		}
		return fmt.Sprintf("%s failed (%s)", e.Op, e.Kind)
	}
	if e.Kind == KindExit {
		return fmt.Sprintf("%s exited with code %d: %v", e.Op, e.ExitCode, e.Err)
	}
	return fmt.Sprintf("%s failed (%s): %v", e.Op, e.Kind, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

func KindOf(err error) Kind {
	var target *Error
	if errors.As(err, &target) {
		return target.Kind
	}
	return KindInternal
}

func IsExit(err error) bool { return KindOf(err) == KindExit }
