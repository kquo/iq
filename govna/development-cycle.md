# Development Cycle

This repo uses an acceptance-criteria-first workflow.

## AC Workflow

- Follow the lifecycle `Draft → Audit → Refine → Implement → Ratify → Package`.
- Treat standalone `Draft` or `draft` as the Director-authorized pre-cycle action that creates the active AC; Draft is not an AC phase.
- Treat standalone `Audit` or `audit` as the adversarial-review phase action that starts the active AC cycle.
- Treat standalone `Refine` or `refine` as the scope-and-decision-resolution phase action.
- Treat standalone `Implement` or `implement` as the implementation-and-verification phase action.
- Treat standalone `Ratify` or `ratify` as the Director acceptance action that initiates the final review and completes Ratify when that review is clean.
- Treat standalone `Package`, `package`, `pack`, and `prep` as equivalent post-Ratify release-preparation actions; do not infer Package from Ratify acceptance.
- Start a cycle only when the director identifies the active AC and explicitly
  requests Audit.
- Use an unnumbered phase instruction when one AC is under `govna/`; require
  the AC number when multiple ACs are present.
- Pause after each lifecycle action until the director explicitly advances the active AC.

## Required Artifacts

- `AGENTS.md`
- `README.md`
- `arch.md`
- `plan.md`
- `govna/`

## Cycle

1. **Choose the next approved item.** Origination is either (a) an `Ideas To Explore` entry promoted after the director rubric-clears it, or (b) director-originated work (governance, adoption, hotfix, refinement). ACs are the single execution surface — draft directly when authorized.
2. **Draft an acceptance-criteria doc.** Start from `govna/ac-template.md` (see preamble for the monotonic-numbering rule); save as `govna/ac<N>-<slug>.md`.
3. **Audit the draft.** Challenge the AC, repository behavior, referenced documentation, scope, edge cases, omissions, and testability without editing the AC or repository. Pause for explicit Director advancement to Refine.
4. **Refine the AC.** Resolve Audit findings and settle Director decisions by editing the AC. Pause when a decision remains unresolved; treat the AC as implementation-ready only after explicit Director confirmation to Implement.
5. **Implement the settled scope.** Implement code, tests, and direct doc updates together; run validation, adversarial verification, defect correction, and the closure audit. Pause for Ratify.
6. **Ratify the delivered AC.** Treat standalone Ratify as acceptance, complete it in the same turn after a clean review, and request no second acceptance signal. Return contract or scope feedback to Refine and implementation-only feedback to Implement without completing Ratify.
7. **Perform Package only when explicitly requested.** Accept standalone `Package`, `package`, `pack`, or `prep` only after Ratify acceptance; follow `govna/build-release.md` for the existing checklist and implementation.

## Notes

- keep roadmap decisions in `plan.md`
- keep architecture changes in `arch.md`
- keep repo-level governance in `AGENTS.md`
- record follow-on ideas in `plan.md` under `Ideas To Explore` with an `IE<N>:` prefix (pre-rubric idea or pointer to a drafted AC stub)
- remove IE entries when the underlying idea is closed — rejected, retired, or (for AC-pointers) the pointed-to AC has shipped
- write AC docs to file (`govna/ac<N>-<slug>.md`); summarize in the response but do not dump full AC content into conversation
- promotion path: pre-rubric IE → discussion → objective-fit rubric (see `AGENTS.md` Approval Boundaries) → AC drafted (IE converts to AC-pointer, same `IE<N>` number) → AC ships (IE removed)
- stub ACs — ACs that carry `TBD — requires scoping before critique gate` in their Out Of Scope and Acceptance Tests sections until scoped — are permitted; flagged in `## Summary` with `Rudimentary stub — requires further scoping before critique gate or implementation authorization.` and remain `PENDING` until a scoping pass runs and the critique gate activates
