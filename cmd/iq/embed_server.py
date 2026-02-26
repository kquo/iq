#!/usr/bin/env python3
"""IQ embedding sidecar — serves POST /embed and GET /health.

Requires: pipx inject mlx-lm mlx-embedding-models
"""
import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

encode_fn = None


def load_model(model_path):
    try:
        from mlx_embedding_models.embedding import EmbeddingModel
        import numpy as np

        model = EmbeddingModel.from_pretrained(model_path)

        # mlx-embedding-models calls tokenizer.batch_encode_plus internally.
        # BERT's slow tokenizer doesn't have this method — patch it.
        tok = model.tokenizer
        if not hasattr(tok, 'batch_encode_plus'):
            tok.batch_encode_plus = lambda texts, **kw: tok(texts, **kw)

        def fn(texts):
            # encode() returns a numpy array of shape (n, dim), already L2-normalised.
            emb = model.encode(texts)
            return np.array(emb).tolist()

        return fn
    except ImportError:
        print(
            "mlx-embedding-models not found — run: pipx inject mlx-lm mlx-embedding-models",
            file=sys.stderr,
            flush=True,
        )
        sys.exit(1)
    except Exception as e:
        print(f"failed to load embed model: {e}", file=sys.stderr, flush=True)
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
        pass  # suppress per-request logs


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
    parser.add_argument("--model", required=True, help="HF snapshot path for embed model")
    parser.add_argument("--port", type=int, default=27000)
    args = parser.parse_args()

    encode_fn = load_model(args.model)
    server = HTTPServer(("127.0.0.1", args.port), Handler)
    # Print ready signal — svc.go polls /health, but this also shows in logs.
    print(f"IQ embed sidecar ready on :{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
