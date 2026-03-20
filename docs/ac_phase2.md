# AC — Phase 2: `cmd/kb/` binary

**Date:** 2026-03-19
**Status:** Draft

---

## Codebase Scan

### `cmd/iq/kb.go`
Five subcommands under `iq kb`: `ingest`, `list`, `search`, `rm`, `clear`. Each delegates directly to `internal/kb` functions. No logic lives here that doesn't also belong in a standalone `kb` binary. These can be copied verbatim with minor surface changes (program name in help text, error messages).

### `internal/kb/kb.go`
Key exported types: `Index`, `Chunk`, `Source`, `Result`. Key exported functions: `Path`, `Exists`, `Load`, `Save`, `Ingest`, `Search`, `Context`, `ChunkFile`, `RemoveSource`, `EmbedText`, `ExtractKeywords`, `KeywordBoost`. Constants: `DefaultK = 3`, `MinScore = 0.72`.

**Problem:** `kb.Path()` calls `config.Dir()`, which is hardcoded to `~/.config/iq`. For `kb` to use its own config dir (`~/.config/kb/`), the storage functions need a parameterized path. The existing path will remain unchanged for `iq`.

### `internal/config/config.go`
`Dir()` returns `~/.config/iq` with no parameters. `config.Load(nil)` loads from this dir. `cmd/kb/` needs its own config directory (`~/.config/kb/`) and config struct.

### `cmd/iq/prompt.go` — KB pipeline
KB is deeply entangled in `executePrompt`: async prefetch goroutine at Step 3, 5-second collect timeout, injection into user message at ASSEMBLE. The `cmd/kb/` binary cannot reuse this function. It needs its own focused inference pipeline scoped to KB-grounded responses only — same pattern, narrower scope: no cue classification, no tool detection, no session management.

### `internal/embed/embed.go`
Exported: `StartSidecar`, `Texts`, `TextsOnPort`, `CosineSimilarity`, `Classify`, `SidecarAlive`, `MlxVenvPython`. `kb ask` uses the same embed sidecar as `iq` but manages it independently.

### `internal/sidecar/`
`StartInfer`, `Stop`, `ReadState`, `AllLiveStates`, `PidAlive` are reused as-is. `cmd/kb/` starts its own embed and inference sidecars — same port allocation pattern as `iq`.

---

## Scope

### In scope

1. **`cmd/kb/main.go`** — binary entry point. `programName = "kb"`, `programVersion = "0.1.0"`. Same `silentErr`, `argsUsage`, root cobra setup as `cmd/lm/main.go`. Custom `printRootHelp` listing all commands.

2. **`cmd/kb/kb.go`** — the five management commands:
   - `kb ingest <path>` — ingest file or directory tree
   - `kb list` — list indexed sources (path, files, chunks, ingested date)
   - `kb search <query>` — raw similarity search with score display and threshold annotation
   - `kb rm <path>` — remove a source
   - `kb clear` — wipe the KB index
   All use `~/.config/kb/kb.json`, not `~/.config/iq/kb.json`.

3. **`kb ask <query>`** — new command. Minimal RAG inference pipeline:
   - Embed query → KB search → assemble context → single inference pass → stream response
   - No cue classification, no tool detection, no session management
   - Requires embed sidecar and at least one inference sidecar running
   - `--model <id>` flag to override inference model
   - `--no-kb` flag: skip KB retrieval, run pure inference (for comparison / fallback)
   - `--top-k <n>` flag: number of KB chunks (default: `kb.DefaultK`)
   - System prompt: "Answer using only the provided context. If the context does not contain the answer, say so."
   - Streams response tokens as they arrive (same `sidecar.Stream` pattern as `iq`)
   - **`kb <query>` (root with args) is synonymous with `kb ask <query>`**, the same way `iq <message>` maps to `iq ask`. The root cobra command's `RunE` joins args and delegates to the same ask pipeline.

4. **`cmd/kb/svc.go`** — sidecar lifecycle: `kb start [model]`, `kb stop [model]`, `kb status`. Same pattern as `cmd/iq/svc.go`. `start` with no args starts the embed sidecar. `start <model>` starts an inference sidecar. `stop` with no args stops all `kb`-managed sidecars.

5. **`cmd/kb/cfg.go`** — `kb config show` (or `kb cfg`): display `~/.config/kb/config.yaml`. Same display pattern as `iq cfg show`.

6. **`KBConfig` struct** — defined in `cmd/kb/`. Embeds `config.Config` for shared fields (`embed_model`, inference params). Adds:
   - `Domain string` — optional label for this KB instance
   - `KBPath string` — optional override for KB index path (default: `~/.config/kb/kb.json`)
   Config file at `~/.config/kb/config.yaml`.

7. **`internal/config`**: add `DirFor(name string) (string, error)` — parameterized `Dir()` returning `~/.config/<name>`. Existing `Dir()` unchanged (still called by `iq`, still returns `~/.config/iq`).

8. **`internal/kb`**: add `PathFor(dir string) string`, `LoadFrom(dir string) (*Index, error)`, `SaveTo(path string, idx *Index) error` alongside existing functions. Existing `Path()`, `Load()`, `Save()` unchanged — `iq` keeps calling them without modification.

9. **`build.sh`**: builds `kb` alongside `iq` and `lm`.

10. **`iq kb` stays in `iq` for now.** The commands are not removed from `iq` in this phase. Phase 3 removes them.

### Out of scope

- Removing `iq kb` from `cmd/iq/` (Phase 3)
- Per-domain `~/.config/kb/<domain>/` directory structure (future — Phase 2 uses a single `~/.config/kb/`)
- Session management in `kb ask` (Phase 3+)
- `kb embed` management command (Phase 3 — `kb start` with no args handles the embed sidecar)
- Cue classification in `kb ask` (out of scope — `kb` is focused, KB-grounded inference only)
- Context budget trimming in `kb ask` (Phase 3+ — `iq`'s `trimToContextBudget` is reusable but threading it in is not required for Phase 2)

---

## Design Decisions

**`~/.config/kb/` vs shared `~/.config/iq/`.**
`kb` uses its own config directory. The KB index at `~/.config/kb/kb.json` is separate from `iq`'s `~/.config/iq/kb.json`. This means `kb ingest` populates a different index than `iq`'s built-in KB. Users who want KB context in `iq` still use `iq kb ingest`. Users who want a standalone general-purpose KB use `kb`. Rationale: the binaries are independent tools — shared state couples their lifecycles and breaks the clean separation. The cost is that a user with an existing `iq` KB must re-ingest if they want it available in `kb`.

**`internal/kb` path parameterization — minimal surface.**
Add `LoadFrom(dir string)` and `SaveTo(path string, idx *Index)` rather than threading config through the whole package. `PathFor(dir string)` computes `filepath.Join(dir, "kb.json")`. `cmd/kb/` calls these. Existing callers in `cmd/iq/` are untouched.

**`kb <query>` and `kb ask <query>` are synonymous.**
The root `RunE` joins args and delegates to the same pipeline as `kb ask`. Same flags (`--model`, `--no-kb`, `--top-k`) bound on both root and `ask` command. Mirrors the `iq <message>` / `iq ask` pattern.

**`kb ask` pipeline — no cue, no tools.**
`kb ask` is a scoped RAG command. The full `iq` prompt pipeline has 6 steps and handles cue routing, tool detection, session context, cache, and context trimming. `kb ask` needs only: embed → search → assemble → infer → stream. Adding cue classification or tool detection would bring the `kb` binary back toward the `iq` problem space and undermine the separation.

**Sidecar management — independent from `iq`.**
`kb` starts its own sidecars. Port assignment via `sidecar.NextAvailablePort` avoids conflicts. An embed sidecar started by `kb` is a separate process from the one started by `iq`, even if they load the same model. The cost (~15MB per sidecar) is acceptable per the decision recorded in `docs/project-split-01.md`.

**Version: `kb v0.1.0`.**
New binary, first release. `iq` bumps to `v0.14.0` (new user-visible commands removed from `iq kb` → `kb`... wait: `iq kb` stays in this phase). Correction: `iq` does not change in Phase 2 — the `iq kb` commands stay. Therefore `iq` version does not bump in Phase 2. Only `kb` ships as a new binary at `v0.1.0`.

---

## Acceptance Tests

1. `kb ingest ~/projects/myapp` ingests into `~/.config/kb/kb.json`, not `~/.config/iq/kb.json`.
2. `kb list` shows indexed sources from `~/.config/kb/kb.json`.
3. `kb search "query"` runs similarity search against the `kb` index, shows score and threshold annotation.
4. `kb rm <path>` removes a source; `kb list` no longer shows it.
5. `kb clear` wipes `~/.config/kb/kb.json`; `kb list` reports empty.
6. `kb start` starts the embed sidecar. `kb status` shows it running.
7. `kb start <model>` starts an inference sidecar. `kb status` shows both.
8. `kb stop` stops all `kb`-managed sidecars. `kb status` shows none.
9. `kb ask "question"` with no sidecars running: error message directing user to run `kb start`.
10. `kb ask "question"` with sidecars running and KB populated: streams a KB-grounded response. Debug output shows: embed query → N results above threshold → assembled context → inference → response.
11. `kb ask "question" --no-kb` runs pure inference with no KB retrieval.
11b. `kb "question"` (root invocation) produces identical output to `kb ask "question"` — same pipeline, same flags accepted.
12. `iq kb ingest / list / search / rm / clear` still work against `~/.config/iq/kb.json` (unchanged).
13. `./build.sh` builds `kb` binary to `$GOPATH/bin/kb`.
14. `kb` with no args prints help listing all commands.
15. `kb cfg show` displays `~/.config/kb/config.yaml` (creating a default if absent).
