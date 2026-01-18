#!/bin/bash
# AviationWX Bridge - Container Startup
# Started by systemd after boot-update completes
# Version: 2.0

set -euo pipefail

readonly CONTAINER_NAME="aviationwx-bridge"
readonly IMAGE_NAME="ghcr.io/alexwitherspoon/aviationwx-bridge"
readonly DATA_DIR="/data/aviationwx"

# Get version to start (set by boot-update)
VERSION=$(cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || echo "latest")

echo "[$(date -Iseconds)] Starting container with version: $VERSION"

# Remove existing container if present
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Start container
docker run -d \
    --name "$CONTAINER_NAME" \
    --restart no \
    -p 1229:1229 \
    -v "${DATA_DIR}:/data" \
    --tmpfs /dev/shm:size=200m \
    --health-cmd='wget --no-verbose --tries=1 --spider http://localhost:1229/healthz || exit 1' \
    --health-interval=30s \
    --health-timeout=3s \
    --health-start-period=10s \
    --health-retries=3 \
    "${IMAGE_NAME}:${VERSION}"

echo "[$(date -Iseconds)] Container started successfully"
