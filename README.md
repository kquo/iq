# IQ

> **Project status: frozen (2026-03-20).**
> Development halted. `iq` and `lm` are kept as learning artifacts — a hands-on study in LLM orchestration, embedding pipelines, and local inference on Apple Silicon. For active development we have moved to [OpenCode](https://github.com/anomalyco/opencode/) with externally-hosted LLMs. The `kb` binary has been superseded by more capable open source alternatives ([AnythingLLM](https://github.com/Mintplex-Labs/anything-llm), [PrivateGPT](https://github.com/zylon-ai/private-gpt), [Khoj](https://github.com/khoj-ai/khoj?tab=readme-ov-file), [Open WebUI](https://github.com/open-webui/open-webui)). See [arch.md](arch.md) for the full technical record.

---

## Development Methodology

This is the workflow used throughout this project. It's worth keeping for reuse in other projects.

### Ground rules

- Every change goes through a single canonical build script (`./build.sh`) that runs `go mod tidy`, `go fmt`, `go fix`, `go vet`, `staticcheck`, all tests, and then builds the binaries. Never run individual toolchain commands directly.
- Versioning follows semver. Decision test: *"Would a user notice this in the CLI help, config show, or start command?"* → yes = MINOR, no = PATCH. MAJOR not until stable public API.
- Each feature has an ID: group letter + sequence number (`A1`, `B2`, etc.), sorted easiest → hardest within each group.

### The cycle

**Step 0 — Repo audit.**
Before starting a new session of work, run a full consistency audit: check all documentation against the code for drift. Grep thresholds, constants, command names, file paths. Fix anything stale before writing new code. This prevents compounding doc debt.

**Step 1 — Roadmap.**
Maintain a `plan.md` (or equivalent) that groups planned features by area and complexity. It's a living document — completed features are removed, not archived. The roadmap also captures project philosophy and constraints (hardware limits, design principles) so decisions have context.

**Step 2 — Development cycle per feature:**

**a. Review.** Read the next feature from the roadmap. Understand what it touches before writing anything.

**b. AC-first (or skip for trivial changes).** Write an acceptance criteria document (`docs/ac_<id>.md`) before any code:
- *Codebase scan* — what does the current code actually do in this area? Quote the relevant functions, files, and line ranges. This prevents building on wrong assumptions.
- *In scope / out of scope* — explicit blast radius. What this change does and deliberately does not do.
- *Design decisions* — record any non-obvious choices and why. Future readers (including you) will thank you.
- *Acceptance tests* — the exact conditions that must be true for the feature to be complete. Written before implementation, verified after.

For small or obvious changes, skip the AC and go straight to implementation.

**c. Implement.** Code against the AC. When editing Go files with deep tab indentation (≥ 3 leading tabs), use Python `str.replace()` via Bash rather than editor exact-match tools — exact-match fails on deep indentation.

**d. Verify.** Run `./build.sh` (no tag). Read the output — fix any vet issues, staticcheck warnings, or test failures before proceeding. Then manually verify each acceptance test from step b.

**e. Pre-release checklist:**
0. Run `./build.sh` clean — fix all failures before touching docs
1. Audit architecture doc against code — grep constants, thresholds, command names, field orders; fix any drift
2. Add version row to architecture doc version history (one visible row, rest in `<details>`)
3. Bump `programVersion` in `cmd/<binary>/main.go`
4. Remove completed features from the roadmap
5. Delete any ephemeral AC memory copies
6. Run `./build.sh <tag> "<short message>"` — the message is a phrase, not a changelog

---

## Legacy

The rest of this file is the original README, preserved for reference.

---

IQ is a command-line tool for managing **offline generative AI systems** on Apple Silicon. It handles local LLM downloads, runs inference sidecars via `mlx_lm`, and routes prompts through a classification layer that selects the right model and cue for each task. The underlying AI models run entirely **on-device**, while IQ provides the CLI interface, workflow management, and task orchestration — all with no cloud dependency and no data leaving your machine.

For a detailed technical overview, see [arch.md](arch.md).

## Why

A personal tool for experimenting with LLM orchestration directly from the Mac terminal. The idea is to run multiple small models locally, route tasks to the right one automatically, and stay close enough to the machinery to understand what's actually happening at each step. It's a research vehicle as much as a utility — the current focus is on building a practical, inspectable inference router, with a longer-term interest in lightweight agentic behaviour: chaining models, tool use, and multi-step reasoning where the user stays in control of every layer.

## Requirements

- Apple Silicon Mac (M1 or later)
- Go (for building)
- Python 3 with `mlx-lm` installed (`pipx install mlx-lm`)
- `hf` CLI (`pipx install huggingface_hub`)
- `mlx-embedding-models` in the mlx-lm venv (`pipx inject mlx-lm mlx-embedding-models`) — used for embeddings (classification + RAG)

IQ uses [Hugging Face](https://huggingface.co) as the official model registry. All model downloads (`lm get`, `lm search`) pull from HF. For access to gated models and to avoid rate limits, set a Hugging Face token:

```bash
export HF_TOKEN=hf_...   # replace with your token
```

## Getting Started

Requires Go installed with `$GOPATH/bin` in your `$PATH`.

```bash
git clone https://github.com/kquo/iq
cd iq
./build.sh
```

Builds three binaries into `$GOPATH/bin`: `iq`, `lm`, `kb`.

## Quick Start

```bash
# Download models
lm get mlx-community/bge-small-en-v1.5-bf16
lm get mlx-community/Llama-3.2-3B-Instruct-4bit
lm get mlx-community/Qwen2.5-7B-Instruct-4bit

# Configure
iq embed set mlx-community/bge-small-en-v1.5-bf16
iq pool add mlx-community/Llama-3.2-3B-Instruct-4bit
iq pool add mlx-community/Qwen2.5-7B-Instruct-4bit

# Start sidecars
iq start

# Run a prompt
iq "explain how transformers work"
```

## Commands

```
iq      — prompt pipeline, cue classification, session management
lm      — model downloads, benchmarks (lm get/search/list/show/rm, lm perf)
kb      — private knowledge base (kb ingest/list/search/ask)
```

Run any binary without arguments for full usage.
