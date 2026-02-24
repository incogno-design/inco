# Inco — Copilot Handbook

## What is Inco

Inco is a compile-time assertion engine for Go. Write `// @inco:` directives in comments, and inco automatically generates corresponding `if` guard blocks, injected via `go build -overlay`. Zero source file invasion.

## Core Rules

### 1. `@inco:` is for Guards, Not Logic

**Use `@inco:`**: nil checks, error checks, range validation, preconditions  
**Use `if`**: business branches, conditional selection, flow control  

Note: `inco audit` ratio is not a "higher is better" metric — do not force-convert business logic `if` statements into `@inco:` just to inflate the ratio.

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

**Standalone** (entire line is a comment) — **preferred**:
```go
// @inco: x != nil
// @inco: x > 0, -panic("must be positive")
```

**Inline** (appended to the end of a code line):
```go
_ = err // @inco: err == nil, -panic(err)
_ = skip // @inco: !skip, -return(filepath.SkipDir)
```

**Decision rule: Is the variable used elsewhere in the function?**
- **Yes** → use **standalone** (no `_ = var` needed)
- **No** → use **inline** (`_ = var // @inco: ...` suppresses the unused variable error)

The inline `_ = var` is the **acknowledgement pattern**: you tell the compiler "this variable exists; its guard is handled by inco."

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

The semantics of `@inco:` is **require** — `// @inco: <expr>` means "I require expr to be true". Generated code is `if !(<expr>) { action }`.

**⚠️ CRITICAL — Condition Direction:**

The expression is what you **expect to hold** (the happy path), NOT the violation condition. This is the **opposite** of how `if` guards are traditionally written in Go:

```go
// Traditional Go guard (violation condition — "what went wrong"):
if err != nil { return nil, err }

// @inco: directive (expected condition — "what must be true"):
// @inco: err == nil, -return(nil, err)
```

**The condition is INVERTED when converting from `if` to `@inco:`.**

| `if` guard writes | `@inco:` writes | Why |
|---|---|---|
| `err != nil` | `err == nil` | expect no error |
| `x == nil` | `x != nil` | expect x exists |
| `n <= 0` | `n > 0` | expect positive |
| `!valid` | `valid` | expect valid |

```go
// @inco: err == nil, -panic(err)              // expect no error
// @inco: n > 0, -continue                     // expect n to be positive
// @inco: !skip, -return(filepath.SkipDir)      // expect not skipped
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

1. Use standalone form when the variable is used later; use inline acknowledgement (`_ = err // @inco: err == nil, -panic(err)`) only when `err` is not referenced elsewhere
2. Use standalone form for parameter validation at function entry: `// @inco: root != ""`
3. Use `-continue` or `-break` for filtering conditions in loops
4. Directives can reference common standard library packages (`fmt`, `errors`, `strings`, `strconv`, `os`, `io`, `filepath`, `time`, `context`, `sync`, `log`, `json`, `http`, etc.) and project dependencies; auto-import handles them automatically. Obscure stdlib packages (`unsafe`, `reflect`, `runtime`, `syscall`, `go/ast`, etc.) are NOT auto-imported
5. **After editing code, always run `go vet ./...`** to ensure there are no unused variables or other issues. Do not leave any unused variable warnings unresolved
6. **Do not overthink** — never repeatedly second-guess, self-doubt, or over-verify the same issue. Make a decision, apply it, and move on
7. **Review `if` as guard clause first** — when encountering an `if` with a single-action body (early return, panic, continue, break) and NO `else`, consider whether it is a precondition check. If yes, convert it to `// @inco:`. If it is a business logic branch, keep the `if`

### if → @inco: Conversion Templates

**Remember: INVERT the condition.** The `if` checks violations; `@inco:` states expectations.

```go
// Before:  if err != nil { return nil, err }
//              ^^^^^^^^ violation condition
// After:   _ = err // @inco: err == nil, -return(nil, err)
//                             ^^^^^^^^ expected condition (INVERTED)

// Before:  if x == nil { panic("x is nil") }
// After:   // @inco: x != nil, -panic("x is nil")
//                    ^^^^^^^^ INVERTED: == nil → != nil

// Before:  if !valid { continue }
// After (valid NOT used later): _ = valid // @inco: valid, -continue
// After (valid IS used later):  // @inco: valid, -continue
//                                         ^^^^^ INVERTED: !valid → valid

// Before:  if n == target { break }
// After (n NOT used later): _ = n // @inco: n != target, -break
// After (n IS used later):  // @inco: n != target, -break
//                                     ^^^^^^^^^^ INVERTED: == → !=
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

## Best Practices & Common Pitfalls

### 1. Long Expression Optimization
If the expression in `@inco:` is too long or complex, extract it into a boolean variable first — this improves readability and works well with `_ = var`:

```go
// ❌ Verbose and hard to read
// @inco: si.ParentStateInfo == nil || si.ParentStateInfo == parentStateInfo, -panic(...)

// ✅ Clear
isParentValid := si.ParentStateInfo == nil || si.ParentStateInfo == parentStateInfo
_ = isParentValid // @inco: isParentValid, -panic(...)
```

### 2. Repeated Var Assignment for Multiple Guards
When applying multiple inco directives to the same variable consecutively (e.g., log first, then panic), repeat `_ = var` before each directive line.

```go
// ✅ Best practice: explicitly suppress unused checks for each line
_ = err // @inco: err == nil, -log("error occurred:", err)
_ = err // @inco: err == nil, -panic(err)
```

### 3. Group Declarations and Directives
Keep variable declarations together, and group `@inco:` directives together. Mixing them makes code harder to scan. A clean visual separation between "setup" and "guards" improves readability.

**Crucial**: When grouping, use **unique variable names** (e.g., `errA`, `errB`) instead of reusing `err`. Reusing `err` across grouped declarations often leads to "declared and not used" errors or accidental shadowing.

```go
// ❌ Scattered & Shadowed: hard to read, error-prone
a, err := doA()
_ = err // @inco: err == nil, -panic(err)
b, err := doB() // err shadowed/reassigned
_ = err // @inco: err == nil, -panic(err)

// ✅ Grouped & Unique: clean, safe
a, errA := doA()
b, errB := doB()

_ = errA // @inco: errA == nil, -panic(errA)
_ = errB // @inco: errB == nil, -panic(errB)
```

**Dependency Exception**: If a later call depends on an earlier guard (e.g., `b` requires `a` to be valid), **do not group them**. Correctness first.

### 4. No Manual `if` Blocks
**Never** write the `if` block manually when using `@inco`. The directive's sole purpose is to *generate* that block for you. Manual repetition defeats the purpose and leads to code duplication.

```go
// ❌ WRONG: Manual if + inco directive
if err != nil { // @inco: err == nil, -panic(err)
    panic(err)
}

// ✅ CORRECT: Let inco generate it
_ = err // @inco: err == nil, -panic(err)
```


