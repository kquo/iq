# Operator Contract Rationale

This explanatory document records why the Operator contract exists. `AGENTS.md` alone defines operational rules and wins every conflict.

## Contract Purpose

Govna exists to make programming and publishing ceremonies—the recurring CODE and DOC checkpoints around intent, authorization, scope, review, implementation or editing, verification, and release—more effective and efficient. Reusable context reduces process reconstruction, ambiguity, duplicated decisions, and avoidable rework across phases and sessions.

Efficiency does not weaken authorization, review, verification, or release gates. Govna keeps decision-bearing choices with the Director and makes only settled, deterministic mechanics reusable.

## Session-Entry Purpose

Session Entry tells a general-purpose agent that constrained repository rules apply before substantive work. It initializes contract identity, substantive-action scope, gates, precedence, and an observable checkpoint without restating the full contract. Audit catches residual drift.

## LLM-Agent Behavior Assumptions

The design assumes similarity-weighted retrieval, stronger compliance with imperative wording, and a modest primacy benefit for role framing. `AGENTS.md` therefore stays imperative and near the action; this document serves human onboarding without diluting that signal.

## Why Plain Language Matters

Concrete wording helps both people and agents identify the required result before interpreting a workflow label. Govna keeps exact labels when they carry contract meaning, but explains each one at first use so the label does not hide the problem, effect, or decision.

## The `Govna contract loaded.` Checkpoint

`Govna contract loaded.` is a human-visible readiness signal emitted only after internalizing `AGENTS.md` and before the first substantive governed action. Its narrow trigger keeps it meaningful; it detects rather than prevents contract skipping.

## Audit Verification

`govna audit` complements session framing by detecting canon incoherence, consumer adoption drift, and local-rule decay across sessions and repositories.

## Why Effective Implementation Scope Is Bounded

Effective implementation scope permits one directly broken supporting file with only one valid correction to be fixed without repeating a decision the Director already settled. It preserves behavior and intent, records every use, and returns to Refine wherever product, scope, security, destructive, publication, release, dependency, migration, architecture, or competing-outcome judgment begins.

## Why Contract Integrity Reporting Is Evidence-Triggered

A contract-integrity finding reports a proven governance-rule problem rather than an implementation bug. Evidence keeps this process from turning wording preferences into findings. Classification routes repository-specific, shared Govna, or unclear findings but never grants editing authority. Blocking findings stop unsafe or decision-bearing work; unchanged acknowledged findings stay silent; authorized corrections land only in their owning governance document.

## Why Contract Growth Is Reviewed

A contract-growth review checks whether new rules duplicate, hide, misplace, or crowd out existing rules. It applies only to proposed or authorized governance changes. Measurements trigger inspection, not findings. Repository evidence routes shared defects upstream without granting editing authority.

## Why Implement Can Close Bounded Completeness Gaps

Implement and its final read-only closure audit can expose a missed path inside an already settled outcome. Requiring another Director instruction for that omission adds a step without adding a decision. A bounded completeness correction continues the original Implement authority only when repository evidence identifies the gap, an active acceptance test already requires the correction, the artifact family is already named, and only one materially valid outcome exists. The Operator may correct at most three missed paths or instructions before asking the Director again. The final AC wording and scope check called Pre-Implementation Verification protects each corrected AC, and visible reporting records every transition.

## Why Clean Ratify Reuses Implement Evidence

Ratify is the Director-triggered final acceptance review, not a request to reconstruct proof that is still current. Implement therefore closes with one session-only evidence snapshot covering repository state, validation inputs, tools, results, and acceptance-test dispositions. Cheap state and identity checks can prove that snapshot unchanged without another render, build, or test. The complete repository is the safe default boundary, so recording the snapshot does not require a per-test dependency inventory.

Missing, incomplete, stale, or uncertain evidence still requires applicable revalidation. An inline correction also invalidates the evidence it affects. Evidence freshness never upgrades a failed, pending, manual, or unexercised disposition, and clean reuse never replaces Ratify's final review or contract-integrity check.

## Canon Versus Local Flexibility

Canon fixes shared roles, workflow, approvals, discipline, and review behavior. Consumers own non-conflicting `## Project Rules`, additional local governance documents, tooling, build scripts, and CI. Propose disputed canon upstream instead of creating permanent local drift.
