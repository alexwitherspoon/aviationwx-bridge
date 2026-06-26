#!/usr/bin/env bash
# E2E harness orchestration: build images, start compose, run tagged Go tests.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

TIER="all"
KEEP_UP=0
NO_BUILD=0
GO_TEST_ARGS=()

usage() {
  cat <<'EOF'
Usage: scripts/e2e-run.sh [options] [-- go-test-args...]

Options:
  --tier 0|1|2|all   Services and tests to run (default: all)
  --keep-up          Leave compose running after tests
  --no-build         Skip docker build
  -h, --help         Show help

Environment:
  AVIATIONWX_ORG_ROOT   Sibling aviationwx.org checkout (default: ../aviationwx.org)
  AVIATIONWX_ORG_SHA    Optional git ref to checkout in sibling before build

Examples:
  scripts/e2e-run.sh --tier 0
  scripts/e2e-run.sh --tier 2 --keep-up
  AVIATIONWX_ORG_SHA=abc1234 scripts/e2e-run.sh --tier 1
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tier)
      TIER="${2:?}"
      shift 2
      ;;
    --keep-up)
      KEEP_UP=1
      shift
      ;;
    --no-build)
      NO_BUILD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      GO_TEST_ARGS=("$@")
      break
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if ! docker info >/dev/null 2>&1; then
  echo "Docker daemon is not running" >&2
  exit 1
fi

ORG_ROOT="${AVIATIONWX_ORG_ROOT:-${ROOT}/../aviationwx.org}"
ORG_ROOT="$(cd "${ORG_ROOT}" 2>/dev/null && pwd || true)"
if [[ -z "${ORG_ROOT}" || ! -d "${ORG_ROOT}" ]]; then
  echo "aviationwx.org checkout not found at AVIATIONWX_ORG_ROOT=${AVIATIONWX_ORG_ROOT:-../aviationwx.org}" >&2
  exit 1
fi

if [[ -n "${AVIATIONWX_ORG_SHA:-}" ]]; then
  echo "Checking out aviationwx.org@${AVIATIONWX_ORG_SHA} in ${ORG_ROOT}"
  git -C "${ORG_ROOT}" checkout "${AVIATIONWX_ORG_SHA}"
fi

if ! [[ -f "${ROOT}/testdata/e2e/tls/ca.pem" \
  && -f "${ROOT}/testdata/e2e/tls/fullchain.pem" \
  && -f "${ROOT}/testdata/e2e/tls/privkey.pem" ]]; then
  "${ROOT}/scripts/e2e/gen-test-tls.sh"
fi

COMPOSE=(docker compose -f docker/docker-compose.e2e.yml)
SERVICES=(upload bridge)
PROFILE_ARGS=()
case "${TIER}" in
  0|1) ;;
  2|all) PROFILE_ARGS=(--profile tier2); SERVICES+=(camera-simulator) ;;
  *)
    echo "Invalid --tier ${TIER}" >&2
    exit 1
    ;;
esac

compose_cmd() {
  if [[ ${#PROFILE_ARGS[@]} -gt 0 ]]; then
    "${COMPOSE[@]}" "${PROFILE_ARGS[@]}" "$@"
  else
    "${COMPOSE[@]}" "$@"
  fi
}

if [[ "${NO_BUILD}" -eq 0 ]]; then
  echo "Building aviationwx.org base image..."
  docker build -f "${ORG_ROOT}/docker/Dockerfile" -t aviationwx-e2e-upload-base "${ORG_ROOT}"
  echo "Building E2E images..."
  "${COMPOSE[@]}" build upload bridge
  if [[ "${TIER}" == "2" || "${TIER}" == "all" ]]; then
    compose_cmd build camera-simulator
  fi
fi

cleanup() {
  if [[ "${KEEP_UP}" -eq 1 ]]; then
    echo "Leaving E2E stack running (--keep-up)"
    return
  fi
  compose_cmd down -v --remove-orphans >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "Starting E2E services: ${SERVICES[*]}"
compose_cmd up -d "${SERVICES[@]}"

if [[ "${TIER}" == "2" || "${TIER}" == "all" ]]; then
  "${ROOT}/scripts/e2e/build-fixtures.sh"
fi

export AVIATIONWX_E2E_STACK=1
export AVIATIONWX_E2E_ROOT="${ROOT}"
export E2E_UPLOAD_HTTPS_ADDR="${E2E_UPLOAD_HTTPS_ADDR:-127.0.0.1:18443}"
export E2E_UPLOAD_SFTP_ADDR="${E2E_UPLOAD_SFTP_ADDR:-127.0.0.1:12223}"
export E2E_BRIDGE_URL="${E2E_BRIDGE_URL:-http://127.0.0.1:1231}"

echo "Running preflight..."
"${ROOT}/scripts/e2e-preflight.sh" "${TIER}"

RUN=()
case "${TIER}" in
  0) RUN=(-run 'UploadContract|UploadIdentity') ;;
  1) RUN=(-run 'UploadContract|UploadIdentity|SFTPIdentity') ;;
  2|all) RUN=() ;;
esac

echo "Running E2E Go tests (tier=${TIER})..."
if [[ ${#RUN[@]} -gt 0 ]]; then
  if [[ ${#GO_TEST_ARGS[@]} -gt 0 ]]; then
    go test -tags=e2e -count=1 -v ./test/e2e/... "${RUN[@]}" "${GO_TEST_ARGS[@]}"
  else
    go test -tags=e2e -count=1 -v ./test/e2e/... "${RUN[@]}"
  fi
else
  if [[ ${#GO_TEST_ARGS[@]} -gt 0 ]]; then
    go test -tags=e2e -count=1 -v ./test/e2e/... "${GO_TEST_ARGS[@]}"
  else
    go test -tags=e2e -count=1 -v ./test/e2e/...
  fi
fi

echo "E2E tier ${TIER} passed."
