Copy this file to `govna/ac<N>-<slug>.md`.
Use a kebab-case slug and `# AC<N> Title` heading.

Set `N` to one above the highest AC number in `govna/` or `git log --all --pretty=%B`.
Count every reference because release-prep deletions do not reset numbering.

The AC is the implementation contract for one approved roadmap item. The full development cycle that wraps around this template lives in `govna/development-cycle.md`. The enforceable rules around when to draft, review, and integrate an AC live in `AGENTS.md`.

# AC<N> Title

## Summary

Describe the change, its nature, code or doc impact, and any named parts in one short paragraph.

## In Scope

List concrete changes and exact paths under useful groupings.
Treat this list as authoritative.
Apply only the effective-scope and emitted-routing exceptions defined in `AGENTS.md`.

### Files to create

- `path/to/new_file` — what it contains
- `govna/new-doc.md` — what it documents

### Files to modify

- `existing_file` — what changes
- `arch.md` — what gets updated

### Schema changes

(If any. Include the new schema version and the migration step.)

## Out Of Scope

List tempting or adjacent work that remains excluded.

- Things deferred to a later AC (link the deferral)
- Adjacent improvements that would be tempting but are not required
- Things that look in scope but aren't (called out to prevent confusion)

## Migration findings

- Record each `migration-required` item emitted by audit under `## In Scope`.
- State the explicit consumer action that completes each migration item.
- Keep automatic migration or deletion out of scope unless this AC explicitly authorizes it.

## Acceptance Tests

Label every AT `[Automated]` or `[Manual]` and `[Pre-release gate]` or `[Post-release verification]`.
Prefer automated pre-release coverage.
Use post-release only when automated regression coverage already gates the behavior class.

**AT1** [Automated] [Pre-release gate] — One-line description of what is verified, with the exact check (file existence, grep pattern, SQL query, or CLI output).

**AT2** [Automated] [Pre-release gate] — ...

**AT3** [Manual] [Pre-release gate] — One-line description plus the live action the user must take to confirm the result.

## Status

`PENDING` — awaiting user authorization to begin implementation.

(Other valid states: `IN PROGRESS`, `DEFERRED` (with reason and tracking ref). For partial completion, list status by phase. Completed ACs are deleted at release time per the development cycle — do not change status to DONE before deletion.)
