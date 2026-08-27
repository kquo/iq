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
- Start a cycle only when the director identifies the active AC and explicitly
  requests Audit.
- Use an unnumbered phase instruction when one AC is under `govna/`.
- Require the AC number when multiple ACs are present.
- Pause after each lifecycle action until the director explicitly advances the active AC.

## Required Artifacts

- `AGENTS.md`
- `README.md`
- `arch.md`
- `plan.md`
- `govna/`

## Cycle

1. **Draft.** Write the authorized AC from `govna/ac-template.md`.
2. **Audit.** Review the AC for missing scope, unsafe assumptions, and untestable requirements without editing it.
3. **Refine.** Update the AC with settled findings and Director decisions.
4. **Implement.**
   - Deliver the settled scope.
   - Test the settled scope.
   - Verify the settled scope.
   - Correct implementation defects.
   - Map every scoped path and test in the final read-only closure audit.
5. **Ratify.**
   - Perform the Director-triggered final review.
   - Apply bounded correction behavior.
6. **Package.** Run `govna/build-release.md` release preparation only after separate Director authorization.

Apply the complete phase, scope, correction, contract-integrity, and advancement rules in `AGENTS.md` throughout this cycle.

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
