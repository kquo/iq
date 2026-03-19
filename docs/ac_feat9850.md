# FEAT9850 — Context-Based Concurrency: Acceptance Criteria

## What the codebase scan found

7 goroutine launches, 4 `sync.WaitGroup` usages, 8 HTTP clients, and 14+ functions that
would benefit from `context.Context` — but no context usage anywhere today.

Full threading through all 14 functions would be a large, risky refactor with marginal
benefit in many cases (HF API calls already have 10s timeouts; health checks have 2s;
session auto-naming is intentionally fire-and-forget). This AC scopes to the highest-value
changes: the hot-path prompt pipeline and the WaitGroup → errgroup replacements.

---

## In scope

### 1. Root context at `executePrompt`
- `executePrompt` receives `ctx context.Context` as its first parameter
- Callers: `runAsk`, `runREPL` create a root context from `context.Background()` with
  SIGINT cancellation (via `signal.NotifyContext`)
- All sub-calls within the pipeline receive this context

### 2. Sidecar transport — context-aware HTTP
- `RawCall(ctx, port, req)` — wrap POST with `http.NewRequestWithContext`
- `Call(ctx, ...)` and `CallWithGrammar(ctx, ...)` — thread ctx through to RawCall
- `Stream(ctx, ...)` — wrap POST with `http.NewRequestWithContext` (Stream currently has
  no timeout at all — this is the most urgent fix)
- Global `inferClient` keeps its 5m timeout; context cancellation can still abort early

### 3. Embed HTTP call — context-aware
- `TextsOnPort(ctx, ...)` — wrap POST with `http.NewRequestWithContext`
- `Texts(ctx, ...)` and `Classify(ctx, ...)` — thread ctx through to TextsOnPort

### 4. KB prefetch — replace ad-hoc goroutine with errgroup
- Current: `go func()` + channel + `time.After(kbTimeout)` in prompt.go
- New: `errgroup.WithContext(ctx)` scoped to the prefetch; `kb.Search` receives ctx
- `kb.Search(ctx, ...)` threads ctx into `embed.Texts` call

### 5. HF enrichment — replace WaitGroup with errgroup
- `HFEnrichModels(ctx, models)` — replace `sync.WaitGroup` with `errgroup.WithContext`
- `HFFetchTags(ctx, entries)` — same replacement
- Per-goroutine errors are collected; first non-nil error cancels remaining fetches
- `HFFetchModel` and `HFSearch` receive ctx and use `http.NewRequestWithContext`

---

## Out of scope

- Health check HTTP clients (2s timeouts, startup phase only — fine as-is)
- `search.go` Brave/DDG clients (already timeout-parameterized)
- Session auto-naming goroutine (intentionally fire-and-forget)
- `tools.Execute` timeout (already uses `time.After(ExecuteTimeout)` — acceptable)
- Sidecar crash detection goroutine (lifecycle goroutine, not request-scoped)
- `cmd/iq/svc.go` doc command WaitGroup (low-frequency command, not hot path)

---

## Acceptance tests

- `TestExecutePromptContextCancel` — pass a pre-cancelled context to `executePrompt`;
  verify it returns a context-related error without reaching inference
- `TestStreamContextCancel` — pass a cancelled context to `Stream`; verify it returns
  before completing
- `TestKBPrefetchCancelled` — cancel context mid-prefetch; verify KB step returns without
  blocking for `kbTimeout`
- All existing tests must continue to pass (callers updated to pass `context.Background()`)

---

## Notes

- `golang.org/x/sync/errgroup` is already available (check go.mod) or add it
- Signature changes follow Go convention: `ctx context.Context` is always the first parameter
- SIGINT cancellation means `iq ask` responds to Ctrl-C cleanly instead of leaving
  in-progress HTTP calls hanging
- This is a MINOR release (function signatures change throughout internal packages)
