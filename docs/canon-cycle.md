# Canon-cycle doctrine

governa initiates canon updates and ships them as overlay-tracked files; consumers detect updates via `governa drift-scan` and adopt them per the workflow below. Both sections of this doc apply at every cycle.

## Governa-side commitments

What governa commits to when shipping canon updates:

1. **Semver classification.** Canon updates ship under the semver rule canonized in `AGENTS.md` Base Rules. AGENTS.md is authoritative for the PATCH/MINOR criteria and examples; this commitment is the pointer.
2. **Registry maintenance.** Format-defining and Expected-divergence registries are governa-maintained. Additions ride along in the same AC that introduces a new format-defining or per-repo-stub file. See `docs/drift-scan.md` `## Format-defining files` and `## Expected-divergence registry`.
3. **Breaking-change protocol.** Removals or shape changes that would clobber a consumer's existing extensions ship as MINOR with a CHANGELOG note flagging the consumer-side migration cost.
4. **drift-scan as the alerting surface.** drift-scan is governa's tool for surfacing canon updates to consumers. The canon-coherence precondition runs canon-only; consumers running drift-scan against a non-coherent canon get a hard-fail report routed to the governa maintainer.

## Consumer-side workflow

What consumers do when receiving canon updates:

1. **The whole-file rule.** When canon ships an update to a file the consumer tracks, the consumer adopts canon's content as a whole-file baseline rather than hand-merging only the changed hunks. Hand-merging produces a third local variant that persists as drift across every future sync, compounding merge cost; whole-file snapshot keeps each file at a clean canon baseline so future syncs are hunk-additive.
2. **Application — pure-canon files.** Whole-file overwrite from canon. Typical examples: `cmd/rel/main.go`, `docs/critique-protocol.md`, `docs/roles.md`, `docs/ac-template.md`, `docs/README.md`.
3. **Mixed-content carve-out.** The whole-file rule does NOT apply to mixed-content files — files where consumer-local content is interleaved with canon structure. Whole-file overwrite would clobber consumer content. These require **hunk-level merge**: apply canon's updates to canon-shape sections, leave consumer-local content alone.
4. **Identification.** The consumer recognizes mixed-content files from their own extensions: any file where they've added repo-specific content alongside canon structure. drift-scan's `Format-defining: yes` flag (per `docs/drift-scan.md` `## Format-defining files`) is an orthogonal routing signal — it forces the file to sync regardless of classification but is independent of mixed-content nature. Format-defining files may be pure-canon (e.g., `docs/ac-template.md`, `docs/critique-protocol.md`) or mixed-content (e.g., `AGENTS.md`); apply the whole-file rule or hunk-level merge based on the file's actual nature.
5. **Typical mixed-content examples.** `AGENTS.md` (canon Base Rules + consumer Project Rules entries + Interaction Mode bullets), `README.md`, `CHANGELOG.md`, `plan.md`.
6. **Canon-above-local-below structure.** Mixed-content files SHOULD use the canon-above-local-below structure: canon sections at the top (governa-maintained, replaced at sync), and a single named project-extension section at the bottom (repo-maintained, untouched at sync). AGENTS.md uses `## Project Rules`; `docs/development-guidelines.md` and `docs/editing-guidelines.md` use `## Project Practices`. The named tail makes hunk-merge mechanical: replace canon zone wholesale, leave the tail alone.
