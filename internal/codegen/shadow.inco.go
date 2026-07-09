// Package codegen produces shadow file content from parsed Go source files
// and their @inco:/@if: directives.
package codegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/incogno-design/inco/internal/directive"
)

// GenerateShadow produces the shadow file content for a source file.
// src is the raw content of path (already read by the caller, so this
// function performs no I/O). It is safe to call from multiple goroutines —
// it only reads the provided parameters.
func GenerateShadow(path string, src []byte, f *ast.File, fset *token.FileSet, root string, importMap map[string]string) []byte {
	// @inco: path != "", -panic("GenerateShadow: empty path")
	// @inco: f != nil, -panic("GenerateShadow: nil AST")

	// 1. Collect directive lines from AST comments.
	directives := make(map[int]*directive.Directive) // 1-based line → Directive
	directive.CollectDirectives(f, func(c *ast.Comment, d *directive.Directive) {
		directives[fset.Position(c.Pos()).Line] = d
	})

	// 2. Split source into lines.
	lines := strings.Split(string(src), "\n")

	// 3. Classify directives and resolve log package name.
	standalone, inline := classifyDirectives(directives, lines, f, fset)
	logPkgName := resolveLogPkgName(directives, f)

	// 4. Build output lines with injected if-blocks.
	output := buildShadowLines(lines, standalone, inline, path, logPkgName, root)

	// 5. Add missing imports.
	content := strings.Join(output, "\n")
	content = addMissingImports(content, f, directives, importMap)

	return []byte(content)
}

// classifyDirectives splits directives into standalone (entire line is a
// comment) and inline (appended to a code statement) maps.
func classifyDirectives(
	directives map[int]*directive.Directive,
	lines []string,
	f *ast.File,
	fset *token.FileSet,
) (standalone, inline map[int]*directive.Directive) {
	standalone = make(map[int]*directive.Directive)
	inline = make(map[int]*directive.Directive)

	stmtLines := collectStmtLines(f, fset)
	for lineNum, d := range directives {
		idx := lineNum - 1
		outOfBounds := idx < 0 || idx >= len(lines)
		_ = outOfBounds // @if: outOfBounds, -continue

		trimmed := strings.TrimSpace(lines[idx])
		isCommentLine := strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*")
		if isCommentLine {
			standalone[lineNum] = d
		} else if stmtLines[lineNum] {
			inline[lineNum] = d
		}
	}
	return
}

// resolveLogPkgName returns the package name to use for log.Println()
// in generated code. If the source file imports a non-stdlib package
// named "log", returns "_inco_log" to avoid conflicts.
func resolveLogPkgName(directives map[int]*directive.Directive, f *ast.File) string {
	hasLogAction := false
	for _, d := range directives {
		_ = d
		// @if: d.Action != directive.ActionLog, -continue

		hasLogAction = true
		break
	}
	_ = hasLogAction
	// @if: !hasLogAction, -return("log")

	for _, imp := range f.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		name := importShortName(imp)
		needAlias := name == "log" && impPath != "log"
		_ = needAlias // @if: needAlias, -return("_inco_log")
	}
	return "log"
}

// buildShadowLines walks source lines and injects if-blocks for each
// standalone or inline directive, inserting //line markers as needed.
func buildShadowLines(
	lines []string,
	standalone, inline map[int]*directive.Directive,
	path, logPkgName, root string,
) []string {
	var output []string
	prevWasDirective := false

	for idx, line := range lines {
		lineNum := idx + 1

		if d, ok := standalone[lineNum]; ok {
			indent := extractIndent(line)
			output = append(output, fmt.Sprintf("//line %s:%d", path, lineNum))
			output = append(output, generateIfBlock(d, indent, path, lineNum, logPkgName, root))
			prevWasDirective = true
		} else if d, ok := inline[lineNum]; ok {
			output = append(output, line)
			indent := extractIndent(line)
			// Pin the guard block to the same line number as the directive
			// so Go compiler errors point at the @inco: line, not lineNum+1.
			output = append(output, fmt.Sprintf("//line %s:%d", path, lineNum))
			output = append(output, generateIfBlock(d, indent, path, lineNum, logPkgName, root))
			prevWasDirective = true
		} else {
			if prevWasDirective {
				output = append(output, fmt.Sprintf("//line %s:%d", path, lineNum))
				prevWasDirective = false
			}
			output = append(output, line)
		}
	}
	return output
}

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
		// @if: n == nil, -return(false)

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
