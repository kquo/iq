# AC Critique Protocol

Formalizes the AC/critique loop referenced in `AGENTS.md` Approval Boundaries. Critique findings live inside the AC file in a `## Critique` section — there is no separate companion file. This doc codifies what that section contains across rounds.

## Where findings live

Every AC carries a `## Critique` section (typically the second-to-last top-level section, above `## Status`). The Director provides findings as free-form text in conversation. The Operator transcribes them into this section. The Director may also edit the AC file directly at their discretion. Ownership is Operator-default, Director-override.

The Operator's response to each finding is visible as AC revisions (via `git log`/`git diff`) plus entries in the `### Disposition Log` subsection under `## Implementation Notes`.

## Round structure — append-only

Each critique round adds a new `### Round N` H3 heading inside the `## Critique` section (round 1 is the initial critique; rounds 2+ are verification passes). Round sections are never edited retroactively; new information goes in a new round.

The Director can push a new critique round at any time, for any reason. The Operator must not treat any round as final unless the Director says so.

## Finding heading level

Each finding is an `#### F<N>` H4 heading with a severity label. Example:

    #### F1 [Blocker] — governance-model.md already carries this convention

Round 1 findings use labels `F1`, `F2`, …. All rounds after round 1 use `F-new-N` labels numbered **monotonically across all subsequent rounds** (e.g., Round 2 uses `F-new-1`, `F-new-2`; if Round 3 introduces a new finding it is `F-new-3`, not a fresh `F-new-1`). This keeps labels globally unique across the AC's critique history and makes cross-references unambiguous.

Heading-level note: the integrated-mode levels are `## Critique` (H2) → `### Round N` (H3) → `#### F<N>` (H4). The extra depth comes from embedding rounds inside the AC rather than using a separate file.

## Authoring mechanism

The Director provides findings as free-form text in conversation. The Operator transcribes the Director's findings into the AC's `## Critique` section, preserving the Director's intent. The Director may also edit the AC file directly when they prefer — there is no restriction on who writes to the file.

## Terminator shape — four fields in order

The final round of a critique cycle writes `### Round N Summary` with exactly these fields in order:

1. **Unresolved findings** — list by severity: blocker / major / minor / nit. Empty allowed.
2. **Residual risks accepted** — items flagged but chosen not to block on. Empty allowed.
3. **Coverage** — what was reviewed this round and what was explicitly skipped. Prevents false confidence.
4. **Verdict** — `no blockers` or `blockers present`.

## Operator's cross-references in the AC

The Operator maintains two sections that pair with the `## Critique` section:

- **`### Disposition Log`** (H3 subsection under `## Implementation Notes`) — cross-references each finding by label and names the resulting AC change. Required for ACs with extensive critique rounds; optional otherwise (`git log` on the AC file carries the same info for short cycles).
- **`## Director Review`** (top-level, between `## Documentation Updates` and `## Status`) — lists every viable-options trade-off chosen during the cycle (not just ones the Operator feels uncertain about).

## Lifecycle

The `## Critique` section is part of the AC file itself. The entire AC (including its critique history) is deleted at release prep alongside other per-AC artifacts.

## Termination

The Director reviews the AC after the verdict `no blockers` lands. The Director may redirect (new round opens as `Round N+1`), accept with changes, or authorize implementation.
