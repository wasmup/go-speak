#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
export UV_CACHE_DIR="$PWD/build/uv-cache"
export UV_PYTHON_INSTALL_DIR="$PWD/build/python"
export HF_HOME="$PWD/build/hf-cache"
uv venv --python 3.12 --allow-existing build/qwen-venv
# CPU wheels avoid downloading CUDA libraries on desktop CPU installations.
uv pip install --python build/qwen-venv/bin/python --torch-backend cpu torch torchaudio
uv pip install --python build/qwen-venv/bin/python --torch-backend cpu 'qwen-tts==0.1.1'
build/qwen-venv/bin/python -c 'from huggingface_hub import snapshot_download; snapshot_download("Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice", local_dir="build/qwen-model")'
echo 'Qwen is ready. Build and run: go build -o build/go-speak . && ./build/go-speak'
