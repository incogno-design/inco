# IDE Incremental Generation — Development Plan

## Goal

Refactor `Engine` to support single-file incremental generation, enabling
IDE integration (LSP, file watchers) without full-project rescans.

---

## PR 1 — Core Single-File API (~200 lines)

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

## PR 2 — Event-Driven & IDE Integration (~150 lines)

- `HandleEvent(FileEvent)` — fsnotify entry point
- `Debouncer` — coalesce rapid file events
- `Reconcile()` — detect renames / drifted state
- `ImportResolver` — lazy-loaded, invalidate on go.mod change
- `scheduleFlush` — batched overlay/manifest writes

## PR 3 — Diagnostics & Per-File Audit/Fmt (~110 lines)

- `AuditFile(path)` — single-file audit
- `FmtFile(path)` — single-file format
- `DiagnoseFile(path)` — LSP-compatible diagnostics
- `cmd/inco/main.inco.go` — build/test incremental skip
