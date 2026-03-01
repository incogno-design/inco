//go:build windows

package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	modkernel32      = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = modkernel32.NewProc("LockFileEx")
	procUnlockFileEx = modkernel32.NewProc("UnlockFileEx")
)

const (
	lockfileExclusiveLock   = 0x00000002
	lockfileFailImmediately = 0x00000001
)

func lockFileEx(h syscall.Handle, flags uint32) error {
	var ol syscall.Overlapped
	r1, _, e1 := procLockFileEx.Call(
		uintptr(h),
		uintptr(flags),
		0, // reserved
		1, // nNumberOfBytesToLockLow
		0, // nNumberOfBytesToLockHigh
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return e1
	}
	return nil
}

func unlockFileEx(h syscall.Handle) error {
	var ol syscall.Overlapped
	r1, _, e1 := procUnlockFileEx.Call(
		uintptr(h),
		0, // reserved
		1, // nNumberOfBytesToUnlockLow
		0, // nNumberOfBytesToUnlockHigh
		uintptr(unsafe.Pointer(&ol)),
	)
	if r1 == 0 {
		return e1
	}
	return nil
}

// AcquireCacheLock creates/opens .inco_cache/lock and acquires an
// exclusive lock. Blocks until the lock is available.
// The caller must call Release() when done (or defer it).
func AcquireCacheLock(root string) (*CacheLock, error) {
	// @inco: root != "", -return(nil, fmt.Errorf("AcquireCacheLock: empty root"))

	cacheDir := filepath.Join(root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: mkdir: %w", err))

	lockPath := filepath.Join(cacheDir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: open: %w", err))

	// Exclusive lock, blocking.
	err = lockFileEx(syscall.Handle(f.Fd()), lockfileExclusiveLock)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("AcquireCacheLock: LockFileEx: %w", err)
	}

	return &CacheLock{file: f}, nil
}

// TryAcquireCacheLock attempts to acquire the lock without blocking.
// Returns (lock, nil) on success, or (nil, err) if the lock is held
// by another process.
func TryAcquireCacheLock(root string) (*CacheLock, error) {
	// @inco: root != "", -return(nil, fmt.Errorf("TryAcquireCacheLock: empty root"))

	cacheDir := filepath.Join(root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("TryAcquireCacheLock: mkdir: %w", err))

	lockPath := filepath.Join(cacheDir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("TryAcquireCacheLock: open: %w", err))

	// Exclusive lock, non-blocking.
	err = lockFileEx(syscall.Handle(f.Fd()), lockfileExclusiveLock|lockfileFailImmediately)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("TryAcquireCacheLock: lock held by another process")
	}

	return &CacheLock{file: f}, nil
}

// Release releases the lock and closes the underlying file.
// Safe to call multiple times.
func (l *CacheLock) Release() {
	// @inco: l != nil, -return
	// @inco: l.file != nil, -return

	unlockFileEx(syscall.Handle(l.file.Fd()))
	l.file.Close()
	l.file = nil
}
