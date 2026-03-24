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
