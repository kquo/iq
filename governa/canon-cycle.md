# Canon-cycle doctrine

governa initiates canon updates and ships them as overlay-tracked files; consumers detect updates via `governa drift-scan` and adopt them per the workflow below. Both sections of this doc apply at every cycle.

## Governa-side commitments

What governa commits to when shipping canon updates:

1. **Semver classification.** Canon updates ship under the semver rule canonized in `AGENTS.md` Base Rules. AGENTS.md is authoritative for the PATCH/MINOR criteria and examples; this commitment is the pointer.
2. **Registry maintenance.** Format-defining and Expected-divergence registries are governa-maintained. Additions ride along in the same AC that introduces a new format-defining or per-repo-stub file. See `governa/drift-scan.md` `## Format-defining files` and `## Expected-divergence registry`.
3. **Breaking-change protocol.** Removals or shape changes that would clobber a consumer's existing extensions ship as MINOR with a CHANGELOG note flagging the consumer-side migration cost.
4. **drift-scan as the alerting surface.** drift-scan is governa's tool for surfacing canon updates to consumers. The canon-coherence precondition runs canon-only; consumers running drift-scan against a non-coherent canon get a hard-fail report routed to the governa maintainer.

## Consumer-side workflow

What consumers do when receiving canon updates:

1. **The whole-file rule.** When canon ships an update to a file the consumer tracks, the consumer adopts canon's content as a whole-file baseline rather than hand-merging only the changed hunks. Hand-merging produces a third local variant that persists as drift across every future sync, compounding merge cost; whole-file snapshot keeps each file at a clean canon baseline so future syncs are hunk-additive.
2. **Application — pure-canon files.** Whole-file overwrite from canon. Typical examples: `governa/roles.md`, `governa/ac-template.md`, `governa/README.md`.
3. **Mixed-content carve-out.** The whole-file rule does NOT apply to mixed-content files — files where consumer-local content is interleaved with canon structure. Whole-file overwrite would clobber consumer content. These require **hunk-level merge**: apply canon's updates to canon-shape sections, leave consumer-local content alone.
4. **Identification.** The consumer recognizes mixed-content files from their own extensions: any file where they've added repo-specific content alongside canon structure. drift-scan's `Format-defining: yes` flag (per `governa/drift-scan.md` `## Format-defining files`) is an orthogonal routing signal — it forces the file to sync regardless of classification but is independent of mixed-content nature. Format-defining files may be pure-canon (e.g., `governa/ac-template.md`) or mixed-content (e.g., `AGENTS.md`); apply the whole-file rule or hunk-level merge based on the file's actual nature.
5. **Boundary-aware mixed-content files.** Files with a documented canon-above/local-below boundary that adopters merge by hand: `AGENTS.md` (boundary `## Project Rules`), `governa/development-guidelines.md` and `governa/editing-guidelines.md` (boundary `## Project Practices`). Files without a named canon boundary (e.g., `README.md`, `CHANGELOG.md`, `plan.md`) are handled by the expected-divergence registry or preserve markers — see `governa/drift-scan.md` `## Expected-divergence registry` and `## Preserve-marker phrase set`.
6. **Canon-above-local-below structure.** Mixed-content files SHOULD use the canon-above-local-below structure: canon sections at the top (governa-maintained, replaced at sync), and a single named project-extension section at the bottom (repo-maintained, untouched at sync). AGENTS.md uses `## Project Rules`; `governa/development-guidelines.md` and `governa/editing-guidelines.md` use `## Project Practices`. The named tail makes hunk-merge mechanical: replace canon zone wholesale, leave the tail alone.
7. **Why hand-merge rather than tool-automated sync.** Mixed-content files (AGENTS.md, development-guidelines.md, editing-guidelines.md) are merged by hand using the canon-above-local-below boundary because LLM-capable agents (the primary consumers) handle structured doc edits reliably from documented conventions, while a previous attempt at tool-automated merge helpers introduced more failure modes than it removed. Documenting the convention is the durable answer; the tool stays focused on the canon-render primitive.

## One-time path-rename cleanup (docs/ → governa/)

The release that renamed governa's canon directory from `docs/` to `governa/` requires a one-time consumer-side cleanup. The cleanup is mechanical and bounded to that single release:

- After running `governa drift-scan` against the new canon, the consumer's pre-rename `docs/` files (the governa-managed subset) appear in the emitted AC as `target-has-no-canon` classifications under `### Routing Decisions`, offered with the standard `keep / delete / migrate-to-canon` options.
- The Director resolves each as `delete`; the consumer then performs the corresponding `git rm` against the orphan files in `docs/`. Files in `docs/` that the consumer owns (not from governa canon) stay untouched and are not surfaced.

## Canon-owned vs repo-owned handling

- Apply these rules whenever drift-scan surfaces a canon-owned or repo-owned divergence.
- Treat canon-owned violations as governa feedback.
- Report canon-owned violations upstream to the governa maintainer.
- Skip local patches of canon-owned text.
- Treat repo-owned violations as local repo work.
- Fix repo-owned violations directly in the next AC.
- Pause when a canon update introduces an Instruction Style violation.
- Report the violation upstream.
- Skip local rewrites of canon-owned text unless an explicit AC declares intentional divergence.

Note: drift-scan provides the diff payload; the consumer agent's review is the classifier. Local patches of canon-owned text create drift. Inside the governa repo itself, both ownership paths apply: canon-owned template/source files need canon fixes; governa-local docs can be edited as local repo work.
