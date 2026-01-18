#!/bin/bash
# AviationWX Bridge - Emergency Recovery Tool
# Version: 2.0

set -euo pipefail

readonly VERSION="2.0"
readonly CONTAINER_NAME="aviationwx-bridge"
readonly IMAGE_NAME="ghcr.io/alexwitherspoon/aviationwx-bridge"
readonly DATA_DIR="/data/aviationwx"

# ============================================================================
# MENU SYSTEM
# ============================================================================

show_menu() {
    clear
    cat <<EOF
╔════════════════════════════════════════════════════════════════╗
║           AviationWX Bridge - Emergency Recovery               ║
║                        Version $VERSION                           ║
╚════════════════════════════════════════════════════════════════╝

Current Status:
  Container: $(docker ps --filter "name=$CONTAINER_NAME" --format '{{.Status}}' 2>/dev/null || echo "Not running")
  Version: $(docker inspect "$CONTAINER_NAME" --format='{{.Config.Image}}' 2>/dev/null | cut -d: -f2 || echo "Unknown")
  Channel: $(jq -r '.update_channel // "latest"' "${DATA_DIR}/global.json" 2>/dev/null || echo "latest")

Recovery Options:

  1. View Status & Logs
  2. Restart Container
  3. Force Update (latest)
  4. Rollback to Last Known Good
  5. Switch Update Channel
  6. Reset Watchdog State
  7. Run Diagnostics
  8. Factory Reset (DANGER)
  
  0. Exit

EOF
    read -rp "Select option: " choice
    echo
    return "$choice" 2>/dev/null || return 99
}

# ============================================================================
# MENU ACTIONS
# ============================================================================

action_status() {
    echo "=== Full Status ==="
    aviationwx status
    echo
    echo "=== Recent Logs (last 20 lines) ==="
    docker logs --tail=20 "$CONTAINER_NAME" 2>&1 || echo "Container not running"
    echo
    read -rp "Press Enter to continue..."
}

action_restart() {
    echo "Restarting container..."
    docker restart "$CONTAINER_NAME" && echo "Success" || echo "Failed"
    sleep 2
}

action_force_update() {
    echo "Forcing update to latest..."
    /usr/local/bin/aviationwx-supervisor.sh force-update
    echo
    read -rp "Press Enter to continue..."
}

action_rollback() {
    echo "=== Rollback to Last Known Good ==="
    
    local last_good
    last_good=$(cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || echo "none")
    
    if [ "$last_good" = "none" ]; then
        echo "Error: No last known good version recorded"
        read -rp "Press Enter to continue..."
        return
    fi
    
    echo "Last Known Good: $last_good"
    read -rp "Rollback to this version? (yes/no): " confirm
    
    if [ "$confirm" = "yes" ]; then
        docker stop "$CONTAINER_NAME" 2>/dev/null || true
        docker rm "$CONTAINER_NAME" 2>/dev/null || true
        
        echo "Pulling $last_good..."
        docker pull "${IMAGE_NAME}:${last_good}"
        
        echo "Starting container..."
        docker run -d \
            --name "$CONTAINER_NAME" \
            --restart no \
            -p 1229:1229 \
            -v "${DATA_DIR}:/data" \
            --tmpfs /dev/shm:size=200m \
            --health-cmd='wget --no-verbose --tries=1 --spider http://localhost:1229/healthz || exit 1' \
            --health-interval=30s \
            --health-timeout=3s \
            --health-start-period=10s \
            --health-retries=3 \
            "${IMAGE_NAME}:${last_good}"
        
        echo "Rollback complete"
    fi
    
    echo
    read -rp "Press Enter to continue..."
}

action_switch_channel() {
    echo "=== Switch Update Channel ==="
    
    local current
    current=$(jq -r '.update_channel // "latest"' "${DATA_DIR}/global.json" 2>/dev/null || echo "latest")
    
    echo "Current channel: $current"
    echo
    echo "1. latest  (stable releases)"
    echo "2. edge    (bleeding edge)"
    echo
    read -rp "Select channel (1-2): " choice
    
    local new_channel
    case "$choice" in
        1) new_channel="latest" ;;
        2) new_channel="edge" ;;
        *) echo "Invalid choice"; return ;;
    esac
    
    if [ ! -f "${DATA_DIR}/global.json" ]; then
        echo '{}' > "${DATA_DIR}/global.json"
    fi
    
    jq --arg channel "$new_channel" '.update_channel = $channel' \
        "${DATA_DIR}/global.json" > "${DATA_DIR}/global.json.tmp"
    mv "${DATA_DIR}/global.json.tmp" "${DATA_DIR}/global.json"
    
    echo "Channel switched to: $new_channel"
    echo
    read -rp "Run update now? (yes/no): " confirm
    
    if [ "$confirm" = "yes" ]; then
        /usr/local/bin/aviationwx-supervisor.sh force-update
    fi
    
    echo
    read -rp "Press Enter to continue..."
}

action_reset_watchdog() {
    echo "=== Reset Watchdog State ==="
    
    if [ -f "${DATA_DIR}/watchdog-state.json" ]; then
        echo "Current state:"
        jq '.' "${DATA_DIR}/watchdog-state.json"
        echo
        read -rp "Reset watchdog counters? (yes/no): " confirm
        
        if [ "$confirm" = "yes" ]; then
            rm -f "${DATA_DIR}/watchdog-state.json"
            echo "Watchdog state reset"
        fi
    else
        echo "No watchdog state found"
    fi
    
    echo
    read -rp "Press Enter to continue..."
}

action_diagnostics() {
    echo "=== System Diagnostics ==="
    echo
    
    echo "Network:"
    ping -c 3 8.8.8.8 && echo "  ✓ Internet reachable" || echo "  ✗ Internet unreachable"
    echo
    
    echo "NTP:"
    timedatectl status | grep "System clock synchronized" || echo "  ✗ Not synchronized"
    echo
    
    echo "Docker:"
    systemctl is-active docker && echo "  ✓ Docker running" || echo "  ✗ Docker not running"
    docker ps &>/dev/null && echo "  ✓ Docker responsive" || echo "  ✗ Docker not responsive"
    echo
    
    echo "Disk Space:"
    df -h "$DATA_DIR" | tail -1
    echo
    
    echo "Memory:"
    free -h
    echo
    
    echo "Temperature (if available):"
    if [ -f /sys/class/thermal/thermal_zone0/temp ]; then
        local temp=$(($(cat /sys/class/thermal/thermal_zone0/temp) / 1000))
        echo "  ${temp}°C"
    else
        echo "  N/A"
    fi
    echo
    
    read -rp "Press Enter to continue..."
}

action_factory_reset() {
    echo "╔════════════════════════════════════════════════════════════════╗"
    echo "║                    ⚠️  FACTORY RESET  ⚠️                       ║"
    echo "║                                                                ║"
    echo "║  This will DELETE:                                             ║"
    echo "║    - All camera configurations                                 ║"
    echo "║    - All settings                                              ║"
    echo "║    - Watchdog state                                            ║"
    echo "║    - Logs and history                                          ║"
    echo "║                                                                ║"
    echo "║  The container will be REMOVED.                                ║"
    echo "║  You will need to reconfigure from scratch.                    ║"
    echo "╚════════════════════════════════════════════════════════════════╝"
    echo
    read -rp "Type 'RESET' to confirm: " confirm
    
    if [ "$confirm" = "RESET" ]; then
        echo
        echo "Stopping container..."
        docker stop "$CONTAINER_NAME" 2>/dev/null || true
        docker rm "$CONTAINER_NAME" 2>/dev/null || true
        
        echo "Backing up to ${DATA_DIR}-backup-$(date +%Y%m%d-%H%M%S)..."
        cp -r "$DATA_DIR" "${DATA_DIR}-backup-$(date +%Y%m%d-%H%M%S)"
        
        echo "Removing configuration..."
        rm -rf "${DATA_DIR:?}/"*
        
        echo
        echo "Factory reset complete."
        echo "To start fresh, run: sudo aviationwx update"
    else
        echo "Factory reset cancelled"
    fi
    
    echo
    read -rp "Press Enter to continue..."
}

# ============================================================================
# MAIN LOOP
# ============================================================================

main() {
    if [ "$EUID" -ne 0 ]; then
        echo "Error: Recovery tool requires root privileges"
        echo "Try: sudo aviationwx recovery"
        exit 1
    fi
    
    while true; do
        show_menu
        choice=$?
        
        case "$choice" in
            1) action_status ;;
            2) action_restart ;;
            3) action_force_update ;;
            4) action_rollback ;;
            5) action_switch_channel ;;
            6) action_reset_watchdog ;;
            7) action_diagnostics ;;
            8) action_factory_reset ;;
            0) echo "Exiting..."; exit 0 ;;
            *) echo "Invalid option"; sleep 1 ;;
        esac
    done
}

main "$@"
