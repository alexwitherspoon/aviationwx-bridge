# Configuration Schema

**Config Version**: 2  
**Format**: JSON  
**Location**: `/data/config.json` (or `$AVIATIONWX_CONFIG`)

## Quick Reference

```json
{
  "version": 2,
  "timezone": "America/Chicago",
  "cameras": [
    {
      "id": "camera-1",
      "name": "Runway View",
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
  ],
  "web_console": {
    "enabled": true,
    "port": 1229,
    "password": "aviationwx"
  }
}
```

## Full Schema

### Root Level

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `version` | integer | Yes | - | Schema version (must be `2`) |
| `timezone` | string | No | `"UTC"` | IANA timezone (e.g., `"America/Chicago"`) |
| `cameras` | array | Yes | - | Array of camera configurations |
| `global` | object | No | (defaults) | Global settings |
| `queue` | object | No | (defaults) | Queue management settings |
| `sntp` | object | No | (defaults) | NTP time health settings |
| `web_console` | object | No | (defaults) | Web console settings |

### Camera Object

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | string | Yes | - | Unique ID (alphanumeric, hyphens) |
| `name` | string | Yes | - | Human-readable name |
| `type` | string | Yes | - | `"http"`, `"rtsp"`, or `"onvif"` |
| `enabled` | boolean | No | `true` | Enable/disable camera |
| `snapshot_url` | string | Cond. | - | HTTP snapshot URL (if type=http) |
| `auth` | object | No | - | HTTP authentication |
| `rtsp` | object | Cond. | - | RTSP settings (if type=rtsp) |
| `onvif` | object | Cond. | - | ONVIF settings (if type=onvif) |
| `capture_interval_seconds` | integer | No | `60` | Capture interval (1-1800) |
| `remote_path` | string | No | `"."` | Remote directory for uploads. Default uploads directly to base_path |
| `image` | object | No | - | Image processing options |
| `upload` | object | Yes | - | Per-camera upload credentials (SFTP/FTPS) |
| `queue` | object | No | - | Per-camera queue overrides |

### Camera Auth Object

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | `"basic"`, `"digest"`, or `"bearer"` |
| `username` | string | Cond. | Username (basic/digest) |
| `password` | string | Cond. | Password (basic/digest) |
| `token` | string | Cond. | Token (bearer) |

### Camera RTSP Object

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | Yes | - | RTSP stream URL |
| `username` | string | No | - | RTSP username |
| `password` | string | No | - | RTSP password |
| `substream` | boolean | No | `false` | Use substream (lower bandwidth) |

### Camera ONVIF Object

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `endpoint` | string | Yes | - | ONVIF device service URL |
| `username` | string | Yes | - | ONVIF username |
| `password` | string | Yes | - | ONVIF password |
| `profile_token` | string | No | (auto) | Media profile token |

### Camera Image Object

Controls optional image resizing/quality for bandwidth management.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `max_width` | integer | No | `0` | Max width in pixels (0=original) |
| `max_height` | integer | No | `0` | Max height in pixels (0=original) |
| `quality` | integer | No | `0` | JPEG quality 1-100 (0=original) |

**Default behavior**: No processing - original image uploaded as-is.

**Presets**:
- Original: `{}` (no processing)
- High: `{"max_width": 1920, "max_height": 1080, "quality": 85}`
- Medium: `{"max_width": 1280, "max_height": 720, "quality": 80}`
- Low: `{"max_width": 854, "max_height": 480, "quality": 70}`

### Camera Upload Object

Each camera has its own upload credentials. Supports both SFTP (recommended) and FTPS (legacy) protocols.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `protocol` | string | No | `"sftp"` | Upload protocol: `"sftp"` (recommended) or `"ftps"` (legacy) |
| `host` | string | Yes | - | Upload server hostname |
| `port` | integer | No | (auto) | Server port (22/2222 for SFTP, 2121 for FTPS) |
| `username` | string | Yes | - | Upload username |
| `password` | string | Yes | - | Upload password |
| `base_path` | string | No | `"/files"` | SFTP only - Base directory for uploads (for chroot environments) |
| `tls` | boolean | No | `true` | Enable FTPS (FTPS only, ignored for SFTP) |
| `tls_verify` | boolean | No | `true` | Verify TLS certificate (FTPS only) |
| `disable_epsv` | boolean | No | `true` | Use PASV instead of EPSV (FTPS only) |
| `timeout_connect_seconds` | integer | No | `60` | Connection timeout |
| `timeout_upload_seconds` | integer | No | `300` | Upload timeout (5 minutes) |

#### Protocol Comparison

| Feature | SFTP (Recommended) | FTPS (Legacy) |
|---------|-------------------|---------------|
| **Port** | 22 or 2222 (single) | 21 or 2121 + dynamic data ports |
| **Encryption** | SSH | TLS |
| **NAT Friendly** | ✅ Yes | ⚠️ Requires PASV mode |
| **Firewall Rules** | ✅ Simple (single port) | ❌ Complex (dynamic ports) |
| **Reliability** | ✅ Excellent | ⚠️ Moderate |
| **Connection Drops** | ✅ Rare (SSH keep-alive) | ⚠️ Common (TCP issues) |

#### Example Configurations

**SFTP (Recommended):**
```json
{
  "remote_path": ".",
  "upload": {
    "protocol": "sftp",
    "host": "upload.aviationwx.org",
    "port": 2222,
    "username": "your-username",
    "password": "your-password",
    "base_path": "/files"
  }
}
```

**Note:** For chroot environments, set `base_path` to the writable directory within the chroot (e.g., `/files`). Set `remote_path` to `"."` to upload directly to that directory without a camera subdirectory.

**FTPS (Legacy):**
```json
{
  "upload": {
    "protocol": "ftps",
    "host": "upload.aviationwx.org",
    "port": 2121,
    "username": "your-username",
    "password": "your-password",
    "tls": true,
    "tls_verify": true,
    "disable_epsv": true
  }
}
```

**Note**: Default host is `upload.aviationwx.org`. Contact [contact@aviationwx.org](mailto:contact@aviationwx.org) for credentials.

### Global Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `capture_timeout_seconds` | integer | `30` | HTTP/ONVIF timeout |
| `rtsp_timeout_seconds` | integer | `10` | RTSP frame timeout |
| `backoff` | object | (below) | Backoff settings |
| `degraded_mode` | object | (below) | Degraded mode settings |
| `time_authority` | object | (below) | Time validation settings |

### Backoff Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `initial_seconds` | integer | `5` | Initial backoff delay |
| `max_seconds` | integer | `300` | Maximum backoff (5 min) |
| `multiplier` | float | `2.0` | Exponential multiplier |
| `jitter` | boolean | `true` | Add random jitter |

### Degraded Mode Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable degraded mode |
| `failure_threshold` | integer | `3` | Failures before degraded |
| `concurrency_limit` | integer | `1` | Max concurrent ops |
| `slow_interval_multiplier` | float | `2.0` | Interval multiplier |

### Time Authority Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `camera_tolerance_seconds` | integer | `5` | Accept camera time within this |
| `camera_warn_drift_seconds` | integer | `30` | Warn if drift exceeds this |
| `camera_reject_drift_seconds` | integer | `300` | Reject camera time beyond this |

### Queue Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_path` | string | `/dev/shm/aviationwx` | Queue storage path |
| `max_total_size_mb` | integer | `100` | Max queue size (all cameras) |
| `max_heap_mb` | integer | `400` | Max Go heap size |
| `defaults` | object | (below) | Default per-camera settings |

### Queue Defaults Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_files` | integer | `100` | Max files per camera |
| `max_size_mb` | integer | `50` | Max size per camera |
| `max_age_seconds` | integer | `3600` | Max file age (1 hour) |

### SNTP Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable NTP health checks |
| `servers` | array | `["pool.ntp.org", "time.google.com"]` | NTP servers |
| `check_interval_seconds` | integer | `300` | Check interval (5 min) |
| `max_offset_seconds` | integer | `5` | Max acceptable offset |
| `timeout_seconds` | integer | `5` | Query timeout |

### Web Console Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable web console |
| `port` | integer | `1229` | Web console port |
| `password` | string | `"aviationwx"` | Login password |

## Complete Example

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
      "snapshot_url": "http://192.168.1.100/cgi-bin/snapshot.cgi",
      "auth": {
        "type": "basic",
        "username": "admin",
        "password": "camera_password"
      },
      "capture_interval_seconds": 60,
      "upload": {
        "host": "upload.aviationwx.org",
        "port": 21,
        "username": "kord-west-user",
        "password": "kord-west-pass",
        "tls": true
      }
    },
    {
      "id": "kord-east",
      "name": "KORD East Runway",
      "type": "rtsp",
      "enabled": true,
      "rtsp": {
        "url": "rtsp://192.168.1.101:554/stream1",
        "username": "admin",
        "password": "camera_password"
      },
      "capture_interval_seconds": 60,
      "image": {
        "max_width": 1280,
        "max_height": 720,
        "quality": 80
      },
      "upload": {
        "host": "upload.aviationwx.org",
        "username": "kord-east-user",
        "password": "kord-east-pass",
        "tls": true
      }
    }
  ],
  "global": {
    "capture_timeout_seconds": 30,
    "rtsp_timeout_seconds": 10,
    "backoff": {
      "initial_seconds": 5,
      "max_seconds": 300,
      "multiplier": 2.0,
      "jitter": true
    },
    "degraded_mode": {
      "enabled": true,
      "failure_threshold": 3,
      "concurrency_limit": 1,
      "slow_interval_multiplier": 2.0
    },
    "time_authority": {
      "camera_tolerance_seconds": 5,
      "camera_warn_drift_seconds": 30,
      "camera_reject_drift_seconds": 300
    }
  },
  "queue": {
    "base_path": "/dev/shm/aviationwx",
    "max_total_size_mb": 100,
    "max_heap_mb": 400,
    "defaults": {
      "max_files": 100,
      "max_size_mb": 50,
      "max_age_seconds": 3600
    }
  },
  "sntp": {
    "enabled": true,
    "servers": ["pool.ntp.org", "time.google.com"],
    "check_interval_seconds": 300,
    "max_offset_seconds": 5,
    "timeout_seconds": 5
  },
  "web_console": {
    "enabled": true,
    "port": 1229,
    "password": "your-secure-password"
  }
}
```

## Validation Rules

1. **version**: Must be `2`
2. **cameras**: At least one camera required
3. **camera.id**: Unique, alphanumeric + hyphens, no spaces
4. **camera.type**: Must be `"http"`, `"rtsp"`, or `"onvif"`
5. **camera.upload**: Required for each camera
6. **capture_interval_seconds**: 1-1800 seconds
7. **image.quality**: 1-100 if specified

## Environment Variables

Config values can be overridden via environment:

| Variable | Description |
|----------|-------------|
| `AVIATIONWX_CONFIG` | Config file path |
| `AVIATIONWX_QUEUE_PATH` | Queue storage path |
| `LOG_LEVEL` | Log level (debug, info, warn, error) |
| `LOG_FORMAT` | Log format (text, json) |

## Migration from v1

Key changes from config version 1:

1. `version` → `2`
2. `interval_seconds` → `capture_interval_seconds`
3. Global `upload` → per-camera `upload` in each camera object
4. `web_console.basic_auth` → `web_console.password`
5. `remote_path` behavior changed:
   - If empty/omitted: uploads to `{base_path}/filename.jpg` (v2.2.5+)
   - If set to custom path: uploads to `{base_path}/{remote_path}/filename.jpg`
   - Note: Prior to v2.2.5, empty `remote_path` defaulted to camera ID
