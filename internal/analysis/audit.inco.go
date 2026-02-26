package analysis

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/imnive-design/inco-go/internal/inco"
)

// ---------------------------------------------------------------------------
// Audit types
// ---------------------------------------------------------------------------

// FuncAudit holds per-function audit data.
type FuncAudit struct {
	Name         string // function name (or "func literal" for closures)
	Line         int    // 1-based line number of declaration
	RequireCount int    // number of require directives in this function
}

// FileAudit holds per-file audit data.
type FileAudit struct {
	Path         string      // absolute path
	RelPath      string      // relative to root
	Funcs        []FuncAudit // declared functions
	IfCount      int         // native if statements
	RequireCount int         // total directives (@inco: + @if:)
	IncoCount    int         // @inco: directives only
	IfDirCount   int         // @if: directives only
	SpacedCount  int         // directives followed by a blank line
}

// AuditResult is the aggregate report.
type AuditResult struct {
	Files           []FileAudit
	IgnoredPaths    []string // files/dirs skipped by .incoignore
	TotalFiles      int
	TotalFuncs      int
	GuardedFuncs    int // functions with >= 1 @inco: directive
	TotalIfs        int
	TotalDirectives int
	TotalInco       int // @inco: count
	TotalIfDir      int // @if: count
	TotalSpaced     int // directives followed by a blank line
}

// ---------------------------------------------------------------------------
// Audit entry point
// ---------------------------------------------------------------------------

// Audit scans all Go source files under root and produces an AuditResult
// summarising @inco: coverage and directive-vs-if ratios.
func Audit(root string) (*AuditResult, error) {
	// @inco: root != "", -return(nil, fmt.Errorf("Audit: root must not be empty"))

	absRoot, err := filepath.Abs(root)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("Audit: %w", err))

	fset := token.NewFileSet()
	var files []FileAudit
	var ignored []string

	inco.WalkGoFiles(absRoot, func(path string) error {
		fa := auditFile(fset, absRoot, path)
		files = append(files, fa)
		return nil
	})

	// Collect ignored paths by walking all .go files and checking .incoignore.
	collectIgnored(absRoot, &ignored)

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })

	r := &AuditResult{Files: files, IgnoredPaths: ignored, TotalFiles: len(files)}
	for _, f := range files {
		r.TotalIfs += f.IfCount
		r.TotalDirectives += f.RequireCount
		r.TotalInco += f.IncoCount
		r.TotalIfDir += f.IfDirCount
		r.TotalSpaced += f.SpacedCount
		for _, fn := range f.Funcs {
			r.TotalFuncs++
			if fn.RequireCount > 0 {
				r.GuardedFuncs++
			}
		}
	}
	return r, nil
}

// ---------------------------------------------------------------------------
// Per-file analysis
// ---------------------------------------------------------------------------

// collectIgnored walks root and appends relative paths of files/dirs
// that are skipped by .incoignore (but not by skipDirRe, which covers
// hidden dirs, vendor, testdata — those are always skipped).
func collectIgnored(root string, out *[]string) {
	ig := inco.NewIgnoreTree(root)
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		// @inco: err == nil, -return(nil)

		if d.IsDir() {
			// @inco: !inco.SkipDir(d.Name()), -return(filepath.SkipDir)

			ig.LeaveDir(path)
			ig.EnterDir(path)
			// @inco: ig.Match(path, true), -return(nil)

			rel, _ := filepath.Rel(root, path)
			*out = append(*out, rel+"/")
			return filepath.SkipDir
		}
		// @inco: inco.IsGoSource(d.Name()) && !inco.IsTestFile(d.Name()), -return(nil)

		if ig.Match(path, false) {
			rel, _ := filepath.Rel(root, path)
			*out = append(*out, rel)
		}
		return nil
	})
	sort.Strings(*out)
}

func auditFile(fset *token.FileSet, root, path string) FileAudit {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	_ = err // @inco: err == nil, -panic(err)

	relPath := path
	if rel, e := filepath.Rel(root, path); e == nil {
		relPath = rel
	}

	fa := FileAudit{Path: path, RelPath: relPath}

	// 1. Parse directives from comments.
	type directiveInfo struct {
		pos token.Pos
	}
	var directives []directiveInfo

	inco.CollectDirectives(f, func(c *ast.Comment, d *inco.Directive) {
		fa.RequireCount++
		if d.Negated {
			fa.IfDirCount++
		} else {
			fa.IncoCount++
		}
		directives = append(directives, directiveInfo{pos: c.Pos()})
	})

	// 1b. Count directives followed by a blank line.
	src, readErr := os.ReadFile(path)
	if readErr == nil {
		srcLines := strings.Split(string(src), "\n")
		directiveLines := make(map[int]bool)
		for _, di := range directives {
			directiveLines[fset.Position(di.pos).Line] = true
		}
		for lineNum := range directiveLines {
			idx := lineNum // 0-based index of next line
			if idx < len(srcLines) && strings.TrimSpace(srcLines[idx]) == "" {
				fa.SpacedCount++
			}
		}
	}

	// 2. Count if statements.
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := n.(*ast.IfStmt); ok {
			fa.IfCount++
		}
		return true
	})

	// 3. Collect functions and map @inco: to enclosing function.
	type funcRange struct {
		name  string
		line  int
		start token.Pos
		end   token.Pos
	}
	var funcRanges []funcRange

	ast.Inspect(f, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			// @inco: fn.Body != nil, -return(true)

			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				name = recvTypeName(fn.Recv.List[0].Type) + "." + name
			}
			funcRanges = append(funcRanges, funcRange{
				name:  name,
				line:  fset.Position(fn.Pos()).Line,
				start: fn.Body.Pos(),
				end:   fn.Body.End(),
			})
		case *ast.FuncLit:
			// @inco: fn.Body != nil, -return(true)

			funcRanges = append(funcRanges, funcRange{
				name:  "func literal",
				line:  fset.Position(fn.Pos()).Line,
				start: fn.Body.Pos(),
				end:   fn.Body.End(),
			})
		}
		return true
	})

	// Map each @inco: to its enclosing function.
	requireCounts := make(map[int]int) // funcRanges index -> count
	for _, d := range directives {
		// Find innermost enclosing function.
		bestIdx := -1
		for i, fr := range funcRanges {
			if fr.start <= d.pos && d.pos <= fr.end {
				if bestIdx == -1 || funcRanges[bestIdx].start < fr.start {
					bestIdx = i
				}
			}
		}
		if bestIdx >= 0 {
			requireCounts[bestIdx]++
		}
	}

	for i, fr := range funcRanges {
		fa.Funcs = append(fa.Funcs, FuncAudit{
			Name:         fr.name,
			Line:         fr.line,
			RequireCount: requireCounts[i],
		})
	}

	return fa
}

// recvTypeName extracts the type name from a method receiver expression.
func recvTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return recvTypeName(t.X)
	case *ast.IndexExpr:
		return recvTypeName(t.X)
	case *ast.IndexListExpr:
		return recvTypeName(t.X)
	}
	return "?"
}

// ---------------------------------------------------------------------------
// Report rendering
// ---------------------------------------------------------------------------

// PrintReport writes a human-readable audit report to w.
func (r *AuditResult) PrintReport(w io.Writer) {
	fmt.Fprintf(w, "inco audit — contract coverage report\n")
	fmt.Fprintf(w, "======================================\n\n")

	fmt.Fprintf(w, "  Files scanned:  %d\n", r.TotalFiles)
	fmt.Fprintf(w, "  Functions:      %d\n\n", r.TotalFuncs)

	// --- @inco: coverage ---
	fmt.Fprintf(w, "@inco: coverage:\n")
	if r.TotalFuncs > 0 {
		pct := float64(r.GuardedFuncs) / float64(r.TotalFuncs) * 100
		fmt.Fprintf(w, "  With @inco::     %d / %d  (%.1f%%)\n", r.GuardedFuncs, r.TotalFuncs, pct)
		fmt.Fprintf(w, "  Without @inco::  %d / %d  (%.1f%%)\n\n",
			r.TotalFuncs-r.GuardedFuncs, r.TotalFuncs, 100-pct)
	} else {
		fmt.Fprintf(w, "  (no functions found)\n\n")
	}

	// --- Directive vs if ---
	fmt.Fprintf(w, "Directive vs if:\n")
	fmt.Fprintf(w, "  @inco::             %d\n", r.TotalInco)
	fmt.Fprintf(w, "  @if::               %d\n", r.TotalIfDir)
	fmt.Fprintf(w, "  ─────────────────────\n")
	fmt.Fprintf(w, "  Total directives:   %d\n", r.TotalDirectives)
	fmt.Fprintf(w, "  Native if stmts:    %d\n", r.TotalIfs)
	total := r.TotalDirectives + r.TotalIfs
	if total > 0 {
		ratio := float64(r.TotalDirectives) / float64(total) * 100
		fmt.Fprintf(w, "  inco/(if+inco):     %.1f%%\n\n", ratio)
	} else {
		fmt.Fprintf(w, "  inco/(if+inco):     — (no directives or if statements)\n\n")
	}

	// --- Spacing ---
	fmt.Fprintf(w, "Directive spacing:\n")
	fmt.Fprintf(w, "  Spaced (blank after):  %d / %d", r.TotalSpaced, r.TotalDirectives)
	if r.TotalDirectives > 0 {
		pct := float64(r.TotalSpaced) / float64(r.TotalDirectives) * 100
		fmt.Fprintf(w, "  (%.1f%%)", pct)
	}
	fmt.Fprintf(w, "\n  Run 'inco fmt' to normalize spacing.\n\n")

	// --- Per-file breakdown ---
	fmt.Fprintf(w, "Per-file breakdown:\n")
	// Calculate column widths.
	maxPath := 4 // "File"
	for _, f := range r.Files {
		if len(f.RelPath) > maxPath {
			maxPath = len(f.RelPath)
		}
	}
	if maxPath > 50 {
		maxPath = 50
	}

	fmt.Fprintf(w, "  %-*s  @inco:  if  funcs  guarded\n", maxPath, "File")
	fmt.Fprintf(w, "  %s  %s\n", strings.Repeat("─", maxPath), "──────  ──  ─────  ───────")
	for _, f := range r.Files {
		guarded := 0
		for _, fn := range f.Funcs {
			if fn.RequireCount > 0 {
				guarded++
			}
		}
		display := f.RelPath
		if len(display) > maxPath {
			display = "…" + display[len(display)-maxPath+1:]
		}
		fmt.Fprintf(w, "  %-*s  %7d  %2d  %5d  %7d\n",
			maxPath, display, f.RequireCount,
			f.IfCount, len(f.Funcs), guarded)
	}

	// --- Unguarded functions ---
	var unguarded []string
	for _, f := range r.Files {
		for _, fn := range f.Funcs {
			if fn.RequireCount == 0 && fn.Name != "func literal" {
				unguarded = append(unguarded, fmt.Sprintf("  %s:%d  %s", f.RelPath, fn.Line, fn.Name))
			}
		}
	}
	if len(unguarded) > 0 {
		fmt.Fprintf(w, "\nFunctions without @inco: (%d):\n", len(unguarded))
		for _, s := range unguarded {
			fmt.Fprintln(w, s)
		}
	}

	// --- Ignored paths ---
	if len(r.IgnoredPaths) > 0 {
		fmt.Fprintf(w, "\nIgnored by .incoignore (%d):\n", len(r.IgnoredPaths))
		for _, p := range r.IgnoredPaths {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
}
