#!/usr/bin/env python3
"""IQ embedding sidecar — serves POST /embed and GET /health.

Launched by `iq start` with --model <hf_model_id> and --port <n>.
Requires: pipx inject mlx-lm mlx-embedding-models
"""
import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

import numpy as np

encode_fn = None


def _patch_mlx_sdpa():
    """Patch mlx.fast.scaled_dot_product_attention to auto-convert integer
    attention masks to an additive float mask matching the query dtype.
    Some encoder models (e.g. mxbai) pass an int 0/1 mask from the tokenizer
    which MLX rejects with 'Mask type must promote to output type float16'."""
    try:
        import mlx.core as mx
        import mlx.fast as mxf

        _orig = mxf.scaled_dot_product_attention

        def _fixed(q, k, v, scale, mask=None, **kw):
            if mask is not None and mask.dtype not in (mx.float16, mx.bfloat16, mx.float32):
                # Convert 0/1 int mask to additive float mask:
                #   attend (1) → 0.0  (no score penalty)
                #   ignore (0) → -1e4 (large negative ≈ -inf after softmax)
                mask = mx.where(
                    mask.astype(mx.bool_),
                    mx.array(0.0, dtype=q.dtype),
                    mx.array(-1e4, dtype=q.dtype),
                )
            return _orig(q, k, v, scale, mask=mask, **kw)

        mxf.scaled_dot_product_attention = _fixed
    except Exception:
        pass  # if MLX is absent the subsequent import will fail with a clear message


def load_model(model_id):
    _patch_mlx_sdpa()

    # ── Primary: mlx-embedding-models (encoder models: BERT, nomic, mxbai…) ──
    # A smoke-test inference is run immediately after loading so runtime dtype
    # errors are caught at startup rather than silently dropping embeddings.
    primary_err = None
    try:
        from mlx_embedding_models.embedding import EmbeddingModel

        model = EmbeddingModel.from_pretrained(model_id)

        # mlx-embedding-models calls tokenizer.batch_encode_plus internally;
        # BERT's slow tokenizer doesn't have this method — patch it.
        tok = model.tokenizer
        if not hasattr(tok, "batch_encode_plus"):
            tok.batch_encode_plus = lambda texts, **kw: tok(texts, **kw)

        # mlx-embedding-models._construct_batch converts attention masks to
        # mx.array while preserving their int64 numpy dtype. mlx.fast SDPA
        # rejects int masks — it requires the mask to promote to the output
        # float dtype. Patch _construct_batch to cast the attention_mask to
        # float16 after MLX conversion. This is the earliest reliable intercept
        # point because the library rebuilds the mask with np.int64 arrays
        # internally (prepending CLS/SEP 1s), so patching the tokenizer output
        # doesn't help.
        import mlx.core as _mx

        _orig_construct = model._construct_batch

        def _patched_construct(batch):
            tensor_batch = _orig_construct(batch)
            if "attention_mask" in tensor_batch:
                tensor_batch["attention_mask"] = tensor_batch["attention_mask"].astype(_mx.float16)
            return tensor_batch

        model._construct_batch = _patched_construct

        def fn(texts):
            emb = np.array(model.encode(texts))
            norms = np.linalg.norm(emb, axis=1, keepdims=True)
            return (emb / np.maximum(norms, 1e-9)).tolist()

        fn(["smoke test"])
        return fn
    except ImportError:
        print(
            "mlx-embedding-models not found — run: pipx inject mlx-lm mlx-embedding-models",
            file=sys.stderr,
            flush=True,
        )
        sys.exit(1)
    except Exception as e:
        primary_err = e

    # ── Fallback: mlx-lm for decoder-only embedding models (Qwen3, Mistral…) ──
    # Decoder embedding models are causal LMs fine-tuned for retrieval.
    # mlx-embedding-models does not support them; mlx-lm (already installed)
    # loads them natively. Embeddings are extracted via last-token pooling on
    # the transformer backbone (hidden states, not logits).
    print(
        f"mlx-embedding-models: {primary_err} — trying mlx-lm decoder fallback",
        file=sys.stderr,
        flush=True,
    )
    try:
        import mlx.core as mx
        from mlx_lm import load as mlx_lm_load

        model, tokenizer = mlx_lm_load(model_id)
        backbone = model.model  # transformer backbone; strips the lm_head

        def fn(texts):
            results = []
            for text in texts:
                tokens = tokenizer.encode(text)
                x = mx.array([tokens])            # (1, seq_len)
                hidden = backbone(x)              # (1, seq_len, hidden_size)
                vec = np.array(hidden[0, -1, :].astype(mx.float32))  # last-token pool
                results.append(vec)
            emb = np.array(results)
            norms = np.linalg.norm(emb, axis=1, keepdims=True)
            return (emb / np.maximum(norms, 1e-9)).tolist()

        fn(["smoke test"])
        return fn
    except Exception as e:
        print(f"failed to load embed model via mlx-lm: {e}", file=sys.stderr, flush=True)
        sys.exit(1)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            body = b'{"status":"ok"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
        else:
            self.send_response(404)
            self.end_headers()

    def do_POST(self):
        if self.path == "/embed":
            try:
                length = int(self.headers.get("Content-Length", 0))
                body = json.loads(self.rfile.read(length))
                texts = body.get("texts", [])
                if not texts:
                    t = body.get("text", "")
                    texts = [t] if t else []
                if not texts:
                    raise ValueError("no texts provided")
                embeddings = encode_fn(texts)
                result = json.dumps({"embeddings": embeddings}).encode()
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(result)))
                self.end_headers()
                self.wfile.write(result)
            except Exception as e:
                err = json.dumps({"error": str(e)}).encode()
                self.send_response(500)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(err)))
                self.end_headers()
                self.wfile.write(err)
        else:
            self.send_response(404)
            self.end_headers()

    def log_message(self, format, *args):
        pass  # suppress per-request logs; errors go to stderr


def main():
    global encode_fn
    parser = argparse.ArgumentParser(description="IQ embedding sidecar")
    parser.add_argument("--model", required=True, help="HF model ID")
    parser.add_argument("--port", type=int, default=27000)
    args = parser.parse_args()

    encode_fn = load_model(args.model)
    server = HTTPServer(("127.0.0.1", args.port), Handler)
    print(f"IQ embed sidecar ready on :{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
