#!/bin/bash
set -euo pipefail

go run github.com/qinqon/oompa/cmd/oompa@latest \
  --agent opencode \
  --agent-model google-vertex-anthropic/claude-opus-4-6@default \
  --repo openperouter/openperouter \
  --watch-prs 317 \
  --reactions ci \
  --skip-fix \
  --one-shot \
  --log-level debug
