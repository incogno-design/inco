package inco

import (
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Engine integration — .incoignore skips files
// ---------------------------------------------------------------------------

func TestEngine_IncoignoreSkipsFile(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"gen.pb.go": `package main

// @inco: true
func Generated() {}
`,
		".incoignore": "*.pb.go\n",
	})
	e := NewEngine(dir)
	e.Run()
	// main.go always gets a shadow; gen.pb.go should be ignored.
	ignoredPath := filepath.Join(dir, "gen.pb.go")
	if _, ok := e.Overlay.Replace[ignoredPath]; ok {
		t.Fatal("gen.pb.go should be ignored by .incoignore but appears in overlay")
	}
	if len(e.Overlay.Replace) != 1 {
		t.Fatalf("expected 1 overlay entry (main.go only), got %d", len(e.Overlay.Replace))
	}
}

func TestEngine_IncoignoreSkipsDir(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"extra/lib.go": `package extra

// @inco: true
func Lib() {}
`,
		".incoignore": "extra/\n",
	})
	e := NewEngine(dir)
	e.Run()
	// extra/ should be skipped; only main.go gets a shadow.
	ignoredPath := filepath.Join(dir, "extra", "lib.go")
	if _, ok := e.Overlay.Replace[ignoredPath]; ok {
		t.Fatal("extra/lib.go should be ignored by .incoignore but appears in overlay")
	}
	if len(e.Overlay.Replace) != 1 {
		t.Fatalf("expected 1 overlay entry (main.go only), got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Engine integration — nested .incoignore in subdirectory
// ---------------------------------------------------------------------------

func TestEngine_NestedIncoignore(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"sub/ok.go": `package sub

// @inco: true
func OK() {}
`,
		"sub/gen.pb.go": `package sub

// @inco: true
func Gen() {}
`,
		"sub/.incoignore": "*.pb.go\n",
	})
	e := NewEngine(dir)
	e.Run()
	// sub/gen.pb.go should be ignored by sub/.incoignore.
	ignoredPath := filepath.Join(dir, "sub", "gen.pb.go")
	if _, ok := e.Overlay.Replace[ignoredPath]; ok {
		t.Fatal("sub/gen.pb.go should be ignored by sub/.incoignore but appears in overlay")
	}
	// main.go and sub/ok.go should both have shadows.
	okPath := filepath.Join(dir, "sub", "ok.go")
	if _, ok := e.Overlay.Replace[okPath]; !ok {
		t.Fatal("sub/ok.go should appear in overlay")
	}
	if len(e.Overlay.Replace) != 2 {
		t.Fatalf("expected 2 overlay entries (main.go + sub/ok.go), got %d", len(e.Overlay.Replace))
	}
}
