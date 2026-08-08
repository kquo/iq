# Audit

`govna audit` compares an adopted repo's governance artifacts against what `govna render` would produce for it now, and emits a `govna/ac<N>-audit-<canon-version>.md` stub listing the divergences for the Director to resolve.

Run it from the consumer repo root (no positional arguments) after `govna render` or `govna apply`.

## Usage

```
govna audit [options]
```

Flags:

- `-f, --flavor code|doc` — overlay flavor (default: auto-detect from repo signals).
- `-s, --stack <name>` — CODE stack (default: inferred from manifests; not accepted with `--flavor doc`).
- `-j, --json` — also print a JSON report to stdout alongside the markdown emission.
- `-l, --diff-lines <N>` — diff truncation limit (default: 200).
- `-n, --repo-name <name>` — override repo name (default: basename of the target directory).
- `-h, --help` — show this help.

Preconditions: the target must be a govna-adopted repo (`AGENTS.md` present, plus a govna adoption signal — one of `govna/ac-template.md`, `govna/release.md`, `govna/build-release.md`, or a `CHANGELOG.md` row referencing `govna apply` or `govna render`) and a git worktree (`.git/` present, `git` on `PATH`).

## Classification

Each canon-governed file gets exactly one of 8 classifications, decided by an ordered check:

1. **Missing from target, preserve-registry entry found** → `match` (suppressed) — a Director has already declared the omission intentional.
2. **Missing from target, no registry entry** → `missing-in-target`.
3. **Byte-equal to canon** → `match`.
4. **Mixed-content file, canon zone byte-equal** (see Mixed-content boundary registry below) → `match` — the repo-owned tail below the boundary is not compared.
5. **Listed in the expected-divergence registry** → `expected-divergence`.
6. **Otherwise divergent, preserve-registry entry found** → `preserve`.
7. **Otherwise divergent, valid baseline entry matches the target comparison region** → `clear-sync` — the target still equals its previously rendered canon and can safely adopt current canon.
8. **Otherwise divergent, baseline entry missing or not matching the target comparison region** → `ambiguity` — a Director must decide sync vs. keep. During first baseline migration only, commit history remains the conservative fallback.

`govna/metadata.txt` gets metadata-specific handling layered on top. An absent file is forced to `migration-required` regardless of the byte-comparison result (see Migration-required items). A present `canon_version` must use strict `vMAJOR.MINOR.PATCH` form. When the target version is lower than embedded canon and replacing only that field makes the whole file byte-equal to rendered canon, the file is forced to `clear-sync` regardless of git history or a preserve-registry entry. Other metadata differences remain whole-file review items. A malformed version fails before AC emission; a target version newer than embedded canon also fails and directs the operator to upgrade govna rather than downgrade consumer metadata.

Retired and otherwise evidenced target files with no canon counterpart in the target's own flavor route to `target-has-no-canon` (see Target-only detection) rather than through this ordered check.

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

Exclude `govna/preserve.txt` from rendered canon, canon baselines, ordinary audit drift, name-referenced target-only evidence, and ordinary rm target-only content. Include it in rm only as the final control-state deletion after applying all registered preserve decisions.

Treat only exact legacy preserve phrases in the Unreleased CHANGELOG Summary as migration evidence: `preserve <path>`, `do not sync <path>`, `intentional divergence: <path>`, and `<path>: keep local`. Route each phrase to an explicit convert-or-remove decision. Remove it only after verifying the resolved registry state. Preserve unrelated Summary text and historical rows. Ignore matching prose in historical CHANGELOG rows, emitted ACs, and every other governance document.

A registry entry on a missing file suppresses `missing-in-target` to a suppressed `match`; an entry on a divergent file routes it to `preserve` instead of `ambiguity` or `clear-sync`. Exceptions are an eligible stale-version-only `govna/metadata.txt`, whose canon-owned `canon_version` cannot be pinned, and a boundary-less CODE `govna/build-release.md`, which remains a reviewed migration.

## Target-only detection

Audit classifies an existing target as `target-has-no-canon` when the path is absent from current flavor canon and at least one bounded evidence source identifies it: the valid prior baseline, the pre-baseline retired-path tombstone registry, other-flavor canon, or a path reference from an already-divergent governed file. Evidence is merged by target path with tombstone replacement metadata retained, then emitted in deterministic path order.

The tombstone registry bridges removals that predate baseline adoption. It currently records `govna/drift-scan.md` as replaced by `govna/audit.md`. Audit recommends deleting the retired path only when the replacement is present; otherwise it recommends restoring or migrating the replacement first.

Audit does not flag arbitrary consumer-owned governance documents that have none of these evidence sources. Audit never deletes or migrates a target file itself.

## Migration-required items

`govna/metadata.txt` or `govna/canon-baseline.txt` absent from an otherwise govna-adopted target classifies as `migration-required`. Every emitted AC includes `## Migration findings` after `## Out Of Scope`: it lists each migration path and completion action, or `None` when no migration exists. Migration paths also remain under `## In Scope`.

## Canon baseline manifest

`govna/canon-baseline.txt` records the exact prior rendered comparison region for each governed file. Its first line is `govna-canon-baseline-v1`, its second line is `canon_version = vMAJOR.MINOR.PATCH`, and each sorted remaining line is `<path><TAB><scope><TAB><sha256>`. Scope is `full` or `before:<boundary-heading>`. The manifest excludes itself and `govna/preserve.txt`; neither is classified as an ordinary governed file.

Audit fails before emission for malformed fields, duplicate or unsorted paths, invalid hashes, unknown or mismatched scopes, or a baseline canon version newer than embedded canon. A valid manifest missing one file entry routes that divergent file to `ambiguity`. Audit leaves the baseline unchanged; the emitted AC installs or replaces it last after all other work succeeds.

- Accept legacy `full` scope only for `govna/build-release.md` in a CODE target whose baseline canon version predates v0.11.0.
- Retain the legacy hash only as migration evidence.
- Apply normal boundary migration and comparison behavior after parsing.
- Reject the exception for DOC targets, other paths, v0.11.0-or-newer baselines, and every other mismatched scope.
- Leave the accepted baseline unchanged during audit.
- Replace it with the rendered bounded baseline only as the emitted adoption AC's final step.

## Canon-coherence precondition

Before comparing anything against the target, audit checks that govna's own rendered canon is internally coherent — a registry-driven, canon-only precondition (`coherence_rules()`) that would catch cases like an overlay template drifting out of sync with its authority doc. The registry ships empty today. If a future rule fails, audit skips target comparison and emits a coherence-failure report.

## Emitted AC stub

Audit emits `govna/ac<N>-audit-<canon-version>.md` (`N` allocated per the monotonic AC-numbering rule) only when the completed report contains actionable work. Clear-sync, missing-target, migration-required, ambiguity, target-has-no-canon, and format-defining forced-sync results are actionable. Match, expected-divergence, and ordinary preserve results are non-actionable. An emitted AC conforms to `govna/ac-template.md`, and its `## In Scope` groups every non-`match` file into one of four buckets:

- **Sync** — `clear-sync`, `missing-in-target`, and any format-defining file forced to sync.
- **Migration** — `migration-required` items, under `## Migration findings`.
- **Out of scope** — `preserve` and `expected-divergence` items, explicitly excluded from this cycle's sync.
- **Review** — `ambiguity` and `target-has-no-canon` items, needing a Director routing decision before either syncing or preserving.

The stub carries an edit-detection marker (SHA-256 body hash). Re-running audit against an unedited stub for the same canon version reuses the same AC number; running it against an edited stub fails with an error directing the Director to commit and delete the stub or rename it off the `audit-<version>` slug.

A non-actionable audit exits successfully, prints the classification tally followed by `no AC emitted`, and performs no AC-number allocation, stub inspection, directory creation, or file write. It never deletes, overwrites, or validates an existing audit stub. With `--json`, the complete report remains available and `emitted` is `null`; no additional prose is written.

Every Director-resolved routing target becomes effective implementation scope while the emitted stub remains unchanged. Explicitly named migration destinations join that scope. `govna/preserve.txt` joins that scope only when a resolved outcome requires creating or changing it, without a second Director authorization.

When baseline migration is present, audit infers validation only from bounded target governance evidence. Positive declarations come only from exactly one AGENTS.md rule shaped ``Run `<command>` as the first validation command ...`` and exactly one rule shaped ``Use `<command>` for repository-wide ... validation ...``; CODE infers `./build.sh` only when both name that command and root `build.sh` is a regular file. DOC infers `Not applicable` only when `govna/release.md` contains the exact canon no-automated-content-validation declaration and AGENTS.md contains no recognized positive declaration. Missing, duplicate, incomplete, mismatched, positive-plus-negative, non-`./build.sh`, or non-regular-file evidence stays unresolved for a Director decision. Audit ignores other prose, governance documents, executables, manifests, CI files, and flavor defaults.

An inferred disposition records its evidence without requesting Director confirmation. A baseline-only migration with inferred validation has no routing decision or manual routing AT. An unresolved disposition remains a numbered routing decision with the existing manual and conditional routing ATs. Emitted count summaries use singular nouns only for a count of one and plural nouns for zero or multiple counts.

Emitted acceptance tests verify sync, migration, deletion, and preservation according to the resolved outcome. The pre-install rendered-canon blanket covers declared sync items except `govna/canon-baseline.txt`, routing targets resolved as sync, and canon-backed migration destinations. After all selected work, the resolved validation command must succeed, or the `Not applicable` evidence must hold. Only after every other applicable automated AT and routing outcome passes does the baseline get installed and verified separately from the same scratch render as the final adoption step.

Every audit-emitted AT carries exactly one source axis and one explicit timing axis. Current audit ATs use `[Automated] [Pre-release gate]` or `[Manual] [Pre-release gate]`; none defer verification until after release.

Pass `--json` to print a machine-readable report (`header`: invocation, canon SHA, target, flavor and its source, repo name, govna/code-stack versions from metadata; `files`: one entry per scanned file with its classification, diff, prior commits, matched preserve-registry entries, legacy preserve-phrase evidence, canon reference, and mixed-content boundary where applicable; `emitted`: the stub's path for actionable reports or `null` for clean reports).
