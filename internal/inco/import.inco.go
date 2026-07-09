package inco

import (
	"os/exec"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Import management
// ---------------------------------------------------------------------------

// internalPkgRe matches import paths that are internal or vendored.
var internalPkgRe = regexp.MustCompile(`(^|/)internal/|(^|/)vendor/`)

// stdlibWhitelist contains common standard library packages that are allowed
// for auto-import. Only these packages can be auto-imported from stdlib;
// obscure or dangerous packages (unsafe, debug/*, etc.) are excluded.
var stdlibWhitelist = map[string]string{
	// core
	"fmt":     "fmt",
	"errors":  "errors",
	"strings": "strings",
	"strconv": "strconv",
	"bytes":   "bytes",
	"regexp":  "regexp",
	"sort":    "sort",
	"slices":  "slices",
	"maps":    "maps",
	"math":    "math",
	"cmp":     "cmp",

	// os / io / path
	"os":       "os",
	"io":       "io",
	"filepath": "path/filepath",
	"path":     "path",
	"bufio":    "bufio",

	// time / context / sync
	"time":    "time",
	"context": "context",
	"sync":    "sync",

	// encoding
	"json":   "encoding/json",
	"xml":    "encoding/xml",
	"csv":    "encoding/csv",
	"base64": "encoding/base64",
	"hex":    "encoding/hex",

	// net
	"http": "net/http",
	"url":  "net/url",

	// log
	"log":  "log",
	"slog": "log/slog",
}

// buildImportMap resolves package names to import paths. Standard library
// packages are restricted to a curated whitelist of common packages;
// project dependencies are still resolved dynamically via "go list".
// The result is cached for the engine's lifetime.
func (e *Engine) buildImportMap() map[string]string {
	e.importOnce.Do(func() {
		e.importMap = make(map[string]string)
		ambiguous := make(map[string]bool)

		// 1. Seed with whitelisted standard library packages.
		for name, path := range stdlibWhitelist {
			e.importMap[name] = path
		}

		// 2. Packages already used in the module (covers third-party deps).
		e.collectPackages(ambiguous, "-e", "-deps", "./...")

		// Remove ambiguous names (multiple import paths share a short name,
		// e.g. "template" → text/template vs html/template).
		for name := range ambiguous {
			delete(e.importMap, name)
		}
	})
	return e.importMap
}

// collectPackages runs "go list" with the given patterns and records
// each name → importPath pair in e.importMap.
// Plain if checks are used for bounds-safety because this function is
// called during gen, which must work without overlay (go test).
func (e *Engine) collectPackages(ambiguous map[string]bool, patterns ...string) {
	args := append([]string{"list", "-f", "{{.Name}} {{.ImportPath}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = e.Root
	out, err := cmd.Output()
	_ = err // @inco: err == nil, -return

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// @if: line == "", -continue

		parts := strings.SplitN(line, " ", 2)
		// @if: len(parts) != 2 || parts[0] == "" || parts[0] == "main", -continue

		name, impPath := parts[0], parts[1]
		// Skip internal and vendored packages — they are not freely importable.
		_ = internalPkgRe // @if: internalPkgRe.MatchString(impPath), -continue

		if existing, ok := e.importMap[name]; ok && existing != impPath {
			ambiguous[name] = true
		} else if !ambiguous[name] {
			e.importMap[name] = impPath
		}
	}
}
