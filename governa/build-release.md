# Build and Release

## Build and Test Rules

- use one canonical local build command and keep this document current
- run formatting, static checks, tests, and packaging through that command or documented sequence
- do not trigger release work during routine implementation

This repo uses a self-contained `build.sh` for all build, release-prep, and release work. No external governa tools are required; everything runs directly from `build.sh`.

## Minimum Validation

- formatting passes
- static checks pass
- automated tests pass
- changed docs match actual behavior

## Canonical Build Commands

```bash
./build.sh
```

To scope the run to selected commands:

```bash
./build.sh driftscan
```

`staticcheck` is pinned to `v0.7.0` and installed to `$(go env GOPATH)/bin/staticcheck` on first run. The installed path is used directly (not any `staticcheck` on `PATH`), so the version is deterministic across environments.

## Sandboxed Execution

Under sandboxed execution that blocks Go's build cache (look for `writing stat cache ... operation not permitted`), `staticcheck` may print a `matched no packages` warning even though it ran cleanly. Treat as advisory unless real findings appear; an unrestricted rerun confirms.

## Pre-Release Checklist

Do not start this checklist unless the user explicitly asks to prep for release or equivalent.

The operator flow is two steps:

1. **Run `./build.sh prep vX.Y.Z "message"`.** Stages version bumps, inserts the CHANGELOG row, deletes completed AC files, sweeps matching AC-pointer IE lines from `plan.md`, runs validation builds before and after, and prints the canonical release command. The agent determines the version (semver classification from the AC's scope) and drafts the release message (≤ 80 characters) before invoking prep. Flags: `--dry-run`/`-n` prints intended writes without touching the working tree; `--no-build`/`-B` skips the pre- and post-check builds.
2. **Run the printed release command (`./build.sh vX.Y.Z "message"`).** Shows `git status --short`, lists every git step it will execute, and prompts for interactive confirmation. On approval it orchestrates `git add → commit → tag → push tag → push branch`.

Present only the release command after prep; do not add trailing commentary about wrapper routing or prompts. The director already knows.

### Appendix: what prep does

`./build.sh prep` runs nine phases internally so the operator flow above stays short. Each phase has a clear failure mode:

1. **Validate inputs.** Semver pattern (`vX.Y.Z`), message non-empty and ≤ 80 characters.
2. **Validate git state.** Inside a git work tree, target tag does not exist yet, HEAD is not at the latest tag with a clean working tree.
3. **Pre-check build.** `./build.sh` run before any writes; skipped with `--no-build`/`-B` or `--dry-run`/`-n`.
4. **Detect version targets.** Scans `cmd/*/main.go` for `programVersion` and `internal/templates/version.go` (each presence-gated). The `programVersion` regex matches both inline (`const programVersion = "..."`) and grouped (`const ( ... programVersion = "..." ... )`) forms. Safe auto-detect filter: 1 `programVersion` target → bump (single-utility repo, repo-tracked). >1 targets → drop all and log a multi-utility warning (per-utility-independent default; each utility owns its own version per its own AC). The skip prevents clobbering independent per-utility SemVers in multi-utility repos.
5. **Detect CHANGELOG targets + fail-fast idempotency guard.** Root `CHANGELOG.md` and `internal/templates/CHANGELOG.md` (template-repo case). If any target already contains a row for the target version, prep exits with a fatal error before any writes.
6. **Parse AC refs.** `AC[0-9]+` scan on the release message; composites like `AC60+AC61` yield multiple refs.
7. **Apply writes.** Version bumps (per-file idempotent no-op when the file already has the target value); CHANGELOG row insertion under `| Unreleased | |`; AC file deletions (AC files are deleted whole; there are no separate companion files); AC-pointer IE-line sweep from `plan.md` (lines matching `→ governa/ac<N>-` for each released AC). Skipped when `--dry-run`/`-n`. Idempotent re-runs leave already-swept lines alone.
8. **Post-check build.** `./build.sh` run after writes; skipped with `--no-build`/`-B` or `--dry-run`/`-n`.
9. **Print release command.** Labeled block: `release command:` followed by the indented command `./build.sh vX.Y.Z "message"`.

CHANGELOG row shape (enforced by prep's insertion code and by convention):

- File shape: `# Changelog` heading, then a 2-column markdown table (`| Version | Summary |` with a `|---------|---------|` separator); first data row is `| Unreleased | |`, followed by one row per release (e.g., `| <version> | <AC-ref>: <one-line summary> |`).
- During a drift-scan adoption cycle, the `| Unreleased | |` row's Summary column may carry preserve marker phrases (per `governa/drift-scan.md` `## Preserve-marker phrase set`). Release prep inserts the new release row beneath the Unreleased row without modifying it, so any markers there persist. When the marker phrase plus the AC reference and summary fits the 80-character release-message limit, echo the marker into the release message so it lands in the new release row for cleaner separation; when it does not fit, leave the marker in the Unreleased row, where it remains recognized by future drift-scan runs from any CHANGELOG row.
- Summaries are single-line, ≤ 500 characters; lead with the AC reference if any.
- Versions are unprefixed (`0.29.0`, not `v0.29.0`).
- Do not backfill historical tags or invent alternative shapes (Keep-a-Changelog, sectioned `## vX.Y.Z`, etc.).
