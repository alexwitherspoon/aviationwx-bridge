#!/usr/bin/env bash
# Regenerate E2E harness TLS material for upload.e2e.test (idempotent).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
TLS_DIR="${ROOT}/testdata/e2e/tls"
HOST="upload.e2e.test"
DAYS=825

mkdir -p "${TLS_DIR}"

CA_KEY="${TLS_DIR}/ca-key.pem"
CA_CERT="${TLS_DIR}/ca.pem"
SERVER_KEY="${TLS_DIR}/privkey.pem"
SERVER_CSR="${TLS_DIR}/server.csr"
SERVER_CERT="${TLS_DIR}/server.pem"
FULLCHAIN="${TLS_DIR}/fullchain.pem"

# A committed or leftover ca.pem without the matching ca-key.pem breaks signing.
if [[ -f "${CA_CERT}" && ! -f "${CA_KEY}" ]]; then
  echo "Removing stale ${CA_CERT} (CA private key missing)" >&2
  rm -f "${CA_CERT}"
fi

if [[ ! -f "${CA_KEY}" ]]; then
  openssl genrsa -out "${CA_KEY}" 4096
  chmod 600 "${CA_KEY}"
fi

if [[ ! -f "${CA_CERT}" ]]; then
  openssl req -x509 -new -nodes -key "${CA_KEY}" -sha256 -days "${DAYS}" \
    -subj "/CN=AviationWX E2E Test CA" \
    -out "${CA_CERT}"
  chmod 644 "${CA_CERT}"
fi

openssl genrsa -out "${SERVER_KEY}" 2048
chmod 600 "${SERVER_KEY}"

openssl req -new -key "${SERVER_KEY}" \
  -subj "/CN=${HOST}" \
  -out "${SERVER_CSR}"

openssl x509 -req -in "${SERVER_CSR}" \
  -CA "${CA_CERT}" -CAkey "${CA_KEY}" -CAcreateserial \
  -out "${SERVER_CERT}" -days "${DAYS}" -sha256 \
  -extfile <(printf "subjectAltName=DNS:%s" "${HOST}")

cat "${SERVER_CERT}" "${CA_CERT}" > "${FULLCHAIN}"
chmod 644 "${FULLCHAIN}" "${SERVER_CERT}"
rm -f "${SERVER_CSR}" "${TLS_DIR}/ca.srl"

echo "Wrote ${TLS_DIR}/ca.pem, fullchain.pem, privkey.pem"
