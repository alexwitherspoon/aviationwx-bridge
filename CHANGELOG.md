# Changelog

All notable changes to AviationWX.org Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Dependencies**: Bump `golang.org/x/crypto` to v0.52.0 (SSH security fixes), `golang.org/x/sys` to v0.45.0; GitHub Actions Docker and golangci-lint pins (see PR #42).
- **Docker**: Pin builder to `golang:1.26.3-alpine3.23` and runtime to `alpine:3.23.4` (was floating `1.26-alpine` / `3.23`).
- **Dependencies**: Bump `golang.org/x/net` to v0.55.0.

## [2.9.1] - 2026-05-22

### Removed
- **Repo**: Unused legacy host scripts `scripts/supervisor.sh` (v1 supervisor) and `scripts/daily-restart.sh` (pre-watchdog cron restart). Production uses `aviationwx-supervisor.sh` and capture-restart instead.

### Fixed
- **Updates**: Upgrade decisions resolve installed semver from the running binary (not Docker `:latest` alone); GitHub remains target source of truth. UI and status show local image tag / `last-known-good` from file; install and container-start pull resolved semver before start.
- **Web/API**: `/readyz` uses a lightweight capture snapshot (no global queue stats); `/api/status` and `/healthz` time out instead of hanging; status responses are coalesced (500ms) to survive UI poll storms.
- **Web UI**: Sends HTTP Basic Auth on API calls; dashboard refresh backs off on failures (5s–30s); re-prompt on 401 instead of sticking a wrong password in sessionStorage.
- **Queue**: Emergency thinning no longer holds the queue manager mutex (unblocks status/readyz during cleanup).
- **Docker**: Healthcheck probes `http://127.0.0.1:1229/readyz` (avoids `localhost` → IPv6 connect timeouts).
- **Host**: Supervisor bounds `docker exec` for version probes; container-start can use a cached image when pull fails; host status files read from volume root (`/data`).

### Changed
- **Host scripts**: Bump to v2.3 (`min_host_version` 2.3) so existing Pis pull updated supervisor/container-start before the container update.

## [2.9.0] - 2026-05-21

### Added
- **Self-recovery**: Resume capture when queue drains below resume threshold (not only after upload).
- **Self-recovery**: Resume capture after tmpfs/disk space recovers (filesystem and memory monitors).
- **Self-recovery**: Hourly SFTP connectivity probe per camera when that camera's queue is empty.
- **Self-recovery**: Retry camera worker start every 15 minutes; restart stale workers (not only missing).
- **Self-recovery**: Drop queued images after repeated read failures (poison pill).
- **Host install**: `aviationwx-capture-restart.timer` (every 5 min); watchdog invokes capture-restart.
- **Updates**: Supervisor enables `aviationwx-capture-restart.timer` on boot/daily update (existing Pis, no `install.sh`).
- **Health**: Docker and `/healthz` align with `/readyz` capture readiness.

### Changed
- **Health checks**: Container health uses `/readyz` instead of `/healthz` alone.
- **Upload**: Catch-up mode uses LIFO (`DequeueNext`) per camera when queue exceeds threshold.
- **Capture-restart**: Unreachable `/readyz` (curl 000) escalates to container restart; max-restart cap no longer clears the streak.
- **Watchdog**: Restarts **exited** bridge container; `unhealthy` (/readyz) is handled by capture-restart rate limits (not immediate restart).

### Fixed
- **Watchdog**: Detect unhealthy containers via `State.Health.Status` (not `State.Status`).
- **Health**: `/healthz` parses typed `getStatus()` (orchestrator struct, enabled camera count).
- **Bridge**: Stop camera retry loop on graceful shutdown.
- **Upload**: Clear per-path read-failure counts after success; drop redundant timeout channel drain.
- **Queue**: Remove duplicate `maybeResumeCapture` call from `MarkUploaded`.
- **Queue**: Resume capture once per memory monitor tick (not twice via filesystem check).
- **Upload**: Prune `fileReadFailures` when queue files are gone; log SFTP probes outside upload mutex.
- **Health**: `/healthz` sets `ntp_healthy` from orchestrator `TimeInfo` when time is unhealthy.
- **Upload**: Keep read-failure count until poisoned file drop succeeds.
- **Queue**: Resync queue counts from disk when drop/mark finds file already removed (avoid double decrement).
- **Upload**: Advance `probeCameraIndex` under write lock (fix RLock data race).
- **Bridge**: Stale camera worker restart uses `AVIATIONWX_READYZ_GRACE_SECONDS` and `AVIATIONWX_READYZ_STALE_SECONDS` (same as `/readyz`).
- **Host**: capture-restart uses `curl ... || code=000` so unreachable does not become `000000`.
- **Host**: Clamp capture-restart streak counters at threshold; write `recovery-exhausted.json` `since` in UTC.
- **Capture**: Immediate wake on `ResumeCapture` (not only when a pending capture exists).
- **Upload**: Upload timeout interrupts SFTP (`InterruptUpload`) so the next upload is not blocked.
- **Upload**: Serialize SFTP `Upload` per client so timeout interrupt closes the correct session under concurrent workers.
- **Upload**: Catch-up LIFO logging is per-camera (matches dequeue).
- **Updates**: Supervisor uses `aviationwx-container-start.sh` (dynamic resource limits) instead of a minimal inline `docker run`.
- **Host recovery**: `recovery-exhausted.json` when capture-restart cap is hit; exposed in `/api/status` and `/healthz`.

## [2.8.0] - 2026-04-15

### Changed
- **Dependencies**: Bump `golang.org/x/crypto` to v0.50.0 and `golang.org/x/sys` to v0.43.0.
- **CI / release automation**: Update GitHub Actions (`actions/setup-go` v6.4.0, `codecov/codecov-action` v6.0.0, Docker build/login/metadata/build-push, `actions/upload-artifact` v7.0.1, `softprops/action-gh-release` v3.0.0, and related workflow steps); pin workflow actions to immutable commit SHAs with version comments for supply-chain consistency.

## [2.7.1] - 2026-03-24

### Changed
- **Hardware**: Linux-only memory and CPU frequency helpers (`/proc`, sysfs) live in `captures_linux.go` with the `linux` build tag; shared parsers remain in `captures.go`.

### Removed
- **Web UI**: Unused `updateQueueStorageDisplay` stub (queue storage is handled by `updateSystemResourcesDisplay`).

## [2.7.0] - 2026-03-15

### Added
- **Tests**: Unit tests for NormalizeUploadConfig (FTPS/FTP migration, port inference, defaults)

### Fixed
- **Config examples**: SFTP-only alignment—port 2222, removed tls and ca_bundle from all examples
- **Config types**: Comment updated from "FTP credentials" to "SFTP credentials"
- **Server tests**: Test payloads updated to SFTP-only (port 2222, no tls)

## [2.6.0] - 2025-02-20

### Fixed
- **Lint**: Use switch on protocol for staticcheck QF1003 (NormalizeUploadConfig)

### Removed
- **FTPS support**: Deprecated and removed. SFTP only.
  - Config with `protocol: "ftps"` or `protocol: "ftp"` is migrated to SFTP (port 2222)
  - Removed `secsy/goftp` dependency
  - Simplified upload config (no TLS, TLSVerify, CABundlePath, DisableEPSV)
  - Web UI: protocol selector removed (SFTP only)

## [2.5.1] - 2026-02-21

### Fixed
- **Web UI**: Webcam preview no longer flashes every second; only updates when a new capture exists (last_capture_time changed)
- **Web UI**: Status text (countdown, badges) updates in place without rebuilding preview images
- **Boot-update**: Use `/releases?per_page=1` instead of `/releases/latest` (avoids 404 for repo names with dots)
- **Boot-update**: Use jq for release metadata parsing (tag_name, prerelease, published_at, min_host_version)

### Added
- **Install**: Add `install_jq` so jq is available for boot-update and recovery scripts
- **Tests**: Preview API endpoint (Go) and shouldRefreshPreview logic (JS)

## [2.5.0] - 2026-02-20

### Changed
- **GHCR image path**: `ghcr.io/alexwitherspoon/aviationwx.org-bridge` → `ghcr.io/alexwitherspoon/aviationwx-org-bridge`
  - Hyphen naming everywhere we control (container, binaries, image). Period only for GitHub URLs.
  - Migration required: update `docker pull` and scripts to use new path.

### Fixed
- **Boot-update**: Add `min_host_version` to release metadata (fixes "No min_host_version" on fresh install)
- **Boot-update**: Fallback to `/releases` list when `/releases/latest` 404s (GitHub API issue with dots in repo names)
- **Boot-update**: Use lowercase repo path for GitHub API
- **Boot-update**: Fallback tag parsing from `name` field when `jq` extraction fails
- **Install banner**: Fix alignment of https://aviationwx.org line
- **Install bootstrap**: Run container-start after boot-update so container actually starts (boot-update only updates version)

## [2.4.1] - 2026-02-20

### Changed
- **Display name consistency**: "AviationWX Bridge" → "AviationWX.org Bridge" across release titles, docs, web UI, and scripts
- **Supervisor**: Use `grep -F` for literal image name match (avoids regex `.` matching any char)
- **ASCII banners**: Fix spacing/alignment in Makefile, install.sh, test-ci-local.sh, aviationwx-recovery.sh

## [2.4.0] - 2026-02-20

### Changed
- **Repository rename**: `aviationwx-bridge` → `AviationWX.org-Bridge`
  - GitHub URL: `github.com/alexwitherspoon/AviationWX.org-Bridge`
  - Docker image: `ghcr.io/alexwitherspoon/aviationwx-org-bridge`
  - Container name: `aviationwx-org-bridge`
  - Binary artifacts: `aviationwx-org-bridge-{arch}`

## [2.3.0] - 2026-02-20

### Added
- **Test Snapshot UI**: Test camera configuration before saving
  - "Test Snapshot" button in Add/Edit Camera modal
  - Live preview of captured image; validates HTTP/RTSP/ONVIF config
  - Extracted `form-utils.js` with `buildCameraConfigFromFormValues` for testability
  - Node built-in test runner for frontend form logic
- **SFTP Mutex**: Serialize Upload and TestConnection per SFTPClient to prevent race conditions
- **Scheduler Panic Recovery**: Wrap upload worker in defer/recover; log panic and stack, treat as failure
- **Watchdog Bridge Check**: Script checks bridge process and web console before restarting services
- **GitHub Copilot Instructions**: Repository-wide `.github/copilot-instructions.md` for Copilot context

### Fixed
- Concurrent SFTP uploads could race; mutex ensures safe reuse of client

### Testing
- `TestHandleTestCamera` and `TestHandleTestUpload` for web API
- `TestBridge_testCamera` unit tests (success, capture error, create error)
- Integration test: `POST /api/test/camera` with TestCamera mock
- Frontend: 12 unit tests for `buildCameraConfigFromFormValues`

### Documentation
- README: Replace 2 screenshots with 5 full-size screenshots (Dashboard, Cameras, Add Camera, Settings, Logs)

## [2.0.1] - 2026-01-18

**Hot-Reload Fixes** - Complete global settings hot-reload support

### Added
- **Timezone Hot-Reload**: Timezone changes now hot-reload without container restart
  - `updateTimezone()` recreates authority and updates all workers
  - Immediate effect on all camera EXIF timestamps
  - No downtime required for timezone changes
- **SNTP Hot-Reload**: SNTP configuration changes now hot-reload
  - `restartSNTP()` stops old service and starts new one
  - Server list, intervals, and thresholds update immediately
  - Time health status reflects new config within seconds
- **Daily Container Restart**: Optional safety net cron job
  - Installed automatically by `install.sh`
  - Runs at 3 AM daily
  - Gracefully restarts container and waits for healthy status
  - Logs to `/data/aviationwx/restart.log`
  - Catches edge cases and ensures fresh state
- **UI Notifications**: Toast notifications for successful config changes
  - Confirms timezone updates with green success message
  - Subtle animation and auto-dismiss after 3 seconds
- **TimeHealth Stop Method**: Added graceful shutdown for SNTP service
  - Context-based cancellation prevents goroutine leaks
  - Clean restart without resource accumulation

### Fixed
- **Camera Interval Hot-Reload**: Workers now start immediately on config update
  - Previously required container restart to apply new intervals
  - `Orchestrator.AddCamera()` now calls `worker.Start()` if orchestrator is running
  - Interval changes take effect within 1 second
- **Global Config Handler**: Now actually handles global settings changes
  - Previously only logged "Global config updated" with no action
  - Now updates timezone and restarts SNTP as needed
- **Orchestrator Methods**: Added `SetTimeAuthority()` for hot updates
  - Propagates new authority to all capture workers
  - Ensures consistent time handling across all cameras

### Changed
- **`install.sh`**: Now installs daily restart cron job
  - Creates `/usr/local/bin/aviationwx-daily-restart` script
  - Adds crontab entry for 3 AM daily execution
  - Uninstall now removes cron job cleanly
- **Completion Message**: Updated to mention daily restart feature

### Documentation
- **`docs/HOT_RELOAD_ANALYSIS.md`**: Comprehensive 300+ line analysis
  - All hot-reload scenarios documented with test cases
  - Impact assessment for each global setting
  - Priority recommendations and implementation details
- **`docs/HOT_RELOAD_FIX.md`**: User-facing guide
  - Installation and testing instructions
  - Troubleshooting common issues
  - Daily restart script usage

## [2.0.0] - 2026-01-18

**Major Release** - Complete architecture refactor for reliability and scalability

### Changed
- **BREAKING**: Configuration architecture completely refactored to service-oriented design
  - **File-per-camera storage** replaces monolithic `config.json`
  - New structure: `/data/global.json` + `/data/cameras/*.json`
  - Automatic migration from legacy format on first startup
  - **ConfigService** eliminates shared config pointer issues
  - Event-driven architecture (no blocking callbacks)
  - Hot-reload now truly atomic and non-blocking
  - **Set `AVIATIONWX_CONFIG_DIR=/data` instead of `AVIATIONWX_CONFIG`**
  
### Added
- **Config**: New `ConfigService` with atomic operations
  - `AddCamera()`, `UpdateCamera()`, `DeleteCamera()` are now atomic
  - `Subscribe()` for async event notifications
  - Thread-safe by design (RWMutex)
  - Automatic config backup before every load/save operation
  - Consistent camera ordering (sorted by ID) prevents UI reordering
- **Migration**: Automatic migration from legacy `config.json`
  - `InitOrMigrate()` detects and migrates old format
  - Original file backed up as `config.json.migrated`
  - Zero downtime migration
- **Worker Job Blocking**: Prevents overlapping capture/upload operations
  - **Capture workers** check if previous job is running before starting new one
  - **Upload workers** wait for previous upload to complete before processing next
  - Maximum job times: Capture = 60s + interval, Upload = 120s
  - Jobs exceeding max time are automatically aborted and logged
  - Prevents resource exhaustion and runaway goroutines
- **Live Logs**: In-memory log buffer accessible via web UI
  - Stores last 1000 log entries in circular buffer
  - **`/api/logs` endpoint** serves formatted logs
  - Web UI **Logs tab** with real-time polling (2 second refresh)
  - Level filtering (ERROR, WARN, INFO, DEBUG)
  - Syntax highlighting for different log levels
  - No Docker access required - runs entirely in-app
- **Crash Recovery**: Comprehensive panic recovery and restart mechanisms
  - Panic handlers in main function and web server goroutine
  - Docker healthcheck using `/healthz` endpoint (no auth required)
  - Automatic restart on crash via `restart: unless-stopped`
  - Full stack traces logged on panic for debugging
  - Detailed startup logging with PID and config paths
- **Logging**: Enhanced structured logging for troubleshooting
  - More detailed error messages in web API responses
  - Camera add/update/delete operations logged with context
  - ConfigService operations logged for audit trail
  - **Automatic log rotation**: 10MB max (2 × 5MB files)
  - Rotated logs are compressed to save space
  - **Raspberry Pi SD card protection**: journald with volatile storage
  - Install script auto-configures RAM-based logging on Pi
  - Zero SD card wear from log writes
- **System Metrics**: Real-time resource monitoring in Dashboard
  - CPU usage percentage (from `/proc/stat`)
  - Memory usage percentage and MB used/total (from `/proc/meminfo`)
  - Queue depth (images waiting to upload)
  - System uptime
  - Works correctly in Docker containers
  - **Update status** in API response (current/latest version, update available flag)
- **NTP/Time Health**: Automatic time synchronization monitoring
  - **Enabled by default** with sensible defaults
  - Default servers: `pool.ntp.org`, `time.google.com`
  - Checks every 5 minutes, reports if offset > 5 seconds
  - Works in Docker (uses UDP port 123, no special config needed)
  - Status visible in Dashboard (shows time offset in ms)
- **Dashboard Monitoring**: Comprehensive worker status visibility
  - Next capture countdown (real-time, updates every second)
  - Currently capturing/uploading indicators (live 🔴 badges)
  - Upload failure tracking per camera with counts
  - Last failure reason displayed for debugging
  - Queue health visualization with progress bars
  - All metrics auto-refresh every 1 second for live monitoring
- **Documentation**: 
  - New `CONFIG_SCHEMA_v2.md` with file-per-camera docs
  - New `LOCAL_TESTING.md` for Docker-based testing
  - New `CRASH_RECOVERY.md` for troubleshooting and monitoring
- **Examples**: New config file examples
  - `configs/global.json.example` - Global settings
  - `configs/camera-http.json.example` - HTTP camera
  - `configs/camera-rtsp.json.example` - RTSP camera
  - `configs/camera-onvif.json.example` - ONVIF camera

### Fixed
- **Web UI**: Eliminated all config data loss issues
  - No more disappearing cameras after edits
  - No more lost timezone/global settings
  - No more pointer synchronization bugs
  - Modal properly closes after successful operations
  - Camera list no longer randomly reorders on refresh
- **Upload**: Fixed cameras uploading to wrong FTP targets (per-camera credentials bug)
  - Each camera now has its own independent uploader with separate credentials
  - Previously, the last camera's uploader credentials were used for all cameras
  - Refactored `UploadWorker` to store per-camera uploaders instead of shared uploader
  - Updated `Orchestrator.AddCamera()` to accept uploader parameter
- **Stability**: Application no longer exits on web server goroutine panics
  - Web server errors are caught and logged
  - Container restarts automatically if fatal error occurs
  - Health checks detect and recover from hangs
- **Tests**: Fixed race condition in `TestQueue_CapturePause`
  - Test now allows for expected queue thinning behavior
  - More robust assertions for capacity-based testing

### Removed
- `internal/config/loader.go` - Replaced by ConfigService
- `OnConfigChange` callback pattern - Replaced by event subscription
- `UpdateConfig()` method on web server - No longer needed
- All TODO comments from codebase (100% clean)

## [1.0.2] - 2026-01-18

**Bugfix release** - Permissions and port configuration

### Added
- **Web UI**: Added upload server port configuration field
  - Users can now specify custom FTPS port when adding/editing cameras
  - Default changed from 21 to 2121 (standard FTPS port)
  - Port field supports values 1-65535
  - Existing configurations will use port 21 until manually updated

### Fixed
- **Install**: Fixed permission denied error when saving config
  - Set ownership of `/data/aviationwx` to uid:gid 1000:1000 (matches container user)
  - Container runs as non-root user `bridge` but data directory was owned by root
  - Fixes "permission denied" error when saving camera configuration via web UI
- **Config**: Create config directory if it doesn't exist
  - Prevents "no such file or directory" error on first run
  - Automatically creates `/data` directory with proper permissions
- **Config**: Updated tests and config loader for new default port 2121

## [1.0.1] - 2026-01-18

**Critical bugfix release** - v1.0.0 Docker images had incorrect architecture binaries

### Fixed
- **Docker**: Fixed multi-arch builds not creating correct architecture binaries
  - Removed default values from `TARGETOS` and `TARGETARCH` ARG declarations
  - Docker buildx now properly injects platform-specific values during multi-arch builds
  - Moved ARG declarations to immediately after FROM statement for proper scope
  - Verified: arm64 binaries now correctly build as ARM64 (not x86-64)
  - **Impact**: v1.0.0 arm64/armv7 images contained x86-64 binaries causing "exec format error"
- **Docker**: Fixed exiftool not being available in container
  - Changed from `perl-image-exiftool` to `exiftool` package
  - The `exiftool` package includes both the command-line tool and Perl libraries
  - Verified on all platforms: linux/amd64, linux/arm64, linux/arm/v7
- **Release**: Fixed artifact preparation in release workflow
  - Changed find command to use -mindepth 2 to handle artifact subdirectories
  - Reordered operations to remove directories before creating checksums

## [1.0.0] - 2026-01-18

⚠️ **Known Issue**: Multi-arch Docker images contain incorrect binaries (fixed in v1.0.1)

First production release of AviationWX.org Bridge! 🎉

This release represents a complete, production-ready weather camera bridge for aviationwx.org with comprehensive features for camera management, image processing, queue management, and automatic updates.

### Fixed
- **CI/CD**: Docker images now only publish after all tests pass
  - Merged `build.yml` into `ci.yml` with proper job dependencies
  - Added `publish` job that depends on `test`, `build`, and `docker` jobs
  - Prevents publishing broken images when tests fail on main branch
  - Manual workflow dispatch still available for emergency rebuilds
- **Install**: Install script now uses `:edge` tag for pre-release testing
  - The `:latest` tag is created when a release is published
  - The `:edge` tag is created on every push to main branch
  - Supervisor uses versioned releases (e.g., `v1.0.0`) as designed
- **CI/CD**: Consolidated and standardized GitHub Actions workflows
  - Merged `test.yml` and `ci.yml` into single comprehensive CI workflow
  - Standardized all workflows to Go 1.24 (eliminates tar cache warnings)
  - Added exiftool installation to all test jobs
  - Improved test reliability and reduced workflow setup costs
- **Tests**: Fixed flaky `TestQueue_CapturePause` test
  - Added explicit verification of queue state before assertions
  - Added better error messages showing actual vs expected capacity
  - Test now checks enqueue errors properly
  - Runs consistently across different environments
- **Tests**: Changed to fail-closed approach for missing dependencies
  - Tests now **FAIL** (not skip) if exiftool is missing
  - Ensures local development environment matches CI/CD requirements
  - Better error messages with installation instructions

### Added
- **Documentation**: Added comprehensive local development setup guide (`docs/LOCAL_DEVELOPMENT.md`)
  - Prerequisites and required tools
  - Quick start instructions
  - Testing commands and workflows
  - Common issues and solutions
  - Development workflow best practices
- **Tooling**: Added local CI validation script (`scripts/test-local.sh`)
  - Runs all the same checks as GitHub Actions CI
  - Validates Go version, exiftool availability
  - Runs linting, formatting, tests with race detection
  - Provides coverage summary and build verification
  - Color-coded output for easy issue identification

### Added
- **System Resources Dashboard**: Real-time monitoring with color-coded health indicators
  - CPU usage with percentage and health level (green/yellow/red)
  - Memory usage with system totals and Go heap stats
  - Queue storage with capacity percentage and image count
  - Overall system health badge (healthy/warning/critical)
  - Uptime display

- **Queue Storage Safeguards**: Multi-layer protection against storage exhaustion
  - Pre-write space checking before image enqueue
  - Automatic cleanup when filesystem space is low (<20% triggers thin, <10% triggers aggressive thin)
  - Write failure recovery with retry after emergency cleanup
  - Graceful capture pause when space cannot be freed
  - Never crashes - always self-recovers

- **Configurable tmpfs Size**: Flexible queue storage sizing
  - Default increased from 100MB to 200MB for better multi-camera support
  - Environment variable `AVIATIONWX_TMPFS_SIZE` for easy customization
  - Environment file at `/data/aviationwx/environment` for Pi installations
  - Sizing recommendations in documentation

- **Queue Storage Documentation**: Comprehensive guide at `docs/QUEUE_STORAGE.md`
  - Multi-layer defense system explanation with diagrams
  - Space exhaustion recovery flowchart
  - Queue thinning strategy visualization
  - Sizing recommendations table
  - Troubleshooting guide

- **Web Console**: Modern web-based configuration interface
  - Dashboard with real-time status, camera overview, and queue health
  - Camera management (add, edit, delete cameras via UI)
  - Timezone configuration with live UTC/local time preview
  - Setup wizard for first-time installation
  - Mobile-friendly responsive design
  - Version display with update notification badge
  
- **Per-Camera Upload Credentials**: Each camera now has its own FTP credentials
  - Default upload server: `upload.aviationwx.org`
  - Contact `contact@aviationwx.org` for credentials

- **Image Quality & Bandwidth Control**: Optional per-camera image processing
  - Default: use original image as-is (no processing)
  - Optional max resolution (width/height) for bandwidth control
  - Optional JPEG quality setting (1-100) for re-encoding
  - Presets in UI: Original (default), High (1080p), Medium (720p), Low (480p)
  
- **Queue Management System**: File-based queue for historic image replay
  - Memory-backed tmpfs storage for Raspberry Pi SD card longevity
  - Configurable queue limits (files, size, age)
  - Health-based thinning to prevent memory exhaustion
  - Emergency pruning for critical situations
  
- **Time Authority System**: Accurate observation timestamp handling
  - NTP health monitoring with configurable servers
  - Camera EXIF validation and correction
  - Bridge-stamped UTC times with markers
  - Configurable drift tolerance
  
- **Exiftool Integration**: Server-compatible EXIF parsing
  - Uses same `exiftool` as aviationwx.org server
  - Validates bridge-written EXIF is readable
  - Bundled in Docker container (`perl-image-exiftool`)
  
- **Scheduler Orchestration**: Decoupled capture and upload workers
  - Per-camera capture goroutines
  - Round-robin upload worker
  - Fail2ban-aware retry logic with backoff
  
- **Automatic Updates** (Raspberry Pi install):
  - Supervisor script checks for updates every 6 hours
  - Normal updates: notification only
  - Critical updates: auto-apply after 24hr grace period
  - Automatic rollback on health check failure
  
- **GitHub Actions CI/CD**:
  - Automated testing with exiftool
  - golangci-lint for code quality
  - Multi-architecture Docker builds (amd64, arm64, armv7)
  - Pre-built binaries for Linux and macOS
  - Semantic versioning with release metadata

- **Structured Logging**: Using Go's `log/slog`
  - Configurable log level (debug, info, warn, error)
  - JSON or text format output

- **Update Checker**: Compares running version with GitHub releases
  - Shows update badge in web UI
  - Provides release URL for manual updates

- **Local CI Testing**: `./scripts/test-ci-local.sh` for pre-push validation

### Changed
- Config version upgraded to v2
- `interval_seconds` renamed to `capture_interval_seconds`
- Upload configuration moved from global to per-camera
- Default web console port changed to `1229` (was 8080)
- Default web console password is `aviationwx`
- EXIF handling migrated from PHP to exiftool CLI
- Default tmpfs size increased from 100MB to 200MB for better multi-camera support
- Queue manager now monitors actual filesystem space (not just application counters)

### Deprecated
- Global `upload` configuration (use per-camera instead)
- `basic_auth` in web_console (use `password` instead)
- `remote_path` in camera config (always uploads to root folder)

### Removed
- PHP EXIF helper (replaced by exiftool)

## [0.1.0] - Initial Development

### Added
- Camera capture support (HTTP, RTSP, ONVIF)
- FTPS upload with TLS
- Basic configuration via JSON
- Degraded mode for network issues
- Backoff and retry logic
