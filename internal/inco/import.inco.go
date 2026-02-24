package inco

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os/exec"
	"regexp"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// ---------------------------------------------------------------------------
// Import management
// ---------------------------------------------------------------------------

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
func (e *Engine) collectPackages(ambiguous map[string]bool, patterns ...string) {
	args := append([]string{"list", "-f", "{{.Name}} {{.ImportPath}}"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = e.Root
	out, err := cmd.Output()
	_ = err // @inco: err == nil, -return
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		// @inco: line != "", -continue
		parts := strings.SplitN(line, " ", 2)
		valid := len(parts) == 2 && parts[0] != "" && parts[0] != "main"
		_ = valid // @inco: valid, -continue
		name, impPath := parts[0], parts[1]
		// Skip internal and vendored packages — they are not freely importable.
		internal := internalPkgRe.MatchString(impPath)
		_ = internal // @inco: !internal, -continue
		if existing, ok := e.importMap[name]; ok && existing != impPath {
			ambiguous[name] = true
		} else if !ambiguous[name] {
			e.importMap[name] = impPath
		}
	}
}

// pkgRefRe matches package-qualified identifiers like fmt.Errorf, errors.New.
var pkgRefRe = regexp.MustCompile(`\b([a-zA-Z_]\w*)\.\w+`)

// internalPkgRe matches import paths that are internal or vendored.
var internalPkgRe = regexp.MustCompile(`(^|/)internal/|(^|/)vendor/`)

// addMissingImports re-parses the shadow content, detects package references
// in directive action args, and adds missing imports via astutil.AddImport.
func (e *Engine) addMissingImports(content string, origFile *ast.File, directives map[int]*Directive) string {
	// 1. Collect all package-qualified identifiers from directives.
	needed := make(map[string]bool)
	// knownPaths tracks imports with well-known paths (e.g. built-in
	// actions like -log). These bypass importMap resolution because the
	// user's project may contain a local package with the same short
	// name, causing ambiguity in buildImportMap.
	knownPaths := make(map[string]string)
	for _, d := range directives {
		sources := d.ActionArgs
		if d.Expr != "" {
			sources = append(sources, d.Expr)
		}
		for _, s := range sources {
			for _, match := range pkgRefRe.FindAllStringSubmatch(s, -1) {
				needed[match[1]] = true
			}
		}
		// -log generates log.Println(...) — the "log" package reference
		// is not in the directive args, so we must add it explicitly.
		// Use knownPaths to avoid ambiguity when the project has a local
		// package also named "log".
		if d.Action == ActionLog {
			needed["log"] = true
			knownPaths["log"] = "log"
		}
	}
	// @inco: len(needed) > 0, -return(content)

	// 1b. Exclude identifiers that are local declarations (variables,
	// parameters, receivers, etc.) — they look like pkg.Func but are
	// actually var.Field / var.Method() and must not trigger an import.
	declared := collectDeclaredNames(origFile)
	for name := range declared {
		delete(needed, name)
	}
	// @inco: len(needed) > 0, -return(content)

	// 2. Determine which packages are already imported.
	imported := make(map[string]bool)
	importedPaths := make(map[string]string) // name → import path
	for _, imp := range origFile.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		// Use local name if aliased, otherwise last segment.
		var name string
		if imp.Name != nil {
			name = imp.Name.Name
		} else {
			parts := strings.Split(path, "/")
			name = parts[len(parts)-1]
		}
		imported[name] = true
		importedPaths[name] = path
	}

	// 3. Find which needed packages are missing.
	importMap := e.buildImportMap()
	var toAdd []string
	// aliases maps package short name → alias when the source file
	// already imports a different package with the same short name.
	// e.g. "log" → "_inco_log" when the file imports "mymod/log"
	// but inco needs stdlib "log" for -log action.
	aliases := make(map[string]string)
	for pkg := range needed {
		if imported[pkg] {
			// Already imported — check if it's the same package we need.
			if kp, ok := knownPaths[pkg]; ok && importedPaths[pkg] != kp {
				// Conflict: file imports a different "log" package.
				// Use alias to inject stdlib alongside it.
				alias := "_inco_" + pkg
				aliases[pkg] = alias
				toAdd = append(toAdd, pkg)
			}
			continue
		}
		if _, ok := knownPaths[pkg]; ok {
			toAdd = append(toAdd, pkg)
		} else if _, ok := importMap[pkg]; ok {
			toAdd = append(toAdd, pkg)
		}
	}
	// @inco: len(toAdd) > 0, -return(content)

	// 4. Re-parse the shadow content and add imports via astutil.
	fset := token.NewFileSet()
	shadowAST, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	_ = err // @inco: err == nil, -return(content)
	for _, pkg := range toAdd {
		if alias, ok := aliases[pkg]; ok {
			astutil.AddNamedImport(fset, shadowAST, alias, knownPaths[pkg])
		} else if path, ok := knownPaths[pkg]; ok {
			astutil.AddImport(fset, shadowAST, path)
		} else {
			astutil.AddImport(fset, shadowAST, importMap[pkg])
		}
	}

	// 5. Re-render.
	var buf strings.Builder
	err = format.Node(&buf, fset, shadowAST)
	_ = err // @inco: err == nil, -return(content)
	return buf.String()
}

// ---------------------------------------------------------------------------
// AST helpers (import-related)
// ---------------------------------------------------------------------------

// collectDeclaredNames returns names of all variables, parameters, receivers,
// constants, and types declared in the file. These must not be mistaken for
// package references when auto-importing (e.g. myVar.Field vs pkg.Func).
func collectDeclaredNames(f *ast.File) map[string]bool {
	names := make(map[string]bool)
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncDecl:
			if x.Recv != nil {
				for _, field := range x.Recv.List {
					for _, id := range field.Names {
						names[id.Name] = true
					}
				}
			}
			collectFieldNames(x.Type.Params, names)
			collectFieldNames(x.Type.Results, names)
		case *ast.FuncLit:
			collectFieldNames(x.Type.Params, names)
			collectFieldNames(x.Type.Results, names)
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		case *ast.ValueSpec:
			for _, id := range x.Names {
				names[id.Name] = true
			}
		case *ast.TypeSpec:
			names[x.Name.Name] = true
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				if id, ok := x.Key.(*ast.Ident); ok {
					names[id.Name] = true
				}
				if x.Value != nil {
					if id, ok := x.Value.(*ast.Ident); ok {
						names[id.Name] = true
					}
				}
			}
		}
		return true
	})
	return names
}

// collectFieldNames adds all named fields from a field list to the set.
func collectFieldNames(fl *ast.FieldList, names map[string]bool) {
	// @inco: fl != nil, -return
	for _, field := range fl.List {
		for _, id := range field.Names {
			names[id.Name] = true
		}
	}
}
