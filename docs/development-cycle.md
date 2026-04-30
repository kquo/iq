# Development Cycle

This repo uses an acceptance-criteria-first workflow.

## Required Artifacts

- `AGENTS.md`
- `README.md`
- `arch.md`
- `plan.md`
- `docs/`

## Cycle

1. **Choose the next approved item.** Origination is either (a) an `Ideas To Explore` entry promoted after the director rubric-clears it, or (b) director-originated work (governance, adoption, hotfix, refinement). ACs are the single execution surface — draft directly when authorized.
2. **Draft an acceptance-criteria doc.** Start from `docs/ac-template.md` (see preamble for the monotonic-numbering rule); save as `docs/ac<N>-<slug>.md`.
3. **Review and tighten scope before implementation.** When the director provides critique findings on the AC, the Operator transcribes them into the AC's `## Critique` section and addresses them, but does not treat the AC as implementation-ready until the director explicitly confirms. See `docs/critique-protocol.md` for the full critique-round protocol.
4. **Implement code, tests, and direct doc updates together.**
5. **Capture decisions in durable docs or code when the AC is complete.** The AC file is removed during release prep (see `docs/build-release.md` Pre-Release Checklist).
6. **Run the build and validation flow.** See `docs/build-release.md`.
7. **Perform release work only when explicitly requested.**

## Notes

- keep roadmap decisions in `plan.md`
- keep architecture changes in `arch.md`
- keep repo-level governance in `AGENTS.md`
- record follow-on ideas in `plan.md` under `Ideas To Explore` with an `IE<N>:` prefix (pre-rubric idea or pointer to a drafted AC stub)
- remove IE entries when the underlying idea is closed — rejected, retired, or (for AC pointers) the pointed-to AC has shipped
- write AC docs to file (`docs/ac<N>-<slug>.md`); summarize in the response but do not dump full AC content into conversation
- promotion path: shape (a) IE → discussion → objective-fit rubric (see `AGENTS.md` Approval Boundaries) → AC drafted (IE converts to shape (b) pointer, same `IE<N>` number) → AC ships (IE removed)
- stub ACs — ACs that carry `TBD — requires scoping before critique gate` in their Out Of Scope and Acceptance Tests sections until scoped — are permitted; flagged in Implementation Notes with `Rudimentary stub — requires further scoping before critique gate or implementation authorization.` and remain `PENDING` until a scoping pass runs and the critique gate activates
