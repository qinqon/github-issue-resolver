#!/bin/bash
set -euo pipefail

NMSTATE_AI=$(gh api /orgs/nmstate/installations --jq '.installations[] | select(.app_slug == "nmstate-ai") | {app_id, id}')
export GITHUB_APP_ID="${GITHUB_APP_ID:-$(echo "$NMSTATE_AI" | jq -r '.app_id')}"
export GITHUB_APP_INSTALLATION_ID="${GITHUB_APP_INSTALLATION_ID:-$(echo "$NMSTATE_AI" | jq -r '.id')}"
export GITHUB_APP_PRIVATE_KEY_PATH="${GITHUB_APP_PRIVATE_KEY_PATH:-$HOME/.secrets/nmstate-ai.2026-04-10.private-key.pem}"

IMAGE="${IMAGE:-quay.io/nmstate/ai-agent:latest}"

podman run --rm \
    -v "${GITHUB_APP_PRIVATE_KEY_PATH}:/secrets/app.pem:ro" \
    -e GITHUB_APP_ID="${GITHUB_APP_ID}" \
    -e GITHUB_APP_INSTALLATION_ID="${GITHUB_APP_INSTALLATION_ID}" \
    -e GITHUB_APP_PRIVATE_KEY_PATH=/secrets/app.pem \
    -e CLOUD_ML_REGION="${CLOUD_ML_REGION:-}" \
    -e ANTHROPIC_VERTEX_PROJECT_ID="${ANTHROPIC_VERTEX_PROJECT_ID:-}" \
    -e GOOGLE_APPLICATION_CREDENTIALS="${GOOGLE_APPLICATION_CREDENTIALS:-}" \
    "${IMAGE}" \
    --owner nmstate --repo kubernetes-nmstate \
    --clone-dir /work \
    --log-level debug --poll-interval 30s \
    --reviewers "mkowalski,emy,qinqon,gemini-code-assist"
