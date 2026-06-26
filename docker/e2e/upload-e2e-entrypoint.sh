#!/usr/bin/env bash
# E2E upload wrapper: TLS nginx on 443, then aviationwx.org entrypoint (Apache :80, SFTP :2222).
set -euo pipefail

if [[ ! -f /etc/nginx/ssl/fullchain.pem || ! -f /etc/nginx/ssl/privkey.pem ]]; then
  echo "E2E upload: missing TLS certs in /etc/nginx/ssl" >&2
  exit 1
fi

nginx -t
nginx

exec /usr/local/bin/docker-entrypoint.sh
