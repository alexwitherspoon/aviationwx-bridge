# Changelog

All notable changes to AviationWX Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-01-18

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
