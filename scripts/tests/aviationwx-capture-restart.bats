#!/usr/bin/env bats
# Tests for scripts/aviationwx-capture-restart.sh (requires bats, jq, flock for lock test).

load test_helper

@test "HTTP 200 resets consecutive_unready" {
	echo '{"consecutive_unready":4,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "HTTP 503 increments consecutive_unready" {
	echo '{"consecutive_unready":0,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=503 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "1" ]
}

@test "non-503 resets consecutive_unready (was spurious streak)" {
	echo '{"consecutive_unready":4,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=404 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "connection failure (000) resets consecutive_unready" {
	echo '{"consecutive_unready":3,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=000 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "corrupt state file is reset then 200 persists valid JSON" {
	echo 'not json at all' >"$STATE_FILE"
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	jq empty "$STATE_FILE"
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "flock excludes concurrent runs" {
	if ! command -v flock >/dev/null 2>&1; then
		skip "flock not installed"
	fi
	echo '{"consecutive_unready":0,"recent_restarts":[]}' >"$STATE_FILE"
	(
		exec 200>>"$DATA_DIR/capture-restart.lock"
		flock 200
		sleep 2
	) &
	local bg=$!
	sleep 0.2
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	kill "$bg" 2>/dev/null || true
	wait "$bg" 2>/dev/null || true
	[ "$status" -eq 0 ]
	[[ "$output" == *"another capture-restart run in progress"* ]]
}
