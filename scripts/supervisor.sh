#!/bin/bash
#
# AviationWX Bridge - Update Supervisor
# https://github.com/alexwitherspoon/aviationwx-bridge
#
# This script manages automatic updates for AviationWX Bridge.
# It is installed by install.sh and runs via systemd timer.
#
# Version: 1 (increment only for breaking changes to this script)
#
# Usage:
#   aviationwx-supervisor check     # Check for updates (default)
#   aviationwx-supervisor update    # Force update to latest
#   aviationwx-supervisor rollback  # Rollback to previous version
#   aviationwx-supervisor status    # Show current status
#

set -euo pipefail

# Configuration
GITHUB_REPO="alexwitherspoon/aviationwx-bridge"
IMAGE_NAME="ghcr.io/${GITHUB_REPO}"
CONTAINER_NAME="aviationwx-bridge"
DATA_DIR="/data/aviationwx"
WEB_PORT="1229"
ENV_FILE="${DATA_DIR}/environment"
DEFAULT_TMPFS_SIZE="200m"

# Timeouts and intervals
HEALTH_TIMEOUT=120          # Seconds to wait for health check
GRACE_PERIOD_CRITICAL=86400 # 24 hours before critical updates auto-apply

# State files
CURRENT_VERSION_FILE="${DATA_DIR}/current-version"
ROLLBACK_VERSION_FILE="${DATA_DIR}/rollback-version"
UPDATE_AVAILABLE_FILE="${DATA_DIR}/update-available.json"
UPDATE_REQUEST_FILE="${DATA_DIR}/update-request"
CRITICAL_SEEN_FILE="${DATA_DIR}/critical-update-seen"
LOG_FILE="${DATA_DIR}/supervisor.log"

# Ensure data directory exists
mkdir -p "${DATA_DIR}"

# Load environment overrides if present
load_environment() {
    if [[ -f "${ENV_FILE}" ]]; then
        # shellcheck source=/dev/null
        source "${ENV_FILE}"
    fi
}

# Get tmpfs size from environment or use default
get_tmpfs_size() {
    load_environment
    echo "${AVIATIONWX_TMPFS_SIZE:-${DEFAULT_TMPFS_SIZE}}"
}

# Logging
log() {
    local level="$1"
    shift
    local message="$*"
    local timestamp
    timestamp=$(date -Iseconds)
    echo "[${timestamp}] [${level}] ${message}" | tee -a "${LOG_FILE}"
}

log_info() { log "INFO" "$@"; }
log_warn() { log "WARN" "$@"; }
log_error() { log "ERROR" "$@"; }

# Get current running version
get_current_version() {
    if [[ -f "${CURRENT_VERSION_FILE}" ]]; then
        cat "${CURRENT_VERSION_FILE}"
    else
        # Try to get from running container
        docker inspect "${CONTAINER_NAME}" --format '{{.Config.Image}}' 2>/dev/null | cut -d: -f2 || echo "unknown"
    fi
}

# Fetch latest release from GitHub
get_latest_release() {
    curl -sf \
        -H "Accept: application/vnd.github.v3+json" \
        -H "User-Agent: AviationWX-Supervisor/1" \
        "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" \
        2>/dev/null
}

# Parse release metadata from release body
# Looks for: <!-- AVIATIONWX_RELEASE_META {...} -->
parse_release_meta() {
    local body="$1"
    echo "$body" | grep -oP '(?<=<!-- AVIATIONWX_RELEASE_META ).*(?= -->)' | head -1 || echo "{}"
}

# Check if container is healthy
check_health() {
    local attempts=0
    while [[ $attempts -lt $HEALTH_TIMEOUT ]]; do
        if curl -sf "http://localhost:${WEB_PORT}/healthz" > /dev/null 2>&1; then
            return 0
        fi
        sleep 1
        ((attempts++))
    done
    return 1
}

# Perform the update
do_update() {
    local new_version="$1"
    local new_tag="${2:-$new_version}"
    local tmpfs_size
    tmpfs_size=$(get_tmpfs_size)

    log_info "Starting update to ${new_version}"

    # Save rollback point
    local current
    current=$(get_current_version)
    if [[ "$current" != "unknown" && "$current" != "$new_version" ]]; then
        echo "$current" > "${ROLLBACK_VERSION_FILE}"
        log_info "Rollback point saved: ${current}"
    fi

    # Pull new image
    log_info "Pulling ${IMAGE_NAME}:${new_tag}"
    if ! docker pull "${IMAGE_NAME}:${new_tag}"; then
        log_error "Failed to pull image"
        return 1
    fi

    # Stop current container
    log_info "Stopping current container"
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    # Start new container
    log_info "Starting new container with ${new_tag} (tmpfs: ${tmpfs_size})"
    docker run -d \
        --name "${CONTAINER_NAME}" \
        --restart=unless-stopped \
        -p "${WEB_PORT}:${WEB_PORT}" \
        -v "${DATA_DIR}:/data" \
        --tmpfs /dev/shm:size="${tmpfs_size}" \
        "${IMAGE_NAME}:${new_tag}"

    # Wait for health check
    log_info "Waiting for health check (${HEALTH_TIMEOUT}s timeout)"
    if check_health; then
        log_info "Health check passed"
        echo "${new_version}" > "${CURRENT_VERSION_FILE}"
        rm -f "${UPDATE_AVAILABLE_FILE}" "${UPDATE_REQUEST_FILE}" "${CRITICAL_SEEN_FILE}"
        log_info "Update to ${new_version} completed successfully"
        return 0
    else
        log_error "Health check failed after ${HEALTH_TIMEOUT}s"
        do_rollback
        return 1
    fi
}

# Rollback to previous version
do_rollback() {
    if [[ ! -f "${ROLLBACK_VERSION_FILE}" ]]; then
        log_error "No rollback version available"
        return 1
    fi

    local rollback_version tmpfs_size
    rollback_version=$(cat "${ROLLBACK_VERSION_FILE}")
    tmpfs_size=$(get_tmpfs_size)
    log_warn "Rolling back to ${rollback_version}"

    # Stop current container
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    # Start rollback version
    docker run -d \
        --name "${CONTAINER_NAME}" \
        --restart=unless-stopped \
        -p "${WEB_PORT}:${WEB_PORT}" \
        -v "${DATA_DIR}:/data" \
        --tmpfs /dev/shm:size="${tmpfs_size}" \
        "${IMAGE_NAME}:${rollback_version}"

    if check_health; then
        log_info "Rollback to ${rollback_version} successful"
        echo "${rollback_version}" > "${CURRENT_VERSION_FILE}"
        rm -f "${CRITICAL_SEEN_FILE}"
        return 0
    else
        log_error "CRITICAL: Rollback also failed! Manual intervention required."
        return 1
    fi
}

# Check for updates
check_updates() {
    log_info "Checking for updates..."

    local current_version
    current_version=$(get_current_version)
    log_info "Current version: ${current_version}"

    # Check for user-requested update
    if [[ -f "${UPDATE_REQUEST_FILE}" ]]; then
        log_info "User requested update via web UI"
        rm -f "${UPDATE_REQUEST_FILE}"
    fi

    # Fetch latest release
    local release
    release=$(get_latest_release)
    if [[ -z "$release" ]]; then
        log_warn "Failed to fetch release info from GitHub"
        return 0
    fi

    # Parse release info
    local latest_tag latest_version release_body release_url
    latest_tag=$(echo "$release" | jq -r '.tag_name // empty')
    latest_version="${latest_tag#v}"  # Remove 'v' prefix
    release_body=$(echo "$release" | jq -r '.body // ""')
    release_url=$(echo "$release" | jq -r '.html_url // ""')

    if [[ -z "$latest_tag" ]]; then
        log_warn "Could not parse latest release tag"
        return 0
    fi

    log_info "Latest version: ${latest_version}"

    # Check if already on latest
    if [[ "$current_version" == "$latest_version" ]]; then
        log_info "Already running latest version"
        rm -f "${UPDATE_AVAILABLE_FILE}"
        return 0
    fi

    # Parse release metadata
    local meta is_critical is_force
    meta=$(parse_release_meta "$release_body")
    is_critical=$(echo "$meta" | jq -r '.critical // false')
    is_force=$(echo "$meta" | jq -r '.force_update // false')

    log_info "Update available: ${current_version} → ${latest_version} (critical=${is_critical}, force=${is_force})"

    # Handle force update (emergency - immediate)
    if [[ "$is_force" == "true" ]]; then
        log_warn "FORCE UPDATE flagged - applying immediately"
        do_update "$latest_version" "$latest_tag"
        return $?
    fi

    # Handle critical update (with grace period)
    if [[ "$is_critical" == "true" ]]; then
        if [[ -f "${CRITICAL_SEEN_FILE}" ]]; then
            local seen_time now elapsed remaining
            seen_time=$(cat "${CRITICAL_SEEN_FILE}")
            now=$(date +%s)
            elapsed=$((now - seen_time))

            if [[ $elapsed -ge $GRACE_PERIOD_CRITICAL ]]; then
                log_warn "Critical update grace period (${GRACE_PERIOD_CRITICAL}s) elapsed - applying update"
                do_update "$latest_version" "$latest_tag"
                return $?
            else
                remaining=$((GRACE_PERIOD_CRITICAL - elapsed))
                log_info "Critical update pending, ${remaining}s until auto-apply"
            fi
        else
            log_info "Critical update detected, starting ${GRACE_PERIOD_CRITICAL}s grace period"
            date +%s > "${CRITICAL_SEEN_FILE}"
        fi
    fi

    # Write update-available file for web UI
    cat > "${UPDATE_AVAILABLE_FILE}" << EOF
{
  "available": true,
  "current_version": "${current_version}",
  "latest_version": "${latest_version}",
  "latest_tag": "${latest_tag}",
  "release_url": "${release_url}",
  "critical": ${is_critical},
  "force": ${is_force},
  "checked_at": "$(date -Iseconds)"
}
EOF

    log_info "Update notification written to ${UPDATE_AVAILABLE_FILE}"
    return 0
}

# Show status
show_status() {
    echo "AviationWX Bridge Supervisor Status"
    echo "===================================="
    echo ""
    echo "Current version: $(get_current_version)"
    echo "Rollback version: $(cat "${ROLLBACK_VERSION_FILE}" 2>/dev/null || echo "none")"
    echo ""
    
    if [[ -f "${UPDATE_AVAILABLE_FILE}" ]]; then
        echo "Update available:"
        cat "${UPDATE_AVAILABLE_FILE}"
    else
        echo "No update available"
    fi
    echo ""

    if [[ -f "${CRITICAL_SEEN_FILE}" ]]; then
        local seen_time now elapsed remaining
        seen_time=$(cat "${CRITICAL_SEEN_FILE}")
        now=$(date +%s)
        elapsed=$((now - seen_time))
        remaining=$((GRACE_PERIOD_CRITICAL - elapsed))
        echo "Critical update pending: ${remaining}s until auto-apply"
    fi
    echo ""

    echo "Container status:"
    docker ps --filter "name=${CONTAINER_NAME}" --format "table {{.Status}}\t{{.Image}}"
    echo ""

    echo "Health check:"
    if curl -sf "http://localhost:${WEB_PORT}/healthz" > /dev/null 2>&1; then
        echo "  ✓ Healthy"
    else
        echo "  ✗ Unhealthy or not running"
    fi
}

# Main entry point
main() {
    local command="${1:-check}"

    case "$command" in
        check)
            check_updates
            ;;
        update)
            log_info "Manual update requested"
            local release latest_tag latest_version
            release=$(get_latest_release)
            latest_tag=$(echo "$release" | jq -r '.tag_name // empty')
            latest_version="${latest_tag#v}"
            if [[ -n "$latest_version" ]]; then
                do_update "$latest_version" "$latest_tag"
            else
                log_error "Could not determine latest version"
                exit 1
            fi
            ;;
        rollback)
            log_warn "Manual rollback requested"
            do_rollback
            ;;
        status)
            show_status
            ;;
        *)
            echo "Usage: aviationwx-supervisor [check|update|rollback|status]"
            echo ""
            echo "Commands:"
            echo "  check     Check for updates (default, runs via timer)"
            echo "  update    Force update to latest version"
            echo "  rollback  Rollback to previous version"
            echo "  status    Show current status"
            exit 1
            ;;
    esac
}

main "$@"


