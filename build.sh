#!/bin/bash
set -euo pipefail

REGISTRY="${REGISTRY:-quay.io}"
REPO="${REPO:-nmstate/ai-agent}"
TAG="${TAG:-latest}"
IMAGE="${REGISTRY}/${REPO}:${TAG}"

echo "Building ${IMAGE}..."
podman build -t "${IMAGE}" -f Containerfile .

if [ "${PUSH:-false}" = "true" ]; then
    echo "Pushing ${IMAGE}..."
    podman push "${IMAGE}"
fi

echo "Done: ${IMAGE}"
