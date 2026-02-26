package inco

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// FileEvent type
// ---------------------------------------------------------------------------

func TestFileEventKind_String(t *testing.T) {
	tests := []struct {
		kind FileEventKind
		want string
	}{
		{EventCreate, "create"},
		{EventModify, "modify"},
		{EventDelete, "delete"},
		{FileEventKind(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Errorf("FileEventKind(%d).String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// isGoSource
// ---------------------------------------------------------------------------

func TestIsGoSource(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"foo.go", true},
		{"bar.inco.go", true},
		{"baz_test.go", false},
		{"readme.md", false},
		{".inco_cache/shadow.go", false},
		{"main.go", true},
	}
	for _, tt := range tests {
		if got := isGoSource(tt.path); got != tt.want {
			t.Errorf("isGoSource(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// HandleEvent — create/modify
// ---------------------------------------------------------------------------

func TestHandleEvent_CreateModify(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)
	e := NewEngine(dir)

	// Create event.
	err := e.HandleEvent(FileEvent{
		Kind: EventCreate,
		Path: filepath.Join(dir, "main.go"),
	})
	if err != nil {
		t.Fatalf("HandleEvent create: %v", err)
	}

	// Shadow should exist in overlay.
	if len(e.Overlay.Replace) == 0 {
		t.Fatal("expected overlay entry after create event")
	}

	// Modify event — should be a cache hit (same content).
	err = e.HandleEvent(FileEvent{
		Kind: EventModify,
		Path: filepath.Join(dir, "main.go"),
	})
	if err != nil {
		t.Fatalf("HandleEvent modify (cache hit): %v", err)
	}

	// Modify with changed content.
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true, -panic("updated")
func main() {}
`)
	err = e.HandleEvent(FileEvent{
		Kind: EventModify,
		Path: filepath.Join(dir, "main.go"),
	})
	if err != nil {
		t.Fatalf("HandleEvent modify (changed): %v", err)
	}
}

// ---------------------------------------------------------------------------
// HandleEvent — delete
// ---------------------------------------------------------------------------

func TestHandleEvent_Delete(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)
	e := NewEngine(dir)

	// First generate shadow.
	err := e.HandleEvent(FileEvent{Kind: EventCreate, Path: filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("HandleEvent create: %v", err)
	}
	if len(e.Overlay.Replace) != 1 {
		t.Fatalf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}

	// Delete event.
	os.Remove(filepath.Join(dir, "main.go"))
	err = e.HandleEvent(FileEvent{Kind: EventDelete, Path: filepath.Join(dir, "main.go")})
	if err != nil {
		t.Fatalf("HandleEvent delete: %v", err)
	}

	if len(e.Overlay.Replace) != 0 {
		t.Errorf("expected 0 overlay entries after delete, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// HandleEvent — non-Go files are ignored
// ---------------------------------------------------------------------------

func TestHandleEvent_IgnoresNonGo(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	err := e.HandleEvent(FileEvent{Kind: EventModify, Path: filepath.Join(dir, "README.md")})
	if err != nil {
		t.Fatalf("HandleEvent non-go: %v", err)
	}
	if len(e.Overlay.Replace) != 0 {
		t.Error("expected no overlay entries for non-Go file")
	}
}

// ---------------------------------------------------------------------------
// HandleEvent — test files are ignored
// ---------------------------------------------------------------------------

func TestHandleEvent_IgnoresTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main_test.go"), `package main
func TestFoo(t *testing.T) {}
`)
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	err := e.HandleEvent(FileEvent{Kind: EventCreate, Path: filepath.Join(dir, "main_test.go")})
	if err != nil {
		t.Fatalf("HandleEvent test file: %v", err)
	}
	if len(e.Overlay.Replace) != 0 {
		t.Error("expected no overlay entries for test file")
	}
}

// ---------------------------------------------------------------------------
// HandleEvent — go.mod change invalidates imports
// ---------------------------------------------------------------------------

func TestHandleEvent_GoModInvalidatesImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/test
go 1.21
`)
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)

	e := NewEngine(dir)

	// Force import map build.
	_ = e.HandleEvent(FileEvent{Kind: EventCreate, Path: filepath.Join(dir, "main.go")})

	// Trigger go.mod change.
	err := e.HandleEvent(FileEvent{Kind: EventModify, Path: filepath.Join(dir, "go.mod")})
	if err != nil {
		t.Fatalf("HandleEvent go.mod: %v", err)
	}

	// importMap should be nil (invalidated).
	e.mu.Lock()
	if e.importMap != nil {
		t.Error("expected importMap to be nil after go.mod change")
	}
	e.mu.Unlock()
}

// ---------------------------------------------------------------------------
// DeleteFile
// ---------------------------------------------------------------------------

func TestDeleteFile_Basic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "main.go")
	if _, ok := e.Overlay.Replace[path]; !ok {
		t.Fatal("expected overlay entry before delete")
	}

	if err := e.DeleteFile(path); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}

	if _, ok := e.Overlay.Replace[path]; ok {
		t.Error("overlay entry still exists after DeleteFile")
	}

	e.mu.Lock()
	if _, ok := e.manifest.Files[path]; ok {
		t.Error("manifest entry still exists after DeleteFile")
	}
	e.mu.Unlock()
}

func TestDeleteFile_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	// Should be a no-op.
	err := e.DeleteFile(filepath.Join(dir, "nope.go"))
	if err != nil {
		t.Errorf("DeleteFile nonexistent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Reconcile
// ---------------------------------------------------------------------------

func TestReconcile_NewFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), `package main
// @inco: true
func a() {}
`)
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	n, err := e.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("Reconcile generated %d files, want 1", n)
	}
	if len(e.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}
}

func TestReconcile_DeletedSource(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), `package main
// @inco: true
func a() {}
`)
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "a.go")
	if _, ok := e.Overlay.Replace[path]; !ok {
		t.Fatal("expected overlay entry before removing source")
	}

	// Remove source file.
	os.Remove(path)

	n, err := e.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 0 {
		t.Errorf("Reconcile generated %d files, want 0 (source was deleted)", n)
	}
	if _, ok := e.Overlay.Replace[path]; ok {
		t.Error("overlay entry still exists after Reconcile with deleted source")
	}
}

func TestReconcile_StaleHash(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.go"), `package main
// @inco: true
func a() {}
`)
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// Modify the source.
	writeFile(t, filepath.Join(dir, "a.go"), `package main
// @inco: true, -panic("changed")
func a() {}
`)

	n, err := e.Reconcile()
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n != 1 {
		t.Errorf("Reconcile generated %d files, want 1 (stale hash)", n)
	}
}

// ---------------------------------------------------------------------------
// InvalidateImports
// ---------------------------------------------------------------------------

func TestInvalidateImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func main() {}
`)
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	// Force import map to be built.
	_ = e.buildImportMap()

	e.mu.Lock()
	if e.importMap == nil {
		t.Fatal("importMap should be non-nil after buildImportMap")
	}
	e.mu.Unlock()

	e.InvalidateImports()

	e.mu.Lock()
	if e.importMap != nil {
		t.Error("importMap should be nil after InvalidateImports")
	}
	e.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Debouncer
// ---------------------------------------------------------------------------

func TestDebouncer_Basic(t *testing.T) {
	var called atomic.Int32
	var lastPath atomic.Value

	d := NewDebouncer(50*time.Millisecond, func(ev FileEvent) {
		called.Add(1)
		lastPath.Store(ev.Path)
	})
	defer d.Stop()

	d.Send(FileEvent{Kind: EventModify, Path: "/a.go"})
	time.Sleep(150 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
	if got := lastPath.Load().(string); got != "/a.go" {
		t.Errorf("expected path /a.go, got %q", got)
	}
}

func TestDebouncer_Coalesces(t *testing.T) {
	var called atomic.Int32

	d := NewDebouncer(100*time.Millisecond, func(ev FileEvent) {
		called.Add(1)
	})
	defer d.Stop()

	// Send 5 rapid events for the same path.
	for i := 0; i < 5; i++ {
		d.Send(FileEvent{Kind: EventModify, Path: "/a.go"})
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce delay + margin.
	time.Sleep(200 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 coalesced call, got %d", got)
	}
}

func TestDebouncer_DifferentPaths(t *testing.T) {
	var mu sync.Mutex
	paths := make(map[string]int)

	d := NewDebouncer(50*time.Millisecond, func(ev FileEvent) {
		mu.Lock()
		paths[ev.Path]++
		mu.Unlock()
	})
	defer d.Stop()

	d.Send(FileEvent{Kind: EventModify, Path: "/a.go"})
	d.Send(FileEvent{Kind: EventModify, Path: "/b.go"})

	time.Sleep(150 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 2 {
		t.Errorf("expected 2 distinct paths, got %d", len(paths))
	}
}

func TestDebouncer_Stop(t *testing.T) {
	var called atomic.Int32

	d := NewDebouncer(100*time.Millisecond, func(ev FileEvent) {
		called.Add(1)
	})

	d.Send(FileEvent{Kind: EventModify, Path: "/a.go"})
	d.Stop()
	time.Sleep(200 * time.Millisecond)

	if got := called.Load(); got != 0 {
		t.Errorf("expected 0 calls after Stop, got %d", got)
	}
}

func TestDebouncer_LatestEventUsed(t *testing.T) {
	var lastKind atomic.Int32

	d := NewDebouncer(50*time.Millisecond, func(ev FileEvent) {
		lastKind.Store(int32(ev.Kind))
	})
	defer d.Stop()

	d.Send(FileEvent{Kind: EventCreate, Path: "/a.go"})
	time.Sleep(10 * time.Millisecond)
	d.Send(FileEvent{Kind: EventModify, Path: "/a.go"})
	time.Sleep(10 * time.Millisecond)
	d.Send(FileEvent{Kind: EventDelete, Path: "/a.go"})

	time.Sleep(150 * time.Millisecond)

	if got := FileEventKind(lastKind.Load()); got != EventDelete {
		t.Errorf("expected last event kind = delete, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ScheduleFlush / CancelFlush
// ---------------------------------------------------------------------------

func TestScheduleFlush(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")

	// Remove the overlay to confirm ScheduleFlush recreates it.
	os.Remove(overlayPath)

	e.ScheduleFlush(50 * time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("overlay.json should exist after ScheduleFlush: %v", err)
	}
}

func TestCancelFlush(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func main() {}
`)
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}

	e.ScheduleFlush(100 * time.Millisecond)
	e.CancelFlush()
	time.Sleep(200 * time.Millisecond)

	// Overlay should NOT exist because we cancelled.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); err == nil {
		t.Error("overlay.json should NOT exist after CancelFlush")
	}
}

// ---------------------------------------------------------------------------
// Integration: HandleEvent cycle
// ---------------------------------------------------------------------------

func TestHandleEvent_FullCycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
// @inco: true
func main() {}
`)

	e := NewEngine(dir)

	// Create.
	path := filepath.Join(dir, "main.go")
	if err := e.HandleEvent(FileEvent{Kind: EventCreate, Path: path}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(e.Overlay.Replace) != 1 {
		t.Fatalf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}

	// Flush.
	if err := e.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// Verify overlay.json exists.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Fatalf("overlay.json should exist after flush: %v", err)
	}

	// Delete.
	os.Remove(path)
	if err := e.HandleEvent(FileEvent{Kind: EventDelete, Path: path}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(e.Overlay.Replace) != 0 {
		t.Errorf("expected 0 overlay entries, got %d", len(e.Overlay.Replace))
	}

	// Flush again.
	if err := e.Flush(); err != nil {
		t.Fatalf("flush after delete: %v", err)
	}
}
