package dynamicplan

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAcquireAdvanceLockSerializesProcesses(t *testing.T) {
	store := Store{Workspace: t.TempDir()}
	first, acquired, err := store.TryAdvanceLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("first lock was not acquired")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := store.AcquireAdvanceLock(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcquireAdvanceLock while held error = %v, want deadline exceeded", err)
	}
	if err := ReleaseAdvanceLock(first); err != nil {
		t.Fatal(err)
	}
	second, err := store.AcquireAdvanceLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := ReleaseAdvanceLock(second); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseAdvanceLockReturnsUnlockError(t *testing.T) {
	store := Store{Workspace: t.TempDir()}
	file, err := store.AcquireAdvanceLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReleaseAdvanceLock(file); err == nil {
		t.Fatal("ReleaseAdvanceLock swallowed closed-descriptor error")
	}
}
