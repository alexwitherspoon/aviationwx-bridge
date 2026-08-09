# Configuration Schema

**Config Version**: 2  
**Format**: JSON  
**Location**: Config directory (`AVIATIONWX_CONFIG_DIR`, default `/data` in the container):

- `global.json` - bridge-wide settings
- `cameras/*.json` - one file per camera
- `stations/*.json` - one file per weather station (optional; epic #102)

Legacy single-file config: `AVIATIONWX_CONFIG` (default `/data/config.json`). New installs use the directory layout above.

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
| `update_channel` | string | No | `"latest"` | Supervisor update channel: `"latest"` or `"edge"` |
| `max_concurrent_uploads` | integer | No | `2` | Parallel SFTP uploads (also mirrored under `global`) |
| `max_concurrent_captures` | integer | No | (profile) | Parallel capture jobs (also mirrored under `global`); if omitted, derived from RAM (~500 MB per slot, max 10) and, when max CPU frequency &lt; 2 GHz, capped to about one concurrent capture per two logical CPUs; if RAM cannot be detected, default **1**. An explicit value (1–10) is used as-is and is not adjusted by profiling |
| `cameras` | array | Yes | - | Array of camera configurations |
| `global` | object | No | (defaults) | Global settings |
| `queue` | object | No | (defaults) | Queue management settings |
| `sntp` | object | No | (defaults) | NTP time health settings |
| `web_console` | object | No | (defaults) | Web console settings |
| `api` | object | No | (omitted) | Optional HTTPS link to `api.aviationwx.org` (fleet health + weather push). Cameras/SFTP work without this |

### API Object (`global.json` → `api`)

Optional. When omitted or `enabled` is false, the bridge does not call the AviationWX HTTPS API.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | boolean | Yes | - | When true, bootstrap/health/weather HTTPS client runs |
| `key` | string | Cond. | - | Bridge API key (`awxb_` + 48 alphanumeric). Required when enabled. Issued by aviationwx.org `scripts/generate-bridge-api-key.php` - not created in the console. Contract: OpenAPI `/v1/bridge/*` (aviationwx PR #277) |
| `base_url` | string | No | `https://api.aviationwx.org` | Advanced override (must be `https://`). Production default is hard-coded when omitted |

### Station files (`stations/<id>.json`)

Optional weather stations. One JSON file per station. Inventory is advertised in HTTPS health heartbeats when `api` is configured.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | string | Yes | - | Unique ID (alphanumeric, hyphens) |
| `name` | string | Yes | - | Display name |
| `type` | string | Yes | - | `davis_weatherlink_live` or `http_interceptor` (more types in epic #103) |
| `enabled` | boolean | No | `true` | Enable/disable station |
| `host` | string | Cond. | - | Davis: IP (IPv4 or IPv6) or hostname of WeatherLink Live. IPv6 may be bare or bracketed; with a non-default port use `[addr]:port`. Discover scans IPv4 CIDRs only. |
| `poll_interval_seconds` | integer | No | `10` | Davis HTTP floor is 10s; must be >= 10 for Davis |
| `txid` | integer | No | - | Davis transmitter id; installer always picks after Test poll |
| `listen_addr` | string | Cond. | `0.0.0.0:8090` | Interceptor: bind address for WU-style ingest (`host:port`) |
| `listen_path` | string | Cond. | `/weatherstation/updateweatherstation.php` | Interceptor: exact URL path devices POST/GET to |
| `dialect` | string | Cond. | `wunderground` | Interceptor: payload dialect (`wunderground` only in this release) |

Runtime (epic #102 / #103 / aviationwx#274):

- **Davis:** enabled stations with a `txid` are polled over LAN (HTTP `/v1/current_conditions`). Discover is operator-initiated only (SSE; CIDR /24-/30 + mDNS).
- **HTTP interceptor:** enabled stations share one listen server on `listen_addr`; routes by exact `listen_path`. All interceptor stations must use the same `listen_addr` (active bind is the lexicographically smallest station id when they differ); a mismatched address is not routed (status degraded). Devices push Weather Underground-compatible GET/POST fields. Discovery is not applicable (stations push to the bridge). Firewall the listen port on the LAN.
- Weather POST carries `provider`, `source_id`, and station-native detail in `provider_meta.raw` only - no bridge-normalized `sample`. Wind is always treated as true north. Core owns unit conversion and weather semantics.
- Missing station time skips weather POST (Davis missing `ts`; interceptor missing/unparsable `dateutc`) - no bridge-clock `observed_at`. Emit ceiling ≤1 Hz. Failed weather POSTs stay in an in-memory retry ring for up to **10 minutes** by station `observed_at` (FIFO; catch-up paced at ≤1 Hz per `source_id`, separate from live - worst case ~2 POSTs/s/station; far-future stamps beyond core skew are dropped); no disk weather queue. This is observation catch-up, not camera image replay - stale webcam frames still must never upload.
- Console: Test poll (Davis) / Test receive sample parse (interceptor; live ingest is last-receive in status). Local WLL + weather POST capture: `docker/wll-simulator/README.md` and golden wire sample `internal/bridgeapi/testdata/weather_post_davis_wll.example.json`.

Davis example:

```json
{
  "id": "station-scappoose-davis",
  "name": "Davis WLL",
  "type": "davis_weatherlink_live",
  "enabled": true,
  "host": "192.168.1.50",
  "poll_interval_seconds": 10,
  "txid": 1
}
```

Interceptor example:

```json
{
  "id": "station-wu-interceptor",
  "name": "WU Interceptor",
  "type": "http_interceptor",
  "enabled": true,
  "listen_addr": "0.0.0.0:8090",
  "listen_path": "/weatherstation/updateweatherstation.php",
  "dialect": "wunderground"
}
```

### Camera Object

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | string | Yes | - | Unique ID (alphanumeric, hyphens). Stored in JSON; when adding a camera via the web UI, the id is generated from the display name with automatic suffixing if needed |
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
| `upload` | object | Yes | - | Per-camera upload credentials (SFTP) |
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
| `endpoint` | string | Yes | - | ONVIF device service URL (e.g. `http://host/onvif/device_service`). The bridge and web API normalize the value: repeated `http://` / `https://` prefixes (common when pasting a full URL) are collapsed to a single scheme; if the value began with `https://`, that scheme is kept, otherwise `http://` is used. |
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

Each camera has its own upload credentials. SFTP only (protocol "ftps"/"ftp" in config are migrated to SFTP). Two cameras may not share the same SFTP identity (same `host`, `port`, and `username`); the bridge rejects duplicates when adding or updating cameras.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `protocol` | string | No | `"sftp"` | Upload protocol (SFTP only; "ftps"/"ftp" migrated to SFTP) |
| `host` | string | Yes | - | Upload server hostname |
| `port` | integer | No | `2222` | SFTP port (aviationwx.org uses 2222) |
| `username` | string | Yes | - | Upload username |
| `password` | string | Yes | - | Upload password |
| `base_path` | string | No | `"/files"` | Base directory for uploads (chroot environments) |
| `timeout_connect_seconds` | integer | No | `60` | Connection timeout |
| `timeout_upload_seconds` | integer | No | `300` | Upload timeout (5 minutes) |

#### Example Configuration

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

**Note:** For chroot environments, set `base_path` to the writable directory within the chroot (e.g., `/files`). Set `remote_path` to `"."` to upload directly to that directory without a camera subdirectory. Default host is `upload.aviationwx.org`. Contact [contact@aviationwx.org](mailto:contact@aviationwx.org) for credentials.

### Global Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_concurrent_uploads` | integer | `2` | Parallel SFTP uploads |
| `max_concurrent_captures` | integer | (profile) | Same as root; see root row for default formula |
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
| `stale_threshold_hours` | integer | `24` | Remain healthy this long after last good NTP sync while servers are unreachable; `0` uses default 24 |

### Web Console Object

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `true` | Enable web console |
| `port` | integer | `1229` | Web console port |
| `password` | string | `"aviationwx"` | Login password |

### Upload SSH host key files (not in JSON)

The bridge stores SFTP host key state under the config directory (`AVIATIONWX_CONFIG_DIR`, default `/data`):

| File | Purpose |
|------|---------|
| `ssh_known_hosts` | TOFU pin for each `upload.host:port` the bridge has connected to |
| `upload_ssh_trusted_keys.json` | Per upload target (`host:port` in `version` 2 `endpoints`): cached HTTPS roster SHA256 fingerprints (`source`: `https-roster`) |
| `upload_ssh_roster_sync.json` | Per upload target (`host`, `port`): `last_error`, `last_attempt_at`, `last_success_at` when a prior sync succeeded |

Primary trust is the HTTPS roster at `https://{upload.host}/.well-known/aviationwx-upload-ssh-host-keys.json`. The bridge fetches it on startup and hourly, and forces a refresh when the live server key does not match the pin or roster. When HTTPS is unreachable, the last cached roster for that upload target is used until it can be refreshed.

**Migration:** Bridges with a v1 `upload_ssh_trusted_keys.json` (top-level `sha256` only) migrate on read to `version` 2 under `upload.aviationwx.org:2222`. Multi-host bridges should use **Refresh SSH keys** for each upload target after upgrade so every host has its own cache entry.

### Web console and network exposure

The bridge targets a **private LAN** (for example a single-board computer at a field or home), not a public internet-facing service. Authenticated JSON APIs and unauthenticated health endpoints (`GET /healthz`, `GET /readyz`) assume **trusted local access**: operators on the same network, or host-side automation (for example watchdog scripts using `127.0.0.1`). They are **not** hardened for anonymous access from the internet. Do not port-forward the web console port or place the host on an untrusted network without firewall rules and a deliberate security review.

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
        "protocol": "sftp",
        "host": "upload.aviationwx.org",
        "port": 2222,
        "username": "kord-west-user",
        "password": "kord-west-pass",
        "base_path": "/files"
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
        "protocol": "sftp",
        "host": "upload.aviationwx.org",
        "port": 2222,
        "username": "kord-east-user",
        "password": "kord-east-pass",
        "base_path": "/files"
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
| `AVIATIONWX_CONFIG_DIR` | Config directory (`global.json`, `cameras/`) |
| `AVIATIONWX_CONFIG` | Legacy single-file config path (migration source) |
| `AVIATIONWX_DATA_DIR` | Mounted data volume root inside the container (default `/data`) |
| `AVIATIONWX_SELF_UPDATE` | When `1` or `true`, web UI `POST /api/update` writes the supervisor trigger file (supervised installs). Unset for IT-managed Docker. |
| `AVIATIONWX_TMPFS_SIZE` | Override the image-queue tmpfs size with a Docker size token (e.g. `200m`, `1g`). Supervised installs read it from the data-dir environment file and otherwise size tmpfs from available RAM; the Docker Compose path uses it for the `/dev/shm` mount. An invalid value is ignored. |
| `AVIATIONWX_QUEUE_PATH` | Queue storage path |
| `AVIATIONWX_UPLOAD_ROSTER_CA_FILE` | Optional PEM file with extra root CAs for HTTPS SFTP host key roster fetch. Unset in production. E2E harness sets this to trust `upload.e2e.test` TLS. |
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
