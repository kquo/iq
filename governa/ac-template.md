Copy this file to `governa/ac<N>-<slug>.md`, where `slug` is a kebab-case identifier and `N` follows the monotonic-numbering rule below. Set the file's heading to `# AC<N> Title`.

AC numbering is monotonic across release-prep deletions. Determine `N` by taking the maximum of (a) AC numbers currently in `governa/` and (b) AC numbers anywhere in `git log --all --pretty=%B` output (which covers commit subject + body — count every AC reference on every line, even when a single commit names multiple, e.g., `AC53+AC54`). Prior ACs removed during release prep still count. `N` is that maximum plus one.

The AC is the implementation contract for one approved roadmap item. The full development cycle that wraps around this template lives in `governa/development-cycle.md`. The enforceable rules around when to draft, review, and integrate an AC live in `AGENTS.md`.

# AC<N> Title

## Summary

Describe the change in one short paragraph. State the nature (feature, refactor, infrastructure, doc) and note whether the work is code or doc-only. For multi-part ACs, name the parts (e.g. "Part A rewrites X; Part B amends Y; Part C propagates to overlays.").

## In Scope

List the concrete changes this AC will make. Use sub-headings for grouping (e.g. "Files to create", "Files to modify", "Schema changes"). Be specific — file paths, function names, table columns. The In Scope list is the authoritative scope contract; the agent only edits files listed here even after the Director authorizes implementation.

### Files to create

- `path/to/new_file` — what it contains
- `governa/new-doc.md` — what it documents

### Files to modify

- `existing_file` — what changes
- `arch.md` — what gets updated

### Schema changes

(If any. Include the new schema version and the migration step.)

## Out Of Scope

List things the AC explicitly does **not** do. This is as important as the In Scope list — it bounds the change and prevents scope creep during implementation.

- Things deferred to a later AC (link the deferral)
- Adjacent improvements that would be tempting but are not required
- Things that look in scope but aren't (called out to prevent confusion)

## Acceptance Tests

Every AT carries a source axis and a timing axis.

- **Source axis** — `[Automated]` or `[Manual]`. Default to `[Automated]` whenever the result is verifiable without a live external service; reserve `[Manual]` for behaviors that genuinely cannot be checked any other way.
- **Timing axis** — `[Pre-release gate]` (default; may be omitted) or `[Post-release verification]` (explicit). Use `[Post-release verification]` only when automated regression coverage already gates pre-release on the underlying class.

**AT1** [Automated] — One-line description of what is verified, with the exact check (file existence, grep pattern, SQL query, or CLI output).

**AT2** [Automated] — ...

**AT3** [Manual] — One-line description plus the live action the user must take to confirm the result.

## Status

`PENDING` — awaiting user authorization to begin implementation.

(Other valid states: `IN PROGRESS`, `DEFERRED` (with reason and tracking ref). For partial completion, list status by phase. Completed ACs are deleted at release time per the development cycle — do not change status to DONE before deletion.)
