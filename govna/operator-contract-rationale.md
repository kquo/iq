# Operator Contract Rationale

This explanatory document records why the Operator contract exists. `AGENTS.md` alone defines operational rules and wins every conflict.

## Session-Entry Purpose

Session Entry tells a general-purpose agent that constrained repository rules apply before substantive work. It initializes contract identity, substantive-action scope, gates, precedence, and an observable checkpoint without restating the full contract. Audit catches residual drift.

## LLM-Agent Behavior Assumptions

The design assumes similarity-weighted retrieval, stronger compliance with imperative wording, and a modest primacy benefit for role framing. `AGENTS.md` therefore stays imperative and near the action; this document serves human onboarding without diluting that signal.

## The `Govna contract loaded.` Checkpoint

`Govna contract loaded.` is a human-visible readiness signal emitted only after internalizing `AGENTS.md` and before the first substantive governed action. Its narrow trigger keeps it meaningful; it detects rather than prevents contract skipping.

## Audit Verification

`govna audit` complements session framing by detecting canon incoherence, consumer adoption drift, and local-rule decay across sessions and repositories.

## Why Effective Implementation Scope Is Bounded

Effective implementation scope avoids repeating settled Director decisions for directly broken, deterministic fallout. It preserves behavior and intent, requires one valid outcome, records every use, and returns to Refine wherever product, scope, security, destructive, publication, release, dependency, migration, architecture, or competing-outcome judgment begins.

## Why Contract Integrity Reporting Is Evidence-Triggered

Evidence-triggered reporting distinguishes contract defects from implementation defects without inviting ambient opinion. Classification routes consumer-local, canon, or unclear findings but never grants editing authority. Blocking findings stop unsafe or decision-bearing work; unchanged acknowledged findings stay silent; authorized corrections land only in their owning governance document.

## Why Contract Growth Is Reviewed

Contract-growth review applies only to proposed or authorized governance changes. Measurements trigger inspection, not findings. Atomicity reduces dropped qualifiers; hierarchy and shared invariants reduce whole-contract dilution. Consumer evidence routes shared defects upstream without granting editing authority.

## Why Implement Can Close Bounded Completeness Gaps

Implement and its closure audit can expose a missed path inside an already settled outcome. Requiring a new Director phase instruction for that deterministic omission adds ceremony without adding a decision. The bounded exception therefore continues the original Implement authority only when repository evidence identifies the gap, an active acceptance test already requires the correction class, the artifact family is already named, and only one materially valid outcome exists. Pre-Implementation Verification protects the corrected AC, reporting keeps the transition visible, and the three-round limit returns repeated or decision-bearing churn to the Director.

## Canon Versus Local Flexibility

Canon fixes shared roles, workflow, approvals, discipline, and review behavior. Consumers own non-conflicting `## Project Rules`, additional local governance documents, tooling, build scripts, and CI. Propose disputed canon upstream instead of creating permanent local drift.
