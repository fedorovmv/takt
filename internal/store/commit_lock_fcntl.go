//go:build aix || solaris

package store

import (
	"os"
	"path/filepath"
	"syscall"
)

func acquireCommitLock(dir string) (func(), error) {
	return acquireCommitFcntlLock(dir, syscall.F_WRLCK)
}

func acquireCommitReadLock(dir string) (func(), error) {
	return acquireCommitFcntlLock(dir, syscall.F_RDLCK)
}

func acquireCommitFcntlLock(dir string, lockType int16) (func(), error) {
	releaseLocal := acquireLocalCommitLock(dir)
	file, err := os.OpenFile(filepath.Join(dir, ".commit.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		releaseLocal()
		return nil, err
	}
	lock := syscall.Flock_t{Type: lockType}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLKW, &lock); err != nil {
		_ = file.Close()
		releaseLocal()
		return nil, err
	}
	return func() {
		lock.Type = syscall.F_UNLCK
		_ = syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, &lock)
		_ = file.Close()
		releaseLocal()
	}, nil
}
