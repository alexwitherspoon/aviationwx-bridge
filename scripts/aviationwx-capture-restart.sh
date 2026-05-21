#!/bin/bash
# AviationWX.org Bridge host helper: restart container when /readyz is unreachable or
# returns HTTP 503 (capture not ready), with consecutive-check thresholds, cooldown, and
# max restarts per 24h.
#
# Installed by install.sh (aviationwx-capture-restart.timer, every 5 minutes) and invoked
# from aviationwx-watchdog.sh (every 1 minute). Thresholds are per invocation: with the
# watchdog, five consecutive 503s typically means ~5 minutes before restart. Run on the host.
#
# Environment (optional):
#   AVIATIONWX_CAPTURE_RESTART_URL                      default http://127.0.0.1:1229/readyz
#   AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE              default 5 (503 not-ready streak)
#   AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE_UNREACHABLE  default 3 (curl 000 / unreachable)
#   AVIATIONWX_CAPTURE_RESTART_MIN_INTERVAL_SEC         default 3600
#   AVIATIONWX_CAPTURE_RESTART_MAX_PER_24H              default 6
#   AVIATIONWX_DATA_DIR                                 default /data/aviationwx
#   CONTAINER_NAME                                      default aviationwx-org-bridge

set -euo pipefail

# Coerce env overrides to non-negative integers; invalid values use defaults (avoids set -e exits on bad arithmetic).
_nonneg_int() {
	local val="${1:-}"
	local def="$2"
	if [[ -z "$val" || ! "$val" =~ ^[0-9]+$ ]]; then
		echo "$def"
	else
		echo "$val"
	fi
}

readonly CONTAINER_NAME="${CONTAINER_NAME:-aviationwx-org-bridge}"
readonly READYZ_URL="${AVIATIONWX_CAPTURE_RESTART_URL:-http://127.0.0.1:1229/readyz}"
readonly CONSECUTIVE_THRESHOLD="$(_nonneg_int "${AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE:-}" 5)"
readonly UNREACHABLE_THRESHOLD="$(_nonneg_int "${AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE_UNREACHABLE:-}" 3)"
readonly MIN_INTERVAL_SEC="$(_nonneg_int "${AVIATIONWX_CAPTURE_RESTART_MIN_INTERVAL_SEC:-}" 3600)"
readonly MAX_PER_24H="$(_nonneg_int "${AVIATIONWX_CAPTURE_RESTART_MAX_PER_24H:-}" 6)"

readonly DATA_DIR="${AVIATIONWX_DATA_DIR:-/data/aviationwx}"
readonly STATE_FILE="${DATA_DIR}/capture-restart-state.json"
readonly RECOVERY_EXHAUSTED_FILE="${DATA_DIR}/recovery-exhausted.json"
readonly LOG_FILE="${DATA_DIR}/capture-restart.log"
readonly LOCK_FILE="${DATA_DIR}/capture-restart.lock"

log_event() {
	local level="$1"
	local message="$2"
	echo "[$(date -Iseconds)] [$level] $message" | tee -a "$LOG_FILE"
}

ensure_state_json() {
	if [ ! -f "$STATE_FILE" ]; then
		echo '{"consecutive_unready":0,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
	fi
	if ! jq empty "$STATE_FILE" 2>/dev/null; then
		log_event "WARN" "state file invalid JSON; resetting to defaults"
		echo '{"consecutive_unready":0,"consecutive_unreachable":0,"recent_restarts":[]}' >"$STATE_FILE"
	fi
}

acquire_lock_or_exit() {
	if ! command -v flock >/dev/null 2>&1; then
		return
	fi
	if ! exec 200>>"$LOCK_FILE"; then
		log_event "ERROR" "cannot open lock file $LOCK_FILE (permissions or disk full)"
		exit 1
	fi
	if ! flock -n 200; then
		log_event "INFO" "another capture-restart run in progress; exiting"
		exit 0
	fi
}

write_state() {
	local consecutive_unready="$1"
	local consecutive_unreachable="$2"
	local pruned_json="$3"
	jq -n \
		--argjson cu "$consecutive_unready" \
		--argjson cru "$consecutive_unreachable" \
		--argjson pruned "$pruned_json" \
		'{consecutive_unready: $cu, consecutive_unreachable: $cru, recent_restarts: $pruned}' >"${STATE_FILE}.tmp"
	mv "${STATE_FILE}.tmp" "$STATE_FILE"
}

clear_recovery_exhausted() {
	rm -f "$RECOVERY_EXHAUSTED_FILE"
}

write_recovery_exhausted() {
	local reason="$1"
	local restarts_24h="$2"
	jq -n \
		--arg reason "$reason" \
		--argjson restarts "$restarts_24h" \
		--argjson max "$MAX_PER_24H" \
		--arg since "$(date -Iseconds)" \
		'{exhausted: true, reason: $reason, restarts_24h: $restarts, max_per_24h: $max, since: $since}' \
		>"${RECOVERY_EXHAUSTED_FILE}.tmp"
	mv "${RECOVERY_EXHAUSTED_FILE}.tmp" "$RECOVERY_EXHAUSTED_FILE"
	log_event "CRITICAL" "host auto-recovery exhausted ($reason); wrote $RECOVERY_EXHAUSTED_FILE"
}

try_restart() {
	local reason="$1"
	local consecutive_unready="$2"
	local consecutive_unreachable="$3"
	local pruned_restarts="$4"
	local now_epoch="$5"

	local count_restart
	count_restart=$(echo "$pruned_restarts" | jq 'length')

	if [ "$count_restart" -ge "$MAX_PER_24H" ]; then
		log_event "ERROR" "max capture restarts in last 24h reached ($count_restart/$MAX_PER_24H); not restarting ($reason)"
		write_recovery_exhausted "$reason" "$count_restart"
		write_state "$consecutive_unready" "$consecutive_unreachable" "$pruned_restarts"
		return 0
	fi

	local last_epoch=0
	if [ "$count_restart" -gt 0 ]; then
		last_epoch=$(echo "$pruned_restarts" | jq 'max_by(.epoch) | .epoch')
	fi

	local elapsed=$((now_epoch - last_epoch))
	if [ "$last_epoch" -gt 0 ] && [ "$elapsed" -lt "$MIN_INTERVAL_SEC" ]; then
		log_event "WARN" "cooldown active (${elapsed}s < ${MIN_INTERVAL_SEC}s); not restarting ($reason)"
		write_state "$consecutive_unready" "$consecutive_unreachable" "$pruned_restarts"
		return 0
	fi

	log_event "ACTION" "restarting container $CONTAINER_NAME ($reason)"
	if docker restart "$CONTAINER_NAME"; then
		local updated
		updated=$(echo "$pruned_restarts" | jq -c --argjson e "$now_epoch" '. + [{epoch: $e}]')
		clear_recovery_exhausted
		write_state 0 0 "$updated"
		log_event "INFO" "restart issued successfully"
	else
		log_event "ERROR" "docker restart failed"
		write_state "$consecutive_unready" "$consecutive_unreachable" "$pruned_restarts"
		return 1
	fi
}

main() {
	mkdir -p "$DATA_DIR" || true
	acquire_lock_or_exit
	ensure_state_json

	local now_epoch
	now_epoch=$(date +%s)

	local code
	if ! code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 --max-time 10 "$READYZ_URL" 2>/dev/null); then
		code="000"
	fi

	local consecutive_unready consecutive_unreachable
	consecutive_unready=$(jq -r '.consecutive_unready // 0' "$STATE_FILE")
	consecutive_unreachable=$(jq -r '.consecutive_unreachable // 0' "$STATE_FILE")

	local pruned_restarts
	pruned_restarts=$(jq -c --argjson now "$now_epoch" \
		'[ .recent_restarts[]? | select(.epoch > ($now - 86400)) ]' "$STATE_FILE")

	if [ "$code" = "200" ]; then
		if [ "$consecutive_unready" != "0" ] || [ "$consecutive_unreachable" != "0" ]; then
			log_event "INFO" "readyz OK; reset consecutive (unready=$consecutive_unready unreachable=$consecutive_unreachable)"
		fi
		clear_recovery_exhausted
		write_state 0 0 "$pruned_restarts"
		return 0
	fi

	if [ "$code" = "503" ]; then
		consecutive_unready=$((consecutive_unready + 1))
		consecutive_unreachable=0
		log_event "WARN" "readyz not ready (503), consecutive=$consecutive_unready/$CONSECUTIVE_THRESHOLD"

		if [ "$consecutive_unready" -lt "$CONSECUTIVE_THRESHOLD" ]; then
			write_state "$consecutive_unready" 0 "$pruned_restarts"
			return 0
		fi
		try_restart "capture readiness stuck" "$consecutive_unready" 0 "$pruned_restarts" "$now_epoch"
		return $?
	fi

	if [ "$code" = "000" ]; then
		consecutive_unreachable=$((consecutive_unreachable + 1))
		consecutive_unready=0
		log_event "WARN" "readyz unreachable (000), consecutive=$consecutive_unreachable/$UNREACHABLE_THRESHOLD"

		if [ "$consecutive_unreachable" -lt "$UNREACHABLE_THRESHOLD" ]; then
			write_state 0 "$consecutive_unreachable" "$pruned_restarts"
			return 0
		fi
		try_restart "bridge unreachable" 0 "$consecutive_unreachable" "$pruned_restarts" "$now_epoch"
		return $?
	fi

	log_event "WARN" "readyz HTTP $code (resetting consecutive; not restarting)"
	write_state 0 0 "$pruned_restarts"
	return 0
}

main "$@"
