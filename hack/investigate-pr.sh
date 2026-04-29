#!/bin/bash
set -euo pipefail

# Build from source to ensure we get the latest code
SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OOMPA=$(mktemp -d)/oompa
go build -o "$OOMPA" "$SCRIPT_DIR/cmd/oompa"
trap "rm -f $OOMPA" EXIT

"$OOMPA" \
  --agent opencode \
  --agent-model google-vertex-anthropic/claude-opus-4-6@default \
  --repo openperouter/openperouter \
  --watch-prs 317 \
  --reactions ci \
  --dry-run \
  --one-shot \
  --log-level debug
