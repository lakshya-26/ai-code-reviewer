#!/usr/bin/env bash
# scripts/download-model.sh
#
# Downloads the recommended GGUF model for local code review.
#
# Model: Qwen2.5-Coder-7B-Instruct (Q5_K_M quantization)
#   - 7B parameters — fits in ~6 GB RAM
#   - Q5_K_M — best quality/size tradeoff for CPU inference
#   - Excellent at code understanding and JSON output
#
# Usage:
#   bash scripts/download-model.sh
#   # or via Makefile:
#   make download-model

set -euo pipefail

MODELS_DIR="$(cd "$(dirname "$0")/.." && pwd)/models"
MODEL_FILENAME="qwen2.5-coder-7b-instruct-q5_k_m.gguf"
MODEL_PATH="$MODELS_DIR/$MODEL_FILENAME"

# HuggingFace URL for the GGUF file
MODEL_URL="https://huggingface.co/Qwen/Qwen2.5-Coder-7B-Instruct-GGUF/resolve/main/qwen2.5-coder-7b-instruct-q5_k_m.gguf"

echo "==> Model directory: $MODELS_DIR"
mkdir -p "$MODELS_DIR"

if [[ -f "$MODEL_PATH" ]]; then
    SIZE=$(du -sh "$MODEL_PATH" | cut -f1)
    echo "==> Model already exists ($SIZE). Delete it to re-download."
    echo "    $MODEL_PATH"
    exit 0
fi

echo "==> Downloading $MODEL_FILENAME (~5.2 GB)..."
echo "    This will take a few minutes on a fast connection."
echo ""

if command -v wget &>/dev/null; then
    wget --progress=bar:force -O "$MODEL_PATH" "$MODEL_URL"
elif command -v curl &>/dev/null; then
    curl -L --progress-bar -o "$MODEL_PATH" "$MODEL_URL"
else
    echo "ERROR: Neither wget nor curl is installed. Install one and retry."
    exit 1
fi

echo ""
echo "==> Download complete: $MODEL_PATH"
echo "    Size: $(du -sh "$MODEL_PATH" | cut -f1)"
echo ""
echo "==> Next steps:"
echo "    docker compose up"
echo "    # or run llama.cpp server directly:"
echo "    llama-server -m $MODEL_PATH --port 8080 --ctx-size 8192"
