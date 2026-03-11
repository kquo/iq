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

## Find Your Best Models

Every Apple Silicon Mac has different memory and thermal characteristics. Use `iq perf sweep` to benchmark candidate models on your hardware and pick the best fit:

```bash
# Download a few candidates to compare (smaller models are faster, larger ones more capable)
iq lm get mlx-community/Llama-3.2-3B-Instruct-4bit    # 3B — lightweight, fast
iq lm get mlx-community/gemma-3-4b-it-4bit             # 4B — balanced
iq lm get mlx-community/Qwen2.5-7B-Instruct-4bit      # 7B — more capable, uses more memory

# Sweep benchmarks each model: temporarily assigns → starts sidecar → benches → stops → restores config
iq perf sweep --tier fast \
  --models mlx-community/Llama-3.2-3B-Instruct-4bit,mlx-community/gemma-3-4b-it-4bit,mlx-community/Qwen2.5-7B-Instruct-4bit

# Review the comparison table any time
iq perf show
```

The sweep prints a comparison table at the end showing throughput, latency, and quality metrics for each model. Assign the winner to a tier:

```bash
iq tier add fast mlx-community/gemma-3-4b-it-4bit   # or whichever model scored best
```

By default sweep runs inference benchmarks (`--type infer`). Add `--type tool` or `--type cue` to compare tool-routing or classification accuracy as well.

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
iq v0.7.8
Work with IQ from the command line.
...
```
