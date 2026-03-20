# Project Split 01 — Three-Binary Monorepo

**Date:** 2026-03-18
**Status:** Phase 1 complete (iq v0.13.0 / lm v0.1.0) — Phase 2 pending

## Why

IQ grew to serve six distinct concerns simultaneously:

1. Coding assistant (file/git/shell tools, code-aware KB, sessions)
2. Private document RAG (KB ingest, vector search, offline inference)
3. Ad hoc local LLM inference (simple prompts, cue routing, cache)
4. Cognition / research (cue tuning, benchmarking, model comparison)
5. Model management (HuggingFace registry, sidecar lifecycle, perf sweep)
6. Agent framework (config-based agent definition, domain tuning)

These concerns have different users, different workflows, and different complexity budgets. Keeping them in one binary creates scope creep, unclear identity, and features that block each other's simplification.

The split separates them into three focused binaries while preserving all existing code in a single monorepo with shared internal packages.

## The Three Binaries

### `iq` — Coding assistant (anchor project)

The primary identity. A fast, local, offline CLI for development tasks including its own development.

**Keeps:**
- Prompt pipeline (classify → route → KB → assemble → infer)
- Cue system (simplified for dev/code domain)
- File, git, shell tools
- Session management and REPL
- KB scoped to code documentation and project artifacts
- Response cache
- Embed sidecar management
- Web search as optional tool
- `iq start / stop / restart / status`
- `iq pool` (renamed from `iq tier` in A1B)

**Gives away:**
- `lm search / get / list / show / rm` → `lm`
- `iq perf / bench` → `lm`
- `iq kb ingest / list / search / rm` (document-focused) → `kb`

---

### `lm` — Local model manager

The "Ollama-like" piece. Manages the local model registry and sidecar lifecycle independently of any specific use case.

**Owns:**
- HuggingFace API client (`internal/lm`)
- Model download, search, list, show, rm
- Sidecar start/stop/status for arbitrary models
- Performance benchmarking (`perf bench / sweep / show`)
- Model manifest (`models.json`)
- Cue classification and tool routing benchmarks (as evaluation harness)

**Does not own:**
- Inference pipelines or prompt assembly
- KB or document indexing
- Any application-level routing logic

---

### `kb` — Private knowledge base

A general-purpose local knowledge engine. Ingest documents, vectorize, retrieve, infer — entirely offline. Research and cognition tooling is built on top of this as a domain-tuned instance.

**Owns:**
- Document ingest, chunking, embedding (`internal/kb`)
- Vector search and retrieval
- Inference over retrieved chunks (full RAG loop)
- Own embed + inference sidecar management (`internal/sidecar`, `internal/embed`)
- Web search as optional retrieval source (`internal/search`)
- Domain-tuned cue system (simplified variant of `iq`'s)
- Per-domain config (corpus, model, inference params)

**Research / cognition:**
A domain-tuned `kb` instance — different document corpus (papers, notes, observations), different cues, different inference params. Same binary, different `~/.config/kb/<domain>/`. No separate binary needed.

---

## Shared Internal Packages

All three binaries live in the same repo and share `internal/`:

| Package | Used by |
|---|---|
| `internal/sidecar` | `iq`, `kb`, `lm` |
| `internal/embed` | `iq`, `kb` |
| `internal/kb` | `iq`, `kb` |
| `internal/lm` | `lm` (and `iq` until extraction) |
| `internal/config` | `iq`, `kb`, `lm` |
| `internal/search` | `iq`, `kb` |
| `internal/tools` | `iq` |
| `internal/cue` | `iq`, `kb` |
| `internal/cache` | `iq`, `kb` |
| `internal/color` | all three |

No circular dependencies. Each binary imports exactly what it needs.

## Repository Structure (target)

```
cmd/
  iq/       ← coding assistant (current cmd/iq/, trimmed)
  lm/       ← model manager (extracted from cmd/iq/lm.go + perf.go)
  kb/       ← knowledge base (new, built on internal/kb + internal/embed)
internal/
  cache/
  color/
  config/
  cue/
  embed/
  kb/
  lm/
  search/
  sidecar/
  tools/
docs/
build.sh    ← builds all three binaries
```

## Extraction Sequence

The split is incremental. No big-bang refactor.

**Phase 1 — Define `lm`**
Move `cmd/iq/lm.go` and `cmd/iq/perf.go` (plus their tests) into `cmd/lm/`. Update `build.sh` to build and install `lm` alongside `iq`. Remove `lm` and `perf` subcommands from `iq`. `internal/lm` is unchanged.

**Phase 2 — Define `kb`**
Create `cmd/kb/` as a new binary. Wire `internal/kb`, `internal/embed`, `internal/sidecar` into a focused CLI: ingest, search, ask. Add inference loop (same pattern as `iq`'s `executePrompt`, scoped to KB-grounded responses). Move KB-focused commands out of `iq`.

**Phase 3 — Trim `iq`**
Remove anything that belongs to `lm` or `kb`. `iq` becomes purely the coding assistant. KB in `iq` remains but scoped to code artifacts only.

## What Is Not Lost

- All `internal/` packages are preserved exactly as-is
- No code is deleted during extraction — it moves to a new `cmd/` directory
- `build.sh` builds all three; a developer gets all tools from one repo
- The "agent is a config" principle applies to all three: `kb` domain instances are configs, `iq` agents are configs, `lm` is configuration-driven too
- Version histories stay in their respective binaries' `arch.md` equivalents; shared package history stays in the root

## Decisions

**Config schema: shared base, extended per binary.**
`internal/config` holds genuinely shared fields — `embed_model`, inference params, `version:`, `kb_min_score`. `iq`-specific fields (`tool_paths`, `brave_api_key`) stay in `internal/config` for now. When `kb` is built, `cmd/kb/` defines its own `KBConfig` struct that embeds `config.Config` for the shared parts and adds kb-specific fields (corpus paths, chunk size, domain name) on top. No changes to `internal/config` needed at that point. Do not over-engineer this before `kb` exists.

**No `lm` HTTP API — each binary manages its own sidecars.**
A `lm` daemon would create a hard runtime dependency, breaking the offline-first, self-contained design of `iq` and `kb`. Port conflicts are already handled by `NextAvailablePort`. The cost of duplicated embed sidecars (~15MB each) when multiple binaries run simultaneously is acceptable. Revisit only if sidecar coordination becomes a genuine pain point in practice.

**Phase 1 starts after A1B.**
`lm.go` and `perf.go` use tier-based config APIs that A1B is replacing. Extracting them before A1B would mean doing the config migration twice. A2 and A3 are independent (touch only `cmd/iq/prompt.go`) and can proceed in any order after A1B. Planned sequence:

```
A1B  →  config schema stable (tiers → flat models list)
A2   →  model-driven tool dispatch      (parallel with A3)
A3   →  context budget management       (parallel with A2)
Phase 1  →  extract cmd/lm/ (clean move on stable config)
Phase 2  →  build cmd/kb/
```
