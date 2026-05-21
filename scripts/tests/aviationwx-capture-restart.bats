#!/usr/bin/env bats
# Tests for scripts/aviationwx-capture-restart.sh (requires bats, jq, flock for lock test).

load test_helper

@test "HTTP 200 resets consecutive counters" {
	echo '{"consecutive_unready":4,"consecutive_unreachable":2,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
	[ "$(jq -r .consecutive_unreachable "$STATE_FILE")" = "0" ]
}

@test "HTTP 503 increments consecutive_unready" {
	echo '{"consecutive_unready":0,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=503 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "1" ]
	[ "$(jq -r .consecutive_unreachable "$STATE_FILE")" = "0" ]
}

@test "non-503 resets consecutive_unready (was spurious streak)" {
	echo '{"consecutive_unready":4,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=404 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "connection failure (000) increments consecutive_unreachable" {
	echo '{"consecutive_unready":3,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
	MOCK_HTTP_CODE=000 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
	[ "$(jq -r .consecutive_unreachable "$STATE_FILE")" = "1" ]
}

@test "corrupt state file is reset then 200 persists valid JSON" {
	echo 'not json at all' >"$STATE_FILE"
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	jq empty "$STATE_FILE"
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "0" ]
}

@test "max restarts writes recovery-exhausted.json" {
	local now
	now=$(date +%s)
	local restarts
	restarts=$(jq -n --argjson now "$now" '[range(6) | $now - (. * 3600)] | map({epoch: .})')
	jq -n \
		--argjson cu 5 \
		--argjson restarts "$restarts" \
		'{consecutive_unready: $cu, consecutive_unreachable: 0, recent_restarts: $restarts}' >"$STATE_FILE"
	MOCK_HTTP_CODE=503 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ -f "$DATA_DIR/recovery-exhausted.json" ]
	[ "$(jq -r .exhausted "$DATA_DIR/recovery-exhausted.json")" = "true" ]
	[ "$(jq -r .restarts_24h "$DATA_DIR/recovery-exhausted.json")" = "6" ]
}

@test "readyz 200 clears recovery-exhausted.json" {
	echo '{"exhausted":true,"reason":"test","restarts_24h":6,"max_per_24h":6,"since":"2026-01-01T00:00:00Z"}' >"$DATA_DIR/recovery-exhausted.json"
	MOCK_HTTP_CODE=200 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ ! -f "$DATA_DIR/recovery-exhausted.json" ]
}

@test "flock excludes concurrent runs" {
	if ! command -v flock >/dev/null 2>&1; then
		skip "flock not installed"
	fi
	echo '{"consecutive_unready":0,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
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

@test "503 streak counter clamps at threshold" {
	local now
	now=$(date +%s)
	local restarts
	restarts=$(jq -n --argjson now "$now" '[range(6) | $now - (. * 3600)] | map({epoch: .})')
	jq -n \
		--argjson cu 5 \
		--argjson restarts "$restarts" \
		'{consecutive_unready: $cu, consecutive_unreachable: 0, recent_restarts: $restarts}' >"$STATE_FILE"
	MOCK_HTTP_CODE=503 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	[ "$(jq -r .consecutive_unready "$STATE_FILE")" = "5" ]
}

@test "recovery-exhausted since is UTC" {
	local now
	now=$(date +%s)
	local restarts
	restarts=$(jq -n --argjson now "$now" '[range(6) | $now - (. * 3600)] | map({epoch: .})')
	jq -n \
		--argjson cu 5 \
		--argjson restarts "$restarts" \
		'{consecutive_unready: $cu, consecutive_unreachable: 0, recent_restarts: $restarts}' >"$STATE_FILE"
	MOCK_HTTP_CODE=503 run bash "$SCRIPT"
	[ "$status" -eq 0 ]
	local since
	since=$(jq -r .since "$DATA_DIR/recovery-exhausted.json")
	[[ "$since" == *Z ]]
}
