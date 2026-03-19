# A1 Acceptance Criteria — Retire pipeline modes, canonicalize single inference path

## Motivation

IQ's `pipeline:` config field was introduced to support `two_tier` vs `single_pool` routing. With the design pivot (see `docs/design-pivot-01.md`), there is only one inference path: one inference sidecar, one pass. The field and all code that branches on it are now dead weight. A1 removes them.

The `restart` command (already built, clean build confirmed) is folded into this release.

## Scope

### In scope
- Remove `pipeline:` field from config schema (`internal/config`)
- Extend config migration to silently strip `pipeline:` from any existing `config.yaml` on load (no user action required; existing configs continue to work)
- Remove constants: `PipelineTwoTier`, `PipelineSinglePool`
- Remove functions: `EffectivePipeline()`, `ValidPipeline()`
- Remove all switch/branch logic in `cmd/iq/` that checks pipeline mode (`prompt.go`, `svc.go`, `cfg.go`)
- Remove `resolveSinglePool()` — it becomes the only path, so inline or rename it to the generic `resolveRoute` equivalent
- Update `iq st` (status): drop the `pipeline:` header line
- Update `iq cfg check`: remove pipeline validation check
- Update all tests that reference pipeline constants or pipeline config fields
- Update `arch.md`: remove pipeline mode references from config example and relevant sections

### Out of scope
- Changing inference logic or embed behavior
- Removing the `restart` command (already merged, keep it)
- Removing tier-related config fields (`tiers:`, `fast`, `slow`) — these still serve as the model pool list for `single_pool` behavior; a follow-on cleanup can rename them if desired
- Any changes to tool dispatch (A2) or context budgeting (A3)

## Acceptance tests

1. **Clean build**: `./build.sh` passes with no vet, staticcheck, or test failures.
2. **Existing config with `pipeline: single_pool`** loads without error; `pipeline:` field is silently dropped on next save.
3. **Existing config with `pipeline: two_tier`** loads without error; field is silently dropped.
4. **Config with no `pipeline:` field** loads without error.
5. **`iq st`** runs cleanly and does not show a pipeline row.
6. **`iq cfg check`** runs cleanly and does not reference pipeline mode.
7. **`iq ask`** handles a prompt end-to-end without referencing pipeline constants.
8. **`iq restart`** stops and restarts sidecars correctly.
9. No references to `PipelineTwoTier`, `PipelineSinglePool`, `EffectivePipeline`, or `ValidPipeline` remain in non-test, non-migration code.
