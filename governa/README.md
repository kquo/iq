# Docs

Use this directory for governed implementation support documents.

Governa ships [`operator-contract-rationale.md`](operator-contract-rationale.md) here — read it to understand the session-entry contract that governs how agents operate in this repo. [`drift-scan.md`](drift-scan.md) describes the consumer-run `governa drift-scan` flow and the emitted AC stub.

Recommended contents to add:

- `ac<N>-<slug>.md` for acceptance criteria (sequential N, kebab-case slug; see `ac-template.md`)
- `development-cycle.md` for repo workflow rules
- `build-release.md` for build, test, and release rules
- reference notes that support implementation decisions

Keep these docs aligned with the repo's actual workflow and architecture.
