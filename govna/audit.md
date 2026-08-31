# Audit

Run `govna audit` without positional arguments from the root of a repository that contains Govna files. Audit compares those files with the governance files built into the executable and writes a reviewable AC only when updates or Director choices are needed.

## Usage

```
govna audit [options]
```

Flags:

Here, flavor means the CODE or DOC set of Govna files selected for the repository.

- `-f, --flavor code|doc` — overlay flavor (default: auto-detect from repo signals).
- `-s, --stack <name>` — CODE stack (default: inferred from manifests; not accepted with `--flavor doc`).
- `-j, --json` — also print a JSON report to stdout alongside the markdown emission.
- `-l, --diff-lines <N>` — diff truncation limit (default: 200).
- `-n, --repo-name <name>` — override repo name (default: basename of the target directory).
- `-h, --help` — show this help.

Require `AGENTS.md`, a Git worktree, and evidence that Govna was added: a Govna AC, release, or build-release file, or a CHANGELOG reference to `govna apply` or `govna render`.

## Classification

The classification is the exact result label beside each file. Audit applies these checks in order:

| Order | Condition | Classification | Plain meaning |
|---|---|---|---|
| 1 | The file is missing and the preserve list records it. | `match` | The file needs no Govna update because the Director chose the omission. |
| 2 | The file is missing and has no preserve entry. | `missing-in-target` | A file from the embedded Govna files is missing from the repository. |
| 3 | The complete file matches the embedded Govna file. | `match` | The file needs no Govna update. |
| 4 | The Govna-managed section of a mixed file matches. | `match` | The repository-owned section is ignored and the Govna section needs no update. |
| 5 | The expected-difference list names the file. | `expected-divergence` | The repository is expected to keep its own version. |
| 6 | The file differs and the preserve list names it. | `preserve` | The preserve list says to keep the repository's version. |
| 7 | The file differs but still matches its saved baseline region. | `clear-sync` | The file is safe to update because its Govna-managed region has no local edits. |
| 8 | The file differs and does not match a saved baseline region. | `ambiguity` | Govna cannot safely choose between updating and keeping the file. |

Every emitted audit AC uses these shared explanations:

| Classification | Explanation |
|---|---|
| `match` | The file already needs no Govna update. |
| `missing-in-target` | A file from current Govna rules is missing from the repository. |
| `expected-divergence` | The repository is expected to keep its own version of this file. |
| `preserve` | The preserve list says to keep the repository's version. |
| `clear-sync` | The file still matches the previously installed Govna version and is safe to update. |
| `ambiguity` | Govna cannot safely choose between updating and keeping the file. |
| `target-has-no-canon` | The file is absent from the selected current canon, but specific repository evidence connects it to Govna. |
| `migration-required` | A required Govna control file is missing and must be added through the AC. |

`govna/metadata.txt` gets metadata-specific handling layered on top. An absent file is forced to `migration-required` regardless of the byte-comparison result (see Migration-required items). A present `canon_version` must use strict `vMAJOR.MINOR.PATCH` form. When the target version is lower than embedded canon and replacing only that field makes the whole file byte-equal to rendered canon, the file is forced to `clear-sync` regardless of git history or a preserve-registry entry. Other metadata differences remain whole-file review items. A malformed version fails before AC emission; a target version newer than embedded canon also fails and directs the operator to upgrade govna rather than downgrade consumer metadata.

Files absent from the selected embedded Govna files use `target-has-no-canon` only when specific baseline, retired-path, other-flavor, or governed-file evidence connects them to Govna. See Target-only detection.

## Format-defining files

`govna/ac-template.md` and `AGENTS.md` are format-defining: any non-`match`, non-`expected-divergence` classification for these two files forces a sync entry in the emitted stub regardless of what the ordered check above produced (an `ambiguity` or `preserve` result still surfaces as a forced-sync note, since these two files define the shape every other AC and canon doc depends on).

## Expected-divergence registry

`plan.md` and `arch.md` are registered as expected per-repo divergence — canon ships them as content stubs, and every adopting repo is expected to carry repo-specific content in their place. Divergence here never routes to review.

## Mixed-content boundary registry

Files with a documented canon-above/local-below boundary, compared only above the boundary line for `match`:

| File | Boundary |
|---|---|
| `AGENTS.md` | `## Project Rules` |
| CODE `govna/build-release.md` | `## Project Practices` |
| `govna/development-guidelines.md` | `## Project Practices` |
| `govna/editing-guidelines.md` | `## Project Practices` |

Treat an existing CODE `govna/build-release.md` without its registered boundary as a one-time reviewed migration. Route it to `ambiguity` for full-file review even when a legacy whole-file preserve phrase exists, and retain that phrase as migration evidence. During reapply, leave the boundary-less file unchanged and emit a manual migration item. Place reviewed repository-specific release mechanics below the new boundary, sync rendered canon above it, and remove any obsolete registry entry and exact legacy phrase only through the consumer's authorized adoption cycle. Keep DOC `govna/release.md` outside this mixed-content model.

## Preserve registry

Use optional `govna/preserve.txt` as the sole durable preserve authority. Treat an absent file as an empty registry. Require an existing file to use this exact schema:

```text
govna-preserve-v1
<repo-relative-path>
```

Require a final newline. Keep entries nonempty, slash-normalized, unique, and byte-sorted. Reject absolute paths, backslashes, tabs, blank entries, `.` or `..` components, leading or trailing slashes, duplicates, and `govna/preserve.txt` itself. Accept a header-only file as the canonical empty on-disk registry.

Add an exact path for a resolved preserve outcome. Remove an exact path for a resolved sync, delete, or canon-backed migration outcome. Preserve unrelated entries. Leave the registry absent or unchanged when its state already satisfies every resolved outcome. Verify registry changes before installing the canon baseline.

- Classify an existing target-only path named in the preserve registry as `preserve`.
- Keep that preserved target-only path visible in audit and JSON results.
- Omit another routing question for that preserved target-only path.

Exclude `govna/preserve.txt` from rendered canon, canon baselines, ordinary audit drift, name-referenced target-only evidence, and ordinary rm target-only content. Include it in rm only as the final control-state deletion after applying all registered preserve decisions.

Treat only exact legacy preserve phrases in the Unreleased CHANGELOG Summary as migration evidence: `preserve <path>`, `do not sync <path>`, `intentional divergence: <path>`, and `<path>: keep local`. Route each phrase under `### Routing capabilities`. Remove it only after verifying its required target and registry state. Preserve unrelated Summary text and historical rows. Ignore matching prose in historical CHANGELOG rows, emitted ACs, and every other governance document.

A registry entry on a missing current-canon file suppresses `missing-in-target` to a suppressed `match`; an entry on a divergent current-canon file or an existing target-only file routes it to `preserve` instead of a review classification. Exceptions are an eligible stale-version-only `govna/metadata.txt`, whose canon-owned `canon_version` cannot be pinned, and a boundary-less CODE `govna/build-release.md`, which remains a reviewed migration.

## Target-only detection

Audit classifies an existing target as `target-has-no-canon` when the path is absent from current flavor canon, the preserve registry does not name it, and at least one bounded evidence source identifies it: the valid prior baseline, the pre-baseline retired-path tombstone registry, other-flavor canon, or a path reference from an already-divergent governed file. Evidence is merged by target path with tombstone replacement metadata retained, then emitted in deterministic path order.

The tombstone registry bridges removals that predate baseline adoption. It currently records `govna/drift-scan.md` as replaced by `govna/audit.md`. A missing current-canon replacement already appears as a direct update. The emitted AC names and installs that replacement before routing the retired source to preserve, explicitly named migration, or delete. It never offers restore as a separate routing outcome.

Audit does not flag arbitrary consumer-owned governance documents that have none of these evidence sources. Audit never deletes or migrates a target file itself.

## Migration-required items

`govna/metadata.txt` or `govna/canon-baseline.txt` absent from an otherwise govna-adopted target classifies as `migration-required`. Every emitted AC includes `## Migration findings` after `## Out Of Scope`: it lists each migration path and completion action, or `None` when no migration exists. Migration paths also remain under `## In Scope`.

## Canon baseline manifest

`govna/canon-baseline.txt` is the baseline: the saved hashes of the Govna-managed file regions previously installed in the repository. Its first line is `govna-canon-baseline-v1`, its second line is `canon_version = vMAJOR.MINOR.PATCH`, and each sorted remaining line is `<path><TAB><scope><TAB><sha256>`. Scope is `full` or `before:<boundary-heading>`. The manifest excludes itself and `govna/preserve.txt`; neither is classified as an ordinary governed file.

Audit fails before emission for malformed fields, duplicate or unsorted paths, invalid hashes, unknown or mismatched scopes, or a baseline canon version newer than embedded canon. A valid manifest missing one file entry routes that divergent file to `ambiguity`. Audit leaves the baseline unchanged; the emitted AC installs or replaces it last after all other work succeeds.

- Accept legacy `full` scope only for `govna/build-release.md` in a CODE target whose baseline canon version predates v0.11.0.
- Retain the legacy hash only as migration evidence.
- Apply normal boundary migration and comparison behavior after parsing.
- Reject the exception for DOC targets, other paths, v0.11.0-or-newer baselines, and every other mismatched scope.
- Leave the accepted baseline unchanged during audit.
- Replace it with the rendered bounded baseline only as the emitted adoption AC's final step.

## Canon-coherence precondition

Before comparing anything against the target, audit checks that Govna's rendered files agree with each other. This deterministic precondition checks only embedded canon. It does not replace the consumer-equivalent candidate-canon review in `govna/canon-cycle.md`.

- Require `govna/roles.md` to reference the release document present in the selected flavor.
- Reject a `govna/roles.md` reference to the absent opposite-flavor release document.
- Require `AGENTS.md` to define the structured Package completion report.
- Require the selected release guide to end that structured report with the exact release command.
- Reject retired command-only Package wording.
- Require integrated audit adoption to enter Refine without another instruction.
- Require integrated audit adoption to stop before Implement.

Audit skips target comparison when any coherence rule fails. The error names the conflicting rendered file and directs the Operator to report it to the Govna maintainer.

## Emitted AC stub

Audit writes `govna/ac<N>-audit-<canon-version>.md` only when the repository has files ready to update, required control files to add, or files needing a Director choice. The canon version identifies the embedded governance-file version; `N` follows the monotonic AC-numbering rule. Clear-sync, missing-target, migration-required, ambiguity, target-has-no-canon, and format-defining forced-sync results require work. Match, expected-divergence, and ordinary preserve results do not. The generated AC follows `govna/ac-template.md` and groups every non-`match` file as follows:

- **Files ready to update** — `clear-sync`, `missing-in-target`, and any format-defining file forced to sync.
- **Required control files** — `migration-required` items under `## Migration findings`.
- **Out of scope** — files that stay unchanged: `preserve` and `expected-divergence`.
- **Files needing a Director choice** — `ambiguity` and `target-has-no-canon`.

The stub carries an edit-detection marker (SHA-256 body hash). Re-running audit against an unedited stub for the same canon version reuses the same AC number. Re-running it against an edited stub fails and directs the Director to delete or rename that generated file before retrying.

An audit with no updates or Director choices exits successfully and prints `No Govna updates or Director choices found`, followed by a plain result tally and `No AC was written.` It performs no AC-number allocation, stub inspection, directory creation, or file write. It never deletes, overwrites, or validates an existing audit stub. With `--json`, the complete report remains available and `emitted` is `null`; no additional prose is written.

### Agent-mediated review

- Resolve the Govna executable path before running the agent-mediated audit.
- Record the detailed version output from that resolved executable.
- Run the ordinary agent-mediated audit without `--json`.
- Enter Audit only when the command emits or reuses one guarded adoption AC.
- Keep a clean result or pre-emission failure outside the AC phases.
- Audit the emitted AC immediately.
- Require the emitted AC marker versions to match the recorded detailed version.
- Create exactly one unique system-temporary scratch directory outside the consumer repository.
- Render the selected canon into that scratch directory once with the resolved executable.
- Require the rendered baseline canon version to match the emitted AC marker.
- Compare every actionable path through the emitted `### Audit Review` instructions.
- Review the exact proposed rules.
- Check rule overlap and placement.
- Resolve every candidate reference.
- Measure prospective contract growth.
- Verify target-side acceptance evidence.
- Keep the emitted AC and consumer repository unchanged during Audit and Refine.
- Remove the exact scratch directory before reporting Audit completion or a blocker.
- End scratch-review authority when Audit ends.
- Report every blocking finding and Director decision.
- Pause while any blocking finding or Director decision remains unresolved.
- Resume Refine after the Director resolves every blocking finding and decision.
- Require a new audit emission when a required correction would change the immutable AC.
- Complete Refine without editing the emitted AC when no blocker remains.
- Run Pre-Implementation Verification after Refine.
- Report implementation readiness only when Pre-Implementation Verification passes.
- Remain in Refine when Pre-Implementation Verification finds a gap.
- Stop before Implement.
- Track the active phase in the session instead of the emitted AC.

Note: the executable ends after deterministic comparison and emission. The original explicit `govna audit` request authorizes this one bounded scratch review and its exact cleanup. The immutable AC records the adoption work; the active session records its phase. JSON remains available as optional machine output, but its diff fields are not required evidence for ordinary agent-mediated review.

Effective implementation scope is the narrow rule that permits a directly affected supporting file to change when the Director already settled its outcome. Every Director-resolved routing target enters that scope while the generated AC remains unchanged. Explicitly named migration destinations also enter it. `govna/preserve.txt` enters only when a resolved outcome requires creating or changing it. `CHANGELOG.md` enters only when a resolved legacy-phrase outcome requires removing an exact phrase. Neither supporting-file adjustment requires a second Director authorization.

### Emitted AC instruction and phase shape

- Name each emitted adoption AC `# AC<N> Adopt Govna Governance Files v<CANON_VERSION>`.
- Place the repository paragraph first under `## Summary`.
- Start the repository paragraph with `This AC updates`.
- Follow it with `The result label (classification)`.
- Place the count paragraph after the repository paragraph.
- Start the count paragraph with `Govna found`.
- Keep the count and Summary paragraphs descriptive.
- Place one `### Audit Review` section before `### Adoption Instructions`.
- Bind Audit Review to the resolved executable and emitted marker versions.
- Require one unique scratch render outside the consumer repository.
- Emit one executable review command for every actionable path.
- Apply `### Mixed-content sync verification` to every existing mixed-content review target.
- Require exact rule, overlap, placement, reference, contract-growth, and acceptance-evidence review.
- Require exact scratch cleanup before the Audit report.
- Omit rendered diff bodies from the emitted AC.
- Omit companion review artifacts.
- Confirm each file selected for update exists in the selected CODE render.
- Place that CODE-render check and all routing procedure under `### Adoption Instructions`.
- Omit the CODE-render check from DOC audit emissions.
- Emit each adoption instruction as one imperative bullet.
- Format every numbered routing entry as one Director decision question.
- End every numbered routing entry with `?`.
- Keep shared implementation procedure out of routing questions.
- End every emitted adoption AC with exact status `` `PENDING` — immutable audit emission; workflow state is tracked in the active session.``

### Routing capabilities

- Offer only these outcomes for a canon-backed ambiguity: sync, preserve, explicitly named migration, delete.
- Offer only these outcomes for an ordinary `target-has-no-canon` item: preserve, explicitly named migration, delete.
- Require the Director to name every migration destination in the routing response.
- Install an exact current-canon replacement before retired-source routing.
- Offer only these outcomes for that retired source: preserve, explicitly named migration, delete.
- Omit restore as a routing outcome.
- Define marker-only evidence as an exact Unreleased CHANGELOG preserve phrase whose referenced path has no independent file action.
- Offer conversion to `govna/preserve.txt` or exact-phrase removal for marker-only evidence.
- Add the referenced path to `govna/preserve.txt` for a conversion choice.
- Leave the referenced target unchanged during marker-only conversion.
- Remove the converted phrase after registry verification.
- Remove only the exact phrase for a marker-only removal choice.
- Preserve the referenced target during marker-only phrase removal.
- Preserve unrelated registry state during marker-only phrase removal.
- Apply each independently actionable file's capability-specific route before legacy-phrase cleanup.
- Treat a preserve choice on that file as conversion of its legacy phrase.
- Verify the result of every resolved sync, migration, or deletion before legacy-phrase cleanup.
- Remove the exact legacy phrase after that verification.
- Preserve unrelated CHANGELOG Summary text and historical rows.

### Mixed-content sync verification

- Capture the SHA-256 digest of each existing mixed-content target from the first byte of its exact registered boundary-heading line through end of file.
- Include the boundary line, its line ending, the complete repository-owned tail, and the final-newline state in the protected region.
- Emit the expected digest and boundary in the file-specific automated acceptance test for every direct sync.
- Emit the same conditional verification for every review item whose Director resolution is sync.
- Recompute the protected-region digest after adoption.
- Require the protected-region digest to match the emitted digest.
- Keep rendered-canon comparison scoped to the canon zone above the boundary.
- Apply that scope to Audit Review when both files contain the exact registered boundary.
- Keep whole-file Audit Review comparison when either file lacks the exact registered boundary.
- Preserve existing missing-target and target-only Audit Review commands.
- Avoid comparing the repository-owned tail with rendered defaults.
- Keep the protected-region digest out of classification, baseline scope, and JSON output.

### Conditional routing verification

- Emit a conditional rendered-region check for each offered sync outcome.
- Emit a conditional preserve-registry exclusion check for each offered sync outcome.
- Emit a conditional target-presence check for each offered preserve outcome.
- Emit a conditional preserve-registry inclusion check for each offered preserve outcome.
- Emit a conditional target-absence check for each offered delete outcome.
- Emit a conditional preserve-registry exclusion check for each offered delete outcome.
- Emit a conditional named-destination check for each offered migration outcome.
- Emit a conditional source check for each offered migration outcome.
- Emit a conditional canon-backed destination check for each offered migration outcome.
- Emit a conditional repository-owned destination check for each offered migration outcome.
- Emit a conditional preserve-registry check for each canon-backed migration outcome.
- Emit a replacement-before-retired-source check for each replacement-missing route.
- Emit a referenced-target state check for each marker-only route.
- Emit a conversion registry check for each marker-only route.
- Emit a removal registry check for each marker-only route.
- Emit an exact-phrase absence check for each legacy-phrase route.
- Emit a target-before-phrase check for each independently actionable legacy-phrase route.
- Emit an unrelated-Summary preservation check for each legacy-phrase route.
- Emit an outside-Summary preservation check for each legacy-phrase route.
- Keep every emitted routing check atomic.
- Keep emitted AT numbering stable across identical reports.

- Apply repository-check inference when baseline installation or replacement is present.
- Infer the repository check only from bounded target governance evidence.
- Accept positive declarations only from exactly one AGENTS.md rule shaped ``Run `<command>` as the first validation command ...`` and exactly one rule shaped ``Use `<command>` for repository-wide ... validation ...``.
- Require both positive declarations to name `./build.sh` for CODE inference.
- Require root `build.sh` to resolve to a regular file for CODE inference.
- Require the selected CODE stack's recognized root manifest before inferring `./build.sh`.
- Recognize `go.mod`, `Cargo.toml`, `Package.swift`, and `.terraform.lock.hcl` or a root `*.tf` for Go, Rust, Swift, and Terraform respectively.
- Require each recognized manifest path used as evidence to resolve to a regular file.
- Treat selected-stack manifest evidence only as proof that the declared repository command can run.
- Keep exact AGENTS.md declarations as the repository-command authority.
- Infer `Not applicable` for DOC only when `govna/release.md` contains the exact canon no-automated-content-validation declaration and AGENTS.md contains no recognized positive declaration.
- Leave missing, duplicate, incomplete, mismatched, positive-plus-negative, non-`./build.sh`, or non-regular-file evidence unresolved for a Director decision.
- Leave absent, non-regular, or other-stack-only selected-manifest evidence unresolved for a Director decision.
- Ignore unrelated manifests, other prose, governance documents, executables, CI files, and flavor defaults.

- Record inferred repository-check evidence without requesting Director confirmation.
- Omit the repository-check question and its manual resolution AT when the check is inferred.
- Emit an unresolved repository check as the final numbered routing decision.
- Use the exact unresolved repository-check question recorded in the note below.
- Emit one manual resolution AT for an unresolved repository check.
- Place the manual repository-check AT after every protected-region AT.
- Emit one automated verification AT for an unresolved repository check.
- Place the automated repository-check AT immediately after its manual AT.
- Use singular nouns in emitted count summaries only for a count of one.
- Use plural nouns in emitted count summaries for zero or multiple counts.

Note: exact unresolved repository-check question: ``<N>. **Repository check**: Which command should run after the selected file updates, or what repository evidence shows that no command applies?``

Emitted acceptance tests verify updates, required control files, every offered routing outcome, replacement ordering, legacy-phrase cleanup, and preservation according to the Director's choices. The pre-install rendered-file check covers declared update items except `govna/canon-baseline.txt`, review targets selected for update, and migration destinations backed by embedded Govna files. After all selected work, the chosen repository command must succeed, or the `Not applicable` evidence must hold. Only after every other applicable automated AT and routing outcome passes does the baseline get installed and verified separately from the same temporary render as the final step.

Every audit-emitted AT carries exactly one source axis and one explicit timing axis. Current audit ATs use `[Automated] [Pre-release gate]` or `[Manual] [Pre-release gate]`; none defer verification until after release.

Pass `--json` to print a machine-readable report (`header`: invocation, canon SHA, target, flavor and its source, repo name, govna/code-stack versions from metadata; `files`: one entry per scanned file with its classification, effective classification when force-synced, diff, prior commits, matched preserve-registry entries, legacy preserve-phrase evidence, canon reference, and mixed-content boundary where applicable; `emitted`: the stub's path for actionable reports or `null` for clean reports).
