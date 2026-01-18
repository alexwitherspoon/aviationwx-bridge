# Changelog

All notable changes to AviationWX Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

First production release of AviationWX Bridge! 🎉

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
