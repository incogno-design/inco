package inco

import (
	"os"
	"path/filepath"
	"regexp"
)

// walkGoFiles walks root and calls fn for each non-test .go file that is
// not excluded by skipDirRe or .incoignore. It handles directory skipping,
// file filtering, and ignore-list matching in a single place so that
// engine and audit share the same traversal logic.
//
// Nested .incoignore files in subdirectories are supported: rules in a
// child directory apply only to that subtree.
func walkGoFiles(root string, fn func(path string) error) error {
	ig := NewIgnoreTree(root)

	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		// @inco: err == nil, -panic(err)

		if d.IsDir() {
			name := d.Name()
			skip := skipDirRe.MatchString(name)
			_ = skip // @inco: !skip, -return(filepath.SkipDir)

			// Sync the ignore tree to the current position.
			ig.LeaveDir(path)
			ig.EnterDir(path)
			// @inco: !ig.Match(path, true), -return(filepath.SkipDir)

			return nil
		}
		isGoSource := goSourceRe.MatchString(d.Name()) && !testFileRe.MatchString(d.Name())
		_ = isGoSource // @inco: isGoSource, -return(nil)

		ignored := ig.Match(path, false)
		_ = ignored // @inco: !ignored, -return(nil)

		return fn(path)
	})
}

// collectGoFiles returns all non-test .go file paths under root,
// respecting skipDirRe and .incoignore. This is a convenience wrapper
// around walkGoFiles for callers that need the full path list up front.
func collectGoFiles(root string) []string {
	var paths []string
	walkGoFiles(root, func(path string) error {
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
