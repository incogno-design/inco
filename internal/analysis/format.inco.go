package analysis

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/imnive-design/inco-go/internal/inco"
)

// Format walks all Go source files under root and adjusts blank-line
// spacing around @inco:/@if: directives.
//
// Typically called between two go fmt passes: go fmt → Format → go fmt.
// Returns (changed, error) — changed is true when any file was modified.
func Format(root string) (bool, error) {
	// @inco: root != "", -return(false, fmt.Errorf("Format: root must not be empty"))

	absRoot, err := filepath.Abs(root)
	_ = err // @inco: err == nil, -return(false, fmt.Errorf("Format: %w", err))

	changed := false
	err = inco.WalkGoFiles(absRoot, func(path string) error {
		src, err := os.ReadFile(path)
		_ = err // @inco: err == nil, -return(fmt.Errorf("Format: read %s: %w", path, err))

		result := FormatDirectiveSpacing(string(src))
		// @inco: result != string(src), -return(nil)

		changed = true
		return os.WriteFile(path, []byte(result), 0o644)
	})
	return changed, err
}

// FormatDirectiveSpacing adjusts blank lines around directive comments in
// a Go source file.
//
// Rules:
//  1. Between consecutive directives: all blank lines are removed.
//  2. After a directive block, before non-directive code: exactly one blank
//     line is ensured (inserted if missing, collapsed if multiple).
//  3. After a directive, before a closing brace: no blank line.
//
// Directives are identified by parsing the AST with the same filtering as the
// engine (doc comments and struct field comments are excluded).
//
// Returns src unchanged if parsing fails or no directives are found.
func FormatDirectiveSpacing(src string) string {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	_ = err // @inco: err == nil, -return(src)

	directiveLines := make(map[int]bool)
	inco.CollectDirectives(f, func(c *ast.Comment, d *inco.Directive) {
		directiveLines[fset.Position(c.Pos()).Line] = true
	})
	// @inco: len(directiveLines) > 0, -return(src)

	lines := strings.Split(src, "\n")
	var out []string

	i := 0
	for i < len(lines) {
		lineNum := i + 1
		out = append(out, lines[i])

		if directiveLines[lineNum] {
			// Skip blank lines after this directive.
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			// Insert blank only before non-directive, non-brace code.
			atEnd := j >= len(lines)
			nextIsDirective := !atEnd && directiveLines[j+1]
			nextIsBrace := !atEnd && strings.HasPrefix(strings.TrimSpace(lines[j]), "}")
			if !atEnd && !nextIsDirective && !nextIsBrace {
				out = append(out, "")
			}
			i = j
		} else {
			i++
		}
	}

	return strings.Join(out, "\n")
}
