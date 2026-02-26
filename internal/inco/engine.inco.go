package inco

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imnive-design/inco-go/internal/codegen"
	"github.com/imnive-design/inco-go/internal/fsutil"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// Engine scans Go source files for @inco: directives and produces an
// overlay that injects the corresponding if-statements at compile time.
type Engine struct {
	Root       string
	Overlay    Overlay
	importMap  map[string]string // lazily built: package name → import path
	importOnce sync.Once

	mu       sync.Mutex // protects manifest and Overlay during incremental ops
	manifest *Manifest  // cached manifest (loaded by Init)
	inited   bool       // true after Init completes

	flushTimer *time.Timer // used by ScheduleFlush; nil when no flush pending
}

// NewEngine creates an engine rooted at the given directory.
func NewEngine(root string) *Engine {
	// @inco: root != "", -panic("NewEngine: root must not be empty")

	return &Engine{
		Root:    root,
		Overlay: Overlay{Replace: make(map[string]string)},
	}
}

// ---------------------------------------------------------------------------
// Init — load cached state
// ---------------------------------------------------------------------------

// Init loads cached state (manifest, overlay) from disk and validates
// shadow file integrity. Safe to call multiple times (no-op after first).
// Called automatically by Run and GenFile.
func (e *Engine) Init() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// @if: e.inited, -return(nil)

	return e.initLocked()
}

func (e *Engine) initLocked() error {
	e.manifest = e.loadManifest()
	// Ensure manifest.Files is never nil (loadManifest may return nil map
	// when running without overlay and the manifest file doesn't exist).
	if e.manifest.Files == nil {
		e.manifest.Files = make(map[string]ManifestEntry)
	}

	for k, v := range e.loadOverlayIfExists() {
		e.Overlay.Replace[k] = v
	}

	// Validate shadow files; remove stale entries.
	for src, entry := range e.manifest.Files {
		if _, err := os.Stat(entry.ShadowPath); err != nil {
			delete(e.manifest.Files, src)
			delete(e.Overlay.Replace, src)
		}
	}

	cleanTempFiles(filepath.Join(e.Root, ".inco_cache"))
	e.inited = true
	return nil
}

func (e *Engine) ensureInit() {
	e.mu.Lock()
	defer e.mu.Unlock()
	// @if: e.inited, -return

	e.initLocked()
}

// ---------------------------------------------------------------------------
// GenFile / CommitFile / Flush — single-file incremental API
// ---------------------------------------------------------------------------

// GenFileResult holds the output of single-file shadow generation.
type GenFileResult struct {
	Path       string // absolute source file path
	SrcHash    string // SHA-256 hex of source content
	ShadowData []byte // generated shadow content
}

// GenFile processes a single source file and returns its shadow content.
// Returns (nil, nil) when the file is unchanged (cache hit).
// Panics from @inco: guards (when running without overlay) are recovered
// and returned as errors.
func (e *Engine) GenFile(path string) (result *GenFileResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			result = nil
			err = fmt.Errorf("GenFile: %v", r)
		}
	}()

	e.ensureInit()

	absPath, err := filepath.Abs(path)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("GenFile: abs: %w", err))

	srcHash, err := hashFile(absPath)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("GenFile: hash: %w", err))

	// Check cache.
	e.mu.Lock()
	if entry, ok := e.manifest.Files[absPath]; ok && entry.SrcHash == srcHash {
		if _, serr := os.Stat(entry.ShadowPath); serr == nil {
			e.mu.Unlock()
			return nil, nil // unchanged
		}
	}
	e.mu.Unlock()

	// Parse — accept partial AST for degraded mode.
	fset := token.NewFileSet()
	f, parseErr := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	_ = parseErr
	_ = f // @inco: f != nil, -return(nil, fmt.Errorf("GenFile: unparseable %s: %w", absPath, parseErr))

	shadow := codegen.GenerateShadow(absPath, f, fset, e.Root, e.buildImportMap())
	return &GenFileResult{
		Path:       absPath,
		SrcHash:    srcHash,
		ShadowData: shadow,
	}, nil
}

// CommitFile writes the shadow file to .inco_cache and updates the
// in-memory overlay and manifest. Call Flush to persist to disk.
func (e *Engine) CommitFile(r *GenFileResult) error {
	// @inco: r != nil, -return(fmt.Errorf("CommitFile: nil result"))

	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove old shadow if it exists.
	if old, ok := e.Overlay.Replace[r.Path]; ok {
		os.Remove(old)
	}

	err := e.writeShadow(r.Path, r.ShadowData)
	_ = err // @inco: err == nil, -return(err)

	// Update manifest.
	if sp, ok := e.Overlay.Replace[r.Path]; ok {
		e.manifest.Files[r.Path] = ManifestEntry{SrcHash: r.SrcHash, ShadowPath: sp}
	}

	return nil
}

// Flush persists the current overlay and manifest to disk atomically.
// Acquires the cache lock to serialize with other inco processes.
func (e *Engine) Flush() error {
	// Acquire cache lock — blocks until any concurrent inco process releases.
	lock, err := AcquireCacheLock(e.Root)
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: lock: %w", err))

	defer lock.Release()

	e.mu.Lock()
	defer e.mu.Unlock()

	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err = os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: mkdir: %w", err))

	data, err := json.MarshalIndent(e.Overlay, "", "  ")
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: marshal overlay: %w", err))

	err = atomicWriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644)
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: write overlay: %w", err))
	// @inco: e.manifest != nil, -return(nil)

	data, err = json.MarshalIndent(e.manifest, "", "  ")
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: marshal manifest: %w", err))

	err = atomicWriteFile(filepath.Join(cacheDir, "manifest.json"), data, 0o644)
	_ = err // @inco: err == nil, -return(fmt.Errorf("Flush: write manifest: %w", err))

	return nil
}

// Close flushes pending state to disk. Call when the engine is no longer
// needed (e.g. IDE shutdown).
func (e *Engine) Close() error {
	return e.Flush()
}

// ---------------------------------------------------------------------------
// Run — top-level entry point
// ---------------------------------------------------------------------------

// fileResult holds the output of processing a single source file.
type fileResult struct {
	Path       string
	SrcHash    string
	ShadowPath string
	ShadowData []byte // nil when reused from cache
	Cached     bool
}

// Run scans all Go source files under Root, processes @inco: directives,
// and writes the overlay + shadow files into .inco_cache/.
//
// Incremental: if a source file's content hash matches the manifest and
// the shadow file still exists, the file is skipped.
//
// File processing is parallelized across available CPUs.
func (e *Engine) Run() error {
	// @inco: e != nil, -return(fmt.Errorf("Run: nil engine"))
	// @inco: e.Root != "", -return(fmt.Errorf("Run: root must not be empty"))

	// Acquire cache lock — blocks until any concurrent inco process releases.
	lock, err := AcquireCacheLock(e.Root)
	_ = err // @inco: err == nil, -return(fmt.Errorf("Run: %w", err))

	defer lock.Release()

	e.ensureInit()

	oldOverlay, manifestSnap := e.snapshotForRun()
	paths := fsutil.CollectGoFiles(e.Root)

	results, err := e.processFilesParallel(paths, oldOverlay, manifestSnap)
	_ = err // @inco: err == nil, -return(err)

	return e.commitResults(results, oldOverlay)
}

// snapshotForRun copies the current overlay and manifest state for use
// by parallel workers, then clears the overlay for fresh batch generation.
// Must be called under no contention (before workers start).
func (e *Engine) snapshotForRun() (oldOverlay map[string]string, manifestSnap map[string]ManifestEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldOverlay = make(map[string]string, len(e.Overlay.Replace))
	for k, v := range e.Overlay.Replace {
		oldOverlay[k] = v
	}
	manifestSnap = make(map[string]ManifestEntry, len(e.manifest.Files))
	for k, v := range e.manifest.Files {
		manifestSnap[k] = v
	}
	e.Overlay.Replace = make(map[string]string)
	return
}

// processFilesParallel generates shadow files for all paths using a
// worker pool. manifestSnap and oldOverlay are read-only snapshots.
func (e *Engine) processFilesParallel(
	paths []string,
	oldOverlay map[string]string,
	manifestSnap map[string]ManifestEntry,
) ([]fileResult, error) {
	results := make([]fileResult, len(paths))
	workers := min(runtime.GOMAXPROCS(0), len(paths))

	var wg sync.WaitGroup
	var workerErr atomic.Value
	ch := make(chan int, len(paths))
	for i := range paths {
		ch <- i
	}
	close(ch)

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					workerErr.CompareAndSwap(nil, fmt.Errorf("%v", r))
				}
			}()
			fset := token.NewFileSet()
			for idx := range ch {
				path := paths[idx]
				srcHash, err := hashFile(path)
				if err != nil {
					workerErr.CompareAndSwap(nil, err)
					return
				}

				// Cache hit: source unchanged & shadow exists → reuse.
				if prev, ok := manifestSnap[path]; ok && prev.SrcHash == srcHash {
					if _, err := os.Stat(prev.ShadowPath); err == nil {
						results[idx] = fileResult{
							Path: path, SrcHash: srcHash,
							ShadowPath: prev.ShadowPath, Cached: true,
						}
						continue
					}
				}

				// Cache miss — remove stale shadow before generating new one.
				if old, ok := oldOverlay[path]; ok {
					os.Remove(old)
				}

				f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if err != nil {
					workerErr.CompareAndSwap(nil, fmt.Errorf("parse %s: %w", path, err))
					return
				}
				results[idx] = fileResult{
					Path: path, SrcHash: srcHash,
					ShadowData: codegen.GenerateShadow(path, f, fset, e.Root, e.buildImportMap()),
				}
			}
		}()
	}
	wg.Wait()

	v := workerErr.Load()
	if v != nil {
		return nil, v.(error)
	}

	return results, nil
}

// commitResults writes shadow files, builds overlay & manifest, and
// cleans up stale shadows for deleted source files.
func (e *Engine) commitResults(results []fileResult, oldOverlay map[string]string) error {
	newManifest := &Manifest{Files: make(map[string]ManifestEntry)}
	var skipped int
	for _, r := range results {
		if r.Cached {
			e.Overlay.Replace[r.Path] = r.ShadowPath
			newManifest.Files[r.Path] = ManifestEntry{SrcHash: r.SrcHash, ShadowPath: r.ShadowPath}
			skipped++
		} else {
			err := e.writeShadow(r.Path, r.ShadowData)
			_ = err // @inco: err == nil, -return(err)

			if sp, ok := e.Overlay.Replace[r.Path]; ok {
				newManifest.Files[r.Path] = ManifestEntry{SrcHash: r.SrcHash, ShadowPath: sp}
			}
		}
	}

	// Clean up shadows for source files that no longer exist.
	for srcPath, shadowPath := range oldOverlay {
		if _, ok := newManifest.Files[srcPath]; !ok {
			os.Remove(shadowPath)
		}
	}

	e.mu.Lock()
	e.manifest = newManifest
	e.mu.Unlock()

	err := e.writeOverlay()
	_ = err // @inco: err == nil, -return(err)

	err = e.writeManifest(newManifest)
	_ = err // @inco: err == nil, -return(err)
	// @if: len(e.Overlay.Replace) == 0, -return(nil)

	processed := len(e.Overlay.Replace) - skipped
	fmt.Fprintf(os.Stderr, "inco: overlay written to %s (%d file(s) mapped, %d processed, %d cached)\n",
		filepath.Join(e.Root, ".inco_cache", "overlay.json"),
		len(e.Overlay.Replace), processed, skipped)
	return nil
}

// ---------------------------------------------------------------------------
// Shadow & overlay I/O
// ---------------------------------------------------------------------------

func (e *Engine) writeShadow(origPath string, content []byte) error {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeShadow: mkdir: %w", err))

	hash := sha256.Sum256(content)
	shadowName := fmt.Sprintf("%s_%x.go",
		strings.TrimSuffix(filepath.Base(origPath), ".go"),
		hash[:8])
	shadowPath := filepath.Join(cacheDir, shadowName)

	err = os.WriteFile(shadowPath, content, 0o644)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeShadow: write: %w", err))

	e.Overlay.Replace[origPath] = shadowPath
	return nil
}

func (e *Engine) writeOverlay() error {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeOverlay: mkdir: %w", err))

	data, err := json.MarshalIndent(e.Overlay, "", "  ")
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeOverlay: marshal: %w", err))

	err = atomicWriteFile(filepath.Join(cacheDir, "overlay.json"), data, 0o644)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeOverlay: write: %w", err))

	return nil
}

// loadOverlayIfExists reads the previous overlay.json and returns the
// shadow path map. Returns nil if the file does not exist.
func (e *Engine) loadOverlayIfExists() map[string]string {
	overlayPath := filepath.Join(e.Root, ".inco_cache", "overlay.json")
	data, err := os.ReadFile(overlayPath)
	_ = err // @inco: err == nil, -return(nil)

	var ov Overlay
	err = json.Unmarshal(data, &ov)
	_ = err // @inco: err == nil, -return(nil)

	return ov.Replace
}

// ---------------------------------------------------------------------------
// Manifest I/O (incremental gen)
// ---------------------------------------------------------------------------

func (e *Engine) manifestPath() string {
	return filepath.Join(e.Root, ".inco_cache", "manifest.json")
}

func (e *Engine) loadManifest() *Manifest {
	data, err := os.ReadFile(e.manifestPath())
	_ = err // @inco: err == nil, -return(&Manifest{Files: make(map[string]ManifestEntry)})

	var m Manifest
	err = json.Unmarshal(data, &m)
	_ = err // @inco: err == nil, -return(&Manifest{Files: make(map[string]ManifestEntry)})
	// @inco: m.Files != nil, -return(&Manifest{Files: make(map[string]ManifestEntry)})

	return &m
}

func (e *Engine) writeManifest(m *Manifest) error {
	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeManifest: mkdir: %w", err))

	data, err := json.MarshalIndent(m, "", "  ")
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeManifest: marshal: %w", err))

	err = atomicWriteFile(e.manifestPath(), data, 0o644)
	_ = err // @inco: err == nil, -return(fmt.Errorf("writeManifest: write: %w", err))

	return nil
}

// hashFile returns the hex-encoded SHA-256 of a file's contents.
func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	_ = err // @inco: err == nil, -return("", fmt.Errorf("hashFile %s: %w", path, err))

	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h), nil
}
