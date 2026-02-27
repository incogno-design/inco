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
// ✅ Guard → @inco: (contract) / @if: (logic flow)
// @inco: db != nil                          — contract: I expect db is not nil
// @inco: err == nil, -panic(err)            — contract: I expect no error
// @if: err != nil, -return(nil, err)        — logic flow: if error, bail out

// ✅ Logic → if
if val < lo { return lo }
if cmd == "build" { runBuild() }
```

### 2. Choosing Between `@inco:` and `@if:`

| Prefix | Condition | Generated code | Semantics |
|--------|-----------|----------------|-----------|
| `@inco:` | Inverted | `if !(expr) { action }` | **Contract** — state what you expect to be true |
| `@if:` | As-is | `if expr { action }` | **Logic flow** — direct `if` replacement for guard clauses |

**When to use `@inco:`** (preferred for contracts):
- Preconditions: `// @inco: root != ""`
- Nil checks: `// @inco: db != nil`
- Error expects: `_ = err // @inco: err == nil, -return(nil, err)`
- Range validation: `// @inco: idx >= 0 && idx < len(items), -continue`

**When to use `@if:`** (logic flow guard clauses):
- Direct `if` migration: `// @if: err != nil, -return(nil, err)`
- Filtering in loops: `// @if: skip, -continue`
- Early return: `// @if: done, -return`

```go
// Same effect, different intent:
// @inco: err == nil, -return(nil, err)   // contract: "I expect no error"
// @if: err != nil, -return(nil, err)     // flow: "if error, return"
```

### 3. Two Forms

**Standalone** (entire line is a comment) — **preferred**:
```go
// @inco: x != nil, -panic("x required")
// @inco: n > 0, -panic("must be positive")
```

**Inline** (appended to the end of a code line):
```go
_ = err // @inco: err == nil, -panic(err)
```

**Decision rule**: Is the variable used elsewhere in the function?
- **Yes** → standalone (no `_ = var` needed)
- **No** → inline (`_ = var // @inco: ...` suppresses the unused variable error)

### 4. Available Actions

| Action | Syntax | Meaning |
|--------|--------|---------|
| panic (default) | `// @inco: <expr>` | Auto-generated panic message |
| panic (custom) | `// @inco: <expr>, -panic("msg")` | Custom panic message |
| return | `// @inco: <expr>, -return(vals...)` | Return specified values |
| return (bare) | `// @inco: <expr>, -return` | Bare return |
| continue | `// @inco: <expr>, -continue` | Continue the loop |
| break | `// @inco: <expr>, -break` | Break the loop |
| log | `// @inco: <expr>, -log(args...)` | log.Println(args...) |

## File Conventions

- `foo.inco.go` — source files containing directives (recommended naming)
- `.inco_cache/` — generated shadow files, overlay.json and manifest.json (add to .gitignore)
- `foo_test.go` — test files (not processed by inco; skipped by both gen and audit)
- `.incoignore` — exclude files/directories (.gitignore-like syntax)

### Auto-skipped Paths

- Hidden directories (`.git`, `.idea`, etc.)
- `vendor/`, `testdata/`, test files (`_test.go`)

## Coding Guidelines

### 0. Always Run Tests Before Committing

**Every project, every commit.** Before staging or committing any change, run the project's full test suite and confirm all tests pass. No exceptions.

- **This project uses `inco test ./...`** (not `go test`). `inco test` generates the overlay first, then runs `go test -overlay`.
- If a test fails, fix it before committing — do not commit broken code.
- "It compiles" is not "it works". Compilation alone is never sufficient verification.
- After multi-step fixes, run the **full** test suite each time, not just the changed file.

### Writing New Code

1. **Prefer `@inco:` for contracts** — state what you expect: `// @inco: err == nil, -return(nil, err)`
2. **Use `@if:` only for logic flow** — direct `if` guard migration: `// @if: err != nil, -return(nil, err)`
3. Use standalone form when the variable is used later; inline (`_ = err // @inco: ...`) only when `err` is not referenced elsewhere
4. Use standalone for parameter validation at function entry: `// @inco: root != "", -panic("root required")`
5. Use `-continue` or `-break` for filtering conditions in loops
6. Directives can reference common stdlib packages (`fmt`, `errors`, `strings`, `strconv`, `os`, `io`, `filepath`, `time`, `context`, `sync`, `log`, `json`, `http`, etc.) and project dependencies; auto-import handles them. Obscure packages (`unsafe`, `reflect`, `runtime`, `syscall`, `go/ast`, etc.) are NOT auto-imported
7. **After editing code, always run `go vet ./...`** to check for unused variables
8. **Do not overthink** — make a decision, apply it, move on
9. **Review `if` as guard clause first** — `if` with single-action body (return, panic, continue, break) and NO `else`? Convert to directive. Business logic? Keep `if`
10. **Minimize `spaced/inco` ratio in audit** — a low ratio means guard semantics and logic flow are cleanly separated. When a directive is immediately followed by its guarded code (no blank line), it signals tight coupling. Use `inco fmt` to normalize, then review: can you restructure to reduce unnecessary spacing?

### if → Directive Conversion

**`@inco:` (preferred) — invert the condition.** State what you expect:

```go
// if err != nil { return nil, err }  →  _ = err // @inco: err == nil, -return(nil, err)
// if x == nil { panic("x is nil") }  →  // @inco: x != nil, -panic("x is nil")
// if !valid { continue }             →  _ = valid // @inco: valid, -continue
```

**`@if:` — copy condition directly.** For logic flow:

```go
// if err != nil { return nil, err }  →  _ = err // @if: err != nil, -return(nil, err)
// if skip { continue }              →  _ = skip // @if: skip, -continue
```

### Do NOT Convert These `if` Statements

- Business logic branches: `if val < lo { return lo }`
- Conditions with else: `if x { A } else { B }`
- Multi-line bodies / side effects
- Init statements with `:=` in the `if`: `if v, ok := m[k]; ok { ... }` (extract first, then convert)

## Install

```bash
go install github.com/incogno-design/inco/cmd/inco@latest
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

Directive conditions must stay short and readable. When a condition has multiple clauses (`&&`, `||`) or spans more than ~60 characters, **always** extract it into a named boolean variable first, then reference the variable in the directive:

```go
// ❌ WRONG — condition too long, hard to read
// @if: strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata", -return(filepath.SkipDir)

// ✅ CORRECT — extract, then directive
skipDir := strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata"
_ = skipDir // @if: skipDir, -return(filepath.SkipDir)

// ✅ Also good for @inco:
isInvalid := si.Parent != nil && si.Parent != expected
_ = isInvalid // @inco: !isInvalid, -panic(...)
```

The variable name documents intent and keeps the directive line scannable.

### 2. Multiple Guards on Same Variable

```go
_ = err // @inco: err == nil, -log("error:", err)
_ = err // @inco: err == nil, -panic(err)
```

### 3. Group Directives Together

Directives should be clustered, not scattered among logic. When all validations are independent, group declarations first, then directives:

```go
// ✅ Independent — group declarations, then directives
a, errA := doA()
b, errB := doB()

_ = errA // @inco: errA == nil, -panic(errA)
_ = errB // @inco: errB == nil, -panic(errB)

use(a, b)
```

Use unique names (`errA`, `errB`) to avoid shadowing.

When validations are **sequential** (each step depends on the previous), interleave declaration → directive pairs with a blank line between each pair:

```go
// ✅ Sequential — declare, validate, blank, declare, validate, blank, logic
absDir, err := filepath.Abs(dir)
_ = err // @inco: err == nil, -panic(err)

err = inco.NewEngine(absDir).Run()
_ = err // @inco: err == nil, -panic(err)

fmt.Println("done")
```

**Do not** scatter directives far from their declarations or mix them into unrelated logic.

### 3b. Unused Variables: Suppress Before the Directive

When a variable is only referenced inside a directive's action (not in later code), place the `_ = var` **above** the directive, not below:

```go
// ✅ CORRECT — suppressor before directive
s, ok := actionNames[k]
_ = s
_ = ok // @inco: !ok, -return(s)
return "unknown"

// ❌ WRONG — suppressor after directive breaks formatting
s, ok := actionNames[k]
_ = ok // @inco: !ok, -return(s)
_ = s
return "unknown"
```

This keeps the directive at the boundary between guard and code, and `inco fmt` spacing rules work naturally.

### 4. Never Write Both `if` and Directive

```go
// ❌ WRONG
if err != nil { panic(err) } // @inco: err == nil, -panic(err)

// ✅ CORRECT — let inco generate the if block
_ = err // @inco: err == nil, -panic(err)
```

### 5. `@inco:` is for Function Parameter Validation

The core purpose of `@inco:` is parameter validation and precondition checks at function entry. The function signature declares types; `@inco:` declares value constraints:

```go
func Process(db *sql.DB, id string) error {
    // @inco: db != nil, -panic("db required")
    // @inco: id != "", -return(fmt.Errorf("empty id"))
    ...
}
```

**Mid-flow** — both `@if:` and `@inco:` work for guard clauses in the middle of a function:

```go
result, err := doWork()
_ = err // @if: err != nil, -return(nil, err)   // flow: if error, return
_ = err // @inco: err == nil, -return(nil, err)  // contract: I expect no error
```
