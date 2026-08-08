package assistant

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"takt/internal/execution"
)

func TestPiVersionProbeHasIndependentTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeSleepingVersionProbe(t)
	started := time.Now()
	_, err := probePiVersionWithTimeout(context.Background(), bin, t.TempDir(), os.Environ(), 25*time.Millisecond)
	assertProbeTimedOut(t, err, time.Since(started))
}

func TestOpenCodeVersionProbeHasIndependentTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := writeSleepingVersionProbe(t)
	started := time.Now()
	_, err := probeOpenCodeVersionWithTimeout(context.Background(), bin, t.TempDir(), os.Environ(), 25*time.Millisecond)
	assertProbeTimedOut(t, err, time.Since(started))
}

func writeSleepingVersionProbe(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 1\nprintf 'v1\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertProbeTimedOut(t *testing.T, err error, elapsed time.Duration) {
	t.Helper()
	if err == nil {
		t.Fatal("expected timeout")
	}
	var execErr *execution.Error
	if !errors.As(err, &execErr) || execErr.Kind != execution.KindTimedOut {
		t.Fatalf("error=%T %v", err, err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("probe timeout took %s", elapsed)
	}
}
