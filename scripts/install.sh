#!/bin/bash
#
# AviationWX Bridge - Installation Script
# https://github.com/alexwitherspoon/aviationwx-bridge
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
#
# This script:
#   1. Installs Docker (if not present)
#   2. Installs the update supervisor
#   3. Pulls and starts the AviationWX Bridge container
#   4. Configures automatic updates
#

set -euo pipefail

# Configuration
REPO="alexwitherspoon/aviationwx-bridge"
IMAGE_NAME="ghcr.io/${REPO}"
CONTAINER_NAME="aviationwx-bridge"
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

# Configure Docker logging for SD card protection (Raspberry Pi)
configure_docker_logging() {
    log_info "Configuring Docker logging for SD card protection..."
    
    # Check if running on Raspberry Pi
    if [[ $OS == "raspbian" ]] || grep -qi "raspberry" /proc/cpuinfo 2>/dev/null; then
        log_info "Raspberry Pi detected - configuring journald with volatile storage"
        
        # Configure journald for volatile (RAM) storage
        if [[ ! -f /etc/systemd/journald.conf.d/aviationwx.conf ]]; then
            mkdir -p /etc/systemd/journald.conf.d
            cat > /etc/systemd/journald.conf.d/aviationwx.conf << 'EOF'
# AviationWX Bridge - Journald volatile storage
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
        
        # Configure Docker to use journald
        if [[ ! -f /etc/docker/daemon.json ]]; then
            mkdir -p /etc/docker
            cat > /etc/docker/daemon.json << 'EOF'
{
  "log-driver": "journald",
  "log-opts": {
    "tag": "{{.Name}}"
  }
}
EOF
            systemctl restart docker
            log_success "Configured Docker to use journald logging"
        else
            log_warn "Docker daemon.json exists - not overwriting"
            log_info "To enable journald logging, add to /etc/docker/daemon.json:"
            log_info '  {"log-driver": "journald", "log-opts": {"tag": "{{.Name}}"}}'
        fi
    else
        log_info "Not a Raspberry Pi - using default Docker logging"
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
# AviationWX Bridge Environment Configuration
# Edit this file to customize settings, then restart the container.
#
# Tmpfs size for image queue (RAM-based storage)
# Default: 200m (200 megabytes)
#
# Sizing recommendations:
#   1-2 cameras @ 1080p: 100m is sufficient
#   3-4 cameras @ 1080p: 200m recommended (default)
#   1-2 cameras @ 4K:    200m recommended
#   3-4 cameras @ 4K:    300m or higher
#
# Note: Pi Zero 2 W has 512MB RAM. Keep this + application memory under ~450MB total.
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

    # Create initial host version file
    echo "2.0" > "${DATA_DIR}/host-version.txt"

    log_success "Host scripts installed"
}

# Setup systemd services
setup_systemd() {
    log_info "Setting up systemd services..."

    # Boot-time update service
    cat > /etc/systemd/system/aviationwx-boot-update.service << 'EOF'
[Unit]
Description=AviationWX Bridge Boot-Time Update
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
Description=AviationWX Bridge Container
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
Description=AviationWX Bridge Daily Update Check

[Timer]
OnCalendar=daily
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    cat > /etc/systemd/system/aviationwx-daily-update.service << 'EOF'
[Unit]
Description=AviationWX Bridge Daily Update
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
Description=AviationWX Bridge Watchdog Timer

[Timer]
OnBootSec=2min
OnUnitActiveSec=1min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    cat > /etc/systemd/system/aviationwx-watchdog.service << 'EOF'
[Unit]
Description=AviationWX Bridge Watchdog
After=docker.service

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-watchdog.sh
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

# Initial bootstrap - run boot-update and start container
bootstrap_container() {
    log_info "Running initial boot-update..."
    
    # Initialize last-known-good to latest
    echo "latest" > "${DATA_DIR}/last-known-good.txt"
    
    # Run boot-update
    /usr/local/bin/aviationwx-supervisor.sh boot-update
    
    # Wait for container to be healthy
    log_info "Waiting for bridge to start..."
    local attempts=0
    local max_attempts=30
    while [[ $attempts -lt $max_attempts ]]; do
        if curl -sf "http://localhost:${WEB_PORT}/healthz" > /dev/null 2>&1; then
            log_success "Bridge is running and healthy"
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
    echo -e "${GREEN}║         AviationWX Bridge - Installation Complete!            ║${NC}"
    echo -e "${GREEN}╠═══════════════════════════════════════════════════════════════╣${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Web Console: ${BLUE}http://${ip}:${WEB_PORT}${NC}                          ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Password:    ${YELLOW}aviationwx${NC} (change this!)                      ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Updates are checked daily.                                    ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Watchdog monitors host health every minute.                   ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  ${BLUE}Next steps:${NC}                                                  ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  1. Open the web console                                      ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  2. Change the default password                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  3. Add your camera(s)                                        ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  4. Configure upload credentials from aviationwx.org          ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Need credentials? Email: ${BLUE}contact@aviationwx.org${NC}              ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}                                                               ${GREEN}║${NC}"
    echo -e "${GREEN}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""
}

# Main installation flow
main() {
    echo ""
    echo -e "${BLUE}╔═══════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║              AviationWX Bridge Installer                      ║${NC}"
    echo -e "${BLUE}║              https://aviationwx.org                           ║${NC}"
    echo -e "${BLUE}╚═══════════════════════════════════════════════════════════════╝${NC}"
    echo ""

    check_root
    detect_os
    install_docker
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
    log_info "Uninstalling AviationWX Bridge..."

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
    log_success "AviationWX Bridge uninstalled"
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
