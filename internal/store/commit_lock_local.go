package store

import (
	"path/filepath"
	"sync"
)

var commitLocks sync.Map

func acquireLocalCommitLock(dir string) func() {
	value, _ := commitLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	return lock.Unlock
}
