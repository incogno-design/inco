package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// LoadIgnore — file not found
// ---------------------------------------------------------------------------

func TestLoadIgnore_NotFound(t *testing.T) {
	dir := t.TempDir()
	ig := LoadIgnore(dir)
	if ig != nil {
		t.Fatal("expected nil for missing .incoignore")
	}
}

// ---------------------------------------------------------------------------
// LoadIgnore — empty / comments only
// ---------------------------------------------------------------------------

func TestLoadIgnore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".incoignore"), []byte("# only comments\n\n"), 0o644)
	ig := LoadIgnore(dir)
	if ig != nil {
		t.Fatal("expected nil for empty .incoignore")
	}
}

// ---------------------------------------------------------------------------
// Match — basename glob
// ---------------------------------------------------------------------------

func TestIgnore_BasenameGlob(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".incoignore"), []byte("*.pb.go\n"), 0o644)
	ig := LoadIgnore(dir)
	if ig == nil {
		t.Fatal("expected non-nil IgnoreList")
	}

	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"foo.pb.go", false, true},
		{"sub/bar.pb.go", false, true},
		{"foo.go", false, false},
		{"foo.pb.go", true, true}, // basename matches dir too
	}
	for _, tt := range tests {
		got := ig.Match(tt.path, tt.isDir)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Match — directory-only pattern (trailing /)
// ---------------------------------------------------------------------------

func TestIgnore_DirOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".incoignore"), []byte("example/\n"), 0o644)
	ig := LoadIgnore(dir)
	if ig == nil {
		t.Fatal("expected non-nil IgnoreList")
	}

	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"example", true, true},      // the dir itself
		{"sub/example", true, true},  // nested dir with same basename
		{"example.go", false, false}, // file, dirOnly pattern → skip
		{"example", false, false},    // file named "example" → skip (dirOnly)
	}
	for _, tt := range tests {
		got := ig.Match(tt.path, tt.isDir)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Match — path pattern (contains /)
// ---------------------------------------------------------------------------

func TestIgnore_PathPattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".incoignore"), []byte("internal/legacy\n"), 0o644)
	ig := LoadIgnore(dir)
	if ig == nil {
		t.Fatal("expected non-nil IgnoreList")
	}

	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"internal/legacy", true, true},         // exact match (dir)
		{"internal/legacy", false, true},        // exact match (file)
		{"internal/legacy/foo.go", false, true}, // under the dir
		{"internal/other", true, false},         // different dir
		{"legacy", true, false},                 // basename alone doesn't match
	}
	for _, tt := range tests {
		// Use filepath.FromSlash to simulate platform-native paths
		// (on Windows this turns / into \, matching filepath.Rel output).
		got := ig.Match(filepath.FromSlash(tt.path), tt.isDir)
		if got != tt.want {
			t.Errorf("Match(%q, %v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Match — nil receiver is safe
// ---------------------------------------------------------------------------

func TestIgnore_NilSafe(t *testing.T) {
	var ig *IgnoreList
	if ig.Match("foo.go", false) {
		t.Fatal("nil IgnoreList should never match")
	}
}
