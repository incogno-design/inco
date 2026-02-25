package inco

import (
	"os"
	"path/filepath"
	"testing"
)

func assertFormat(t *testing.T, name, src, want string) {
	t.Helper()
	got := FormatDirectiveSpacing(src)
	if got != want {
		t.Errorf("%s: FormatDirectiveSpacing mismatch\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

// ---------------------------------------------------------------------------
// FormatDirectiveSpacing unit tests
// ---------------------------------------------------------------------------

func TestFormatSpacing_ConsecutiveDirectivesRemoveBlanks(t *testing.T) {
	src := `package p

func foo(x int, y int) {
	// @inco: x > 0

	// @inco: y > 0
	println(x, y)
}
`
	want := `package p

func foo(x int, y int) {
	// @inco: x > 0
	// @inco: y > 0

	println(x, y)
}
`
	assertFormat(t, "consecutive-blanks-removed", src, want)
}

func TestFormatSpacing_DirectiveBeforeCodeInsertBlank(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0
	println(x)
}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0

	println(x)
}
`
	assertFormat(t, "insert-blank", src, want)
}

func TestFormatSpacing_AlreadyOneBlank(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0

	println(x)
}
`
	assertFormat(t, "already-one-blank", src, src)
}

func TestFormatSpacing_MultipleBlanksCollapsed(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0



	println(x)
}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0

	println(x)
}
`
	assertFormat(t, "multiple-blanks-collapsed", src, want)
}

func TestFormatSpacing_DirectiveBeforeClosingBrace(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0
}
`
	assertFormat(t, "before-closing-brace", src, src)
}

func TestFormatSpacing_DirectiveBeforeClosingBraceRemoveBlank(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0

}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0
}
`
	assertFormat(t, "before-closing-brace-remove-blank", src, want)
}

func TestFormatSpacing_NoDirectives(t *testing.T) {
	src := `package p

func foo(x int) {
	println(x)
}
`
	assertFormat(t, "no-directives", src, src)
}

func TestFormatSpacing_InlineDirectives(t *testing.T) {
	src := `package p

func foo() {
	var errA, errB error
	_ = errA // @inco: errA == nil, -panic(errA)
	_ = errB // @inco: errB == nil, -panic(errB)
	println("ok")
}
`
	want := `package p

func foo() {
	var errA, errB error
	_ = errA // @inco: errA == nil, -panic(errA)
	_ = errB // @inco: errB == nil, -panic(errB)

	println("ok")
}
`
	assertFormat(t, "inline-directives", src, want)
}

func TestFormatSpacing_MixedIncoAndIf(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0

	// @if: x > 10, -return
	println(x)
}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0
	// @if: x > 10, -return

	println(x)
}
`
	assertFormat(t, "mixed-inco-if", src, want)
}

func TestFormatSpacing_ThreeConsecutiveDirectives(t *testing.T) {
	src := `package p

func foo(x int, y string, z float64) {
	// @inco: x > 0

	// @inco: y != ""

	// @inco: z > 0
	println(x, y, z)
}
`
	want := `package p

func foo(x int, y string, z float64) {
	// @inco: x > 0
	// @inco: y != ""
	// @inco: z > 0

	println(x, y, z)
}
`
	assertFormat(t, "three-consecutive", src, want)
}

func TestFormatSpacing_DirectiveBeforeReturn(t *testing.T) {
	src := `package p

func foo(x int) int {
	// @inco: x > 0
	return x
}
`
	want := `package p

func foo(x int) int {
	// @inco: x > 0

	return x
}
`
	assertFormat(t, "before-return", src, want)
}

func TestFormatSpacing_DirectiveBeforeReturnAlreadySpaced(t *testing.T) {
	src := `package p

func foo(x int) int {
	// @inco: x > 0

	return x
}
`
	assertFormat(t, "before-return-already-spaced", src, src)
}

func TestFormatSpacing_ParseError(t *testing.T) {
	src := "not valid go source {"
	assertFormat(t, "parse-error", src, src)
}

func TestFormatSpacing_StandaloneBeforeInline(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0
	_ = x // @inco: x < 100
	println(x)
}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0
	_ = x // @inco: x < 100

	println(x)
}
`
	assertFormat(t, "standalone-before-inline", src, want)
}

func TestFormatSpacing_ReturnPrefixVariable(t *testing.T) {
	// "returnValue" starts with "return" but is NOT a return statement.
	// Directive before it should insert a blank line.
	src := `package p

func foo(x int) int {
	// @inco: x > 0
	returnValue := x * 2
	return returnValue
}
`
	want := `package p

func foo(x int) int {
	// @inco: x > 0

	returnValue := x * 2
	return returnValue
}
`
	assertFormat(t, "return-prefix-variable", src, want)
}

func TestFormatSpacing_BareReturn(t *testing.T) {
	src := `package p

func foo(x int) {
	// @inco: x > 0
	return
}
`
	want := `package p

func foo(x int) {
	// @inco: x > 0

	return
}
`
	assertFormat(t, "bare-return", src, want)
}

// ---------------------------------------------------------------------------
// Format integration tests
// ---------------------------------------------------------------------------

func TestFormat_Integration(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func foo(x int, y int) {
	// @inco: x > 0

	// @inco: y > 0
	println(x, y)
}
`)

	err := Format(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := `package main

func foo(x int, y int) {
	// @inco: x > 0
	// @inco: y > 0

	println(x, y)
}
`
	if string(got) != want {
		t.Errorf("Format result mismatch\n--- got ---\n%s\n--- want ---\n%s", string(got), want)
	}
}

func TestFormat_NoDirectivesUntouched(t *testing.T) {
	dir := t.TempDir()
	content := `package main

func foo(x int) {
	if x > 0 {
		println(x)
	}
}
`
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, content)

	err := Format(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("file without directives was modified")
	}
}
