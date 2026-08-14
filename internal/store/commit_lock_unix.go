//go:build unix

package store

import (
	"os"
	"path/filepath"
	"syscall"
)

func acquireCommitLock(dir string) (func(), error) {
	return acquireCommitFileLock(dir, syscall.LOCK_EX)
}

func acquireCommitReadLock(dir string) (func(), error) {
	return acquireCommitFileLock(dir, syscall.LOCK_SH)
}

func acquireCommitFileLock(dir string, mode int) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, ".commit.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), mode); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
