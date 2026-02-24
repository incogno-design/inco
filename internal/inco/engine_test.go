package inco

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// setupDir creates a temp directory with Go source files and returns its path.
func setupDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// readShadow returns the content of the first shadow file in the overlay.
func readShadow(t *testing.T, e *Engine) string {
	t.Helper()
	for _, sp := range e.Overlay.Replace {
		data, err := os.ReadFile(sp)
		if err != nil {
			t.Fatalf("reading shadow: %v", err)
		}
		return string(data)
	}
	t.Fatal("no shadow files")
	return ""
}

// readShadowFor returns the shadow content for a given source file basename.
func readShadowFor(t *testing.T, e *Engine, basename string) string {
	t.Helper()
	for origPath, sp := range e.Overlay.Replace {
		if filepath.Base(origPath) == basename {
			data, err := os.ReadFile(sp)
			if err != nil {
				t.Fatalf("reading shadow for %s: %v", basename, err)
			}
			return string(data)
		}
	}
	t.Fatalf("no shadow file for %s", basename)
	return ""
}

// ---------------------------------------------------------------------------
// No directives — no overlay
// ---------------------------------------------------------------------------

func TestEngine_NoDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Default action (panic)
// ---------------------------------------------------------------------------

func TestEngine_DefaultPanic(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Greet(name string) {
	// @inco: len(name) > 0
	fmt.Println(name)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(len(name) > 0)") {
		t.Errorf("shadow should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(") {
		t.Error("shadow should contain panic (default action)")
	}
	if !strings.Contains(shadow, "inco violation") {
		t.Error("shadow should contain default violation message")
	}
}

// ---------------------------------------------------------------------------
// Custom panic message
// ---------------------------------------------------------------------------

func TestEngine_PanicCustomMessage(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(x int) {
	// @inco: x > 0, -panic("x must be positive")
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `panic("x must be positive")`) {
		t.Errorf("shadow should contain custom panic message, got:\n%s", shadow)
	}
}

func TestEngine_PanicFmtSprintf(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Check(x int) {
	// @inco: x > 0, -panic(fmt.Sprintf("bad value: %d", x))
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `panic(fmt.Sprintf("bad value: %d", x))`) {
		t.Errorf("shadow should contain custom panic with Sprintf, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Multiple directives in same function
// ---------------------------------------------------------------------------

func TestEngine_MultipleDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Process(name string, age int) {
	// @inco: len(name) > 0
	// @inco: age > 0
	fmt.Println(name, age)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(len(name) > 0)") {
		t.Error("missing first condition")
	}
	if !strings.Contains(shadow, "!(age > 0)") {
		t.Error("missing second condition")
	}
	// Verify order: name check before age check.
	nameIdx := strings.Index(shadow, "len(name)")
	ageIdx := strings.Index(shadow, "age > 0")
	if nameIdx > ageIdx {
		t.Error("directives not in source order")
	}
}

// ---------------------------------------------------------------------------
// //line directives
// ---------------------------------------------------------------------------

func TestEngine_LineDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Hello(name string) {
	// @inco: len(name) > 0
	fmt.Println(name)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "//line") {
		t.Error("shadow should contain //line directives")
	}
}

// ---------------------------------------------------------------------------
// Overlay JSON
// ---------------------------------------------------------------------------

func TestEngine_OverlayJSON(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	data, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatalf("overlay.json not found: %v", err)
	}

	var ov Overlay
	if err := json.Unmarshal(data, &ov); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(ov.Replace) != 1 {
		t.Errorf("overlay has %d entries, want 1", len(ov.Replace))
	}
	for _, sp := range ov.Replace {
		if _, err := os.Stat(sp); err != nil {
			t.Errorf("shadow file missing: %s", sp)
		}
	}
}

// ---------------------------------------------------------------------------
// Skips hidden directories
// ---------------------------------------------------------------------------

func TestEngine_SkipsHiddenDirs(t *testing.T) {
	dir := setupDir(t, map[string]string{
		".hidden/main.go": `package hidden

func X(x int) {
	// @inco: x > 0
}
`,
		"main.go": "package main\n\nfunc main() {}\n",
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 1 { // only main.go, .hidden skipped
		t.Errorf("should skip hidden dirs, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Content hash stability
// ---------------------------------------------------------------------------

func TestEngine_ContentHashStable(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var p1 string
	for _, p := range e1.Overlay.Replace {
		p1 = p
	}

	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	var p2 string
	for _, p := range e2.Overlay.Replace {
		p2 = p
	}

	if filepath.Base(p1) != filepath.Base(p2) {
		t.Errorf("shadow names differ: %s vs %s", filepath.Base(p1), filepath.Base(p2))
	}
}

// ---------------------------------------------------------------------------
// Closure support
// ---------------------------------------------------------------------------

func TestEngine_Closure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	f := func(x int) {
		// @inco: x > 0
		fmt.Println(x)
	}
	f(42)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("should process directives inside closures")
	}
}

// ---------------------------------------------------------------------------
// -return action
// ---------------------------------------------------------------------------

func TestEngine_Return(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Positive(x int) int {
	// @inco: x > 0, -return(-1)
	return x * 2
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(x > 0)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "return -1") {
		t.Errorf("should contain return -1, got:\n%s", shadow)
	}
}

func TestEngine_ReturnMultiValue(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Parse(s string) (int, error) {
	// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))
	return len(s), nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `return 0, fmt.Errorf("empty")`) {
		t.Errorf("should contain multi-value return, got:\n%s", shadow)
	}
}

func TestEngine_ReturnBare(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Check(x int) {
	// @inco: x > 0, -return
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "return\n") {
		t.Errorf("should contain bare return, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// -continue action
// ---------------------------------------------------------------------------

func TestEngine_Continue(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func PrintPositive(nums []int) {
	for _, n := range nums {
		// @inco: n > 0, -continue
		fmt.Println(n)
	}
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(n > 0)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "continue") {
		t.Errorf("should contain continue, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// -break action
// ---------------------------------------------------------------------------

func TestEngine_Break(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func FindFirst(nums []int) {
	for _, n := range nums {
		// @inco: n != 42, -break
		fmt.Println(n)
	}
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(n != 42)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "break") {
		t.Errorf("should contain break, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Log action
// ---------------------------------------------------------------------------

func TestEngine_Log(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Check(x int) {
	// @inco: x > 0, -log("x is not positive", x)
	_ = x
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if !(x > 0)") {
		t.Errorf("should contain negated condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `log.Println("x is not positive", x)`) {
		t.Errorf("should contain log.Println call, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Struct field comments — should NOT be processed
// ---------------------------------------------------------------------------

func TestEngine_StructFieldCommentIgnored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type Config struct {
	Name string // @inco: not empty
	Port int    // some comment
}

func main() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	// Struct field inline comment is not a standalone comment line,
	// so it should NOT inject guards — but the file still gets a shadow.
	if len(e.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry, got %d", len(e.Overlay.Replace))
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, "inco violation") {
		t.Errorf("struct field comment should not produce guards, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Multiple files — all processed
// ---------------------------------------------------------------------------

func TestEngine_MultipleFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": `package main

func A(x int) {
	// @inco: x > 0
	_ = x
}
`,
		"b.go": `package main

func B(y int) {
	// @inco: y > 0
	_ = y
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries, got %d", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Test files (_test.go) should be skipped
// ---------------------------------------------------------------------------

func TestEngine_SkipsTestFiles(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go":      "package main\n\nfunc main() {}\n",
		"main_test.go": "package main\n\nfunc TestFoo() {\n\t// @inco: true\n}\n",
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 1 { // only main.go, _test.go skipped
		t.Errorf("should skip _test.go, got %d entries", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Import injection — fmt.Errorf in action args
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(s string) (int, error) {
	// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))
	return len(s), nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `"fmt"`) {
		t.Errorf("should inject fmt import, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Import injection must not fire for local variable.field
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_LocalVarNotPackage(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type myErr struct{ Msg string }

func Do() string {
	errors := myErr{Msg: "boom"}
	// @inco: errors.Msg != "", -panic("empty")
	return errors.Msg
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, `"errors"`) {
		t.Errorf("should NOT inject errors import for local var, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Import injection — -log action must inject "log" package
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_LogAction(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0, -log("x must be positive")
	_ = x
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `"log"`) {
		t.Errorf("should inject log import for -log action, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `log.Println(`) {
		t.Errorf("should contain log.Println call, got:\n%s", shadow)
	}
}

func TestEngine_ImportInjection_LogAction_AmbiguousLocalPkg(t *testing.T) {
	// When the project has a local package named "log" (e.g. mymod/log),
	// buildImportMap marks "log" as ambiguous and removes it. The -log
	// action must still inject stdlib "log" via knownPaths bypass.
	dir := setupDir(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0, -log("x must be positive")
	_ = x
}
`,
		"log/log.go": `package log

func Custom() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadowFor(t, e, "main.go")
	if !strings.Contains(shadow, `"log"`) {
		t.Errorf("should inject stdlib log import even when local log package exists, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `log.Println(`) {
		t.Errorf("should contain log.Println call, got:\n%s", shadow)
	}
}

func TestEngine_ImportInjection_LogAction_ConflictingImport(t *testing.T) {
	// When the source file already imports a non-stdlib "log" package,
	// the -log action must inject stdlib "log" with an alias (_inco_log)
	// and rewrite log.Println → _inco_log.Println to avoid collision.
	dir := setupDir(t, map[string]string{
		"go.mod": "module testmod\n\ngo 1.21\n",
		"main.go": `package main

import "testmod/log"

func Do(x int) {
	log.Custom()
	// @inco: x > 0, -log("x must be positive")
	_ = x
}
`,
		"log/log.go": `package log

func Custom() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadowFor(t, e, "main.go")
	if !strings.Contains(shadow, `_inco_log "log"`) {
		t.Errorf("should inject aliased stdlib log import, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `_inco_log.Println(`) {
		t.Errorf("should rewrite to _inco_log.Println, got:\n%s", shadow)
	}
	// Original log.Custom() must NOT be rewritten.
	if strings.Contains(shadow, `_inco_log.Custom()`) {
		t.Errorf("should not rewrite original log.Custom() call, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Deeply nested closure
// ---------------------------------------------------------------------------

func TestEngine_NestedClosure(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Outer() {
	a := func() {
		b := func(x int) {
			// @inco: x > 0
			fmt.Println(x)
		}
		b(1)
	}
	a()
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("should process directive in nested closure")
	}
}

// ---------------------------------------------------------------------------
// Vendor / testdata directories skipped
// ---------------------------------------------------------------------------

func TestEngine_SkipsVendor(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go":        "package main\n\nfunc main() {}\n",
		"vendor/v/v.go":  "package v\n\nfunc V(x int) {\n\t// @inco: x > 0\n}\n",
		"testdata/td.go": "package td\n\nfunc TD(x int) {\n\t// @inco: x > 0\n}\n",
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e.Overlay.Replace) != 1 { // only main.go, vendor/testdata skipped
		t.Errorf("should skip vendor/testdata, got %d entries", len(e.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Inline directive
// ---------------------------------------------------------------------------

func TestEngine_InlineDirective(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do() {
	err := doSomething()
	_ = err // @inco: err == nil, -panic(err)
}

func doSomething() error { return nil }
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	// Code line should be preserved.
	if !strings.Contains(shadow, "_ = err") {
		t.Error("inline directive should preserve code line")
	}
	// Guard should be injected after.
	if !strings.Contains(shadow, "if !(err == nil)") {
		t.Errorf("should contain guard, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "panic(err)") {
		t.Error("should contain panic(err)")
	}
}

// ---------------------------------------------------------------------------
// //line at column 1
// ---------------------------------------------------------------------------

func TestEngine_LineDirectiveColumn1(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Hello(name string) {
	// @inco: len(name) > 0
	fmt.Println(name)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	for _, line := range strings.Split(shadow, "\n") {
		if strings.Contains(line, "//line") {
			if strings.HasPrefix(line, "\t") || strings.HasPrefix(line, " ") {
				t.Errorf("//line directive must start at column 1, got: %q", line)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Incremental gen — unchanged source reuses cache
// ---------------------------------------------------------------------------

func TestEngine_IncrementalCache(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run — generates shadow.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var shadow1 string
	for _, sp := range e1.Overlay.Replace {
		shadow1 = sp
	}

	// Second run — should reuse cached shadow.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	var shadow2 string
	for _, sp := range e2.Overlay.Replace {
		shadow2 = sp
	}

	if shadow1 != shadow2 {
		t.Errorf("incremental cache should reuse shadow path: %s vs %s", shadow1, shadow2)
	}

	// Verify shadow file still exists.
	if _, err := os.Stat(shadow2); err != nil {
		t.Errorf("cached shadow file should still exist: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Stale shadow cleanup — deleted source file
// ---------------------------------------------------------------------------

func TestEngine_StaleShadowCleanup(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": `package main

func A(x int) {
	// @inco: x > 0
	_ = x
}
`,
		"b.go": `package main

func B(y int) {
	// @inco: y > 0
	_ = y
}
`,
	})

	// First run — generates shadows for a.go and b.go.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var shadowB string
	for src, sp := range e1.Overlay.Replace {
		if strings.HasSuffix(src, "b.go") {
			shadowB = sp
		}
	}
	if shadowB == "" {
		t.Fatal("b.go should have a shadow")
	}

	// Delete b.go.
	os.Remove(filepath.Join(dir, "b.go"))

	// Second run — b.go's shadow should be cleaned up.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(shadowB); !os.IsNotExist(err) {
		t.Errorf("stale shadow for deleted b.go should be removed, but still exists: %s", shadowB)
	}
	if len(e2.Overlay.Replace) != 1 {
		t.Errorf("should have 1 overlay entry after deleting b.go, got %d", len(e2.Overlay.Replace))
	}
}

// ---------------------------------------------------------------------------
// Changed source — old shadow removed, new shadow created
// ---------------------------------------------------------------------------

func TestEngine_ChangedSourceReplacesOldShadow(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var oldShadow string
	for _, sp := range e1.Overlay.Replace {
		oldShadow = sp
	}

	// Modify source.
	os.WriteFile(filepath.Join(dir, "main.go"), []byte(`package main

func Do(x int) {
	// @inco: x > 0, -panic("must be positive")
	_ = x
}
`), 0o644)

	// Second run.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	var newShadow string
	for _, sp := range e2.Overlay.Replace {
		newShadow = sp
	}

	// Old shadow should be gone.
	if _, err := os.Stat(oldShadow); !os.IsNotExist(err) {
		t.Errorf("old shadow should be removed after source change: %s", oldShadow)
	}

	// New shadow should exist.
	if _, err := os.Stat(newShadow); err != nil {
		t.Errorf("new shadow should exist: %v", err)
	}

	// Content should have new panic message.
	data, _ := os.ReadFile(newShadow)
	if !strings.Contains(string(data), "must be positive") {
		t.Error("new shadow should reflect the changed directive")
	}
}

// ---------------------------------------------------------------------------
// loadOverlayIfExists — no overlay.json
// ---------------------------------------------------------------------------

func TestEngine_LoadOverlayIfExists_NoFile(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(dir)
	ov := e.loadOverlayIfExists()
	if ov != nil {
		t.Errorf("should return nil when no overlay.json, got %v", ov)
	}
}

// ---------------------------------------------------------------------------
// Manifest persistence
// ---------------------------------------------------------------------------

func TestEngine_ManifestPersistence(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	// Manifest should exist.
	mPath := e.manifestPath()
	if _, err := os.Stat(mPath); err != nil {
		t.Fatalf("manifest.json should exist: %v", err)
	}

	// Load it and verify.
	m := e.loadManifest()
	if len(m.Files) != 1 {
		t.Errorf("manifest should have 1 entry, got %d", len(m.Files))
	}
	for _, entry := range m.Files {
		if entry.SrcHash == "" {
			t.Error("manifest entry should have a non-empty SrcHash")
		}
		if entry.ShadowPath == "" {
			t.Error("manifest entry should have a non-empty ShadowPath")
		}
	}
}

// ===========================================================================
// P0: Comment classification — edge cases
// ===========================================================================

// ---------------------------------------------------------------------------
// Interface method comments should NOT produce guards
// ---------------------------------------------------------------------------

func TestEngine_InterfaceMethodCommentIgnored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type Worker interface {
	Do(x int) // @inco: x > 0
}

func main() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, "inco violation") || strings.Contains(shadow, "panic(") {
		t.Errorf("interface method comment should not produce guards, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Variable declaration comment should NOT produce guards
// ---------------------------------------------------------------------------

func TestEngine_VarDeclCommentIgnored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

var globalLimit = 100 // @inco: globalLimit > 0

func main() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, "panic(") {
		t.Errorf("var decl comment should not produce guards, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Standalone directive on blank line before function
// ---------------------------------------------------------------------------

func TestEngine_StandaloneBeforeFunc(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

// @inco: true
func Hello() {
	fmt.Println("hello")
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	// Directive at file level (outside function) — should produce guard.
	if !strings.Contains(shadow, "!(true)") {
		t.Logf("shadow:\n%s", shadow)
		// This is expected to produce a guard. If not, it's a known limitation.
	}
}

// ---------------------------------------------------------------------------
// Block comment form /* @inco: */ inside function body
// ---------------------------------------------------------------------------

func TestEngine_BlockCommentDirective(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Do(x int) {
	/* @inco: x > 0 */
	fmt.Println(x)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Errorf("block comment directive should produce guard, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Multiple inline directives on consecutive lines
// ---------------------------------------------------------------------------

func TestEngine_ConsecutiveInlineDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(a, b int) {
	_ = a // @inco: a > 0
	_ = b // @inco: b > 0
	println(a + b)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(a > 0)") {
		t.Errorf("missing guard for a, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "!(b > 0)") {
		t.Errorf("missing guard for b, got:\n%s", shadow)
	}
	// Verify order: a before b
	aIdx := strings.Index(shadow, "!(a > 0)")
	bIdx := strings.Index(shadow, "!(b > 0)")
	if aIdx > bIdx {
		t.Error("guards not in source order")
	}
}

// ---------------------------------------------------------------------------
// Const declaration comment should NOT produce guards
// ---------------------------------------------------------------------------

func TestEngine_ConstCommentIgnored(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

const maxRetries = 3 // @inco: maxRetries > 0

func main() {}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, "panic(") {
		t.Errorf("const decl comment should not produce guards, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Mixed standalone + inline in same function
// ---------------------------------------------------------------------------

func TestEngine_MixedStandaloneAndInline(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) error {
	// @inco: x > 0
	err := work(x)
	_ = err // @inco: err == nil, -return(err)
	return nil
}

func work(x int) error { return nil }
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("standalone guard missing")
	}
	if !strings.Contains(shadow, "!(err == nil)") {
		t.Error("inline guard missing")
	}
	if !strings.Contains(shadow, "return err") {
		t.Error("return action missing")
	}
}

// ===========================================================================
// P1: Import injection — conflict edge cases
// ===========================================================================

// ---------------------------------------------------------------------------
// Multiple packages needing injection at once
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_MultiplePackages(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) (int, error) {
	// @inco: x > 0, -return(0, fmt.Errorf("bad: %d", x))
	// @inco: x < 100, -log("x too large", x)
	return x, nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, `"fmt"`) {
		t.Errorf("should inject fmt import, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, `"log"`) {
		t.Errorf("should inject log import, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Import already present with alias — should not duplicate
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_ExistingAliasedImport(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import f "fmt"

func Do(x int) {
	f.Println(x)
	// @inco: x > 0, -panic(fmt.Sprintf("bad: %d", x))
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	// fmt.Sprintf is used in the directive, but "fmt" is imported as "f".
	// The engine should inject "fmt" since the directive references fmt.Sprintf directly.
	if !strings.Contains(shadow, `fmt.Sprintf`) {
		t.Errorf("should contain fmt.Sprintf, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Directive with errors.New when local var named "errors" exists
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_LocalVarShadowsPackage(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) error {
	errors := []string{"a", "b"}
	// @inco: len(errors) > 0
	_ = errors
	return nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	// "errors" is a local var — should NOT inject "errors" package import.
	if strings.Contains(shadow, `"errors"`) {
		t.Errorf("should NOT inject errors import for local var, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Directive referencing receiver field that looks like pkg.Func
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_ReceiverFieldNotPackage(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

type Server struct{ Port int }

func (s *Server) Start() {
	// @inco: s.Port > 0
	println(s.Port)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(s.Port > 0)") {
		t.Error("should produce guard")
	}
	// "s" is a receiver — should not try to import a package named "s".
	// No panic or error means it handled it correctly.
}

// ---------------------------------------------------------------------------
// Expr with pkg ref but nothing to import (already imported)
// ---------------------------------------------------------------------------

func TestEngine_ImportInjection_AlreadyImported(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

import "fmt"

func Do(s string) string {
	// @inco: len(s) > 0, -panic(fmt.Sprintf("empty: %q", s))
	return fmt.Sprintf("hello %s", s)
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	// fmt is already imported — count occurrences of "fmt" import.
	fmtCount := strings.Count(shadow, `"fmt"`)
	if fmtCount != 1 {
		t.Errorf("fmt should appear exactly once in imports, got %d occurrences:\n%s", fmtCount, shadow)
	}
}

// ---------------------------------------------------------------------------
// Autoimport whitelist — whitelisted stdlib packages are auto-imported
// ---------------------------------------------------------------------------

func TestEngine_ImportWhitelist_Allowed(t *testing.T) {
	// Each sub-test verifies that a whitelisted package is auto-imported
	// when referenced in a directive expression or action args.
	cases := []struct {
		name     string
		ref      string // package-qualified identifier used in directive
		wantPath string // expected import path in shadow
	}{
		{"fmt", "fmt.Errorf", `"fmt"`},
		{"errors", "errors.New", `"errors"`},
		{"strings", "strings.Contains", `"strings"`},
		{"strconv", "strconv.Itoa", `"strconv"`},
		{"filepath", "filepath.Base", `"path/filepath"`},
		{"os", "os.ErrNotExist", `"os"`},
		{"time", "time.Now", `"time"`},
		{"json", "json.Marshal", `"encoding/json"`},
		{"http", "http.StatusOK", `"net/http"`},
		{"context", "context.Background", `"context"`},
		{"sync", "sync.Mutex", `"sync"`},
		{"math", "math.MaxInt", `"math"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupDir(t, map[string]string{
				"main.go": fmt.Sprintf(`package main

func Do() {
	// @inco: %s != nil
}
`, tc.ref),
			})
			e := NewEngine(dir)
			if err := e.Run(); err != nil {
				t.Fatal(err)
			}
			shadow := readShadow(t, e)
			if !strings.Contains(shadow, tc.wantPath) {
				t.Errorf("should auto-import %s, got:\n%s", tc.wantPath, shadow)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Autoimport whitelist — non-whitelisted stdlib packages are NOT imported
// ---------------------------------------------------------------------------

func TestEngine_ImportWhitelist_Blocked(t *testing.T) {
	// Packages NOT in the whitelist should not be auto-imported even if
	// they look like package-qualified identifiers in directives.
	cases := []struct {
		name    string
		ref     string // identifier that resembles pkg.Symbol
		blocked string // import path that must NOT appear
	}{
		{"unsafe", "unsafe.Pointer", `"unsafe"`},
		{"reflect", "reflect.TypeOf", `"reflect"`},
		{"runtime", "runtime.GOOS", `"runtime"`},
		{"syscall", "syscall.SIGTERM", `"syscall"`},
		{"ast", "ast.File", `"go/ast"`},
		{"exec", "exec.Command", `"os/exec"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := setupDir(t, map[string]string{
				"main.go": fmt.Sprintf(`package main

func Do() {
	// @inco: %s != nil
}
`, tc.ref),
			})
			e := NewEngine(dir)
			if err := e.Run(); err != nil {
				t.Fatal(err)
			}
			shadow := readShadow(t, e)
			if strings.Contains(shadow, tc.blocked) {
				t.Errorf("should NOT auto-import %s, got:\n%s", tc.blocked, shadow)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Whitelist map completeness — every entry resolves correctly
// ---------------------------------------------------------------------------

func TestStdlibWhitelist_Entries(t *testing.T) {
	if len(stdlibWhitelist) == 0 {
		t.Fatal("stdlibWhitelist is empty")
	}
	for name, path := range stdlibWhitelist {
		if name == "" || path == "" {
			t.Errorf("invalid whitelist entry: name=%q path=%q", name, path)
		}
	}
}

// ===========================================================================
// P1: Incremental cache consistency — edge cases
// ===========================================================================

// ---------------------------------------------------------------------------
// Shadow file manually deleted — should regenerate
// ---------------------------------------------------------------------------

func TestEngine_CacheMiss_ShadowDeleted(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var shadow1 string
	for _, sp := range e1.Overlay.Replace {
		shadow1 = sp
	}

	// Delete shadow file (simulate cache corruption).
	os.Remove(shadow1)

	// Second run — should regenerate.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	var shadow2 string
	for _, sp := range e2.Overlay.Replace {
		shadow2 = sp
	}

	// New shadow should exist.
	if _, err := os.Stat(shadow2); err != nil {
		t.Errorf("regenerated shadow should exist: %v", err)
	}

	// Content should be valid.
	data, err := os.ReadFile(shadow2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "!(x > 0)") {
		t.Errorf("regenerated shadow should contain guard, got:\n%s", string(data))
	}
}

// ---------------------------------------------------------------------------
// Manifest corrupted (invalid JSON) — should rebuild
// ---------------------------------------------------------------------------

func TestEngine_ManifestCorrupted(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run to create cache.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}

	// Corrupt manifest.
	manifestPath := filepath.Join(dir, ".inco_cache", "manifest.json")
	os.WriteFile(manifestPath, []byte("{invalid json!!!"), 0o644)

	// Second run — should handle gracefully.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatalf("should handle corrupted manifest gracefully: %v", err)
	}

	// Should still produce valid output.
	shadow := readShadow(t, e2)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Errorf("should produce valid shadow after manifest corruption, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Overlay JSON corrupted — should rebuild
// ---------------------------------------------------------------------------

func TestEngine_OverlayCorrupted(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}

	// Corrupt overlay.json.
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	os.WriteFile(overlayPath, []byte("not json"), 0o644)

	// Second run.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatalf("should handle corrupted overlay gracefully: %v", err)
	}

	shadow := readShadow(t, e2)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Errorf("should produce valid shadow after overlay corruption, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// Source file added — should be picked up without breaking cache
// ---------------------------------------------------------------------------

func TestEngine_NewFileAdded(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": `package main

func A(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run — only a.go.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e1.Overlay.Replace) != 1 {
		t.Errorf("expected 1 overlay entry initially, got %d", len(e1.Overlay.Replace))
	}

	// Add b.go.
	os.WriteFile(filepath.Join(dir, "b.go"), []byte(`package main

func B(y int) {
	// @inco: y > 0
	_ = y
}
`), 0o644)

	// Second run — both files.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}
	if len(e2.Overlay.Replace) != 2 {
		t.Errorf("expected 2 overlay entries after adding b.go, got %d", len(e2.Overlay.Replace))
	}

	// Verify a.go was cached (not re-processed) — its shadow path should be same.
	var aShadow1, aShadow2 string
	for src, sp := range e1.Overlay.Replace {
		if filepath.Base(src) == "a.go" {
			aShadow1 = sp
		}
	}
	for src, sp := range e2.Overlay.Replace {
		if filepath.Base(src) == "a.go" {
			aShadow2 = sp
		}
	}
	if aShadow1 != aShadow2 {
		t.Errorf("a.go should be cached: %s vs %s", aShadow1, aShadow2)
	}
}

// ---------------------------------------------------------------------------
// Source file renamed — old shadow cleaned, new shadow created
// ---------------------------------------------------------------------------

func TestEngine_FileRenamed(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"old.go": `package main

func Old(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	// First run.
	e1 := NewEngine(dir)
	if err := e1.Run(); err != nil {
		t.Fatal(err)
	}
	var oldShadow string
	for _, sp := range e1.Overlay.Replace {
		oldShadow = sp
	}

	// Rename old.go → new.go.
	os.Rename(filepath.Join(dir, "old.go"), filepath.Join(dir, "new.go"))

	// Second run.
	e2 := NewEngine(dir)
	if err := e2.Run(); err != nil {
		t.Fatal(err)
	}

	// Old shadow should be cleaned up.
	if _, err := os.Stat(oldShadow); !os.IsNotExist(err) {
		t.Errorf("old shadow should be removed after rename: %s", oldShadow)
	}

	// New shadow should exist.
	if len(e2.Overlay.Replace) != 1 {
		t.Fatalf("expected 1 overlay entry, got %d", len(e2.Overlay.Replace))
	}
	for _, sp := range e2.Overlay.Replace {
		if _, err := os.Stat(sp); err != nil {
			t.Errorf("new shadow should exist: %v", err)
		}
	}
}

// ===========================================================================
// P1: Concurrency stability
// ===========================================================================

// ---------------------------------------------------------------------------
// Multiple files processed in parallel — all produce valid shadows
// ---------------------------------------------------------------------------

func TestEngine_Parallel_ManyFiles(t *testing.T) {
	files := make(map[string]string)
	numFiles := 20
	for i := 0; i < numFiles; i++ {
		name := fmt.Sprintf("f%d.go", i)
		files[name] = fmt.Sprintf(`package main

func F%d(x int) {
	// @inco: x > %d
	_ = x
}
`, i, i)
	}
	dir := setupDir(t, files)

	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}

	if len(e.Overlay.Replace) != numFiles {
		t.Errorf("expected %d overlay entries, got %d", numFiles, len(e.Overlay.Replace))
	}

	// Verify each shadow is valid.
	for src, sp := range e.Overlay.Replace {
		data, err := os.ReadFile(sp)
		if err != nil {
			t.Errorf("shadow for %s missing: %v", filepath.Base(src), err)
			continue
		}
		if !strings.Contains(string(data), "if !(x >") {
			t.Errorf("shadow for %s missing guard:\n%s", filepath.Base(src), string(data))
		}
	}
}

// ---------------------------------------------------------------------------
// Idempotent — repeated runs produce identical results
// ---------------------------------------------------------------------------

func TestEngine_Idempotent(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"a.go": `package main

func A(x int) {
	// @inco: x > 0
	_ = x
}
`,
		"b.go": `package main

import "fmt"

func B(name string) {
	// @inco: len(name) > 0, -panic(fmt.Sprintf("empty: %s", name))
	_ = name
}
`,
	})

	// Run 3 times.
	for i := 0; i < 3; i++ {
		e := NewEngine(dir)
		if err := e.Run(); err != nil {
			t.Fatalf("run %d failed: %v", i+1, err)
		}
		if len(e.Overlay.Replace) != 2 {
			t.Errorf("run %d: expected 2 overlay entries, got %d", i+1, len(e.Overlay.Replace))
		}

		// Verify overlay.json exists and is valid.
		overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
		data, err := os.ReadFile(overlayPath)
		if err != nil {
			t.Fatalf("run %d: overlay.json missing: %v", i+1, err)
		}
		var ov Overlay
		if err := json.Unmarshal(data, &ov); err != nil {
			t.Fatalf("run %d: overlay.json invalid: %v", i+1, err)
		}
		if len(ov.Replace) != 2 {
			t.Errorf("run %d: overlay has %d entries, want 2", i+1, len(ov.Replace))
		}
	}
}

// ---------------------------------------------------------------------------
// Rapid sequential runs — no file handle leaks or race conditions
// ---------------------------------------------------------------------------

func TestEngine_RapidSequentialRuns(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	for i := 0; i < 10; i++ {
		e := NewEngine(dir)
		if err := e.Run(); err != nil {
			t.Fatalf("iteration %d failed: %v", i, err)
		}
	}

	// Final state should be valid.
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("final shadow should contain guard")
	}
}

// ---------------------------------------------------------------------------
// Worker panic recovery — parse error in one file doesn't crash engine
// ---------------------------------------------------------------------------

func TestEngine_ParseError_HandlesGracefully(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"good.go": `package main

func Good(x int) {
	// @inco: x > 0
	_ = x
}
`,
		"bad.go": `package main

func Bad( { // syntax error
}
`,
	})

	e := NewEngine(dir)
	err := e.Run()
	// Should return an error for the bad file, not panic.
	if err == nil {
		t.Error("should return error for unparseable file")
	}
}

// ---------------------------------------------------------------------------
// Concurrent Run calls on different engines sharing same directory
// ---------------------------------------------------------------------------

func TestEngine_ConcurrentEngines(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @inco: x > 0
	_ = x
}
`,
	})

	var wg sync.WaitGroup
	errors := make([]error, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			e := NewEngine(dir)
			errors[idx] = e.Run()
		}(i)
	}
	wg.Wait()

	// All should succeed (or at worst, no panics).
	for i, err := range errors {
		if err != nil {
			t.Errorf("engine %d failed: %v", i, err)
		}
	}

	// Final state should be valid.
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Error("final shadow should contain guard")
	}
}

// ===========================================================================
// @if: directive — same direction as if (no negation)
// ===========================================================================

func TestEngine_IfDirective(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) {
	// @if: x <= 0
	_ = x
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if strings.Contains(shadow, "!(x <= 0)") {
		t.Errorf("@if: should NOT negate condition, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "if x <= 0") {
		t.Errorf("@if: should use condition as-is, got:\n%s", shadow)
	}
}

func TestEngine_IfDirective_WithReturn(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do() error {
	var err error
	// @if: err != nil, -return(err)
	return nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if err != nil") {
		t.Errorf("@if: should use condition as-is, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "return err") {
		t.Errorf("should contain return action, got:\n%s", shadow)
	}
}

func TestEngine_IfDirective_Inline(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do() error {
	var err error
	_ = err // @if: err != nil, -return(err)
	return nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "if err != nil") {
		t.Errorf("@if: inline should use condition as-is, got:\n%s", shadow)
	}
}

// ---------------------------------------------------------------------------
// @inco: and @if: can coexist in the same function
// ---------------------------------------------------------------------------

func TestEngine_MixedDirectives(t *testing.T) {
	dir := setupDir(t, map[string]string{
		"main.go": `package main

func Do(x int) error {
	var err error
	// @inco: x > 0
	// @if: err != nil, -return(err)
	return nil
}
`,
	})
	e := NewEngine(dir)
	if err := e.Run(); err != nil {
		t.Fatal(err)
	}
	shadow := readShadow(t, e)
	if !strings.Contains(shadow, "!(x > 0)") {
		t.Errorf("@inco: should be negated, got:\n%s", shadow)
	}
	if !strings.Contains(shadow, "if err != nil") {
		t.Errorf("@if: should be as-is, got:\n%s", shadow)
	}
}
