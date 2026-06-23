# Deployment Guide

This guide covers deploying AviationWX.org Bridge to production.

## Deployment Options

Choose the deployment method that matches your environment:

| Method | Best For | Updates |
|--------|----------|---------|
| [Raspberry Pi Install Script](#raspberry-pi-recommended) | Set-and-forget remote devices | Automatic with rollback |
| [Docker (IT-Managed)](#docker-it-managed) | Professional environments | Manual via your tooling |

---

## Raspberry Pi (Recommended)

### One-Command Installation

```bash
curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/AviationWX.org-Bridge/main/scripts/install.sh | sudo bash
```

This script:
1. Installs Docker (if not present)
2. Installs the update supervisor
3. Pulls and starts the AviationWX.org Bridge container
4. Configures automatic security updates
5. Sets up automatic restart on boot

### After Installation

- **Web Console**: `http://<device-ip>:1229`
- **Default Password**: `aviationwx` (change immediately!)
- **Health Check**: `http://<device-ip>:1229/readyz` (capture readiness; `/healthz` for process-level status)

### Network exposure

AviationWX.org Bridge is a **local LAN utility**: the web console and health endpoints are for trusted local operators and on-device scripts, not for the public internet. **Do not** expose port `1229` to the WAN (avoid router port forwarding and public binding). Use normal firewall or router rules so only your intended LAN (or VPN) can reach the device, consistent with any other admin UI on a small embedded host.

### Automatic Updates

The supervisor checks for updates **once per day** (`aviationwx-daily-update.timer`, with up to 30 minutes jitter).

| Update Type | Behavior |
|-------------|----------|
| Normal | Shows in web UI; user applies when ready |
| Critical (security) | Auto-applies after 24-hour grace period |
| Emergency | Applies immediately |

All updates automatically rollback if health checks fail.

### Self-recovery (no nightly restart required)

Install enables layered recovery instead of a blind daily container restart:

| Mechanism | Interval | Action |
|-----------|----------|--------|
| **Watchdog** | Every 1 min | NTP/network/Docker fixes; restarts **exited** container; runs capture-restart for `/readyz` failures (Docker **unhealthy** uses same rate limits, ~5 min to restart on 503) |
| **Capture-restart timer** | Every 5 min | Same script as watchdog backup; slower streak accumulation (~25 min to restart on 503) |
| **Bridge (in-process)** | Continuous | Queue resume after drain/space recovery; hourly SFTP probe per empty camera queue; camera retry/restart every 15 min |

Docker health checks use **`/readyz`** so an alive but non-capturing container is unhealthy.

Do not add a nightly container restart cron unless you have a specific ops reason; capture-restart targets stale state without disrupting healthy sites.

**Existing Pis:** `aviationwx-capture-restart.timer` is enabled automatically on the next `boot-update` / daily update once supervisor v2.2+ is deployed (no `install.sh` rerun). Verify with `systemctl is-enabled aviationwx-capture-restart.timer`.

**Recovery cap exhausted:** After 6 container restarts in 24h, the host writes `/data/aviationwx/recovery-exhausted.json` (visible in the web UI status and `/healthz` as unhealthy). Cleared when `/readyz` returns 200 or a restart succeeds. Requires manual intervention until the condition clears.

**Manual update:**
```bash
sudo aviationwx update
```

Supervisor **upgrade checks** compare the running bridge semver and the **GitHub release** for your channel (not the Docker `:latest` tag alone). The web UI still shows the configured local image tag from `configured-image-tag.txt` / `last-known-good.txt` when it differs from the running version.

**Check status:**
```bash
sudo /usr/local/bin/aviationwx-supervisor.sh status
```

**Rollback** (recreate container from last-known-good):
```bash
sudo /usr/local/bin/aviationwx-container-start.sh "$(cat /data/aviationwx/last-known-good.txt)"
```

---

## Docker (IT-Managed)

For environments with existing Docker infrastructure.

### Basic Deployment

```bash
docker pull ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest

docker run -d \
  --name aviationwx-org-bridge \
  --restart=unless-stopped \
  -p 1229:1229 \
  -v /opt/aviationwx/data:/data \
  --tmpfs /dev/shm:size=200m \
  ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest
```

### Docker Compose

```yaml
services:
  aviationwx-org-bridge:
    image: ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest
    container_name: aviationwx-org-bridge
    restart: unless-stopped
    ports:
      - "1229:1229"
    volumes:
      - ./data:/data
    tmpfs:
      - /dev/shm:size=200m
    environment:
      - LOG_LEVEL=info
```

### Version Pinning

For production stability:

```yaml
# Pin to specific version
image: ghcr.io/alexwitherspoon/aviationwx-org-bridge:1.0.0

# Or pin to minor version (gets patches)
image: ghcr.io/alexwitherspoon/aviationwx-org-bridge:1.0
```

### Updating

```bash
docker pull ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest
docker stop aviationwx-org-bridge
docker rm aviationwx-org-bridge
docker run -d ... # same run command
```

Or use your existing update tooling (Watchtower, Portainer, ArgoCD, etc.)

### Web console update banner

The bridge checks GitHub for new releases in all deployments. **Applying** an update from the web UI requires the Pi supervisor path:

| Deployment | `AVIATIONWX_SELF_UPDATE` | Web UI behavior |
|------------|--------------------------|-----------------|
| Pi (`install.sh` + supervisor) | `1` (default in `aviationwx-container-start.sh`; set `AVIATIONWX_SELF_UPDATE=0` to disable) | Banner applies update via host supervisor |
| Docker / docker-compose (IT-managed) | unset (default) | Banner shows version availability and links to release notes; `POST /api/update` returns 409 |

Do not mount the Docker socket into the bridge container to enable in-container updates.

---

## Configuration

### Initial Setup

1. Open web console at `http://<ip>:1229`
2. Log in with default password (`aviationwx`)
3. **Change the password immediately**
4. Set your timezone
5. Add cameras with their SFTP credentials

### Get SFTP Credentials

Contact [contact@aviationwx.org](mailto:contact@aviationwx.org) to obtain upload credentials for your cameras.

### Example Configuration

```json
{
  "version": 2,
  "timezone": "America/Chicago",
  "cameras": [
    {
      "id": "kord-west",
      "name": "KORD West Runway",
      "type": "http",
      "enabled": true,
      "snapshot_url": "http://192.168.1.100/snapshot.jpg",
      "capture_interval_seconds": 60,
      "upload": {
        "host": "upload.aviationwx.org",
        "port": 2222,
        "username": "your-username",
        "password": "your-password",
        "base_path": "/files"
      }
    }
  ],
  "web_console": {
    "enabled": true,
    "port": 1229,
    "password": "your-secure-password"
  }
}
```

---

## Security

### Hardening Checklist

- [ ] Change default web console password
- [ ] Use HTTPS proxy if exposing to internet (Caddy, nginx, Tailscale)
- [ ] Restrict file permissions: `chmod 600 /data/aviationwx/config.json`
- [ ] Enable firewall, allow only necessary ports
- [ ] Keep system and Docker updated

### Remote Access

**Recommended: Tailscale**

```bash
# Install Tailscale on host
curl -fsSL https://tailscale.com/install.sh | sh
sudo tailscale up

# Access via Tailscale IP (no port forwarding needed)
http://100.x.x.x:1229
```

**Alternative: Reverse Proxy with HTTPS**

```bash
# Example with Caddy
# Caddyfile
aviationwx.yourdomain.com {
    reverse_proxy localhost:1229
}
```

---

## Monitoring

### Health Endpoints

| Endpoint | Purpose |
|----------|---------|
| `/readyz` | Capture readiness (Docker health check; 503 when stale) |
| `/healthz` | Process and orchestrator status (503 when capture not ready) |
| `/api/status` | Detailed system status (JSON) |

```bash
# Capture readiness (matches Docker HEALTHCHECK)
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:1229/readyz

# Process health
curl http://localhost:1229/healthz

# Detailed status
curl http://localhost:1229/api/status | jq
```

### Logs

```bash
# Docker logs
docker logs aviationwx-org-bridge -f

# Last 100 lines
docker logs aviationwx-org-bridge --tail=100

# Supervisor logs (Pi install only)
cat /data/aviationwx/supervisor.log
```

### Metrics (Status Response)

```json
{
  "version": "1.0.0",
  "git_commit": "abc1234",
  "running": true,
  "uptime_seconds": 86400,
  "cameras": [...],
  "upload_stats": {
    "total": 1440,
    "success": 1438,
    "failed": 2
  },
  "ntp_healthy": true,
  "update": {
    "available": false,
    "latest_version": "1.0.0"
  }
}
```

---

## Queue Storage Configuration

The bridge uses a RAM-based filesystem (tmpfs) to store images waiting for upload. This protects SD cards from write wear and provides fast I/O, but the size must be configured appropriately for your setup.

### Why This Matters

Images queue up when:
- Upload connection is temporarily unavailable
- Network is slower than capture rate
- SFTP server is briefly unreachable

The queue must be large enough to hold images during these periods without losing data.

### Sizing Recommendations

| Setup | Image Size (typical) | 10-min Backlog | Recommended tmpfs |
|-------|---------------------|----------------|-------------------|
| 1-2 cameras @ 1080p | ~1 MB | ~20 images | `100m` |
| 3-4 cameras @ 1080p | ~1 MB | ~40 images | `200m` (default) |
| 1-2 cameras @ 4K | ~3-5 MB | ~20 images | `200m` |
| 3-4 cameras @ 4K | ~3-5 MB | ~40 images | `300m` or higher |

**Note**: Pi Zero 2 W has 512MB RAM. Keep tmpfs + application memory under ~450MB total.

### Checking Current Usage

The web UI dashboard shows queue storage usage in real-time:
- **Healthy** (green): Under 50% capacity
- **Catching Up** (yellow): 50-80% capacity  
- **Critical** (red): Over 80% capacity

You can also check via API:
```bash
curl http://localhost:1229/api/status | jq '.orchestrator.queue_storage'
```

### Changing the tmpfs Size

The tmpfs size is set at container startup via Docker and cannot be changed without restarting the container.

#### Raspberry Pi (install script)

Edit the environment file, then restart:

```bash
# Edit the environment file
sudo nano /data/aviationwx/environment

# Uncomment and modify this line:
AVIATIONWX_TMPFS_SIZE=300m

# Restart to apply (uses supervisor to recreate container)
sudo /usr/local/bin/aviationwx-supervisor.sh force-update
```

#### Docker Run

Change the `--tmpfs` size parameter:

```bash
docker stop aviationwx-org-bridge
docker rm aviationwx-org-bridge
docker run -d \
  --name aviationwx-org-bridge \
  --restart=unless-stopped \
  -p 1229:1229 \
  -v /data/aviationwx:/data \
  --tmpfs /dev/shm:size=300m \
  ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest
```

#### Docker Compose

Update the `tmpfs` size in your compose file:

```yaml
services:
  aviationwx-org-bridge:
    # ... other config ...
    tmpfs:
      - /dev/shm:size=300m  # Increased from default 200m
```

Then recreate the container:
```bash
docker-compose down
docker-compose up -d
```

#### Using Environment Variable (Docker Compose)

For easier management, use the `AVIATIONWX_TMPFS_SIZE` environment variable:

```yaml
services:
  aviationwx-org-bridge:
    # ... other config ...
    tmpfs:
      - /dev/shm:size=${AVIATIONWX_TMPFS_SIZE:-200m}
```

Then set in your `.env` file:
```bash
AVIATIONWX_TMPFS_SIZE=300m
```

### Application-Level Queue Settings

In addition to the tmpfs size (filesystem limit), the application has its own queue limits in `config.json`:

```json
{
  "queue": {
    "max_total_size_mb": 100,    // Max total queue size (all cameras)
    "max_heap_mb": 400           // Trigger emergency thin if heap exceeds this
  }
}
```

The `max_total_size_mb` should be less than or equal to your tmpfs size. If you increase tmpfs, you may also want to increase this setting.

---

## Resource Limits

### Raspberry Pi Zero 2 W (512MB RAM)

The bridge is optimized for low memory:

- Queue stored in tmpfs (RAM): 200MB default
- Heap limit: 400MB
- Typical usage: 150-300MB total

```yaml
# Optional: enforce limits in Docker
deploy:
  resources:
    limits:
      memory: 400M
```

### Disk Usage

Config and logs stored in `/data` volume (~10MB typical).

Queue stored in tmpfs (`/dev/shm`) - no SD card wear.

---

## Backup & Recovery

### Backup Config

```bash
# Manual backup
cp /data/aviationwx/config.json ~/config-backup.json

# Or via Docker volume
docker run --rm \
  -v aviationwx-data:/data \
  -v $(pwd):/backup \
  alpine tar czf /backup/config-backup.tar.gz -C /data .
```

### Restore Config

```bash
# Copy back
cp ~/config-backup.json /data/aviationwx/config.json

# Restart
docker restart aviationwx-org-bridge
```

### Complete Recovery

If device fails:

1. Flash fresh Raspberry Pi OS
2. Run install script: `curl ... | sudo bash`
3. Restore config backup
4. Cameras will resume uploading

---

## Troubleshooting

### Container Won't Start

```bash
# Check logs
docker logs aviationwx-org-bridge

# Validate config
python3 -m json.tool /data/aviationwx/config.json

# Check resources
free -h
df -h
```

### Camera Capture Fails

```bash
# Test camera URL from host
curl -v http://192.168.1.100/snapshot.jpg

# Check camera auth in config
# Verify network connectivity to camera
```

### Upload Fails

```bash
# Check SFTP credentials
# Check firewall allows outbound port 2222

# Test connection
ssh -p 2222 your-username@upload.aviationwx.org
```

### Memory Issues

```bash
# Check container memory
docker stats aviationwx-org-bridge

# Check queue storage usage
curl http://localhost:1229/api/status | jq '.orchestrator.queue_storage'
```

If memory usage is consistently high:
- Check queue storage in web UI - if it's backing up, investigate upload issues
- Reduce image resolution via camera settings or `image.max_width` in config
- Increase capture interval to reduce queue buildup
- If queue is healthy but memory is high, tmpfs may be oversized

If queue shows "Critical" status frequently:
- Increase tmpfs size (see [Queue Storage Configuration](#queue-storage-configuration))
- Check upload connection stability
- Reduce image quality to decrease file sizes

---

## Support

- **Documentation**: [github.com/alexwitherspoon/AviationWX.org-Bridge](https://github.com/alexwitherspoon/AviationWX.org-Bridge)
- **Issues**: [GitHub Issues](https://github.com/alexwitherspoon/AviationWX.org-Bridge/issues)
- **Credentials**: [contact@aviationwx.org](mailto:contact@aviationwx.org)
