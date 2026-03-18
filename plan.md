# IQ Roadmap

## Philosophy

IQ is a **framework for local AI agents**, built hands-on as a way to learn how LLMs, embeddings, RAG, tool use, and local inference actually work by building with them. The goal is to have fun doing vibe coding while developing a real intuition for the technology: what embedding similarity feels like in practice, why grammar-constrained decoding helps small models, where latency comes from, how cue classification degrades at the edges.

The biggest honest constraint is that **local hardware is still too limited for serious tooling at the level of what major AI players are doing**. Anthropic ships Claude Code, OpenAI ships Codex — these run on data-center GPU clusters with millisecond inference and frontier models that dwarf anything that fits on a laptop. Claude Code scores 80.8% on SWE-bench Verified (a benchmark of real-world software engineering tasks: code comprehension, bug fixing, algorithm implementation). Building IQ locally is more like building a bicycle to understand how engines work than competing with a Formula 1 team. That framing is freeing: the right measure of success is "did I learn something and does it work well enough to be useful?" not "does it match Claude Code?"

**Objective:** Use IQ itself to continue its own ongoing development — a fast, local, offline CLI utility with LLM capabilities, useful for ad hoc prompts and local development including its own. The workflow is agentic plan/edit/test loops in a terminal with local models: generate, test, keep what works, fix what doesn't. Cloud-optional by design — local inference is the default and the priority, but remote APIs are supported for setups where a local model isn't up to the task. Small improvements compound. The north star is a system capable enough to meaningfully assist its own development.

Within that spirit, IQ is also a **framework for building domain-specific local AI agents**, not a general-purpose chatbot. Its design is guided by these principles:

1. **Model selection sets the ceiling; pipeline design closes the gap** — IQ's performance and accuracy is entirely dependent on the specific models used. The framework routes and orchestrates; the models do the thinking.

2. **Domain-specificity is a strength** — a focused IQ instance with curated models, tuned cues, and a targeted KB will always outperform a generic one.

3. **An agent is a domain-tuned IQ instance** — same binary, different configuration. An agent is defined by its model choices, cue set, knowledge base, tool paths, and inference parameters — all expressed in `config.yaml` and `cues.yaml`. No code changes needed to create a new agent.

4. **Single-pass is the goal** — on Apple Silicon, every additional inference pass multiplies latency. The embed layer handles classification and tool detection before inference; the pipeline should deliver one clean inference pass for the vast majority of queries.

5. **Sequential orchestration only** — Apple Silicon has one unified GPU. Multiple concurrent inference processes compete for the same bandwidth and each runs slower. IQ is designed around sequential, not parallel, model execution.

See [`docs/design-pivot-01.md`](docs/design-pivot-01.md) for the rationale behind the current roadmap structure.


## Development Methodology

Each feature follows an **AC-first workflow**: before any code is written, an **acceptance criteria** (AC) document is drafted that defines exactly what the implementation must achieve. The AC covers: what the codebase scan found (motivating the change), what is explicitly in scope and out of scope (blast-radius management), and the acceptance tests that must pass. This keeps implementations focused, prevents scope creep, and gives future contributors a clear record of intent.

AC documents live in `docs/` and are checked into the repo alongside the code they describe.

Features are identified by group letter + sequence number: `A1`, `B2`, etc. Sorted easiest → hardest within each group.


## Group A — Pipeline consolidation

Finish the simplification that A1 started. Changes here reduce inference passes and remove routing complexity.

**A1B — Rename `tiers:` to flat `models:` list** *(schema v2 migration)*
`tiers: {fast: ..., slow: ...}` is vestigial naming from the two-tier era. Replace with a flat `models: [model-id, ...]` list and global-only inference params. Requires: config schema v2, `migrateV1` to flatten tiers on load, rename `iq tier` → `iq model` command group, remove `TierConfig`/`TierOrder`/`TierForModel`/`TierModels`, simplify `ResolveInferParams` (drop tier arg), update sidecar `State.Tier` handling, update `lm.SuggestTier`, update probe/perf/status display. See `docs/a1b_ac.md` (to be written before implementation).

**A2 — Model-driven tool dispatch (drop the grammar harness)**
Tool detection is already handled by embed short-circuit. For cases that reach the grammar pass today, replace it with model-driven dispatch: send tool definitions in the system prompt and let the model emit `<tool_call>` blocks or plain text organically. Eliminates the last conditional inference pass for capable models. Smaller models that can't reliably emit structured calls get a lightweight fallback.

**A3 — Context budget management**
Before inference, estimate assembled context size against the target model's known context window. Trim in priority order: KB chunks first, then session history; system prompt and user input are never trimmed. Warn the user if anything was dropped. Pure Go work, no inference changes.


## Group B — Write tools

What makes IQ useful for local development. Each write tool requires a spec, a safety model, and a confirmation prompt before execution. Read-only tools already exist; these are the next tier.

**B1 — File write tools**
`write_file`, `append_file`, `create_file`. Confirmation prompt before any write. No silent overwrite. This closes the gap between IQ as a read-only query tool and IQ as a local coding assistant.

**B2 — Git read tools**
`git_status`, `git_diff`, `git_log`. Read-only, zero risk. Lets IQ reason about the current repo state inline, without the user pasting output manually. Foundation for future write-side git tools.

**B3 — Shell exec**
Execute a user-confirmed shell command and capture output. Intentionally narrow scope: no piping, no background processes, one command at a time, explicit confirmation before each execution. Useful for running tests, builds, or inspecting system state within an IQ session.


## Group C — Self-knowledge

IQ should know when it's uncertain or out of its depth and say so clearly, rather than silently returning a low-quality result.

**C1 — Confidence surfacing**
When the embed classification score falls below threshold, or when cue match is weak, surface it inline: *"Low confidence routing (score: 0.31) — result may be unreliable."* This information already exists in `--debug` output; C1 exposes it at normal verbosity as a brief, non-blocking note.

**C2 — Capability limit detection**
Detect queries likely beyond the local model's reliable range: multi-step reasoning chains, large-scale code generation, tasks that reference files IQ hasn't read. Emit a structured note: *"This query may exceed local model capability — consider breaking it into smaller steps or using a larger model."* Not a blocker; honest signal.

**C3 — Token budget warning**
Before inference, if the assembled context exceeds ~80% of the model's window, warn the user what was trimmed and why rather than silently truncating. Pairs with A3 (context budget management) — A3 does the trimming, C3 surfaces it.


## Group D — Observability

Structured visibility into what IQ is doing, so it can assist its own development and diagnose regressions without `--debug` spelunking.

**D1 — Structured trace log**
Each pipeline run appends a JSON line to `~/.config/iq/run/trace.log`: timestamp, cue matched, tool detected, model used, embed scores, pass count, latency per step, cache hit/miss, token count. Pure Go, no inference changes.

**D2 — `iq trace` command**
Display recent runs from the trace log in a readable table. Support basic filters (by cue, tool, model). Makes latency profiling, regression detection, and benchmarking tractable without external tooling.


## Group E — Domain tuning for IQ itself

IQ as its own first domain-tuned agent. These features make IQ aware of its own codebase and make cue tuning empirical.

**E1 — IQ self-KB**
Curate a focused KB from IQ's own development artifacts: `arch.md`, `plan.md`, key source files, build output patterns. IQ becomes its own first domain-tuned agent and a real test of the KB pipeline at practical scale.

**E2 — Cue benchmarking loop**
An `iq bench` command that runs a fixed set of labeled prompts through the classify step and reports accuracy %, average score, and worst misses. Runnable after any cue or model change. Makes cue tuning empirical rather than intuitive. Extends the work started in FEAT9690.


## Group F — Future

Features that are architecturally sound but require hardware or model capabilities not available today on Apple Silicon. Deferred, not abandoned. See [`docs/design-pivot-01.md`](docs/design-pivot-01.md) for context.

**F1 — Confidence-based inference agent** *(was FEAT9780)*
A meta-agent that evaluates response quality per pass. Below-threshold responses trigger a handoff to the next model with a note on what's missing. Practical when 14B+ models run at usable speed locally, or when a cloud-backed tier is available.

**F2 — Capability-tagged model pool** *(was FEAT9800)*
Replace the flat model pool with capability tags per model (`reasoning`, `code`, `long-context`, etc.). Queries route to the best-tagged model with round-robin within a tag group. Practical when ANE-optimized specialist models exist and can be swapped without bandwidth penalty. When implemented, reintroduces a `routing:` config field (not `pipeline:`) to select the dispatch strategy.

**F3 — MCP / agent orchestration** *(was FEAT9740)*
Sidecars evolve into persistent agents with state, tool access, and inter-agent communication. Requires sequential-safe orchestration design (no concurrent GPU use). Builds on F1 (confidence routing) and B3 (shell exec). Orchestration mode would be expressed via the same `routing:` config field introduced in F2.

**F4 — Layered memory controller** *(was FEAT9750)*
A `MemoryController` unifying response cache, session buffer, vector memory, and persistent KB into a single fetch/store/retrieve/prune interface. Modular and offline-friendly. Builds on what already exists.

**F5 — WebUI prompt interface** *(was FEAT9760)*
A minimal web interface at `http://localhost:PORT/` mirroring `iq ask`. SSE streaming, session management, same `executePrompt` backend. Out of scope for the current CLI-first focus.

**F6 — Cue-triggered web RAG** *(was FEAT9820)*
A `current_events` cue that pre-fetches web results and injects them into context at the ASSEMBLE step. Extends the existing `web_search` tool into a first-class RAG source. Depends on A3 (context budget management) being solid first.

**F7 — ANN scaling for embeddings** *(was FEAT9720)*
Replace brute-force cosine similarity with an ANN library (hnswlib, FAISS, Annoy) for KB search. Only relevant past ~10K chunks. Current 384-dim brute force is fine for small KBs.


## Appendix — Apple Silicon Constraints

On Apple Silicon, the real constraint is **GPU memory bandwidth, not capacity**. Every token generated requires streaming the model's weights through the GPU — generation speed is bounded by how fast weights move, not how many fit. With a 4-bit 8B model (~4GB), you're doing a lot of weight streaming per token regardless of how much RAM you have free.

Apple runs models primarily through **Core ML**, its native ML framework tightly integrated into iOS, macOS, and Apple Silicon hardware. Core ML supports ONNX, TensorFlow, PyTorch, and Apple's proprietary MLModel format, and automatically routes computation to the best available hardware — CPU, GPU, or **Apple Neural Engine (ANE)**. Models are typically converted to `.mlmodel` files via **coremltools** (Python) or Xcode, which handles quantization and optimization. For **MLX models** specifically, Apple doesn't natively support `.mlx` files — you need a conversion step (coremltools or MLX → ONNX/MLModel export) before using them in Swift/Core ML. Once converted, you get full Apple Silicon acceleration without manual threading or GPU management.

There is a full Swift SDK: Core ML itself is a Swift/Objective-C framework with type-safe APIs, SwiftUI integration, async inference, batch processing, and pipeline composition. **Create ML** provides on-device training for lightweight models. Metal and ANE acceleration are handled transparently by Core ML's runtime — you can tune hardware routing via model configuration options, but in practice the framework does a reasonable job of picking the right accelerator.

**The 2-pass inference problem in IQ is the real latency culprit:**

1. **Pass 1**: fast model runs a routing grammar (`<tool:NAME>` or `<no_tool>`) — full inference call
2. **Pass 2**: slow model runs the actual response — another full inference call

For most queries, you're paying for 2 sequential inference passes. That's the real latency multiplier, compounding further on queries that also trigger tool execution (pass 3+). The embed sidecar already tells you — before any model inference — whether tools are needed and what cue/tier to use. The fast model's routing grammar pass is largely redundant for the non-tool path. Feature A1/A2 address this directly: retire the two-tier architecture and drop the grammar harness, going straight to a single clean inference pass.

**On GPU parallelism:** Apple Silicon has one unified GPU, not partitioned per-process. Multiple MLX (or Core ML) processes running simultaneously share the same GPU cores and bandwidth — whether routed through Metal, ANE, or both. There's no true parallelism; they compete and each run slower. The "fan out to multiple bots simultaneously" idea doesn't translate to a speed win on this hardware; it would likely be slower than a single model pass. Core ML's automatic hardware routing doesn't change this — it optimizes *within* a single model's inference, not *across* concurrent model invocations.

The version of the bot orchestrator that *does* work is **sequential routing to tiny specialists** — a 0.5B model fine-tuned purely for tool extraction is genuinely faster than a 7B general model with a grammar harness, not because of parallelism but because it's smaller and generates fewer tokens to do the same narrow task. These specialist models also convert cleanly to `.mlmodel` format and can leverage ANE efficiently, since smaller models with simpler architectures map better to the Neural Engine's fixed-function pipeline. That's where the architecture has teeth: **specialization → smaller models → faster per-pass → better ANE utilization**, not parallelism.

**On model loading and memory residency:** A common concern with sequential orchestration is the cost of loading and unloading models between passes. In practice, this isn't an issue on Apple Silicon. Core ML and MLX both keep compiled models hot in memory — you load them once at app startup and they stay resident. Apple Silicon's unified memory architecture means model weights sit in RAM that is GPU and ANE memory simultaneously; there's no "upload to VRAM" step like you'd have with a discrete GPU. Sequential inference across 2-3 small models just reads from weights already in unified memory — the per-pass cost is the inference itself, not a load/unload cycle. The real constraint is total RAM: if models collectively exceed available memory, you get eviction and reload penalties. But a 0.5B specialist alongside a 4-bit 8B main model is roughly ~5GB total, which fits comfortably on any Apple Silicon Mac.
