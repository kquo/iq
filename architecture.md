# IQ Architecture

## Overview

IQ is a local LLM orchestration tool for Apple Silicon. It manages the full lifecycle of MLX-format language models — discovery, download, tier assignment, cue management, runtime serving, and intelligent prompt routing — through a unified CLI. All inference runs locally with no cloud dependency.

---

## System Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              iq CLI (Go)                                 │
│                                                                          │
│  iq lm     iq cfg     iq cue    iq svc     iq prompt    iq probe         │
│  (models)  (config)   (roles)    (service)  (infer/REPL) (raw debug)     │
└────┬───────────┬───────────┬─────────┬──────────┬────────────┬───────────┘
     │           │           │         │          │            │
     ▼           ▼           ▼         ▼          ▼            ▼
┌─────────┐ ┌─────────┐ ┌────────┐ ┌─────────────────────┐ ┌────────────┐
│ HF      │ │config   │ │roles   │ │ mlx_lm.server       │ │ sessions/  │
│ cache   │ │.yaml    │ │.yaml   │ │ sidecars (pool)     │ │ <id>.yaml  │
│         │ │         │ │        │ │                     │ │            │
│~/.cache/│ │tiers:   │ │name    │ │ fast pool :27001+   │ │ id         │
│hugging  │ │  fast:  │ │category│ │ slow pool :27001+   │ │ name       │
│face/hub/│ │  - m1   │ │desc    │ │                     │ │ cue/tier   │
│models-- │ │  - m2   │ │prompt  │ │ dynamic ports,      │ │ messages[] │
│org--repo│ │  slow:  │ │tier    │ │ one state file per  │ │            │
│/snapshot│ │  - m3   │ │hint    │ │ running model       │ └────────────┘
│  /hash/ │ └─────────┘ └────────┘ │                     │
└─────────┘                        │ OpenAI-compatible   │
                                   │ HTTP API            │
                                   └─────────────────────┘
```

---

## Components

### `iq lm` — Model Management

Handles the full model lifecycle. Models are downloaded from [mlx-community](https://huggingface.co/models?filter=mlx) via the `hf` CLI and stored in the standard HuggingFace cache at `~/.cache/huggingface/hub/`. A manifest at `~/.config/iq/models.json` tracks what IQ knows about.

Key operations: `search`, `get`, `list`, `show`, `rm`.

`iq lm search` queries the HF API, enriches results in parallel (one goroutine per model) to populate DISK and EST MEM, and displays DISK / PARAMS / EST MEM / DOWNLOADS. Accepts an optional query string or a numeric count (e.g. `iq lm search 100`).

`iq lm get` infers a suggested tier from disk size (< 2GB → fast, else slow) and prints the `iq cfg tier add` command to assign it.

`iq lm rm` refuses to remove a model assigned to a tier or whose sidecar is running.

### `iq cfg` — Configuration

Manages `~/.config/iq/config.yaml`. Tiers are **pools** — each tier holds a list of model IDs, not a single slot.

```
fast    sub-2GB models — used for quick inference tasks
slow    2GB+ models    — used for quality inference
embed   embedding model — used for cue classification (fixed port 27000)
```

Commands: `cfg show` (path + model table), `cfg tier show`, `cfg tier add <tier> <model>`, `cfg tier rm <tier> <model>`, `cfg embed show/set/rm` (embedding model for classification).

`cfg show` renders the same model table as `lm list`, scoped to assigned models only.

Auto-migration: on first load, an old four-tier config (`tiny`/`fast`/`balanced`/`quality`) is silently converted to the two-tier pool format using the 2GB disk threshold.

### `iq cue` — Cue Definitions

Manages `~/.config/iq/cues.yaml`, seeded from an embedded default set of 55 cues across 10 categories:

```
language_tasks  generation  reasoning  code       retrieval
summarization   dialogue    safety     domain     ml_ops
```

Each cue carries a `name`, `category`, `description`, `system_prompt`, `suggested_tier`, and an optional direct `model` override (kept for power users, not actively promoted in routing).

Role management: `list`, `show`, `add`, `edit`, `rm`, `assign`, `unassign`, `reset`, `sync`.

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

`iq svc doc` runs preflight checks: `python3` on PATH, `mlx_lm.server` found, `--model` flag present, all assigned model cache dirs exist, embed model cache present, `mlx_embeddings` Python package importable.

**Embedding sidecar** — a separate Python HTTP process (`embed_server.py`, embedded in the IQ binary) that runs `bge-small-en-v1.5-mlx` (or a user-configured model via `iq cfg embed set`). Fixed port: 27000. Started and stopped alongside generative sidecars via `iq svc start/stop`. Serves `POST /embed` (accepts a list of texts, returns L2-normalised float32 vectors) and `GET /health`. State file: `~/.config/iq/run/embed.json`. Requires `pipx install mlx-embeddings`.

`iq svc status` shows TIER / MODEL / ENDPOINT / PID / UPTIME / MEM for all assigned models, plus IQ process memory and combined total.

### `iq prompt` — Inference and REPL

Routes user prompts through a five-step pipeline:

**1. Classify** — the user input is embedded by the embedding sidecar (see below) and compared against pre-computed embeddings of all cue descriptions via cosine similarity. The highest-scoring cue is selected. This replaces the previous LLM-based classifier — no generative call, no instruction-following dependency, deterministic result. Falls back to `initial` if the embed sidecar is not running. Every prompt therefore makes two calls: one cheap embedding call (~10ms), then the full inference call with the resolved cue's system prompt.

> **What embeddings are.** An embedding is a fixed-size vector of numbers — in IQ's case, 384 floats — that a neural network uses to represent the meaning of a piece of text. Networks trained on large corpora learn to place semantically similar content close together in this high-dimensional space, which is what makes similarity search possible: "explain a transformer model" and "describe how attention works" will produce vectors that point in nearly the same direction, even though they share no words. This numerical representation of meaning is the bridge between raw data and neural cognition. It enables similarity search and retrieval (vector DBs), routing and classification without generative inference, memory systems in agentic AI, and multi-modal fusion (images and text embedded into the same space so they can be compared directly). In IQ, each cue description is embedded once and cached; at query time the user input is embedded and the closest cue is selected in microseconds via cosine similarity — no token generation, no instruction-following uncertainty.

The cue embedding cache (`~/.config/iq/cue_embeddings.json`) is built on first use and refreshed automatically when cues change (add/edit/rm/reset/sync). The cache stores a 384-float L2-normalised vector per cue, keyed by cue name, along with the embed model ID and generation timestamp.

**2. Route** — resolves sidecar from the cue. Priority: cue direct model override → cue `suggested_tier` → fast fallback → cross-tier fallback → error.

**3. Build** — assembles the message array: system prompt from the cue, session history (if any), new user message.

**4. Infer** — sends to the target sidecar via `POST /v1/chat/completions`. Streams tokens to stdout by default.

**5. Persist** — appends the turn to `~/.config/iq/sessions/<id>.yaml`. After the first exchange, a background goroutine asks the smallest fast-tier model to generate a short name and description for the session.

**Flags:**
```
-r, --cue <n>      Skip classification, use this cue directly
-c, --category <n>  Restrict auto-classification to one category
    --tier <n>      Override tier directly, bypass cue system
-s, --session <id>  Load/continue a named session
-n, --dry-run       Trace steps 1–3, skip inference
-d, --debug         Trace all steps including inference
    --no-stream     Collect full response before printing
```

`--dry-run` and `--debug` print a step-by-step trace to stderr showing exactly which sidecar handled classification, how the route was resolved, the full effective prompt, and elapsed time per step.

**REPL mode** — entered when no message arg and stdin is a terminal. Supports `/cue`, `/session`, `/clear`, `/dry-run`, `/debug`, `/help`, `/quit`. Pipe-friendly: `echo "..." | iq prompt` takes the stdin path.

### `iq probe` — Raw Sidecar Access

Bypasses the IQ framework entirely. Sends a message directly to a specific sidecar for debugging and model exploration.

```
iq probe <model|tier> [flags] <message>

-s, --system <text>   Optional system prompt
-S, --no-stream       Collect full response before printing
```

Accepts a tier name (routes to any live sidecar in that pool) or a specific model ID. Prints routing info (tier, model, port) in gray before the response, and elapsed time in gray after.

---

## File Layout

```
~/.config/iq/
├── config.yaml              # tier pool assignments
├── models.json              # manifest of downloaded models
├── cues.yaml                    # cue definitions (seeded from embedded defaults)
├── cue_embeddings.json          # cosine similarity cache (auto-built, invalidated on cue changes)
├── run/
│   ├── embed.json                                        # embed sidecar state
│   ├── embed.log
│   ├── mlx-community--SmolLM2-135M-Instruct-8bit.json   # sidecar state
│   ├── mlx-community--SmolLM2-135M-Instruct-8bit.log
│   ├── mlx-community--Phi-4-mini-reasoning-4bit.json
│   └── mlx-community--Phi-4-mini-reasoning-4bit.log
└── sessions/
    └── <id>.yaml            # conversation history per session

~/.cache/huggingface/hub/
└── models--org--repo/
    ├── blobs/               # actual file content (deduplicated)
    └── snapshots/
        └── <hash>/          # symlinks into blobs/ — this is --model path
            ├── config.json
            ├── model.safetensors
            └── tokenizer.json
```

---

## Data Flow: Prompt Request

```
User input
    │
    ├── --cue given? ─────────────────────────────────────┐
    │                                                     │
    ▼  (auto-classify)                                    ▼ (skip classify)
POST /v1/chat/completions                          resolve cue directly
  smallest fast-tier sidecar                              │
  system: cue classifier prompt                           │
  user:   input                                           │
    │                                                     │
    ▼                                                     │
  cue name (exact or fuzzy match) ◄───────────────────────┘
    │
    ▼
resolveRoute()
  cue.model override  →  pickSidecar(tier, false)
  cue.suggested_tier  →  pickSidecar(tier, false)
  fallback             →  pickSidecar("fast", false)
    │
    ▼
build messages[]
  system:    cue.system_prompt
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
| 0.2.7 | Initial public release |
| 0.2.8 | rename role→cue, add initial fallback cue, probe --cue flag |
| 0.2.9 | embedding-based classification, normalise suggested_tier values |
| 0.2.10 | switch embed library to mlx-embedding-models |
