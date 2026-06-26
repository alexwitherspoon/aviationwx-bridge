#!/usr/bin/env bash
# Contract checks for the E2E harness stack (host-side).
set -euo pipefail

TIER="${1:-all}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"

HTTPS_ADDR="${E2E_UPLOAD_HTTPS_ADDR:-127.0.0.1:18443}"
SFTP_ADDR="${E2E_UPLOAD_SFTP_ADDR:-127.0.0.1:12223}"
CA_FILE="${ROOT}/testdata/e2e/tls/ca.pem"
HTTPS_HOST="${HTTPS_ADDR%:*}"
HTTPS_PORT="${HTTPS_ADDR#*:}"

wait_tcp() {
  local addr="$1" timeout="${2:-180}"
  local start
  start=$(date +%s)
  while true; do
    if (echo >/dev/tcp/"${addr%:*}"/"${addr#*:}") >/dev/null 2>&1; then
      return 0
    fi
    if (( $(date +%s) - start >= timeout )); then
      echo "timeout waiting for tcp ${addr}" >&2
      return 1
    fi
    sleep 2
  done
}

echo "Preflight: waiting for upload HTTPS ${HTTPS_ADDR}..."
wait_tcp "${HTTPS_ADDR}"

ROSTER_URL="https://upload.e2e.test:${HTTPS_PORT}/.well-known/aviationwx-upload-ssh-host-keys.json"
echo "Preflight: roster JSON..."
BODY="$(curl -fsS --cacert "${CA_FILE}" --resolve "upload.e2e.test:${HTTPS_PORT}:${HTTPS_HOST}" "${ROSTER_URL}")"
echo "${BODY}" | jq -e '.version == 1' >/dev/null
echo "${BODY}" | jq -e '.hostname == "upload.e2e.test"' >/dev/null
echo "${BODY}" | jq -e '.port == 2222' >/dev/null
echo "${BODY}" | jq -e '(.sha256 | length) > 0' >/dev/null

HDR="$(curl -fsS -D - -o /dev/null --cacert "${CA_FILE}" --resolve "upload.e2e.test:${HTTPS_PORT}:${HTTPS_HOST}" "${ROSTER_URL}")"
echo "${HDR}" | grep -qi 'cache-control:.*no-store'

echo "Preflight: waiting for SFTP ${SFTP_ADDR}..."
wait_tcp "${SFTP_ADDR}"

if [[ "${TIER}" == "2" || "${TIER}" == "all" ]]; then
  SIM_URL="${E2E_SIMULATOR_URL:-http://127.0.0.1:18080}"
  echo "Preflight: camera-simulator ${SIM_URL}..."
  curl -fsS "${SIM_URL}/healthz" >/dev/null
fi

if [[ "${TIER}" != "0" ]]; then
  BRIDGE_URL="${E2E_BRIDGE_URL:-http://127.0.0.1:1231}"
  echo "Preflight: bridge ${BRIDGE_URL}/readyz..."
  curl -fsS -u admin:aviationwx "${BRIDGE_URL}/readyz" >/dev/null
fi

echo "Preflight OK (tier=${TIER})"
