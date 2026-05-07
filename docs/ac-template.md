Copy this file to `docs/ac<N>-<slug>.md`, where `slug` is a kebab-case identifier and `N` follows the monotonic-numbering rule below. Set the file's heading to `# AC<N> Title`.

AC numbering is monotonic across release-prep deletions. Determine `N` by taking the maximum of (a) AC numbers currently in `docs/` and (b) AC numbers anywhere in `git log --all --pretty=%B` output (which covers commit subject + body — count every AC reference on every line, even when a single commit names multiple, e.g., `AC53+AC54`). Prior ACs removed during release prep still count. `N` is that maximum plus one.

The AC is the implementation contract for one approved roadmap item. The full development cycle that wraps around this template lives in `docs/development-cycle.md`. The enforceable rules around when to draft, review, and integrate an AC live in `AGENTS.md`.

## Companion Artifacts

Critique lives inside the AC itself, not in a companion file. The AC carries a `## Critique` section (typically above `## Status`); Director findings per round land there. The Operator transcribes the Director's conversational findings; the Director may also edit the AC file directly. The Operator's responses land as AC revisions + `### Disposition Log` entries under `## Implementation Notes`. See `docs/critique-protocol.md` for round-append structure (`### Round N` / `#### F<N>`), `F-new-N` monotonic numbering across subsequent rounds, and the four-field terminator shape. Delete this `## Companion Artifacts` section when copying the template into a real AC.

# AC<N> Title

One-paragraph summary of what this AC delivers and why. State the change in plain terms — feature, refactor, infrastructure, or doc. Note whether it is code or doc-only.

## Summary

Describe the change in one short paragraph.

## Objective Fit

1. **Outcome.** What this delivers, in one sentence.
2. **Priority.** Why this over higher-priority work. If it's an intentional pivot, name the trade-off.
3. **Dependencies.** Prior ACs or decisions this builds on or contradicts. State explicit contradictions and why they're intentional.

## In Scope

List the concrete changes this AC will make. Use sub-headings for grouping (e.g. "Files to create", "Files to modify", "Schema changes"). Be specific — file paths, function names, table columns. The In Scope list is the authoritative scope contract.

### Files to create

- `path/to/new_file` — what it contains
- `docs/new-doc.md` — what it documents

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

## Implementation Notes

(Optional but encouraged.) Notes on approach, gotchas, edge cases, or design decisions that the implementer needs to know but that don't belong in the spec proper. If a particular approach was already considered and rejected, capture it here.

## Acceptance Tests

Every AT must be labeled `[Automated]` or `[Manual]`:

- **Automated** — The result can be verified from CLI output, test assertions, or file inspection. Automated ATs are run during implementation and re-run as part of the pre-release checklist.
- **Manual** — Requires a live end-to-end action and must be confirmed by the user. The agent cannot self-verify these.

Default to Automated whenever the result is verifiable without a live external service. Manual ATs add friction to the release flow, so reserve them for behaviors that genuinely cannot be checked any other way.

Source axis (`[Automated]` / `[Manual]`) names who verifies. Timing axis (`[Pre-release gate]` / `[Post-release verification]`) names when verification happens. `[Pre-release gate]` is the default and may be omitted; `[Post-release verification]` is explicit. Use `[Post-release verification]` only when automated regression coverage already gates pre-release on the underlying class. The label communicates that the AT is a confidence check, not a gate, so future Operators do not promote it back into a gate.

**AT1** [Automated] — One-line description of what is verified, with the exact check (file existence, grep pattern, SQL query, or CLI output).

**AT2** [Automated] — ...

**AT3** [Manual] — One-line description plus the live action the user must take to confirm the result.

## Documentation Updates

List the docs that must be updated as part of this AC. If a change touches code that has corresponding documentation, update those docs in the same pass.

- `arch.md` — what section
- `README.md` — what section
- `CHANGELOG.md` — the release row is added at release prep time, not during implementation (the file itself is created by `governa apply` as a stub)

## Director Review

This section lists trade-offs the Director still needs to decide. **Each entry must be numbered (`1.`, `2.`, …) and lead with a literal question ending in `?`** so the Director can reference entries inline ("Regarding #1, …"). If you cannot phrase the entry as an open question awaiting the Director's answer, it does not belong here. Send mechanical computations and showing-work notes to Implementation Notes; settled decisions go inline with `Director-set` attribution; choices covered by repo precedent are not surfaced at all. After the question, state the Operator's lean and a one-line why. Once the Director decides (in conversation or by editing the AC), move the item out of this section and attribute it inline (Summary, In Scope/Out of Scope, Implementation Notes) with a `Director-set` parenthetical. Write `None` when nothing is open.

The Director resolves these during critique rounds — in conversation or by editing the AC directly.

1. Should we do X or Y? Operator leans X (alternative: Y). Why: <one-line>.

## Status

`PENDING` — awaiting user authorization to begin implementation.

(Other valid states: `IN PROGRESS`, `DEFERRED` (with reason and tracking ref). For partial completion, list status by phase. Completed ACs are deleted at release time per the development cycle — do not change status to DONE before deletion.)
