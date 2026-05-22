# Shared setup for aviationwx-capture-restart.bats (sourced via `load test_helper`).

setup() {
	local script_dir
	script_dir="$(cd "$(dirname "${BATS_TEST_FILENAME}")" && pwd)"
	export REPO_ROOT="$(cd "$script_dir/../.." && pwd)"
	export SCRIPT="$REPO_ROOT/scripts/aviationwx-capture-restart.sh"
	export DATA_DIR="${BATS_TEST_TMPDIR}/data"
	export AVIATIONWX_DATA_DIR="$DATA_DIR"
	mkdir -p "$DATA_DIR"
	export STATE_FILE="$DATA_DIR/capture-restart-state.json"
	# Production defaults — tests must not depend on ambient shell overrides.
	export AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE=5
	export AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE_UNREACHABLE=3
	export AVIATIONWX_CAPTURE_RESTART_MIN_INTERVAL_SEC=3600
	export AVIATIONWX_CAPTURE_RESTART_MAX_PER_24H=6
	unset AVIATIONWX_CAPTURE_RESTART_URL
	export PATH="${BATS_TEST_TMPDIR}/bin:${PATH}"
	mkdir -p "${BATS_TEST_TMPDIR}/bin"
	cat >"${BATS_TEST_TMPDIR}/bin/curl" <<'EOF'
#!/bin/bash
printf '%s' "${MOCK_HTTP_CODE:-200}"
exit 0
EOF
	chmod +x "${BATS_TEST_TMPDIR}/bin/curl"
	cat >"${BATS_TEST_TMPDIR}/bin/docker" <<'EOF'
#!/bin/bash
exit 0
EOF
	chmod +x "${BATS_TEST_TMPDIR}/bin/docker"
}
