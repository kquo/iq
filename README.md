# iq

> **Project status: frozen (2026-03-20).**
> Development halted. `iq` and `lm` are kept as learning artifacts — a hands-on study in LLM orchestration, embedding pipelines, and local inference on Apple Silicon. For active development we have moved to [OpenCode](https://github.com/anomalyco/opencode/) with externally-hosted LLMs. The `kb` binary has been superseded by more capable open source alternatives ([AnythingLLM](https://github.com/Mintplex-Labs/anything-llm), [PrivateGPT](https://github.com/zylon-ai/private-gpt), [Khoj](https://github.com/khoj-ai/khoj), [Open WebUI](https://github.com/open-webui/open-webui)). See [arch.md](arch.md) for the full technical record.

iq is a command-line tool for managing **offline generative AI systems** on Apple Silicon. It handles local LLM downloads, runs inference sidecars via `mlx_lm`, and routes prompts through a classification layer that selects the right model and cue for each task. The underlying AI models run entirely **on-device**, while iq provides the CLI interface, workflow management, and task orchestration — all with no cloud dependency and no data leaving your machine.

## Why

A personal tool for experimenting with LLM orchestration directly from the Mac terminal. The idea is to run multiple small models locally, route tasks to the right one automatically, and stay close enough to the machinery to understand what's actually happening at each step. It's a research vehicle as much as a utility — focus on a practical, inspectable inference router, with a longer-term interest in lightweight agentic behaviour: chaining models, tool use, and multi-step reasoning where the user stays in control of every layer.

For a detailed technical overview, see [arch.md](arch.md). For governance, see [AGENTS.md](AGENTS.md) and [docs/](docs/).

## Requirements

- Apple Silicon Mac (M1 or later)
- Go (for building)
- Python 3 with `mlx-lm` installed (`pipx install mlx-lm`)
- `hf` CLI (`pipx install huggingface_hub`)
- `mlx-embedding-models` in the mlx-lm venv (`pipx inject mlx-lm mlx-embedding-models`) — used for embeddings (classification + RAG)

iq uses [Hugging Face](https://huggingface.co) as the official model registry. All model downloads (`lm get`, `lm search`) pull from HF. For access to gated models and to avoid rate limits, set a Hugging Face token:

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
