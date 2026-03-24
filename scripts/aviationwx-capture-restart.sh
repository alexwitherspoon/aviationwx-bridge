#!/bin/bash
# AviationWX.org Bridge — optional host helper: restart container when /readyz returns HTTP 503
# (capture not ready), with consecutive-check threshold, cooldown, and max restarts per 24h.
#
# Run on the host (not inside the container). Example: every 5 minutes via cron:
#   */5 * * * * /usr/local/bin/aviationwx-capture-restart.sh
#
# Environment (optional):
#   AVIATIONWX_CAPTURE_RESTART_URL              default http://127.0.0.1:1229/readyz
#   AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE      default 5
#   AVIATIONWX_CAPTURE_RESTART_MIN_INTERVAL_SEC default 3600
#   AVIATIONWX_CAPTURE_RESTART_MAX_PER_24H      default 6
#   AVIATIONWX_DATA_DIR                         default /data/aviationwx
#   CONTAINER_NAME                              default aviationwx-org-bridge

set -euo pipefail

readonly CONTAINER_NAME="${CONTAINER_NAME:-aviationwx-org-bridge}"
readonly READYZ_URL="${AVIATIONWX_CAPTURE_RESTART_URL:-http://127.0.0.1:1229/readyz}"
readonly CONSECUTIVE_THRESHOLD="${AVIATIONWX_CAPTURE_RESTART_CONSECUTIVE:-5}"
readonly MIN_INTERVAL_SEC="${AVIATIONWX_CAPTURE_RESTART_MIN_INTERVAL_SEC:-3600}"
readonly MAX_PER_24H="${AVIATIONWX_CAPTURE_RESTART_MAX_PER_24H:-6}"

readonly DATA_DIR="${AVIATIONWX_DATA_DIR:-/data/aviationwx}"
readonly STATE_FILE="${DATA_DIR}/capture-restart-state.json"
readonly LOG_FILE="${DATA_DIR}/capture-restart.log"

log_event() {
	local level="$1"
	local message="$2"
	echo "[$(date -Iseconds)] [$level] $message" | tee -a "$LOG_FILE"
}

main() {
	mkdir -p "$DATA_DIR" || true
	if [ ! -f "$STATE_FILE" ]; then
		echo '{"consecutive_unready":0,"recent_restarts":[]}' >"$STATE_FILE"
	fi

	local now_epoch
	now_epoch=$(date +%s)

	local code
	code=$(curl -s -o /dev/null -w "%{http_code}" --connect-timeout 3 --max-time 10 "$READYZ_URL" || echo "000")

	local consecutive
	consecutive=$(jq -r '.consecutive_unready // 0' "$STATE_FILE")

	# Prune restarts older than 24h (epoch seconds)
	local pruned_restarts
	pruned_restarts=$(jq -c --argjson now "$now_epoch" \
		'[ .recent_restarts[]? | select(.epoch > ($now - 86400)) ]' "$STATE_FILE")

	local count_restart
	count_restart=$(echo "$pruned_restarts" | jq 'length')

	if [ "$code" = "200" ]; then
		if [ "$consecutive" != "0" ]; then
			log_event "INFO" "readyz OK; reset consecutive (was $consecutive)"
		fi
		jq -n \
			--argjson pruned "$pruned_restarts" \
			'{consecutive_unready: 0, recent_restarts: $pruned}' >"${STATE_FILE}.tmp"
		mv "${STATE_FILE}.tmp" "$STATE_FILE"
		return 0
	fi

	if [ "$code" != "503" ]; then
		log_event "WARN" "readyz HTTP $code (only 503 increases consecutive; not restarting)"
		return 0
	fi

	consecutive=$((consecutive + 1))
	log_event "WARN" "readyz not ready (503), consecutive=$consecutive/$CONSECUTIVE_THRESHOLD"

	if [ "$consecutive" -lt "$CONSECUTIVE_THRESHOLD" ]; then
		jq -n \
			--argjson cu "$consecutive" \
			--argjson pruned "$pruned_restarts" \
			'{consecutive_unready: $cu, recent_restarts: $pruned}' >"${STATE_FILE}.tmp"
		mv "${STATE_FILE}.tmp" "$STATE_FILE"
		return 0
	fi

	if [ "$count_restart" -ge "$MAX_PER_24H" ]; then
		log_event "ERROR" "max capture restarts in last 24h reached ($count_restart/$MAX_PER_24H); not restarting"
		jq -n \
			--argjson pruned "$pruned_restarts" \
			'{consecutive_unready: 0, recent_restarts: $pruned}' >"${STATE_FILE}.tmp"
		mv "${STATE_FILE}.tmp" "$STATE_FILE"
		return 0
	fi

	local last_epoch=0
	if [ "$count_restart" -gt 0 ]; then
		last_epoch=$(echo "$pruned_restarts" | jq 'max_by(.epoch) | .epoch')
	fi

	local elapsed=$((now_epoch - last_epoch))
	if [ "$last_epoch" -gt 0 ] && [ "$elapsed" -lt "$MIN_INTERVAL_SEC" ]; then
		log_event "WARN" "cooldown active (${elapsed}s < ${MIN_INTERVAL_SEC}s); not restarting"
		jq -n \
			--argjson cu "$consecutive" \
			--argjson pruned "$pruned_restarts" \
			'{consecutive_unready: $cu, recent_restarts: $pruned}' >"${STATE_FILE}.tmp"
		mv "${STATE_FILE}.tmp" "$STATE_FILE"
		return 0
	fi

	log_event "ACTION" "restarting container $CONTAINER_NAME (capture readiness stuck)"
	if docker restart "$CONTAINER_NAME"; then
		local updated
		updated=$(echo "$pruned_restarts" | jq -c --argjson e "$now_epoch" '. + [{epoch: $e}]')
		jq -n \
			--argjson updated "$updated" \
			'{consecutive_unready: 0, recent_restarts: $updated}' >"${STATE_FILE}.tmp"
		mv "${STATE_FILE}.tmp" "$STATE_FILE"
		log_event "INFO" "restart issued successfully"
	else
		log_event "ERROR" "docker restart failed"
		return 1
	fi
}

main "$@"
