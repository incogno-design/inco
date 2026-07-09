# Inco — Agent Handbook

## What is Inco

Inco is a compile-time assertion engine for Go. You write guard conditions as
plain comments; inco generates `if` blocks into shadow files and injects them via
`go build -overlay`. Zero source file invasion.

Two directive prefixes:

| Prefix | Condition | Generated | Meaning |
|--------|-----------|-----------|---------|
| `@inco:` | inverted | `if !(expr) { action }` | **Contract** — state what you expect to be true |
| `@if:` | as-is | `if expr { action }` | **Logic flow** — direct `if` guard-clause replacement |

```go
// @inco: err == nil, -return(nil, err)   // contract: "I expect no error"
// @if:   err != nil, -return(nil, err)   // flow:     "if error, return"
```

Prefer `@inco:` for contracts (preconditions, nil/error checks, range validation).
Use `@if:` when migrating a plain `if` guard clause verbatim.

## Directives are Guards, Not Logic

- **Directive**: nil checks, error checks, range validation, preconditions, early returns, loop filtering.
- **Keep `if`**: business branches, conditional selection, anything with `else`, multi-line bodies/side effects.

```go
// @inco: db != nil, -panic("db required")   // guard
if val < lo { return lo }                     // business logic — keep
```

## Two Forms

- **Standalone** (preferred): entire line is a comment. Use when the variable is
  referenced elsewhere in the function.

  ```go
  // @inco: n > 0, -panic("must be positive")
  ```

- **Inline**: appended to a code line. Use `_ = var //` when the variable is *not*
  referenced elsewhere (suppresses the unused-variable error).

  ```go
  _ = err // @inco: err == nil, -panic(err)
  ```

## Actions

| Action | Syntax |
|--------|--------|
| panic (default msg) | `// @inco: <expr>` |
| panic (custom) | `-panic("msg")` |
| return | `-return(vals...)` / bare `-return` |
| continue / break | `-continue` / `-break` |
| log | `-log(args...)` → `log.Println(args...)` |

## if → Directive Conversion

```go
// if err != nil { return nil, err }  →  _ = err   // @inco: err == nil, -return(nil, err)
// if x == nil { panic("x is nil") }  →            // @inco: x != nil,  -panic("x is nil")
// if !valid { continue }             →  _ = valid // @inco: valid,     -continue
```

Do **not** convert: business branches (`if val < lo`), `if/else`, multi-line bodies,
or `if v, ok := m[k]; ok` (extract the init first, then convert).

## Best Practices

1. **Long conditions → extract a named bool first.** Keep directive lines scannable
   (multiple clauses or >~60 chars).

   ```go
   skipDir := strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata"
   _ = skipDir // @if: skipDir, -return(filepath.SkipDir)
   ```

2. **Group directives together**, don't scatter them among logic. Independent checks:
   declare all, then all directives (use unique names to avoid shadowing). Sequential
   checks: declare → directive → blank line, repeat.
3. **Suppress unused vars *before* the directive**, not after:

   ```go
   s, ok := actionNames[k]
   _ = s
   _ = ok // @inco: !ok, -return(s)
   ```

4. **Never write both `if` and a directive** for the same check — let inco generate the block.
5. **`@inco:` shines at function entry** — the signature declares types, `@inco:`
   declares value constraints:

   ```go
   func Process(db *sql.DB, id string) error {
       // @inco: db != nil, -panic("db required")
       // @inco: id != "", -return(fmt.Errorf("empty id"))
   }
   ```

## File Conventions

- `foo.inco.go` — source files with directives (recommended naming).
- `.inco_cache/` — generated shadow files, `overlay.json`, `manifest.json` (gitignore it).
- `.incoignore` — exclude paths (.gitignore syntax).
- Auto-skipped: hidden dirs, `vendor/`, `testdata/`, `_test.go`.

Directives may reference common stdlib (`fmt`, `errors`, `strings`, `strconv`, `os`,
`io`, `filepath`, `time`, `context`, `sync`, `log`, `json`, `http`, …) and project
deps — auto-import handles them. Obscure packages (`unsafe`, `reflect`, `runtime`,
`syscall`, `go/ast`, …) are **not** auto-imported.

## Workflow

- **Tests before every commit**: this project uses `inco test ./...` (generates the
  overlay, then runs `go test -overlay`) — not plain `go test`. Fix failures before
  committing. Run the *full* suite after multi-step changes.
- **After editing, run `go vet ./...`** to catch unused variables.

```bash
go install github.com/incogno-design/inco/cmd/inco@latest

inco build ./...      # gen + build
inco test  ./...      # gen + test
inco audit .          # coverage report
inco fmt   .          # normalize directive spacing
inco release .        # bake guards into source
inco release clean .  # restore
inco clean .          # delete .inco_cache/
```
