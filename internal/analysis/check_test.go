package analysis

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSrc = "package p\n\nfunc F(x *int) int {\n\t// @inco: x != nil, -return(0)\n\n\treturn *x\n}\n"

// A bare `// @inco:` with no expression: ParseDirective rejects it, so
// DiagnoseFile flags an invalid-directive warning.
const malformedSrc = "package p\n\nfunc G() int {\n\t// @inco:\n\treturn 0\n}\n"

func writeSrc(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckFlagsMalformedDirective(t *testing.T) {
	dir := t.TempDir()
	writeSrc(t, dir, "good.inco.go", validSrc)
	writeSrc(t, dir, "bad.inco.go", malformedSrc)

	problems, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 {
		t.Fatalf("Check returned %d problems, want 1: %+v", len(problems), problems)
	}
	if problems[0].Code != "invalid-directive" {
		t.Errorf("code = %q, want invalid-directive", problems[0].Code)
	}
	if !strings.HasSuffix(problems[0].Path, "bad.inco.go") {
		t.Errorf("path = %q, want …/bad.inco.go", problems[0].Path)
	}
}

func TestCheckCleanDirHasNoProblems(t *testing.T) {
	dir := t.TempDir()
	writeSrc(t, dir, "ok.inco.go", validSrc)

	problems, err := Check(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("Check returned %d problems, want 0: %+v", len(problems), problems)
	}
}
