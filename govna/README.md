# Docs

Use this directory for governed implementation support documents.

govna ships [`operator-contract-rationale.md`](operator-contract-rationale.md) here — read it to understand the session-entry contract that governs how agents operate in this repo. [`audit.md`](audit.md) documents the `govna audit` command and its emitted AC stub.

See [`code-stacks.md`](code-stacks.md) for the first-class Go, Rust, Terraform, and Swift CODE stack contracts.

Recommended contents to add:

- `ac<N>-<slug>.md` for acceptance criteria (sequential N, kebab-case slug; see `ac-template.md`)
- `development-cycle.md` for repo workflow rules
- `build-release.md` for build, test, and release rules
- reference notes that support implementation decisions

Keep these docs aligned with the repo's actual workflow and architecture.
