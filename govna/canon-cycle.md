# Canon-cycle doctrine

This document describes govna's canon-update workflow, built on `govna audit` and `govna render`.

govna initiates canon updates and ships them as overlay-tracked files; consumers detect updates via `govna audit` and adopt them per the workflow below. Both sections of this doc apply at every cycle.

## govna-side commitments

What govna commits to when shipping canon updates:

1. **Semver classification.** Canon updates ship under the semver rule canonized in `AGENTS.md` Base Rules. AGENTS.md is authoritative for the PATCH/MINOR criteria and examples; this commitment is the pointer.
2. **Registry maintenance.** Format-defining and Expected-divergence registries are govna-maintained. Additions ride along in the same AC that introduces a new format-defining or per-repo-stub file. See `govna/audit.md` `## Format-defining files` and `## Expected-divergence registry`.
3. **Breaking-change protocol.** Removals or shape changes that would clobber a consumer's existing extensions ship as MINOR with a CHANGELOG note flagging the consumer-side migration cost.
4. **audit as the alerting surface.** audit is govna's tool for surfacing canon updates to consumers. The canon-coherence precondition runs canon-only; consumers running audit against a non-coherent canon get a hard-fail report routed to the govna maintainer.

## Metadata and retired routing marker

- Treat `govna/metadata.txt` as the authoritative consumer identity record.
- Require `schema_version`, `canon_version`, and `repo_type`; require `code_stack` only for CODE consumers.
- govna has no legacy marker file to accept during a compatibility window — it never shipped one.
- Write metadata during `render`/`apply`.
- Write `govna/canon-baseline.txt` during `render` and `apply` from deterministic comparison-region hashes.
- Advance the consumer baseline only after every other applicable acceptance test, resolved routing outcome, and resolved validation disposition passes.
- Route an existing target path as retired when it remains in the prior baseline but disappears from current canon.
- Use the bounded retired-path tombstone registry for removals that predate baseline adoption.
- Preserve unrelated consumer-owned governance documents unless another bounded target-only evidence source identifies them.

## Consumer-side workflow

What consumers do when receiving canon updates:

1. **The whole-file rule.** When canon ships an update to a file the consumer tracks, the consumer adopts canon's content as a whole-file baseline rather than hand-merging only the changed hunks. Hand-merging produces a third local variant that persists as drift across every future sync, compounding merge cost; whole-file snapshot keeps each file at a clean canon baseline so future syncs are hunk-additive.
2. **Application — pure-canon files.** Whole-file overwrite from canon. Typical examples: `govna/roles.md`, `govna/ac-template.md`, `govna/README.md`.
3. **Mixed-content carve-out.** The whole-file rule does NOT apply to mixed-content files — files where consumer-local content is interleaved with canon structure. Whole-file overwrite would clobber consumer content. These require **hunk-level merge**: apply canon's updates to canon-shape sections, leave consumer-local content alone.
4. **Identification.** The consumer recognizes mixed-content files from their own extensions: any file where they've added repo-specific content alongside canon structure. audit's `Format-defining: yes` flag (per `govna/audit.md` `## Format-defining files`) is an orthogonal routing signal — it forces the file to sync regardless of classification but is independent of mixed-content nature. Format-defining files may be pure-canon (e.g., `govna/ac-template.md`) or mixed-content (e.g., `AGENTS.md`); apply the whole-file rule or hunk-level merge based on the file's actual nature.
5. **Boundary-aware mixed-content files.** Files with a documented canon-above/local-below boundary that adopters merge by hand: `AGENTS.md` (boundary `## Project Rules`); `govna/development-guidelines.md`, `govna/editing-guidelines.md`, and CODE `govna/build-release.md` (boundary `## Project Practices`). Files without a named canon boundary (e.g., `README.md`, `CHANGELOG.md`, `plan.md`) would be handled by the expected-divergence registry or preserve registry — see `govna/audit.md` `## Expected-divergence registry` and `## Preserve registry`.
6. **Canon-above-local-below structure.** Mixed-content files SHOULD use the canon-above-local-below structure: canon sections at the top (govna-maintained, replaced at sync), and a single named project-extension section at the bottom (repo-maintained, untouched at sync). AGENTS.md uses `## Project Rules`; `govna/development-guidelines.md`, `govna/editing-guidelines.md`, and CODE `govna/build-release.md` use `## Project Practices`. DOC `govna/release.md` remains full canon. The named tail makes hunk-merge mechanical: replace canon zone wholesale, leave the tail alone.
7. **Why hand-merge rather than tool-automated sync.** Mixed-content files (AGENTS.md, development-guidelines.md, editing-guidelines.md) are intended to be merged by hand using the canon-above-local-below boundary because LLM-capable agents (the primary consumers) handle structured doc edits reliably from documented conventions. Documenting the convention is the durable answer; the tool stays focused on the canon-render primitive.
8. **Baseline completion.** Exclude `govna/canon-baseline.txt` from pre-install rendered-canon comparisons. Install and verify it separately from the same scratch render only after all other applicable acceptance tests, resolved routing outcomes, and the resolved validation disposition pass; do not require an immediate audit rerun.

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

Note: audit provides the diff payload; the consumer agent's review is the classifier. Local patches of canon-owned text create drift. Inside the govna repo itself, both ownership paths apply: canon-owned template/source files need canon fixes; govna-local docs can be edited as local repo work.
