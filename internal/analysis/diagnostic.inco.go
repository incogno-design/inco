package analysis

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"github.com/imnive-design/inco-go/internal/directive"
)

// ---------------------------------------------------------------------------
// AuditFile — single-file audit
// ---------------------------------------------------------------------------

// AuditFile audits a single file and returns its per-file audit data.
// root is used only for computing the RelPath field.
func AuditFile(root, path string) (*FileAudit, error) {
	absPath, err := filepath.Abs(path)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AuditFile: %w", err))

	absRoot, err := filepath.Abs(root)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("AuditFile: %w", err))

	fset := token.NewFileSet()
	fa := auditFile(fset, absRoot, absPath)
	return &fa, nil
}

// ---------------------------------------------------------------------------
// FmtFile — single-file format
// ---------------------------------------------------------------------------

// FmtFile applies directive spacing rules to a single file.
// Returns true if the file was modified.
func FmtFile(path string) (bool, error) {
	absPath, err := filepath.Abs(path)
	_ = err // @inco: err == nil, -return(false, fmt.Errorf("FmtFile: %w", err))

	src, err := os.ReadFile(absPath)
	_ = err // @inco: err == nil, -return(false, fmt.Errorf("FmtFile: %w", err))

	result := FormatDirectiveSpacing(string(src))
	// @if: result == string(src), -return(false, nil)

	err = os.WriteFile(absPath, []byte(result), 0o644)
	_ = err // @inco: err == nil, -return(false, fmt.Errorf("FmtFile: write: %w", err))

	return true, nil
}

// ---------------------------------------------------------------------------
// DiagnoseFile — LSP-compatible diagnostics
// ---------------------------------------------------------------------------

// Diagnostic represents a single diagnostic message for a source file,
// compatible with the Language Server Protocol (LSP).
type Diagnostic struct {
	Path     string       `json:"path"`           // absolute file path
	Range    DiagRange    `json:"range"`          // location
	Severity DiagSeverity `json:"severity"`       // error, warning, info, hint
	Source   string       `json:"source"`         // "inco"
	Message  string       `json:"message"`        // human-readable message
	Code     string       `json:"code,omitempty"` // machine-readable code
	Tags     []DiagTag    `json:"tags,omitempty"` // additional tags
}

// DiagRange is a zero-based line/column range.
type DiagRange struct {
	Start DiagPosition `json:"start"`
	End   DiagPosition `json:"end"`
}

// DiagPosition is a zero-based line/column position.
type DiagPosition struct {
	Line      int `json:"line"`      // 0-based
	Character int `json:"character"` // 0-based
}

// DiagSeverity matches LSP DiagnosticSeverity.
type DiagSeverity int

const (
	DiagError   DiagSeverity = 1
	DiagWarning DiagSeverity = 2
	DiagInfo    DiagSeverity = 3
	DiagHint    DiagSeverity = 4
)

// DiagTag matches LSP DiagnosticTag.
type DiagTag int

const (
	DiagTagUnnecessary DiagTag = 1
	DiagTagDeprecated  DiagTag = 2
)

// DiagnoseFile parses a single file and returns LSP-compatible diagnostics.
// Diagnostics include:
//   - Parse errors (severity: error)
//   - Unguarded functions (severity: hint)
//   - Spacing issues (severity: info, with auto-fix available)
//   - Invalid directives (severity: warning)
func DiagnoseFile(root, path string) ([]Diagnostic, error) {
	absPath, err := filepath.Abs(path)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("DiagnoseFile: %w", err))

	src, err := os.ReadFile(absPath)
	_ = err // @inco: err == nil, -return(nil, fmt.Errorf("DiagnoseFile: read: %w", err))

	fset := token.NewFileSet()
	f, parseErr := parser.ParseFile(fset, absPath, src, parser.ParseComments)

	var diags []Diagnostic

	// 1. Parse errors → error severity.
	if parseErr != nil && f == nil {
		diags = append(diags, Diagnostic{
			Path:     absPath,
			Range:    DiagRange{Start: DiagPosition{0, 0}, End: DiagPosition{0, 0}},
			Severity: DiagError,
			Source:   "inco",
			Message:  parseErr.Error(),
			Code:     "parse-error",
		})
		return diags, nil
	}

	// Even with partial AST, report parse errors.
	if parseErr != nil {
		diags = append(diags, Diagnostic{
			Path:     absPath,
			Range:    DiagRange{Start: DiagPosition{0, 0}, End: DiagPosition{0, 0}},
			Severity: DiagWarning,
			Source:   "inco",
			Message:  "partial parse: " + parseErr.Error(),
			Code:     "parse-warning",
		})
	}

	lines := strings.Split(string(src), "\n")

	// 2. Validate directives and detect spacing issues.
	//
	// Collect all directive line numbers first, then check spacing.
	// Spacing rules (must match FormatDirectiveSpacing):
	//   - Between consecutive directives: no blank lines allowed.
	//   - After directive block, before closing brace: no blank lines.
	//   - After directive block, before other code: exactly one blank line.
	//   - Multiple blank lines anywhere after a directive: collapse to 0 or 1.
	directiveLines := make(map[int]bool) // 1-based
	type directiveInfo struct {
		line   int
		col    int
		endCol int
	}
	var allDirectives []directiveInfo

	directive.CollectDirectives(f, func(c *ast.Comment, d *directive.Directive) {
		line := fset.Position(c.Pos()).Line
		col := fset.Position(c.Pos()).Column - 1 // 0-based
		endCol := col + len(c.Text)
		directiveLines[line] = true
		allDirectives = append(allDirectives, directiveInfo{line, col, endCol})
	})

	for _, di := range allDirectives {
		idx := di.line // 0-based index of next line (line is 1-based)
		_ = idx        // @if: idx >= len(lines), -continue

		// Count blank lines after the directive.
		blankCount := 0
		j := idx
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			blankCount++
			j++
		}

		_ = blankCount // @if: blankCount == 0, -continue

		// Determine what follows the blank lines.
		atEnd := j >= len(lines)
		nextIsDirective := !atEnd && directiveLines[j+1]
		nextIsBrace := !atEnd && strings.HasPrefix(strings.TrimSpace(lines[j]), "}")

		// Apply FormatDirectiveSpacing rules:
		needsReport := false
		if nextIsDirective {
			// Between consecutive directives: no blanks allowed.
			needsReport = true
		} else if nextIsBrace {
			// Before closing brace: no blanks allowed.
			needsReport = true
		} else if blankCount > 1 {
			// Before other code: exactly one blank line (multiple → collapse).
			needsReport = true
		}
		// blankCount == 1 before non-directive, non-brace code → correct, skip.

		if needsReport {
			diags = append(diags, Diagnostic{
				Path: absPath,
				Range: DiagRange{
					Start: DiagPosition{di.line - 1, di.col},
					End:   DiagPosition{di.line - 1, di.endCol},
				},
				Severity: DiagInfo,
				Source:   "inco",
				Message:  "directive followed by blank line — run 'inco fmt' to normalize",
				Code:     "spacing",
				Tags:     []DiagTag{DiagTagUnnecessary},
			})
		}
	}

	// 3. Check for invalid directive syntax in comments.
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			text := c.Text
			// Look for attempts at @inco: or @if: that don't parse.
			if (strings.Contains(text, "@inco:") || strings.Contains(text, "@if:")) &&
				directive.ParseDirective(text) == nil {
				line := fset.Position(c.Pos()).Line
				col := fset.Position(c.Pos()).Column - 1
				endCol := col + len(text)
				diags = append(diags, Diagnostic{
					Path: absPath,
					Range: DiagRange{
						Start: DiagPosition{line - 1, col},
						End:   DiagPosition{line - 1, endCol},
					},
					Severity: DiagWarning,
					Source:   "inco",
					Message:  "invalid directive syntax: " + text,
					Code:     "invalid-directive",
				})
			}
		}
	}

	// 4. Unguarded functions → hint severity.
	absRoot, _ := filepath.Abs(root)
	ast.Inspect(f, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		_ = ok // @if: !ok || fn.Body == nil, -return(true)

		// Check if any directive falls within this function body.
		hasDirective := false
		for line := range directiveLines {
			pos := fset.Position(fn.Body.Pos()).Line
			end := fset.Position(fn.Body.End()).Line
			inRange := line >= pos && line <= end
			_ = inRange // @if: !inRange, -continue

			hasDirective = true
			break
		}
		if !hasDirective {
			line := fset.Position(fn.Pos()).Line
			col := fset.Position(fn.Name.Pos()).Column - 1
			endCol := col + len(fn.Name.Name)

			name := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) > 0 {
				name = recvTypeName(fn.Recv.List[0].Type) + "." + name
			}

			relPath := absPath
			if r, e := filepath.Rel(absRoot, absPath); e == nil {
				relPath = r
			}

			diags = append(diags, Diagnostic{
				Path: absPath,
				Range: DiagRange{
					Start: DiagPosition{line - 1, col},
					End:   DiagPosition{line - 1, endCol},
				},
				Severity: DiagHint,
				Source:   "inco",
				Message:  fmt.Sprintf("function %s has no @inco: directives (%s:%d)", name, relPath, line),
				Code:     "unguarded",
			})
		}
		return true
	})

	return diags, nil
}

// ---------------------------------------------------------------------------
// DiagnoseJSON — convenience JSON output
// ---------------------------------------------------------------------------

// DiagnoseJSON returns DiagnoseFile results as a JSON string, suitable
// for piping to an IDE or LSP client.
func DiagnoseJSON(root, path string) (string, error) {
	diags, err := DiagnoseFile(root, path)
	_ = err // @inco: err == nil, -return("", err)

	data, err := json.MarshalIndent(diags, "", "  ")
	_ = err // @inco: err == nil, -return("", fmt.Errorf("DiagnoseJSON: %w", err))

	return string(data), nil
}
