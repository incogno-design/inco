package inco

import "os"

// CacheLock represents an advisory file lock on .inco_cache/lock.
// Multiple inco processes (watch, gen, clean, fmt) use this to serialize
// their writes to the cache directory, preventing corruption when
// e.g. `inco gen` runs in an external terminal while the VS Code
// extension's `inco watch` is active.
//
// On Unix, uses POSIX flock(2) — purely advisory, zero overhead when no
// contention, and automatically released on process exit/crash.
//
// On Windows, uses LockFileEx/UnlockFileEx from kernel32.dll for
// equivalent exclusive file locking semantics.
type CacheLock struct {
	file *os.File
}
