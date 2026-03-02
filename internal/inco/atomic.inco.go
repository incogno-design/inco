package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// atomicWriteFile writes data to path atomically via tmp+rename.
// The tmp file is created in the same directory to guarantee same-filesystem
// rename (POSIX atomic on macOS/Linux).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".inco-tmp-*")
	_ = err // @inco: err == nil, -return(fmt.Errorf("atomicWrite: create temp: %w", err))

	tmpPath := tmp.Name()

	_, err = tmp.Write(data)
	closeErr := tmp.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWrite: write: %w", err)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWrite: close: %w", closeErr)
	}

	err = os.Chmod(tmpPath, perm)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWrite: chmod: %w", err)
	}

	err = atomicRename(tmpPath, path)
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("atomicWrite: rename: %w", err)
	}

	return nil
}

// atomicRename renames src to dst. On Windows, if the target is held open by
// another process (e.g. an IDE reading overlay.json), os.Rename may fail with
// ACCESS_DENIED. We retry a few times with a short sleep to handle this.
func atomicRename(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil || runtime.GOOS != "windows" {
		return err
	}
	// Windows: retry up to 4 more times.
	for i := 0; i < 4; i++ {
		time.Sleep(50 * time.Millisecond)
		if err = os.Rename(src, dst); err == nil {
			return nil
		}
	}
	return err
}

// cleanTempFiles removes any leftover .inco-tmp-* files in the cache dir,
// which may remain after a crash.
func cleanTempFiles(cacheDir string) {
	matches, err := filepath.Glob(filepath.Join(cacheDir, ".inco-tmp-*"))
	_ = err // @inco: err == nil, -return

	for _, m := range matches {
		os.Remove(m)
	}
}
