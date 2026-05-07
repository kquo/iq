# Build and Release

Reference for this repo's build pipeline, pre-release checklist, and acceptance test conventions. The enforceable one-liners live in `AGENTS.md`; this document explains the pipeline, the steps, and the rationale.

## Build

This repo has a single canonical build/test workflow: `./build.sh`.

`./build.sh` is a thin Bash dispatcher. The real implementation lives in `go run ./cmd/build` (build/test) and `go run ./cmd/rel` (release). Both `cmd/build` and `cmd/rel` are `go run` entrypoints — they are intentionally not installed as binaries.

**Flavor-divergent rel tool.** CODE overlay's `cmd/rel/main.go` is a thin wrapper around the `github.com/queone/governa-reltool` library — CODE consumers are Go projects with `go.mod`, so the library import has zero friction. DOC overlay ships a stdlib-only inline implementation instead, because content repos shouldn't be required to be Go modules just to run a release script. Drift-scan flags the divergent files; the divergence is intentional and load-bearing.

The build pipeline runs these steps in order, fail-hard on each:

1. `mdcheck` — **fail-hard.** Scans tracked markdown files for nested-fence bugs (3-backtick outer fence containing a tagged 3-backtick inner opener). The fix is to widen the outer fence to 4+ backticks or switch to `~~~`.
2. `go mod tidy` — ensure `go.mod` and `go.sum` are consistent
3. `go fmt ./...` — **fail-hard.** If `go fmt` rewrote any file (non-empty stdout), the build fails. Re-run after committing the formatting fix.
4. `go fix ./...` — advisory; output is logged but does not break the build
5. `go vet ./...` — **fail-hard**
6. Test suite with coverage — fail-hard on any test failure
7. `staticcheck ./...` — **fail-hard.** Installed via `go install staticcheck@latest` before each run.
8. Binary build — installs utilities to `$GOPATH/bin`

Invoking individual Go tools directly skips the tidy/fmt/lint pipeline above. A "passing" direct invocation can still produce a build that `./build.sh` would reject. The wrapper guarantees that what passes locally is what would pass in CI.

## Sandboxed Execution

Under sandboxed execution that blocks Go's build cache (look for `writing stat cache ... operation not permitted`), `staticcheck` may print a `matched no packages` warning even though it ran cleanly. Treat as advisory unless real findings appear; an unrestricted rerun confirms.

## Acceptance Tests

Every AT in an AC document must be labeled `[Automated]` or `[Manual]`.

- **Automated** — The result can be verified from CLI output, test assertions, or file inspection. Automated ATs are run during implementation and re-run as part of the pre-release checklist.
- **Manual** — Requires a live end-to-end action and must be confirmed by the user. The agent cannot self-verify these.

Default to Automated whenever the result is verifiable without a live external service. Manual ATs add friction to the release flow, so reserve them for behaviors that genuinely cannot be checked any other way.

Source axis (`[Automated]` / `[Manual]`) names who verifies. Timing axis (`[Pre-release gate]` / `[Post-release verification]`) names when verification happens. `[Pre-release gate]` is the default and may be omitted; `[Post-release verification]` is explicit. Use `[Post-release verification]` only when automated regression coverage already gates pre-release on the underlying class. The label communicates that the AT is a confidence check, not a gate, so future Operators do not promote it back into a gate.

## Pre-Release Checklist

Do not start this checklist unless the user explicitly asks to prep for release or equivalent.

The operator flow is two steps:

1. **Run `go run ./cmd/prep/ vX.Y.Z "message"`.** Stages version bumps, inserts the CHANGELOG row, deletes completed AC files, sweeps matching AC-pointer IE lines from `plan.md`, runs validation builds before and after, and prints the canonical release command. The agent determines the version (semver classification from the AC's scope) and drafts the release message (≤ 80 characters) before invoking prep.
2. **Run the printed release command (`./build.sh vX.Y.Z "message"`).** `cmd/rel` shows `git status --short`, lists every git step it will execute, and prompts for interactive confirmation. On approval it orchestrates `git add → commit → tag → push tag → push branch`. Optional: run `git diff` between the two steps if you want to inspect the CHANGELOG row wording and version-string values before committing — `cmd/rel`'s own status preview is sufficient to catch wrong-file inclusions or deletions.

Present only the release command after prep; do not add trailing commentary about wrapper routing or prompts. The director already knows.

### Appendix: what prep does

`go run ./cmd/prep/` runs nine phases internally so the operator flow above stays short:

1. **Validate inputs.** Semver pattern (`vX.Y.Z`), message non-empty and ≤ 80 characters.
2. **Validate git state.** Inside a git work tree, target tag does not exist yet, HEAD is not at the latest tag with a clean working tree.
3. **Pre-check build.** `./build.sh` run before any writes; skipped with `--no-build` or `--dry-run`.
4. **Detect version targets.** Scans `cmd/*/main.go` for `programVersion`. Both inline (`const programVersion = "..."`) and grouped (`const ( ... programVersion = "..." ... )`) forms are matched. Safe auto-detect filter: 1 `programVersion` target → bump (single-utility repo, repo-tracked). >1 targets → drop all and log a multi-utility warning (per-utility-independent default; each utility owns its own version per its own AC).
5. **Detect CHANGELOG targets + fail-fast idempotency guard.** Root `CHANGELOG.md`. If it already contains a row for the target version, prep exits with a fatal error before any writes.
6. **Parse AC refs.** `AC[0-9]+` scan on the release message; composites like `AC60+AC61` yield multiple refs.
7. **Apply writes.** Version bumps (per-file idempotent no-op when the file already has the target value); CHANGELOG row insertion under `| Unreleased | |`; AC file deletions (critique and disposition content live inside the AC file per `docs/critique-protocol.md`, so there are no separate companion files to delete); AC-pointer IE-line sweep from `plan.md` (lines matching `→ docs/ac<N>-` for each released AC). Skipped when `--dry-run`. Idempotent re-runs leave already-swept lines alone.
8. **Post-check build.** `./build.sh` run after writes; skipped with `--no-build` or `--dry-run`.
9. **Print release command.** Labeled block: `release command:` followed by the indented command `./build.sh vX.Y.Z "message"`.

CHANGELOG row shape (enforced by prep's insertion code and by convention):

- File shape: `# Changelog` heading, then a 2-column markdown table (`| Version | Summary |` with a `|---------|---------|` separator); first data row is `| Unreleased | |`, followed by one row per release (e.g., `| <version> | <AC-ref>: <one-line summary> |`).
- Summaries are single-line, ≤ 500 characters; lead with the AC reference if any.
- Versions are unprefixed (`0.29.0`, not `v0.29.0`).
- Do not backfill historical tags or invent alternative shapes (Keep-a-Changelog, sectioned `## vX.Y.Z`, etc.).
- When an AC locks a local form against canon (preserves a customization, declares intentional divergence, blocks a sync), include an explicit `preserve <path> <qualifier>` phrase in the summary. `governa drift-scan` only recognizes explicit markers; implicit AC references won't lock the file. Recognized phrase set: `preserve <path>`, `do not sync <path>`, `intentional divergence: <path>`, `<path>: keep local`.

Flags: `--dry-run` (or `-n`) prints intended writes without touching the working tree; `--no-build` skips phases 3 and 8. Both are for power users or tests — the common path is plain `go run ./cmd/prep/ vX.Y.Z "message"`.
