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
- Start an AC cycle only after the Director identifies the AC and authorizes Audit or integrated audit adoption identifies the emitted AC.
- Apply an unnumbered Audit, Refine, Implement, or Ratify instruction when exactly one AC can enter the requested phase.
- Require the AC number when multiple ACs can enter the requested phase.
- Ask the Director for the AC number and last completed lifecycle action when phase eligibility cannot be established.
- Treat one active Ratified AC as an established one-AC release batch.
- Treat only a Director-named set of Ratified ACs as an established multi-AC release batch.
- Accept only `Package` followed by a plus-joined list of uppercase `AC<number>` references as the named-batch Package form.
- Apply standalone `Package`, `package`, `pack`, or `prep` to the established Ratified release batch.
- Ask the Director to name the release batch when multiple ungrouped Ratified ACs can enter Package.
- Reject a named release batch that contains a non-Ratified AC.
- Enter integrated Audit only when `govna audit` emits or reuses one guarded adoption AC.
- Keep a clean audit result or pre-emission failure outside the AC phases.
- Resume integrated Refine after the Director resolves every blocking finding and decision.
- Stop integrated audit adoption before Implement.
- Pause after each lifecycle action unless integrated audit adoption authorizes immediate Refine.

## Required Artifacts

- `AGENTS.md`
- `README.md`
- `arch.md`
- `plan.md`
- `govna/`

## Cycle

1. **Draft.** Write the authorized AC from `govna/ac-template.md`.
2. **Audit.** Review the AC for missing scope, unsafe assumptions, and untestable requirements without editing it. Start this review immediately when an explicit agent-mediated `govna audit` request emits or reuses one guarded adoption AC.
3. **Refine.** Update a hand-authored AC with settled findings and Director decisions. Keep an audit-emitted AC unchanged and record its resolved decisions in the active session.
4. **Implement.**
   - Deliver the settled scope.
   - Test the settled scope.
   - Verify the settled scope.
   - Correct implementation defects.
   - Map every scoped path and test in the final read-only closure audit.
5. **Ratify.**
   - Perform the Director-triggered final review.
   - Apply bounded correction behavior.
6. **Package.** Run `govna/build-release.md` release preparation for the established Ratified release batch only after separate Director authorization.

Apply the complete phase, scope, correction, contract-integrity, and advancement rules in `AGENTS.md` throughout this cycle.

The `govna` executable ends after deterministic audit comparison and emission. The Operator performs the integrated Audit, Refine, and Pre-Implementation Verification steps. A required change to an immutable emitted AC needs a new audit emission. Package compares the release message's unique AC references with the established batch before prep.

During Director-authorized Implement, a bounded completeness correction fixes a missed path or instruction when the active AC already settles the required result. The Operator may complete at most three correction rounds within the existing artifact family. Each round updates the AC in Refine, reruns the final AC wording and scope check called Pre-Implementation Verification, and returns to Implement. A Director-owned decision or fourth round pauses for the Director.

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
