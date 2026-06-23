# AviationWX.org Bridge - Copilot Instructions

## Project Summary

AviationWX.org Bridge is a **safety-critical** Go daemon that captures webcam snapshots from local network cameras (HTTP, ONVIF, RTSP) and uploads them to aviationwx.org for pilot weather assessment. It runs on Raspberry Pi Zero 2 W (512MB RAM) and similar embedded devices. Memory and reliability are primary constraints.

**Key facts:**
- Config version 2; upload credentials are per-camera, not global
- Web console on port 1229 (default password: aviationwx)
- Queue in tmpfs at `/dev/shm/aviationwx`
- EXIF: use `exiftool` only (no PHP, no Go libraries); timestamps in UTC with `OffsetTimeOriginal=+00:00`
- Bridge clock (NTP-synced) is the time authority; camera EXIF only used if within tolerance

## Build & Validate

**Prerequisites:** Go 1.26+, exiftool (tests require it: `brew install exiftool` or `apt install libimage-exiftool-perl`)

```bash
# Format (always run before commit)
gofmt -s -w .

# Vet
go vet ./...

# Build
make build
# or: go build -o bin/bridge ./cmd/bridge

# Tests (required before commit)
make test          # Go tests with race detector
make test-js       # Frontend form-utils tests

# Full check (format, vet, all tests)
make check
```

**CI runs:** `go vet`, `gofmt -s -l`, `golangci-lint`, `go test -v -race`, multi-arch build. See `.github/workflows/ci.yml`.

## Project Layout

| Path | Purpose |
|------|---------|
| `cmd/bridge/` | Main entry point; Bridge orchestrator, createCamera, testCamera |
| `internal/camera/` | HTTP, RTSP, ONVIF camera implementations |
| `internal/config/` | Config service, types, validation, migration |
| `internal/scheduler/` | Orchestrator, upload worker, backoff, degraded mode |
| `internal/upload/` | SFTP client |
| `internal/web/` | Web server, static assets (JS/CSS) |
| `internal/time/` | EXIF, SNTP, time authority |
| `internal/queue/` | File-based queue manager |
| `internal/image/` | Image processing (resize, quality) |
| `configs/` | Example configs (config.json.example, global.json.example, camera-*.json.example) |
| `docker/` | Dockerfile, docker-compose.yml |
| `scripts/` | install.sh, watchdog, test-ci-local.sh |

**Key files:** `cmd/bridge/main.go`, `internal/config/types.go`, `docs/CONFIG_SCHEMA.md`, `CODE_STYLE.md`

## Code Conventions

Follow [CODE_STYLE.md](CODE_STYLE.md) for all changes.

**Safety & reliability:**
- Each capture cycle must fetch a new frame; do not skip capture to upload older backlog only
- Each camera degrades independently; never fail silently
- Self-recovery: resume capture when queue drains; `/readyz` for Docker health; host capture-restart via supervisor or timer (see DEPLOYMENT.md)
- Use `context.Context` for cancellation and timeouts
- Wrap errors: `fmt.Errorf("context: %w", err)`

**Go style:**
- `internal/` for private code; `pkg/` only when importable
- Interfaces for testability; accept interfaces, return structs
- Table-driven tests when appropriate
- Add tests for critical paths and new features

**Dependencies:** Minimize. Prefer stdlib; document rationale for new deps.

## Repo Hygiene

Do not commit scratch working docs (analysis, summaries, checklists, implementation plans). Remove or consolidate before commit. Only commit docs that belong long-term.

This file is the in-repo context for **GitHub Copilot** automated PR review. Keep it aligned with [CODE_STYLE.md](../CODE_STYLE.md).

**Maintainer voice:** Plain technical English. PRs use Summary/Description, bullet Changes, Test plan checklists, and `Closes #N`. Review replies: one sentence plus commit SHA; no performative praise.
