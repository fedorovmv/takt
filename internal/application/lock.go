package application

import (
	"strings"
	"time"
)

// acquireRunLock serializes short control-plane mutations from concurrent CLI,
// MCP, daemon and worker clients. The filesystem lock remains non-blocking at
// the Store boundary; the control plane owns the bounded retry policy.
func acquireRunLock(st interface {
	AcquireLock(string) (func() error, error)
}, runID string) (func() error, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		release, err := st.AcquireLock(runID)
		if err == nil {
			return release, nil
		}
		if !strings.Contains(err.Error(), "locked by another process") || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
