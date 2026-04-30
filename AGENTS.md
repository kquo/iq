# AGENTS.md

## Governed Sections

`CLAUDE.md` is a symlink to this file; do not edit them independently.

Detail and rationale live in `docs/development-guidelines.md`, `docs/build-release.md`, `docs/development-cycle.md`.

Sections (fixed set):

- `Governed Sections`
- `Interaction Mode`
- `Approval Boundaries`
- `File-Change Discipline`
- `Review Style`
- `Base Rules`
- `Project Rules`

Rules:

- Preserve each section's semantic intent across edits.
- Add new rules under the best-fit section. Do not invent top-level sections.
- Do not reorder sections or rewrite the file unless the user requests a contract change.
- When updating, name the exact sections to change and keep edits local.
- Treat this file as a governed config artifact, not freeform prose.
- Use flat `##` with inline bullets. Avoid `###` sub-subsections — split or extract instead. Exception: documented technical need (e.g., grouped domain rules in Project Rules).

## Interaction Mode

- Default to exploratory discussion.
- Do not create files or make changes without explicit authorization.
- When authorized, make the smallest change that satisfies the request.
- Surface assumptions, ambiguities, and missing context before any direction-changing action.
- The agent is automatically the Operator per `docs/roles.md`. No role announcement or switching.

## Approval Boundaries

- Authorization is per-scope. Prior approval does not extend by analogy.
- Require explicit approval for: create, delete, rename, publish, release, or any destructive change.
- Require explicit approval for: governance files, CI/release config, secrets handling, external integrations.
- In-scope edits to existing files are allowed once the user has authorized implementation.
- Stop and ask when a request is ambiguous and the change is hard to reverse.
- Do not prepare, execute, publish, deploy, or distribute without explicit user request.
- **Never run `git commit`. Draft the message; present the command for the user to run.** No EXCEPTION.
- Do not start release-prep bookkeeping early. Begin only when the user asks to prep for release.
- Never run the release command. Present it for the user to run. Follow the Pre-Release Checklist in `docs/build-release.md`.
- **AC-first workflow** (non-trivial changes):
  - Draft `docs/ac<N>-<slug>.md` before implementation, using `docs/ac-template.md` if available. Define scope, out-of-scope, objective fit, required tests.
  - Objective Fit must answer: (1) what outcome this serves, (2) why this beats competing work, (3) what decisions/constraints it depends on, (4) direct roadmap work or intentional pivot.
  - Do not implement until the AC is critiqued and the user confirms it is implementation-ready.
- **AC critique gate:** After drafting, the director reviews and provides critique findings. The Operator transcribes findings into the AC's `## Critique` section and addresses them. Proceed only when the director explicitly confirms the AC is implementation-ready. See `docs/critique-protocol.md`.

## File-Change Discipline

- Prefer targeted edits over broad rewrites.
- Preserve user changes and unrelated local modifications.
- Update only files required for the task, plus directly affected docs. Keep docs current in the same pass — no silent drift, no deferral.
- When a change adds a file, command, flag, or major decision, update affected docs in the same pass.
- When a decision changes mid-implementation, complete the migration in one pass: files, docs, tests together. No half-migrated states.
- Update user-facing docs when commands, setup, workflows, outputs, published structure, or operating instructions change.
- Update architecture, planning, or style docs only when materially affected.
- Every AC doc ends with `## Status`. Valid states: `PENDING`, `IN PROGRESS`, `DEFERRED` (with reason). Use per-phase status for partial completion. Do not set DONE — completed ACs are deleted per the development cycle.
- Record follow-on improvements in `plan.md` or the repo's planning artifact. If neither exists, note them to the user. Do not expand scope ad hoc.
- Do not commit personal absolute filesystem paths. Use repo-relative paths or placeholders like `<project-root>`.
- **Code changes are not complete without accompanying tests. No exceptions for "small" changes, CLI output, or formatting.**
- **Codify corrections about repo behavior in the appropriate governance doc — not in agent-local memory. Refine existing rules in place; add new ones to the best-fit section. Agent memory is not a shadow governance system.**

## Review Style

- Lead with findings, not summaries. Cite file paths and concrete behavior.
- Prioritize bugs, regressions, missing tests, and drift from documented behavior.
- If no issues found, say so directly. Note residual risk or verification gaps.
- Keep completions terse: what changed, flat bullets, one-sentence next step. No "What's in it" / "Main conclusion" / "Next steps" headers unless asked.
- Prefer plain text and simple bullets. Use tables or richer structure only when content clearly benefits.
- Do not note skipped checks unless the omission is unusual or affects confidence.
- Architectural decisions to the director: present two bounded options plus a recommendation. One viable option → state as recommendation. More than two → name the best two, note the rest in one sentence.
- Exception: substantial completion reports must include the three-part self-review structure (Verified / Red-teamed / Not checked) defined in `docs/roles.md`, even when the default is terse.

## Base Rules

- Use the repo's canonical build command (`./build.sh` or equivalent). Never run individual tool commands directly. See `docs/build-release.md`.
- For single-utility smoke tests, use `go run ./cmd/<tool>/` or `go build -o /tmp/<name> ./cmd/<tool>/`. Do not `go build ./cmd/<tool>/` from repo root — drops a stray binary.
- Follow semver. PATCH: invisible to users (fixes, refactors, tooling). MINOR: user-visible (commands, flags, schema, behavior). Batch PATCH-level changes.
- Pin dependencies to explicit versions. Document any reason to stay on an older version.
- Every feature or logic change includes tests in the same pass.
- Wrap user-facing errors with operation context and recovery guidance.
- Every AC labels each acceptance test `[Automated]` or `[Manual]`. See `docs/ac-template.md`.
- New CLI flags pair a one-letter short form (standard, leads help output) with a long-form alias. Migrate existing flags when their code is next touched.
- Follow existing repo patterns unless an approved improvement says otherwise.
- Comment public functions.
- Prefer dedicated tools: `fd` (files), `rg` (text), `jq` (JSON), `pup` (HTML), `sd` (in-place replace), `sqlite-utils` (SQLite), `ast-grep` (structural). Batch independent shell calls. Do not re-read files already in context.

## Project Rules

- **Multi-utility repo.** This repo builds three installable binaries from `cmd/iq/`, `cmd/lm/`, `cmd/kb/`, plus the governa Go entrypoints `cmd/build/`, `cmd/prep/`, `cmd/rel/` (script-only, run via `go run`). `./build.sh <name>` validates and installs only that utility; `./build.sh` builds all installable binaries.
- **Per-utility programVersion.** Each `cmd/<name>/main.go` declares its own `programVersion` const. `cmd/prep` auto-bumps only on single-utility repos and prints a multi-utility warning here; releases are per-binary and manual: bump the affected `programVersion`, run `./build.sh`, then run the release command shown by `cmd/build`.
- **Semver decision test.** Would a user notice the change in `iq --help`, `iq cfg show`, or `iq start`? Yes → MINOR (reset PATCH to 0). No → PATCH. Batch several PATCH fixes into one release. MAJOR not until a stable public API commitment.
- **External runtime deps.** iq depends on Apple Silicon, Python `mlx-lm` (via pipx), the `hf` CLI, and `mlx-embedding-models` injected into the `mlx-lm` venv. Verify with `iq doc`. iq is offline-first — do not introduce code paths that assume cloud LLM availability.
- Follow existing repo patterns unless an approved improvement says otherwise.
