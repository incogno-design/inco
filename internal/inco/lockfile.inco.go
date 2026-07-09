package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// CacheLock represents an advisory file lock on .inco_cache/lock.
// Concurrent inco processes (e.g. two `inco gen` runs, or `inco gen`
// racing `inco clean`) use this to serialize their writes to the cache
// directory, preventing corruption.
//
// Uses POSIX flock(2) — purely advisory, zero overhead when no
// contention, and automatically released on process exit/crash.
type CacheLock struct {
	file *os.File
}

// openLockFile creates the cache directory and opens .inco_cache/lock
// (creating it if needed), ready for locking.
func openLockFile(root string) (*os.File, error) {
	// @inco: root != "", -return(nil, fmt.Errorf("AcquireCacheLock: empty root"))

	cacheDir := filepath.Join(root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: mkdir: %w", err))

	lockPath := filepath.Join(cacheDir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: open: %w", err))

	return f, nil
}

// AcquireCacheLock creates/opens .inco_cache/lock and acquires an
// exclusive flock. Blocks until the lock is available.
// The caller must call Release() when done (or defer it).
func AcquireCacheLock(root string) (*CacheLock, error) {
	f, err := openLockFile(root)
	if err != nil {
		return nil, err
	}
	// LOCK_EX = exclusive, blocks until acquired.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("AcquireCacheLock: flock: %w", err)
	}
	return &CacheLock{file: f}, nil
}

// Release releases the lock and closes the underlying file.
// Safe to call multiple times.
func (l *CacheLock) Release() {
	// @inco: l != nil, -return
	// @inco: l.file != nil, -return

	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	l.file.Close()
	l.file = nil
}
