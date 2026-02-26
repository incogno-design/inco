# IDE Incremental Generation — Development Plan

## Status: ✅ Complete

All three PRs have been implemented on branch `feat/ide-incremental`.

---

## PR 1 — Core Single-File API ✅ `caf1108`

Introduce the minimal public API for single-file generation while keeping
`Run()` backward-compatible by rebuilding it on top of the new primitives.

### New files

- `internal/inco/atomic.inco.go` — `atomicWriteFile`: write-to-temp + rename
- `internal/inco/atomic_test.go` — unit tests for atomic write

### Modified files

- `internal/inco/types.inco.go`
  - Add `sync.Mutex` and cached manifest/overlay fields to `Engine`
  - Add `Overlay.Replace` nil-safe helpers

- `internal/inco/engine.inco.go`
  - `Init()` — load manifest + overlay, validate shadow integrity
  - `GenFile(path) ([]byte, bool, error)` — single-file shadow generation
  - `CommitFile(path, shadow) error` — write shadow + update overlay/manifest
  - `Flush() error` — force persist overlay + manifest
  - `Close() error` — alias for Flush (graceful shutdown)
  - Refactor `Run()` to use `GenFile` + `CommitFile` loop internally

- `internal/inco/import.inco.go`
  - `safeAddImports` — wrap `astutil.AddImport` with recover

- `internal/inco/engine_test.go`
  - Tests for `Init`, `GenFile`, `CommitFile`, `Flush`
  - Test partial AST degradation
  - Test concurrent `GenFile` safety

### Not changed

- `cmd/inco/main.inco.go` — CLI behavior unchanged
- `audit.inco.go`, `format.inco.go`, `release.inco.go` — deferred to PR 2/3

---

## PR 2 — Event-Driven & IDE Integration ✅ `7c8e1cb`

- `HandleEvent(FileEvent)` — fsnotify entry point
- `Debouncer` — coalesce rapid file events
- `Reconcile()` — detect renames / drifted state
- `InvalidateImports()` — reset on go.mod change
- `ScheduleFlush` / `CancelFlush` — batched overlay/manifest writes
- `DeleteFile(path)` — remove shadow for deleted source
- `inco watch [dir]` — CLI command with fsnotify + debouncer

## PR 3 — Diagnostics & Per-File Audit/Fmt ✅ `4a1e678`

- `AuditFile(root, path)` — single-file audit
- `FmtFile(path)` — single-file format
- `DiagnoseFile(root, path)` — LSP-compatible diagnostics
- `DiagnoseJSON(root, path)` — JSON output for piping
- `inco diagnose <file>` — CLI command

---

## IDE Integration Guide

### Quick Start

```bash
# Install
go install github.com/imnive-design/inco-go/cmd/inco@latest

# Start watching (runs initial gen, then watches for changes)
inco watch .
```

### Architecture

```
Editor (save file)
    │
    ▼
fsnotify event ──► Debouncer (100ms) ──► HandleEvent()
                                              │
                         ┌────────────────────┴────────────────────┐
                         ▼                                         ▼
                  GenFile(path)                             DeleteFile(path)
                         │
                         ▼
                  CommitFile(result)
                         │
                         ▼
              ScheduleFlush(200ms) ──► Flush()
                                         │
                              ┌──────────┴──────────┐
                              ▼                      ▼
                       overlay.json            manifest.json
```

### Programmatic Usage (Go)

```go
package main

import (
    "fmt"
    "path/filepath"

    inco "github.com/imnive-design/inco-go/internal/inco"
)

func main() {
    e := inco.NewEngine("/path/to/project")

    // Initialize (loads manifest, cleans stale shadows)
    e.Init()

    // --- Single file flow ---

    // Generate shadow for one file
    r, err := e.GenFile("path/to/file.inco.go")
    if err != nil { panic(err) }
    if r != nil {
        // Cache miss — commit the new shadow
        e.CommitFile(r)
    }

    // Persist to disk
    e.Flush()

    // --- Event-driven flow (IDE) ---

    // Process file events from your watcher
    e.HandleEvent(inco.FileEvent{
        Kind: inco.EventModify,
        Path: "/abs/path/to/file.inco.go",
    })
    e.ScheduleFlush(200 * time.Millisecond)

    // On go.mod change
    e.InvalidateImports()

    // Full resync if needed
    n, _ := e.Reconcile()
    fmt.Printf("regenerated %d files\n", n)

    // Cleanup
    e.Close()
}
```

### Diagnostics (LSP Integration)

```bash
# Get JSON diagnostics for a file
inco diagnose path/to/file.inco.go
```

Output format (LSP-compatible):
```json
[
  {
    "path": "/abs/path/to/file.inco.go",
    "range": {
      "start": {"line": 5, "character": 0},
      "end": {"line": 5, "character": 20}
    },
    "severity": 4,
    "source": "inco",
    "message": "function foo has no @inco: directives (file.inco.go:6)",
    "code": "unguarded"
  }
]
```

Severity levels: `1`=error, `2`=warning, `3`=info, `4`=hint

Diagnostic codes:
| Code | Severity | Meaning |
|------|----------|---------|
| `parse-error` | 1 | File cannot be parsed at all |
| `parse-warning` | 2 | Partial parse (some AST recovered) |
| `invalid-directive` | 2 | Syntax error in @inco:/@if: comment |
| `spacing` | 3 | Directive followed by blank line |
| `unguarded` | 4 | Function has no @inco: directives |

### Per-File Audit & Format

```go
// Single-file audit
fa, _ := inco.AuditFile(".", "path/to/file.inco.go")
fmt.Printf("%s: %d directives, %d functions\n", fa.RelPath, fa.RequireCount, len(fa.Funcs))

// Single-file format
changed, _ := inco.FmtFile("path/to/file.inco.go")
```

### VS Code Integration Example

In `.vscode/tasks.json`:
```json
{
  "version": "2.0.0",
  "tasks": [
    {
      "label": "inco watch",
      "type": "shell",
      "command": "inco watch .",
      "isBackground": true,
      "problemMatcher": {
        "owner": "inco",
        "pattern": {
          "regexp": "^inco: (.+)$",
          "message": 1
        },
        "background": {
          "activeOnStart": true,
          "beginsPattern": "^inco: watching",
          "endsPattern": "^$"
        }
      },
      "runOptions": { "runOn": "folderOpen" }
    }
  ]
}
```

This auto-starts `inco watch` when you open the project.

### Neovim Integration Example

```lua
-- In init.lua or after/plugin/inco.lua
local group = vim.api.nvim_create_augroup("Inco", { clear = true })

vim.api.nvim_create_autocmd("BufWritePost", {
  group = group,
  pattern = "*.inco.go",
  callback = function(ev)
    -- Run diagnose and populate quickfix
    local path = ev.file
    vim.fn.jobstart({ "inco", "diagnose", path }, {
      stdout_buffered = true,
      on_stdout = function(_, data)
        local json = table.concat(data, "\n")
        local ok, diags = pcall(vim.json.decode, json)
        if not ok or not diags then return end

        local items = {}
        for _, d in ipairs(diags) do
          table.insert(items, {
            filename = d.path,
            lnum = d.range.start.line + 1,
            col = d.range.start.character + 1,
            text = d.message,
            type = ({ "E", "W", "I", "H" })[d.severity] or "I",
          })
        end
        vim.fn.setqflist(items, "r")
      end,
    })
  end,
})
```
