# IQ Roadmap

## Philosophy

IQ is a **framework for local AI agents**, built hands-on as a way to learn how LLMs, embeddings, RAG, tool use, and local inference actually work by building with them. The goal is to have fun doing vibe coding while developing a real intuition for the technology: what embedding similarity feels like in practice, why grammar-constrained decoding helps small models, where latency comes from, how cue classification degrades at the edges.

The biggest honest constraint is that **local hardware is still too limited for serious tooling at the level of what major AI players are doing**. Anthropic ships Claude Code, OpenAI ships Codex — these run on data-center GPU clusters with millisecond inference and frontier models that dwarf anything that fits on a laptop. Claude Code scores 80.8% on SWE-bench Verified (a benchmark of real-world software engineering tasks: code comprehension, bug fixing, algorithm implementation). Building IQ locally is more like building a bicycle to understand how engines work than competing with a Formula 1 team. That framing is freeing: the right measure of success is "did I learn something and does it work well enough to be useful?" not "does it match Claude Code?"

**Objective:** Use IQ itself to continue its own ongoing development — a local, offline coding copilot that gets better every iteration. The workflow is agentic plan/edit/test loops in a terminal with local models: generate, test, keep what works, fix what doesn't. Cloud-optional by design — local inference is the default and the priority, but remote APIs are supported for setups where a local model isn't up to the task. Optimizing for accuracy, performance, security, and reproducibility through tight feedback loops. Small improvements compound. The north star is a system capable enough to meaningfully assist its own development.

Within that spirit, IQ is also a **framework for building domain-specific local AI agents**, not a general-purpose chatbot. Its design is guided by these principles:

1. **Model selection sets the ceiling; pipeline design closes the gap** — IQ's performance and accuracy is entirely dependent on the specific models used within each tier (embed, fast, slow). The framework routes and orchestrates; the models do the thinking. Model selection defines what's possible; pipeline work determines how close you get to that limit in practice.

2. **Domain-specificity is a strength** — a focused IQ instance with curated models, tuned cues, and a targeted KB will always outperform a generic one. You must fine-tune which models to use for the specific domain you want IQ to focus on. Trying to be good at everything means being great at nothing.

3. **An agent is a domain-tuned IQ instance** — same binary, different configuration. An agent is defined by its model choices, cue set, knowledge base, tool paths, and inference parameters — all expressed in `config.yaml` and `cues.yaml`. No code changes needed to create a new agent.

4. **Multi-agent = multiple domain instances** — each covering a vertical (tech, life, mind, society, etc.), each with its own config directory, KB, and model assignments. Orchestration across agents is a higher-level concern (see FEAT9740).

These principles set the lens for evaluating all roadmap work. There are two tracks: **foundational pipeline work** (routing, memory, orchestration) that benefits every agent equally, and **domain-tuning work** (model benchmarking, cue customization, KB curation, per-domain config) that defines what any specific agent actually is. Neither track is optional. Pipeline work without domain tuning produces a capable system that's good at nothing in particular; domain tuning without pipeline foundations produces a brittle, slow, hard-to-configure agent. The question to ask of any feature: does it make agents more capable, more accurate, or easier to tune for a specific domain?

Below are sorted easiest → hardest within each group.


## Group B — Structural Cleanup


## Group C — Cross-Cutting Quality


## Group D — Architecture Hardening

**FEAT9850** — **Context-based concurrency**
Wire `context.Context` through the call chain. Replace ad-hoc goroutines (KB prefetch, HF enrichment, sidecar crash detection) with `errgroup`. Add cancellation propagation. Touches the prompt pipeline, sidecar lifecycle, and embed calls.


## Group E — Routing & Intelligence

**FEAT9820** — **Cue-triggered web RAG** *(extends existing web_search tool)*
Add a `current_events` cue that, when matched during classification, extracts a search query from the raw prompt, pre-fetches web results, and injects them into context at Step 3 alongside KB chunks — so the model sees fresh web data without needing a tool-call loop. Key work: query extraction before inference, a new fetcher path, and ranking/truncating web chunks vs KB chunks. Web search as a tool already exists (v0.6.3); this promotes it to a RAG source.


**FEAT9800** — **Capability-tagged model pool**
Replace the fixed `fast`/`slow` tier model with capability tags per model (e.g., `fast`, `reasoning`, `code`, `long-context`). Queries route to the best-tagged model, with round-robin within a tag group. This is a fundamental rethink of the routing layer — the cue system's `suggested_tier` field, `resolveRoute`, and `pickSidecar` all change.

**FEAT9790** — **Adaptive tool dispatch (grammar-free for capable models)**
Currently IQ forces a binary routing decision on pass 1 via a logits-constrained grammar (`<tool:NAME>` or `<no_tool>`), with a Go-side guard that overrides the model when embed signals disagree. This compensates for smaller models (3B–8B) that can't reliably decide when to use tools or emit well-formed calls without structural enforcement.

The cost: the harness makes the decision for the model in ambiguous cases, and sometimes gets it wrong (e.g., prompts that mention tools but don't want to invoke them). Every edge case patched in Go adds complexity that a better model wouldn't need.

Future direction: with 14B+ models, drop the routing grammar and tool guard entirely. Let the model receive tool definitions in the system prompt and decide organically — emit `<tool_call>` blocks when needed, plain text otherwise. This moves tool dispatch intelligence from Go heuristics back into the model, where it belongs.

Implementation: the dispatch mode (grammar-constrained vs. model-driven) should be selectable per model or per tier — possibly via a capability tag (see FEAT9800) like `tool_native: true`. Smaller models keep the grammar harness; larger models get the freedom. The tradeoff is model size/speed vs. harness complexity.

**FEAT9780** — **Confidence-based inference agent**
The inference loop is managed by a meta-agent that evaluates response quality. Each model in the pipeline emits a confidence score (0.00–1.00). Above threshold (e.g., 0.50): emit response and stop. Below threshold: state what's missing (more context, specific tools, web data) and pass to the next model. This turns the single-pass inference into a multi-model pipeline with self-assessment. Requires: structured output parsing, confidence calibration, and a pipeline orchestrator.


## Group F — External Integration

**FEAT9770** — **External API / OpenRouter support**
Allow any tier to use a remote model via OpenRouter or a user-specified OpenAI-compatible API endpoint. Config would add an `api:` field to tier models (e.g., `api:openrouter/anthropic/claude-3.5-sonnet`). The sidecar layer would need an HTTP-passthrough mode that forwards to remote endpoints instead of local mlx_lm. Key decisions: skip OpenRouter and go direct-to-API? How to handle auth tokens? Latency expectations change completely for remote models.

**FEAT9760** — **WebUI prompt interface**
Serve a web interface at `http://localhost:PORT/` that mirrors the interactive CLI `iq ask`. Needs: an HTTP server (Go stdlib or chi), a simple chat UI (vanilla JS or htmx), SSE or WebSocket streaming, and session management. The backend would call the same `executePrompt` pipeline. Scope depends on UI ambition — a minimal terminal-style interface is days; a polished chat UI is weeks.


## Group G — Memory & Knowledge Architecture

**FEAT9750** — **Layered memory system**
Extend the existing response cache, session buffer, and KB into a unified memory architecture. Four layers — response cache, session buffer, vector memory (partially exists via KB), and persistent KB — currently operate as separate systems with no shared controller.

New work: a `MemoryController` that unifies fetch/store/retrieve/prune across all layers. Memory is injected into inference context in a controlled, token-efficient manner. Includes: periodic pruning of old/low-similarity entries, optional summarization of long sequences to save context space, and a clean Go/Python interface.

Key principles:
- **Offline-friendly** — all storage local (SQLite, JSON, vector indexes). No cloud dependencies.
- **Incremental adoption** — cache → semantic memory → persistent KB. Each layer is modular.
- **Memory hygiene** — prune, distill, compress. Keep context tight.

**Context assembly** — Step 4 ASSEMBLE currently concatenates prompt components (system prompt, KB chunks, session history, tool outputs, user input) linearly with no awareness of context limits. This needs:
- **Context budget management** — when assembled components exceed the model's context window, decide what gets trimmed and in what order. The budget depends on the target model's max context length, which varies per sidecar.
- **Priority ranking** — user input is sacred, system prompt is critical. Below that: how do you rank KB chunks vs session history vs tool results? Recency? Relevance score? A fixed priority order?
- **Compression** — summarize older session turns or large tool outputs to fit more signal into fewer tokens. Could use the fast-tier model itself to compress before handing off to the slow-tier model for inference.


## Group H — Agent & Orchestration (largest scope)

**FEAT9740** — **MCP / agent orchestration**
Sidecars evolve from inference endpoints into persistent agents with state, tool access, and inter-agent communication. This is the long-term vision for IQ as an agent platform rather than a prompt router. Requires: agent lifecycle management, message passing between agents, shared state/memory, and a control plane. Builds on FEAT9750 (memory), FEAT9780 (confidence routing), and FEAT9800 (capability tags).

## Group Z — Future-Proofing (scope TBD, defer until needed)

**FEAT9730** — **Tool execution sandboxing**
Current read-only guards suffice today. If write tools land, add ephemeral working directories, output sanitization, and possibly `os.Chroot` isolation. Design when write tools are specced.

**FEAT9720** — **ANN scaling for embeddings**
Replace brute-force cosine similarity with an ANN library (e.g., hnswlib, FAISS, Annoy) for KB search. Only matters when KB grows past ~10K chunks. Current 384-dim brute force is fine for small KBs.


## Appendix — Apple Silicon Constraints

On Apple Silicon, the real constraint is GPU memory bandwidth, not capacity. Every token generated requires streaming the model's weights through the GPU — generation speed is bounded by how fast weights move, not how many fit. With a 4-bit 8B model (~4GB), you're doing a lot of weight streaming per token regardless of how much RAM you have free.

But in IQ specifically, the more likely culprit is the **2-pass inference pattern**:

1. **Pass 1**: fast model runs a routing grammar (`<tool:NAME>` or `<no_tool>`) — this is a full inference call
2. **Pass 2**: slow model runs the actual response — another full inference call

For most queries, you're paying for 2 inference passes sequentially. That's the real latency multiplier, and it compounds on queries that also trigger tool execution (pass 3+).

The embed sidecar already tells you — before any model inference — whether tools are needed and what cue/tier to use. The fast model's routing grammar pass is largely redundant for the non-tool path. FEAT9990 is the direct fix: skip the grammar pass when embed signals are confident, and go straight to the slow model for one clean inference. That alone would roughly halve latency on non-tool queries.

**On the bot orchestrator and GPU parallelism:** Apple Silicon has one unified GPU, not partitioned per-process. Multiple MLX processes running simultaneously share the same GPU cores and bandwidth. There's no true parallelism — they'd compete and each run slower. So the "fan out to multiple bots simultaneously" idea doesn't translate to a speed win on this hardware; it would likely be slower than a single model pass.

The version of the bot orchestrator that *does* work is sequential routing to tiny specialists — a 0.5B model fine-tuned purely for tool extraction is genuinely faster than a 7B general model with a grammar harness, not because of parallelism but because it's smaller and generates fewer tokens to do the same narrow task. That's where the architecture has teeth: **specialization → smaller models → faster per-pass**, not parallelism.
