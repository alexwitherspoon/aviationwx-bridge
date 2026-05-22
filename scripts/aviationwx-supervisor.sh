#!/bin/bash
# AviationWX.org Bridge - Unified Supervisor
# Handles updates for both host scripts and container
# Version: 2.2

set -euo pipefail

readonly SCRIPT_VERSION="2.2"
readonly GITHUB_REPO="alexwitherspoon/aviationwx.org-bridge"
readonly IMAGE_NAME="ghcr.io/alexwitherspoon/aviationwx-org-bridge"
readonly CONTAINER_NAME="aviationwx-org-bridge"
readonly DATA_DIR="/data/aviationwx"
readonly CONFIG_FILE="${DATA_DIR}/global.json"
readonly LOG_FILE="${DATA_DIR}/supervisor.log"
readonly SCRIPTS_BASE_URL="https://raw.githubusercontent.com/${GITHUB_REPO}/main/scripts"

# Update configuration
readonly MIN_RELEASE_AGE_HOURS=2
readonly SKIP_PRERELEASE=true
readonly PULL_TIMEOUT=600  # 10 minutes for slow connections (Pi Zero)

# Dry-run mode for testing
readonly DRY_RUN="${AVIATIONWX_DRY_RUN:-false}"

# ============================================================================
# LOGGING
# ============================================================================

log_event() {
    local level="$1"
    local message="$2"
    local timestamp
    timestamp=$(date -Iseconds)
    echo "[$timestamp] [$level] $message" | tee -a "$LOG_FILE"
}

# ============================================================================
# DRY-RUN WRAPPER
# ============================================================================

execute_action() {
    local description="$1"
    shift
    local command="$*"
    
    if [ "$DRY_RUN" = "true" ]; then
        log_event "DRY-RUN" "Would execute: $description"
        log_event "DRY-RUN" "Command: $command"
        return 0
    else
        log_event "ACTION" "$description"
        eval "$command"
    fi
}

# ============================================================================
# SELF-UPDATE LOGIC
# ============================================================================

check_host_scripts_update() {
    log_event "INFO" "Checking for host scripts update (current: v$SCRIPT_VERSION)"

    local release_json
    release_json=$(get_latest_release_json) || return 1

    local required_host_version
    required_host_version=$(echo "$release_json" | jq -r '.body // ""' 2>/dev/null | \
        grep -oE '"min_host_version": "[0-9.]+"' | head -1 | grep -oE '[0-9]+\.[0-9]+')
    
    if [ -z "$required_host_version" ]; then
        log_event "INFO" "No min_host_version in release metadata"
        return 1
    fi
    
    if version_less_than "$SCRIPT_VERSION" "$required_host_version"; then
        log_event "INFO" "Host scripts update required: v$SCRIPT_VERSION → v$required_host_version"
        return 0
    fi
    
    log_event "INFO" "Host scripts up to date: v$SCRIPT_VERSION"
    return 1
}

update_host_scripts() {
    local new_version="$1"
    
    log_event "ACTION" "Updating host scripts to v$new_version"
    
    local temp_dir
    temp_dir=$(mktemp -d)
    
    local scripts=(
        "aviationwx"
        "aviationwx-supervisor.sh"
        "aviationwx-watchdog.sh"
        "aviationwx-capture-restart.sh"
        "aviationwx-recovery.sh"
        "aviationwx-container-start.sh"
    )
    
    # Download all scripts
    for script in "${scripts[@]}"; do
        local url="${SCRIPTS_BASE_URL}/${script}"
        log_event "INFO" "Downloading $script..."
        
        if ! curl -sfL "$url" -o "${temp_dir}/${script}"; then
            log_event "ERROR" "Failed to download $script"
            rm -rf "$temp_dir"
            return 1
        fi
    done
    
    # Validate scripts
    for script in "${scripts[@]}"; do
        if [ ! -s "${temp_dir}/${script}" ]; then
            log_event "ERROR" "Downloaded $script is empty"
            rm -rf "$temp_dir"
            return 1
        fi
        
        if ! head -1 "${temp_dir}/${script}" | grep -q '^#!/bin/bash'; then
            log_event "ERROR" "$script missing shebang"
            rm -rf "$temp_dir"
            return 1
        fi
    done
    
    # Backup current scripts
    local backup_dir="/usr/local/bin/aviationwx-backup-$(date +%Y%m%d-%H%M%S)"
    
    if [ "$DRY_RUN" != "true" ]; then
        mkdir -p "$backup_dir"
        for script in "${scripts[@]}"; do
            if [ -f "/usr/local/bin/${script}" ]; then
                cp "/usr/local/bin/${script}" "$backup_dir/"
            fi
        done
    fi
    
    # Install new scripts
    for script in "${scripts[@]}"; do
        execute_action "Install $script" "cp '${temp_dir}/${script}' '/usr/local/bin/${script}' && chmod +x '/usr/local/bin/${script}'"
    done
    
    # Update host version file
    execute_action "Update host version" "echo '$new_version' > '${DATA_DIR}/host-version.txt'"
    
    # Cleanup
    rm -rf "$temp_dir"
    
    log_event "SUCCESS" "Host scripts updated to v$new_version"
    log_event "INFO" "Backup saved to $backup_dir"

    ensure_capture_restart_timer
    
    return 0
}

# ============================================================================
# SYSTEMD (idempotent; for Pis installed before capture-restart timer existed)
# ============================================================================

# ensure_capture_restart_timer installs/enables aviationwx-capture-restart.timer.
# Unit definitions must stay in sync with install.sh setup_systemd().
ensure_capture_restart_timer() {
    if ! command -v systemctl >/dev/null 2>&1; then
        return 0
    fi
    if [ "$(id -u)" -ne 0 ]; then
        log_event "WARN" "Skipping capture-restart timer setup (not root)"
        return 0
    fi

    local timer_unit="/etc/systemd/system/aviationwx-capture-restart.timer"
    local service_unit="/etc/systemd/system/aviationwx-capture-restart.service"
    local installed_units=false

    if [ ! -f "$timer_unit" ]; then
        installed_units=true
        cat >"$timer_unit" <<'EOF'
[Unit]
Description=AviationWX.org Bridge Capture Restart Check

[Timer]
OnBootSec=5min
OnUnitActiveSec=5min
Persistent=true

[Install]
WantedBy=timers.target
EOF
    fi

    if [ ! -f "$service_unit" ]; then
        installed_units=true
        cat >"$service_unit" <<'EOF'
[Unit]
Description=AviationWX.org Bridge Capture Restart
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-capture-restart.sh
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
    fi

    if [ "$installed_units" = true ]; then
        log_event "ACTION" "Installed capture-restart systemd units"
    fi

    systemctl daemon-reload 2>/dev/null || true

    if ! systemctl is-enabled aviationwx-capture-restart.timer &>/dev/null; then
        log_event "ACTION" "Enabling aviationwx-capture-restart.timer"
        if ! systemctl enable aviationwx-capture-restart.timer 2>/dev/null; then
            log_event "WARN" "Failed to enable aviationwx-capture-restart.timer"
            return 0
        fi
    fi

    if ! systemctl is-active aviationwx-capture-restart.timer &>/dev/null; then
        systemctl start aviationwx-capture-restart.timer 2>/dev/null || true
    fi
}

# ============================================================================
# VERSION DETECTION
# ============================================================================

# is_semver_tag reports whether the value is a concrete release (e.g. 2.9.0).
is_semver_tag() {
    [[ "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.]+)?$ ]]
}

# is_floating_image_tag is true for Docker tags that do not pin a release (latest, edge).
is_floating_image_tag() {
    case "$1" in
        latest|edge|"") return 0 ;;
    esac
    is_semver_tag "$1" && return 1
    return 0
}

# get_container_image_tag returns the Docker image tag (may be "latest", not a semver).
get_container_image_tag() {
    local image
    image=$(docker inspect "$CONTAINER_NAME" --format='{{.Config.Image}}' 2>/dev/null) || return 1
    if [[ ! "$image" == *aviationwx-org-bridge* ]]; then
        return 1
    fi
    if [[ "$image" == *@sha256:* ]]; then
        echo "unknown"
        return 0
    fi
    echo "${image##*:}"
}

# get_running_bridge_version returns the semver compiled into the running container.
get_running_bridge_version() {
    if ! docker ps --filter "name=^${CONTAINER_NAME}$" --format '{{.Names}}' 2>/dev/null | grep -qx "$CONTAINER_NAME"; then
        return 1
    fi
    local ver
    ver=$(docker exec "$CONTAINER_NAME" /usr/local/bin/bridge -version 2>/dev/null | awk '{print $1}') || return 1
    if is_semver_tag "$ver"; then
        echo "$ver"
        return 0
    fi
    return 1
}

# get_last_known_good_label returns last-known-good.txt as stored (may be "latest"; for display).
get_last_known_good_label() {
    cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || echo "none"
}

# get_installed_version_for_update resolves the semver used when deciding whether to upgrade.
# Uses running binary and GitHub target semantics, not floating Docker tags or stale files alone.
get_installed_version_for_update() {
    local running
    running=$(get_running_bridge_version 2>/dev/null) || running=""
    if [ -n "$running" ]; then
        echo "$running"
        return 0
    fi

    local tag
    tag=$(get_container_image_tag 2>/dev/null) || tag=""
    if is_semver_tag "$tag"; then
        echo "$tag"
        return 0
    fi

    local lkg
    lkg=$(get_last_known_good_label)
    if is_semver_tag "$lkg"; then
        echo "$lkg"
        return 0
    fi

    if [ -n "$tag" ] && [ "$tag" != "unknown" ]; then
        echo "$tag"
    else
        echo "unknown"
    fi
}

get_latest_release_json() {
    local cache_file="${DATA_DIR}/release-cache.json"
    local cache_max_age=3600

    if [ -f "$cache_file" ]; then
        local age
        age=$(( $(date +%s) - $(stat -c %Y "$cache_file" 2>/dev/null || echo 0) ))
        if [ $age -lt $cache_max_age ]; then
            cat "$cache_file"
            return 0
        fi
    fi

    local attempt=0
    while [ $attempt -lt 3 ]; do
        local raw
        raw=$(curl -sf --max-time 10 \
            -H "Accept: application/vnd.github.v3+json" \
            "https://api.github.com/repos/${GITHUB_REPO}/releases?per_page=1" 2>/dev/null)
        if [ -n "$raw" ]; then
            local release_json
            release_json=$(echo "$raw" | jq -c '.[0] // empty' 2>/dev/null)
            if [ -n "$release_json" ] && [ "$release_json" != "null" ]; then
                echo "$release_json" > "$cache_file"
                echo "$release_json"
                return 0
            fi
        fi
        attempt=$((attempt + 1))
        sleep 5
    done

    if [ -f "$cache_file" ]; then
        log_event "WARN" "GitHub API unavailable, using stale cache" >&2
        cat "$cache_file"
        return 0
    fi
    log_event "ERROR" "Cannot fetch releases (GitHub API down, no cache)"
    return 1
}

get_update_channel() {
    if [ -f "$CONFIG_FILE" ]; then
        jq -r '.update_channel // "latest"' "$CONFIG_FILE" 2>/dev/null || echo "latest"
    else
        echo "latest"
    fi
}

# ============================================================================
# VERSION LOGIC
# ============================================================================

determine_target_version() {
    local channel="$1"

    log_event "INFO" "Determining target version for channel: $channel" >&2

    local release_json
    release_json=$(get_latest_release_json) || return 1

    if [ "$channel" = "edge" ]; then
        log_event "INFO" "Using edge channel" >&2
        echo "edge"
    else
        local tag_name
        tag_name=$(echo "$release_json" | jq -r '.tag_name // empty' 2>/dev/null)
        if [ -z "$tag_name" ]; then
            log_event "ERROR" "Cannot parse release tag" >&2
            return 1
        fi

        if [ "$SKIP_PRERELEASE" = true ]; then
            local is_prerelease
            is_prerelease=$(echo "$release_json" | jq -r '.prerelease // false' 2>/dev/null)
            if [ "$is_prerelease" = "true" ]; then
                log_event "WARN" "Latest release is pre-release, skipping" >&2
                return 1
            fi
        fi

        if [ "${BOOT_MODE:-}" != "watchdog" ]; then
            local published_at
            published_at=$(echo "$release_json" | jq -r '.published_at // empty' 2>/dev/null)
            if [ -n "$published_at" ]; then
                local published_epoch now_epoch age_hours
                published_epoch=$(date -d "$published_at" +%s 2>/dev/null || echo 0)
                now_epoch=$(date +%s)
                age_hours=$(( (now_epoch - published_epoch) / 3600 ))
                if [ "${AVIATIONWX_SKIP_AGE_CHECK:-false}" != "true" ] && [ $age_hours -lt $MIN_RELEASE_AGE_HOURS ]; then
                    log_event "INFO" "Release too recent: $tag_name (${age_hours}h old). Use 'force-update' or AVIATIONWX_SKIP_AGE_CHECK=true to override" >&2
                    return 1
                fi
            fi
        fi

        echo "${tag_name#v}"
    fi
}

is_version_deprecated() {
    local version="$1"

    local release_json
    release_json=$(get_latest_release_json) || return 1

    local deprecated_versions
    deprecated_versions=$(echo "$release_json" | jq -r '.body // ""' 2>/dev/null | \
        grep -oE '"deprecates": \[[^\]]*\]' | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | tr '\n' ' ')
    
    if echo "$deprecated_versions" | grep -qw "$version"; then
        log_event "WARN" "Version $version is deprecated"
        return 0
    fi
    
    return 1
}

version_less_than() {
    local v1="$1"
    local v2="$2"
    [ "$(printf '%s\n' "$v1" "$v2" | sort -V | head -n1)" = "$v1" ] && [ "$v1" != "$v2" ]
}

# ============================================================================
# IMAGE AVAILABILITY CHECK
# ============================================================================

check_image_exists() {
    local image="$1"
    local version="$2"
    
    # Try to get image manifest (lightweight check)
    if docker manifest inspect "${image}:${version}" &>/dev/null; then
        return 0
    fi
    
    return 1
}

pull_image_with_retry() {
    local image="$1"
    local version="$2"
    local max_attempts="${3:-3}"
    local retry_delay="${4:-15}"
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        log_event "INFO" "Pulling image (attempt $attempt/$max_attempts): ${image}:${version}"
        log_event "INFO" "Timeout: ${PULL_TIMEOUT}s (may take several minutes on slow connections)"
        
        # Capture exit code properly (don't use if directly)
        local exit_code=0
        timeout $PULL_TIMEOUT docker pull "${image}:${version}" || exit_code=$?
        
        if [ $exit_code -eq 0 ]; then
            log_event "SUCCESS" "Image pulled successfully"
            return 0
        elif [ $exit_code -eq 124 ]; then
            log_event "WARN" "Pull timed out after ${PULL_TIMEOUT}s"
        else
            log_event "WARN" "Pull failed with exit code $exit_code"
        fi
        
        if [ $attempt -lt $max_attempts ]; then
            log_event "INFO" "Retrying in ${retry_delay}s..."
            sleep $retry_delay
            # Increase delay for next attempt (exponential backoff, max 60s)
            retry_delay=$((retry_delay * 2))
            if [ $retry_delay -gt 60 ]; then
                retry_delay=60
            fi
        fi
        
        attempt=$((attempt + 1))
    done
    
    log_event "ERROR" "Failed to pull image after $max_attempts attempts"
    return 1
}

check_release_workflow_status() {
    local version="$1"
    
    # Only check for version tags (not 'edge' or 'latest')
    # Version comes WITHOUT 'v' prefix (e.g., "2.2.3"), so check for semver pattern
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        return 1
    fi
    
    # Try to check GitHub Actions status (requires gh CLI or API)
    # Fallback: check if release exists (API uses v-prefixed tags)
    local release_exists
    release_exists=$(curl -sf --max-time 5 \
        "https://api.github.com/repos/${GITHUB_REPO}/releases/tags/v${version}" 2>/dev/null)
    
    if [ -n "$release_exists" ]; then
        local published_at
        published_at=$(echo "$release_exists" | jq -r '.published_at // empty' 2>/dev/null)
        
        if [ -n "$published_at" ]; then
            local published_epoch
            published_epoch=$(date -d "$published_at" +%s 2>/dev/null || echo 0)
            local now_epoch
            now_epoch=$(date +%s)
            local age_seconds=$(( now_epoch - published_epoch ))
            
            # If release was published in last 15 minutes, build might still be in progress
            if [ $age_seconds -lt 900 ]; then
                log_event "INFO" "Release v${version} published ${age_seconds}s ago, Docker build may be in progress" >&2
                return 0
            fi
        fi
    fi
    
    return 1
}

wait_for_image_with_progress() {
    local image="$1"
    local version="$2"
    local max_wait="$3"
    local elapsed=0
    local check_interval=30
    
    log_event "INFO" "Waiting for Docker image to be published..." >&2
    log_event "INFO" "Image: ${image}:${version}" >&2
    log_event "INFO" "Max wait time: ${max_wait}s" >&2
    
    while [ $elapsed -lt $max_wait ]; do
        # Check if image exists
        if check_image_exists "$image" "$version"; then
            log_event "SUCCESS" "Docker image is now available!" >&2
            return 0
        fi
        
        # Show progress
        local remaining=$(( max_wait - elapsed ))
        log_event "INFO" "Still waiting... (${remaining}s remaining)" >&2
        
        sleep $check_interval
        elapsed=$((elapsed + check_interval))
    done
    
    log_event "ERROR" "Timeout waiting for Docker image" >&2
    return 1
}

# ============================================================================
# UPDATE LOGIC
# ============================================================================

pull_and_validate_update() {
    local target_version="$1"
    
    # Check resources before pulling
    if ! check_resources_available; then
        log_event "ERROR" "Insufficient resources for update"
        return 1
    fi
    
    # Check if image exists first
    log_event "INFO" "Checking if image exists: ${IMAGE_NAME}:${target_version}"
    if ! check_image_exists "$IMAGE_NAME" "$target_version"; then
        log_event "WARN" "Docker image not found: ${IMAGE_NAME}:${target_version}"
        
        # Check if release workflow might be in progress
        if check_release_workflow_status "$target_version"; then
            log_event "INFO" "Recent release detected - Docker build may be in progress"
            log_event "INFO" "This typically takes 8-10 minutes for multi-arch builds"
            
            # Wait up to 15 minutes for image to become available
            if wait_for_image_with_progress "$IMAGE_NAME" "$target_version" 900; then
                log_event "SUCCESS" "Image is ready, proceeding with update"
            else
                log_event "ERROR" "Timed out waiting for Docker image to be published"
                log_event "INFO" "You can check build status at:"
                log_event "INFO" "  https://github.com/${GITHUB_REPO}/actions"
                log_event "INFO" "Or retry the update later: aviationwx force-update"
                return 1
            fi
        else
            log_event "ERROR" "Image does not exist and no recent release found"
            log_event "INFO" "Possible causes:"
            log_event "INFO" "  1. Release workflow failed (check GitHub Actions)"
            log_event "INFO" "  2. Version tag doesn't exist"
            log_event "INFO" "  3. Network connectivity issue"
            log_event "INFO" "Check: https://github.com/${GITHUB_REPO}/actions"
            return 1
        fi
    fi
    
    # Pull image with retry logic
    log_event "ACTION" "Pulling ${IMAGE_NAME}:${target_version}"
    if [ "$DRY_RUN" = "true" ]; then
        log_event "DRY-RUN" "Would pull: ${IMAGE_NAME}:${target_version}"
    else
        if ! pull_image_with_retry "$IMAGE_NAME" "$target_version" 3 10; then
            log_event "ERROR" "Failed to pull image after retries"
            return 1
        fi
    fi
    
    # Stop old container
    execute_action "Stop container" "docker stop $CONTAINER_NAME 2>/dev/null || true"
    execute_action "Remove container" "docker rm $CONTAINER_NAME 2>/dev/null || true"
    
    # Start new container
    log_event "ACTION" "Starting container with $target_version"
    if ! start_container "$target_version"; then
        log_event "ERROR" "Container failed to start"
        return 1
    fi
    
    # Wait for health check
    if wait_for_healthy 60; then
        log_event "SUCCESS" "Container healthy with $target_version"
        return 0
    else
        log_event "ERROR" "Container failed health check"
        
        # Save logs for debugging
        docker logs "$CONTAINER_NAME" > "${DATA_DIR}/failed-start-$(date +%Y%m%d-%H%M%S).log" 2>&1
        
        return 1
    fi
}

# start_container recreates the bridge using aviationwx-container-start.sh so updates
# get the same RAM/CPU/tmpfs limits as install and boot (not a minimal inline docker run).
start_container() {
    local version="$1"
    local start_script="/usr/local/bin/aviationwx-container-start.sh"

    if [ "$DRY_RUN" = "true" ]; then
        log_event "DRY-RUN" "Would start container via $start_script version: $version"
        return 0
    fi

    if [ ! -x "$start_script" ]; then
        log_event "ERROR" "$start_script not found or not executable"
        return 1
    fi

    if ! "$start_script" "$version"; then
        log_event "ERROR" "container-start failed for version $version"
        return 1
    fi

    return 0
}

wait_for_healthy() {
    local timeout="$1"
    local elapsed=0
    
    if [ "$DRY_RUN" = "true" ]; then
        return 0
    fi
    
    while [ $elapsed -lt $timeout ]; do
        # Check if container exited
        if ! docker ps --filter "name=$CONTAINER_NAME" --format '{{.Names}}' | grep -q "$CONTAINER_NAME"; then
            log_event "ERROR" "Container exited during startup"
            
            local exit_code
            exit_code=$(docker inspect "$CONTAINER_NAME" --format='{{.State.ExitCode}}' 2>/dev/null || echo "unknown")
            log_event "ERROR" "Exit code: $exit_code"
            
            return 1
        fi
        
        # Check health status
        local health
        health=$(docker inspect "$CONTAINER_NAME" --format='{{.State.Health.Status}}' 2>/dev/null || echo "none")
        
        if [ "$health" = "healthy" ]; then
            return 0
        fi
        
        sleep 5
        elapsed=$((elapsed + 5))
    done
    
    log_event "ERROR" "Health check timeout after ${timeout}s"
    return 1
}

rollback_to_last_known_good() {
    local last_good
    last_good=$(cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || echo "latest")
    
    log_event "ACTION" "Rolling back to last known good: $last_good"
    
    if [ "$DRY_RUN" = "true" ]; then
        return 0
    fi
    
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
    
    # Pull last known good with retry (might be in cache, but network could be flaky)
    if ! pull_image_with_retry "$IMAGE_NAME" "$last_good" 3 15; then
        log_event "ERROR" "Failed to pull rollback image, trying to start from cache"
    fi
    
    if start_container "$last_good"; then
        log_event "INFO" "Rollback complete"
    else
        log_event "ERROR" "Rollback failed - container did not start"
        return 1
    fi
}

check_resources_available() {
    # Check memory
    local free_mem_mb
    free_mem_mb=$(free -m 2>/dev/null | awk '/^Mem:/ {print $7}' || echo "1000")
    
    if [ "$free_mem_mb" -lt 100 ]; then
        log_event "ERROR" "Insufficient memory: ${free_mem_mb}MB free"
        return 1
    fi
    
    # Check disk
    local free_disk_mb
    free_disk_mb=$(df /var/lib/docker 2>/dev/null | tail -1 | awk '{print int($4/1024)}' || echo "1000")
    
    if [ "$free_disk_mb" -lt 500 ]; then
        log_event "WARN" "Low disk space: ${free_disk_mb}MB, attempting cleanup"
        
        # Cleanup old images (keep images from last week)
        docker image prune -af --filter "until=168h" 2>&1 | head -5
        
        # Cleanup stopped containers
        docker container prune -f 2>&1 | head -5
        
        # Check again
        free_disk_mb=$(df /var/lib/docker 2>/dev/null | tail -1 | awk '{print int($4/1024)}')
        
        if [ "$free_disk_mb" -lt 300 ]; then
            log_event "ERROR" "Still insufficient disk: ${free_disk_mb}MB"
            return 1
        fi
    fi
    
    return 0
}

# ============================================================================
# UTILITY FUNCTIONS
# ============================================================================

wait_for_network() {
    local timeout="$1"
    local elapsed=0
    
    while [ $elapsed -lt $timeout ]; do
        if ping -c 1 -W 2 8.8.8.8 &>/dev/null; then
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    
    return 1
}

# ============================================================================
# BOOT UPDATE
# ============================================================================

boot_update() {
    log_event "INFO" "=== Boot-time update check ==="
    
    # Step 1: Check if host scripts need update
    if check_host_scripts_update; then
        local release_json tag_name required_version
        release_json=$(get_latest_release_json)
        tag_name=$(echo "$release_json" | jq -r '.tag_name // empty' 2>/dev/null)
        required_version="${tag_name#v}"
        
        if update_host_scripts "$required_version"; then
            log_event "INFO" "Host scripts updated, re-running supervisor"
            # Re-exec ourselves with new version
            exec /usr/local/bin/aviationwx-supervisor.sh boot-update
        fi
    fi

    ensure_capture_restart_timer
    
    # Step 2: Check if last boot was watchdog-triggered
    if [ -f "${DATA_DIR}/last-reboot-reason.json" ]; then
        local reboot_reason
        reboot_reason=$(jq -r '.reboot_reason // "unknown"' "${DATA_DIR}/last-reboot-reason.json" 2>/dev/null)
        if [ "$reboot_reason" = "watchdog" ]; then
            log_event "INFO" "Watchdog triggered last boot (potential crash loop)"
            export BOOT_MODE="watchdog"
        fi
    fi
    
    # Step 3: Wait for network
    if ! wait_for_network 30; then
        log_event "WARN" "Network timeout, starting with current version"
        return 0
    fi
    
    # Step 4: Determine target version
    local channel
    channel=$(get_update_channel)
    
    local target_version
    target_version=$(determine_target_version "$channel") || {
        log_event "WARN" "Could not determine target version, using current"
        return 0
    }
    
    # Step 5: Upgrade decision (resolved install version vs GitHub target; not Docker :latest alone)
    local installed_version image_tag lkg_label
    installed_version=$(get_installed_version_for_update)
    image_tag=$(get_container_image_tag 2>/dev/null || echo "none")
    lkg_label=$(get_last_known_good_label)

    if is_floating_image_tag "$image_tag"; then
        log_event "INFO" "Upgrade check: installed=$installed_version target=$target_version (GitHub) local_tag=$image_tag last_known_good=$lkg_label"
    else
        log_event "INFO" "Upgrade check: installed=$installed_version target=$target_version local_tag=$image_tag"
    fi

    if [ "$target_version" = "$installed_version" ]; then
        log_event "INFO" "Already on target version"
        return 0
    fi
    
    # Step 6: Check if deprecated
    if is_version_deprecated "$target_version"; then
        log_event "WARN" "Target version is deprecated, skipping"
        return 0
    fi
    
    # Step 7: Pull and validate
    if pull_and_validate_update "$target_version"; then
        # Success - record as last known good
        echo "$target_version" > "${DATA_DIR}/last-known-good.txt"
        log_event "SUCCESS" "Updated: $installed_version → $target_version"
    else
        # Failed - rollback
        log_event "ERROR" "Update failed, rolling back"
        rollback_to_last_known_good
    fi
}

# ============================================================================
# COMMAND DISPATCHER
# ============================================================================

usage() {
    cat <<EOF
AviationWX Supervisor - Update Management

Usage: aviationwx-supervisor.sh COMMAND

Commands:
  boot-update          Run update check at boot time
  daily-update         Run daily update check (same as boot-update)
  force-update         Force update (ignores cache, age checks)
  print-target-version Print channel target version from GitHub (stdout only)
  status               Show current status

Environment:
  AVIATIONWX_DRY_RUN=true    Run in dry-run mode (log only, no actions)

EOF
}

main() {
    mkdir -p "$DATA_DIR"
    
    # Check for manual update trigger from web UI
    if [ -f "${DATA_DIR}/trigger-update" ]; then
        log_event "INFO" "Manual update triggered via web UI"
        
        # Check if it's a force update request
        trigger_content=$(cat "${DATA_DIR}/trigger-update" 2>/dev/null || echo "")
        if [ "$trigger_content" = "force" ]; then
            log_event "INFO" "Force update requested - skipping age check"
            export AVIATIONWX_SKIP_AGE_CHECK=true
        fi
        
        rm -f "${DATA_DIR}/trigger-update"
        rm -f "${DATA_DIR}/release-cache.json" # Force fresh check
        boot_update
        exit 0
    fi
    
    case "${1:-}" in
        boot-update|daily-update)
            boot_update
            ;;
        force-update)
            # Clear cache and skip age check to force immediate update
            rm -f "${DATA_DIR}/release-cache.json"
            export AVIATIONWX_SKIP_AGE_CHECK=true
            boot_update
            ;;
        print-target-version)
            local channel
            channel=$(get_update_channel)
            determine_target_version "$channel"
            ;;
        status)
            echo "Supervisor version: v$SCRIPT_VERSION"
            echo "Container image tag: $(get_container_image_tag 2>/dev/null || echo 'none')"
            echo "Last known good (file): $(get_last_known_good_label)"
            echo "Update channel: $(get_update_channel)"
            if docker ps --filter "name=^${CONTAINER_NAME}$" --format '{{.Names}}' 2>/dev/null | grep -qx "$CONTAINER_NAME"; then
                echo "Running bridge version: $(get_running_bridge_version 2>/dev/null || echo 'unknown')"
            fi
            ;;
        *)
            usage
            exit 1
            ;;
    esac
}

main "$@"
