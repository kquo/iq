# Canon-cycle doctrine

This document describes govna's canon-update workflow, built on `govna audit` and `govna render`.

govna initiates canon updates and ships them as overlay-tracked files; consumers detect updates via `govna audit` and adopt them per the workflow below. Both sections of this doc apply at every cycle.

## govna-side commitments

1. **Semver.** Apply `AGENTS.md` PATCH/MINOR rules.
2. **Registries.** Update format-defining or expected-divergence registries in the AC that adds the governed file.
3. **Breaking changes.** Ship extension-clobbering removals or shape changes as MINOR with migration cost in the CHANGELOG.
4. **Alerting.**
   - Surface updates through audit.
   - Hard-fail incoherent canon for maintainer correction.

## Metadata and retired routing marker

- Treat `govna/metadata.txt` as the authoritative consumer identity record.
- Require `schema_version`, `canon_version`, and `repo_type`.
- Require `code_stack` only for CODE consumers.
- govna has no legacy marker file to accept during a compatibility window — it never shipped one.
- Write metadata during `render`/`apply`.
- Write `govna/canon-baseline.txt` during `render` and `apply` from deterministic comparison-region hashes.
- Advance the consumer baseline only after every other applicable acceptance test, resolved routing outcome, and resolved validation disposition passes.
- Route an existing target path as retired when it remains in the prior baseline but disappears from current canon.
- Use the bounded retired-path tombstone registry for removals that predate baseline adoption.
- Preserve unrelated consumer-owned governance documents unless another bounded target-only evidence source identifies them.

## Consumer-side workflow

1. **Pure canon.** Replace tracked pure-canon files wholesale to avoid persistent third variants.
2. **Mixed content.** Hunk-merge canon structure while preserving consumer content.
3. **Routing.** Treat format-defining status independently from pure-versus-mixed application.
4. **Boundaries.**
   - Replace canon above `## Project Rules` in `AGENTS.md`.
   - Replace canon above `## Project Practices` in development/editing guidelines and CODE build-release.
   - Keep each boundary and local tail.
   - Keep DOC release full canon.
5. **Unbounded files.** Route expected or preserved divergence through the registries in `govna/audit.md`.
6. **Baseline.**
   - Install the baseline from the same scratch render only after other tests, routes, and validation pass.
   - Verify the baseline from that scratch render after installation.
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
