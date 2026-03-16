# IQ Architecture

## Overview

IQ is a local generative AI system for Apple Silicon, capable of running LLMs entirely offline with no cloud dependency. The **`iq`** CLI binary orchestrates this system — managing model downloads, tier assignment, cue definitions, knowledge base access, sidecar processes, and intelligent prompt routing — all from a unified command-line interface.

## System Diagram

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                               iq CLI (Go)                                    │
│                                                                              │
│ iq lm    iq start/stop  iq cue  iq kb   iq ask    iq pry   iq perf iq config │
│ (models)  (service)     (cues)  (RAG)   (infer)   (raw)    (bench) (schema)  │
└────┬──────────┬──────────┬────────┬───────┬─────────┬────────────┬───────────┘
     │          │          │        │       │         │            │
     ▼          ▼          ▼        ▼       ▼         ▼            ▼
┌─────────┐ ┌──────┐ ┌────────┐ ┌──────────────────────┐ ┌──────────────────┐
│ HF      │ │config│ │cues    │ │ infer_server.py      │ │ sessions/        │
│ cache   │ │.yaml │ │.yaml   │ │ sidecars (pool)      │ │ <id>.yaml        │
│         │ │      │ │        │ │                      │ │                  │
│~/.cache/│ │tiers:│ │name    │ │ fast pool :27001+    │ │ kb.json          │
│hugging  │ │ fast │ │category│ │ slow pool :27001+    │ │ (vector index)   │
│face/hub/│ │ slow │ │desc    │ │                      │ │                  │
│models-- │ │      │ │prompt  │ │ OpenAI-compatible    │ └──────────────────┘
│org--repo│ │      │ │tier    │ │ HTTP API             │ ┌──────────────────┐
│/snapshot│ └──────┘ └────────┘ └──────────────────────┘ │ embed sidecar    │
│  /hash/ │                                              │ :27000           │
└─────────┘                                              └──────────────────┘
```


## Package Structure

Domain logic lives in isolated packages under `internal/`. Each package owns one conceptual domain — its types, helpers, constants, and persistence logic — and exports a clean API consumed by `cmd/iq` and by sibling packages.

| Package | Domain |
|---------|--------|
| `internal/config` | Config CRUD, tier definitions, embed model, migrations |
| `internal/search` | DuckDuckGo web search client with Brave fallback; `Client` struct for concurrency-safe config |
| `internal/sidecar` | Sidecar lifecycle, port allocation, pool dispatch, state files, HTTP transport (Call/Stream/RawCall) |
| `internal/cue` | Cue types, CRUD, defaults, lookup helpers, embedded default YAML |
| `internal/embed` | Embed sidecar startup, HTTP embedding calls, cosine similarity, cue classifier |
| `internal/cache` | Response cache (FNV64a hashing, TTL, load/save) |
| `internal/tools` | Tool registry, parser, executor (timeout + output cap), signal detection, confirm mode |
| `internal/lm` | HuggingFace API client, model search/enrich, manifest CRUD, model parsing, formatting helpers |
| `internal/kb` | Knowledge base index, chunking, hybrid search, ingest |

The `cmd/iq` package is the CLI entry point — it wires commands (cobra), flags, the prompt pipeline, REPL, and orchestration.

## Components

### Model Management

The **`iq lm`** command handles the full model lifecycle. Models are downloaded from [mlx-community](https://huggingface.co/models?filter=mlx) via the `hf` CLI and stored in the standard HuggingFace cache at `~/.cache/huggingface/hub/`. A manifest at `~/.config/iq/models.json` tracks what IQ knows about.

Key operations: `search`, `get`, `list`, `show`, `rm`.

`iq lm search` queries the HF API, enriches results in parallel (one goroutine per model) to populate DISK and EST MEM, and displays TASK / DISK / PARAMS / EST MEM / DOWNLOADS. The TASK column shows the HuggingFace `pipeline_tag` — green for `text-generation` and `feature-extraction` (displayed as `embedding`), red for unsupported types (e.g. `image-text-to-text`). Accepts an optional query string or a numeric count (e.g. `iq lm search 100`).

`iq lm get` checks the model's task type before downloading; if it is not `text-generation`, a yellow warning is printed (download proceeds anyway). After download, the `pipeline_tag` is cached in the manifest for offline display. Infers a suggested tier from disk size (< 2GB → fast, else slow) and prints the `iq tier add` command to assign it.

`iq lm list` displays TASK alongside DISK / PULLED / PARAMS / EST MEM / TIER. On first display, missing task tags are backfilled from the HF API in parallel (with local `config.json` inference as fallback) and persisted to the manifest.

`iq lm show` displays the TASK field (backfilled from HF API or local `config.json` inference if not cached).

**Local task inference** (`inferTaskFromConfig`) — when the HF API returns no `pipeline_tag`, IQ reads the model's local `config.json` and infers the task: vision indicator keys (`vision_config`, `visual`, `vision_tower`, `image_size`) or known VLM `model_type` values → `image-text-to-text`; known text-generation `model_type` values (only after confirming no vision indicators) → `text-generation`.

`iq lm rm` auto-clears tier assignments and stops running sidecars (including the embed sidecar) with yellow warnings before prompting for confirmation. The confirmation prompt is printed in yellow with `[y/N]` in default color.

### Configuration

Manages `~/.config/iq/config.yaml` via the `internal/config` package. Exports `Message`, `Config`, `TierConfig`, `InferParams`, `ResolvedParams` structs, `Dir()`, `Path()`, `Load()`, `Save()`, `EmbedModel()`, `TierForModel()`, `AllAssignedModels()`, `ResolveInferParams()`, `TierOrder`, and `DefaultEmbedModel`. `Message` is the shared role+content type used across inference, session persistence, and cache key computation. `Load()` returns in-memory defaults on read-only filesystems. Tiers are **pools** — each tier holds a list of model IDs and optional inference parameter overrides.

```
fast    sub-2GB models — used for quick inference tasks
slow    2GB+ models    — used for quality inference
```

**Inference parameters** — eight parameters can be tuned globally and/or per-tier:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `repetition_penalty` | 1.3 | Penalises repeated tokens (1.0 = off) |
| `temperature` | 0.7 | Sampling temperature (lower = more deterministic) |
| `max_tokens` | 8192 | Maximum tokens in response |
| `top_p` | — | Nucleus sampling: sample from the smallest token set whose cumulative probability ≥ p |
| `min_p` | — | Discard tokens below this probability relative to the top token |
| `top_k` | — | Sample from only the k most probable tokens |
| `stop` | — | List of strings that halt generation early (trimmed from response) |
| `seed` | — | Fix the random seed for reproducible outputs |

The first three always carry a hardcoded default. The last five are unset by default (nil/empty) — when absent, mlx_lm uses its own defaults. Resolution order: **per-tier override > global config > hardcoded default**. Pointer types (`*float64`, `*int`) distinguish "not set" from "set to zero."

Practical guidance: stacking multiple sampling strategies (e.g. `top_k` + `top_p` + `min_p`) can interact in non-obvious ways. Most setups get 90% of the benefit from `temperature` + `top_p` (or `min_p`) + `stop`.

**Recommended per-tier tuning** — each tier serves a different role, and inference parameters should reflect that:

| Parameter | `fast` (triage) | `slow` (quality) | Why |
|-----------|----------------|-----------------|-----|
| `temperature` | 0.3 | 0.7 | Fast tasks (classify, short answers, tool routing) need consistency; slow tasks (reasoning, generation) benefit from expressiveness |
| `repetition_penalty` | 1.1 | 1.3 | Short outputs rarely loop; longer outputs need a heavier hand to avoid repetition |
| `max_tokens` | 2048 | 8192 | Cap fast responses since triage doesn't need length; give slow models room for deeper reasoning |

```yaml
# Global defaults (populated on first creation; editable)
version: 1
repetition_penalty: 1.3
temperature: 0.7
max_tokens: 8192

tiers:
  fast:
    models:
      - mlx-community/Llama-3.2-3B-Instruct-4bit
    # Per-tier overrides — tuned for quick, deterministic triage
    repetition_penalty: 1.1
    temperature: 0.3
    max_tokens: 2048
  slow:
    models:
      - mlx-community/Qwen2.5-7B-Instruct-4bit
    # Slow tier inherits global defaults (0.7 temp, 1.3 rep, 8192 tokens)

embed_model: mlx-community/bge-small-en-v1.5-bf16
pipeline: two_tier  # optional; two_tier is the default (single_pool also valid)
```

Use `iq perf sweep` to validate these choices on your hardware — sweep the same models under both tier configs to see whether the parameter differences move the needle on latency and quality.

Tier commands: `iq tier show`, `iq tier add <tier> <model>`, `iq tier rm <tier> <model>`.

Embed model commands: `iq embed show`, `iq embed set <model>`, `iq embed rm`.

**Schema versioning** — `config.yaml` carries a `version:` field (integer). `ConfigVersion = 1` is the current schema. On load, the version is peeked first: version 0 (absent field) triggers the legacy migration chain; version > `ConfigVersion` returns a hard error ("upgrade iq"); current version is unmarshalled directly. After any migration the version is stamped to `ConfigVersion` and the file is saved. `Load()` always returns a `Config` with `Version == ConfigVersion` after a successful migration.

Auto-migration chain (v0 → v1): old config formats are silently converted — four-tier single-string (`tiny`/`fast`/`balanced`/`quality`) uses the 2GB disk threshold, flat-list tiers (`tiers: {fast: [model-a]}`) become structured `TierConfig`, legacy `cue_model`/`kb_model` fields are migrated to the unified `embed_model`.

**Config inspection** — `iq config` (or `iq config show`) dumps the full effective configuration: config.yaml settings (including `pipeline` mode), tier pools with per-tier overrides, cue summary (count + categories), KB status (sources/chunks), all operational thresholds (cue classify 0.40, keyword boost 0.10, tool classify 0.60, KB min score 0.72, KB top-k 3), and runtime constants (ports, timeouts, cache TTL, registry sizes). `iq config validate` checks config.yaml parse, tier model assignments, embed model, deprecated fields, tool path existence, inference param ranges, cue uniqueness, and KB integrity — reports errors and warnings.

### Cue Definitions

The **`iq cue`** command manages `~/.config/iq/cues.yaml`, seeded from an embedded default set of 17 cues across 8 categories.

```
general  code  reasoning  language_tasks  generation  summarization  safety  domain
```

Each cue carries a `name`, `category`, `description`, `system_prompt`, `suggested_tier`, and an optional direct `model` override (kept for power users, not actively promoted in routing).

Commands: `list`, `show`, `add`, `edit`, `rm`, `assign`, `unassign`, `reset`, `sync`.

`sync` merges new factory cues into an existing `cues.yaml` without overwriting user customisations — useful when upgrading IQ to a version that adds new cues.

### Service Daemon

The **`iq start`** / **`iq stop`** commands manage sidecar processes. Each sidecar runs as a detached `infer_server.py` process (a custom MLX inference server embedded in the Go binary, extracted to `~/.config/iq/` at startup). Ports are assigned dynamically starting at 27001. State is persisted to `~/.config/iq/run/<model-slug>.json` (PID, port, tier, model, start time), and logs go to `~/.config/iq/run/<model-slug>.log`.

Start sequence:
1. Allocate next free port from 27001+
2. Resolve HF snapshot directory (`snapshots/<hash>/`) — the `--model` path
3. **VLM guard** — read `config.json` and reject vision-language models (checks for `vision_config`, `vision_tower`, `image_size` keys and known VLM `model_type` values). `mlx_lm.load` cannot handle vision weights.
4. Locate Python interpreter from the `mlx-lm` pipx venv; extract `infer_server.py` to `~/.config/iq/`
5. Spawn detached subprocess (`Setsid: true`)
6. Poll `GET /v1/models` until 200 OK or 120s timeout. A background goroutine calls `cmd.Wait()` to detect early crashes reliably (avoids zombie-pid false positives from signal-0 checks).
7. On failure: print last 10 log lines + path

`iq start/stop` accepts a tier name (acts on the whole pool), a model ID (acts on one), or no argument (all assigned models). On first run with no tiers configured, `iq start` prints a recommended setup with example `iq lm get` and `iq tier add` commands.

**Pool dispatcher (`pickSidecar`)** — scans live state files for a given tier and returns one. With `preferSmallest: true`, it returns the model with the smallest disk footprint (used by the auto-naming background goroutine).

`iq doc` checks runtime dependencies: `python3` available, `mlx_lm.server` found (needed for its venv Python) and `--model` flag supported, `mlx-embedding-models` package installed, all assigned model HuggingFace cache dirs exist.

**Embeddings** — handled by a single local Python sidecar (`embed_model`, port 27000) started with `iq start`. Serves cue classification, tool detection, and KB indexing/retrieval. Configure via `iq embed`.

`iq status` (alias: `iq st`) shows TIER / MODEL / ENDPOINT / PID / UPTIME / MEM for all assigned models plus the embed sidecar row, IQ process memory, and combined total.

### Knowledge Base

The **`iq kb`** command manages `~/.config/iq/kb.json`, an embedded vector index used for RAG (Retrieval-Augmented Generation).

> **What RAG is.** Large language models know only what was in their training data. RAG extends this by retrieving relevant passages from your own documents at query time and injecting them into the prompt as plain text context — no fine-tuning, no model modification. The model reasons over retrieved material just as it would any other text in its context window. The key insight: embeddings are used for *retrieval* (finding relevant passages by semantic similarity), but the model itself only ever sees text. Embeddings never enter the model directly in this architecture.

**How it works end-to-end:**

```
iq kb ingest ~/projects/myapp
    │
    ├── walk directory (skips .git, node_modules, vendor, __pycache__, hidden dirs)
    ├── read each text file (.go, .md, .py, .yaml, ...)
    ├── structure-aware chunking (see below)
    ├── embed each chunk via embed sidecar :27000 (batches of 20)
    └── store chunk text + 384-float vector in kb.json

iq ask "how does the auth middleware work?"
    │
    ├── embed user input → query vector
    ├── hybrid scoring: cosine_similarity + keyword boost — Go, in-memory
    ├── top-3 chunks retrieved (score ≥ 0.72 threshold)
    ├── injected as plain text context in user message:
    │     "Relevant context from knowledge base:
    │      KB Result Chunk 01: /path/to/middleware.go (lines 42–81)
    │      <chunk text>
    │      KB Result Chunk 02: /path/to/README.md (lines 12–51)
    │      <chunk text>"
    └── inference proceeds as normal — model sees your actual code
```

**Chunking strategies** — the chunker dispatches by file type for structure-aware splits:

| File type | Strategy | Boundaries |
|-----------|----------|------------|
| `.go` | Declaration-based | Each top-level `func`, `type`, `var`, `const` = one chunk |
| `.md` | Heading-based | Each heading + its content body = one chunk; label carries full heading path |
| `.yaml`, `.yml`, `.toml` | Key-value blocks | Top-level key groups |
| Everything else | Prose/paragraph | Paragraphs grouped up to 1600 runes per chunk |

Each chunk text is prefixed with `File: path/to/file.go` metadata before embedding to improve retrieval relevance.

**Hybrid scoring** — KB search combines cosine similarity with keyword boosting. `extractKeywords` pulls meaningful tokens from the query (splits on whitespace/punctuation, expands camelCase, keeps tokens ≥ 4 chars). Each keyword found in a chunk adds +0.05; function call patterns (`keyword(`) add an extra +0.12 to surface callsites over definitions. Total keyword boost is capped at +0.25.

KB retrieval is **always-on** when `kb.json` exists and the embed sidecar is running. Disable per-prompt with `-K / --no-kb`. The minimum injection score (`kb_min_score`, default 0.72) is configurable in `config.yaml`; `iq config show` reports the effective value. The `-d / --debug` flag adds a STEP 3 KB RETRIEVE trace showing the threshold, each chunk's source, line range, and similarity score.

Commands: `ingest` (alias: `in`), `list`, `search`, `rm`, `clear`.

```
iq kb ingest <path>     # file or directory tree
iq kb in <path>         # alias
iq kb list              # show sources with file/chunk counts and ingest time
iq kb search <query>    # raw similarity search — shows score + preview, no inference
iq kb rm <path>         # remove a source and all its chunks
iq kb clear             # wipe entire kb.json
```

`iq pry` also supports KB retrieval via `-k / --kb`.

### Inference and REPL

The **`iq ask`** command provides an interactive REPL. One-shot prompts can also be sent directly via `iq "message"`, which routes through the same pipeline. The `ask` subcommand remains available as an explicit alias.

Routes user prompts through a 6-step pipeline:

**Step 1 — CLASSIFY.** Hybrid scoring: the user input is embedded via the embed sidecar (:27000) and compared against pre-computed cue description embeddings via cosine similarity. A deterministic keyword boost (+0.10) is added when the cue name phrase (e.g. "code review", "math", "summarization") appears in the input, preventing embedding drift from silently misrouting explicit intent. The highest hybrid score wins (minimum threshold: 0.40). Falls back to `initial` if the embed sidecar is not running. Debug trace shows `method: hybrid` when a keyword boost influenced the result.

> **What embeddings are.** An embedding is a fixed-size vector of numbers — in IQ's case, 384 floats — that a neural network uses to represent the meaning of a piece of text. Networks trained on large corpora learn to place semantically similar content close together in this high-dimensional space: "explain a transformer model" and "describe how attention works" will produce vectors pointing in nearly the same direction even though they share no words. This numerical representation of meaning is the bridge between raw data and neural cognition. It enables similarity search and retrieval (vector DBs), routing and classification without generative inference, memory systems in agentic AI, and multi-modal fusion (images and text embedded into the same space so they can be compared directly). In IQ, embeddings serve triple duty: classifying prompts to cues, detecting when tools are needed, and retrieving relevant knowledge base chunks for RAG.

The cue embedding cache (`~/.config/iq/cue_embeddings.json`) is built on first use and refreshed automatically when cues change.

**Step 1b — TOOL DETECT.** Determines whether to enable read-only tools for this prompt. Three detection paths, checked in order:

1. **Forced** — `-T` flag forces tools on; `--no-tools` forces them off.
2. **File-path heuristic** — deterministic check for slash-separated paths (excluding URLs) or words ending in known source-code extensions (`.go`, `.py`, `.md`, `.json`, etc.).
3. **Embed-based signal matching** — reuses the input vector already computed in Step 1 (zero extra API calls). Compares against 5 pre-embedded tool signal descriptions via cosine similarity. If the best match exceeds the tool threshold (0.60), tools are enabled.

The 5 tool signals and the tools they cover:

| Signal | Tools | Description |
|--------|-------|-------------|
| `time_date` | `get_time` | Time, date, day of the week |
| `file_access` | `read_file`, `list_dir`, `file_info` | Read/list files, file metadata |
| `file_search` | `search_text`, `count_lines` | Search for text in files, count lines |
| `calculation` | `calc` | Math expressions, percentages, arithmetic |
| `web_search` | `web_search` | Current events, latest news, up-to-date facts, live web lookup |

Tool signal embeddings are cached in `~/.config/iq/tool_embeddings.json` and versioned with an FNV32a hash over signal names and descriptions so they auto-refresh when signals change.

**Step 2 — ROUTE.** Resolves sidecar from the cue. Priority: cue direct model override → cue `suggested_tier` → fast fallback → cross-tier fallback → error.

**Step 3 — KB RETRIEVE.** If `kb.json` exists and the embed sidecar is running (and `--no-kb` is not set), the top-3 most similar chunks are retrieved via hybrid scoring. Only chunks whose score meets the minimum threshold (`kb_min_score`, default 0.72; configurable in `config.yaml`) are injected as plain text context in the user message. If no chunks clear the threshold, KB injection is skipped entirely. Skipped silently if KB is empty or unavailable.

**Step 4 — ASSEMBLE.** Combines system prompt (from cue, plus tool instructions if tools enabled), session history (if any), and user message (with KB context prepended if any) into the structured message array sent to inference. `--dump-prompt <file>` writes this array as indented JSON and exits before inference (use `-` for stdout).

**Step 4b — CACHE CHECK.** Computes an FNV64a hash over the assembled message array and model ID, then looks up the hash in `~/.config/iq/response_cache.json`. On a hit (entry exists and is within the 1-hour TTL), the cached response is returned immediately and inference is skipped entirely. Disabled in session mode, when tools are enabled (tool results depend on live execution), and via `--no-cache`.

**Step 5 — INFERENCE LOOP.** Sends to the target sidecar via `POST /v1/chat/completions`. Non-tool path streams tokens to stdout by default; trace mode (`-d`) forces non-streaming to prevent stdout/stderr interleaving from corrupting the debug output.

When tools are enabled, inference takes one of two paths based on how tools were detected:

**Embed short-circuit path** (`tt.Reason == "embed"`): When Step 1b identifies a tool signal via embedding, IQ skips the routing grammar pass entirely and executes the identified tool directly. `GuardArgs` builds the argument map from the user input (e.g., for `calc`, `extractCalcExpression` converts natural language to a valid math expression). If the tool succeeds, output is printed directly. If it fails (or `GuardArgs` returns nil for unextractable args), IQ falls back to direct inference with the cue's plain system prompt (tool instructions stripped to prevent re-invocation). This path covers all embed-detected tool signals: `time_date`, `file_access`, `file_search`, `calculation`, and `web_search`.

**Routing grammar path** (`tt.Reason == "file path"` or `"forced"`): Used when file-path heuristics or `--tools` flag triggered tool mode rather than embed signal. Runs in a non-streaming loop:
1. **Pass 1 — routing grammar.** The request includes a `routing_grammar` field listing available tool names. The custom `infer_server.py` sidecar uses a `RoutingGrammarProcessor` (logits processor) to constrain the model's first tokens to one of `<tool:NAME>` or `<no_tool>`, then generates freely. This forces a structural routing decision before the model can fabricate an answer.
2. **Route parse.** IQ parses the routing prefix with `parseRoutingPrefix()`. If `<tool:NAME>`, arguments are extracted by `parseRoutingArgs()` which handles valid JSON, broken JSON (unquoted keys, `=` separators), and CLI flag formats (`--key=value`). IQ executes the tool; if it succeeds, output is printed directly to the user (no pass 2). If it fails, the error is injected and the model is called again (pass 2) to explain.
3. **Passes 2+ — standard tool loop** (up to 5 iterations). IQ's parser extracts `<tool_call>` blocks — handles correct JSON, broken JSON (regex fallback), wrong tag names (`<get_time>` instead of `<tool_call>`), `<tool:NAME>` routing prefix format on follow-up passes, unclosed tags, and markdown-fenced JSON. Successful tool output is printed directly; errors trigger another inference pass. Loop ends when no tool calls remain, or after 5 iterations.

**Thinking model support** — models like DeepSeek-R1 that emit `<think>...</think>` reasoning blocks are handled transparently: during streaming, think-block tokens are buffered in memory (not echoed to the user); the clean result is printed after stripping. Any tokens streamed before `<think>` is detected are tracked so they are not re-printed when the final result is shown. Non-streaming mode strips think blocks from the full response.

**Step 5b — CACHE WRITE.** On a cache miss, stores the inference response in `response_cache.json` keyed by the same FNV64a hash from Step 4b. Expired entries (>1 hour) are pruned on write. Skipped when cache is disabled or session mode is active.

**Step 6 — PERSIST.** Appends the turn to `~/.config/iq/sessions/<id>.yaml`. Reads and writes are protected by `syscall.Flock` advisory locks (`<id>.yaml.lock`) — shared for reads, exclusive for writes — preventing YAML corruption from concurrent REPL instances. After the first exchange, a background goroutine asks the smallest fast-tier model to generate a short name (≤ 5 words) and description (≤ 15 words) for the session.

**Flags:**
```
-r, --cue <n>       Skip classification, use this cue directly
-c, --category <n>  Restrict auto-classification to one category
    --tier <n>      Override tier directly, bypass cue system
-s, --session <id>  Load/continue a named session
-K, --no-kb         Disable knowledge base retrieval for this prompt
    --no-cache      Disable response cache
-T, --tools         Force enable read-only tool use
    --no-tools      Disable tool use
-n, --dry-run       Trace steps 1–4, skip inference
    --dump-prompt <f> Write assembled messages as JSON (- for stdout), skip inference
-d, --debug         Trace all steps including inference
    --no-stream     Collect full response before printing
```

**REPL mode** — entered when no message arg and stdin is a terminal. Supports `/cue`, `/session`, `/clear`, `/dry-run`, `/debug`, `/tools` (cycles auto → on → off → auto), `/help`, `/quit`. Pipe-friendly: `echo "..." | iq ask` takes the stdin path.

### Tools

> **How tool use actually works.** The model never executes anything — it is a sandboxed token predictor with no OS access, no network, and no file system. What happens: IQ's system prompt gives the model a list of tool definitions (name, description, parameter schema). When the model decides a tool would help, it emits a structured `<tool_call>` block — not an execution, just a formatted request. IQ's Go code detects that syntax, validates the call, runs the actual function, and injects the result back into the conversation as a new message. The model then continues from there. The "agentic" behaviour is a loop IQ drives: call model → check for tool calls → execute tool → append result → call model again → repeat until the model emits a plain-text response. The model cannot initiate anything between turns, cannot run in the background, and cannot do anything IQ's harness code does not explicitly handle. This is why all tools are read-only and file paths are validated before execution — IQ is the one pulling the trigger.

In **ask mode** (via `iq "<prompt>"` or `iq ask "<prompt>"`), eight read-only tools are available. All file-access tools enforce path security: only the current working directory and paths listed in `config.yaml` `tool_paths` are allowed. Paths are resolved through symlinks and checked via prefix matching.

**Execution safety** — every tool handler runs inside a goroutine with a 30-second timeout (`ExecuteTimeout`). If the handler does not return in time, the call returns a timeout error instead of blocking the inference loop. Tool output injected into the conversation context is capped at 32 KB (`MaxOutputBytes`); longer output is truncated with a marker. Each tool carries a `ReadOnly` flag (true for all current tools); future write tools will require `--confirm` mode to execute.

**Schema validation** — `ValidateCall()` checks tool calls against the registry schema before execution: tool must exist, required params present, no unknown params, correct types (string/number). `ParseCallsStrict()` wraps the permissive parser and rejects malformed calls, preventing silent parameter misinterpretation. The permissive parser (`ParseCalls`) remains the default for resilience; strict mode is available for automation and high-assurance use.

| Tool | Parameters | Description |
|------|-----------|-------------|
| `get_time` | *(none)* | Current date, time, timezone, day of week |
| `read_file` | `path` (required) | Read file contents (max 64KB) |
| `list_dir` | `path` (required) | List directory entries |
| `file_info` | `path` (required) | File size, modification time, permissions |
| `calc` | `expression` (required) | Evaluate math: `+`, `-`, `*`, `/`, `%`, parentheses, decimals |
| `search_text` | `pattern` (required), `path` | Regex search across files (max 50 matches, skips .git/vendor/etc.) |
| `count_lines` | `path` (required) | Count lines in a file |
| `web_search` | `query` (required), `count` | Search the web via DuckDuckGo with Brave fallback (default 3 results, max 20) |

The tool system prompt (`buildRoutingToolPrompt`) is appended to the system message when tools are active. It lists all available tools with their parameter schemas, the current working directory, and instructs the model to emit `<tool:TOOL_NAME>` (followed by JSON arguments) or `<no_tool>` (followed by a direct answer) as its first output. The routing grammar logits processor enforces this structurally.

### Raw Sidecar Access

The **`iq pry`** command bypasses the IQ prompt pipeline, sending a message directly to a specific sidecar for debugging and model exploration.


```
iq pry <model|tier> [flags] <message>

-c, --cue <n>       Use a cue's system prompt
-s, --system <text> Use a literal system prompt
-k, --kb            Retrieve knowledge base context (prepended to system prompt)
-S, --no-stream     Collect full response before printing
```

`--cue` and `--system` are mutually exclusive. Accepts a tier name or specific model ID. Prints routing info in gray before the response and elapsed time after.

### Benchmarking

The **`iq perf`** command evaluates model performance using an embedded benchmark corpus. Results are stored in `~/.config/iq/benchmarks.json`.

Benchmark types:
- **KB retrieval** — measures search quality (MRR = Mean Reciprocal Rank)
- **Cue classification** — measures accuracy and average similarity score against the embedded benchmark corpus
- **Tool use** — sends 14 prompts (2 per tool) through the routing grammar pipeline; measures routing accuracy (did the model pick the correct tool?) and execution success rate. Use `-v` for per-prompt debug detail.
- **Inference latency** — measures P50/P95 latency and throughput

**Multi-model comparison** — `--models m1,m2,m3` runs the same benchmark across multiple models and prints a comparison table at the end. For embed-type benchmarks (kb, cue), temporary sidecars are spun up as needed. For infer/tool, `bench` auto-starts the sidecar if not running (using the model's configured tier, defaulting to `fast`) and stops it after the run.

**Automated sweep** — `iq perf sweep --models m1,m2 --tier fast --type infer` automates the full tier-assignment cycle: for each model it temporarily assigns to the tier, starts the sidecar, runs benchmarks, stops the sidecar, and restores the original tier config. Produces a comparison table at the end.

**Model not downloaded** — if a model's HuggingFace snapshot is missing, both `bench` and `sweep` print a red hint: `model not downloaded — run: iq lm get <model>`.

Commands:
```
iq perf bench [--type <type>] [--model <id>] [-v]             # run benchmarks
iq perf bench --type cue --models model-a,model-b,model-c     # compare models
iq perf sweep --models m1,m2 --tier fast --type infer          # automated sweep
iq perf show [model] [type]                                    # display stored results
iq perf clear                                                  # wipe benchmark history
```

### Embed Sidecar

A single Python process (`embed_server.py`, embedded in the Go binary) runs on port 27000. It uses `mlx-embedding-models` to serve embedding requests over HTTP.

**Model-specific handling:**
- **nomic** models: `"search_query: "` / `"search_document: "` instruction prefixes
- **mxbai** models: `"Represent this sentence for searching relevant passages: "` prefix (query only)
- **bge** models (default): no prefix, max 1600 runes per text

The default embed model is `mlx-community/bge-small-en-v1.5-bf16` (384-dimensional vectors).

**Dependencies:**
```
pipx install mlx-lm
pipx inject mlx-lm mlx-embedding-models
```

### Dev Hot-Reload for Python Sidecars

Both `infer_server.py` and `embed_server.py` are embedded in the Go binary via `//go:embed` and extracted to `~/.config/iq/` on first start. If the file already exists at that path, the write is skipped — so edits to `~/.config/iq/infer_server.py` or `~/.config/iq/embed_server.py` take effect on the next `iq start` without a Go rebuild.

To reset to the embedded version, delete the file and restart: `rm ~/.config/iq/infer_server.py && iq start`.

### Web Search Library

A `Client` struct in `internal/search` carries configuration (Brave API key) and per-instance rate-limit state, replacing the former package-level variable and eliminating a latent data race on concurrent access. Primary backend is DuckDuckGo HTML scraping; Brave Search API serves as a JSON-based fallback when `brave_api_key` is configured.

- **Client struct**: `search.NewClient(braveAPIKey)` — each client owns its own rate limiter and config; wired via `tools.SetSearchClient()` at prompt startup
- **Rate limiter**: 1-second minimum interval between DDG requests (per-client `sync.Mutex` + `time.Time`)
- **Pinned CSS selectors**: `.result`, `.result__title a`, `.result__url`, `.result__snippet` — validated by HTML fixture test
- **Retry logic**: exponential backoff on DDG 202 (throttling) responses, up to 3 retries
- **Brave fallback**: if DDG fails and `brave_api_key` is set in config.yaml, queries the Brave Search API
- **Public API**: `Client.Search()`, `Client.SearchWithOption()` — method-based; free-function wrappers `Search()`, `SearchWithOption()` use a default client for backward compatibility


## File Layout

```
~/.config/iq/
├── config.yaml                  # tier pool assignments + embed model + tool_paths + brave_api_key + pipeline
├── models.json                  # manifest of downloaded models (id, pulled_at, hf_cache_path, task)
├── cues.yaml                    # cue definitions (seeded from embedded defaults)
├── cue_embeddings.json          # cue description embeddings (auto-built, versioned)
├── tool_embeddings.json         # tool signal embeddings (auto-built, FNV32a versioned)
├── response_cache.json          # inference response cache (FNV64a keyed, 1h TTL)
├── kb.json                      # knowledge base: chunk text + 384-float vectors (RAG)
├── infer_server.py              # extracted inference sidecar script (see dev hot-reload below)
├── embed_server.py              # extracted embedding sidecar script (see dev hot-reload below)
├── benchmarks.json              # performance benchmark results
├── run/
│   ├── <model-slug>.json        # generative sidecar state (PID, port, tier, model)
│   └── <model-slug>.log
└── sessions/
    ├── <id>.yaml                # conversation history per session
    └── <id>.yaml.lock           # advisory flock file (created on first access)

~/.cache/huggingface/hub/
└── models--org--repo/
    ├── blobs/                   # actual file content (deduplicated)
    └── snapshots/
        └── <hash>/              # symlinks into blobs/ — this is --model path
            ├── config.json
            ├── model.safetensors
            └── tokenizer.json
```

## Data Flow: Prompt Execution

The diagram below shows how a user prompt flows through IQ’s internal pipeline, from ingestion to final output. All steps are executed locally via sidecars and orchestrated by the CLI, incorporating cue classification, tool detection, knowledge base retrieval, caching, inference, and session persistence.

```
User input
    │
    ├── --cue given? ──────────────────────────────────────────┐
    │                                                          │
    ▼  (auto-classify)                                         ▼ (skip classify)
STEP 1  CLASSIFY — POST /embed → embed :27000                    resolve cue directly
  input text → 384-float input vector                          │
    │                                                          │
    ▼                                                          │
  cosine_similarity(input_vec, cue_vecs[])                     │
  best score ≥ 0.40 → cue name                                 │
    │                                                          │
    ▼                                                          │
  highest-score cue name ◄─────────────────────────────────────┘
    │
    ▼
STEP 3  KB PREFETCH (async, launched here — runs concurrently with 1b and 2)
  goroutine: POST /embed → query vector → hybrid search → top-3 chunks
  5s timeout — on timeout or error, prompt proceeds without KB context
    │                     ┌─ concurrently ─┐
    ▼                     │                │
STEP 1b  TOOL DETECT      │                │
  -T/--no-tools flag? → forced             │
  hasFilePath(input)? → enabled            │
  else: cosine_similarity(input_vec,       │
        tool_signal_vecs[])                │
  best score ≥ 0.60 → tools enabled        │
    │                     │                │
    ▼                     │                │
STEP 2  RESOLVE ROUTE     │                │
  cue.model override  →  pickSidecar      │
  cue.suggested_tier  →  pickSidecar      │
  fallback            →  pickSidecar       │
    │                     │                │
    ▼                     └────────────────┘
STEP 3  KB COLLECT  (blocks with 5s timeout to receive async KB result)
  received: top-3 chunks (score ≥ 0.72) → plain text context block
  timeout/error: proceed without KB context
    │
    ▼
STEP 4  ASSEMBLE
  system:    cue.system_prompt + tool instructions (if tools enabled)
  ...        session history (if -s)
  user:      kb_context (if any) + input
    │
    ▼
STEP 4b CACHE CHECK  (if !session && !tools && !--no-cache)
  FNV64a hash of messages[] + model ID → lookup response_cache.json
  ├── hit (within 1h TTL): return cached response, skip to STEP 6
  └── miss: continue to inference
    │
    ▼
STEP 5  INFERENCE LOOP  (skipped on cache hit)
  ├── no tools: SSE stream → stdout (token by token); non-streaming in trace mode
  └── tools: two dispatch paths
       embed short-circuit (reason=embed): execute tool directly, skip grammar pass
         ├── tool succeeds: print output directly
         └── tool fails / no args: fall back to direct inference (plain system prompt)
       routing grammar path (reason=file path|forced):
         pass 1: routing grammar → model emits <tool:NAME> or <no_tool>
         if <tool:NAME>: execute tool → print output directly (pass 2 only on error)
         passes 2+: parse <tool_call>/<tool:NAME> blocks → execute → print or re-infer
         loop until no tool calls remain (up to 5 iterations)
    │
    ▼
STEP 5b CACHE WRITE  (on cache miss, stores response)
    │
    ▼
STEP 6  PERSIST
  append turn to session YAML
  background: auto-name via smallest fast-tier (first turn only)
```

## Debug Trace Format

IQ prints a detailed **debug trace** of each step when run with **`-d` or `--debug`**. Each step prints a clean header and structured sub-fields:

```
STEP 1  CLASSIFY
  task          Cosine-similarity match user input against 17 cue descriptions
  call          embed bge-small-en-v1.5-bf16 @ localhost:27000
  resolved_cue  initial (score: 0.5457)
  elapsed       40ms

STEP 1b TOOL DETECT
  task          Cosine-similarity match input vector against 5 tool signal descriptions
  best_signal   time_date (score: 0.72)
  result        enabled (embed)
  elapsed       1ms

STEP 2  RESOLVE ROUTE
  task          Map resolved cue to model tier and running sidecar
  model         Llama-3.2-3B-Instruct-4bit @ localhost:27001
  cue           initial → general/fast
  tier_source   suggested_tier
  elapsed       0ms

STEP 3  KB RETRIEVE
  task          Cosine-similarity search user input against KB chunks
  call          embed bge-small-en-v1.5-bf16 @ localhost:27000
  threshold     0.72
  chunks        3 results
  top           score:0.9053  cues_default.yaml:1–201
  top           score:0.8298  prompt.go:293–313
  top           score:0.8227  arch.md:226–298
  elapsed       66ms

STEP 4  ASSEMBLE
  task          Combine system prompt, session history, and user message into message array
  [system]
    ...
  [user]
    ...

STEP 4b CACHE CHECK
  task          Hash messages and check response cache
  key           a3f7c2e1deadbeef
  result        miss
  elapsed       0ms

STEP 5  INFERENCE LOOP
  task          Send assembled messages to model sidecar for generation
  mode          embed short-circuit: time_date (skipping routing grammar)
  tool_call     get_time(null)
  tool_result   2026-03-08 14:57:17 EDT (Sunday)
  elapsed       2ms

  # Routing grammar path (reason=file path or forced):
  # PASS 1        routing grammar
  # call          POST localhost:27001/v1/chat/completions
  # raw_resp      "<tool:read_file>"
  # route         <tool:read_file>
  # tool_call     read_file({"path":"go.mod"})
  # tool_result   module github.com/...
  # latency 1     320ms
  # elapsed       320ms

  # If tool fails, pass 2 is called to explain the error:
  # PASS 2        explain tool result
  # call          POST localhost:27001/v1/chat/completions
  # raw_resp      "The file could not be read because..."
  # latency 2     1200ms

STEP 5b CACHE WRITE
  task          Store response in cache
  key           a3f7c2e1deadbeef
  ttl           60m
  elapsed       0ms

STEP 6  SESSION
  task          Persist conversation to disk
  id            abc123
  saved         ~/.config/iq/sessions/abc123.yaml
  turns         1
  elapsed       0ms
```

Dry-run mode (`-n`) prints Steps 1–4 only, skipping inference.


## Source Files

### Domain Packages

| File | Purpose |
|------|---------|
| `internal/config/config.go` | Config struct, Load/Save, tier helpers, embed model, kb_min_score, legacy migrations |
| `internal/search/search.go` | DuckDuckGo HTML search client, retry logic, result parsing |
| `internal/sidecar/sidecar.go` | Sidecar state, lifecycle (start/stop), port allocation, pool dispatch, process helpers |
| `internal/sidecar/transport.go` | OpenAI-compatible HTTP transport: ChatRequest, Call, CallWithGrammar, Stream, RawCall, StripThinkBlocks |
| `internal/sidecar/infer_server.py` | Custom MLX inference sidecar with routing grammar support (embedded in binary) |
| `internal/cue/cue.go` | Cue struct, Load/Save, Find, ForModel, embedded default YAML |
| `internal/cue/cues_default.yaml` | 17 default cues across 8 categories (embedded in binary) |
| `internal/embed/embed.go` | Embed sidecar lifecycle, HTTP embedding calls, cosine similarity, cue classifier |
| `internal/embed/embed_server.py` | Python embedding sidecar (MLX-based, embedded in binary) |
| `internal/cache/cache.go` | Response cache with FNV64a hashing, TTL expiry, check/write |
| `internal/tools/tools.go` | Tool registry (8 tools), parser, executor, tool signals, embed-based detection |
| `internal/tools/tools_test.go` | Tests for calcEval, extractCalcExpression, ParseCalls, ValidatePath, HasFilePath, routing, registry |
| `internal/lm/lm.go` | HF API types/client, manifest CRUD, cache helpers, model param/quant parsing, formatting |
| `internal/kb/kb.go` | KB index types, chunking strategies, hybrid search, ingest, persistence |

### CLI Package

| File | Purpose |
|------|---------|
| `cmd/iq/main.go` | CLI entry point, root command, version, help routing |
| `cmd/iq/svc.go` | Status display, tier/embed commands, thin wrappers for sidecar package |
| `cmd/iq/cue.go` | Cue CLI commands (list, show, add, edit, rm, assign, reset, sync) |
| `cmd/iq/prompt.go` | 8-step execution pipeline, session management, REPL, trace output |
| `cmd/iq/prompt_test.go` | End-to-end orchestration tests with mock sidecar (httptest) |
| `cmd/iq/tools.go` | Tool trace helpers (printToolCallTrace, printToolResultTrace, printToolStatus) |
| `cmd/iq/kb.go` | KB CLI commands (ingest, list, search, rm, clear) |
| `cmd/iq/lm.go` | Cobra commands for lm search/get/list/show/rm (thin shim over internal/lm) |
| `cmd/iq/perf.go` | Benchmark corpus, bench/show/clear commands, metrics |
| `cmd/iq/cfg.go` | `iq config` — show effective config, validate config files |
| `cmd/iq/probe.go` | `iq pry` — raw sidecar access |
| `cmd/iq/bench_corpus.yaml` | Benchmark test data (embedded in binary) |


## Version History

| Version | Summary |
|---------|---------|
| 0.8.19  | `pipeline: single_pool` mode (FEAT9810): `PipelineSinglePool` constant; `pickAnySidecar` picks first live non-embed sidecar; `resolveSinglePool` routes with cue system prompt but no tier discrimination; pipeline guard replaced with switch; 2 new tests |

<details>
<summary>Older versions (v0.2.7 – v0.8.18)</summary>

| Version | Summary |
|---------|---------|
| 0.8.18  | Config schema versioning (FEAT9840): `version:` field in config.yaml; `ConfigVersion = 1`; version-dispatched `Load` (v0 → migration chain, v>current → error); `migrateV0` extracts all legacy migration logic; `normalizeConfig` helper; 3 new schema version tests |

| Version | Summary |
|---------|---------|
| 0.8.17  | Stale sidecar state / port exhaustion (FEAT9860): `NextAvailablePort` uses `AllLiveStates` (skips dead PIDs); `StartInfer` and `StartSidecar` remove state + kill process on early crash and readiness timeout; 3 new port-allocation tests |
| 0.8.16  | `RawCall` timeout + status-code guard (FEAT9870): swap bare `http.Post` for `inferClient` (`http.Client{Timeout: 5m}`); explicit non-200 error with status code and body; `Stream` unchanged (timeout would cancel mid-stream); `TestRawCallNonOK` added |
| 0.8.15  | Synthesis pass for `read_file` short-circuit: after file is read, model answers original question using content (same pattern as `web_search`); fixes "does arch.md have version history?", "print last 10 lines", etc.; other file tools remain self-contained (FEAT9872) |
| 0.8.14  | `GuardArgs` for `read_file`/`file_info`: extract filename from natural language via `extractFilePath`/`looksLikeFilePath`; handles "tail arch.md", "print main.go", "does file X have…"; nil on no-match → falls back to inference; 9 new unit tests (FEAT9874) |
| 0.8.13  | Fix embed short-circuit tool selection: `SelectTool(signal, input)` replaces hard-coded `expected[0]`; keyword dispatch routes `file_access` to `list_dir`/`file_info`/`read_file` and `file_search` to `count_lines`/`search_text`; `GuardArgs` for `list_dir` extracts path or defaults to "."; 15 new unit tests (FEAT9875) |
| 0.8.12  | Fix `RemoveSource` prefix collision: replace bare `strings.HasPrefix` with exact-path + directory-boundary match (`== absPath \|\| HasPrefix(absPath+"/")`) to prevent silent deletion of sibling paths; add `internal/kb/kb_test.go` with 4 table tests (FEAT9880) |
| 0.8.11  | Test coverage expansion: `ParseCallsStrict` table tests, `resolveRoute` tier fallback, sidecar transport (`RawCall`/`Call`/`StripThinkBlocks`) with httptest, config migration paths (`migrateFlatTiers`/`migrateOldFourTier`/legacy embed model), embed (`CosineSimilarity`/`TextsOnPort`/`keywordScore`); aggregate coverage 11.8%→15.1% (FEAT9890) |
| 0.8.10  | Replace `queone/utl` color wrappers with zero-dependency `internal/color` package; TTY detection via `os.Stdout.Stat()`, respects `NO_COLOR`/`TERM=dumb`; rename `Gre`→`Grn`; binary shrinks ~110KB; removes 7 transitive dependencies (FEAT9900) |
| 0.8.9   | Error handling audit: config parse errors now surfaced (was silently swallowed); DDG body leak fixed; `web_search` fallback parser extracts `query` arg; `BuildRoutingPrompt`/`RegistryNames`/`ParseCalls`/`ParseCallsStrict` accept explicit `[]Tool` registry; `build.sh` suppresses PASS lines, adds weighted coverage summary and `-v` flag (FEAT9910) |
| 0.8.8   | Extended inference parameters: `top_p`, `min_p`, `top_k`, `stop`, `seed` added to `InferParams`/`ResolvedParams`; threaded through `ChatRequest`, `Call`/`CallWithGrammar`/`Stream`, and `infer_server.py`; nil = unset (mlx_lm defaults); stop sequences trim post-generation; `iq config` surfaces all 8 params with mlx_lm defaults annotated (FEAT9920) |
| 0.8.7   | `TestHelpFlagCoverage` uses `VisitAll` to assert every registered flag appears in hand-crafted help output; fixed genuine drift: `--dump-prompt` missing from `iq ask -h`, `--kb` missing from `iq pry -h`; build.sh highlights FAIL lines in red (FEAT9930) |
| 0.8.6   | Advisory `flock` locking for session reads/writes (`syscall.Flock`); shared lock on load, exclusive on save; `.lock` sidecar file per session; concurrent-write unit test (FEAT9940) |
| 0.8.5   | `pipeline:` mode selector in config.yaml (`two_tier` default); single `config.Load` at top of `executePrompt` replaces 4 internal loads; pipeline validation at entry; `iq config` shows effective pipeline mode (FEAT9950) |
| 0.8.4   | `NewRegistry()` constructor replaces `init()` for tools package; `Registry` global initialized via constructor; unit test for tool count, names, and instance isolation (FEAT9960) |
| 0.8.3   | Idiomatic Go naming: `program_name`/`program_version` → `programName`/`programVersion` (FEAT9980); `errSilent` sentinel replaced with named `silentErr` type so `errors.Is` works correctly (FEAT9970) |
| 0.8.2   | `kb_min_score` configurable in config.yaml; STEP 3 trace shows threshold and zero-result message; `traceBlock` truncates KB chunk content to 4-line preview; trace mode forces non-streaming to fix debug output corruption; fix double-print of pre-think tokens in streaming think models |
| 0.8.1   | Embed short-circuit generalized to all tool signals (FEAT9990): skip routing grammar pass when embed is confident; `extractCalcExpression` converts natural language to math for calc tool; fallback strips tool system prompt to prevent re-invocation markup leak |
| 0.8.0   | `iq perf bench` auto-starts/stops sidecar for infer/tool types; red download hint for missing models in bench and sweep (`lm.IsModelNotDownloaded`) |
| 0.7.9   | Per-tier tuning guide in arch.md; `short|long` alias format in help; trim trailing blank lines from all help output; README "Find Your Best Models" section |
| 0.7.8   | Async KB prefetch with 5s timeout; `iq perf sweep` automates model comparison; README onboarding guide |
| 0.7.7   | `iq config show/validate`: canonical config inspection and validation command |
| 0.7.6   | Hybrid cue classification: keyword boost prevents embedding drift; strict tool schema validation (ValidateCall/ParseCallsStrict); multi-model benchmark harness (--models flag) |
| 0.7.5   | Concurrency-safe `search.Client` struct replaces package-level braveAPIKey; tool execution safety: 30s timeout, 32KB output cap, ReadOnly/confirm gating |
| 0.7.4   | Extract sidecar HTTP transport to `internal/sidecar/transport.go`; extract LM domain logic (~500 lines) to `internal/lm/lm.go`; `cmd/iq/lm.go` becomes thin CLI shim |
| 0.7.3   | `--dump-prompt` flag writes assembled message array as JSON before inference; end-to-end orchestration test with mock sidecar (httptest); build.sh v2.2.0: indented output, `-v` tests, green command echo |
| 0.7.2   | Housekeeping: rename search.SearchParam/SearchResult → Param/Result; unify chatMessage/cache.Message into config.Message; broaden MlxVenvPython fallback paths (PIPX_HOME, /opt/homebrew/bin); config.Load resilient to read-only filesystems |
| 0.7.1   | Web search hardening: rate limiter, pinned CSS selectors with fixture test, Brave Search API fallback; config.yaml populates all defaults on first creation |
| 0.7.0   | Configurable inference parameters: per-tier and global `repetition_penalty`, `temperature`, `max_tokens`; structured `TierConfig` with auto-migration from flat-list format; temperature support in `infer_server.py` |
| 0.6.15  | Add test assertion for tool/signal registry coverage drift |
| 0.6.14  | Replace 24-bit timestamp session IDs with 32-bit crypto/rand |
| 0.6.13  | Python sidecar dev hot-reload from `~/.config/iq/`; fix `~` not expanded in PATH for `mlx_lm.server` lookup; move tools tests to `internal/tools` |
| 0.6.12  | Update arch.md section headers and file paths; minor build.sh adjustment |
| 0.6.11  | Extract `kb` to `internal/kb` domain package — completes `internal/` restructuring |
| 0.6.10  | Extract `tools` to `internal/tools` domain package |
| 0.6.9   | Extract `cache` to `internal/cache` domain package |
| 0.6.8   | Extract `embed` to `internal/embed` domain package |
| 0.6.7   | Extract `cue` to `internal/cue` domain package |
| 0.6.6   | Extract `sidecar` to `internal/sidecar` domain package |
| 0.6.5   | Extract `search` to `internal/search` domain package |
| 0.6.4   | Begin `internal/` restructuring — extract `config` as first domain package; planned: search, sidecar, embed, cache, tools, kb |
| 0.6.3   | Web search tool: DuckDuckGo integration via `web_search` tool and embed signal; short-circuit skips routing grammar for web queries; synthesis prompt with date injection; toolMinScore 0.66→0.60 |
| 0.6.2   | Tool use benchmark (`iq perf bench --type tool`): 14 prompts across 7 tools, measures routing accuracy and execution success; `-v` flag for per-prompt debug detail |
| 0.6.1   | Robust tool arg parsing (broken JSON, unquoted keys, `=` separators, `--flag=value` CLI format); print successful tool output directly instead of pass 2 re-inference; inject cwd into tool system prompt; PASS/GUARD/latency debug trace format; parse `<tool:NAME>` routing prefix on follow-up passes |
| 0.6.0   | TASK label `feature-extraction` displayed as `embedding` (green); `lm rm` auto-stops sidecars and clears tier/embed assignments with yellow warnings instead of blocking; yellow confirmation prompt; README documents HF as official registry with token recommendation |
| 0.5.11  | Flatten CLI: promote `iq svc` subcommands to root (`iq start/stop/status/doc/tier/embed`); `iq svc` kept as hidden backward-compat alias |
| 0.5.10  | Display raw HF pipeline_tag (lowercase with hyphens); local task inference from config.json as fallback when HF returns no tag (checks vision indicators before model_type) |
| 0.5.9   | Model task display: show HF pipeline_tag (TASK column) in lm search/list/show with green/red color coding; warn on non-text-generation downloads; cache task in manifest with parallel backfill |
| 0.5.8   | VLM guard: reject vision-language models at svc start (checks config.json for vision indicators); early crash detection via cmd.Wait() goroutine replaces zombie-prone signal-0 check for immediate failure reporting |
| 0.5.7   | Routing grammar: replace mlx_lm.server with custom infer_server.py sidecar supporting constrained decoding via logits processors; routing grammar forces `<tool:NAME>` or `<no_tool>` prefix on pass 1; tool guard direct-calls tool when model picks `<no_tool>` despite embed signal; toolMinScore 0.72→0.66 |
| 0.5.6   | Move Step 1b before Step 2; tool guard reprompt on pass-1 simulation; disable cache when tools enabled; document tool execution model in arch.md |
| 0.5.5   | Arg validation UX: yellow error + command help on wrong args |
| 0.5.4   | Tune KB and tool thresholds: kbMinScore 0.50→0.72, kbDefaultK 5→3, toolMinScore 0.50→0.72; use kbDefaultK constant in all call sites; instruct model to use tool results on follow-up pass |
| 0.5.3   | Response cache (Steps 4b/5b): FNV64a-keyed response cache with 1h TTL, --no-cache flag; rename Step 4→ASSEMBLE, Step 5→INFERENCE LOOP; capitalize all step names; add pass numbers to tool loop trace; add call trace for non-tool path |
| 0.5.2   | Fix `iq pry` to resolve embed sidecar by model ID; reject embed models with clear error instead of 404 |
| 0.5.1   | Architecture docs rewritten: add tool system, perf/bench, debug trace format, embed sidecar details, hybrid KB scoring, structure-aware chunking, source file map; fix diagram and data flow |
| 0.5.0   | Embed-based tool detection replaces keyword lists; reuse input vector from classify step (zero extra API calls); new debug trace format with step headers, call/task sub-fields, and Step 1b tool detect |
| 0.4.8   | Consolidate 58→17 cues across 8 categories; keyword-rich descriptions for embedding separation; lower classifyMinScore 0.68→0.40; bench accuracy 29%→100% (28/28); print threshold in bench output |
| 0.4.7   | Root-level prompts (`iq "message"`); `-?` help alias; extract `addPromptFlags` helper |
| 0.4.6   | Skip embed sidecar start when model not downloaded (immediate hint); print last log lines on embed sidecar timeout |
| 0.4.5   | First-run hint for `iq svc start` when no tier models configured; update Quick Start with recommended defaults |
| 0.4.4   | Merge dual embed sidecars into single `embed` sidecar on :27000; default to bge-small-en-v1.5-bf16; auto-migrate cue_model/kb_model → embed_model |
| 0.4.3   | Rename `iq probe` → `iq pry` (probe kept as alias) |
| 0.4.2   | Rename `iq prompt` → `iq ask` (prompt kept as alias); add pre-commit checklist to CLAUDE.md |
| 0.4.1   | fix: version bump, remove Ollama from docs, fix diagram alignment |
| 0.4.0   | Replace Ollama with local MLX embed sidecars (embed_server.py, cue :27000 / kb :27001); fix mxbai int attention-mask via _construct_batch patch; mlx-lm decoder fallback for Qwen3-Embedding; registerInManifest for embed models; embed model guard in lm rm; build.sh auto-commit/tag/push; cue classifier confidence threshold (0.68); KB RAG uses cue system prompt instead of hardcoded reading-comprehension template; architecture docs purged of Ollama references |
| 0.3.1   | MLX embed sidecars, dual embed roles (cue/kb), hybrid KB retrieval, RAG quality improvements |
| 0.3.0   | RAG knowledge base (iq kb), KB retrieval in prompt and probe |
| 0.2.10  | switch embed library to mlx-embedding-models, fix BertTokenizer compat |
| 0.2.9   | embedding-based classification, normalise suggested_tier values |
| 0.2.8   | rename role→cue, add initial fallback cue, probe --cue flag |
| 0.2.7   | Initial public release |

</details>
