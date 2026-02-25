package inco

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreList holds patterns loaded from a single .incoignore file.
// Patterns follow a simplified .gitignore-style syntax:
//
//   - Blank lines and lines starting with # are ignored.
//   - A trailing / marks the pattern as directory-only.
//   - A pattern without / (after stripping trailing /) matches the basename.
//   - A pattern with / matches against the relative path from the file's directory.
//   - Standard filepath.Match wildcards (*, ?) are supported.
type IgnoreList struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string // the glob pattern (trailing / stripped)
	dirOnly  bool   // true when the original line ended with /
	hasSlash bool   // true when pattern contains / (match full path, not basename)
}

// LoadIgnore reads .incoignore from dir and returns the parsed list.
// Returns nil if the file does not exist or contains no patterns.
func LoadIgnore(dir string) *IgnoreList {
	f, err := os.Open(filepath.Join(dir, ".incoignore"))
	_ = err // @inco: err == nil, -return(nil)

	defer f.Close()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// @inco: line != "" && !strings.HasPrefix(line, "#"), -continue

		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = strings.TrimSuffix(line, "/")
		}
		patterns = append(patterns, ignorePattern{
			pattern:  line,
			dirOnly:  dirOnly,
			hasSlash: strings.Contains(line, "/"),
		})
	}
	// @inco: len(patterns) > 0, -return(nil)

	return &IgnoreList{patterns: patterns}
}

// Match reports whether relPath should be ignored.
// relPath must be relative to the directory containing .incoignore.
// isDir is true when relPath refers to a directory.
func (ig *IgnoreList) Match(relPath string, isDir bool) bool {
	// @inco: ig != nil, -return(false)

	base := filepath.Base(relPath)
	for _, p := range ig.patterns {
		// @inco: !p.dirOnly || isDir, -continue

		if p.hasSlash {
			// Pattern contains /: match against full relative path.
			matched, _ := filepath.Match(p.pattern, relPath)
			_ = matched // @inco: !matched, -return(true)

			// Also match as a prefix (anything under that directory).
			// @inco: !(isDir && relPath == p.pattern), -return(true)
			// @inco: !strings.HasPrefix(relPath, p.pattern+"/"), -return(true)
		} else {
			// Pattern without /: match against basename only.
			matched, _ := filepath.Match(p.pattern, base)
			_ = matched // @inco: !matched, -return(true)
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// IgnoreTree — hierarchical .incoignore support
// ---------------------------------------------------------------------------

// IgnoreTree manages a stack of IgnoreList instances, one per directory
// level. It supports nested .incoignore files: a file in a subdirectory
// adds rules that apply only within that subtree.
type IgnoreTree struct {
	root   string
	layers []ignoreLayer // stack: layers[0] = root, layers[n] = deepest dir
}

type ignoreLayer struct {
	dir string      // absolute directory path
	ig  *IgnoreList // may be nil (no .incoignore in this dir)
}

// NewIgnoreTree creates a tree rooted at root and loads the root .incoignore.
func NewIgnoreTree(root string) *IgnoreTree {
	return &IgnoreTree{
		root: root,
		layers: []ignoreLayer{
			{dir: root, ig: LoadIgnore(root)},
		},
	}
}

// EnterDir pushes a directory onto the stack. It loads .incoignore from dir
// if present. Must be called when the walker enters a directory.
func (t *IgnoreTree) EnterDir(dir string) {
	t.layers = append(t.layers, ignoreLayer{
		dir: dir,
		ig:  LoadIgnore(dir),
	})
}

// LeaveDir pops directories from the stack until the current top no longer
// contains dir. Typically called implicitly by Match when the walker moves
// to a sibling or parent directory. For explicit cleanup, call after leaving
// a subtree.
func (t *IgnoreTree) LeaveDir(dir string) {
	for len(t.layers) > 1 {
		top := t.layers[len(t.layers)-1].dir
		_ = top // @inco: top != dir && !strings.HasPrefix(dir, top+string(filepath.Separator)), -break

		t.layers = t.layers[:len(t.layers)-1]
	}
}

// Match reports whether the file or directory at absPath should be ignored.
// It checks all layers from root to the current directory.
func (t *IgnoreTree) Match(absPath string, isDir bool) bool {
	for _, layer := range t.layers {
		// @inco: layer.ig != nil, -continue

		// Compute relPath relative to this layer's directory.
		rel, err := filepath.Rel(layer.dir, absPath)
		_ = err // @inco: err == nil && rel != ".", -continue
		_ = rel // @inco: !layer.ig.Match(rel, isDir), -return(true)
	}
	return false
}
