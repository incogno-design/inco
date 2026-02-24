package inco

import (
	"reflect"
	"testing"
)

// ---------------------------------------------------------------------------
// ParseDirective — basic recognition
// ---------------------------------------------------------------------------

func TestParseDirective_Nil(t *testing.T) {
	for _, input := range []string{
		"",
		"// just a comment",
		"// @inco",     // missing colon
		"// @inco:",    // no expression
		"// @inco:   ", // whitespace only
		"/* block comment */",
		"// @INCO: x > 0", // wrong case
	} {
		if d := ParseDirective(input); d != nil {
			t.Errorf("ParseDirective(%q) = %+v, want nil", input, d)
		}
	}
}

func TestParseDirective_ExprOnly(t *testing.T) {
	d := ParseDirective("// @inco: x > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q, want %q", d.Expr, "x > 0")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if len(d.ActionArgs) != 0 {
		t.Errorf("ActionArgs = %v, want empty", d.ActionArgs)
	}
}

func TestParseDirective_FuncCallExpr(t *testing.T) {
	d := ParseDirective("// @inco: len(name) > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "len(name) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

// ---------------------------------------------------------------------------
// Actions — comma+dash syntax
// ---------------------------------------------------------------------------

func TestParseDirective_PanicBare(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -panic")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_PanicWithMessage(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -panic("x must be positive")`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`"x must be positive"`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_PanicFmtSprintf(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -panic(fmt.Sprintf("bad: %d", x))`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`fmt.Sprintf("bad: %d", x)`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_ReturnBare(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -return")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v, want ActionReturn", d.Action)
	}
	if len(d.ActionArgs) != 0 {
		t.Errorf("ActionArgs = %v, want empty", d.ActionArgs)
	}
}

func TestParseDirective_ReturnSingleValue(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -return(-1)")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{"-1"}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_ReturnMultiValue(t *testing.T) {
	d := ParseDirective(`// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{"0", `fmt.Errorf("empty")`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
	if d.Expr != "len(s) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_Continue(t *testing.T) {
	d := ParseDirective("// @inco: n > 0, -continue")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionContinue {
		t.Errorf("Action = %v, want ActionContinue", d.Action)
	}
	if d.Expr != "n > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_Break(t *testing.T) {
	d := ParseDirective("// @inco: n != 42, -break")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionBreak {
		t.Errorf("Action = %v, want ActionBreak", d.Action)
	}
	if d.Expr != "n != 42" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_Log(t *testing.T) {
	d := ParseDirective(`// @inco: x > 0, -log("x must be positive", x)`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionLog {
		t.Errorf("Action = %v, want ActionLog", d.Action)
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if len(d.ActionArgs) != 2 {
		t.Errorf("ActionArgs = %v, want 2 args", d.ActionArgs)
	}
}

// ---------------------------------------------------------------------------
// Edge cases — comma inside expression
// ---------------------------------------------------------------------------

func TestParseDirective_CommaInFuncCallIsNotAction(t *testing.T) {
	// The comma inside foo(a, b) should NOT be treated as an action separator.
	d := ParseDirective("// @inco: foo(a, b) > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "foo(a, b) > 0" {
		t.Errorf("Expr = %q, want %q", d.Expr, "foo(a, b) > 0")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
}

func TestParseDirective_CommaInFuncCallWithAction(t *testing.T) {
	d := ParseDirective(`// @inco: foo(a, b) > 0, -panic("bad")`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "foo(a, b) > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	want := []string{`"bad"`}
	if !reflect.DeepEqual(d.ActionArgs, want) {
		t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, want)
	}
}

func TestParseDirective_MapLiteralComma(t *testing.T) {
	// m[k] is not depth-tracked by parens, but this should still be expr-only.
	d := ParseDirective("// @inco: m[k] > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "m[k] > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

func TestParseDirective_NestedParenComma(t *testing.T) {
	d := ParseDirective("// @inco: f(g(a, b), c) != nil, -return(-1)")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "f(g(a, b), c) != nil" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
}

// ---------------------------------------------------------------------------
// Block comment form
// ---------------------------------------------------------------------------

func TestParseDirective_BlockComment(t *testing.T) {
	d := ParseDirective("/* @inco: x > 0 */")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
}

// ---------------------------------------------------------------------------
// stripComment helper
// ---------------------------------------------------------------------------

func TestStripComment(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"// hello", "hello"},
		{"//hello", "hello"},
		{"/* block */", "block"},
		{"  // spaced  ", "spaced"},
		{"not a comment", ""},
	}
	for _, c := range cases {
		got := stripComment(c.input)
		if got != c.want {
			t.Errorf("stripComment(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// splitTopLevel helper
// ---------------------------------------------------------------------------

func TestSplitTopLevel(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"a, b, c", []string{"a", "b", "c"}},
		{`f(x, y), z`, []string{"f(x, y)", "z"}},
		{`"a,b", c`, []string{`"a,b"`, "c"}},
		{"single", []string{"single"}},
		{"", nil},
		// Raw string with comma inside.
		{"`a,b`, c", []string{"`a,b`", "c"}},
		// Raw string with backslash (no escaping in raw strings).
		{"`a\\b`, c", []string{"`a\\b`", "c"}},
		// Double-quoted string with escaped quote.
		{`"a\"b", c`, []string{`"a\"b"`, "c"}},
		// Double-quoted string with escaped backslash before closing quote.
		{`"a\\", c`, []string{`"a\\"`, "c"}},
	}
	for _, c := range cases {
		got := splitTopLevel(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitTopLevel(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// splitTopLevel — deep nesting & edge cases (P0 corner case tests)
// ---------------------------------------------------------------------------

func TestSplitTopLevel_DeepNesting(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{
			"triple nested",
			`f(g(h(x, y), z), w), k`,
			[]string{`f(g(h(x, y), z), w)`, "k"},
		},
		{
			"mixed bracket types",
			`m[k], f(a, b), s{x: 1}`,
			[]string{"m[k]", "f(a, b)", "s{x: 1}"},
		},
		{
			"string with parens",
			`"f(x,y)", z`,
			[]string{`"f(x,y)"`, "z"},
		},
		{
			"raw string with parens",
			"`f(x,y)`, z",
			[]string{"`f(x,y)`", "z"},
		},
		{
			"nested quotes in func",
			`fmt.Sprintf("a=%d, b=%d", a, b), c`,
			[]string{`fmt.Sprintf("a=%d, b=%d", a, b)`, "c"},
		},
		{
			"empty string arg",
			`"", x`,
			[]string{`""`, "x"},
		},
		{
			"whitespace only",
			"  ",
			nil,
		},
		{
			"single whitespace-padded",
			"  x  ",
			[]string{"x"},
		},
		{
			"map literal with nested braces",
			`map[string]int{"a": 1, "b": 2}, ok`,
			[]string{`map[string]int{"a": 1, "b": 2}`, "ok"},
		},
		{
			"escaped backslash then comma",
			`"a\\", "b"`,
			[]string{`"a\\"`, `"b"`},
		},
		{
			"raw string with backtick-like content",
			"`hello\nworld`, rest",
			[]string{"`hello\nworld`", "rest"},
		},
		{
			"4-level nesting",
			`a(b(c(d(1, 2), 3), 4), 5)`,
			[]string{"a(b(c(d(1, 2), 3), 4), 5)"},
		},
		{
			"consecutive string args",
			`"a", "b", "c"`,
			[]string{`"a"`, `"b"`, `"c"`},
		},
		{
			"bracket inside string",
			`"m[k]", v`,
			[]string{`"m[k]"`, "v"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := splitTopLevel(c.input)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("splitTopLevel(%q) = %v, want %v", c.input, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParseDirective — complex nesting (P0 corner case tests)
// ---------------------------------------------------------------------------

func TestParseDirective_DeeplyNestedExpr(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		expr   string
		action ActionKind
		args   []string
	}{
		{
			"triple nested func call in expr",
			"// @inco: f(g(h(x, y), z)) > 0",
			"f(g(h(x, y), z)) > 0",
			ActionPanic,
			nil,
		},
		{
			"triple nested with action",
			`// @inco: f(g(h(x, y), z)) > 0, -return(-1)`,
			"f(g(h(x, y), z)) > 0",
			ActionReturn,
			[]string{"-1"},
		},
		{
			"fmt.Sprintf inside return args",
			`// @inco: x > 0, -return(0, fmt.Errorf("val=%d, limit=%d", x, limit))`,
			"x > 0",
			ActionReturn,
			[]string{"0", `fmt.Errorf("val=%d, limit=%d", x, limit)`},
		},
		{
			"comma in quoted string in panic",
			`// @inco: x > 0, -panic(fmt.Sprintf("x=%d, want>0", x))`,
			"x > 0",
			ActionPanic,
			[]string{`fmt.Sprintf("x=%d, want>0", x)`},
		},
		{
			"multi-arg return with nested calls",
			`// @inco: ok, -return(nil, wrap(inner(a, b), "msg"))`,
			"ok",
			ActionReturn,
			[]string{"nil", `wrap(inner(a, b), "msg")`},
		},
		{
			"log with multiple complex args",
			`// @inco: n > 0, -log("count is", n, fmt.Sprintf("(%d)", n))`,
			"n > 0",
			ActionLog,
			[]string{`"count is"`, "n", `fmt.Sprintf("(%d)", n)`},
		},
		{
			"expr with type assertion",
			`// @inco: v.(type) != nil, -panic("type assertion failed")`,
			"v.(type) != nil",
			ActionPanic,
			[]string{`"type assertion failed"`},
		},
		{
			"boolean expression with multiple ops",
			`// @inco: a != nil && b != nil && len(c) > 0, -return(ErrInvalid)`,
			"a != nil && b != nil && len(c) > 0",
			ActionReturn,
			[]string{"ErrInvalid"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ParseDirective(c.input)
			if d == nil {
				t.Fatal("got nil")
			}
			if d.Expr != c.expr {
				t.Errorf("Expr = %q, want %q", d.Expr, c.expr)
			}
			if d.Action != c.action {
				t.Errorf("Action = %v, want %v", d.Action, c.action)
			}
			if c.args == nil {
				if len(d.ActionArgs) != 0 {
					t.Errorf("ActionArgs = %v, want empty", d.ActionArgs)
				}
			} else if !reflect.DeepEqual(d.ActionArgs, c.args) {
				t.Errorf("ActionArgs = %v, want %v", d.ActionArgs, c.args)
			}
		})
	}
}

// TestParseDirective_ActionLikeInString verifies that strings containing
// patterns resembling actions (e.g. "-return" in a string literal) are
// handled correctly.
func TestParseDirective_ActionLikeInString(t *testing.T) {
	// The -return inside the string is at depth>0 (inside Sprintf parens),
	// so actionRe should NOT match it as a separator.
	d := ParseDirective(`// @inco: x > 0, -panic(fmt.Sprintf("-return(%d)", x))`)
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q, want %q", d.Expr, "x > 0")
	}
}

// TestParseDirective_TrailingWhitespace ensures trailing spaces don't break parsing.
func TestParseDirective_TrailingWhitespace(t *testing.T) {
	d := ParseDirective("// @inco: x > 0, -return(1)   ")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x > 0" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
}

func TestParseDirective_Inco_NotNegated(t *testing.T) {
	d := ParseDirective("// @inco: x > 0")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Negated {
		t.Error("Negated should be false for @inco:")
	}
}

// ---------------------------------------------------------------------------
// @if: directive — condition used as-is (no negation)
// ---------------------------------------------------------------------------

func TestParseDirective_If_ExprOnly(t *testing.T) {
	d := ParseDirective("// @if: err != nil")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "err != nil" {
		t.Errorf("Expr = %q, want %q", d.Expr, "err != nil")
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v, want ActionPanic", d.Action)
	}
	if !d.Negated {
		t.Error("Negated should be true for @if:")
	}
}

func TestParseDirective_If_WithReturn(t *testing.T) {
	d := ParseDirective("// @if: err != nil, -return(nil, err)")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "err != nil" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionReturn {
		t.Errorf("Action = %v", d.Action)
	}
	if !d.Negated {
		t.Error("Negated should be true for @if:")
	}
}

func TestParseDirective_If_WithPanic(t *testing.T) {
	d := ParseDirective("// @if: x == nil, -panic(\"x is nil\")")
	if d == nil {
		t.Fatal("got nil")
	}
	if d.Expr != "x == nil" {
		t.Errorf("Expr = %q", d.Expr)
	}
	if d.Action != ActionPanic {
		t.Errorf("Action = %v", d.Action)
	}
	if !d.Negated {
		t.Error("Negated should be true for @if:")
	}
}

func TestParseDirective_If_Nil(t *testing.T) {
	for _, input := range []string{
		"// @if",     // missing colon
		"// @if:",    // no expression
		"// @if:   ", // whitespace only
		"// @IF: x",  // wrong case
	} {
		if d := ParseDirective(input); d != nil {
			t.Errorf("ParseDirective(%q) = %+v, want nil", input, d)
		}
	}
}
