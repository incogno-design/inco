// Package fsutil provides file-system traversal and ignore-list support
// for the inco engine and analysis tools.
package fsutil

import (
	"os"
	"path/filepath"
	"regexp"
)

// WalkGoFiles walks root and calls fn for each non-test .go file that is
// not excluded by skipDirRe or .incoignore. It handles directory skipping,
// file filtering, and ignore-list matching in a single place so that
// engine and audit share the same traversal logic.
//
// Nested .incoignore files in subdirectories are supported: rules in a
// child directory apply only to that subtree.
//
// Plain if checks are used (instead of @inco:/@if:) because this function
// must work correctly without overlay (e.g. plain go test).
func WalkGoFiles(root string, fn func(path string) error) error {
	ig := NewIgnoreTree(root)

	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			name := d.Name()
			if skipDirRe.MatchString(name) {
				return filepath.SkipDir
			}

			// Sync the ignore tree to the current position.
			ig.LeaveDir(path)
			ig.EnterDir(path)
			if ig.Match(path, true) {
				return filepath.SkipDir
			}

			return nil
		}

		if !goSourceRe.MatchString(d.Name()) || testFileRe.MatchString(d.Name()) {
			return nil
		}

		if ig.Match(path, false) {
			return nil
		}

		return fn(path)
	})
}

// CollectGoFiles returns all non-test .go file paths under root,
// respecting skipDirRe and .incoignore. This is a convenience wrapper
// around WalkGoFiles for callers that need the full path list up front.
func CollectGoFiles(root string) []string {
	var paths []string
	WalkGoFiles(root, func(path string) error {
		paths = append(paths, path)
		return nil
	})
	return paths
}

// ---------------------------------------------------------------------------
// Shared regex patterns
// ---------------------------------------------------------------------------

// skipDirRe matches directory names that should be skipped during scanning:
// hidden dirs (starting with .), vendor, testdata.
var skipDirRe = regexp.MustCompile(`^\.|^vendor$|^testdata$`)

// goSourceRe matches .go filenames.
var goSourceRe = regexp.MustCompile(`^.+\.go$`)

// testFileRe matches Go test files.
var testFileRe = regexp.MustCompile(`_test\.go$`)

// ---------------------------------------------------------------------------
// Predicate helpers (exported for sub-packages)
// ---------------------------------------------------------------------------

// SkipDir reports whether a directory name should be skipped during scanning.
func SkipDir(name string) bool { return skipDirRe.MatchString(name) }

// IsGoSource reports whether a filename is a Go source file.
func IsGoSource(name string) bool { return goSourceRe.MatchString(name) }

// IsTestFile reports whether a filename is a Go test file.
func IsTestFile(name string) bool { return testFileRe.MatchString(name) }
