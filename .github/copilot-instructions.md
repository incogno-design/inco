# Inco — Copilot Handbook

## What is Inco

Inco is a compile-time assertion engine for Go. Write `// @inco:` directives in comments, and inco automatically generates corresponding `if` guard blocks, injected via `go build -overlay`. Zero source file invasion.

## Core Rules

### 1. `@inco:` is for Guards, Not Logic

**Use `@inco:`**: nil checks, error checks, range validation, preconditions  
**Use `if`**: business branches, conditional selection, flow control

```go
// ✅ Guard → @inco:
// @inco: db != nil
// @inco: err == nil, -panic(err)
// @inco: len(s) > 0, -return(0, fmt.Errorf("empty"))

// ✅ Logic → if
if val < lo { return lo }
if cmd == "build" { runBuild() }
```

### 2. Two Directive Forms

**Standalone** (entire line is a comment):
```go
// @inco: x != nil
// @inco: x > 0, -panic("must be positive")
```

**Inline** (appended to the end of a code line):
```go
_ = err // @inco: err == nil, -panic(err)
_ = skip // @inco: !skip, -return(filepath.SkipDir)
```

Use the inline form when a variable is only used within the directive — `_ = var` suppresses the compiler's unused variable error. This is the **acknowledgement pattern**: you explicitly tell the compiler "I know this variable exists; its guard is handled by inco." Always prefer this pattern over leaving variables unacknowledged.

### 3. Available Actions

| Action | Syntax | Meaning |
|--------|--------|---------|
| panic (default) | `// @inco: <expr>` | Auto-generated panic message |
| panic (custom) | `// @inco: <expr>, -panic("msg")` | Custom panic message |
| return | `// @inco: <expr>, -return(vals...)` | Return specified values |
| return (bare) | `// @inco: <expr>, -return` | Bare return |
| continue | `// @inco: <expr>, -continue` | Continue the loop |
| break | `// @inco: <expr>, -break` | Break the loop |
| log | `// @inco: <expr>, -log(args...)` | log.Println(args...) |

### 4. Directive Semantics

The semantics of `@inco:` is **require** — `// @inco: <expr>` is equivalent to `require <expr>`, meaning "expr must be true". When the condition is not met, the action is executed (default: panic). Generated code is `if !(<expr>) { action }`.

Note that expressions are **positive** — write the condition you expect to hold:
```go
// @inco: err == nil, -panic(err)    // expect no error
// @inco: n > 0, -continue           // expect n to be positive
// @inco: !skip, -return(filepath.SkipDir)  // expect not skipped
```

## File Conventions

- `foo.inco.go` — source files containing `@inco:` directives (recommended naming)
- `.inco_cache/` — generated shadow files, overlay.json and manifest.json (add to .gitignore)
- `foo_test.go` — test files (not processed by inco; skipped by both gen and audit)
- `.incoignore` — exclude files/directories, supports hierarchical nesting (.gitignore-like syntax)

### Auto-skipped Paths

The following paths are always skipped regardless of `.incoignore` configuration:
- Hidden directories (`.git`, `.idea`, etc.)
- `vendor/`
- `testdata/`
- Test files (`_test.go`)

## Coding Guidelines

### Writing New Code

1. Use `// @inco:` for defensive checks, not `if`
2. Prefer inline form for error handling: `_ = err // @inco: err == nil, -panic(err)`
3. Use standalone form for parameter validation at function entry: `// @inco: root != ""`
4. Use `-continue` or `-break` for filtering conditions in loops
5. Directives can reference any available package (e.g., `fmt.Errorf`, `filepath.SkipDir`); auto-import handles it automatically
6. **Always use the acknowledgement pattern** (`_ = var`) when a variable is only referenced inside an inline directive — this is not a workaround but the idiomatic way to bind variables to inco guards. Omitting it causes compiler errors for unused variables

### if → @inco: Conversion Templates

```go
// Before:
if err != nil { return nil, err }
// After:
_ = err // @inco: err == nil, -return(nil, err)

// Before:
if x == nil { panic("x is nil") }
// After:
// @inco: x != nil, -panic("x is nil")

// Before:
if !valid { continue }
// After:
_ = valid // @inco: valid, -continue

// Before:
if n == target { break }
// After:
_ = n // @inco: n != target, -break
```

### Do NOT Convert These `if` Statements

- Business logic branches: `if val < lo { return lo }`
- Conditions with else: `if x { A } else { B }`
- Functional checks: `if cmd == "build" { ... }`
- Conditional blocks with side effects (multi-line body)

## Common Commands

```bash
# Install
go install github.com/imnive-design/inco-go/cmd/inco@latest

# Daily development
inco build ./...     # gen + build
inco test ./...      # gen + test
inco audit .         # coverage report

# Release (go build works without inco)
inco release .            # .inco.go → .inco (backup) + .go (with guards)
inco release --dry-run .  # preview, no file writes
inco release clean .      # restore

# Clean up
inco clean .         # delete .inco_cache/
```

### Single-repo Distribution

For single-repo projects, develop on the `inco` branch (with `.inco.go` sources) and use CI to automatically release to `main` (plain `.go` with guards baked in). See `.github/workflows/release-single-repo.yml` for an example. Consumers can `go install` / `go get` from `main` without needing inco.

## Engine Details

### Incremental Builds

`inco gen` maintains `.inco_cache/manifest.json`, recording each source file's SHA-256 hash. Unchanged files are skipped; orphaned old shadow files are automatically cleaned up.

### Parallel Processing

File parsing and shadow generation run in parallel based on `GOMAXPROCS`, each goroutine with its own `token.FileSet`.

### Auto Import

`pkg.Func` references in directive arguments are automatically injected as imports. A package name → path mapping is built once via `go list` (cached); ambiguous packages with the same name (e.g., `template`) are removed, and internal/vendor packages are filtered out.

## Audit Metrics

`inco audit` reports two key metrics:

- **inco/(if+inco)**: The ratio of guard directives to all conditional checks. This reflects the degree of separation between guards and business logic — higher is not necessarily better. A ratio that is too high suggests business branches may have been incorrectly converted to `@inco:`. The ideal state is: all guards use `@inco:`, all business logic stays in `if`, and the ratio naturally settles at a reasonable value.
- **Function coverage**: The percentage of functions with at least one `@inco:` directive — higher is better.

The remaining `if` statements should all be genuine business logic.

## Best Practices & Common Pitfalls

### 1. The Acknowledgement Pattern
`_ = var` is not a workaround — it is the **acknowledgement pattern**: you explicitly tell the compiler "I know this variable exists; its guard is handled by inco." This is the canonical way to bind a variable to an inline directive:

```go
// ❌ Wrong: err unused — compiler doesn't see the guard
// @inco: err == nil, -panic(err)

// ✅ Correct: acknowledge err, attach the guard
_ = err // @inco: err == nil, -panic(err)
```

### 2. Long Expression Optimization
If the expression in `@inco:` is too long or complex, extract it into a boolean variable first — this improves readability and works well with `_ = var`:

```go
// ❌ Verbose and hard to read
// @inco: si.ParentStateInfo == nil || si.ParentStateInfo == parentStateInfo, -panic(...)

// ✅ Clear
isParentValid := si.ParentStateInfo == nil || si.ParentStateInfo == parentStateInfo
_ = isParentValid // @inco: isParentValid, -panic(...)
```

### 3. Repeated Var Assignment for Multiple Guards
When applying multiple inco directives to the same variable consecutively (e.g., log first, then panic), repeat `_ = var` before each directive line.

```go
// ✅ Best practice: explicitly suppress unused checks for each line
_ = err // @inco: err == nil, -log("error occurred:", err)
_ = err // @inco: err == nil, -panic(err)
```

### 4. Group Declarations and Directives
Keep variable declarations together, and group `@inco:` directives together. Mixing them makes code harder to scan. A clean visual separation between "setup" and "guards" improves readability:

```go
// ❌ Scattered: declarations and directives interleaved
a, err := doA()
_ = err // @inco: err == nil, -panic(err)
b, err := doB()
_ = err // @inco: err == nil, -panic(err)

// ✅ Grouped: declarations first, then guards
a, errA := doA()
b, errB := doB()
_ = errA // @inco: errA == nil, -panic(errA)
_ = errB // @inco: errB == nil, -panic(errB)
```

When calls are independent of each other, batch the declarations at the top and the directives below. This makes it immediately clear which variables are being guarded. If a later call depends on an earlier guard (e.g., `b` requires `a` to be valid), keep that dependency boundary — group what you can, but don't sacrifice correctness for aesthetics.

### 5. Guards vs Business Logic
Always remember that the `inco audit` ratio is not a "higher is better" metric. Do not force-convert `if` statements with business semantics into `@inco:` just to inflate the ratio. `@inco:` is only for hard constraints where "if not met, subsequent code cannot or should not execute".
