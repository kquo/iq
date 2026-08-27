# Canon-cycle doctrine

Govna embeds a versioned set of governance files called canon. This document explains how Govna publishes those files and how a repository reviews and installs an update with `govna audit` and `govna render`.

Govna publishes updated files. A repository uses `govna audit` to separate safe updates from files that need a Director choice, then follows the workflow below.

## govna-side commitments

1. **Semver.** Apply `AGENTS.md` PATCH/MINOR rules.
2. **Registries.** Update format-defining or expected-divergence registries in the AC that adds the governed file.
3. **Breaking changes.** Ship extension-clobbering removals or shape changes as MINOR with migration cost in the CHANGELOG.
4. **Alerting.**
   - Surface updates through audit.
   - Hard-fail incoherent canon for maintainer correction.

## Metadata and retired routing marker

- Treat `govna/metadata.txt` as the record of a repository's Govna file version and CODE or DOC type.
- Require `schema_version`, `canon_version`, and `repo_type`.
- Require `code_stack` only for CODE consumers.
- Skip legacy marker compatibility because Govna never shipped a marker file.
- Write metadata during `render`/`apply`.
- Write `govna/canon-baseline.txt` during `render` and `apply` as the saved hashes of Govna-managed file regions.
- Advance the saved baseline only after every other applicable acceptance test and Director choice passes and the repository check succeeds or its `Not applicable` evidence holds.
- Route an existing target path as retired when it remains in the prior baseline but disappears from current canon.
- Use the retired-path tombstone registry, the saved list of old Govna paths, for removals that predate baseline adoption.
- Preserve unrelated repository-owned governance documents unless specific baseline, retired-path, other-flavor, or governed-file evidence connects them to Govna.

## Consumer-side workflow

1. **Govna-only files.** Replace the whole file with the current embedded version.
2. **Files with Govna and local sections.** Update the Govna-managed section and preserve the repository-owned section.
3. **Director choices.** Decide whether each review file should update, remain local, migrate, or be removed.
4. **Boundaries.**
   - Replace canon above `## Project Rules` in `AGENTS.md`.
   - Replace canon above `## Project Practices` in development/editing guidelines and CODE build-release.
   - Keep each boundary and local tail.
   - Keep DOC release full canon.
5. **Files without a local-content boundary.** Follow the expected-difference and preserve-list rules in `govna/audit.md`.
6. **Saved baseline.**
   - Write the baseline from the same temporary render only after other tests and Director choices pass and the repository check succeeds.
   - Verify the baseline against that temporary render after installation.
   - Skip an immediate audit rerun.
7. **Rust evidence.** Refresh validation evidence only after that baseline copy and verification.

## Canon-owned vs repo-owned handling

- Apply these rules whenever audit surfaces a canon-owned or repo-owned divergence.
- Treat canon-owned violations as govna feedback.
- Report canon-owned violations upstream to the govna maintainer.
- Skip local patches of canon-owned text.
- Treat repo-owned violations as local repo work.
- Fix repo-owned violations directly in the next AC.
- Pause when a canon update introduces an Instruction Style violation.
- Report the violation upstream.
- Skip local rewrites of canon-owned text unless an explicit AC declares intentional divergence.

Note: audit supplies diffs; the agent classifies ownership. Fix canon in its source and templates, and local rules in their owning repo docs.
