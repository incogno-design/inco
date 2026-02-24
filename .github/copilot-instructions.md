# Inco — Copilot Handbook

## What is Inco

Inco is a compile-time assertion engine for Go. Two directive prefixes:

- `@inco:` — **contract**. Write the condition you expect to hold. Generated: `if !(expr) { action }`
- `@if:` — **guard**. Same condition as `if`. Generated: `if expr { action }`

Directives are plain comments; inco generates `if` guard blocks in shadow files, injected via `go build -overlay`. Zero source file invasion.

## Core Rules

### 1. Directives are for Guards, Not Logic

**Use `@inco:`/`@if:`**: nil checks, error checks, range validation, preconditions
**Use `if`**: business branches, conditional selection, flow control

```go
// ✅ Guard → @inco: / @if:
// @inco: db != nil
// @if: err != nil, -panic(err)
// @if: root == "", -panic("root required")

// ✅ Logic → if
if val < lo { return lo }
if cmd == "build" { runBuild() }
```

### 2. Two Directive Prefixes

| Prefix | Condition | Generated code | Use when |
|--------|-----------|----------------|----------|
| `@inco:` | Inverted | `if !(expr) { action }` | Stating a contract/expectation |
| `@if:` | As-is | `if expr { action }` | Converting an `if` guard directly |

```go
// Same effect, different style:
// @inco: err == nil, -return(nil, err)   // contract: "I expect no error"
// @if: err != nil, -return(nil, err)     // guard: "if error, return"
```

### 3. Two Forms

**Standalone** (entire line is a comment) — **preferred**:
```go
// @if: x == nil, -panic("x required")
// @inco: n > 0, -panic("must be positive")
```

**Inline** (appended to the end of a code line):
```go
_ = err // @if: err != nil, -panic(err)
```

**Decision rule**: Is the variable used elsewhere in the function?
- **Yes** → standalone (no `_ = var` needed)
- **No** → inline (`_ = var // @if: ...` suppresses the unused variable error)

### 4. Available Actions

| Action | Syntax | Meaning |
|--------|--------|---------|
| panic (default) | `// @if: <expr>` | Auto-generated panic message |
| panic (custom) | `// @if: <expr>, -panic("msg")` | Custom panic message |
| return | `// @if: <expr>, -return(vals...)` | Return specified values |
| return (bare) | `// @if: <expr>, -return` | Bare return |
| continue | `// @if: <expr>, -continue` | Continue the loop |
| break | `// @if: <expr>, -break` | Break the loop |
| log | `// @if: <expr>, -log(args...)` | log.Println(args...) |

## File Conventions

- `foo.inco.go` — source files containing directives (recommended naming)
- `.inco_cache/` — generated shadow files, overlay.json and manifest.json (add to .gitignore)
- `foo_test.go` — test files (not processed by inco; skipped by both gen and audit)
- `.incoignore` — exclude files/directories (.gitignore-like syntax)

### Auto-skipped Paths

- Hidden directories (`.git`, `.idea`, etc.)
- `vendor/`, `testdata/`, test files (`_test.go`)

## Coding Guidelines

### Writing New Code

1. Use standalone form when the variable is used later; inline (`_ = err // @if: ...`) only when `err` is not referenced elsewhere
2. Use standalone for parameter validation at function entry: `// @if: root == "", -panic("root required")`
3. Use `-continue` or `-break` for filtering conditions in loops
4. Directives can reference common stdlib packages (`fmt`, `errors`, `strings`, `strconv`, `os`, `io`, `filepath`, `time`, `context`, `sync`, `log`, `json`, `http`, etc.) and project dependencies; auto-import handles them. Obscure packages (`unsafe`, `reflect`, `runtime`, `syscall`, `go/ast`, etc.) are NOT auto-imported
5. **After editing code, always run `go vet ./...`** to check for unused variables
6. **Do not overthink** — make a decision, apply it, move on
7. **Review `if` as guard clause first** — `if` with single-action body (return, panic, continue, break) and NO `else`? Convert to `// @if:`. Business logic? Keep `if`

### if → Directive Conversion

**`@if:` — copy condition directly.** No thinking needed:

```go
// if err != nil { return nil, err }  →  _ = err // @if: err != nil, -return(nil, err)
// if x == nil { panic("x is nil") }  →  // @if: x == nil, -panic("x is nil")
// if !valid { continue }             →  _ = valid // @if: !valid, -continue
```

**`@inco:` — invert the condition.** State what you expect:

```go
// if err != nil { return nil, err }  →  _ = err // @inco: err == nil, -return(nil, err)
// if x == nil { panic("x is nil") }  →  // @inco: x != nil, -panic("x is nil")
```

### Do NOT Convert These `if` Statements

- Business logic branches: `if val < lo { return lo }`
- Conditions with else: `if x { A } else { B }`
- Multi-line bodies / side effects

## Install

```bash
go install github.com/imnive-design/inco-go/cmd/inco@latest
```

## Common Commands

```bash
inco build ./...     # gen + build
inco test ./...      # gen + test
inco audit .         # coverage report
inco release .       # bake guards into source
inco release clean . # restore
inco clean .         # delete .inco_cache/
```

## Best Practices

### 1. Long Expressions → Extract Variable

```go
isInvalid := si.Parent != nil && si.Parent != expected
_ = isInvalid // @if: isInvalid, -panic(...)
```

### 2. Multiple Guards on Same Variable

```go
_ = err // @if: err != nil, -log("error:", err)
_ = err // @if: err != nil, -panic(err)
```

### 3. Group Declarations, Then Directives

Use unique names (`errA`, `errB`) to avoid shadowing:

```go
a, errA := doA()
b, errB := doB()

_ = errA // @if: errA != nil, -panic(errA)
_ = errB // @if: errB != nil, -panic(errB)
```

**Exception**: If `b` depends on `a` being valid, don't group — correctness first.

### 4. Never Write Both `if` and Directive

```go
// ❌ WRONG
if err != nil { panic(err) } // @if: err != nil, -panic(err)

// ✅ CORRECT — let inco generate the if block
_ = err // @if: err != nil, -panic(err)
```
