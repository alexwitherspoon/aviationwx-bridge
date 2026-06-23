# AviationWX.org Bridge

Remote bridge device for capturing webcam snapshots and uploading them to AviationWX.org. Designed for low-power single-board computers (SBCs) such as the Raspberry Pi Zero 2 W and comparable boards.

## Overview

AviationWX.org Bridge is a lightweight daemon that:
- Captures webcam snapshots from local network cameras (HTTP, ONVIF, RTSP)
- Queues images locally with accurate observation timestamps
- Uploads to `upload.aviationwx.org` via SFTP
- Uses tmpfs (RAM) for image buffering to avoid SD card wear
- Provides a modern web console for configuration and monitoring

**Perfect for**: Airport operators wanting to provide webcam feeds to pilots for weather assessment and flight safety.

**Network:** The bridge is a **local LAN** service (trusted private network or VPN). It is **not** designed to be reachable from the public internet; do not port-forward the web console. See [Network exposure](docs/DEPLOYMENT.md#network-exposure) in the deployment guide.

---

## Installation

Choose the path that matches your environment:

### Path A: Supervised Install (Set and Forget)

**Best for:** Dedicated single-board computers at remote locations with minimal IT support. Works on a Raspberry Pi or a comparable SBC (see [Hardware Requirements](#hardware-requirements)).

One command installs everything:

```bash
curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/AviationWX.org-Bridge/main/scripts/install.sh | sudo bash
```

**This script will:**
1. Install Docker (if not already installed)
2. Install a lightweight update supervisor
3. Pull and start the AviationWX.org Bridge container
4. Configure automatic security updates
5. Set up automatic restart on boot

**After installation:**
- Web console: `http://<your-device-ip>:1229`
- Default password: `aviationwx` (change this immediately!)
- Updates are checked daily (systemd timer)
- Critical security updates apply automatically

---

### Path B: Docker (IT-Managed)

**Best for:** Professional environments with existing Docker infrastructure and IT teams. This path runs the container directly, without the host supervisor, so updates and rollback are handled by your own tooling.

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

**Docker Compose:**

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
      - /dev/shm:size=200m  # Adjust based on camera count/resolution
```

**Your responsibility:**
- Manage updates via your existing tooling (Portainer, Watchtower, Kubernetes, etc.)
- Monitor container health
- Handle rollbacks if needed

**We provide:**
- Semantic versioned Docker images (`:latest`, `:1.0.0`, `:1.0`)
- Health: `/readyz` (capture readiness, used by Docker); `/healthz` (process status)
- Changelog with breaking changes clearly marked

---

## Updates

### Supervised Install (Path A)

Updates are handled automatically by the supervisor:

| Update Type | Behavior |
|-------------|----------|
| **Normal** | Notification shown in web UI; user can apply when convenient |
| **Critical** | Auto-applies after 24-hour grace period |
| **Emergency** | Applies immediately (rare, security issues only) |

All updates include automatic rollback if health checks fail.

**Manual update:**
```bash
sudo systemctl start aviationwx-supervisor
```

**Check update status:**
```bash
cat /data/aviationwx/update-available.json
```

### Docker (Path B)

The web console still notifies you when a newer release is available. One-click apply is disabled unless `AVIATIONWX_SELF_UPDATE=1` (supervised installs set this automatically). Update the image with your orchestration tooling:

```bash
docker pull ghcr.io/alexwitherspoon/aviationwx-org-bridge:latest
docker stop aviationwx-org-bridge
docker rm aviationwx-org-bridge
docker run -d ... # (same run command as before)
```

---

## Configuration

Access the web console at `http://<device-ip>:1229/` to configure:

- Camera sources (URL, authentication)
- Capture intervals (1 second to 30 minutes)
- Local timezone (for EXIF interpretation)
- Image processing (resize, quality)
- Queue management settings

### SFTP Credentials

Contact [contact@aviationwx.org](mailto:contact@aviationwx.org) to obtain upload credentials.

### Example Config

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
        "protocol": "sftp",
        "host": "upload.aviationwx.org",
        "port": 2222,
        "username": "your-username",
        "password": "your-password"
      }
    }
  ]
}
```

---

## Features

- **Multiple Camera Types**: HTTP snapshot, ONVIF, RTSP (via ffmpeg)
- **Historic Replay**: Queue images for time-series display on aviationwx.org
- **Accurate Timestamps**: UTC observation times with EXIF validation (via exiftool)
- **Web Console**: Modern dashboard with camera preview and management
- **Secure Upload**: SFTP with fail2ban-aware retry logic
- **Low Memory**: Optimized for low-memory SBCs such as the Raspberry Pi Zero 2 W (512MB RAM)
- **NTP Health**: Automatic time validation and drift detection
- **Auto Updates**: Critical security updates with automatic rollback (Path A)
- **Hot-Reload**: Camera, timezone, and SNTP config changes apply instantly (no restart)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     AviationWX.org Bridge                       │
├─────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Camera    │  │   Camera    │  │    Web Console      │  │
│  │   Worker    │  │   Worker    │  │    (port 1229)      │  │
│  └──────┬──────┘  └──────┬──────┘  └─────────────────────┘  │
│         │                │                                   │
│         ▼                ▼                                   │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │              File Queue (tmpfs /dev/shm)                │ │
│  │   ├── camera-1/                                         │ │
│  │   │   ├── 20231225T143022Z.jpg                          │ │
│  │   │   └── 20231225T143122Z.jpg                          │ │
│  │   └── camera-2/                                         │ │
│  │       └── 20231225T143052Z.jpg                          │ │
│  └─────────────────────────────────────────────────────────┘ │
│         │                                                    │
│         ▼                                                    │
│  ┌─────────────────────────────────────────────────────────┐ │
│  │              Upload Worker (round-robin)                │ │
│  │   → SFTP to upload.aviationwx.org                        │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

---

## Hardware Requirements

The bridge runs on any 64-bit Linux host with Docker (ARM64, ARMv7, or x86-64). It is built for small, low-power single-board computers but also runs on a mini-PC or VM.

**Minimum (low-memory SBC, e.g. Raspberry Pi Zero 2 W):**
- 512MB RAM
- 8GB SD card (or eMMC/USB)
- Network access to cameras and the internet

**Recommended:**
- 3GB or more RAM (4GB and up is comfortable)
- A quad-core CPU in the class of the Raspberry Pi 4 or newer
- 16GB+ storage (SD, eMMC, or NVMe)
- Wired ethernet for reliability

**Example boards:** The Raspberry Pi 4 and 5 are the best-documented choices. Comparable single-board computers also work, including the Radxa ROCK series, Orange Pi 5 series, Libre Computer boards, or an Intel N100-class mini-PC. These are examples, not endorsements; any board that runs 64-bit Linux with Docker and meets the recommended specs is a good fit. The Raspberry Pi has the broadest community and OS support, while other boards often offer more RAM, storage, or I/O.

---

## Security

- Container runs as non-root user
- Minimal Linux capabilities
- SFTP for secure uploads
- Web console protected by password authentication
- Read-only filesystem (only `/data` writable)
- Automatic security updates (Path A)

---

## Troubleshooting

### View logs
```bash
# Container logs
docker logs aviationwx-org-bridge

# Supervisor logs (Path A, supervised install only)
cat /data/aviationwx/supervisor.log
```

### Restart the bridge
```bash
docker restart aviationwx-org-bridge
```

### Force rollback (Path A only)
```bash
sudo /usr/local/bin/aviationwx-container-start.sh "$(cat /data/aviationwx/last-known-good.txt)"
```

### Complete reinstall
```bash
docker stop aviationwx-org-bridge
docker rm aviationwx-org-bridge
# Re-run installation command
```

---

## Documentation

- **[Development](docs/DEVELOPMENT.md)** - Build, test, run locally
- **[Deployment Guide](docs/DEPLOYMENT.md)** - Production deployment details
- **[Queue & Memory Management](docs/QUEUE_STORAGE.md)** - How storage and memory are managed
- **[Config Reference](docs/CONFIG_SCHEMA.md)** - Full configuration options
- **[Changelog](CHANGELOG.md)** - Version history

---

## Web Console Screenshots

### Dashboard

![Dashboard](docs/images/dashboard.jpg)

### Cameras

![Cameras](docs/images/cameras.jpg)

### Add Camera

![Add Camera](docs/images/add-camera.jpg)

### Settings

![Settings](docs/images/settings.jpg)

### Logs

![Logs](docs/images/logs.jpg)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - See [LICENSE](LICENSE)

---

**Made for pilots, by pilots** ✈️

Contact: [contact@aviationwx.org](mailto:contact@aviationwx.org)
