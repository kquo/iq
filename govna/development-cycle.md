# Development Cycle

This repo uses an acceptance-criteria-first workflow.

The lifecycle makes recurring programming checkpoints and their settled context reusable across phases and sessions. This reduces process reconstruction and avoidable rework without weakening authorization, review, verification, or release gates.

## AC Workflow

- Follow the lifecycle `Draft → Audit → Refine → Implement → Ratify → Package`.
- Treat standalone `Draft` or `draft` as the Director-authorized pre-cycle action that creates the active AC.
- Keep Draft outside the AC phases.
- Treat standalone `Audit` or `audit` as the adversarial-review phase action that starts the active AC cycle.
- Treat standalone `Refine` or `refine` as the scope-and-decision-resolution phase action.
- Treat standalone `Implement` or `implement` as the implementation-and-verification phase action.
- Treat standalone `Ratify` or `ratify` as the Director acceptance action.
- Initiate the final review on that action.
- Complete Ratify when that review is clean.
- Treat standalone `Package`, `package`, `pack`, and `prep` as equivalent post-Ratify release-preparation actions.
- Do not infer Package from Ratify acceptance.
- Start an AC cycle only after the Director identifies the AC and authorizes Audit, integrated audit adoption identifies the emitted AC, or a completed Draft identifies the active AC.
- Apply an unnumbered Audit, Refine, Implement, or Ratify instruction when exactly one AC can enter the requested phase.
- Require the AC number when multiple ACs can enter the requested phase.
- Ask the Director for the AC number and last completed lifecycle action when phase eligibility cannot be established.
- Define the pending release batch as every unpackaged AC whose implementation is present in the unreleased repository state.
- Include an implemented AC in the pending release batch while it awaits Ratify.
- Measure the projected complete pending release batch with one private provisional prefix-plus-summary string before another AC enters Implement.
- Use the provisional string only for the 80-byte fit check.
- Discard the provisional string after the fit check.
- Start another Implement only when the projected complete pending release batch can fit one compliant release message.
- Require Package for the current fitting batch before another Implement when the projection cannot fit.
- Require every pending release-batch member to complete Ratify before Package.
- Treat one active Ratified AC as an established one-AC release batch only when it is the complete pending release batch.
- Treat only a Director-named complete pending release batch as an established multi-AC release batch.
- Accept only `Package` followed by a plus-joined list of uppercase `AC<number>` references as the named-batch Package form.
- Apply standalone `Package`, `package`, `pack`, or `prep` to the established Ratified release batch.
- Ask the Director to name the release batch when multiple ungrouped Ratified ACs can enter Package.
- Reject a named release batch that contains a non-Ratified AC.
- Recheck the complete pending release batch and exact release message before Package runs prep.
- Reject an oversized or partial release batch.
- Prohibit automatic release-batch splitting.
- Enter integrated Audit only when `govna audit` emits or reuses one guarded adoption AC.
- Keep a clean audit result or pre-emission failure outside the AC phases.
- Resume integrated Refine after the Director resolves every blocking finding and decision.
- Stop integrated audit adoption before Implement.
- Pause after each lifecycle action unless integrated audit adoption, completed-Draft automatic Audit entry, or eligible automatic Refine entry authorizes the immediate next action.

## Required Artifacts

- `AGENTS.md`
- `README.md`
- `arch.md`
- `plan.md`
- `govna/`

## Cycle

1. **Draft.** Write the authorized AC from `govna/ac-template.md`.
2. **Audit.**
   - Review the AC for missing scope, unsafe assumptions, and untestable requirements without editing it.
   - Start this review immediately when an explicit agent-mediated `govna audit` request emits or reuses one guarded adoption AC.
3. **Refine.**
   - Update a hand-authored AC with settled findings and Director decisions.
   - Keep an audit-emitted AC unchanged.
   - Record its resolved decisions in the active session.
4. **Implement.**
   - Deliver the settled scope.
   - Test the settled scope.
   - Verify the settled scope.
   - Correct implementation defects.
   - Map every scoped path and test in the final read-only closure audit.
5. **Ratify.**
   - Perform the Director-triggered final review.
   - Reuse the Implement evidence snapshot when `AGENTS.md` defines it as current.
   - Revalidate affected evidence when `AGENTS.md` defines it as missing or stale.
   - Apply bounded correction behavior.
6. **Package.** Run `govna/build-release.md` release preparation for the established Ratified release batch only after separate Director authorization.

### Implement Evidence Snapshot

- Define the Implement evidence snapshot as the session-only identity record used to prove that validation evidence remains unchanged.
- Record the active AC content identity and acceptance-requirement identity.
- Record the primary repository's HEAD, index, tracked-worktree, untracked-path, and untracked-content identities.
- Record each relevant ignored-path and external-input identity.
- Use exact values or deterministic content digests for every recorded identity.
- Record exact validation commands, parameters, working directories, and relevant environment or configuration inputs.
- Record every validation result and acceptance-test disposition without changing its status.
- Record each resolved tool path, executable identity, tool version, and canon identity.
- Use the complete primary-repository state as the default dependency boundary.
- Narrow the dependency boundary only when repository evidence proves that a reused check cannot read the excluded state.
- Treat the snapshot as incomplete when a relevant ignored or external input cannot be identified.

Apply the complete phase, scope, correction, contract-integrity, and advancement rules in `AGENTS.md` throughout this cycle.

The `govna` executable ends after deterministic audit comparison and emission. The Operator performs the integrated Audit, Refine, and Pre-Implementation Verification steps. A required change to an immutable emitted AC needs a new audit emission. The pre-Implement fit check uses one private provisional string only to prevent an oversized pending batch. Package requires every implemented batch member to be Ratified, compares the complete pending batch with the exact message, and rejects partial or oversized batches before prep.

During Director-authorized Implement, a bounded completeness correction fixes a missed path or instruction when the active AC already settles the required result. The Operator may complete at most three correction rounds within the existing artifact family. Each round updates the AC in Refine, reruns the final AC wording and scope check called Pre-Implementation Verification, and returns to Implement. A Director-owned decision or fourth round pauses for the Director.

A completed Draft flows into Audit, and a clean Audit flows into Refine, because those actions mutate nothing except the active AC document and stay cheap to redo before implementation. Automatic Refine entry requires every finding to be advancement-eligible: outside every Director-owned category with exactly one materially valid correction. Refine-to-Implement, Ratify, and Package stay Director-gated because they mutate the repository, accept work, or prepare a release.

## Notes

- Keep roadmap decisions and follow-on `IE<N>:` items in `plan.md`.
- Keep architecture in `arch.md`.
- Keep repo governance in `AGENTS.md`.
- Remove an IE when rejected, retired, or shipped through its AC pointer.
- Keep ACs in `govna/ac<N>-<slug>.md`.
- Summarize ACs rather than reproduce them in chat.
- Mark an unscoped stub in `## Summary`.
- Keep an unscoped stub's scope and tests TBD.
- Leave an unscoped stub `PENDING` until scoped.
