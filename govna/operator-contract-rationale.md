# Operator Contract Rationale

govna initializes AI coding-agent behavior through an explicit session-entry contract defined in [`AGENTS.md`](../AGENTS.md). This document explains the design reasoning behind that contract: why it exists, what it assumes about LLM behavior, what the `Govna contract loaded.` checkpoint is for, how audit keeps the contract honest, and where the boundary between govna canon and local project rules sits.

The rationale is explanatory only. Operational rules live in `AGENTS.md`; nothing here overrides or supplements them. When this document and `AGENTS.md` appear to conflict, `AGENTS.md` wins.

## Session-Entry Purpose

Coding agents bring general habits — preambles, autonomous commits, scope creep, ceremony narration — that work fine in unstructured projects but drift away from a constrained operating contract. On a newly adopted govna repo, agents have no way to know they have entered such a contract unless something tells them at session start.

The `### Session Entry` subsection of `AGENTS.md` is that signal. Six imperative bullets initialize an agent's behavior before substantive work begins: name the contract, define what counts as substantive, summarize the gate set, fix the conflict-resolution order, and require an observable readiness checkpoint. The subsection is short and austere by design; it does not restate the rules that live deeper in `AGENTS.md`, only the framing an agent needs at the moment of session entry.

The value of session-entry framing is reducing drift, not eliminating it. Even capable models will miss edge cases. Audit catches what slips through (see below).

## LLM-Agent Behavior Assumptions

The contract reflects an engineering judgment about how current frontier LLMs absorb instructions, not a hard guarantee. Three assumptions in particular shape the design:

- **Context retrieval is similarity-weighted, not top-down.** Models receive `AGENTS.md` as a block via the system-prompt region and attend to it based on the query at hand. Position bias inside that block is mild for content under ~10k tokens; a clearly labeled subsection is roughly as retrievable as a top-level section.
- **Imperative phrasing improves compliance.** Rules written as direct instructions ("Treat X as Y", "Skip Z") map cleanly to instruction-following. Descriptive prose, hedging, and explanatory paragraphs dilute the signal — which is why rationale lives in this document rather than in `AGENTS.md`.
- **Identity framings benefit slightly from primacy.** "You are the Operator" near the top of the contract gets marginally more weight than the same statement deeper in. The effect is small but real, which is why `### Session Entry` lives near the top of `## Interaction Mode` rather than buried elsewhere.

A note on the LLM-attention versus human-signal distinction: the rules in `AGENTS.md` are written for LLM compliance. The root-`README.md` pointer to this document is written for human onboarding. The two audiences are real, separate, and need different surfaces — conflating them by promoting rationale into `AGENTS.md` would harm LLM compliance, and conflating them by removing the human pointer would harm onboarding.

## The `Govna contract loaded.` Checkpoint

`### Session Entry` requires the agent to emit the literal string `Govna contract loaded.` before its first substantive govna-governed action of a session, and only after internalizing `AGENTS.md`. This is the operational version of the old BASIC `Ready.` prompt: a stated checkpoint that gates substantive work and creates a reviewable signal if the agent skips it.

Two design choices matter:

- **Gated emission, not ritual emission.** The agent must not state the line unless `AGENTS.md` has been internalized in the session. The integrity burden sits on the agent. A model that emits the line without internalizing the contract has violated the rule, and the Director can call that out in review.
- **Narrow trigger.** Emission triggers on the first substantive govna-governed action of a session — planning, editing, reviewing, command choice, or implementation work that touches the contract surface. Pure conversational answers and questions that do not invoke govna workflow do not trigger the line. This keeps the checkpoint meaningful rather than mechanical.

The checkpoint is not enforced automatically. It is a human-visible signal that the Director can scan for in completion reports. Its value is in detection of contract-skipping, not in preventing it.

## Audit Verification

No prompt structure eliminates the need to verify. `govna audit` is the enforcement loop — comparing a repo's current governance artifacts against what the templates would produce now, surfacing divergence the Director should resolve.

Audit addresses three classes of slip the session-entry rule cannot cover by itself:

- **Canon-coherence violations** inside the govna source — where `AGENTS.md`, `templates/base/AGENTS.md`, and overlay templates have drifted apart.
- **Adoption drift** in consumer repos — where the consumer's `AGENTS.md` has aged relative to current govna templates and the consumer agent should cherry-pick improvements.
- **Local rule decay** — where the consumer added Project Rules and either contradicted base canon or let them rot.

The session-entry rule shapes what an agent does within a session. Audit keeps the across-session and across-repo picture honest. Both are needed.

## Canon Versus Local Flexibility

govna is intentionally constrained at the canon layer. The base `AGENTS.md` contract — Operator/Director roles, AC-first workflow, approval boundaries, file-change discipline, review style, session-entry rule — is short, imperative, and not negotiable. It is not a flexible framework that consumer repos shape to taste. The fewer primitives govna ships, the less there is to drift against.

Inside that constraint, adopted repos retain meaningful room:

- **Project Rules** (the last section of `AGENTS.md`) are owned by the consumer repo. Consumers add repo-specific rules that do not contradict base canon.
- **Local docs under `govna/`** are consumer-owned beyond the govna-shipped set. Consumers add, replace, or remove docs as the repo's workflow evolves.
- **Tooling, build scripts, and CI** are consumer-owned. govna offers a template starting point but does not prescribe a build pipeline beyond it.

The boundary is sharp: **canon is constrained; local is open**. An adopted agent must follow the canon contract; it can also follow whatever local rules the consumer has added, in the order specified by the conflict-resolution rule (user-in-scope > `AGENTS.md` > referenced docs > model defaults).

If a consumer believes a base canon rule is wrong, the path is to propose a change to govna upstream — not to rewrite the rule locally and let audit complain about it forever.
