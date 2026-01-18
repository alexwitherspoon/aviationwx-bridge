#!/bin/bash
# AviationWX Bridge - Host-Level Watchdog
# Monitors host health (NTP, network, Docker) and triggers recovery
# Version: 2.0

set -euo pipefail

readonly WATCHDOG_VERSION="2.0"
readonly STATE_FILE="/data/aviationwx/watchdog-state.json"
readonly LOG_FILE="/data/aviationwx/watchdog.log"
readonly DATA_DIR="/data/aviationwx"

# Progressive escalation over 30 minutes
readonly CHECK_INTERVAL=60
readonly NTP_RESTART_AT=15       # 15 minutes
readonly NETWORK_RESTART_AT=10   # 10 minutes  
readonly DOCKER_RESTART_AT=15    # 15 minutes
readonly REBOOT_AT=25            # 25 minutes (if 2+ systems critical)
readonly REBOOT_COOLDOWN_HOURS=24

# Dry-run mode
readonly DRY_RUN="${AVIATIONWX_DRY_RUN:-false}"

# State variables
declare -g ntp_failures=0
declare -g network_failures=0
declare -g docker_failures=0
declare -g last_reboot="never"

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
# STATE MANAGEMENT
# ============================================================================

load_state() {
    if [ ! -f "$STATE_FILE" ]; then
        return 0
    fi
    
    # Try to load state, fallback to defaults if corrupted
    if ! jq empty "$STATE_FILE" 2>/dev/null; then
        log_event "WARN" "Corrupted state file, resetting to defaults"
        return 0
    fi
    
    ntp_failures=$(jq -r '.ntp_failures // 0' "$STATE_FILE")
    network_failures=$(jq -r '.network_failures // 0' "$STATE_FILE")
    docker_failures=$(jq -r '.docker_failures // 0' "$STATE_FILE")
    last_reboot=$(jq -r '.last_reboot // "never"' "$STATE_FILE")
}

save_state() {
    local temp_file="${STATE_FILE}.tmp.$$"
    
    jq -n \
        --arg ntp "$ntp_failures" \
        --arg net "$network_failures" \
        --arg docker "$docker_failures" \
        --arg reboot "$last_reboot" \
        --arg timestamp "$(date -Iseconds)" \
        '{
            ntp_failures: ($ntp | tonumber),
            network_failures: ($net | tonumber),
            docker_failures: ($docker | tonumber),
            last_reboot: $reboot,
            last_check: $timestamp
        }' > "$temp_file"
    
    # Validate JSON before moving
    if jq empty "$temp_file" 2>/dev/null; then
        mv "$temp_file" "$STATE_FILE"
        
        # Keep backup
        cp "$STATE_FILE" "${STATE_FILE}.backup"
    else
        log_event "ERROR" "Failed to write valid state file"
        rm -f "$temp_file"
    fi
}

# ============================================================================
# HOST HEALTH CHECKS
# ============================================================================

check_ntp() {
    # Check if system clock is synchronized
    if timedatectl status 2>/dev/null | grep -q "System clock synchronized: yes"; then
        if [ $ntp_failures -gt 0 ]; then
            log_event "INFO" "NTP recovered after $ntp_failures minutes"
        fi
        ntp_failures=0
        return 0
    fi
    
    # NTP not synchronized
    ntp_failures=$((ntp_failures + 1))
    
    if [ $ntp_failures -ge $NTP_RESTART_AT ]; then
        log_event "ERROR" "NTP not synced for ${ntp_failures} min, restarting service"
        
        # Try different NTP services
        if systemctl is-active --quiet systemd-timesyncd; then
            execute_action "Restart systemd-timesyncd" "systemctl restart systemd-timesyncd"
        elif systemctl is-active --quiet chronyd; then
            execute_action "Restart chronyd" "systemctl restart chronyd"
        elif systemctl is-active --quiet ntpd; then
            execute_action "Restart ntpd" "systemctl restart ntpd"
        else
            log_event "WARN" "No NTP service found"
        fi
        
        # Give it time to sync
        sleep 5
    else
        log_event "WARN" "NTP not synchronized (${ntp_failures} min)"
    fi
}

check_network() {
    # Test multiple targets to avoid false positives
    local targets=("8.8.8.8" "1.1.1.1" "8.8.4.4")
    
    for target in "${targets[@]}"; do
        if ping -c 1 -W 2 "$target" &>/dev/null; then
            if [ $network_failures -gt 0 ]; then
                log_event "INFO" "Network recovered after $network_failures minutes"
            fi
            network_failures=0
            return 0
        fi
    done
    
    # All targets failed
    network_failures=$((network_failures + 1))
    
    if [ $network_failures -ge $NETWORK_RESTART_AT ]; then
        log_event "ERROR" "Network down for ${network_failures} min, restarting interface"
        
        # Detect default interface
        local iface
        iface=$(ip route 2>/dev/null | grep default | awk '{print $5}' | head -1)
        
        if [ -n "$iface" ]; then
            execute_action "Restart network interface $iface" "ip link set '$iface' down && sleep 2 && ip link set '$iface' up && sleep 5"
        else
            log_event "ERROR" "Cannot determine network interface"
        fi
    else
        log_event "WARN" "Network unreachable (${network_failures} min)"
    fi
}

check_docker() {
    # Check if Docker daemon is running and responsive
    if systemctl is-active --quiet docker; then
        if timeout 5 docker ps &>/dev/null; then
            if [ $docker_failures -gt 0 ]; then
                log_event "INFO" "Docker recovered after $docker_failures minutes"
            fi
            docker_failures=0
            return 0
        fi
    fi
    
    # Docker is unhealthy
    docker_failures=$((docker_failures + 1))
    
    if [ $docker_failures -ge $DOCKER_RESTART_AT ]; then
        log_event "ERROR" "Docker unhealthy for ${docker_failures} min, restarting daemon"
        execute_action "Restart Docker" "systemctl restart docker && sleep 10"
    else
        log_event "WARN" "Docker unhealthy (${docker_failures} min)"
    fi
}

check_temperature() {
    # Pi-specific check
    if [ ! -f /sys/class/thermal/thermal_zone0/temp ]; then
        return 0
    fi
    
    local temp_millic
    temp_millic=$(cat /sys/class/thermal/thermal_zone0/temp 2>/dev/null || echo 0)
    local temp_c=$((temp_millic / 1000))
    
    if [ $temp_c -ge 82 ]; then
        log_event "CRITICAL" "Temperature critical: ${temp_c}°C (throttling active)"
    elif [ $temp_c -ge 75 ]; then
        log_event "WARN" "Temperature elevated: ${temp_c}°C (approaching throttle)"
    fi
}

check_disk() {
    local usage
    usage=$(df "$DATA_DIR" 2>/dev/null | tail -1 | awk '{print $5}' | sed 's/%//' || echo "0")
    
    if [ "$usage" -ge 95 ]; then
        log_event "CRITICAL" "Disk usage critical: ${usage}%"
        cleanup_disk_space
    elif [ "$usage" -ge 90 ]; then
        log_event "WARN" "Disk usage high: ${usage}%"
    fi
}

cleanup_disk_space() {
    log_event "ACTION" "Attempting disk cleanup"
    
    # Trim log files (keep last 100 lines)
    for log_file in "$DATA_DIR"/*.log; do
        if [ -f "$log_file" ]; then
            execute_action "Trim $log_file" "tail -n 100 '$log_file' > '${log_file}.tmp' && mv '${log_file}.tmp' '$log_file'"
        fi
    done
    
    # Remove old config backups (keep last 5)
    find "$DATA_DIR" -name "*.bak" -type f 2>/dev/null | sort -r | tail -n +6 | while read -r f; do
        execute_action "Remove old backup $f" "rm -f '$f'"
    done
    
    # Clean old failure logs (keep last 3)
    find "$DATA_DIR" -name "failed-*.log" -type f 2>/dev/null | sort -r | tail -n +4 | while read -r f; do
        execute_action "Remove old failure log $f" "rm -f '$f'"
    done
}

# ============================================================================
# REBOOT DECISION
# ============================================================================

should_reboot() {
    local critical_count=0
    
    # Count critical systems
    [ $ntp_failures -ge $REBOOT_AT ] && critical_count=$((critical_count + 1))
    [ $network_failures -ge $REBOOT_AT ] && critical_count=$((critical_count + 1))
    [ $docker_failures -ge $REBOOT_AT ] && critical_count=$((critical_count + 1))
    
    if [ $critical_count -lt 2 ]; then
        return 1  # Need 2+ critical systems
    fi
    
    # Check reboot cooldown
    if [ "$last_reboot" != "never" ]; then
        local last_reboot_epoch
        last_reboot_epoch=$(date -d "$last_reboot" +%s 2>/dev/null || echo 0)
        local now_epoch
        now_epoch=$(date +%s)
        local hours_since_reboot=$(( (now_epoch - last_reboot_epoch) / 3600 ))
        
        if [ $hours_since_reboot -lt $REBOOT_COOLDOWN_HOURS ]; then
            log_event "CRITICAL" "Multiple systems failing, but rebooted ${hours_since_reboot}h ago (cooldown: ${REBOOT_COOLDOWN_HOURS}h)"
            return 1
        fi
    fi
    
    return 0
}

trigger_reboot() {
    log_event "CRITICAL" "REBOOTING HOST (ntp:$ntp_failures net:$network_failures docker:$docker_failures)"
    
    # Record reboot reason for boot-update script
    jq -n \
        --arg reason "watchdog" \
        --arg ntp "$ntp_failures" \
        --arg net "$network_failures" \
        --arg docker "$docker_failures" \
        --arg timestamp "$(date -Iseconds)" \
        '{
            reboot_reason: $reason,
            ntp_failures: ($ntp | tonumber),
            network_failures: ($net | tonumber),
            docker_failures: ($docker | tonumber),
            timestamp: $timestamp
        }' > "${DATA_DIR}/last-reboot-reason.json"
    
    last_reboot=$(date -Iseconds)
    save_state
    
    if [ "$DRY_RUN" = "true" ]; then
        log_event "DRY-RUN" "Would reboot host now"
        return 0
    fi
    
    sync
    sleep 2
    /sbin/reboot
}

# ============================================================================
# MAIN EXECUTION
# ============================================================================

main() {
    # Ensure data directory exists
    mkdir -p "$DATA_DIR"
    
    # Load previous state
    load_state
    
    log_event "INFO" "Watchdog check (ntp:$ntp_failures net:$network_failures docker:$docker_failures)"
    
    # Run all health checks
    check_ntp
    check_network
    check_docker
    check_temperature
    check_disk
    
    # Evaluate reboot decision
    if should_reboot; then
        trigger_reboot
    fi
    
    # Save state for next run
    save_state
    
    log_event "INFO" "Watchdog check complete"
}

# Run main function
main "$@"
