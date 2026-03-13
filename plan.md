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

---

Sorted easiest → hardest within each group.

---

## Immediate — Do First

**FEAT9990** — **Skip routing grammar pass when embed is confident**
The current pipeline always runs a fast-model grammar pass (producing `<tool:NAME>` or `<no_tool>`) before invoking the slow model for the actual response. Most queries therefore pay for 2 inference passes sequentially — the routing pass plus the response — which is the primary latency driver on local hardware.

The embed sidecar already determines with high confidence whether tools are needed (Step 1b tool detect). When no tool signals fire, skip the routing grammar pass entirely and go straight to inference. The grammar pass should only run when tool signals are present. This generalizes the existing `web_search` short-circuit (which already skips the grammar pass) to all non-tool queries.

Scope: a conditional in the Step 5 dispatch logic in `cmd/iq/`. No config changes, no new packages. Partial implementation of FEAT9790 that can ship immediately without any architectural work.

---

## Group A — Cosmetic & Mechanical (hours)

**FEAT9980** — **Idiomatic Go naming**
Rename `program_name`/`program_version` to `programName`/`programVersion`. Trivial find-and-replace in one file. Need to ensure `./build.sh` is also updated.

**FEAT9970** — **Proper `errSilent` sentinel**
Replace `fmt.Errorf("")` with a named type so `errors.Is` works correctly. Define a type, swap the one comparison in `runCLI`. Tiny blast radius.

---

## Group B — Structural Cleanup (1–2 days each)

**FEAT9960** — **`NewRegistry()` constructor for tools**
Replace global `Registry` var and `init()` with a constructor. Improves testability and isolation. Touches `internal/tools` + call sites in `cmd/iq`.

**FEAT9950** — **`pipeline:` mode selector in config**
Add a `pipeline:` field to config.yaml that selects which execution strategy `executePrompt` uses. The current 2-tier design becomes `pipeline: two_tier` (the default; backward-compatible when field is absent). This is the freeze/isolation mechanism that lets alternate pipeline designs be added without touching the existing path. The selector dispatches to different implementations of the inference pipeline; the embed sidecar, KB, tool detection, and session handling remain shared infrastructure regardless of mode.

Scope: new field in `internal/config`, a dispatch switch at the `cmd/iq/` prompt pipeline entry point. No behavior change when field is absent or set to `two_tier`. Required by FEAT9810.

**FEAT9940** — **Session file locking**
Add `flock`-style advisory locking to session YAML reads/writes. Prevents corruption from concurrent REPL instances. Small, self-contained.

**FEAT9930** — **Unify help with cobra**
Replace manual `printRootHelp()` (and subcommand help functions) with cobra templates or `SetHelpTemplate`. Eliminates drift between registered commands and printed help. Moderate — touches every command file but each change is mechanical.

**FEAT9920** — **Extended inference parameters**
Currently the sidecar supports three params (`temperature`, `repetition_penalty`, `max_tokens`). Extend `InferParams`/`ResolvedParams` in config, pass through `sidecar.Call`, and handle in `infer_server.py`'s logits processor.

*High-value additions (covers 90% of practical tuning):*
- `top_p` / nucleus sampling — sample from the smallest token set whose cumulative probability exceeds p
- `min_p` — discard tokens below a minimum probability relative to the top token (newer, increasingly popular)
- `stop` — strings that halt generation early
- `seed` — reproducible outputs for benchmarking

*Secondary (useful in specific scenarios):*
- `top_k` — only sample from the k most probable tokens
- `frequency_penalty` — penalizes based on how many times a token has appeared (distinct from repetition_penalty in some APIs)
- `presence_penalty` — flat penalty if a token has appeared at all
- `logit_bias` — manually boost/suppress specific token IDs

*Exotic (defer unless needed):*
- `top_a`, `typical_p` / locally typical sampling, `mirostat` (targets a specific perplexity), tail-free sampling (TFS), eta sampling

**Important tradeoff:** stacking multiple sampling strategies (e.g., `top_k` + `top_p` + `min_p` + `temperature`) can interact in non-obvious ways and sometimes cancel each other out or produce worse results than tuning one or two well. Most practitioners get 90% of the way with just `temperature` + `top_p` (or `min_p`) + `stop` sequences. The implementation should make it easy to configure these but the docs should steer users toward simplicity.

---

## Group C — Cross-Cutting Quality (3–5 days each)

**FEAT9910** — **Error handling audit**
Standardize: wrapped errors (`%w`) for domain functions, sentinel types for control flow, eliminate `nil, nil` returns. Spread across many files but each fix is small. Best done as a sweep with a checklist.

*Known issues to fold in during this sweep:*
- Config parse errors silently swallowed — `Load` returns empty defaults on YAML unmarshal failure with no error surfaced (`internal/config/config.go:208`)
- DuckDuckGo search leaks response bodies — `defer resp.Body.Close()` inside a pagination loop keeps bodies open until the whole search finishes (`internal/search/search.go:325`)
- Tool-call fallback parser ignores `web_search` args — malformed-JSON fallback only extracts `path|expression|pattern`; a broken `web_search` emission silently drops the query (`internal/tools/tools.go:745`)

**FEAT9900** — **`queone/utl` review & improvement**
Audit the package, document its API, decide what to keep/replace/upstream. The goal is contributor-friendliness, not wholesale replacement.

**FEAT9890** — **Test coverage expansion**
Table-driven tests for: `ParseCalls`/`ParseCallsStrict`, `resolveRoute` tier fallback, sidecar lifecycle (start/ready/stop with httptest), config migration paths, embed classification. Biggest bang-for-buck reliability investment. Best paired with FEAT9910 since clean error handling makes testing easier.

**FEAT9880** — **KB source removal prefix collision**
`RemoveSource` uses `strings.HasPrefix`, so removing `/data/app` silently deletes `/data/app2` as well. Fix: switch to exact-path or directory-boundary matching (`source == path || strings.HasPrefix(source, path+"/")`). Silent data loss — high priority despite the small fix. See `internal/kb/kb.go:616`.

**FEAT9870** — **`RawCall` timeout and status-code guard**
`transport.go` uses bare `http.Post` with no deadline and never checks `resp.StatusCode`, so a stuck or erroring sidecar hangs the CLI indefinitely or surfaces as a misleading JSON parse error. Fix: add an `http.Client` with a configurable timeout and an explicit non-200 error return. See `internal/sidecar/transport.go:65`.

**FEAT9860** — **Stale sidecar state / port exhaustion**
`NextAvailablePort` scans all state files rather than live PIDs, and neither `StartInfer` nor `StartSidecar` removes state on early exit or readiness timeout. A crash during startup leaves a dead state entry that permanently "reserves" a port, eventually yielding "no available ports" even when nothing is running. Fix: validate PID liveness during port scanning and add cleanup paths in the early-exit and timeout branches. See `internal/sidecar/sidecar.go:177`, `sidecar.go:321`, `internal/embed/embed.go:89`.

---

## Group D — Architecture Hardening (1–2 weeks each)

**FEAT9850** — **Context-based concurrency**
Wire `context.Context` through the call chain. Replace ad-hoc goroutines (KB prefetch, HF enrichment, sidecar crash detection) with `errgroup`. Add cancellation propagation. Touches the prompt pipeline, sidecar lifecycle, and embed calls.

**FEAT9840** — **Configuration schema versioning**
Add a `version:` field to config.yaml, embedded JSON schema for validation, and a versioned migration chain. Replaces the current ad-hoc auto-migration. Benefits from FEAT9910 (clean error handling) being done first.

---

## Group E — Routing & Intelligence

**FEAT9830** — **Relevance-gated context injection**
Currently KB chunks are injected unconditionally (top-N by similarity, regardless of score). This introduces irrelevant context that can confuse the model and cause hallucinations — e.g., a general-knowledge question gets KB noise that overrides the model's training signal and produces a confident wrong answer. The same risk applies to web results and tool outputs.

Fix: gate each context source behind a relevance signal before injection:
- **KB** — only inject chunks whose cosine similarity to the query embedding exceeds a configurable minimum (e.g., `kbMinScore`). The score is already computed during retrieval; this is a threshold check, not new work. If no chunks clear the bar, skip KB injection entirely and let the model answer from training data.
- **Web** — only inject web results when the `web_search` embed signal fires (see FEAT9820 for promoting this to a pre-fetch RAG path).
- **Tools** — already partially gated by embed-based tool detection (see FEAT9790).

The unifying principle: every context source needs a classifier or score gate that answers "is this source relevant to this query?" before injection. Sources that don't pass are skipped. This is distinct from FEAT9750's context assembly, which handles budget/priority *after* sources are admitted — this feature defines the admission step.

**FEAT9820** — **Cue-triggered web RAG** *(extends existing web_search tool)*
Add a `current_events` cue that, when matched during classification, extracts a search query from the raw prompt, pre-fetches web results, and injects them into context at Step 3 alongside KB chunks — so the model sees fresh web data without needing a tool-call loop. Key work: query extraction before inference, a new fetcher path, and ranking/truncating web chunks vs KB chunks. Web search as a tool already exists (v0.6.3); this promotes it to a RAG source.

**FEAT9810** — **`pipeline: single_pool` — one model, one pass**
First alternate pipeline mode (requires FEAT9950). All models go into a single flat pool; the fast/slow tier distinction is dropped. The embed sidecar still classifies the cue and detects tools. The pipeline selects a model from the pool (initially first-available) and sends the prompt in a single inference pass — no routing grammar pass, no tier switch.

The speed benefit is elimination of the 2-pass pattern: one embed call (fast) + one inference call. This is the right first experiment for setups where 2-hop latency overhead outweighs the quality benefit of triage routing.

When FEAT9800 lands, `single_pool` either merges into it or becomes the degenerate case where all models are tagged `general`. Scope: new `pipeline: single_pool` execution path in `cmd/iq/`, flat pool config structure (or reuse existing tier models as a merged list). Requires FEAT9950.

**FEAT9800** — **Capability-tagged model pool**
Replace the fixed `fast`/`slow` tier model with capability tags per model (e.g., `fast`, `reasoning`, `code`, `long-context`). Queries route to the best-tagged model, with round-robin within a tag group. This is a fundamental rethink of the routing layer — the cue system's `suggested_tier` field, `resolveRoute`, and `pickSidecar` all change.

**FEAT9790** — **Adaptive tool dispatch (grammar-free for capable models)**
Currently IQ forces a binary routing decision on pass 1 via a logits-constrained grammar (`<tool:NAME>` or `<no_tool>`), with a Go-side guard that overrides the model when embed signals disagree. This compensates for smaller models (3B–8B) that can't reliably decide when to use tools or emit well-formed calls without structural enforcement.

The cost: the harness makes the decision for the model in ambiguous cases, and sometimes gets it wrong (e.g., prompts that mention tools but don't want to invoke them). Every edge case patched in Go adds complexity that a better model wouldn't need.

Future direction: with 14B+ models, drop the routing grammar and tool guard entirely. Let the model receive tool definitions in the system prompt and decide organically — emit `<tool_call>` blocks when needed, plain text otherwise. This moves tool dispatch intelligence from Go heuristics back into the model, where it belongs.

Implementation: the dispatch mode (grammar-constrained vs. model-driven) should be selectable per model or per tier — possibly via a capability tag (see FEAT9800) like `tool_native: true`. Smaller models keep the grammar harness; larger models get the freedom. The tradeoff is model size/speed vs. harness complexity.

**FEAT9780** — **Confidence-based inference agent**
The inference loop is managed by a meta-agent that evaluates response quality. Each model in the pipeline emits a confidence score (0.00–1.00). Above threshold (e.g., 0.50): emit response and stop. Below threshold: state what's missing (more context, specific tools, web data) and pass to the next model. This turns the single-pass inference into a multi-model pipeline with self-assessment. Requires: structured output parsing, confidence calibration, and a pipeline orchestrator.

---

## Group F — External Integration

**FEAT9770** — **External API / OpenRouter support**
Allow any tier to use a remote model via OpenRouter or a user-specified OpenAI-compatible API endpoint. Config would add an `api:` field to tier models (e.g., `api:openrouter/anthropic/claude-3.5-sonnet`). The sidecar layer would need an HTTP-passthrough mode that forwards to remote endpoints instead of local mlx_lm. Key decisions: skip OpenRouter and go direct-to-API? How to handle auth tokens? Latency expectations change completely for remote models.

**FEAT9760** — **WebUI prompt interface**
Serve a web interface at `http://localhost:PORT/` that mirrors the interactive CLI `iq ask`. Needs: an HTTP server (Go stdlib or chi), a simple chat UI (vanilla JS or htmx), SSE or WebSocket streaming, and session management. The backend would call the same `executePrompt` pipeline. Scope depends on UI ambition — a minimal terminal-style interface is days; a polished chat UI is weeks.

---

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

---

## Group H — Agent & Orchestration (largest scope)

**FEAT9740** — **MCP / agent orchestration**
Sidecars evolve from inference endpoints into persistent agents with state, tool access, and inter-agent communication. This is the long-term vision for IQ as an agent platform rather than a prompt router. Requires: agent lifecycle management, message passing between agents, shared state/memory, and a control plane. Builds on FEAT9750 (memory), FEAT9780 (confidence routing), and FEAT9800 (capability tags).

---

## Group Z — Future-Proofing (scope TBD, defer until needed)

**FEAT9730** — **Tool execution sandboxing**
Current read-only guards suffice today. If write tools land, add ephemeral working directories, output sanitization, and possibly `os.Chroot` isolation. Design when write tools are specced.

**FEAT9720** — **ANN scaling for embeddings**
Replace brute-force cosine similarity with an ANN library (e.g., hnswlib, FAISS, Annoy) for KB search. Only matters when KB grows past ~10K chunks. Current 384-dim brute force is fine for small KBs.

---

## Appendix — Apple Silicon Constraints

On Apple Silicon, the real constraint is GPU memory bandwidth, not capacity. Every token generated requires streaming the model's weights through the GPU — generation speed is bounded by how fast weights move, not how many fit. With a 4-bit 8B model (~4GB), you're doing a lot of weight streaming per token regardless of how much RAM you have free.

But in IQ specifically, the more likely culprit is the **2-pass inference pattern**:

1. **Pass 1**: fast model runs a routing grammar (`<tool:NAME>` or `<no_tool>`) — this is a full inference call
2. **Pass 2**: slow model runs the actual response — another full inference call

For most queries, you're paying for 2 inference passes sequentially. That's the real latency multiplier, and it compounds on queries that also trigger tool execution (pass 3+).

The embed sidecar already tells you — before any model inference — whether tools are needed and what cue/tier to use. The fast model's routing grammar pass is largely redundant for the non-tool path. FEAT9990 is the direct fix: skip the grammar pass when embed signals are confident, and go straight to the slow model for one clean inference. That alone would roughly halve latency on non-tool queries.

**On the bot orchestrator and GPU parallelism:** Apple Silicon has one unified GPU, not partitioned per-process. Multiple MLX processes running simultaneously share the same GPU cores and bandwidth. There's no true parallelism — they'd compete and each run slower. So the "fan out to multiple bots simultaneously" idea doesn't translate to a speed win on this hardware; it would likely be slower than a single model pass.

The version of the bot orchestrator that *does* work is sequential routing to tiny specialists — a 0.5B model fine-tuned purely for tool extraction is genuinely faster than a 7B general model with a grammar harness, not because of parallelism but because it's smaller and generates fewer tokens to do the same narrow task. That's where the architecture has teeth: **specialization → smaller models → faster per-pass**, not parallelism.
