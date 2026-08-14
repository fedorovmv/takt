//go:build !unix && !windows

package store

import (
	"path/filepath"
	"sync"
)

var commitLocks sync.Map

func acquireCommitLock(dir string) (func(), error) {
	return acquireOtherCommitLock(dir), nil
}

func acquireCommitReadLock(dir string) (func(), error) {
	return acquireOtherCommitLock(dir), nil
}

func acquireOtherCommitLock(dir string) func() {
	value, _ := commitLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}
