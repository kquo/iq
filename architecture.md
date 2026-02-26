# IQ Architecture

## Overview

IQ is a local LLM orchestration tool for Apple Silicon. It manages the full lifecycle of MLX-format language models — discovery, download, tier assignment, role management, runtime serving, and intelligent prompt routing — through a unified CLI. All inference runs locally with no cloud dependency.

---

## System Diagram

```
┌──────────────────────────────────────────────────────────────────────────┐
│                              iq CLI (Go)                                 │
│                                                                          │
│  iq lm     iq cfg     iq role    iq svc     iq prompt    iq probe        │
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
│face/hub/│ │  - m1   │ │desc    │ │                     │ │ role/tier  │
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
fast    sub-2GB models — used for classification and quick tasks
slow    2GB+ models    — used for quality inference
```

Commands: `cfg show` (path + model table), `cfg tier show`, `cfg tier add <tier> <model>`, `cfg tier rm <tier> <model>`.

`cfg show` renders the same model table as `lm list`, scoped to assigned models only.

Auto-migration: on first load, an old four-tier config (`tiny`/`fast`/`balanced`/`quality`) is silently converted to the two-tier pool format using the 2GB disk threshold.

### `iq role` — Role Definitions

Manages `~/.config/iq/roles.yaml`, seeded from an embedded default set of 55 roles across 10 categories:

```
language_tasks  generation  reasoning  code       retrieval
summarization   dialogue    safety     domain     ml_ops
```

Each role carries a `name`, `category`, `description`, `system_prompt`, `suggested_tier`, and an optional direct `model` override (kept for power users, not actively promoted in routing).

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

**Pool dispatcher (`pickSidecar`)** — scans live state files for a given tier and returns one. With `preferSmallest: true` (used by the classifier), it returns the model with the smallest disk footprint to minimise classification latency.

`iq svc doc` runs preflight checks: `python3` on PATH, `mlx_lm.server` found, `--model` flag present, all assigned model cache dirs exist.

`iq svc status` shows TIER / MODEL / ENDPOINT / PID / UPTIME / MEM for all assigned models, plus IQ process memory and combined total.

### `iq prompt` — Inference and REPL

Routes user prompts through a five-step pipeline:

**1. Classify** — the input is sent to the smallest live fast-tier sidecar with a compact role list. The model returns a role name, cleaned and matched exactly or via Levenshtein fuzzy match (threshold: distance ≤ 8). Falls back to `general_reasoning_basic` on failure.

**2. Route** — resolves sidecar from the role. Priority: role direct model override → role `suggested_tier` → fast fallback → cross-tier fallback → error.

**3. Build** — assembles the message array: system prompt from the role, session history (if any), new user message.

**4. Infer** — sends to the target sidecar via `POST /v1/chat/completions`. Streams tokens to stdout by default.

**5. Persist** — appends the turn to `~/.config/iq/sessions/<id>.yaml`. After the first exchange, a background goroutine asks the smallest fast-tier model to generate a short name and description for the session.

**Flags:**
```
-r, --role <n>      Skip classification, use this role directly
-c, --category <n>  Restrict auto-classification to one category
    --tier <n>      Override tier directly, bypass role system
-s, --session <id>  Load/continue a named session
-n, --dry-run       Trace steps 1–3, skip inference
-d, --debug         Trace all steps including inference
    --no-stream     Collect full response before printing
```

`--dry-run` and `--debug` print a step-by-step trace to stderr showing exactly which sidecar handled classification, how the route was resolved, the full effective prompt, and elapsed time per step.

**REPL mode** — entered when no message arg and stdin is a terminal. Supports `/role`, `/session`, `/clear`, `/dry-run`, `/debug`, `/help`, `/quit`. Pipe-friendly: `echo "..." | iq prompt` takes the stdin path.

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
├── roles.yaml               # role definitions (seeded from embedded defaults)
├── run/
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
    ├── --role given? ─────────────────────────────────────┐
    │                                                      │
    ▼  (auto-classify)                                     ▼ (skip classify)
POST /v1/chat/completions                           resolve role directly
  smallest fast-tier sidecar                               │
  system: role classifier prompt                           │
  user:   input                                            │
    │                                                      │
    ▼                                                      │
  role name (exact or fuzzy match) ◄────────────────────-─┘
    │
    ▼
resolveRoute()
  role.model override  →  pickSidecar(tier, false)
  role.suggested_tier  →  pickSidecar(tier, false)
  fallback             →  pickSidecar("fast", false)
    │
    ▼
build messages[]
  system:    role.system_prompt
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
