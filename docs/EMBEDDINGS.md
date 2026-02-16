# Local Embeddings Setup

This guide shows how to run `localclaw` memory search with local vector embeddings using Hugging Face models, including [`google/embeddinggemma-300m`](https://huggingface.co/google/embeddinggemma-300m).

`localclaw` expects a local embedding runtime executable that supports:

- command: `embed --format json`
- stdin JSON payload:
  - `model` (string)
  - `modelPath` (optional string)
  - `modelCacheDir` (optional string)
  - `input` (array of strings)
- stdout JSON payload:
  - `{"embeddings":[[...],[...]]}`

## Prerequisites

- Python 3.11+ available as `python3`
- `localclaw` checked out locally
- Hugging Face account access for gated models

Important for Apple Silicon users:

- Use an ARM venv and force the runtime wrapper to run with `arch -arm64`.
- This works even when your Go toolchain default is `go env GOARCH=amd64`.

## 1. Create an embeddings environment

```bash
export LOCALCLAW_EMBED_HOME="$HOME/.localclaw/embeddings"
mkdir -p "$LOCALCLAW_EMBED_HOME"
python3 -m venv "$LOCALCLAW_EMBED_HOME/.venv"
"$LOCALCLAW_EMBED_HOME/.venv/bin/pip" install --upgrade pip

# Example known-good stack for localclaw subprocess usage
"$LOCALCLAW_EMBED_HOME/.venv/bin/pip" install \
  "numpy<2" \
  "torch==2.6.0" \
  "transformers==4.57.1" \
  "sentence-transformers==5.2.2" \
  "huggingface_hub==0.36.0"
```

The versions above are required for `google/embeddinggemma-300m` support (`model_type=gemma3_text`).

Sanity check the runtime stack:

```bash
"$LOCALCLAW_EMBED_HOME/.venv/bin/python" - <<'PY'
import platform
import torch
import transformers
import sentence_transformers
print("python_arch=", platform.machine())
print("torch=", torch.__version__)
print("transformers=", transformers.__version__)
print("sentence_transformers=", sentence_transformers.__version__)
PY
```

## 2. Download model files from Hugging Face

```bash
export MODEL_DIR="$HOME/.localclaw/models/embeddinggemma-300m"
mkdir -p "$MODEL_DIR"
"$LOCALCLAW_EMBED_HOME/.venv/bin/hf" auth login
"$LOCALCLAW_EMBED_HOME/.venv/bin/hf" download \
  google/embeddinggemma-300m \
  --local-dir "$MODEL_DIR"
```

`google/embeddinggemma-300m` is gated. If you see `GatedRepo`/`401`/`403`, authenticate and request access on the model page, then wait for approval from the repo authors.

If you use a fine-grained token, enable public gated repo read access. You can verify current token flags with:

```bash
"$LOCALCLAW_EMBED_HOME/.venv/bin/python" - <<'PY'
from huggingface_hub import HfApi
print(HfApi().whoami()["auth"]["accessToken"]["fineGrained"]["canReadGatedRepos"])
PY
```

Expected output for gated downloads is `True`.

Optional model smoke test:

```bash
"$LOCALCLAW_EMBED_HOME/.venv/bin/python" - <<'PY'
import os
from transformers import AutoConfig
from sentence_transformers import SentenceTransformer
model_dir = os.path.expanduser("~/.localclaw/models/embeddinggemma-300m")
cfg = AutoConfig.from_pretrained(model_dir)
print("model_type=", cfg.model_type)
model = SentenceTransformer(model_dir, trust_remote_code=False)
print("vector_dim=", len(model.encode(["smoke"], normalize_embeddings=True)[0]))
PY
```

## 3. Install a local embedding runtime wrapper

Create a Python implementation plus shell entrypoint so localclaw always uses the venv interpreter.

```bash
mkdir -p "$HOME/.localclaw/bin"

cat > "$HOME/.localclaw/bin/localclaw-embed-runtime.py" <<'PY'
import contextlib
import json
import os
import sys

os.environ.setdefault("HF_HUB_DISABLE_PROGRESS_BARS", "1")
os.environ.setdefault("TRANSFORMERS_VERBOSITY", "error")
os.environ.setdefault("TOKENIZERS_PARALLELISM", "false")

from sentence_transformers import SentenceTransformer

def fail(msg: str) -> None:
    print(msg, file=sys.stderr)
    raise SystemExit(1)

def main() -> None:
    if len(sys.argv) != 4 or sys.argv[1] != "embed" or sys.argv[2] != "--format" or sys.argv[3] != "json":
        fail("usage: localclaw-embed-runtime embed --format json")

    payload = json.load(sys.stdin)
    model = str(payload.get("model", "")).strip()
    model_path = str(payload.get("modelPath", "")).strip()
    model_cache_dir = str(payload.get("modelCacheDir", "")).strip()
    texts = payload.get("input")

    if not model:
        fail("payload.model is required")
    if not isinstance(texts, list) or any(not isinstance(v, str) for v in texts):
        fail("payload.input must be an array of strings")

    source = model_path if model_path else model
    with contextlib.redirect_stdout(sys.stderr):
        encoder = SentenceTransformer(source, cache_folder=(model_cache_dir or None), trust_remote_code=False)
        vectors = encoder.encode(texts, normalize_embeddings=True).tolist()

    json.dump({"embeddings": vectors}, sys.stdout)
    sys.stdout.write("\n")

if __name__ == "__main__":
    main()
PY

cat > "$HOME/.localclaw/bin/localclaw-embed-runtime" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
exec arch -arm64 "$HOME/.localclaw/embeddings/.venv/bin/python" "$HOME/.localclaw/bin/localclaw-embed-runtime.py" "$@"
SH

chmod +x "$HOME/.localclaw/bin/localclaw-embed-runtime"
```

If you are not on Apple Silicon, remove `arch -arm64` and call the venv `python` directly.

## 4. Configure `localclaw` memorySearch

Example config fragment:

```json
{
  "agents": {
    "defaults": {
      "memorySearch": {
        "enabled": true,
        "provider": "local",
        "fallback": "local",
        "model": "/Users/you/.localclaw/models/embeddinggemma-300m",
        "store": {
          "vector": {
            "enabled": true
          }
        },
        "local": {
          "runtimePath": "/Users/you/.localclaw/bin/localclaw-embed-runtime",
          "queryTimeoutSeconds": 120,
          "batchTimeoutSeconds": 300
        }
      }
    }
  }
}
```

Notes:

- `provider=local` + `fallback=local` makes local embeddings mandatory. Misconfigured runtime/model returns an error instead of silently downgrading to keyword-only search.
- If you want keyword-only fallback, set `"fallback": "none"`.
- `memorySearch.local.modelPath` is validated as a file path. For Hugging Face snapshot directories, put the directory path in `memorySearch.model` (as shown above) and leave `modelPath` unset.

## 5. Validate locally

```bash
go run ./cmd/localclaw -config ./localclaw.json memory index --force
go run ./cmd/localclaw -config ./localclaw.json memory search "incident response timeline" --json
```

To sanity check vector retrieval, run a semantically related query with weak/no exact keyword overlap and confirm you still get results.
