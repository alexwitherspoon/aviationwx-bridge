#!/bin/bash
#
# AviationWX.org Bridge - Installation Script
# https://github.com/alexwitherspoon/aviationwx.org-bridge
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx.org-bridge/main/scripts/install.sh | sudo bash
#
# This script:
#   1. Installs Docker (if not present)
#   2. Installs the update supervisor
#   3. Pulls and starts the AviationWX.org Bridge container
#   4. Configures automatic updates
#

set -euo pipefail

# Configuration
REPO="alexwitherspoon/aviationwx.org-bridge"
IMAGE_NAME="ghcr.io/alexwitherspoon/aviationwx-org-bridge"
CONTAINER_NAME="aviationwx-org-bridge"
DATA_DIR="/data/aviationwx"
WEB_PORT="1229"
ENV_FILE="${DATA_DIR}/environment"

# Default tmpfs size (can be overridden in environment file)
# Sizing guide:
#   1-2 cameras @ 1080p: 100m
#   3-4 cameras @ 1080p: 200m (default)
#   4 cameras @ 4K: 300m or higher
DEFAULT_TMPFS_SIZE="200m"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[OK]${NC} $*"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# Check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

# Detect OS
detect_os() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS=$ID
        VERSION=$VERSION_ID
    else
        log_error "Cannot detect OS. /etc/os-release not found."
        exit 1
    fi
    log_info "Detected OS: $OS $VERSION"
}

# Install Docker if not present
install_docker() {
    if command -v docker &> /dev/null; then
        log_success "Docker is already installed"
        docker --version
        return 0
    fi

    log_info "Installing Docker..."
    
    case $OS in
        raspbian|debian|ubuntu)
            # Use official Docker convenience script
            curl -fsSL https://get.docker.com | sh
            ;;
        *)
            log_error "Unsupported OS: $OS"
            log_info "Please install Docker manually and re-run this script."
            exit 1
            ;;
    esac

    # Enable Docker service
    systemctl enable docker
    systemctl start docker

    log_success "Docker installed successfully"
}

# Install jq if not present (JSON parsing for config, release metadata, daemon merge)
install_jq() {
    if command -v jq &> /dev/null; then
        log_success "jq is already installed"
        return 0
    fi
    log_info "Installing jq..."
    case $OS in
        raspbian|debian|ubuntu)
            apt-get update -qq && apt-get install -y -qq jq
            ;;
        *)
            log_error "Please install jq manually: https://stedolan.github.io/jq/download/"
            exit 1
            ;;
    esac
    log_success "jq installed"
}

# Configure RAM-based Docker logging to protect SD-card-backed single-board
# computers from log write wear. Detection currently keys on Raspberry Pi, the
# most common SD-card board; other SBCs use default logging unless configured
# manually (or run from eMMC/SSD where wear is less of a concern).
configure_docker_logging() {
    log_info "Configuring Docker logging for SD card protection..."
    
    # Auto-detect Raspberry Pi (most common SD-card-backed SBC)
    if [[ $OS == "raspbian" ]] || grep -qi "raspberry" /proc/cpuinfo 2>/dev/null; then
        log_info "Raspberry Pi detected - configuring journald with volatile storage"
        
        # Configure journald for volatile (RAM) storage
        if [[ ! -f /etc/systemd/journald.conf.d/aviationwx.conf ]]; then
            mkdir -p /etc/systemd/journald.conf.d
            cat > /etc/systemd/journald.conf.d/aviationwx.conf << 'EOF'
# AviationWX.org Bridge - Journald volatile storage
# Stores logs in RAM to prevent SD card wear
[Journal]
Storage=volatile
RuntimeMaxUse=20M
EOF
            systemctl restart systemd-journald
            log_success "Configured journald for volatile storage (20MB RAM)"
        else
            log_info "Journald already configured"
        fi
        
        # Configure Docker to use journald (merge with existing config)
        mkdir -p /etc/docker
        
        if [[ ! -f /etc/docker/daemon.json ]]; then
            # Create new config
            cat > /etc/docker/daemon.json << 'EOF'
{
  "log-driver": "journald",
  "log-opts": {
    "tag": "{{.Name}}"
  }
}
EOF
            log_success "Created Docker daemon.json with journald logging"
        else
            log_info "Merging journald config into existing daemon.json"
            cp /etc/docker/daemon.json /etc/docker/daemon.json.backup
            if jq '. + {"log-driver": "journald", "log-opts": {"tag": "{{.Name}}"}}' \
                /etc/docker/daemon.json > /etc/docker/daemon.json.tmp 2>/dev/null && \
                jq empty /etc/docker/daemon.json.tmp 2>/dev/null; then
                mv /etc/docker/daemon.json.tmp /etc/docker/daemon.json
                log_success "Merged journald config into daemon.json (backup saved)"
            else
                log_error "Failed to merge config - keeping original"
                rm -f /etc/docker/daemon.json.tmp
                return 1
            fi
        fi
        
        # Restart Docker to apply changes
        log_info "Restarting Docker to apply logging configuration..."
        systemctl restart docker
        sleep 2
        
        # Verify Docker is running
        if systemctl is-active --quiet docker; then
            log_success "Docker restarted successfully with journald logging"
        else
            log_error "Docker failed to restart - restoring backup"
            if [[ -f /etc/docker/daemon.json.backup ]]; then
                mv /etc/docker/daemon.json.backup /etc/docker/daemon.json
                systemctl restart docker
            fi
            return 1
        fi
    else
        log_info "No Raspberry Pi detected - using default Docker logging (set up RAM-based logging manually on other SD-card-backed boards)"
    fi
}

# Create data directory and environment file
setup_data_dir() {
    log_info "Setting up data directory: ${DATA_DIR}"
    mkdir -p "${DATA_DIR}"
    chmod 755 "${DATA_DIR}"
    
    # Set ownership to uid:gid 1000:1000 (matches container's bridge user)
    chown -R 1000:1000 "${DATA_DIR}"

    # Create environment file if it doesn't exist
    if [[ ! -f "${ENV_FILE}" ]]; then
        cat > "${ENV_FILE}" << 'EOF'
# AviationWX.org Bridge Environment Configuration
# Uncomment a setting, then apply it:
#   sudo systemctl restart aviationwx-container.service
#
# Image queue (tmpfs in RAM). By default the supervised install sizes this
# automatically from available RAM. Set a Docker size token (e.g. 200m, 1g)
# to override; an invalid value is ignored and the automatic size is used.
#
# Sizing guidance (RAM is the cap - keep tmpfs plus the container memory
# limit comfortably under total RAM):
#   1-2 cameras @ 1080p: 100m is sufficient
#   3-4 cameras @ 1080p: 200m
#   1-2 cameras @ 4K:    200m
#   3-4 cameras @ 4K:    300m or higher
#
# AVIATIONWX_TMPFS_SIZE=200m
EOF
        log_info "Created environment file: ${ENV_FILE}"
        chown 1000:1000 "${ENV_FILE}"
    fi

    log_success "Data directory ready"
}

# Install host scripts
install_host_scripts() {
    log_info "Installing host management scripts..."

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
        log_info "Downloading ${script}..."
        curl -fsSL \
            "https://raw.githubusercontent.com/${REPO}/main/scripts/${script}" \
            -o "/usr/local/bin/${script}"
        chmod +x "/usr/local/bin/${script}"
    done

    # Host version tracks supervisor release (see aviationwx-supervisor.sh SCRIPT_VERSION)
    echo "2.2" > "${DATA_DIR}/host-version.txt"

    log_success "Host scripts installed"
}

# Setup systemd services
setup_systemd() {
    log_info "Setting up systemd services..."

    # Boot-time update service
    cat > /etc/systemd/system/aviationwx-boot-update.service << 'EOF'
[Unit]
Description=AviationWX.org Bridge Boot-Time Update
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target
Before=aviationwx-container.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-supervisor.sh boot-update
TimeoutStartSec=300
StandardOutput=journal
StandardError=journal
RemainAfterExit=yes

[Install]
WantedBy=multi-user.target
EOF

    # Container start service
    cat > /etc/systemd/system/aviationwx-container.service << 'EOF'
[Unit]
Description=AviationWX.org Bridge Container
After=aviationwx-boot-update.service docker.service
Requires=docker.service
BindsTo=aviationwx-boot-update.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-container-start.sh
RemainAfterExit=yes
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    # Daily update timer
    cat > /etc/systemd/system/aviationwx-daily-update.timer << 'EOF'
[Unit]
Description=AviationWX.org Bridge Daily Update Check

[Timer]
OnCalendar=daily
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    cat > /etc/systemd/system/aviationwx-daily-update.service << 'EOF'
[Unit]
Description=AviationWX.org Bridge Daily Update
After=docker.service
Requires=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-supervisor.sh daily-update
TimeoutStartSec=300
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    # Watchdog timer
    cat > /etc/systemd/system/aviationwx-watchdog.timer << 'EOF'
[Unit]
Description=AviationWX.org Bridge Watchdog Timer

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    cat > /etc/systemd/system/aviationwx-watchdog.service << 'EOF'
[Unit]
Description=AviationWX.org Bridge Watchdog
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-watchdog.sh
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    # Capture-restart timer (/readyz 503 → container restart when capture is stale)
    cat > /etc/systemd/system/aviationwx-capture-restart.timer << 'EOF'
[Unit]
Description=AviationWX.org Bridge Capture Restart Check

[Timer]
OnBootSec=5min
OnUnitActiveSec=5min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    cat > /etc/systemd/system/aviationwx-capture-restart.service << 'EOF'
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

    # Reload and enable
    systemctl daemon-reload
    systemctl enable aviationwx-boot-update.service
    systemctl enable aviationwx-container.service
    systemctl enable aviationwx-daily-update.timer
    systemctl enable aviationwx-watchdog.timer
    systemctl enable aviationwx-capture-restart.timer

    log_success "Systemd services configured and enabled"
}

# Load environment overrides if present
load_environment() {
    if [[ -f "${ENV_FILE}" ]]; then
        log_info "Loading environment from ${ENV_FILE}"
        # shellcheck source=/dev/null
        source "${ENV_FILE}"
    fi
}

# Get tmpfs size from environment or use default
get_tmpfs_size() {
    echo "${AVIATIONWX_TMPFS_SIZE:-${DEFAULT_TMPFS_SIZE}}"
}

# Migration: stop old container from pre-rename installs (aviationwx-bridge → aviationwx-org-bridge)
migrate_from_old_container() {
    local old_container="aviationwx-bridge"
    local old_container_alt="aviationwx.org-bridge"
    for c in "$old_container" "$old_container_alt"; do
        if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$c"; then
            log_info "Migrating from old container ($c) - stopping and removing..."
            docker stop "$c" 2>/dev/null || true
            docker rm "$c" 2>/dev/null || true
            log_success "Old container removed"
        fi
    done
}

# Initial bootstrap - run boot-update and start container
bootstrap_container() {
    migrate_from_old_container
    
    log_info "Running initial boot-update..."

    # Seed last-known-good from GitHub (not the Docker "latest" tag)
    local target_version=""
    target_version=$(BOOT_MODE=watchdog /usr/local/bin/aviationwx-supervisor.sh print-target-version 2>/dev/null || true)
    target_version=$(printf '%s\n' "$target_version" | head -1 | tr -d '[:space:]')
    if [[ "$target_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "$target_version" > "${DATA_DIR}/last-known-good.txt"
        log_info "Resolved latest release from GitHub: $target_version"
    else
        log_warn "Could not resolve release from GitHub; boot-update will determine version"
    fi

    /usr/local/bin/aviationwx-supervisor.sh boot-update || true

    if docker ps --filter "name=${CONTAINER_NAME}" --format '{{.Names}}' 2>/dev/null | grep -qx "${CONTAINER_NAME}"; then
        log_info "Container already running after boot-update"
    else
        log_info "Starting bridge container..."
        local start_version
        start_version=$(cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || true)
        if [ -z "$start_version" ] || [ "$start_version" = "latest" ] || [ "$start_version" = "edge" ]; then
            start_version=$(BOOT_MODE=watchdog /usr/local/bin/aviationwx-supervisor.sh print-target-version 2>/dev/null || true)
            start_version=$(printf '%s\n' "$start_version" | head -1 | tr -d '[:space:]')
        fi
        if [[ "$start_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][A-Za-z0-9.]+)?$ ]] || [ "$start_version" = "edge" ]; then
            /usr/local/bin/aviationwx-container-start.sh "$start_version"
        else
            log_warn "Could not resolve release from GitHub; starting with container-start default tag"
            /usr/local/bin/aviationwx-container-start.sh
        fi
    fi
    
    # Wait for container to be healthy
    log_info "Waiting for bridge to start..."
    local attempts=0
    local max_attempts=30
    while [[ $attempts -lt $max_attempts ]]; do
        if curl -sf "http://127.0.0.1:${WEB_PORT}/readyz" > /dev/null 2>&1; then
            log_success "Bridge is running and capture-ready"
            return 0
        fi
        sleep 2
        ((attempts++))
    done

    if [[ $attempts -ge $max_attempts ]]; then
        log_warn "Health check timed out, but container may still be starting"
    fi
}

# Remove deprecated items from previous versions
remove_deprecated_items() {
    log_info "Cleaning up deprecated components..."
    
    # Deprecated scripts (removed in various versions)
    local deprecated_scripts=(
        "aviationwx-daily-restart"         # v2.0: Replaced by watchdog
        "aviationwx-daily-restart.sh"      # v2.0: Alternative name
        # Add future deprecated scripts here with version comment
    )
    
    # Deprecated systemd services/timers
    local deprecated_systemd=(
        "aviationwx-daily-restart.service"
        "aviationwx-daily-restart.timer"
        # Add future deprecated services here with version comment
    )
    
    # Deprecated cron patterns (will be removed from crontab)
    local deprecated_cron_patterns=(
        "aviationwx-daily-restart"
        # Add future deprecated cron patterns here
    )
    
    # Remove deprecated scripts
    local removed_count=0
    for script in "${deprecated_scripts[@]}"; do
        if [[ -f "/usr/local/bin/${script}" ]]; then
            log_info "Removing deprecated script: ${script}"
            rm -f "/usr/local/bin/${script}"
            ((removed_count++))
        fi
    done
    
    # Stop, disable, and remove deprecated systemd units
    for unit in "${deprecated_systemd[@]}"; do
        if systemctl list-unit-files | grep -q "^${unit}"; then
            log_info "Removing deprecated systemd unit: ${unit}"
            systemctl stop "${unit}" 2>/dev/null || true
            systemctl disable "${unit}" 2>/dev/null || true
            rm -f "/etc/systemd/system/${unit}"
            ((removed_count++))
        fi
    done
    
    # Reload systemd if we removed any units
    if [[ ${removed_count} -gt 0 ]]; then
        systemctl daemon-reload
    fi
    
    # Remove deprecated cron jobs
    for pattern in "${deprecated_cron_patterns[@]}"; do
        if crontab -l 2>/dev/null | grep -q "${pattern}"; then
            log_info "Removing deprecated cron job: ${pattern}"
            crontab -l 2>/dev/null | grep -v "${pattern}" | crontab - 2>/dev/null || true
            ((removed_count++))
        fi
    done
    
    if [[ ${removed_count} -gt 0 ]]; then
        log_success "Removed ${removed_count} deprecated component(s)"
    else
        log_info "No deprecated components found"
    fi
}

# Get device IP
get_ip() {
    hostname -I 2>/dev/null | awk '{print $1}' || echo "localhost"
}

# Print completion message
print_complete() {
    local ip
    ip=$(get_ip)

    echo ""
    echo -e "${GREEN}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${GREEN}║         AviationWX.org Bridge - Installation Complete!         ║${NC}"
    echo -e "${GREEN}╠═══════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Web Console: ${BLUE}http://${ip}:${WEB_PORT}${NC}                         ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Password:    ${YELLOW}aviationwx${NC} (change this!)                     ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Updates are checked daily.                                   ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Watchdog monitors host health every minute.                  ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  ${BLUE}Next steps:${NC}                                                 ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  1. Open the web console                                     ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  2. Change the default password                              ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  3. Add your camera(s)                                       ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  4. Configure upload credentials from aviationwx.org         ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Need credentials? Email: ${BLUE}contact@aviationwx.org${NC}             ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

# Main installation flow
main() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║              AviationWX.org Bridge Installer                   ║${NC}"
    echo -e "${BLUE}║              https://aviationwx.org                           ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    check_root
    detect_os
    install_docker
    install_jq
    configure_docker_logging
    setup_data_dir
    install_host_scripts
    setup_systemd
    remove_deprecated_items
    bootstrap_container
    print_complete
}

# Handle uninstall
uninstall() {
    log_info "Uninstalling AviationWX.org Bridge..."

    # Stop and remove container
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    # Current systemd services/timers
    local current_systemd=(
        "aviationwx-boot-update.service"
        "aviationwx-container.service"
        "aviationwx-daily-update.service"
        "aviationwx-daily-update.timer"
        "aviationwx-watchdog.service"
        "aviationwx-watchdog.timer"
        "aviationwx-capture-restart.service"
        "aviationwx-capture-restart.timer"
    )
    
    # Deprecated systemd services/timers (from older versions)
    local deprecated_systemd=(
        "aviationwx-daily-restart.service"
        "aviationwx-daily-restart.timer"
    )
    
    # Stop and disable all services (current + deprecated)
    for unit in "${current_systemd[@]}" "${deprecated_systemd[@]}"; do
        systemctl stop "${unit}" 2>/dev/null || true
        systemctl disable "${unit}" 2>/dev/null || true
        rm -f "/etc/systemd/system/${unit}"
    done
    
    systemctl daemon-reload

    # Current scripts
    local current_scripts=(
        "aviationwx"
        "aviationwx-supervisor.sh"
        "aviationwx-watchdog.sh"
        "aviationwx-capture-restart.sh"
        "aviationwx-recovery.sh"
        "aviationwx-container-start.sh"
    )
    
    # Deprecated scripts (from older versions)
    local deprecated_scripts=(
        "aviationwx-daily-restart"
        "aviationwx-daily-restart.sh"
    )
    
    # Remove all scripts (current + deprecated)
    for script in "${current_scripts[@]}" "${deprecated_scripts[@]}"; do
        rm -f "/usr/local/bin/${script}"
    done
    
    # Remove deprecated cron jobs
    crontab -l 2>/dev/null | grep -v "aviationwx" | crontab - 2>/dev/null || true

    log_warn "Data directory preserved at ${DATA_DIR}"
    log_warn "To remove data: sudo rm -rf ${DATA_DIR}"
    log_success "AviationWX.org Bridge uninstalled"
}

# Parse arguments
case "${1:-}" in
    uninstall)
        check_root
        uninstall
        ;;
    *)
        main
        ;;
esac
