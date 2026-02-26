package codegen

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	"github.com/imnive-design/inco-go/internal/directive"
	"golang.org/x/tools/go/ast/astutil"
)

// pkgRefRe matches package-qualified identifiers like fmt.Errorf, errors.New.
var pkgRefRe = regexp.MustCompile(`\b([a-zA-Z_]\w*)\.\w+`)

// addMissingImports re-parses the shadow content, detects package references
// in directive action args, and adds missing imports via astutil.AddImport.
func addMissingImports(content string, origFile *ast.File, directives map[int]*directive.Directive, importMap map[string]string) string {
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
		if d.Action == directive.ActionLog {
			needed["log"] = true
			knownPaths["log"] = "log"
		}
	}
	// @if: len(needed) == 0, -return(content)

	// 1b. Exclude identifiers that are local declarations (variables,
	// parameters, receivers, etc.) — they look like pkg.Func but are
	// actually var.Field / var.Method() and must not trigger an import.
	declared := collectDeclaredNames(origFile)
	for name := range declared {
		delete(needed, name)
	}
	// @if: len(needed) == 0, -return(content)

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
	// @if: len(toAdd) == 0, -return(content)

	// 4. Re-parse the shadow content and add imports via astutil.
	fset := token.NewFileSet()
	shadowAST, err := parser.ParseFile(fset, "", content, parser.ParseComments)
	_ = err // @inco: err == nil, -return(content)

	err = safeAddImports(fset, shadowAST, toAdd, aliases, knownPaths, importMap)
	_ = err // @inco: err == nil, -return(content)

	// 5. Re-render.
	var buf strings.Builder
	err = format.Node(&buf, fset, shadowAST)
	_ = err // @inco: err == nil, -return(content)

	return buf.String()
}

// ---------------------------------------------------------------------------
// Safe import injection
// ---------------------------------------------------------------------------

// safeAddImports wraps astutil.AddImport calls with panic recovery.
// On partial/broken ASTs, astutil may panic — this prevents the entire
// generation from failing.
func safeAddImports(fset *token.FileSet, f *ast.File, toAdd []string,
	aliases map[string]string, knownPaths map[string]string,
	importMap map[string]string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("safeAddImports: %v", r)
		}
	}()

	for _, pkg := range toAdd {
		if alias, ok := aliases[pkg]; ok {
			astutil.AddNamedImport(fset, f, alias, knownPaths[pkg])
		} else if path, ok := knownPaths[pkg]; ok {
			astutil.AddImport(fset, f, path)
		} else {
			astutil.AddImport(fset, f, importMap[pkg])
		}
	}
	return nil
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
// The nil check uses plain if because fl is frequently nil (e.g. functions
// with no return values) and this guard is essential for correctness even
// when running without overlay (plain go test).
func collectFieldNames(fl *ast.FieldList, names map[string]bool) {
	// @inco: fl != nil, -return

	for _, field := range fl.List {
		for _, id := range field.Names {
			names[id.Name] = true
		}
	}
}
