#!/usr/bin/env bats
# Semver gate for Go >= go.mod (same logic as scripts/test-local.sh).

@test "awk reads go directive from repo go.mod" {
  local root
  root="$(cd "$(dirname "${BATS_TEST_FILENAME}")/../.." && pwd)"
  run awk '/^go[[:space:]]+[0-9]/{gsub(/\r/,""); print $2; exit}' "${root}/go.mod"
  [ "$status" -eq 0 ]
  [ -n "$output" ]
}

@test "toolchain older than go.mod fails check" {
  REQ_GO=1.26.0
  HAVE_GO=1.25.0
  [ "$(printf '%s\n' "$REQ_GO" "$HAVE_GO" | sort -V | head -n1)" != "$REQ_GO" ]
}

@test "toolchain equal to go.mod passes check" {
  REQ_GO=1.26.0
  HAVE_GO=1.26.0
  [ "$(printf '%s\n' "$REQ_GO" "$HAVE_GO" | sort -V | head -n1)" = "$REQ_GO" ]
}

@test "toolchain newer than go.mod passes check" {
  REQ_GO=1.26.0
  HAVE_GO=1.27.0
  [ "$(printf '%s\n' "$REQ_GO" "$HAVE_GO" | sort -V | head -n1)" = "$REQ_GO" ]
}
