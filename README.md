# IQ

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

IQ uses [Hugging Face](https://huggingface.co) as the official model registry. All model downloads (`iq lm get`, `iq lm search`) pull from HF. For access to gated models and to avoid rate limits, set a Hugging Face token:

```bash
export HF_TOKEN=hf_k7mX2pQ9nRvL4wD8fYcJ3tBsH6eA1gNuZ5iW   # replace with your token
```

You can create a token at <https://huggingface.co/settings/tokens>.

## Getting Started

Requires Go installed with `$GOPATH/bin` in your `$PATH`.

```bash
git clone https://github.com/kquo/iq
cd iq
./build.sh
```

## Quick Start

```bash
# Download recommended models (or substitute your own)
iq lm get mlx-community/bge-small-en-v1.5-bf16
iq lm get mlx-community/Llama-3.2-3B-Instruct-4bit
iq lm get mlx-community/Qwen2.5-7B-Instruct-4bit

# Configure embed model and tier assignments
iq embed set mlx-community/bge-small-en-v1.5-bf16
iq tier add fast mlx-community/Llama-3.2-3B-Instruct-4bit
iq tier add slow mlx-community/Qwen2.5-7B-Instruct-4bit

# Start sidecars
iq start

# Run a prompt — auto-classifies and routes to the right model
iq "explain how transformers work"
```

Any MLX-compatible embedding model works for `embed`, and any MLX-compatible generative model works for `fast` / `slow` tiers. Use `iq lm search` to browse available models.

```bash
# Debug: see classification and routing without inferring
iq -n "explain how transformers work"

# Full trace including inference
iq -d "explain how transformers work"

# Interactive REPL
iq ask

# Raw access to a specific sidecar, bypassing the IQ framework
iq pry fast "hello"
iq pry slow "explain attention" -s "You are a terse assistant."
```

## Commands
Run `iq` without arguments to see the **usage**.

```bash
$ iq
iq v0.6.2
Work with IQ from the command line.
...
```
