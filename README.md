# IQ

IQ is a command-line tool for running and orchestrating **local** LLMs on Apple Silicon. It manages model downloads, runs inference sidecars via `mlx_lm`, and routes prompts through a classification layer that selects the right model and cue for each task. All inference runs **locally** — no cloud dependency, no data leaving your machine.

For a detailed technical overview, see [architecture.md](architecture.md).

## Why

A personal tool for experimenting with LLM orchestration directly from the Mac terminal. The idea is to run multiple small models locally, route tasks to the right one automatically, and stay close enough to the machinery to understand what's actually happening at each step. It's a research vehicle as much as a utility — the current focus is on building a practical, inspectable inference router, with a longer-term interest in lightweight agentic behaviour: chaining models, tool use, and multi-step reasoning where the user stays in control of every layer.

## Requirements

- Apple Silicon Mac (M1 or later)
- Go (for building)
- Python 3 with `mlx-lm` installed (`pipx install mlx-lm`)
- `hf` CLI (`pipx install huggingface_hub`)

## Getting Started

```bash
git clone https://github.com/kquo/iq
cd iq
./build.sh
```

Requires Go installed with `$GOPATH/bin` in your `$PATH`.

## Quick Start

```bash
# Search for models
iq lm search
iq lm search gemma

# Download models
iq lm get mlx-community/SmolLM2-135M-Instruct-8bit
iq lm get mlx-community/Qwen2.5-0.5B-Instruct-4bit
iq lm get mlx-community/Phi-4-mini-reasoning-4bit

# Assign to tiers (< 2GB → fast, >= 2GB → slow)
iq cfg tier add fast mlx-community/SmolLM2-135M-Instruct-8bit
iq cfg tier add fast mlx-community/Qwen2.5-0.5B-Instruct-4bit
iq cfg tier add slow mlx-community/Phi-4-mini-reasoning-4bit

# Start inference sidecars
iq svc start

# Run a prompt — auto-classifies and routes to the right model
iq prompt "explain how transformers work"

# Debug: see classification and routing without inferring
iq prompt -n "explain how transformers work"

# Full trace including inference
iq prompt -d "explain how transformers work"

# Raw access to a specific sidecar, bypassing the IQ framework
iq probe fast "hello"
iq probe slow "explain attention" -s "You are a terse assistant."
```

## Commands

| Command | Description |
|---------|-------------|
| `iq lm` | Search, download, list, and manage local models |
| `iq cfg` | Manage tier pool assignments and view configuration |
| `iq svc` | Start, stop, and monitor inference sidecars |
| `iq prompt` | Route prompts through classification and cue system |
| `iq probe` | Send raw messages directly to a model sidecar |
| `iq cue` | Manage the cue library |

Run `iq <command> --help` for details on any command.
