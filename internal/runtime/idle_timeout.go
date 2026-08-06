package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var ErrIdleTimeout = errors.New("node idle timeout exceeded")

type idleMonitor struct {
	ctx    context.Context
	cancel context.CancelCauseFunc
	touch  chan struct{}
	done   chan struct{}
	once   sync.Once
}

func newIdleMonitor(parent context.Context, value string) (*idleMonitor, error) {
	if strings.TrimSpace(value) == "" {
		ctx, cancel := context.WithCancelCause(parent)
		return &idleMonitor{ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return nil, fmt.Errorf("invalid idle_timeout %q", value)
	}
	ctx, cancel := context.WithCancelCause(parent)
	monitor := &idleMonitor{ctx: ctx, cancel: cancel, touch: make(chan struct{}, 1), done: make(chan struct{})}
	go monitor.run(duration)
	monitor.Touch()
	return monitor, nil
}

func (m *idleMonitor) run(duration time.Duration) {
	defer close(m.done)
	timer := time.NewTimer(duration)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			m.cancel(ErrIdleTimeout)
			return
		case <-m.touch:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(duration)
		case <-m.ctx.Done():
			return
		}
	}
}

func (m *idleMonitor) Context() context.Context { return m.ctx }

func (m *idleMonitor) Touch() {
	if m == nil || m.touch == nil {
		return
	}
	select {
	case m.touch <- struct{}{}:
	default:
	}
}

func (m *idleMonitor) Close() {
	if m == nil {
		return
	}
	m.once.Do(func() { m.cancel(nil) })
	if m.touch != nil {
		<-m.done
	}
}
