# AC — Phase 1: Extract `cmd/lm/`

**Date:** 2026-03-19
**iq version:** v0.13.0 (MINOR — `iq lm` and `iq perf` removed from iq CLI)
**lm version:** v0.1.0 (new binary)
**Status:** Draft

---

## Motivation

`cmd/iq/lm.go` and `cmd/iq/perf.go` implement model management and benchmarking — concerns that belong to a standalone tool, not to the coding assistant. Phase 1 extracts them into a new `cmd/lm/` binary while leaving all `internal/` packages untouched.

---

## Codebase Scan

| Item | Finding |
|---|---|
| `cmd/iq/lm.go` | Defines `newLmCmd()` with 5 subcommands: `search`, `get`, `list`, `show`, `rm`. Helper: `printLmHelp()`, `shellescape()`. Imports: `color`, `config`, `cue`, `embed`, `lm`, `sidecar`. |
| `cmd/iq/perf.go` | Defines `newPerfCmd()` with 4 subcommands: `bench`, `sweep`, `show`, `clear`. ~30 helper functions. Imports: `color`, `config`, `cue`, `embed` (as `iembed`), `kb`, `lm`, `sidecar`, `tools`. Embeds `bench_corpus.yaml`. |
| `cmd/iq/lm.go → perf.go` | `lm show` calls `loadBenchStore()`, `resultsFor()`, `formatBenchRow()` defined in perf.go. Both files moving together — no refactor needed. |
| `cmd/iq/main.go` | `newLmCmd()` and `newPerfCmd()` are added at lines 178 and 182. Both must be removed. |
| `build.sh` | Loops over `./cmd/*` — adding `cmd/lm/` is automatically picked up. No build.sh changes needed. |
| Test files | No `lm_test.go` or `perf_test.go` exist. Nothing to move. |
| `bench_corpus.yaml` | Embedded in perf.go via `//go:embed`. Must move alongside perf.go to `cmd/lm/`. |

---

## Scope

### In scope

- Create `cmd/lm/` with a `main.go` root command
- Move `cmd/iq/lm.go` → `cmd/lm/lm.go` (package `main`, adjust if needed)
- Move `cmd/iq/perf.go` → `cmd/lm/perf.go` (package `main`, adjust if needed)
- Move `cmd/iq/bench_corpus.yaml` → `cmd/lm/bench_corpus.yaml`
- Remove `newLmCmd()` and `newPerfCmd()` from `cmd/iq/main.go`
- Remove the corresponding `AddCommand` lines from `cmd/iq/main.go`
- Remove `iq lm` and `iq perf` from `cmd/iq/main.go` root help text
- Update `cmd/iq/main.go` root help to remove references to `lm` and `perf`
- `cmd/lm/` binary version starts at `v0.1.0`

### Out of scope

- Refactoring any logic in lm.go or perf.go
- Moving sidecar lifecycle commands (`start`/`stop`/`restart`/`status`) — these stay in `iq`
- Changes to any `internal/` package
- Creating `cmd/kb/` (Phase 2)
- Adding new features to `lm`

---

## `cmd/lm/` Command Structure

```
lm [search|get|list|show|rm]   ← model management (was: iq lm)
lm perf [bench|sweep|show|clear] ← benchmarking (was: iq perf)
```

`newLmCmd()` becomes the root of the `lm` binary. `newPerfCmd()` becomes a subcommand of the `lm` root (same as it was under `iq`).

The `lm` binary root (`lm --help`) lists both sets of commands.

---

## `cmd/lm/main.go`

Minimal entry point — mirrors `cmd/iq/main.go` structure:

```go
package main

const (
    programName    = "lm"
    programVersion = "0.1.0"
)

func main() {
    root := &cobra.Command{
        Use:   "lm",
        Short: "Local model manager",
        ...
    }
    root.AddCommand(
        newLmSearchCmd(),
        newLmGetCmd(),
        newLmListCmd(),
        newLmShowCmd(),
        newLmRmCmd(),
        newPerfCmd(),
    )
    root.Execute()
}
```

`newLmCmd()` in the moved lm.go is dissolved — its subcommands are promoted to root level. The `printLmHelp()` and root help function are rewritten for the new binary's root.

---

## `iq` CLI Changes

Remove from `cmd/iq/main.go`:
- `newLmCmd()` from `root.AddCommand(...)`
- `newPerfCmd()` from `root.AddCommand(...)`
- Any mention of `lm` or `perf` in the `iq` root help output

The `iq` root help should note that model management moved to the `lm` binary.

---

## Acceptance Tests

1. **`lm --help`** — prints top-level help listing `search`, `get`, `list`, `show`, `rm`, `perf`
2. **`lm list`** — lists models from `~/.config/iq/config.yaml` (same config, shared `internal/config`)
3. **`lm perf --help`** — prints perf subcommand help
4. **`lm perf show`** — reads `~/.config/iq/benchmarks.json` (same path as before)
5. **`iq lm`** — command no longer exists; `iq` returns an error or "unknown command"
6. **`iq perf`** — command no longer exists
7. **`./build.sh`** — builds both `iq` and `lm` binaries; both installed to `$GOPATH/bin`; all tests pass
8. **`iq --help`** — `lm` and `perf` not listed; root help mentions `lm` binary for model management
9. **No `internal/` changes** — `git diff internal/` shows nothing
10. **`lm show <model>`** — shows benchmark results (cross-file call from lm.go to perf.go helpers works correctly in new package)
