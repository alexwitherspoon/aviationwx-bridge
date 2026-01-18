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

# Install supervisor script
install_supervisor() {
    log_info "Installing update supervisor..."

    # Download supervisor script
    curl -fsSL \
        "https://raw.githubusercontent.com/${REPO}/main/scripts/supervisor.sh" \
        -o /usr/local/bin/aviationwx-supervisor
    chmod +x /usr/local/bin/aviationwx-supervisor

    # Create systemd service
    cat > /etc/systemd/system/aviationwx-supervisor.service << 'EOF'
[Unit]
Description=AviationWX Bridge Update Supervisor
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/aviationwx-supervisor check
TimeoutStartSec=300
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

    # Create systemd timer (runs every 6 hours)
    cat > /etc/systemd/system/aviationwx-supervisor.timer << 'EOF'
[Unit]
Description=AviationWX Bridge Update Check Timer

[Timer]
OnBootSec=5min
OnUnitActiveSec=6h
RandomizedDelaySec=30min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    # Enable timer
    systemctl daemon-reload
    systemctl enable aviationwx-supervisor.timer
    systemctl start aviationwx-supervisor.timer

    log_success "Supervisor installed and timer enabled"
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

# Pull and start the container
start_container() {
    load_environment
    local tmpfs_size
    tmpfs_size=$(get_tmpfs_size)

    log_info "Pulling latest AviationWX Bridge image..."
    docker pull "${IMAGE_NAME}:latest"

    # Stop existing container if present
    if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
        log_info "Stopping existing container..."
        docker stop "${CONTAINER_NAME}" 2>/dev/null || true
        docker rm "${CONTAINER_NAME}" 2>/dev/null || true
    fi

    log_info "Starting AviationWX Bridge (tmpfs: ${tmpfs_size})..."
    docker run -d \
        --name "${CONTAINER_NAME}" \
        --restart=unless-stopped \
        -p "${WEB_PORT}:${WEB_PORT}" \
        -v "${DATA_DIR}:/data" \
        --tmpfs /dev/shm:size="${tmpfs_size}" \
        "${IMAGE_NAME}:latest"

    # Wait for container to be healthy
    log_info "Waiting for bridge to start..."
    local attempts=0
    local max_attempts=30
    while [[ $attempts -lt $max_attempts ]]; do
        if curl -sf "http://localhost:${WEB_PORT}/healthz" > /dev/null 2>&1; then
            log_success "Bridge is running and healthy"
            break
        fi
        sleep 2
        ((attempts++))
    done

    if [[ $attempts -ge $max_attempts ]]; then
        log_warn "Health check timed out, but container may still be starting"
    fi

    # Record initial version
    local version
    version=$(docker inspect "${CONTAINER_NAME}" --format '{{.Config.Image}}' | cut -d: -f2)
    echo "${version}" > "${DATA_DIR}/current-version"
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
    echo -e "${GREEN}║${NC}  Updates are checked every 6 hours.                           ${GREEN}║${NC}"
    echo -e "${GREEN}║${NC}  Critical updates apply automatically after 24 hours.         ${GREEN}║${NC}"
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
    setup_data_dir
    install_supervisor
    start_container
    print_complete
}

# Handle uninstall
uninstall() {
    log_info "Uninstalling AviationWX Bridge..."

    # Stop and remove container
    docker stop "${CONTAINER_NAME}" 2>/dev/null || true
    docker rm "${CONTAINER_NAME}" 2>/dev/null || true

    # Remove supervisor
    systemctl stop aviationwx-supervisor.timer 2>/dev/null || true
    systemctl disable aviationwx-supervisor.timer 2>/dev/null || true
    rm -f /etc/systemd/system/aviationwx-supervisor.service
    rm -f /etc/systemd/system/aviationwx-supervisor.timer
    rm -f /usr/local/bin/aviationwx-supervisor
    systemctl daemon-reload

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
