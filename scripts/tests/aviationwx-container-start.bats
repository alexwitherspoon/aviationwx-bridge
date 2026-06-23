#!/usr/bin/env bats
# Tests for aviationwx-container-start.sh helpers (resolve_start_version, data dir).

load test_helper

setup_file() {
	export REPO_ROOT="$(cd "$(dirname "${BATS_TEST_FILENAME}")/../.." && pwd)"
	export CONTAINER_START_LIB="${BATS_TMPDIR}/container-start-lib.sh"
	# Omit strict-mode line so sourcing does not mutate the bats harness shell options.
	sed -n '1,130p' "$REPO_ROOT/scripts/aviationwx-container-start.sh" \
		| grep -v '^set -euo pipefail$' >"$CONTAINER_START_LIB"
}

setup() {
	export DATA_DIR="${BATS_TEST_TMPDIR}/data"
	export AVIATIONWX_DATA_DIR="$DATA_DIR"
	# Point at a non-existent path so host-installed supervisor cannot affect tests.
	export AVIATIONWX_SUPERVISOR="${BATS_TEST_TMPDIR}/no-aviationwx-supervisor.sh"
	rm -rf "$DATA_DIR"
	# shellcheck source=/dev/null
	source "$CONTAINER_START_LIB"
}

@test "resolve_start_version passes through semver tags" {
	run resolve_start_version "2.9.1"
	[ "$status" -eq 0 ]
	[ "$output" = "2.9.1" ]
}

@test "resolve_start_version passes through edge channel" {
	run resolve_start_version "edge"
	[ "$status" -eq 0 ]
	[ "$output" = "edge" ]
}

@test "resolve_start_version falls back when supervisor is not installed" {
	run resolve_start_version "latest"
	[ "$status" -eq 0 ]
	[ "$output" = "latest" ]
}

@test "startup mkdir allows writing configured-image-tag on fresh data dir" {
	[ ! -d "$DATA_DIR" ]
	run bash -c "
		export DATA_DIR='$DATA_DIR'
		export AVIATIONWX_DATA_DIR='$DATA_DIR'
		mkdir -p \"\${DATA_DIR}\"
		echo '2.9.0' >\"\${DATA_DIR}/configured-image-tag.txt\"
		test -f \"\${DATA_DIR}/configured-image-tag.txt\"
	"
	[ "$status" -eq 0 ]
}

@test "container start passes AVIATIONWX_SELF_UPDATE with host override default" {
	run grep -F 'AVIATIONWX_SELF_UPDATE=${AVIATIONWX_SELF_UPDATE:-1}' "$REPO_ROOT/scripts/aviationwx-container-start.sh"
	[ "$status" -eq 0 ]
}
