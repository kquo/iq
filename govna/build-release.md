# Build and Release

## Build and Test Rules

- Keep one documented canonical build command.
- Route formatting, checks, tests, and packaging through it.
- Keep release work out of routine implementation.

Use self-contained `build.sh` for build, release prep, and release work without external govna tools.

### Build Presentation

- Reuse the canonical build color policy and palette across supported CODE stacks.
- Color phase headings, command previews, status values, failures, prep output, and release output by semantic role.
- Emit plain output when stdout is not a terminal.
- Emit plain output when `NO_COLOR` is set.
- Emit plain output when `TERM=dumb`.
- Require a 256-color-capable terminal before emitting ANSI sequences.
- Preserve plain-text content and output streams when color is disabled.
- Keep self-contained build scripts compatible with Bash 3.2.

## Minimum Validation

- Require formatting, static checks, automated tests, and behavior-aligned docs to pass.



## Canonical Build Commands

```bash
./build.sh
```

To scope the run to selected commands:

```bash
./build.sh <target> [<target> ...]
```

Use space-separated target names. Supported CODE stacks may retain package-wide shared-code validation while limiting target-specific checks, tests, artifacts, and installation to the selected targets.

Run `./build.sh` without targets for repository-wide validation. Follow the applicable stack guidance above for release-prep evidence, pre-change validation, and build-state reuse. Release-prep validation uses the package-wide form.

## Independent Utility Versions

- Treat the repository/package version as the version input and release metadata governed by the existing release mechanism.
- Require one normalized record for each installable utility with its canonical target name, declaration location, declared version, and `--version` invocation.
- Accept only `^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` as a strict stable SemVer declaration.
- Require `--version` to exit 0.
- Require `--version` to print exactly `<utility-id> <MAJOR.MINOR.PATCH>` or `<utility-id> v<MAJOR.MINOR.PATCH>` plus its newline to stdout.
- Require `--version` to write nothing to stderr.
- Validate every declaration before compilation.
- Validate each compiled utility result before installing that utility.
- Validate every compiled utility result before release-metadata writes.
- Reject missing, empty, malformed, duplicate, orphaned, and mis-mapped records with a non-zero error that names the utility and recovery action.
- Preserve all independent utility declarations and outputs during repository release prep.

## Pre-Release Checklist

- Start this checklist only when the director explicitly requests standalone `Package`, `package`, `pack`, or `prep` in the active Ratified AC context.
- Do not treat `./build.sh prep ...` or ordinary build-preparation language as a workflow request.

Note: the operator flow has two steps.

1. **Run prep.**
   - Classify the AC scope under semver.
   - Draft a release message that names the delivered user-visible result in no more than 80 characters.
   - Run the stack-defined `./build.sh prep vX.Y.Z "message"` invocation.
   - Pass current validation evidence with `--validation-token` or `-t` when supported.
   - Run ordinary canonical pre-change validation for Go prep.
   - Run ordinary canonical post-change validation for Go prep.
   - Reserve validation-token evidence for Rust prep.
   - Refresh validation-token evidence for Rust prep.
   - Use `--dry-run` or `-n` to inspect without writes.
   - Use `--no-build` or `-B` only under the applicable stack policy.

   Before running prep, satisfy this repository's declared version-target contract and keep repository/package and independently versioned utility declarations aligned as required by its Project Practices.
2. **Run the printed release command.**
   - Run `./build.sh vX.Y.Z "message"`.
   - Confirm the displayed status and Git steps.
   - Approve the interactive prompt to execute the displayed sequence.

Present only the release command after prep.
Do not add trailing commentary about wrapper routing or prompts.

### Appendix: what prep does

`./build.sh prep` runs nine phases internally so the operator flow above stays short. Each phase has a clear failure mode:

1. **Validate inputs.** Semver pattern (`vX.Y.Z`), message non-empty and ≤ 80 characters.
2. **Validate git state.** Inside a git work tree, target tag does not exist yet, HEAD is not at the latest tag with a clean working tree.
3. **Run pre-change validation.** Follow the applicable stack policy for current build evidence, fallback validation, and failure handling before writes.
4. **Process version targets.**
   - Detect every version target.
   - Validate every version target.
   - Follow this repository's Project Practices.
   - Follow the stack build implementation.
   - Reject missing, malformed, duplicate, or unsafe targets before any write.
5. **Guard CHANGELOG idempotency.**
   - Detect the root `CHANGELOG.md` target.
   - Reject an existing row for the target version before any write.
6. **Parse AC refs.** `AC[0-9]+` scan on the release message; composites like `AC<m>+AC<n>` yield multiple refs.
7. **Apply writes.**
   - Apply idempotent version bumps.
   - Insert the CHANGELOG row under `| Unreleased | |`.
   - Delete each released AC file whole.
   - Sweep matching AC-pointer IE lines from `plan.md`.
   - Skip writes under `--dry-run` or `-n`.
   - Leave already-swept lines unchanged on rerun.
8. **Run post-change validation.** Follow the applicable stack policy for build-state reuse, output, failure handling, and cleanup.
9. **Print release command.** Labeled block: `release command:` followed by the indented command `./build.sh vX.Y.Z "message"`.

CHANGELOG row shape (enforced by prep's insertion code and by convention):

- Use a `# Changelog` heading.
- Follow it with the two-column `| Version | Summary |` table.
- Use `|---------|---------|` as the separator.
- Keep `| Unreleased | |` as the first data row.
- Add one row per release.
- Keep summaries single-line and no longer than 500 characters.
- Lead summaries with the AC reference when one exists.
- Versions are unprefixed (`0.29.0`, not `v0.29.0`).
- Do not backfill historical tags or invent alternative shapes (Keep-a-Changelog, sectioned `## vX.Y.Z`, etc.).

## Project Practices
