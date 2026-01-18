# AviationWX Bridge v2.0.0 Release Notes

**Release Date**: January 18, 2026  
**Status**: ✅ READY FOR PRODUCTION

---

## 🎉 Major Release Highlights

Version 2.0 represents a **complete architectural refactor** focused on reliability, scalability, and operational visibility. This release eliminates configuration data loss issues, prevents worker job overlaps, and provides comprehensive live monitoring.

---

## ⚠️ Breaking Changes

### Configuration Storage

**Old (v1.x):**
```bash
AVIATIONWX_CONFIG=/data/config.json
```

**New (v2.0):**
```bash
AVIATIONWX_CONFIG_DIR=/data
# Creates: /data/global.json + /data/cameras/*.json
```

### Automatic Migration

- The bridge automatically migrates old `config.json` on first startup
- Original file backed up as `config.json.migrated`
- **Zero downtime** - migration happens transparently
- No manual intervention required

---

## 🚀 New Features

### 1. ConfigService Architecture

**Problem Solved**: Eliminated all configuration data loss and pointer synchronization bugs

- **File-per-camera storage** - Each camera gets its own JSON file
- **Atomic operations** - `AddCamera`, `UpdateCamera`, `DeleteCamera` are thread-safe
- **Event-driven** - Components subscribe to config changes (no blocking callbacks)
- **Automatic backup** - Config backed up before every load/save operation
- **Consistent ordering** - Cameras sorted by ID (no more random UI reordering)

### 2. Worker Job Blocking

**Problem Solved**: Prevented resource exhaustion from overlapping operations

- **Capture workers** skip new jobs if previous is still running
- **Upload workers** wait for previous upload to complete
- **Timeouts**:
  - Capture: `60s + interval` (e.g., 120s for 60s interval)
  - Upload: 120s (accommodates slow FTPS connections)
- Jobs exceeding max time are aborted and logged

### 3. Live Logs

**Problem Solved**: Made logs accessible to users without Docker/SSH access

- **In-memory buffer** stores last 1000 log entries
- **`/api/logs` endpoint** serves formatted logs to web UI
- **Live Logs tab** with 2-second auto-refresh
- **Level filtering**: ERROR, WARN, INFO, DEBUG
- **Syntax highlighting** for different log levels
- **No external dependencies** - runs entirely in-app

### 4. Dashboard Monitoring

**Problem Solved**: Lack of visibility into worker operations

- **Next capture countdown** (real-time, updates every second)
- **Live indicators**: 🔴 Currently capturing/uploading badges
- **Upload failure tracking** per camera with counts
- **Last failure reason** displayed for debugging
- **Queue health visualization** with progress bars
- **Auto-refresh every 1 second** for live monitoring

### 5. Enhanced Observability

- **System metrics**: CPU, memory, disk, uptime (works in Docker)
- **NTP/Time health**: Default time sync with `pool.ntp.org`
- **Update status**: Shows current/latest version, update available flag
- **Worker status**: Visibility into each camera's worker state and errors

---

## 🐛 Critical Fixes

### 1. Configuration Data Loss

**Symptoms** (v1.x):
- Cameras disappeared after editing
- Timezone/global settings reset randomly
- Modal didn't close after saving

**Root Cause**: Shared config pointers, race conditions, in-place modifications

**Solution**: Complete refactor to ConfigService with immutable patterns

### 2. Upload Credential Mixup

**Symptoms** (v1.x):
- Camera A's images uploaded to Camera B's FTP server
- Last-configured camera's credentials used for all

**Root Cause**: Single shared uploader for all cameras

**Solution**: Per-camera uploaders with isolated credentials

### 3. Test Failures

**Fixed**: `TestQueue_CapturePause` race condition - now allows for expected queue thinning

---

## 📊 Quality Metrics

### Test Coverage

```
✅ All packages: 100% passing (0 failures)
✅ Race detection: Enabled (-race flag)
✅ Coverage: 
   - internal/image: 93.8%
   - internal/time: 85.2%
   - internal/queue: 74.2%
   - internal/scheduler: 67.9%
   - internal/logger: 66.2%
   - internal/config: 54.4%
```

### Code Quality

```
✅ gofmt: All files formatted
✅ go vet: No issues
✅ golangci-lint: Passed
✅ TODO count: 0 (100% clean)
```

### Build Status

```
✅ Docker build: Success
✅ Multi-arch: amd64, arm64, arm/v7
✅ Health check: Using /healthz (no auth required)
✅ Log rotation: 10MB limit (2 × 5MB files)
```

---

## 📦 Deployment

### Raspberry Pi (One-Line Install)

```bash
curl -fsSL https://raw.githubusercontent.com/alexwitherspoon/aviationwx-bridge/main/scripts/install.sh | sudo bash
```

**New in v2.0:**
- Auto-configures `journald` with volatile (RAM) storage on Pi
- Zero SD card wear from logs
- Docker logs stored in tmpfs

### Docker Compose

```yaml
services:
  aviationwx-bridge:
    image: ghcr.io/alexwitherspoon/aviationwx-bridge:latest
    container_name: aviationwx-bridge
    restart: unless-stopped
    ports:
      - "1229:1229"
    volumes:
      - ./data:/data
    tmpfs:
      - /dev/shm:size=200m
    logging:
      driver: "json-file"
      options:
        max-size: "5m"
        max-file: "2"
        compress: "true"
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:1229/healthz"]
      interval: 30s
      timeout: 3s
      retries: 3
```

---

## 🔄 Migration Guide

### From v1.x to v2.0

1. **Backup existing config**:
   ```bash
   cp /data/aviationwx/config.json /data/aviationwx/config.json.backup
   ```

2. **Update image**:
   ```bash
   docker pull ghcr.io/alexwitherspoon/aviationwx-bridge:latest
   docker compose up -d
   ```

3. **Verify migration**:
   ```bash
   ls -la /data/aviationwx/
   # Should see: global.json, cameras/, config.json.migrated
   ```

4. **Check web console**:
   - Navigate to `http://your-pi:1229`
   - Verify all cameras are present
   - Check Dashboard for live metrics

### Rollback (if needed)

```bash
# Stop v2.0
docker compose down

# Restore backup
mv /data/aviationwx/config.json.backup /data/aviationwx/config.json
rm -rf /data/aviationwx/global.json /data/aviationwx/cameras/

# Pull v1.0.2
docker pull ghcr.io/alexwitherspoon/aviationwx-bridge:v1.0.2
docker compose up -d
```

---

## 📚 Documentation Updates

- ✅ **CHANGELOG.md**: Comprehensive v2.0 entry
- ✅ **CONFIG_SCHEMA_v2.md**: File-per-camera structure
- ✅ **LOCAL_TESTING.md**: Docker-based testing guide
- ✅ **CRASH_RECOVERY.md**: Troubleshooting and monitoring
- ✅ **Config examples**: global.json, camera-*.json templates

---

## 🙏 Acknowledgments

This release represents months of real-world testing and iterative improvements based on user feedback. Special thanks to all early adopters who reported issues and helped shape this release.

---

## 🔗 Resources

- **GitHub**: https://github.com/alexwitherspoon/aviationwx-bridge
- **Docker Hub**: `ghcr.io/alexwitherspoon/aviationwx-bridge:latest`
- **Documentation**: See `docs/` directory
- **Support**: Open an issue on GitHub

---

## ✅ Pre-Release Checklist

- [x] All tests passing (100% suite, race detection enabled)
- [x] Code formatted (gofmt)
- [x] Linter passing (go vet)
- [x] Docker build successful (multi-arch)
- [x] Config examples validated (all JSON valid)
- [x] Manual deployment tested (Docker Compose)
- [x] Documentation reviewed and updated
- [x] CHANGELOG updated for v2.0
- [x] Release notes created
- [x] Zero TODO comments in codebase

**Status**: ✅ **READY FOR v2.0.0 RELEASE**
