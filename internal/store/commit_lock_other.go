//go:build !unix && !windows

package store

// Filesystems without a native advisory-lock API supported by the Go stdlib
// are serialized within one process. JS/WASI cannot host competing Takt
// processes; additional multiprocess platforms need a native backend.

func acquireCommitLock(dir string) (func(), error) {
	return acquireOtherCommitLock(dir), nil
}

func acquireCommitReadLock(dir string) (func(), error) {
	return acquireOtherCommitLock(dir), nil
}

func acquireOtherCommitLock(dir string) func() {
	return acquireLocalCommitLock(dir)
}
