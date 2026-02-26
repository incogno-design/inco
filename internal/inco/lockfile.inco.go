package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// CacheLock represents an advisory file lock on .inco_cache/lock.
// Multiple inco processes (watch, gen, clean, fmt) use this to serialize
// their writes to the cache directory, preventing corruption when
// e.g. `inco gen` runs in an external terminal while the VS Code
// extension's `inco watch` is active.
//
// Uses POSIX flock(2) — purely advisory, zero overhead when no
// contention, and automatically released on process exit/crash.
type CacheLock struct {
	file *os.File
}

// AcquireCacheLock creates/opens .inco_cache/lock and acquires an
// exclusive flock. Blocks until the lock is available.
// The caller must call Release() when done (or defer it).
func AcquireCacheLock(root string) (*CacheLock, error) {
	// @inco: root != "", -return(nil, fmt.Errorf("AcquireCacheLock: empty root"))

	cacheDir := filepath.Join(root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: mkdir: %w", err))

	lockPath := filepath.Join(cacheDir, "lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AcquireCacheLock: open: %w", err))

	// LOCK_EX = exclusive, blocks until acquired.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("AcquireCacheLock: flock: %w", err)
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

	// LOCK_EX|LOCK_NB = exclusive, non-blocking.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("AcquireCacheLock: lock held by another process")
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
