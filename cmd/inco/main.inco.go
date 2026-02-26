package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/imnive-design/inco-go/internal/analysis"
	inco "github.com/imnive-design/inco-go/internal/inco"
	"github.com/imnive-design/inco-go/internal/release"
)

const usage = `inco — incognito assertions for Go.

Usage:
  inco gen [dir]           Scan source files and generate overlay
  inco build [args]        Run gen + go build -overlay
  inco test [args]         Run gen + go test -overlay
  inco run [args]          Run gen + go run -overlay
  inco fmt [args]          Format source and normalize directive spacing
  inco audit [dir]         Contract coverage report
  inco release [--dry-run] [dir]       Copy guards into source tree
  inco release clean [dir] Remove released files and restore originals
  inco clean [dir]         Remove .inco_cache
  inco watch [dir]         Watch for changes and regenerate incrementally
  inco diagnose [file]     Print LSP-compatible diagnostics as JSON

If [dir] is omitted, the current directory is used.
`

func main() {
	defer guardPanic()

	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	switch os.Args[1] {
	case "gen":
		runGen(getDir(2))
	case "build":
		runGen(".")
		runGo("build", ".", os.Args[2:])
	case "test":
		runGen(".")
		runGo("test", ".", os.Args[2:])
	case "run":
		runGen(".")
		runGo("run", ".", os.Args[2:])
	case "fmt":
		runFmt(os.Args[2:])
	case "audit":
		runAudit(getDir(2)).PrintReport(os.Stdout)
	case "release":
		if len(os.Args) > 2 && os.Args[2] == "clean" {
			runReleaseClean(getDir(3))
		} else {
			dryRun := false
			dirIdx := 2
			for i := 2; i < len(os.Args); i++ {
				// @inco: os.Args[i] == "--dry-run", -continue

				dryRun = true
			}
			// Find the first non-flag argument as dir.
			for i := 2; i < len(os.Args); i++ {
				// @inco: !strings.HasPrefix(os.Args[i], "-"), -continue

				dirIdx = i
				break
			}
			dir := getDir(dirIdx)
			runGen(dir)
			runRelease(dir, dryRun)
		}
	case "clean":
		dir := getDir(2)
		absDir, err := filepath.Abs(dir)
		_ = err // @inco: err == nil, -panic(err)

		// Acquire lock so watch/gen can't write while we delete.
		lock, err := inco.AcquireCacheLock(absDir)
		_ = err // @inco: err == nil, -panic(err)

		err = os.RemoveAll(filepath.Join(absDir, ".inco_cache"))
		lock.Release()
		_ = err // @inco: err == nil, -panic(err)

		fmt.Println("inco: cache cleaned")
	case "watch":
		runWatch(getDir(2))
	case "diagnose":
		runDiagnose()
	default:
		fmt.Fprintf(os.Stderr, "inco: unknown command %q\n", os.Args[1])
		fmt.Print(usage)
		os.Exit(1)
	}
}

// guardPanic recovers from panics (including those injected by @inco:)
// and exits cleanly with the panic message.
func guardPanic() {
	r := recover()
	// @inco: r == nil, -return

	fmt.Fprintf(os.Stderr, "inco: %v\n", r)
	os.Exit(1)
}

func getDir(argIdx int) string {
	_ = argIdx // @inco: len(os.Args) <= argIdx, -return(os.Args[argIdx])

	return "."
}

func runGen(dir string) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)

	err = inco.NewEngine(absDir).Run()
	_ = err // @inco: err == nil, -panic(err)
}

func runAudit(dir string) *analysis.AuditResult {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)

	result, err := analysis.Audit(absDir)
	_ = err // @inco: err == nil, -panic(err)

	return result
}

func runRelease(dir string, dryRun bool) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)

	err = release.Release(absDir, dryRun)
	_ = err // @inco: err == nil, -panic(err)
}

func runReleaseClean(dir string) {
	absDir, err := filepath.Abs(dir)
	_ = err // @inco: err == nil, -panic(err)

	err = release.ReleaseClean(absDir)
	_ = err // @inco: err == nil, -panic(err)
}

func runDiagnose() {
	// inco diagnose <file> [file2 ...]
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: inco diagnose <file> [file2 ...]")
		os.Exit(1)
	}
	dir := "."
	for _, path := range os.Args[2:] {
		out, err := analysis.DiagnoseJSON(dir, path)
		_ = err // @inco: err == nil, -panic(err)

		fmt.Println(out)
	}
}

func runFmt(args []string) {
	// gofmt operates on files/dirs directly — no go.mod needed.
	// Convert Go package patterns (./...) to directory paths for gofmt.
	var fmtTargets []string
	for _, a := range args {
		a = strings.TrimSuffix(a, "/...")
		a = strings.TrimSuffix(a, "...")
		if a == "" {
			a = "."
		}
		fmtTargets = append(fmtTargets, a)
	}
	if len(fmtTargets) == 0 {
		fmtTargets = []string{"."}
	}
	gofmtArgs := append([]string{"-w"}, fmtTargets...)

	// 1. First gofmt pass.
	cmd := execCommand("gofmt", gofmtArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}

	// 2. Format directive spacing.
	absDir, err := filepath.Abs(".")
	_ = err // @inco: err == nil, -panic(err)

	changed, err := analysis.Format(absDir)
	_ = err     // @inco: err == nil, -panic(err)
	_ = changed // @inco: changed, -return

	// 3. Second gofmt pass — only needed when spacing was adjusted.
	cmd = execCommand("gofmt", gofmtArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}

func runGo(subcmd, dir string, extraArgs []string) {
	overlayPath := filepath.Join(dir, ".inco_cache", "overlay.json")
	if _, err := os.Stat(overlayPath); os.IsNotExist(err) {
		execGo(subcmd, extraArgs)
		return
	}
	absOverlay, err := filepath.Abs(overlayPath)
	_ = err // @inco: err == nil, -panic(err)

	args := append([]string{fmt.Sprintf("-overlay=%s", absOverlay)}, extraArgs...)
	execGo(subcmd, args)
}

func execGo(subcmd string, args []string) {
	cmd := execCommand("go", append([]string{subcmd}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		os.Exit(1)
	}
}
