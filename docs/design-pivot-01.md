# Design Pivot 01 — Pragmatic Local CLI Refocus

**Date:** 2026-03-18
**Status:** Adopted

## What triggered this pivot

A structured review of the roadmap against the Apple Silicon Constraints appendix (see `arch.md`) surfaced a growing tension: several planned features assumed inference capabilities — concurrency, multi-model parallelism, fast multi-pass pipelines — that Apple Silicon's unified GPU architecture cannot deliver in practice.

The specific conflicts:

- **Two-tier routing** (fast model → slow model) pays two sequential inference passes for most queries. The embed short-circuit (shipped in v0.9.4) already obsoleted the fast model's main job. The `two_tier` architecture became a liability rather than an asset.
- **FEAT9780 (confidence-based multi-model routing)** adds deliberate pass overhead — exactly the wrong direction for bandwidth-limited hardware.
- **FEAT9800 (capability-tagged model pool)** adds routing complexity without addressing the single-GPU constraint. Tags don't create parallelism.
- **FEAT9740 (MCP / agent orchestration)** implicitly assumed concurrent agent execution, which the appendix explicitly rules out on Apple Silicon.

At the same time, IQ's current strengths were pointing in a clearer direction: a fast embed layer for classification and tool detection, a single clean inference pass in `single_pool` mode, and a tight CLI workflow that could genuinely serve local development if given write tools and self-awareness.

## Old direction (pre-pivot)

- Two-tier routing: fast model for classification/grammar pass, slow model for inference
- Roadmap trending toward multi-model confidence pipelines, capability-tagged pools, concurrent agent orchestration
- Feature naming: `FEAT####` (descending from FEAT9990)
- Ambitious Group A–D structure that outpaced what local hardware can support today

## New direction (post-pivot)

**IQ is a fast, local, offline CLI utility with LLM capabilities, useful for ad hoc prompts and local development including its own.**

Design principles that survive the pivot:
- Single embed pass for classification, tool detection, KB retrieval (unchanged)
- Single inference pass in `single_pool` mode as the canonical pipeline
- Domain-specificity is a strength — a tuned IQ instance beats a generic one
- Sequential orchestration only — no parallelism assumptions

What changed:
- `two_tier` retired as primary mode; `single_pool` is now canonical
- Grammar-harness routing pass eliminated (A2) — embed short-circuit + model-driven dispatch replaces it
- Roadmap reorganized into five pragmatic groups (A–E) and one future group (F)
- Feature naming simplified to group letter + sequence number: `A1`, `B2`, etc.
- Ambitious multi-model and orchestration features deferred to Group F without deletion — the ideas are sound, the hardware isn't there yet

## What moved to Group F (future)

| New ID | Original ID | Feature |
|--------|-------------|---------|
| F1 | FEAT9780 | Confidence-based multi-model routing |
| F2 | FEAT9800 | Capability-tagged model pool |
| F3 | FEAT9740 | MCP / agent orchestration |
| F4 | FEAT9750 | Layered memory controller |
| F5 | FEAT9760 | WebUI prompt interface |
| F6 | FEAT9820 | Cue-triggered web RAG |
| F7 | FEAT9720 | ANN scaling for embeddings |

These features remain valid and desirable. They become practical when local models improve (14B+ at usable speed), when Apple ships better ANE-optimized model support, or when a cloud-backed mode is added. They are deferred, not abandoned.

## What the new roadmap focuses on

- **Group A** — Finish the pipeline simplification: retire `two_tier`, drop the grammar harness, add context budget management
- **Group B** — Write tools (file write/edit, git read, shell exec) — what makes IQ useful for local development
- **Group C** — Self-knowledge: confidence surfacing, capability limit detection, token budget warnings
- **Group D** — Observability: structured trace log, `iq trace` command
- **Group E** — Domain tuning for IQ itself: self-KB, cue benchmarking loop

The north star is unchanged: a system capable enough to meaningfully assist its own development. The pivot is about building toward that on hardware that exists today.
