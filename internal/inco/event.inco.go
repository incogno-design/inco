package inco

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// FileEvent — the unit of change notification
// ---------------------------------------------------------------------------

// FileEventKind identifies the type of file system event.
type FileEventKind int

const (
	EventCreate FileEventKind = iota
	EventModify
	EventDelete
)

var eventNames = [...]string{"create", "modify", "delete"}

func (k FileEventKind) String() string {
	if int(k) < len(eventNames) {
		return eventNames[k]
	}
	return "unknown"
}

// FileEvent represents a single file-system change notification.
// Path must be absolute.
type FileEvent struct {
	Kind FileEventKind
	Path string
}

// ---------------------------------------------------------------------------
// HandleEvent — process a single file event
// ---------------------------------------------------------------------------

// HandleEvent processes a file system event, returning any generation error.
// For create/modify events, it runs GenFile + CommitFile.
// For delete events, it removes the shadow entry.
// Non-.go files and test files are silently ignored.
// A go.mod change invalidates the import resolver cache.
func (e *Engine) HandleEvent(ev FileEvent) error {
	e.ensureInit()

	// go.mod change → invalidate import map.
	if filepath.Base(ev.Path) == "go.mod" {
		e.InvalidateImports()
		return nil
	}

	// Only process .go source files (not test files, not already in cache).
	if !isGoSource(ev.Path) {
		return nil
	}

	switch ev.Kind {
	case EventCreate, EventModify:
		r, err := e.GenFile(ev.Path)
		if err != nil {
			return fmt.Errorf("HandleEvent %s %s: %w", ev.Kind, ev.Path, err)
		}
		// nil result means cache hit — no work needed.
		if r == nil {
			return nil
		}
		return e.CommitFile(r)

	case EventDelete:
		return e.DeleteFile(ev.Path)

	default:
		return fmt.Errorf("HandleEvent: unknown event kind %d", ev.Kind)
	}
}

// ---------------------------------------------------------------------------
// DeleteFile — remove shadow for a deleted source
// ---------------------------------------------------------------------------

// DeleteFile removes the shadow file and overlay/manifest entries for the
// given source path. Safe to call even if the source was never processed.
func (e *Engine) DeleteFile(path string) error {
	e.ensureInit()

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("DeleteFile: abs: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	shadowPath, ok := e.Overlay.Replace[absPath]
	if !ok {
		return nil // nothing to clean up
	}

	os.Remove(shadowPath)
	delete(e.Overlay.Replace, absPath)
	if e.manifest != nil {
		delete(e.manifest.Files, absPath)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Reconcile — sync manifest with disk reality
// ---------------------------------------------------------------------------

// Reconcile compares the current manifest against actual filesystem state
// and fixes any drift:
//   - Source files that no longer exist → shadow removed
//   - Source files not in manifest → generated
//   - Stale shadows (hash mismatch) → regenerated
//
// Returns the number of files that were (re)generated.
func (e *Engine) Reconcile() (int, error) {
	e.ensureInit()

	// 1. Remove entries for deleted source files.
	e.mu.Lock()
	for src, entry := range e.manifest.Files {
		if _, err := os.Stat(src); err != nil {
			os.Remove(entry.ShadowPath)
			delete(e.manifest.Files, src)
			delete(e.Overlay.Replace, src)
		}
	}
	e.mu.Unlock()

	// 2. Walk current source files and (re)generate as needed.
	paths := collectGoFiles(e.Root)
	generated := 0

	for _, path := range paths {
		r, err := e.GenFile(path)
		if err != nil {
			return generated, fmt.Errorf("Reconcile: %w", err)
		}
		if r == nil {
			continue // cache hit
		}
		if err := e.CommitFile(r); err != nil {
			return generated, fmt.Errorf("Reconcile: %w", err)
		}
		generated++
	}

	return generated, nil
}

// ---------------------------------------------------------------------------
// InvalidateImports — reset the import resolver cache
// ---------------------------------------------------------------------------

// InvalidateImports forces the import map to be rebuilt on the next
// GenFile / Run call. Call this when go.mod or go.sum changes.
func (e *Engine) InvalidateImports() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.importOnce = sync.Once{}
	e.importMap = nil
}

// ---------------------------------------------------------------------------
// isGoSource — filter helper
// ---------------------------------------------------------------------------

// isGoSource reports whether path looks like a non-test Go source file
// that inco should process.
func isGoSource(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	if strings.HasSuffix(path, "_test.go") {
		return false
	}
	// Skip files inside .inco_cache.
	if strings.Contains(path, ".inco_cache") {
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Debouncer — coalesce rapid events
// ---------------------------------------------------------------------------

// Debouncer coalesces rapid file events. When an event arrives for a path,
// it waits for [delay] of quiet before calling the handler. Subsequent
// events for the same path reset the timer.
type Debouncer struct {
	delay   time.Duration
	handler func(FileEvent)

	mu     sync.Mutex
	timers map[string]*time.Timer
	latest map[string]FileEvent // most recent event per path
}

// NewDebouncer creates a debouncer with the given quiet period and handler.
func NewDebouncer(delay time.Duration, handler func(FileEvent)) *Debouncer {
	return &Debouncer{
		delay:   delay,
		handler: handler,
		timers:  make(map[string]*time.Timer),
		latest:  make(map[string]FileEvent),
	}
}

// Send queues a file event. The handler will fire after [delay] ms of
// quiet for this path. If a new event arrives before the timer fires,
// the timer resets and the latest event is used.
func (d *Debouncer) Send(ev FileEvent) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.latest[ev.Path] = ev

	if t, ok := d.timers[ev.Path]; ok {
		t.Reset(d.delay)
		return
	}

	path := ev.Path // capture for closure
	d.timers[path] = time.AfterFunc(d.delay, func() {
		d.mu.Lock()
		latest := d.latest[path]
		delete(d.timers, path)
		delete(d.latest, path)
		d.mu.Unlock()

		d.handler(latest)
	})
}

// Stop cancels all pending timers.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for path, t := range d.timers {
		t.Stop()
		delete(d.timers, path)
		delete(d.latest, path)
	}
}

// ---------------------------------------------------------------------------
// scheduleFlush — auto-flush after quiet period
// ---------------------------------------------------------------------------

// ScheduleFlush arranges for Flush to be called after [delay] of quiet.
// Each call resets the timer. Useful for batching overlay writes during
// rapid editing. Thread-safe.
func (e *Engine) ScheduleFlush(delay time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.flushTimer != nil {
		e.flushTimer.Reset(delay)
		return
	}

	e.flushTimer = time.AfterFunc(delay, func() {
		e.mu.Lock()
		e.flushTimer = nil
		e.mu.Unlock()
		e.Flush()
	})
}

// CancelFlush cancels any pending scheduled flush.
func (e *Engine) CancelFlush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.flushTimer != nil {
		e.flushTimer.Stop()
		e.flushTimer = nil
	}
}
