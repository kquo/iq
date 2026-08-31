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

## Candidate-canon review

Candidate canon is the embedded governance version under review. A consumer-equivalent review uses the rendered files, actual audit output, and emitted AC that an adopting repository receives. A state-equivalence fixture covers one distinct audit branch without repeating a profile-independent branch for every profile. A Govna-canon finding is a fault in shared governance rather than repository-owned behavior.

### Render and audit

- Run this review before completing a Govna canon change.
- Render DOC and every registered CODE stack.
- Exercise every rendered profile through ordinary non-JSON `govna audit` at least once.
- Resolve the Govna executable before each consumer-equivalent audit.
- Reuse that resolved executable for the Audit scratch render.
- Verify the emitted marker versions against the resolved executable.
- Verify the rendered baseline canon version against the emitted marker.
- Cover every audit classification across the state-equivalence fixtures.
- Cover each force-sync state across the state-equivalence fixtures.
- Cover every routing outcome across the state-equivalence fixtures.
- Cover every marker-only choice across the state-equivalence fixtures.
- Cover legacy-phrase cleanup across the state-equivalence fixtures.
- Cover retired-replacement ordering across the state-equivalence fixtures.
- Cover mixed-content protection across the state-equivalence fixtures.
- Cover inferred, not-applicable, and unresolved repository checks across the state-equivalence fixtures.
- Reuse a fixture only for a profile-independent branch.
- Exercise each flavor-specific branch in its owning profile.
- Exercise each stack-specific branch in its owning profile.

### Emitted AC review

- Audit every actionable emitted AC immediately.
- Complete one bounded scratch review for every actionable emitted AC.
- Compare every actionable path without JSON diff fields.
- Keep the emitted AC and consumer fixture byte-identical during review.
- Remove the exact scratch directory before completing review.
- Verify every offered routing outcome is executable.
- Verify every offered routing outcome has conditional acceptance coverage.
- Verify every emitted reference resolves in the selected render or named repository state.
- Verify the emitted instructions agree with the selected render.
- Verify phase entry and exit remain compatible.
- Verify completion output remains compatible across its governing documents.

### Behavior parity

- Map every provided command or executable artifact to its governing claims.
- Map every governing claim to a named behavioral regression.
- Verify every supported profile provides its required canonical command implementation.
- Block completion on any claim-to-behavior mismatch.

### Release-batch safety

- Exercise one-AC and fitting multi-AC pending batches.
- Exercise an oversized projected batch before another Implement.
- Exercise oversized, partial, and partly unaccepted batches before Package prep.
- Require every implemented pending-batch member to complete Ratify before Package.
- Verify whole-tree release staging cannot bypass complete-batch rules.
- Verify 80-byte acceptance with ASCII and multibyte messages.
- Reject 81-byte messages before release-prep mutation.
- Verify numbered Audit and Refine steps remain atomic in CODE and DOC renders.

### Review evidence

- Keep the review record in the closure-audit session.
- Record each profile, render, fixture scenario, audit result, and emitted AC.
- Record each reference, instruction, phase, route, acceptance, and completion check.
- Record behavior parity and finding disposition.
- Block Implement completion on any unresolved Govna-canon finding.

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

Note: JSON audit output may supply bounded diffs, but ordinary agent-mediated review uses the immutable AC and one authorized scratch render. Fix canon in its source and templates, and local rules in their owning repo docs.
