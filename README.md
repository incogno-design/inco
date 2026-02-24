# Inco

Inco is a compile-time assertion engine for Go. Write contract directives as plain comments; they are transformed into runtime guards in shadow files, wired in via `go build -overlay`.

## Philosophy

Business logic should be pure. Defensive noise — `if x == nil`, `if err != nil` — belongs in the shadow, not in your source.

Write the intent; Inco generates the shield.

### `if` is for logic, not for guarding

In an Inco codebase, `if` should express **logic flow** — branching on business conditions, selecting behavior. Not for:

- **Nil guards** → `// @inco: ptr != nil`
- **Value validation** → `// @inco: x > 0`
- **Range checks** → `// @inco: i < len(s)`

When every defensive check is a directive, the remaining `if` statements carry **real** semantic weight — genuine decisions, not boilerplate.

## Directive Syntax

Two prefixes — `@inco:` (contract, condition inverted) and `@if:` (guard, same as `if`).

`@inco:` is the core — write the condition you expect to hold, inco generates the inverse guard. `@if:` exists to lower the migration barrier: existing `if` guard clauses can be converted to directives with zero mental overhead — just copy the condition as-is.

Two forms — **standalone** and **inline**:

### Standalone (entire line is directive)

```
// @inco: <expr>
// @inco: <expr>, -panic("msg")
// @inco: <expr>, -return(values...)
// @if: <expr>, -continue
// @if: <expr>, -break
```

### Inline (code + trailing directive)

```go
_ = err // @if: err != nil, -panic(err)
_ = skip // @inco: !skip, -return(filepath.SkipDir)
```

Inline directives attach to a code statement at the end of the line. The engine uses AST analysis to distinguish inline directives from decorative comments (e.g. struct field comments are ignored).

The default action is `-panic` with an auto-generated message.

### Example: Bank Transfer

```go
// Transfer moves amount cents from one account to another.
func Transfer(from *Account, to *Account, amount int) error {
    // @inco: from != nil
    // @inco: to != nil
    // @inco: from != to, -panic("cannot transfer to self")
    // @inco: amount > 0, -panic("amount must be positive")
    // @inco: from.Balance >= amount, -return(fmt.Errorf("insufficient funds: have %d, need %d", from.Balance, amount))

    from.Balance -= amount
    to.Balance += amount

    fmt.Printf("transferred %d from %s to %s\n", amount, from.ID, to.ID)
    return nil
}
```

Five directives, each expressing a precondition the caller must satisfy. No `if` noise — just intent.

### Directive Semantics

`@inco:` is a **contract** — "I expect this to hold". Generated code is `if !(<expr>) { action }`. Write the condition you expect to be true. This is the idiomatic way to express preconditions in Inco.

`@if:` is a **migration helper** — same condition as `if`. Generated code is `if <expr> { action }`. When migrating an existing codebase, `if err != nil { return err }` becomes `// @if: err != nil, -return(err)` with no condition inversion needed.

```go
// @inco: err == nil, -panic(err)    // contract: I expect no error
// @inco: n > 0, -continue           // contract: n must be positive
// @if: err != nil, -return(err)     // guard: if error, return
// @if: x == nil, -panic("x nil")    // guard: if nil, panic
```

### Actions

| Action | Syntax | Meaning |
|--------|--------|---------|
| panic (default) | `// @inco: <expr>` | Panic with auto message |
| panic (custom) | `// @inco: <expr>, -panic("msg")` | Panic with custom message |
| return | `// @if: <expr>, -return(vals...)` | Return specified values |
| return (bare) | `// @if: <expr>, -return` | Bare return |
| continue | `// @if: <expr>, -continue` | Continue enclosing loop |
| break | `// @if: <expr>, -break` | Break enclosing loop |
| log | `// @if: <expr>, -log(args...)` | log.Println(args...) |

### Generated Output

After `inco gen`, the above becomes a shadow file in `.inco_cache/`:

```go
func Transfer(from *Account, to *Account, amount int) error {
    if !(from != nil) {
        panic("inco violation: from != nil (at transfer.inco.go:14)")
    }
    if !(to != nil) {
        panic("inco violation: to != nil (at transfer.inco.go:15)")
    }
    if !(from != to) {
        panic("cannot transfer to self")
    }
    if !(amount > 0) {
        panic("amount must be positive")
    }
    if !(from.Balance >= amount) {
        return fmt.Errorf("insufficient funds: have %d, need %d", from.Balance, amount)
    }

    from.Balance -= amount
    to.Balance += amount

    fmt.Printf("transferred %d from %s to %s\n", amount, from.ID, to.ID)
    return nil
}
```

Shadow files live in `.inco_cache/` and are wired in via `go build -overlay`.

## Auto-Import

When directive arguments reference packages (e.g. `fmt.Sprintf`, `errors.New`), Inco automatically adds the corresponding import to the shadow file via `astutil.AddImport`. No manual import management needed.

Standard library auto-imports are restricted to a curated whitelist of common packages:

| Category | Packages |
|----------|----------|
| Core | `fmt` `errors` `strings` `strconv` `bytes` `regexp` `sort` `slices` `maps` `math` `cmp` |
| OS / IO | `os` `io` `filepath` `path` `bufio` |
| Time / Sync | `time` `context` `sync` |
| Encoding | `json` `xml` `csv` `base64` `hex` |
| Net | `http` `url` |
| Log | `log` `slog` |

Third-party dependencies used in your module are resolved dynamically via `go list -e -deps ./...` (results are cached across files). Ambiguous package names (e.g. a local package shadowing a stdlib name) are removed from the mapping to prevent incorrect imports. Internal and vendored packages are also filtered out.

## Usage

```bash
# Install
go install github.com/imnive-design/inco-go/cmd/inco@latest

# Generate overlay
inco gen [dir]

# Build / Test / Run with contracts enforced
inco build ./...
inco test ./...
inco run .

# Release: bake guards into source tree (no overlay needed)
inco release [dir]

# Revert release
inco release clean [dir]

# Contract coverage audit
inco audit [dir]

# Clean cache
inco clean [dir]
```

## Release Mode

`inco release` bakes guards into your source tree — no overlay, no build tags, no `inco` tool needed at build time.

### Convention: `.inco.go` files

Name source files that contain directives with a `.inco.go` extension:

```
foo.inco.go   ← source with @inco:/@if: directives
```

`inco gen` and `inco build` treat `.inco.go` files exactly like `.go` files (they end in `.go`, so the scanner picks them up).

### Release workflow

```bash
inco release .
```

For each `.inco.go` file in the overlay:

1. **Generate** `foo.go` — shadow content with guards injected (the `// Code generated by inco. DO NOT EDIT.` header is prepended; `//line` directives are preserved so stack traces still point to original source lines)
2. **Backup** `foo.inco.go` → `foo.inco` — renamed so the Go compiler ignores it

After release:

```bash
go build ./...    # compiles foo.go (with guards) — no overlay, no inco needed
```

### Dry run

Preview what release would do without writing any files:

```bash
inco release --dry-run .
```

Prints `[dry-run] <path>` for each file that would be generated or renamed.

### Restore

```bash
inco release clean .
```

This removes each generated `foo.go` and restores `foo.inco` → `foo.inco.go`.

### When to use

- **Distribution**: ship a self-contained project with contracts baked in
- **CI/CD**: build with guards without installing `inco`
- **One-click restore**: `inco release clean` brings you back to development mode

### Single-repo distribution (CI example)

If you want to develop on an `inco` branch (with `.inco.go` sources) and automatically publish released code to `main`, add a workflow like [`.github/workflows/release-single-repo.yml`](.github/workflows/release-single-repo.yml):

```yaml
name: Release inco → main

on:
  push:
    branches: [inco]

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: inco

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Install inco
        run: go install github.com/imnive-design/inco-go/cmd/inco@latest

      - name: Release
        run: |
          inco release .

          # Rename remaining .inco.go files (those without directives) → .go
          find . -name '*.inco.go' -not -path './.inco_cache/*' | while read -r f; do
            target="${f%.inco.go}.go"
            mv "$f" "$target"
          done

          rm -rf .inco_cache

      - name: Push to main
        run: |
          git config user.name  "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git commit -m "release: inco@${GITHUB_SHA::7}" --allow-empty
          git push origin HEAD:main --force
```

This gives you a clean separation: `inco` branch holds the `.inco.go` sources with directives, `main` branch always contains plain `.go` files with guards baked in — consumers can `go install` or `go get` from `main` without ever knowing about inco.

## Build from Source

```bash
go install github.com/imnive-design/inco-go/cmd/inco@latest
```

```bash
make build      # inco build → bin/inco
make test       # Run tests with contracts enforced
make gen        # Regenerate overlay
make clean      # Remove .inco_cache/ and bin/
make install    # Install to $GOPATH/bin
```

## Audit

`inco audit` scans your codebase and reports:

- **@inco: coverage**: percentage of functions guarded by at least one directive
- **inco/(if+inco) ratio**: what fraction of all conditional guards are directives
- **Per-file breakdown**: directive count, `if` count, function count, and guarded function count per file
- **Unguarded functions**: list of functions without any directive (closures excluded)
- **Ignored files**: files/dirs excluded by `.incoignore`

Test files (`_test.go`), hidden directories, `vendor/`, and `testdata/` are always skipped.

```
$ inco audit .
inco audit — contract coverage report
======================================

  Files scanned:  9
  Functions:      52

@inco: coverage:
  With @inco::     30 / 52  (57.7%)
  Without @inco::  22 / 52  (42.3%)

Directive vs if:
  @inco::           67
  ─────────────────────
  Total directives:   67
  Native if stmts:    59
  inco/(if+inco):     53.2%

Per-file breakdown:
  File              @inco:  if  funcs  guarded
  ────────────────  ──────  ──  ─────  ───────
  engine.inco.go        40  20     15       12
  directive.inco.go      4   8      5        3
  ...

Functions without @inco: (22):
  internal/inco/types.inco.go:15  String

Ignored by .incoignore (4):
  example/demo.inco.go
  example/edge_cases.inco.go
  example/generics.inco.go
  example/transfer.inco.go
```

The `inco/(if+inco)` ratio reflects the degree of separation between guards and business logic — **higher is not necessarily better**. A ratio that is too high suggests business branches may have been incorrectly converted to directives. The ideal state is: all guards use `@inco:` or `@if:`, all business logic stays in `if`, and the ratio naturally settles at a reasonable value.

## How It Works

1. `inco gen` scans all `.go` files for `// @inco:` / `// @if:` comments (respecting `.incoignore`; test files, hidden directories, `vendor/`, and `testdata/` are always skipped)
2. Uses `go/ast` to classify each directive as **standalone** (comment-only line) or **inline** (attached to a statement)
3. Generates shadow files in `.inco_cache/` — standalone directives become `if`-blocks in place; inline directives keep the code line and inject the `if`-block after it
4. Injects `//line` directives so panic stack traces point back to **original** source lines
5. Produces `overlay.json` for `go build -overlay`
6. Shadow files replace originals via overlay — source files are not modified on disk

### AST-Based Classification

The engine parses each source file as an AST and collects the set of line numbers that contain Go statements (`AssignStmt`, `ExprStmt`, `ReturnStmt`, `IncDecStmt`, `SendStmt`, `GoStmt`, `DeferStmt`, `BranchStmt`). When a directive comment is found:

- **Comment-only line** → standalone directive (full line replaced by `if`-block)
- **Line in statement set** → inline directive (code preserved, `if`-block injected after)
- **Other** (struct field comment, etc.) → ignored

This prevents false matches on decorative comments like `RequireCount int // @inco: directives`.

### Incremental Builds

The engine maintains a `manifest.json` in `.inco_cache/` that records a SHA-256 hash for each source file. On subsequent runs, files with unchanged hashes are skipped entirely — only modified files are re-parsed and re-generated. Orphaned shadow files (whose source has been deleted) are automatically cleaned up.

### Parallel Processing

File parsing and shadow generation run in parallel across `GOMAXPROCS` worker goroutines, each with an independent `token.FileSet` to avoid contention. The first error is propagated atomically.

### Shadow File Naming

Shadow files use content-hash naming: `<basename>_<sha256[:16]>.go`. This ensures stable Go build cache keys — editing a file produces a new shadow name, preventing stale cache hits.

## Project Structure

```
cmd/inco/           CLI: gen, build, test, run, audit, release, clean
internal/inco/      Core engine:
  audit.inco.go       Contract coverage auditing
  directive.inco.go   Directive parsing (@inco:, @if:)
  engine.inco.go      AST processing, code generation, overlay I/O
  ignore.inco.go      .incoignore file parsing and hierarchical matching
  release.inco.go     Release mode: bake guards into source
  types.inco.go       Core types (Directive, ActionKind, Overlay)
  walk.inco.go        Shared file traversal logic
```

## Notes

Inco is self-hosting — it uses `@inco:` directives in its own source code. Since directives are plain Go comments, the code compiles with or without expansion.

### The Acknowledgement Pattern

`_ = var` is the **acknowledgement pattern** — you explicitly tell the compiler "I know this variable exists; its guard is handled by inco." When a variable is only used in a directive, `_ = var` makes the intent clear:

```go
_ = err // @inco: err == nil, -panic(err)
```

`_ = err` acknowledges the variable in source; the directive generates the real guard in the overlay. The code compiles cleanly with or without inco.

## Design

- **Comment-based**: Plain Go comments — no custom syntax, no broken IDE support
- **Fail-fast**: panic by default — or return, continue, break as needed
- **Zero-overhead option**: Strip directives in production, or keep for fail-fast
- **Incremental**: SHA-256 manifest — only changed files are re-processed
- **Parallel**: Worker goroutines scale to `GOMAXPROCS` with independent `token.FileSet`
- **Cache-friendly**: Content-hash (SHA-256) based shadow filenames for stable build cache
- **Source-mapped**: `//line` directives preserve original file:line in stack traces
- **Auto-import**: Package references in directive args are auto-imported (with disambiguation)

## Best Practices & Common Pitfalls

### 1. Long Expression Optimization

If the expression is too long or complex, extract it into a boolean variable first — this improves readability and works well with `_ = var`:

```go
// ❌ Verbose and hard to read
// @inco: !(si.ParentStateInfo != nil && si.ParentStateInfo != parentStateInfo), -panic(...)

// ✅ Clear
isInvalid := si.ParentStateInfo != nil && si.ParentStateInfo != parentStateInfo
_ = isInvalid // @inco: !isInvalid, -panic(...)
```

### 2. Repeated Var Assignment for Multiple Guards

When applying multiple directives to the same variable consecutively (e.g., log first, then panic), repeat `_ = var` before each directive line:

```go
// ✅ Best practice: explicitly suppress unused checks for each line
_ = err // @inco: err == nil, -log("error occurred:", err)
_ = err // @inco: err == nil, -panic(err)
```

### 3. `@inco:` at Function Head, `@if:` in Flow — Prefer Logic Separation

**Function entry** — always use `@inco:` (contracts):

```go
func Process(db *sql.DB, id string) error {
    // @inco: db != nil, -panic("db required")
    // @inco: id != "", -return(fmt.Errorf("empty id"))
    ...
}
```

**Mid-flow guards** — `@if:` is acceptable for inline guard clauses:

```go
result, err := doWork()
_ = err // @if: err != nil, -return(nil, err)
```

**Better: extract logic into a function, then use `@inco:` at entry**:

```go
// ❌ Inline @if: scattered in flow
func Run() error {
    data, err := fetch()
    _ = err // @if: err != nil, -return(err)
    parsed, err := parse(data)
    _ = err // @if: err != nil, -return(err)
    ...
}

// ✅ Extract + @inco: at each function head
func Run() error {
    data := mustFetch()    // panics on error via @inco:
    parsed := mustParse(data)
    ...
}

func mustFetch() []byte {
    data, err := fetch()
    _ = err // @inco: err == nil, -panic(err)
    return data
}
```

The goal: push guard logic to function boundaries so the caller stays clean and every function opens with clear contracts.

## License

MIT
