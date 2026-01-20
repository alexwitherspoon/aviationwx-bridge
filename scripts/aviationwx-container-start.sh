#!/bin/bash
# AviationWX Bridge - Container Startup
# Started by systemd after boot-update completes
# Version: 2.0
# Features: Dynamic resource limits based on system capabilities

set -euo pipefail

readonly CONTAINER_NAME="aviationwx-bridge"
readonly IMAGE_NAME="ghcr.io/alexwitherspoon/aviationwx-bridge"
readonly DATA_DIR="/data/aviationwx"

# ============================================================================
# RESOURCE DETECTION
# ============================================================================

# Detect total system memory in MB
get_total_memory_mb() {
    local mem_kb
    mem_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
    echo $((mem_kb / 1024))
}

# Detect total CPU cores
get_total_cpus() {
    nproc 2>/dev/null || echo 1
}

# Calculate appropriate Docker memory limit based on total RAM
# Strategy:
#   < 1GB:   Use 60% (be very conservative - Pi Zero 2 W)
#   1-2GB:   Use 65% (still conservative - Pi 3/4 1GB)
#   2-4GB:   Use 70% (balanced - Pi 4 2GB)
#   4-8GB:   Use 75% (comfortable - Pi 4 4GB)
#   > 8GB:   Use 80% (generous - Server/Pi 4 8GB)
calculate_memory_limits() {
    local total_mb=$1
    local docker_limit_mb
    local go_limit_mb
    local tmpfs_mb
    
    if [ "$total_mb" -lt 1024 ]; then
        # < 1GB: Very conservative (Pi Zero 2 W: 416MB * 0.6 = 250MB)
        docker_limit_mb=$((total_mb * 60 / 100))
        go_limit_mb=$((docker_limit_mb * 85 / 100))  # 85% of Docker limit
        tmpfs_mb=100
    elif [ "$total_mb" -lt 2048 ]; then
        # 1-2GB: Conservative (Pi 3B+: 1GB * 0.65 = 650MB)
        docker_limit_mb=$((total_mb * 65 / 100))
        go_limit_mb=$((docker_limit_mb * 88 / 100))
        tmpfs_mb=150
    elif [ "$total_mb" -lt 4096 ]; then
        # 2-4GB: Balanced (Pi 4 2GB: 2GB * 0.7 = 1.4GB)
        docker_limit_mb=$((total_mb * 70 / 100))
        go_limit_mb=$((docker_limit_mb * 90 / 100))
        tmpfs_mb=200
    elif [ "$total_mb" -lt 8192 ]; then
        # 4-8GB: Comfortable (Pi 4 4GB: 4GB * 0.75 = 3GB)
        docker_limit_mb=$((total_mb * 75 / 100))
        go_limit_mb=$((docker_limit_mb * 90 / 100))
        tmpfs_mb=300
    else
        # > 8GB: Generous (Server/Pi 4 8GB)
        docker_limit_mb=$((total_mb * 80 / 100))
        go_limit_mb=$((docker_limit_mb * 90 / 100))
        tmpfs_mb=500
    fi
    
    # Ensure minimums for stability
    [ "$docker_limit_mb" -lt 200 ] && docker_limit_mb=200
    [ "$go_limit_mb" -lt 170 ] && go_limit_mb=170
    [ "$tmpfs_mb" -lt 50 ] && tmpfs_mb=50
    
    echo "$docker_limit_mb $go_limit_mb $tmpfs_mb"
}

# Calculate appropriate CPU limits
# Strategy: Reserve 1 core for OS on multi-core systems
#   1 core:   Use all (no choice)
#   2 cores:  Use 1.5 (leave 0.5 for OS)
#   3 cores:  Use 2 (leave 1 for OS)
#   4+ cores: Use N-1 (leave 1 core for OS)
calculate_cpu_limits() {
    local total_cpus=$1
    local docker_cpus
    local cpu_shares
    
    if [ "$total_cpus" -eq 1 ]; then
        docker_cpus="1"
        cpu_shares=1024  # Full priority (only core)
    elif [ "$total_cpus" -eq 2 ]; then
        docker_cpus="1.5"
        cpu_shares=768   # 75% priority
    else
        docker_cpus=$((total_cpus - 1))
        cpu_shares=768   # 75% priority (OS gets 100%)
    fi
    
    echo "$docker_cpus $cpu_shares"
}

# ============================================================================
# MAIN STARTUP
# ============================================================================

# Get version to start (set by boot-update)
VERSION=$(cat "${DATA_DIR}/last-known-good.txt" 2>/dev/null || echo "latest")

# Detect system resources
TOTAL_MEMORY_MB=$(get_total_memory_mb)
TOTAL_CPUS=$(get_total_cpus)

echo "[$(date -Iseconds)] System resources detected:"
echo "  Total Memory: ${TOTAL_MEMORY_MB} MB"
echo "  Total CPUs: ${TOTAL_CPUS}"

# Calculate resource limits
read -r DOCKER_MEM GO_MEM TMPFS_SIZE <<< "$(calculate_memory_limits "$TOTAL_MEMORY_MB")"
read -r DOCKER_CPUS CPU_SHARES <<< "$(calculate_cpu_limits "$TOTAL_CPUS")"

echo "[$(date -Iseconds)] Calculated resource limits:"
echo "  Docker Memory Limit: ${DOCKER_MEM} MB"
echo "  Go Memory Limit (GOMEMLIMIT): ${GO_MEM} MB"
echo "  tmpfs Size: ${TMPFS_SIZE} MB"
echo "  Docker CPU Limit: ${DOCKER_CPUS} CPUs"
echo "  CPU Shares: ${CPU_SHARES}"

echo "[$(date -Iseconds)] Starting container with version: $VERSION"

# Remove existing container if present
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Start container with dynamic resource limits
docker run -d \
    --name "$CONTAINER_NAME" \
    --restart no \
    \
    `# Memory limits (calculated based on system RAM)` \
    --memory="${DOCKER_MEM}m" \
    --memory-reservation="$((DOCKER_MEM * 85 / 100))m" \
    --memory-swap="${DOCKER_MEM}m" \
    --oom-kill-disable=false \
    \
    `# CPU limits (calculated based on system cores)` \
    --cpus="${DOCKER_CPUS}" \
    --cpu-shares="${CPU_SHARES}" \
    \
    `# Process limits (prevent fork bombs)` \
    --pids-limit=200 \
    \
    `# Pass Go memory limit as environment variable` \
    -e "GOMEMLIMIT=${GO_MEM}MiB" \
    \
    `# Network and volumes` \
    -p 1229:1229 \
    -v "${DATA_DIR}:/data" \
    --tmpfs "/dev/shm:size=${TMPFS_SIZE}m" \
    \
    `# Health check` \
    --health-cmd='wget --no-verbose --tries=1 --spider http://localhost:1229/healthz || exit 1' \
    --health-interval=30s \
    --health-timeout=5s \
    --health-start-period=30s \
    --health-retries=3 \
    \
    "${IMAGE_NAME}:${VERSION}"

echo "[$(date -Iseconds)] Container started successfully with dynamic resource limits"
