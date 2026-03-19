# A1B Acceptance Criteria — Rename `tiers:` to flat `models:` list (schema v2)

## Motivation

`tiers: {fast: ..., slow: ...}` is vestigial naming from the two-tier routing era. The
design pivot retired tier-based routing; the pool is now flat. The tier labels no longer
reflect routing semantics — they are just model groupings with no dispatch meaning. A1B
replaces the tier map with a plain ordered list of model entries, each carrying an optional
per-model inference param override. The renamed `iq model` command group drops the tier
argument entirely.

## Codebase scan — what references tiers today

| File | What changes |
|---|---|
| `internal/config/config.go` | Remove `TierConfig`, `Tiers` map, `TierOrder`, `TierForModel`, `TierModels`, `AllAssignedModels`, `emptyTiers`, `normalizeConfig`; add `ModelEntry`, flat `Models []ModelEntry`; update `ResolveInferParams` signature; add `AllModels()`, `HasModel()`; bump `ConfigVersion` to 2; add `migrateV1` |
| `internal/config/config_test.go` | Update all tier-based tests to flat-model equivalents |
| `internal/sidecar/sidecar.go` | Drop `tier` param from `StartInfer`; set `State.Tier = "infer"` for inference sidecars; simplify `PickSidecar` (only used for embed; rename or inline) |
| `internal/sidecar/sidecar_test.go` | Update accordingly |
| `internal/lm/lm.go` | Rename `SuggestTier` → `SuggestSize`, return `"small"`/`"large"` (display hint only, not a routing concept) |
| `cmd/iq/svc.go` | Rename `iq tier` → `iq model`; `tier add <tier> <model>` → `model add <model>`; `tier rm <tier> <model>` → `model rm <model>`; `tier show` → `model list`; remove tier column from `iq st`; update start/stop/restart arg parsing; update first-run hints |
| `cmd/iq/prompt.go` | `ResolveInferParams(cfg, route.Tier)` → `ResolveInferParams(cfg, sc.Model)`; remove `Tier`/`TierSource` from `route` struct; session naming using `sc.Model`; `--tier` flag → `--model` flag |
| `cmd/iq/cfg.go` | Replace tiers display with flat models display; update check (no models assigned) |
| `cmd/iq/probe.go` | `ResolveInferParams(cfg, sc.Tier)` → `ResolveInferParams(cfg, sc.Model)` |
| `cmd/iq/perf.go` | Remove `--tier` flag from sweep; remove `benchTierFor`, `sweepCleanupModel` tier manipulation; use flat pool add/remove instead |
| `cmd/iq/lm.go` | Update any `SuggestTier` call → `SuggestSize` |
| `arch.md` | Update config example, schema docs, command reference |

## New schema (v2)

```yaml
version: 2
embed_model: mlx-community/bge-small-en-v1.5-bf16
models:
  - id: mlx-community/Llama-3.2-3B-Instruct-4bit
  - id: mlx-community/Qwen2.5-7B-Instruct-4bit
    temperature: 0.5
    max_tokens: 4096
temperature: 0.7
repetition_penalty: 1.3
max_tokens: 8192
```

Each entry in `models:` is a `ModelEntry`:
```go
type ModelEntry struct {
    ID string `yaml:"id"`
    InferParams `yaml:",inline"`  // optional per-model overrides
}
```

`ResolveInferParams(cfg *Config, modelID string) ResolvedParams` — looks up by model ID in
`cfg.Models`, applies per-model overrides over global defaults. Falls back to global+hardcoded
default when modelID is not in the list (e.g., probe with an arbitrary model).

## Scope

### In scope
- `internal/config`: `ModelEntry`, flat `models:` list, `ConfigVersion = 2`, `migrateV1`, updated `ResolveInferParams`, `AllModels()`, `HasModel()`, remove all tier types/funcs
- `internal/sidecar`: drop `tier` param from `StartInfer`; `State.Tier = "infer"` for inference sidecars; embed sidecar remains `State.Tier = "embed"`
- `internal/lm`: rename `SuggestTier` → `SuggestSize`
- `cmd/iq/svc.go`: rename command group `iq tier` → `iq pool`; simplify add/rm (no tier arg); remove TIER column from status display; update first-run hints
- `cmd/iq/prompt.go`: update `ResolveInferParams` call, `route` struct cleanup, `--tier` → `--model` flag
- `cmd/iq/cfg.go`: flat models display, updated empty-pool check
- `cmd/iq/probe.go`: updated `ResolveInferParams` call
- `cmd/iq/perf.go`: remove tier manipulation from sweep; use flat pool
- `cmd/iq/lm.go`: `SuggestTier` → `SuggestSize`
- All tests updated
- `arch.md` updated

### Out of scope
- Inference logic or embed behavior
- A2 (model-driven tool dispatch)
- A3 (context budget management)
- Phase 1 extraction (`cmd/lm/`)
- Per-model context window metadata (F2 territory)

## Migration: v1 → v2 (`migrateV1`)

`migrateV1` runs when `version == 1` is read from disk:
1. For each tier in `{"fast", "slow"}`:
   - For each model ID in that tier's `Models` list:
     - Create a `ModelEntry{ID: modelID}` and copy any non-nil inference param overrides from the `TierConfig` into the entry's `InferParams`
2. Append entries in tier order (fast first, slow second) to preserve the user's rough ordering
3. Print a migration notice to stderr: `"config.yaml migrated: tiers → flat models list (v2)"`
4. Stamp `version: 2` and save

`migrateV0` still handles pre-v1 formats (flat-list, old-4-tier, legacy field names). The
version dispatch in `Load` gains a `case 1:` branch for `migrateV1`.

## Command rename: `iq tier` → `iq pool`

| Old | New |
|---|---|
| `iq tier show` | `iq pool list` |
| `iq tier add <tier> <model>` | `iq pool add <model>` |
| `iq tier rm <tier> <model>` | `iq pool rm <model>` |
| `--tier <n>` flag on `iq ask`/`iq pry` | `--model <n>` flag |

`iq pool` with no subcommand is synonymous with `iq pool list` — bare invocation shows the
pool rather than printing help. Implemented via `RunE` on the parent command (same pattern
as the `list` subcommand).

`iq start [model]`, `iq stop [model]`, `iq restart [model]` — already accept model IDs;
the tier-name branch is removed.

## Sidecar changes

`sidecar.StartInfer(modelID, modelPath, pythonPath string)` — drop the `tier` param. The
`State.Tier` field is set to `"infer"` for all inference sidecars (previously the tier name).
`"embed"` remains the value for embed sidecars. This is the only remaining use of the field:
distinguishing embed from inference sidecars in `pickAnySidecar` and status display.

`sidecar.PickSidecar(tier string, ...)` is currently only called from `pickSidecar()` in
`svc.go`, which is only needed for probe (`iq pry --tier`). After A1B: `pickSidecar` is
replaced by a model-ID-aware lookup, or probe is updated to use `pickAnySidecar` with model
filtering. Decide during implementation — keep the function if it simplifies probe, inline it
if it doesn't.

## Acceptance tests

1. **Clean build**: `./build.sh` passes with no vet, staticcheck, or test failures.
2. **v1 config (tiers, no per-tier params)** loads, migrates to v2, models appear in flat list in tier order; migration notice printed to stderr.
3. **v1 config with per-tier inference params** (e.g., `slow` tier has `temperature: 0.5`) — each model that was in that tier gets `temperature: 0.5` as a per-model override in v2.
4. **v2 config** loads directly, no migration runs.
5. **No `version:` field** (v0) still migrates via existing `migrateV0` chain, then saves as v2.
6. **`iq pool`** (bare) and **`iq pool list`** produce identical output.
7. **`iq pool add mlx-community/some-model`** adds to flat pool; `iq pool list` shows it.
8. **`iq pool rm mlx-community/some-model`** removes from pool; absent from `iq pool list`.
9. **`iq st`** shows MODEL column only (no TIER column); embed sidecar displayed separately.
10. **`iq cfg show`** displays flat `models:` list with resolved effective params per model.
11. **`iq cfg check`** warns when no models assigned.
12. **`iq ask`** end-to-end with at least one model in pool — no tier references in output or debug trace.
13. **`iq pry <model>`** still resolves the correct sidecar.
14. No references to `TierConfig`, `TierOrder`, `TierForModel`, `TierModels`, `AllAssignedModels`, or `SuggestTier` remain in non-migration code.
