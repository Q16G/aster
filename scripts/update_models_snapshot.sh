#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SNAPSHOT_PATH="${SCRIPT_DIR}/../internal/provider/models_snapshot.json"
URL="${ASTER_MODELS_URL:-https://models.dev/api.json}"

echo "Fetching models.dev data from ${URL} ..."
if curl -fsSL --max-time 30 "${URL}" -o "${SNAPSHOT_PATH}.tmp"; then
    mv "${SNAPSHOT_PATH}.tmp" "${SNAPSHOT_PATH}"
    SIZE=$(wc -c < "${SNAPSHOT_PATH}" | tr -d ' ')
    echo "Snapshot updated: ${SNAPSHOT_PATH} (${SIZE} bytes)"
else
    rm -f "${SNAPSHOT_PATH}.tmp"
    echo "Failed to fetch models.dev data. Keeping existing snapshot." >&2
    exit 1
fi
