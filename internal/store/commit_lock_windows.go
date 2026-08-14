//go:build windows

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

func acquireCommitLock(dir string) (func(), error) {
	return acquireWindowsCommitFileLock(dir, true)
}

func acquireCommitReadLock(dir string) (func(), error) {
	return acquireWindowsCommitFileLock(dir, false)
}

func acquireWindowsCommitFileLock(dir string, exclusive bool) (func(), error) {
	file, err := os.OpenFile(filepath.Join(dir, ".commit.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	var flags uintptr
	if exclusive {
		flags = 0x00000002 // LOCKFILE_EXCLUSIVE_LOCK
	}
	overlapped := &syscall.Overlapped{}
	if result, _, callErr := lockFileEx.Call(uintptr(file.Fd()), flags, 0, 1, 0, uintptr(unsafe.Pointer(overlapped))); result == 0 {
		_ = file.Close()
		return nil, fmt.Errorf("LockFileEx: %w", callErr)
	}
	return func() {
		_, _, _ = unlockFileEx.Call(uintptr(file.Fd()), 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		_ = file.Close()
	}, nil
}
