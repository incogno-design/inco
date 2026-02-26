package inco

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// simpleGoFile is a minimal Go source file with no directives.
const simpleGoFile = "package main\n\nfunc main() {}\n"

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func TestInit_FreshDir(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !e.inited {
		t.Error("expected inited=true")
	}
	if e.manifest == nil {
		t.Fatal("manifest should be non-nil after Init")
	}
}

func TestInit_Idempotent(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}
	// Second call should be a no-op (with overlay, @inco guard returns nil).
	// Without overlay, initLocked runs again but produces same result.
	if err := e.Init(); err != nil {
		t.Fatal(err)
	}
}

func TestInit_LoadsExistingManifest(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})

	// Run once to generate cache.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	overlayCount := len(e1.Overlay.Replace)
	if overlayCount == 0 {
		t.Fatal("expected overlay entries after Run")
	}

	// New engine, Init should load the existing cache.
	e2 := NewEngine(dir)
	if err := e2.Init(); err != nil {
		t.Fatal(err)
	}
	if len(e2.Overlay.Replace) != overlayCount {
		t.Errorf("Init loaded %d overlay entries, want %d", len(e2.Overlay.Replace), overlayCount)
	}
}

func TestInit_RemovesStaleShadows(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})

	// Run to generate cache.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}

	// Delete the shadow file manually.
	for _, sp := range e1.Overlay.Replace {
		os.Remove(sp)
	}

	// Init should detect missing shadow and remove stale entries.
	e2 := NewEngine(dir)
	if err := e2.Init(); err != nil {
		t.Fatal(err)
	}
	if len(e2.Overlay.Replace) != 0 {
		t.Errorf("expected 0 overlay entries after stale cleanup, got %d", len(e2.Overlay.Replace))
	}
}

func TestInit_CleansUpTempFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})

	cacheDir := filepath.Join(dir, ".inco_cache")
	os.MkdirAll(cacheDir, 0o755)
	os.WriteFile(filepath.Join(cacheDir, ".inco-tmp-leftover"), []byte("x"), 0o644)

	e := NewEngine(dir)
	e.Init()

	// Temp file should be cleaned.
	entries, _ := os.ReadDir(cacheDir)
	for _, entry := range entries {
		if entry.Name() == ".inco-tmp-leftover" {
			t.Error("leftover temp file should have been cleaned by Init")
		}
	}
}

// ---------------------------------------------------------------------------
// GenFile
// ---------------------------------------------------------------------------

func TestGenFile_Basic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result for first generation")
	}
	if result.Path == "" {
		t.Error("result.Path should be non-empty")
	}
	if result.SrcHash == "" {
		t.Error("result.SrcHash should be non-empty")
	}
	if len(result.ShadowData) == 0 {
		t.Error("result.ShadowData should be non-empty")
	}
}

func TestGenFile_CacheHit(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)

	// Run to populate cache.
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// GenFile on same unchanged file should return nil (cache hit).
	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result != nil {
		t.Error("expected nil result (cache hit) for unchanged file")
	}
}

func TestGenFile_CacheMissAfterEdit(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)

	// Run to populate cache.
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// Modify the file.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644)

	// GenFile should detect change and regenerate.
	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result after file edit")
	}
}

func TestGenFile_NonexistentFile(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	_, err := e.GenFile(filepath.Join(dir, "does_not_exist.go"))
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestGenFile_AutoInitsEngine(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	// Don't call Init — GenFile should auto-init.
	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !e.inited {
		t.Error("GenFile should have auto-initialized the engine")
	}
}

// ---------------------------------------------------------------------------
// CommitFile
// ---------------------------------------------------------------------------

func TestCommitFile_Basic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if err := e.CommitFile(result); err != nil {
		t.Fatalf("CommitFile: %v", err)
	}

	// Overlay should now have the entry.
	absPath := result.Path
	if _, ok := e.Overlay.Replace[absPath]; !ok {
		t.Error("overlay should contain entry after CommitFile")
	}

	// Shadow file should exist on disk.
	sp := e.Overlay.Replace[absPath]
	if _, err := os.Stat(sp); err != nil {
		t.Errorf("shadow file should exist: %v", err)
	}

	// Manifest should be updated in memory.
	if e.manifest == nil {
		t.Fatal("manifest should be non-nil")
	}
	if _, ok := e.manifest.Files[absPath]; !ok {
		t.Error("manifest should contain entry after CommitFile")
	}
}

func TestCommitFile_OverwritesOldShadow(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)

	// First gen+commit.
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	absPath := filepath.Join(dir, "main.go")
	absPath, _ = filepath.Abs(absPath)
	oldShadow := e.Overlay.Replace[absPath]

	// Edit source and gen+commit again.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(42) }\n"), 0o644)

	result, err := e.GenFile(absPath)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected non-nil result after edit")
	}
	if err := e.CommitFile(result); err != nil {
		t.Fatal(err)
	}

	newShadow := e.Overlay.Replace[absPath]
	if newShadow == oldShadow {
		t.Error("shadow path should differ after content change")
	}

	// Old shadow should be removed.
	if _, err := os.Stat(oldShadow); !os.IsNotExist(err) {
		t.Error("old shadow file should have been removed")
	}
}

// ---------------------------------------------------------------------------
// Flush
// ---------------------------------------------------------------------------

func TestFlush_WritesOverlayAndManifest(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CommitFile(result); err != nil {
		t.Fatal(err)
	}

	if err := e.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// overlay.json should exist and be valid.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	data, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("read overlay: %v", err)
	}
	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		t.Fatalf("unmarshal overlay: %v", err)
	}
	if len(ov.Replace) == 0 {
		t.Error("overlay.json should have entries")
	}

	// manifest.json should exist and be valid.
	manifestPath := filepath.Join(dir, ".inco_cache", "manifest.json")
	data, err = os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(m.Files) == 0 {
		t.Error("manifest.json should have entries")
	}
}

func TestFlush_AtomicWrite(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if err := e.CommitFile(result); err != nil {
		t.Fatal(err)
	}
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	// No .inco-tmp-* files should remain.
	cacheDir := filepath.Join(dir, ".inco_cache")
	entries, _ := os.ReadDir(cacheDir)
	for _, entry := range entries {
		if len(entry.Name()) > 10 && entry.Name()[:10] == ".inco-tmp-" {
			t.Errorf("leftover temp file: %s", entry.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// Close
// ---------------------------------------------------------------------------

func TestClose_FlushesState(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("GenFile: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if err := e.CommitFile(result); err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	// overlay.json should exist.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("overlay.json should exist after Close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GenFile + CommitFile + Flush integration
// ---------------------------------------------------------------------------

func TestIncrementalFlow_FullCycle(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	e.Init()

	// Step 1: GenFile
	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}

	// Step 2: CommitFile
	if err := e.CommitFile(result); err != nil {
		t.Fatal(err)
	}

	// Step 3: Flush
	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	// Step 4: New engine loading from cache
	e2 := NewEngine(dir)
	e2.Init()

	if len(e2.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e2.Overlay.Replace))
	}

	// Step 5: GenFile again (should be cache hit)
	result2, err := e2.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if result2 != nil {
		t.Error("expected cache hit (nil result) after Flush + reload")
	}
}

func TestIncrementalFlow_MultipleFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
	})
	e := NewEngine(dir)
	e.Init()

	for _, name := range []string{"a.go", "b.go"} {
		result, err := e.GenFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("GenFile(%s): %v", name, err)
		}
		if result == nil {
			t.Fatalf("expected non-nil result for %s", name)
		}
		if err := e.CommitFile(result); err != nil {
			t.Fatalf("CommitFile(%s): %v", name, err)
		}
	}

	if err := e.Flush(); err != nil {
		t.Fatal(err)
	}

	if len(e.Overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Concurrent GenFile safety
// ---------------------------------------------------------------------------

func TestGenFile_ConcurrentSafe(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
		"c.go": "package main\n\nfunc C() {}\n",
	})
	e := NewEngine(dir)
	e.Init()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	results := make([]*GenFileResult, 3)

	for i, name := range []string{"a.go", "b.go", "c.go"} {
		wg.Add(1)
		go func(idx int, n string) {
			defer wg.Done()
			r, err := e.GenFile(filepath.Join(dir, n))
			results[idx] = r
			errs[idx] = err
		}(i, name)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("GenFile[%d]: %v", i, err)
		}
	}
	for i, r := range results {
		if r == nil {
			t.Errorf("GenFile[%d]: got nil result", i)
		}
	}
}

func TestCommitFile_ConcurrentSafe(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": "package main\n\nfunc A() {}\n",
		"b.go": "package main\n\nfunc B() {}\n",
	})
	e := NewEngine(dir)
	e.Init()

	// Generate both files first.
	var genResults []*GenFileResult
	for _, name := range []string{"a.go", "b.go"} {
		r, err := e.GenFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		genResults = append(genResults, r)
	}

	// Commit concurrently.
	var wg sync.WaitGroup
	errs := make([]error, len(genResults))
	for i, r := range genResults {
		wg.Add(1)
		go func(idx int, result *GenFileResult) {
			defer wg.Done()
			errs[idx] = e.CommitFile(result)
		}(i, r)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("CommitFile[%d]: %v", i, err)
		}
	}

	if len(e.Overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Run backward compatibility
// ---------------------------------------------------------------------------

func TestRun_StillWorks(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}

	// Overlay JSON should have been written.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); err != nil {
		t.Errorf("overlay.json should exist: %v", err)
	}

	// Manifest should have been written.
	manifestPath := filepath.Join(dir, ".inco_cache", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest.json should exist: %v", err)
	}
}

func TestRun_ThenGenFile_CacheHit(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// GenFile on unchanged source should be a cache hit.
	result, err := e.GenFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected cache hit after Run")
	}
}

func TestRun_Incremental(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": simpleGoFile,
	})
	e := NewEngine(dir)

	// First run.
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// Second run without changes — should use cache.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e2.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e2.Overlay.Replace))
	}
}
