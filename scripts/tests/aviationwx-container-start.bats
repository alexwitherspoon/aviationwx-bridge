#!/usr/bin/env bats
# Tests for aviationwx-container-start.sh helpers (resolve_start_version, data dir).

load test_helper

setup_file() {
	export REPO_ROOT="$(cd "$(dirname "${BATS_TEST_FILENAME}")/../.." && pwd)"
	export CONTAINER_START_LIB="${BATS_TMPDIR}/container-start-lib.sh"
	# Capture the helper functions (everything before the main body, which starts
	# at the first `mkdir -p` line) so sourcing does not launch a container. Omit
	# the strict-mode line so sourcing does not mutate the bats harness shell options.
	awk '/^mkdir -p /{exit} {print}' "$REPO_ROOT/scripts/aviationwx-container-start.sh" \
		| grep -v '^set -euo pipefail$' >"$CONTAINER_START_LIB"
}

setup() {
	export DATA_DIR="${BATS_TEST_TMPDIR}/data"
	export AVIATIONWX_DATA_DIR="$DATA_DIR"
	# Point at a non-existent path so host-installed supervisor cannot affect tests.
	export AVIATIONWX_SUPERVISOR="${BATS_TEST_TMPDIR}/no-aviationwx-supervisor.sh"
	# Start each test with no inherited tmpfs override.
	unset AVIATIONWX_TMPFS_SIZE
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

@test "read_env_file_var returns empty when the file is missing" {
	run read_env_file_var "${DATA_DIR}/environment" "AVIATIONWX_TMPFS_SIZE"
	[ "$status" -eq 0 ]
	[ "$output" = "" ]
}

@test "read_env_file_var returns the value for a key" {
	mkdir -p "$DATA_DIR"
	printf 'AVIATIONWX_TMPFS_SIZE=300m\n' >"${DATA_DIR}/environment"
	run read_env_file_var "${DATA_DIR}/environment" "AVIATIONWX_TMPFS_SIZE"
	[ "$status" -eq 0 ]
	[ "$output" = "300m" ]
}

@test "read_env_file_var skips comments and strips inline comments" {
	mkdir -p "$DATA_DIR"
	printf '# comment\nAVIATIONWX_TMPFS_SIZE=300m   # inline note\n' >"${DATA_DIR}/environment"
	run read_env_file_var "${DATA_DIR}/environment" "AVIATIONWX_TMPFS_SIZE"
	[ "$status" -eq 0 ]
	[ "$output" = "300m" ]
}

@test "read_env_file_var honors the last definition and strips quotes" {
	mkdir -p "$DATA_DIR"
	printf 'AVIATIONWX_TMPFS_SIZE=150m\nAVIATIONWX_TMPFS_SIZE="400m"\n' >"${DATA_DIR}/environment"
	run read_env_file_var "${DATA_DIR}/environment" "AVIATIONWX_TMPFS_SIZE"
	[ "$status" -eq 0 ]
	[ "$output" = "400m" ]
}

@test "resolve_tmpfs_spec uses the RAM-derived default with no override" {
	run resolve_tmpfs_spec "200m"
	[ "$status" -eq 0 ]
	[ "$output" = "200m" ]
}

@test "resolve_tmpfs_spec honors the AVIATIONWX_TMPFS_SIZE process override" {
	export AVIATIONWX_TMPFS_SIZE="1g"
	run resolve_tmpfs_spec "200m"
	[ "$status" -eq 0 ]
	[ "$output" = "1g" ]
}

@test "resolve_tmpfs_spec reads the override from the environment file" {
	mkdir -p "$DATA_DIR"
	printf 'AVIATIONWX_TMPFS_SIZE=512m\n' >"${DATA_DIR}/environment"
	run resolve_tmpfs_spec "200m"
	[ "$status" -eq 0 ]
	[ "$output" = "512m" ]
}

@test "resolve_tmpfs_spec prefers the process env over the file" {
	mkdir -p "$DATA_DIR"
	printf 'AVIATIONWX_TMPFS_SIZE=512m\n' >"${DATA_DIR}/environment"
	export AVIATIONWX_TMPFS_SIZE="256m"
	run resolve_tmpfs_spec "200m"
	[ "$status" -eq 0 ]
	[ "$output" = "256m" ]
}

@test "resolve_tmpfs_spec rejects an invalid override and falls back to default" {
	export AVIATIONWX_TMPFS_SIZE="not-a-size"
	run resolve_tmpfs_spec "200m"
	[ "$status" -eq 0 ]
	[[ "$output" == *"200m"* ]]
	[[ "$output" == *"ignoring invalid"* ]]
}

@test "container start applies the resolved tmpfs spec to the mount" {
	run grep -F -- '--tmpfs "/dev/shm:size=${TMPFS_SPEC}"' "$REPO_ROOT/scripts/aviationwx-container-start.sh"
	[ "$status" -eq 0 ]
}
