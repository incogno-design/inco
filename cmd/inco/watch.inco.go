package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	inco "github.com/imnive-design/inco-go/internal/inco"
)

// runWatch starts a file watcher on the project directory and
// incrementally regenerates shadows when .go files change.
func runWatch(dir string) {
	absDir, err := filepath.Abs(dir)
	// @inco: err == nil, -panic(err)
	_ = err

	e := inco.NewEngine(absDir)
	err = e.Init()
	// @inco: err == nil, -panic(err)
	_ = err

	// Initial full gen.
	err = e.Run()
	// @inco: err == nil, -panic(err)
	_ = err

	watcher, err := fsnotify.NewWatcher()
	// @inco: err == nil, -panic(fmt.Sprintf("watch: %v", err))
	_ = err

	defer watcher.Close()

	// Watch project directories (recursively).
	err = addDirsRecursive(watcher, absDir)
	// @inco: err == nil, -panic(fmt.Sprintf("watch: add dirs: %v", err))
	_ = err

	fmt.Fprintf(os.Stderr, "inco: watching %s for changes...\n", absDir)

	const flushDelay = 200 * time.Millisecond

	debouncer := inco.NewDebouncer(100*time.Millisecond, func(ev inco.FileEvent) {
		if err := e.HandleEvent(ev); err != nil {
			fmt.Fprintf(os.Stderr, "inco: %v\n", err)
			return
		}
		e.ScheduleFlush(flushDelay)

		rel, _ := filepath.Rel(absDir, ev.Path)
		fmt.Fprintf(os.Stderr, "inco: %s %s\n", ev.Kind, rel)
	})
	defer debouncer.Stop()

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			ev, skip := translateEvent(event, absDir)
			if skip {
				continue
			}
			debouncer.Send(ev)

			// If a new directory was created, start watching it.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					watcher.Add(event.Name)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "inco: watcher error: %v\n", err)
		}
	}
}

// translateEvent converts an fsnotify event to a FileEvent.
// Returns skip=true if the event should be ignored.
func translateEvent(event fsnotify.Event, root string) (inco.FileEvent, bool) {
	path := event.Name

	// Skip non-.go files, test files, and cache files.
	if !strings.HasSuffix(path, ".go") {
		// Still check for go.mod changes.
		if filepath.Base(path) == "go.mod" {
			return inco.FileEvent{Kind: inco.EventModify, Path: path}, false
		}
		return inco.FileEvent{}, true
	}
	if strings.HasSuffix(path, "_test.go") {
		return inco.FileEvent{}, true
	}
	if strings.Contains(path, ".inco_cache") {
		return inco.FileEvent{}, true
	}

	var kind inco.FileEventKind
	switch {
	case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
		kind = inco.EventDelete
	case event.Has(fsnotify.Create):
		kind = inco.EventCreate
	case event.Has(fsnotify.Write):
		kind = inco.EventModify
	default:
		return inco.FileEvent{}, true
	}

	return inco.FileEvent{Kind: kind, Path: path}, false
}

// addDirsRecursive walks root and adds all directories to the watcher,
// skipping hidden dirs, vendor, testdata, and .inco_cache.
func addDirsRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}
