# Docs

Use this directory for the documents that explain how work is planned, reviewed, implemented, and released in this repository.

Read [`operator-contract-rationale.md`](operator-contract-rationale.md) to understand how the contract makes programming and publishing ceremonies more effective and efficient, including why the session-entry rules work as they do. Read [`audit.md`](audit.md) to understand how `govna audit` compares repository files with Govna's embedded governance files and writes an AC for review.

See [`code-stacks.md`](code-stacks.md) for the first-class Go, Rust, Terraform, and Swift CODE stack contracts.

Recommended contents to add:

- `ac<N>-<slug>.md` for a numbered, testable change contract; see `ac-template.md`
- `development-cycle.md` for the Draft-through-Package workflow
- `build-release.md` for build, test, installation, and release rules
- reference notes for durable implementation decisions

Keep these docs aligned with the repo's actual workflow and architecture.
