package inco

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// AuditFile
// ---------------------------------------------------------------------------

func TestAuditFile_Basic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func guarded() {
	// @inco: true
}

func unguarded() {}
`)

	fa, err := AuditFile(dir, filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("AuditFile: %v", err)
	}

	if fa.RequireCount != 1 {
		t.Errorf("RequireCount = %d, want 1", fa.RequireCount)
	}
	if fa.IncoCount != 1 {
		t.Errorf("IncoCount = %d, want 1", fa.IncoCount)
	}
	if len(fa.Funcs) != 2 {
		t.Errorf("len(Funcs) = %d, want 2", len(fa.Funcs))
	}

	// Check guarded func has RequireCount=1.
	var guardedFn *FuncAudit
	for i := range fa.Funcs {
		if fa.Funcs[i].Name == "guarded" {
			guardedFn = &fa.Funcs[i]
		}
	}
	if guardedFn == nil {
		t.Fatal("function 'guarded' not found")
	}
	if guardedFn.RequireCount != 1 {
		t.Errorf("guarded.RequireCount = %d, want 1", guardedFn.RequireCount)
	}
}

func TestAuditFile_IfDirective(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func foo() {
	// @if: x > 0, -return
}
`)

	fa, err := AuditFile(dir, filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("AuditFile: %v", err)
	}
	if fa.IfDirCount != 1 {
		t.Errorf("IfDirCount = %d, want 1", fa.IfDirCount)
	}
	if fa.IncoCount != 0 {
		t.Errorf("IncoCount = %d, want 0", fa.IncoCount)
	}
}

func TestAuditFile_RelPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main
func main() {}
`)

	fa, err := AuditFile(dir, filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatalf("AuditFile: %v", err)
	}
	if fa.RelPath != "main.go" {
		t.Errorf("RelPath = %q, want %q", fa.RelPath, "main.go")
	}
}

// ---------------------------------------------------------------------------
// FmtFile
// ---------------------------------------------------------------------------

func TestFmtFile_AdjustsSpacing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	// Source with missing blank line after directive.
	writeFile(t, path, `package main

// @inco: true
x := 1
`)

	changed, err := FmtFile(path)
	if err != nil {
		t.Fatalf("FmtFile: %v", err)
	}
	if !changed {
		t.Error("expected file to be changed")
	}

	data, _ := os.ReadFile(path)
	result := string(data)
	// Should have blank line between directive and code.
	if !strings.Contains(result, "// @inco: true\n\nx := 1") {
		t.Errorf("expected blank line after directive, got:\n%s", result)
	}
}

func TestFmtFile_NoChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

func main() {}
`)

	changed, err := FmtFile(path)
	if err != nil {
		t.Fatalf("FmtFile: %v", err)
	}
	if changed {
		t.Error("expected no change for file without directives")
	}
}

func TestFmtFile_Nonexistent(t *testing.T) {
	_, err := FmtFile("/nonexistent/file.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

// ---------------------------------------------------------------------------
// DiagnoseFile
// ---------------------------------------------------------------------------

func TestDiagnoseFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.go")
	writeFile(t, path, `not valid go code at all!!!`)

	diags, err := DiagnoseFile(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseFile: %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected at least one diagnostic for parse error")
	}
	// The parser may produce a partial AST, so severity could be warning.
	hasParseIssue := false
	for _, d := range diags {
		if d.Code == "parse-error" || d.Code == "parse-warning" {
			hasParseIssue = true
		}
	}
	if !hasParseIssue {
		t.Error("expected parse-error or parse-warning diagnostic")
	}
}

func TestDiagnoseFile_UnguardedFunction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

func unguarded() {}
`)

	diags, err := DiagnoseFile(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseFile: %v", err)
	}

	var found bool
	for _, d := range diags {
		if d.Code == "unguarded" {
			found = true
			if d.Severity != DiagHint {
				t.Errorf("unguarded severity = %d, want %d (hint)", d.Severity, DiagHint)
			}
			if !strings.Contains(d.Message, "unguarded") {
				t.Errorf("message should mention function name: %q", d.Message)
			}
		}
	}
	if !found {
		t.Error("expected 'unguarded' diagnostic")
	}
}

func TestDiagnoseFile_GuardedFunction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

func guarded() {
	// @inco: true
}
`)

	diags, err := DiagnoseFile(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseFile: %v", err)
	}

	for _, d := range diags {
		if d.Code == "unguarded" && strings.Contains(d.Message, "guarded") {
			t.Error("guarded function should not produce 'unguarded' diagnostic")
		}
	}
}

func TestDiagnoseFile_SpacingIssue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

// @inco: true

func main() {}
`)

	diags, err := DiagnoseFile(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseFile: %v", err)
	}

	var found bool
	for _, d := range diags {
		if d.Code == "spacing" {
			found = true
			if d.Severity != DiagInfo {
				t.Errorf("spacing severity = %d, want %d (info)", d.Severity, DiagInfo)
			}
		}
	}
	if !found {
		t.Error("expected 'spacing' diagnostic for directive followed by blank line")
	}
}

func TestDiagnoseFile_NonexistentFile(t *testing.T) {
	_, err := DiagnoseFile(".", "/nonexistent.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDiagnoseFile_ZeroBasedPositions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

func foo() {}
`)

	diags, err := DiagnoseFile(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseFile: %v", err)
	}

	for _, d := range diags {
		// All positions should be >= 0 (0-based).
		if d.Range.Start.Line < 0 || d.Range.Start.Character < 0 {
			t.Errorf("negative position: line=%d, char=%d", d.Range.Start.Line, d.Range.Start.Character)
		}
	}
}

// ---------------------------------------------------------------------------
// DiagnoseJSON
// ---------------------------------------------------------------------------

func TestDiagnoseJSON_ValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, `package main

func main() {}
`)

	out, err := DiagnoseJSON(dir, path)
	if err != nil {
		t.Fatalf("DiagnoseJSON: %v", err)
	}
	if !strings.HasPrefix(out, "[") {
		t.Errorf("expected JSON array, got: %s", out[:min(len(out), 50)])
	}
}

func TestDiagnoseJSON_Nonexistent(t *testing.T) {
	_, err := DiagnoseJSON(".", "/nonexistent.go")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
