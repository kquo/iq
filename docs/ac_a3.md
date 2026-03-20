# AC — A3: Context Budget Management

**Date:** 2026-03-19
**Version:** v0.12.0 (MINOR — new config field `context_window`)
**Status:** Draft

---

## Motivation

IQ assembles a messages slice from up to four sources (system prompt, KB chunks, session history, user input) and sends it to the model sidecar without any size check. If the assembled context exceeds the model's context window, the sidecar silently truncates or errors. There is no warning, no trimming, and no per-model window configuration.

---

## Codebase Scan

| Location | Finding |
|---|---|
| `internal/config/config.go` | `ModelEntry` has no `context_window` field; `InferParams` has `MaxTokens` (output limit only) |
| `internal/lm/lm.go` | `ManifestEntry` and `HFModel` have no context length field |
| `cmd/iq/prompt.go:650–690` | Step 4 ASSEMBLE builds `messages` slice; no size check before or after |
| `internal/kb/kb.go` | `MaxRunes = 1600`, `DefaultK = 3` — up to ~4800 chars of KB context, never trimmed |
| `cmd/iq/prompt.go` | Session history grows unbounded in multi-turn sessions |

---

## Scope

### In scope

- Add optional `context_window` field to `ModelEntry` in `internal/config/config.go`
- After Step 4 ASSEMBLE, before Step 4b CACHE CHECK: estimate total context size in tokens (chars / 4)
- If estimated tokens exceed `context_window`, trim in priority order:
  1. KB chunks — drop from the end of the KB context string (fewest chunks first)
  2. Session history turns — drop oldest assistant+user pairs first (never drop the first system message)
  3. System prompt and current user input are never trimmed
- If anything was trimmed, print a single gray warning line before the response
- If `context_window` is 0 (unset), skip trimming entirely — feature is opt-in per model
- Update `arch.md`: Step 4 ASSEMBLE description, config API exports table, debug trace format, `config.go` source files row, version history
- Update `iq cfg show` to display `context_window` when set

### Out of scope

- Exact token counting (tiktoken, sentencepiece, etc.) — chars/4 heuristic only
- Global/default context window (per-model only; if unset, no trimming)
- Trimming the system prompt or current user input under any circumstance
- Changes to sidecar, inference parameters, or streaming path
- `lm` manifest changes

---

## Config Change

```yaml
# config.yaml (example)
models:
  - id: mlx-community/Qwen2.5-Coder-7B-Instruct-4bit
    context_window: 32768   # optional; 0 or absent = disabled
    max_tokens: 4096
  - id: mlx-community/Phi-3.5-mini-instruct-4bit
    context_window: 4096
```

`ModelEntry` struct change:

```go
type ModelEntry struct {
    ID            string `yaml:"id"`
    ContextWindow int    `yaml:"context_window,omitempty"`
    InferParams   `yaml:",inline"`
}
```

---

## Trimming Algorithm

After Step 4 ASSEMBLE, when `route.ContextWindow > 0`:

```
estimatedTokens = sum(len(msg.Content) for each msg) / 4
budget = route.ContextWindow - route.MaxTokens  // reserve output space
if estimatedTokens <= budget: no-op

trimmed = {kbChunks: 0, sessionTurns: 0}

Phase 1 — KB chunks (when kbContext != ""):
  Drop chunks from the end of kbContext one at a time, re-estimate.
  Stop when within budget or no chunks remain.

Phase 2 — Session history (when sess != nil):
  Drop oldest assistant+user pairs (messages[1:3], messages[3:5], etc.)
  Never drop messages[0] (system message).
  Stop when within budget or only system + current user remain.

If trimmed.kbChunks > 0 or trimmed.sessionTurns > 0:
  print gray warning: "[context trimmed: N KB chunk(s), M session turn(s)]"
```

---

## Warning Format

```
[context trimmed: 2 KB chunk(s), 3 session turn(s) to fit 32768-token window]
```

- Printed to stdout in gray (`color.Gra(...)`) immediately before the response
- Single line, non-blocking
- Omits parts that were not trimmed (e.g. "2 KB chunk(s)" only if session turns untouched)
- Not printed in trace mode (trace already shows message assembly detail)

---

## `iq cfg show` Output

When `context_window` is set on a model entry:

```
models:
  mlx-community/Qwen2.5-Coder-7B-Instruct-4bit
    context_window  32768
    max_tokens      4096
```

When unset, line is omitted.

---

## Debug Trace

Add a sub-field in the Step 4 ASSEMBLE block:

```
STEP 4  ASSEMBLE
  messages      3
  est_tokens    1842
  budget        28672        (context_window=32768, max_tokens=4096)
  trimmed       —            (or: "2 KB chunks, 1 session turn")
```

When `context_window` is 0 (unset), `budget` and `trimmed` lines are omitted.

---

## Acceptance Tests

1. **No context_window set** — assemble large session; no trimming occurs, no warning printed.
2. **KB trim** — set `context_window: 2000`; inject 3 KB chunks totaling >1600 estimated tokens; verify only 1–2 chunks remain in assembled messages, warning printed.
3. **Session trim** — set `context_window: 2000`; build a 10-turn session exceeding budget; verify oldest turns dropped, system message preserved, current user input preserved.
4. **System prompt never trimmed** — even when system prompt alone approaches budget, it is never removed.
5. **User input never trimmed** — current turn user input always present in final messages.
6. **Warning format** — when KB chunks and session turns both trimmed, single line mentions both counts.
7. **Warning suppressed in trace mode** — `--debug` output shows trimming in Step 4 fields, no extra warning line.
8. **Budget accounts for MaxTokens** — `budget = context_window - max_tokens`; if model has `max_tokens: 4096` and `context_window: 8192`, budget is 4096 tokens for input.
9. **`iq cfg show`** — model with `context_window` set shows it; model without does not.
10. **Unit tests** — pure-function tests for the trim logic (token estimator, KB chunk dropper, session turn dropper) in a new or existing `_test.go`.
