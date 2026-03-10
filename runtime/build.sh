#!/usr/bin/env bash

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="$ROOT/runtime"
OUT_DIR="$RUNTIME_DIR/bin"
NAME="dux-runtime"

TARGETS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
  "windows amd64"
  "windows arm64"
)

mkdir -p "$OUT_DIR"

for target in "${TARGETS[@]}"; do
  read -r GOOS GOARCH <<<"$target"
  EXT=""
  if [[ "$GOOS" == "windows" ]]; then
    EXT=".exe"
  fi

  OUT_FILE="$OUT_DIR/${NAME}-${GOOS}-${GOARCH}${EXT}"
  echo "building $OUT_FILE"
  (
    cd "$RUNTIME_DIR"
    GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build -o "$OUT_FILE" ./cmd/dux-runtime
  )
done

echo "build complete"
