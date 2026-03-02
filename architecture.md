# IQ Architecture

## Overview

IQ is a local LLM orchestration tool for Apple Silicon. It manages the full lifecycle of MLX-format language models — discovery, download, tier assignment, cue management, knowledge base, runtime serving, and intelligent prompt routing — through a unified CLI. All inference runs locally with no cloud dependency.

---

## System Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                               iq CLI (Go)                                   │
│                                                                             │
│  iq lm    iq svc    iq cue    iq kb    iq ask       iq pry      iq status   │
│  (models) (service) (cues)   (RAG)    (infer/REPL) (raw debug) (alias: st)  │
└────┬──────────┬──────────┬────────┬───────┬─────────┬────────────┬──────────┘
     │          │          │        │       │         │            │
     ▼          ▼          ▼        ▼       ▼         ▼            ▼
┌─────────┐ ┌──────┐ ┌────────┐ ┌──────────────────────┐ ┌──────────────────┐
│ HF      │ │config│ │cues    │ │ mlx_lm.server        │ │ sessions/        │
│ cache   │ │.yaml │ │.yaml   │ │ sidecars (pool)      │ │ <id>.yaml        │
│         │ │      │ │        │ │                      │ │                  │
│~/.cache/│ │tiers:│ │name    │ │ fast pool :27002+    │ │ kb.json          │
│hugging  │ │ fast │ │category│ │ slow pool :27002+    │ │ (vector index)   │
│face/hub/│ │ slow │ │desc    │ │                      │ │                  │
│models-- │ │      │ │prompt  │ │ OpenAI-compatible    │ └──────────────────┘
│org--repo│ │      │ │tier    │ │ HTTP API             │ ┌──────────────────┐
│/snapshot│ └──────┘ └────────┘ └──────────────────────┘ │ embed sidecars   │
│  /hash/ │                                              │ :27000 | :27001  │
└─────────┘                                              └──────────────────┘
```

---

## Components

### `iq lm` — Model Management

Handles the full model lifecycle. Models are downloaded from [mlx-community](https://huggingface.co/models?filter=mlx) via the `hf` CLI and stored in the standard HuggingFace cache at `~/.cache/huggingface/hub/`. A manifest at `~/.config/iq/models.json` tracks what IQ knows about.

Key operations: `search`, `get`, `list`, `show`, `rm`.

`iq lm search` queries the HF API, enriches results in parallel (one goroutine per model) to populate DISK and EST MEM, and displays DISK / PARAMS / EST MEM / DOWNLOADS. Accepts an optional query string or a numeric count (e.g. `iq lm search 100`).

`iq lm get` infers a suggested tier from disk size (< 2GB → fast, else slow) and prints the `iq svc tier add` command to assign it.

`iq lm rm` refuses to remove a model assigned to a tier or whose sidecar is running.

### Configuration

Manages `~/.config/iq/config.yaml`. Configuration commands live under `iq svc` — there is no separate `iq cfg` command. Tiers are **pools** — each tier holds a list of model IDs, not a single slot.

```
fast    sub-2GB models — used for quick inference tasks
slow    2GB+ models    — used for quality inference
```

Tier commands: `iq svc tier show`, `iq svc tier add <tier> <model>`, `iq svc tier rm <tier> <model>`.

Embed model commands: `iq svc embed show`, `iq svc embed set <cue|kb> <model>`, `iq svc embed rm <cue|kb>`.

Auto-migration: on first load, an old four-tier config (`tiny`/`fast`/`balanced`/`quality`) is silently converted to the two-tier pool format using the 2GB disk threshold.

### `iq cue` — Cue Definitions

Manages `~/.config/iq/cues.yaml`, seeded from an embedded default set of 56 cues across 10 categories:

```
language_tasks  generation  reasoning  code       retrieval
summarization   dialogue    safety     domain     ml_ops
```

Each cue carries a `name`, `category`, `description`, `system_prompt`, `suggested_tier`, and an optional direct `model` override (kept for power users, not actively promoted in routing).

Commands: `list`, `show`, `add`, `edit`, `rm`, `assign`, `unassign`, `reset`, `sync`.

### `iq svc` — Service Daemon

Manages sidecar processes. Each sidecar is a detached `mlx_lm.server` process. Ports are assigned dynamically starting at 27001 — no fixed port per tier. State is persisted to `~/.config/iq/run/<model-slug>.json` (PID, port, tier, model, start time). Logs go to `~/.config/iq/run/<model-slug>.log`.

Start sequence:
1. Allocate next free port from 27001+
2. Resolve HF snapshot directory (`snapshots/<hash>/`) — the path `mlx_lm.server --model` requires
3. Locate `mlx_lm.server` binary via augmented PATH (covers pipx, homebrew, venv installs)
4. Spawn detached subprocess (`Setsid: true`)
5. Poll `GET /v1/models` until 200 OK or 120s timeout
6. On failure: print last 10 log lines + path

`iq svc start/stop` accepts a tier name (acts on the whole pool), a model ID (acts on one), or no argument (all assigned models).

**Pool dispatcher (`pickSidecar`)** — scans live state files for a given tier and returns one. With `preferSmallest: true`, it returns the model with the smallest disk footprint (used by the auto-naming background goroutine).

`iq svc doc` runs preflight checks: `mlx_lm.server` found and `--model` flag supported, `mlx-embedding-models` package installed, all assigned model HuggingFace cache dirs exist.

**Embeddings** — handled by a single local Python sidecar (`embed_model`, port 27000) started with `iq svc start`. Serves both cue classification and KB indexing/retrieval. Configure via `iq svc embed`.

`iq svc status` shows TIER / MODEL / ENDPOINT / PID / UPTIME / MEM for all assigned models plus the embed sidecar row, IQ process memory, and combined total.

### `iq kb` — Knowledge Base

Manages `~/.config/iq/kb.json` — an embedded vector index for RAG (Retrieval-Augmented Generation).

> **What RAG is.** Large language models know only what was in their training data. RAG extends this by retrieving relevant passages from your own documents at query time and injecting them into the prompt as plain text context — no fine-tuning, no model modification. The model reasons over retrieved material just as it would any other text in its context window. The key insight: embeddings are used for *retrieval* (finding relevant passages by semantic similarity), but the model itself only ever sees text. Embeddings never enter the model directly in this architecture.

**How it works end-to-end:**

```
iq kb ingest ~/projects/myapp
    │
    ├── walk directory (skips .git, node_modules, vendor, __pycache__, hidden dirs)
    ├── read each text file (.go, .md, .py, .txt, .yaml, ...)
    ├── split into overlapping line-based chunks (40 lines, 5-line overlap)
    ├── embed each chunk via embed sidecar :27000 (batches of 20)
    └── store chunk text + 384-float vector in kb.json

iq ask "how does the auth middleware work?"
    │
    ├── embed user input → query vector
    ├── cosine_similarity(query_vec, all_chunk_vecs) — Go, in-memory
    ├── top-5 chunks retrieved
    ├── injected into system prompt as plain text:
    │     "Relevant context from knowledge base:
    │      ─── /path/to/middleware.go (lines 42–81) ───
    │      <chunk text>
    │      ─── /path/to/README.md (lines 12–51) ───
    │      <chunk text>"
    └── inference proceeds as normal — model sees your actual code
```

KB retrieval is **always-on** when `kb.json` exists and the embed sidecar is running. Disable per-prompt with `-K / --no-kb`. The `-d / --debug` flag adds a STEP 3 KB RETRIEVE trace showing each chunk's source, line range, and similarity score.

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

### `iq ask` — Inference and REPL

One-shot prompts can be sent directly via `iq "message"` (routes through the same pipeline). The `ask` subcommand is still available for the interactive REPL (`iq ask`) and as an explicit alias.

Routes user prompts through a pipeline:

**1. Classify** — the user input is embedded via the embed sidecar (:27000) and compared against pre-computed embeddings of all cue descriptions via cosine similarity. The highest-scoring cue is selected. No generative call, no instruction-following dependency, deterministic result. Falls back to `initial` if the embed sidecar is not running. Every prompt makes two calls: one embedding call (~10ms), then the full inference call.

> **What embeddings are.** An embedding is a fixed-size vector of numbers — in IQ's case, 384 floats — that a neural network uses to represent the meaning of a piece of text. Networks trained on large corpora learn to place semantically similar content close together in this high-dimensional space: "explain a transformer model" and "describe how attention works" will produce vectors pointing in nearly the same direction even though they share no words. This numerical representation of meaning is the bridge between raw data and neural cognition. It enables similarity search and retrieval (vector DBs), routing and classification without generative inference, memory systems in agentic AI, and multi-modal fusion (images and text embedded into the same space so they can be compared directly). In IQ, embeddings serve double duty: classifying prompts to cues, and retrieving relevant knowledge base chunks for RAG.

The cue embedding cache (`~/.config/iq/cue_embeddings.json`) is built on first use and refreshed automatically when cues change.

**2. Route** — resolves sidecar from the cue. Priority: cue direct model override → cue `suggested_tier` → fast fallback → cross-tier fallback → error.

**3. KB Retrieve** — if `kb.json` exists and the kb embed sidecar is running (and `--no-kb` is not set), the top-5 most similar chunks are retrieved and appended to the cue's system prompt as plain text context. Skipped silently if kb is empty or unavailable.

**4. Build** — assembles the message array: system prompt (cue + KB context if any), session history (if any), new user message.

**5. Infer** — sends to the target sidecar via `POST /v1/chat/completions`. Streams tokens to stdout by default.

**6. Persist** — appends the turn to `~/.config/iq/sessions/<id>.yaml`. After the first exchange, a background goroutine asks the smallest fast-tier model to generate a short name and description for the session.

**Flags:**
```
-r, --cue <n>       Skip classification, use this cue directly
-c, --category <n>  Restrict auto-classification to one category
    --tier <n>      Override tier directly, bypass cue system
-s, --session <id>  Load/continue a named session
-K, --no-kb         Disable knowledge base retrieval for this prompt
-n, --dry-run       Trace steps 1–4, skip inference
-d, --debug         Trace all steps including inference
    --no-stream     Collect full response before printing
```

**REPL mode** — entered when no message arg and stdin is a terminal. Supports `/cue`, `/session`, `/clear`, `/dry-run`, `/debug`, `/help`, `/quit`. Pipe-friendly: `echo "..." | iq ask` takes the stdin path.

### `iq pry` — Raw Sidecar Access

Bypasses the IQ prompt pipeline. Sends a message directly to a specific sidecar for debugging and model exploration.

```
iq pry <model|tier> [flags] <message>

-c, --cue <n>       Use a cue's system prompt
-s, --system <text> Use a literal system prompt
-k, --kb            Retrieve knowledge base context (prepended to system prompt)
-S, --no-stream     Collect full response before printing
```

Accepts a tier name or specific model ID. Prints routing info in gray before the response and elapsed time after.

---

## File Layout

```
~/.config/iq/
├── config.yaml                  # tier pool assignments + embed model
├── models.json                  # manifest of downloaded models
├── cues.yaml                    # cue definitions (seeded from embedded defaults)
├── cue_embeddings.json          # cosine similarity cache (auto-built, invalidated on cue changes)
├── kb.json                      # knowledge base: chunk text + 384-float vectors (RAG)
├── run/
│   ├── <model-slug>.json        # generative sidecar state (PID, port, tier, model)
│   └── <model-slug>.log
└── sessions/
    └── <id>.yaml                # conversation history per session

~/.cache/huggingface/hub/
└── models--org--repo/
    ├── blobs/                   # actual file content (deduplicated)
    └── snapshots/
        └── <hash>/              # symlinks into blobs/ — this is --model path
            ├── config.json
            ├── model.safetensors
            └── tokenizer.json
```

---

## Data Flow: Prompt Request

```
User input
    │
    ├── --cue given? ──────────────────────────────────────────┐
    │                                                          │
    ▼  (auto-classify)                                         ▼ (skip classify)
POST /embed  →  embed :27000 (embed_model)              resolve cue directly
  input text  →  384-float vector                              │
    │                                                          │
    ▼                                                          │
  cosine_similarity(input_vec, cue_vecs[])                     │
    │                                                          │
    ▼                                                          │
  highest-score cue name ◄─────────────────────────────────────┘
    │
    ▼
resolveRoute()
  cue.model override  →  pickSidecar(tier, false)
  cue.suggested_tier  →  pickSidecar(tier, false)
  fallback            →  pickSidecar("fast", false)
    │
    ▼
KB retrieve  (if kb.json exists && embed running && !--no-kb)
  POST /embed → query vector (embed :27000)
  cosine_similarity(query_vec, all_chunk_vecs[]) — Go, in-memory
  top-5 chunks → plain text context block
    │
    ▼
build messages[]
  system:    cue.system_prompt + "\n\n" + kb_context (if any)
  ...        session history (if -s)
  user:      input
    │
    ▼
POST /v1/chat/completions  →  target sidecar port
  SSE stream  →  stdout (token by token)
    │
    ▼
append turn to session YAML
  background: auto-name via smallest fast-tier (first turn only)
```

---

## Version History

| Version | Summary |
|---------|---------|
| 0.2.7   | Initial public release |
| 0.2.8   | rename role→cue, add initial fallback cue, probe --cue flag |
| 0.2.9   | embedding-based classification, normalise suggested_tier values |
| 0.2.10  | switch embed library to mlx-embedding-models, fix BertTokenizer compat |
| 0.3.0   | RAG knowledge base (iq kb), KB retrieval in prompt and probe |
| 0.3.1   | MLX embed sidecars, dual embed roles (cue/kb), hybrid KB retrieval, RAG quality improvements |
| 0.4.0   | Replace Ollama with local MLX embed sidecars (embed_server.py, cue :27000 / kb :27001); fix mxbai int attention-mask via _construct_batch patch; mlx-lm decoder fallback for Qwen3-Embedding; registerInManifest for embed models; embed model guard in lm rm; build.sh auto-commit/tag/push; cue classifier confidence threshold (0.68); KB RAG uses cue system prompt instead of hardcoded reading-comprehension template; architecture docs purged of Ollama references |
| 0.4.1   | fix: version bump, remove Ollama from docs, fix diagram alignment |
| 0.4.2   | Rename `iq prompt` → `iq ask` (prompt kept as alias); add pre-commit checklist to CLAUDE.md |
| 0.4.3   | Rename `iq probe` → `iq pry` (probe kept as alias) |
| 0.4.4   | Merge dual embed sidecars into single `embed` sidecar on :27000; default to bge-small-en-v1.5-bf16; auto-migrate cue_model/kb_model → embed_model |
| 0.4.5   | First-run hint for `iq svc start` when no tier models configured; update Quick Start with recommended defaults |
| 0.4.6   | Skip embed sidecar start when model not downloaded (immediate hint); print last log lines on embed sidecar timeout |
| 0.4.7   | Root-level prompts (`iq "message"`); `-?` help alias; extract `addPromptFlags` helper |
