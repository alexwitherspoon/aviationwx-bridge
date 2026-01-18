#!/bin/bash
# Daily restart script for AviationWX Bridge
# Add to crontab: 0 3 * * * /opt/aviationwx/daily-restart.sh >> /var/log/aviationwx-restart.log 2>&1

set -e

CONTAINER_NAME="aviationwx-bridge"
LOG_PREFIX="[$(date '+%Y-%m-%d %H:%M:%S')]"

echo "$LOG_PREFIX Starting daily restart of $CONTAINER_NAME"

# Check if container exists
if ! docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "$LOG_PREFIX ERROR: Container $CONTAINER_NAME not found"
    exit 1
fi

# Graceful restart
echo "$LOG_PREFIX Restarting container..."
docker restart "$CONTAINER_NAME"

# Wait for health check
echo "$LOG_PREFIX Waiting for container to be healthy..."
for i in {1..30}; do
    HEALTH=$(docker inspect --format='{{.State.Health.Status}}' "$CONTAINER_NAME" 2>/dev/null || echo "none")
    if [ "$HEALTH" = "healthy" ]; then
        echo "$LOG_PREFIX Container is healthy"
        exit 0
    fi
    echo "$LOG_PREFIX Health check $i/30: $HEALTH"
    sleep 2
done

echo "$LOG_PREFIX WARNING: Container did not become healthy within 60 seconds"
exit 1
