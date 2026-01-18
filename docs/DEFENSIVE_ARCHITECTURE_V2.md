# Defensive Architecture Implementation - v2.0

Implementation of the watchdog-based auto-recovery architecture designed for minimal maintenance deployment on resource-constrained devices (Raspberry Pi Zero 2 W).

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│ HOST LEVEL                                                      │
│                                                                 │
│  ┌───────────────┐      ┌─────────────────┐                   │
│  │   Systemd     │      │   Systemd       │                   │
│  │   Timers      │      │   Services      │                   │
│  ├───────────────┤      ├─────────────────┤                   │
│  │ • Watchdog    │      │ • Boot Update   │                   │
│  │   (1 min)     │◄────►│ • Container     │                   │
│  │               │      │   Start         │                   │
│  │ • Daily       │      │ • Daily Update  │                   │
│  │   Update      │      │ • Watchdog      │                   │
│  └───────┬───────┘      └────────┬────────┘                   │
│          │                       │                             │
│          ▼                       ▼                             │
│  ┌───────────────────────────────────────┐                    │
│  │      Host Scripts (Self-Updating)     │                    │
│  ├───────────────────────────────────────┤                    │
│  │ • aviationwx-supervisor.sh (v2.0)     │                    │
│  │ • aviationwx-watchdog.sh (v2.0)       │                    │
│  │ • aviationwx-container-start.sh       │                    │
│  │ • aviationwx (CLI)                    │                    │
│  │ • aviationwx-recovery.sh              │                    │
│  └───────────────┬───────────────────────┘                    │
│                  │                                             │
│                  ▼                                             │
│  ┌──────────────────────────────────────────────┐             │
│  │ Host Health Checks                           │             │
│  ├──────────────────────────────────────────────┤             │
│  │ • NTP sync                  (15m → restart)  │             │
│  │ • Network connectivity      (10m → restart)  │             │
│  │ • Docker daemon             (15m → restart)  │             │
│  │ • Temperature monitoring    (log only)       │             │
│  │ • Disk space                (cleanup)        │             │
│  │ • ESCALATION: 2+ failures   (25m → reboot)   │             │
│  │ • Reboot cooldown           (24 hours)       │             │
│  └──────────────────────────────────────────────┘             │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ CONTAINER LEVEL (Docker)                                        │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ Docker Health Check (30s interval)                       │  │
│  │ GET /healthz → 200 = healthy                             │  │
│  └──────────────────────────────────────────────────────────┘  │
│                              │                                  │
│                              ▼                                  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ AviationWX Bridge Container                              │  │
│  ├──────────────────────────────────────────────────────────┤  │
│  │ • --restart=no (watchdog manages)                        │  │
│  │ • Memory limit via Go runtime (350MB)                    │  │
│  │ • GOMAXPROCS = numCPU - 1                                │  │
│  │ • Panic recovery with logging                            │  │
│  │ • Resource limiter (adaptive throttling)                 │  │
│  │ • Queue manager (emergency thinning)                     │  │
│  │ • /healthz endpoint (detailed status)                    │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ PERSISTENT STORAGE (/data/aviationwx)                          │
│                                                                 │
│ • global.json              - Global config + update channel    │
│ • cameras/*.json           - Per-camera configs                │
│ • last-known-good.txt      - Last stable version               │
│ • host-version.txt         - Host scripts version              │
│ • watchdog-state.json      - Watchdog failure counters         │
│ • supervisor.log           - Update history                    │
│ • watchdog.log             - Host health events                │
│ • last-reboot-reason.json  - Why host rebooted                 │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│ EPHEMERAL STORAGE (/dev/shm - tmpfs)                           │
│                                                                 │
│ • Image queue (RAM-based, fail-forward recovery)               │
│ • Default: 200MB                                                │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Key Design Principles

### 1. Fail Forward
- **No automatic downgrading**
- Always move to latest stable release
- Bad updates trigger rollback to `last-known-good.txt`
- Ephemeral data (queue) can be lost on failure

### 2. Progressive Escalation
- 30-minute recovery target
- Escalating aggression: restart service → restart Docker → reboot host
- 24-hour reboot cooldown prevents boot loops

### 3. Separation of Concerns
- **Host level**: NTP, network, Docker daemon health
- **Container level**: Application health, memory, goroutines
- Watchdog monitors host, Docker monitors container

### 4. Self-Updating Host Scripts
- Supervisor checks for host script updates
- If `min_host_version` in release metadata > current, host scripts update first
- Scripts download from GitHub, validate, backup old versions, then replace

### 5. Single Last-Known-Good
- One source of truth: `/data/aviationwx/last-known-good.txt`
- Updated only after successful health check post-update
- Used for rollback on failed updates

### 6. GitHub-Driven Health/Deprecation
- Release metadata includes `deprecates` array for known-bad versions
- `min_host_version` ensures host can support new features
- `edge_stable_commit` for edge channel stability tracking

## Recovery Timeline

| Time    | Event                                    |
|---------|------------------------------------------|
| 0m      | Failure detected (NTP/Network/Docker)    |
| 1-9m    | Repeated checks, incrementing counters   |
| 10m     | Network restart (if network failing)     |
| 15m     | NTP service restart (if NTP failing)     |
| 15m     | Docker daemon restart (if Docker failing)|
| 25m     | Reboot (if 2+ systems still failing)     |
| +24h    | Reboot cooldown expires                  |

## Update Flow

### Boot-Time Update
```
1. System boots
2. aviationwx-boot-update.service runs
   ├─ Check if host scripts need update (min_host_version)
   │  ├─ If yes: download, validate, install, re-exec
   │  └─ If no: continue
   ├─ Wait for network (30s timeout)
   ├─ Fetch latest release from GitHub API
   ├─ Check channel (latest or edge)
   ├─ Determine target version
   ├─ Check if current == target
   │  ├─ If same: start with current
   │  └─ If different: pull new image
   ├─ Pull and validate new image
   │  ├─ Success: update last-known-good.txt
   │  └─ Fail: rollback to last-known-good.txt
3. aviationwx-container.service starts container
4. Docker health checks begin (30s interval)
5. Watchdog begins monitoring (1min interval)
```

### Daily Update Check
```
1. aviationwx-daily-update.timer triggers (daily, randomized)
2. aviationwx-supervisor.sh daily-update
   ├─ Same flow as boot-time update
   ├─ BUT: respects MIN_RELEASE_AGE_HOURS (default: 2)
   └─ Skip if watchdog-triggered boot (avoid crash loops)
```

### Force Update (User-Initiated)
```
sudo aviationwx update
  ├─ Clears release cache
  ├─ Ignores release age check
  └─ Immediately pulls and applies latest
```

## Edge vs Latest Channels

### Latest Channel (Default)
- Pulls from latest stable GitHub release
- Skips pre-releases (`prerelease: false`)
- Respects `MIN_RELEASE_AGE_HOURS`
- Safe for production

### Edge Channel
- Pulls from `edge` tag (or latest pre-release)
- No age restrictions
- Experimental features, potentially unstable
- Automatic failback if unstable (planned, not yet implemented)

To switch channels, edit `/data/aviationwx/global.json`:
```json
{
  "update_channel": "edge"
}
```

## User Interface

### CLI: `aviationwx`
```bash
aviationwx status              # Show system status
aviationwx logs [service] [N]  # Show logs (bridge, supervisor, watchdog)
aviationwx restart             # Restart container (requires root)
aviationwx update              # Force update check (requires root)
aviationwx recovery            # Emergency recovery tool (requires root)
```

### Recovery Tool: `aviationwx-recovery.sh`
Interactive menu for:
1. View Status & Logs
2. Restart Container
3. Force Update (latest)
4. Rollback to Last Known Good
5. Switch Update Channel
6. Reset Watchdog State
7. Run Diagnostics
8. Factory Reset (DANGER)

## Host Scripts

### `aviationwx-supervisor.sh` (v2.0)
**Purpose:** Manage updates for host scripts and container  
**Features:**
- Self-updating (checks `min_host_version` in releases)
- GitHub API integration with retry and caching
- Pull and validate container images
- Wait for health before marking as successful
- Rollback on failure
- Dry-run mode for testing

**Commands:**
- `boot-update` - Run at boot time
- `daily-update` - Run daily (same as boot-update)
- `force-update` - Ignore caches and age checks
- `status` - Show current state

### `aviationwx-watchdog.sh` (v2.0)
**Purpose:** Monitor host health and trigger recovery  
**Features:**
- NTP synchronization check
- Network connectivity check (multiple targets)
- Docker daemon health check
- Temperature monitoring (Pi-specific)
- Disk space monitoring with automatic cleanup
- Progressive escalation with named constants
- 24-hour reboot cooldown
- Stateful (tracks failure counts across runs)

**State File:** `/data/aviationwx/watchdog-state.json`
```json
{
  "ntp_failures": 0,
  "network_failures": 0,
  "docker_failures": 0,
  "last_reboot": "2026-01-17T12:34:56+00:00",
  "last_check": "2026-01-18T08:22:15+00:00"
}
```

### `aviationwx-container-start.sh`
**Purpose:** Start container with correct version  
**Features:**
- Reads version from `last-known-good.txt`
- Sets Docker health check config
- Sets `--restart=no` (watchdog manages restarts)

### `aviationwx` (CLI)
**Purpose:** User-facing command-line interface  
**Features:**
- Show status, logs, watchdog state
- Trigger restarts and updates
- Launch recovery tool
- No-root operations for read-only commands

### `aviationwx-recovery.sh`
**Purpose:** Emergency recovery and diagnostics  
**Features:**
- Interactive menu system
- Rollback capability
- Channel switching
- Diagnostics (network, NTP, Docker, disk, memory, temperature)
- Factory reset with confirmation

## Systemd Integration

### Services
- `aviationwx-boot-update.service` - Runs once at boot, before container
- `aviationwx-container.service` - Starts container (oneshot, remains after exit)
- `aviationwx-daily-update.service` - Runs daily update check
- `aviationwx-watchdog.service` - Runs watchdog check

### Timers
- `aviationwx-daily-update.timer` - Daily, randomized 30min
- `aviationwx-watchdog.timer` - Every 1 minute, starts 2min after boot

### Dependencies
```
boot:
  ├─ docker.service
  ├─ network-online.target
  ├─ aviationwx-boot-update.service
  └─ aviationwx-container.service (BindsTo boot-update)

timers:
  ├─ aviationwx-daily-update.timer
  └─ aviationwx-watchdog.timer
```

## Installation & Migration

### Fresh Install
```bash
curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
```

### Upgrade from v1.x
Same command. The installer detects existing installations and:
1. Installs new host scripts
2. Creates systemd services
3. Removes old cron job
4. Updates container with new restart policy
5. Preserves all configuration

See `docs/MIGRATION_V2.md` for detailed migration guide.

## Testing

### Dry-Run Mode
Set environment variable to test without executing actions:
```bash
export AVIATIONWX_DRY_RUN=true
sudo -E /usr/local/bin/aviationwx-supervisor.sh boot-update
sudo -E /usr/local/bin/aviationwx-watchdog.sh
```

All actions will be logged but not executed.

### Chaos Testing (TODO)
- Simulate network failure
- Simulate NTP desync
- Simulate Docker daemon hang
- Simulate container crash
- Verify recovery timeline

## Monitoring

### Logs
```bash
# Supervisor (updates)
tail -f /data/aviationwx/supervisor.log
journalctl -u aviationwx-boot-update.service -f

# Watchdog (host health)
tail -f /data/aviationwx/watchdog.log
journalctl -u aviationwx-watchdog.service -f

# Container (application)
docker logs -f aviationwx-bridge
journalctl -u docker.service -f | grep aviationwx-bridge
```

### Status Check
```bash
sudo aviationwx status
```

Shows:
- Host scripts version
- Container status and health
- Current image version
- Update channel
- Last known good version
- Watchdog failure counters

## Future Enhancements

### Not Yet Implemented
1. **Edge Failback Logic** - Automatic switch from edge to latest if unstable
2. **Chaos Testing Suite** - Automated failure injection tests
3. **Image Verification** - Docker Content Trust or digest validation
4. **Central Logging** - Optional metrics/logs upload to central service
5. **Web UI Integration** - Expose watchdog status and recovery options in web console

### Deferred
- **Config Migration Service** - Automatic schema evolution (current: fail-forward)
- **Multi-Region Updates** - Staggered rollout based on geography
- **A/B Testing** - Canary deployments for edge users

## Persistence Contract

### Configuration Files
- **Global config**: Additive changes only, old keys remain valid
- **Camera configs**: Per-camera files, versioned format
- **Breaking changes**: Must include migration logic or `min_host_version` bump

### Ephemeral Data
- **Image queue**: RAM-based (tmpfs), can be lost on failure
- **Fail-forward**: System doesn't try to preserve queue state across crashes

### State Files
- **Watchdog state**: Versioned JSON, backward compatible
- **Last-known-good**: Simple text file, always latest stable version

## Security Considerations

### Script Downloads
- All scripts downloaded via HTTPS from GitHub
- Validated for:
  - Non-empty files
  - Correct shebang
  - Basic syntax check (future: signature verification)
- Backup created before replacement

### Docker Images
- Pulled from GHCR (GitHub Container Registry)
- Future: Digest pinning and verification
- Current: Tag-based (`:latest`, `:edge`, `:v2.0.0`)

### Credentials
- Stored in per-camera JSON files
- File permissions: `1000:1000` (bridge user)
- Not exposed in logs or status output

### Reboots
- Require 24-hour cooldown to prevent boot loops
- Require 2+ critical system failures (not single failure)
- Log reboot reason for post-mortem

## Resource Constraints

Target device: **Raspberry Pi Zero 2 W (512MB RAM)**

### Memory Budget
- OS + system services: ~150MB
- Docker daemon: ~50MB
- AviationWX Bridge: ~350MB (Go runtime limit)
- Image queue (tmpfs): 200MB (configurable)
- **Total: ~750MB** (exceeds physical RAM, relies on some swap/compression)

### CPU Budget
- 4 cores (ARM Cortex-A53)
- Application: 3 cores (`GOMAXPROCS = numCPU - 1`)
- Leaves 1 core for system and Docker

### Disk Budget
- Docker images: ~100-200MB
- Configuration: <1MB
- Logs: Rotated, capped at 100 lines per file
- Watchdog cleans old backups and failure logs

## Summary

The v2.0 defensive architecture provides:

✅ **No-touch operation** - Minimal maintenance, auto-recovery  
✅ **Progressive escalation** - 30-minute recovery target with increasing aggression  
✅ **Fail-forward** - Always move to latest stable, no downgrades  
✅ **Separation of concerns** - Host vs container health monitoring  
✅ **Self-updating** - Host scripts and container both auto-update  
✅ **User-friendly** - Simple CLI, interactive recovery tool  
✅ **Boot-loop protection** - 24-hour reboot cooldown  
✅ **Resource-aware** - Designed for 512MB RAM devices  
✅ **Testable** - Dry-run mode for safe testing  

This architecture balances **reliability** (watchdog, auto-recovery) with **stability** (fail-forward, cooldowns) while respecting **resource constraints** (Pi Zero 2 W).
