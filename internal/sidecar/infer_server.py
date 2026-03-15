#!/usr/bin/env python3
"""IQ inference sidecar — serves POST /v1/chat/completions and GET /v1/models.

Replaces mlx_lm.server with a custom server that supports constrained decoding
via a routing grammar for tool-use prompts. When no routing_grammar is present
in the request, behaves identically to stock mlx_lm.server.

Launched by `iq start` with --model <hf_model_id> and --port <n>.
Requires: pipx install mlx-lm
"""
import argparse
import json
import sys
import time
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer
from threading import Lock

import mlx.core as mx
from mlx_lm import load as mlx_load
from mlx_lm import stream_generate
from mlx_lm.sample_utils import make_logits_processors

# Global model state — loaded once at startup, protected by lock for inference.
_model = None
_tokenizer = None
_model_id = ""
_lock = Lock()


def _apply_chat_template(messages):
    """Convert chat messages to token IDs via the tokenizer's chat template."""
    return _tokenizer.apply_chat_template(
        messages, add_generation_prompt=True, tokenize=True
    )


# ── Routing grammar logits processor ─────────────────────────────────────────


class RoutingGrammarProcessor:
    """A logits processor that constrains the first few tokens to be a routing
    prefix: either <tool:TOOL_NAME> or <no_tool>.

    Once the prefix is complete, the processor becomes a no-op and generation
    continues unconstrained.

    Works by pre-encoding all valid route strings into token ID sequences.
    State is derived from the `tokens` argument on each call (not tracked
    separately) because generate_step speculatively prefetches the next token
    before yielding — so __call__ may be invoked multiple times before the
    caller can update external state.
    """

    DONE = -1

    def __init__(self, tool_names, tokenizer):
        self.tokenizer = tokenizer
        self._prompt_len = None  # set on first __call__

        # Pre-encode all valid routes into token ID sequences.
        routes = [f"<tool:{name}>" for name in tool_names] + ["<no_tool>"]
        self._route_seqs = []
        for route in routes:
            ids = tokenizer.encode(route, add_special_tokens=False)
            self._route_seqs.append(ids)

    def __call__(self, tokens, logits):
        # On first call, tokens is the prompt tail — record its length.
        # Subsequent calls append one generated token each time.
        if self._prompt_len is None:
            self._prompt_len = len(tokens)

        # How many tokens have been generated so far (including the one
        # about to be sampled from these logits).
        gen_pos = len(tokens) - self._prompt_len

        # Determine which routes are still valid given generated tokens.
        gen_tokens = tokens[self._prompt_len :].tolist() if gen_pos > 0 else []
        live_routes = self._route_seqs
        for i, tid in enumerate(gen_tokens):
            live_routes = [
                seq for seq in live_routes if i < len(seq) and seq[i] == tid
            ]

        # Check if any route is already fully matched.
        for seq in live_routes:
            if gen_pos >= len(seq):
                return logits  # done — unconstrained

        # Collect allowed token IDs at this position.
        allowed = set()
        for seq in live_routes:
            if gen_pos < len(seq):
                allowed.add(seq[gen_pos])

        if not allowed:
            return logits  # no valid continuation — unconstrained

        # Mask: set disallowed logits to -inf.
        vocab_size = logits.shape[-1]
        mask = mx.full(logits.shape, float("-inf"))
        for tid in allowed:
            if tid < vocab_size:
                mask[0, tid] = 0.0
        return logits + mask


# ── Inference ─────────────────────────────────────────────────────────────────


def _generate(messages, max_tokens, stream, repetition_penalty, temperature,
              routing_grammar, top_p=None, min_p=None, top_k=None, stop=None, seed=None):
    """Run inference and yield (text_chunk, finish_reason) tuples.

    Extended sampling params (top_p, min_p, top_k, seed) are forwarded to
    mlx_lm when present; omitting them lets mlx_lm use its own defaults.
    Stop sequences are checked post-generation against the accumulated text.
    """
    prompt_tokens = _apply_chat_template(messages)

    kwargs = {"max_tokens": max_tokens, "temp": temperature}
    if top_p is not None:
        kwargs["top_p"] = top_p
    if min_p is not None:
        kwargs["min_p"] = min_p
    if top_k is not None:
        kwargs["top_k"] = top_k

    # Build logits processors list: repetition penalty + optional routing grammar.
    processors = []
    if repetition_penalty and repetition_penalty != 1.0:
        processors.extend(make_logits_processors(repetition_penalty=repetition_penalty))
    if routing_grammar and routing_grammar.get("tool_names"):
        processors.append(
            RoutingGrammarProcessor(routing_grammar["tool_names"], _tokenizer)
        )
    if processors:
        kwargs["logits_processors"] = processors

    if seed is not None:
        mx.random.seed(seed)

    with _lock:
        full_text = []
        for resp in stream_generate(
            _model, _tokenizer, prompt=prompt_tokens, **kwargs
        ):
            if resp.text:
                full_text.append(resp.text)
                if stream:
                    yield resp.text, None

        result = "".join(full_text)

        # Trim at the first stop sequence found, if any.
        # Note: in streaming mode tokens are already yielded above, so the
        # caller may have printed past the stop boundary. The trimmed result
        # is still returned correctly; this is acceptable because stop sequences
        # are most useful for non-streaming (tool calls, structured output).
        if stop:
            for s in stop:
                idx = result.find(s)
                if idx != -1:
                    result = result[:idx]

        if stream:
            yield "", "stop"
        else:
            yield result, "stop"


def _build_chat_response(content, model, finish_reason="stop", stream=False):
    """Build an OpenAI-compatible chat completion response."""
    resp_id = f"chatcmpl-{uuid.uuid4().hex[:12]}"
    created = int(time.time())
    if stream:
        return {
            "id": resp_id,
            "object": "chat.completion.chunk",
            "created": created,
            "model": model,
            "choices": [
                {
                    "index": 0,
                    "delta": {"content": content} if content else {},
                    "finish_reason": finish_reason,
                }
            ],
        }
    return {
        "id": resp_id,
        "object": "chat.completion",
        "created": created,
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": finish_reason,
            }
        ],
    }


# ── HTTP handler ──────────────────────────────────────────────────────────────


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/v1/models":
            body = json.dumps(
                {
                    "object": "list",
                    "data": [
                        {
                            "id": _model_id,
                            "object": "model",
                            "owned_by": "local",
                        }
                    ],
                }
            ).encode()
            self._respond(200, body)
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        if self.path == "/v1/chat/completions":
            try:
                length = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(length))
                messages = body.get("messages", [])
                max_tokens = body.get("max_tokens", 8192)
                stream = body.get("stream", False)
                rep_penalty = body.get("repetition_penalty", 1.0)
                temperature = body.get("temperature", 0.7)
                routing_grammar = body.get("routing_grammar")
                top_p = body.get("top_p")
                min_p = body.get("min_p")
                top_k = body.get("top_k")
                stop = body.get("stop") or None
                seed = body.get("seed")

                if stream:
                    self.send_response(200)
                    self.send_header("Content-Type", "text/event-stream")
                    self.send_header("Cache-Control", "no-cache")
                    self.end_headers()
                    for text, finish in _generate(
                        messages, max_tokens, True, rep_penalty, temperature, routing_grammar,
                        top_p=top_p, min_p=min_p, top_k=top_k, stop=stop, seed=seed,
                    ):
                        chunk = _build_chat_response(
                            text, _model_id, finish_reason=finish, stream=True
                        )
                        line = f"data: {json.dumps(chunk)}\n\n"
                        self.wfile.write(line.encode())
                        self.wfile.flush()
                    self.wfile.write(b"data: [DONE]\n\n")
                    self.wfile.flush()
                else:
                    content = ""
                    for text, finish in _generate(
                        messages, max_tokens, False, rep_penalty, temperature, routing_grammar,
                        top_p=top_p, min_p=min_p, top_k=top_k, stop=stop, seed=seed,
                    ):
                        content = text
                    resp = _build_chat_response(content, _model_id)
                    self._respond(200, json.dumps(resp).encode())
            except Exception as e:
                err = json.dumps({"error": str(e)}).encode()
                self._respond(500, err)
        else:
            self.send_response(404)
            self.end_headers()

    def _respond(self, code, body):
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        pass  # suppress per-request logs


# ── Main ──────────────────────────────────────────────────────────────────────


def main():
    global _model, _tokenizer, _model_id

    parser = argparse.ArgumentParser(description="IQ inference sidecar")
    parser.add_argument("--model", required=True, help="HF model ID or local path")
    parser.add_argument("--port", type=int, default=27001)
    args = parser.parse_args()

    _model_id = args.model
    print(f"Loading model: {_model_id}", file=sys.stderr, flush=True)
    _model, _tokenizer = mlx_load(_model_id)
    print(
        f"IQ infer sidecar ready on :{args.port} ({_model_id})",
        file=sys.stderr,
        flush=True,
    )

    server = HTTPServer(("127.0.0.1", args.port), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
