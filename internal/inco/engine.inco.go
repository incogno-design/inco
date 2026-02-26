package inco

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
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
	// @inco: !e.inited, -return(nil)

	return e.initLocked()
}

func (e *Engine) initLocked() error {
	e.manifest = e.loadManifest()
	// Ensure manifest.Files is never nil (loadManifest may return nil map
	// when running without overlay and the manifest file doesn't exist).
	if e.manifest.Files == nil {
		e.manifest.Files = make(map[string]ManifestEntry)
	}

	prev := e.loadOverlayIfExists()
	if prev != nil {
		for k, v := range prev {
			e.Overlay.Replace[k] = v
		}
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
	// @inco: !e.inited, -return

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
	if f == nil {
		return nil, fmt.Errorf("GenFile: unparseable %s: %w", absPath, parseErr)
	}

	shadow := e.generateShadow(absPath, f, fset)
	return &GenFileResult{
		Path:       absPath,
		SrcHash:    srcHash,
		ShadowData: shadow,
	}, nil
}

// CommitFile writes the shadow file to .inco_cache and updates the
// in-memory overlay and manifest. Call Flush to persist to disk.
func (e *Engine) CommitFile(r *GenFileResult) error {
	if r == nil {
		return fmt.Errorf("CommitFile: nil result")
	}

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
func (e *Engine) Flush() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	cacheDir := filepath.Join(e.Root, ".inco_cache")
	err := os.MkdirAll(cacheDir, 0o755)
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

	e.ensureInit()

	// Snapshot state for workers (avoid lock contention).
	e.mu.Lock()
	oldOverlay := make(map[string]string, len(e.Overlay.Replace))
	for k, v := range e.Overlay.Replace {
		oldOverlay[k] = v
	}
	manifestSnap := make(map[string]ManifestEntry, len(e.manifest.Files))
	for k, v := range e.manifest.Files {
		manifestSnap[k] = v
	}
	// Clear overlay for fresh batch generation — commitResults will repopulate.
	e.Overlay.Replace = make(map[string]string)
	e.mu.Unlock()

	paths := collectGoFiles(e.Root)

	// Process files concurrently.
	results := make([]fileResult, len(paths))
	workers := min(runtime.GOMAXPROCS(0), len(paths))

	var wg sync.WaitGroup
	var workerErr atomic.Value // stores first error from a worker
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
			// Each goroutine gets its own fset to avoid contention.
			fset := token.NewFileSet()
			for idx := range ch {
				path := paths[idx]
				srcHash, err := hashFile(path)
				if err != nil {
					workerErr.CompareAndSwap(nil, err)
					return
				}

				// Check cache: source unchanged & shadow file exists → reuse.
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

				// Parse and process.
				f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
				if err != nil {
					workerErr.CompareAndSwap(nil, fmt.Errorf("parse %s: %w", path, err))
					return
				}
				shadowData := e.generateShadow(path, f, fset)
				results[idx] = fileResult{
					Path: path, SrcHash: srcHash,
					ShadowData: shadowData,
				}
			}
		}()
	}
	wg.Wait()

	v := workerErr.Load()
	_ = v // @inco: v == nil, -return(v.(error))

	return e.commitResults(results, oldOverlay)
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
	// @inco: len(e.Overlay.Replace) > 0, -return(nil)

	processed := len(e.Overlay.Replace) - skipped
	fmt.Fprintf(os.Stderr, "inco: overlay written to %s (%d file(s) mapped, %d processed, %d cached)\n",
		filepath.Join(e.Root, ".inco_cache", "overlay.json"),
		len(e.Overlay.Replace), processed, skipped)
	return nil
}

// ---------------------------------------------------------------------------
// File processing
// ---------------------------------------------------------------------------

// generateShadow produces the shadow file content for a source file.
// It is safe to call from multiple goroutines — it only reads e.Root
// and uses the provided fset.
func (e *Engine) generateShadow(path string, f *ast.File, fset *token.FileSet) []byte {
	// @inco: path != "", -panic("generateShadow: empty path")
	// @inco: f != nil, -panic("generateShadow: nil AST")

	// 1. Collect directive lines from AST comments.
	directives := make(map[int]*Directive) // 1-based line → Directive
	CollectDirectives(f, func(c *ast.Comment, d *Directive) {
		line := fset.Position(c.Pos()).Line
		directives[line] = d
	})

	// 2. Read source as lines.
	src, err := os.ReadFile(path)
	_ = err // @inco: err == nil, -panic(err)

	lines := strings.Split(string(src), "\n")

	// 3. Classify directives as standalone or inline using AST.
	standalone := make(map[int]*Directive)
	inline := make(map[int]*Directive)

	stmtLines := collectStmtLines(f, fset)
	for lineNum, d := range directives {
		idx := lineNum - 1
		// @inco: idx >= 0 && idx < len(lines), -continue

		trimmed := strings.TrimSpace(lines[idx])
		isCommentLine := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
		if isCommentLine {
			standalone[lineNum] = d
		} else if stmtLines[lineNum] {
			inline[lineNum] = d
		}
	}

	// 3b. Determine the effective package name for -log action.
	//     If the file already imports a non-stdlib "log", we'll need an alias.
	logPkgName := "log"
	hasLogAction := false
	for _, d := range directives {
		if d.Action == ActionLog {
			hasLogAction = true
			break
		}
	}
	if hasLogAction {
		for _, imp := range f.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			var name string
			if imp.Name != nil {
				name = imp.Name.Name
			} else {
				parts := strings.Split(impPath, "/")
				name = parts[len(parts)-1]
			}
			if name == "log" && impPath != "log" {
				logPkgName = "_inco_log"
				break
			}
		}
	}

	// 4. Build output.
	var output []string
	prevWasDirective := false

	for idx, line := range lines {
		lineNum := idx + 1

		if d, ok := standalone[lineNum]; ok {
			indent := extractIndent(line)
			output = append(output, fmt.Sprintf("//line %s:%d", path, lineNum))
			output = append(output, e.generateIfBlock(d, indent, path, lineNum, logPkgName))
			prevWasDirective = true
		} else if d, ok := inline[lineNum]; ok {
			output = append(output, line)
			indent := extractIndent(line)
			output = append(output, e.generateIfBlock(d, indent, path, lineNum, logPkgName))
			prevWasDirective = true
		} else {
			if prevWasDirective {
				output = append(output, fmt.Sprintf("//line %s:%d", path, lineNum))
				prevWasDirective = false
			}
			output = append(output, line)
		}
	}

	// 5. Add missing imports.
	content := strings.Join(output, "\n")
	content = e.addMissingImports(content, f, directives)

	return []byte(content)
}

// ---------------------------------------------------------------------------
// Code generation
// ---------------------------------------------------------------------------

// generateIfBlock returns the text of the injected if-statement.
//
//	if !(expr) {
//	    panic(...)
//	}
func (e *Engine) generateIfBlock(d *Directive, indent, path string, line int, logPkgName string) string {
	var cond string
	if d.Negated {
		cond = d.Expr
	} else {
		cond = fmt.Sprintf("!(%s)", d.Expr)
	}
	body := e.buildPanicBody(d, path, line, logPkgName)
	return fmt.Sprintf("%sif %s {\n%s\t%s\n%s}", indent, cond, indent, body, indent)
}

// buildPanicBody generates the action statement for @inco:.
//
//   - ActionReturn + args → return arg0, arg1, ...
//   - ActionReturn bare   → return
//   - ActionContinue      → continue
//   - ActionDo + args     → args[0]; args[1]; ...
//   - ActionBreak         → break
//   - ActionPanic + args  → panic(arg)
//   - ActionPanic default → panic("inco violation: <expr> (at file:line)")
func (e *Engine) buildPanicBody(d *Directive, path string, line int, logPkgName string) string {
	switch d.Action {
	case ActionReturn:
		// @inco: len(d.ActionArgs) == 0, -return("return " + strings.Join(d.ActionArgs, ", "))

		return "return"
	case ActionContinue:
		return "continue"
	case ActionBreak:
		return "break"
	case ActionDo:
		return strings.Join(d.ActionArgs, "; ")
	case ActionLog:
		return logPkgName + ".Println(" + strings.Join(d.ActionArgs, ", ") + ")"
	default: // ActionPanic
		// @inco: len(d.ActionArgs) == 0, -return("panic(" + d.ActionArgs[0] + ")")

		relPath := path
		if rel, err := filepath.Rel(e.Root, path); err == nil {
			relPath = rel
		}
		msg := fmt.Sprintf("inco violation: %s (at %s:%d)", d.Expr, relPath, line)
		return fmt.Sprintf("panic(%q)", msg)
	}
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

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

// extractIndent returns the leading whitespace of a line.
func extractIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// collectStmtLines walks the AST and returns a set of line numbers that
// contain statements inside function bodies. A directive comment whose
// line appears in this set is classified as "inline" rather than "standalone".
func collectStmtLines(f *ast.File, fset *token.FileSet) map[int]bool {
	lines := make(map[int]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		// @inco: n != nil, -return(false)

		switch n.(type) {
		case *ast.AssignStmt, *ast.ExprStmt, *ast.ReturnStmt,
			*ast.IncDecStmt, *ast.SendStmt, *ast.GoStmt, *ast.DeferStmt,
			*ast.BranchStmt:
			lines[fset.Position(n.Pos()).Line] = true
		}
		return true
	})
	return lines
}
