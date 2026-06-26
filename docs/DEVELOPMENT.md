# Development

## Prerequisites

- Go 1.26+
- Docker and Docker Compose
- exiftool (required for tests): `brew install exiftool` or `apt install libimage-exiftool-perl`

## Run

```bash
# With Docker (recommended)
cd docker && docker compose up --build

# Or native
go run ./cmd/bridge
```

Web console: http://localhost:1229 (password: aviationwx)

**Network model:** The bridge is intended for **private LAN** deployment only. See [DEPLOYMENT.md — Network exposure](DEPLOYMENT.md#network-exposure) and [CONFIG_SCHEMA.md — Web console and network exposure](CONFIG_SCHEMA.md#web-console-and-network-exposure).

## Test

```bash
go test ./...
go test -race ./...
npm ci          # installs devDependencies (Bats for shell tests)
npm run test:sh # optional: scripts/aviationwx-capture-restart.sh (Bats)
./scripts/test-ci-local.sh
```

## Build

```bash
go build -o bridge ./cmd/bridge
```

For a 64-bit ARM single-board computer (Raspberry Pi 4/5, Radxa, Orange Pi, etc.): `GOOS=linux GOARCH=arm64 go build -o bridge ./cmd/bridge`

## Docker

```bash
make dev          # creates docker/data, builds, starts compose
# or: cd docker && docker compose up --build
docker compose -f docker/docker-compose.yml logs -f
```

Use `docker-compose.test.yml` for isolated test runs (port 1230, separate data dir).

**Health endpoints (manual check):**

```bash
curl -sS http://localhost:1229/readyz | jq .
curl -sS -o /dev/null -w "%{http_code}\n" http://localhost:1229/healthz
```

**Host `container-start` (local, uses built image tag):**

```bash
docker build -f docker/Dockerfile -t ghcr.io/alexwitherspoon/aviationwx-org-bridge:local-test .
mkdir -p docker/data && echo "local-test" > docker/data/last-known-good.txt
AVIATIONWX_DATA_DIR="$(pwd)/docker/data" AVIATIONWX_SELF_UPDATE=0 ./scripts/aviationwx-container-start.sh local-test
```

## E2E harness (local)

Three-service Docker stack exercises bridge upload identity against a sibling [aviationwx.org](https://github.com/alexwitherspoon/aviationwx) checkout: upload (HTTPS roster + SFTP), bridge, and camera-simulator.

**Prerequisites:** Docker, sibling repo at `../aviationwx.org` (override with `AVIATIONWX_ORG_ROOT`), `exiftool`, `ffmpeg` (tier 2 image fixtures), `jq`.

```bash
make e2e                    # full stack (tier all)
./scripts/e2e-run.sh --tier 0   # upload roster + bridge identity only
./scripts/e2e-run.sh --tier 1   # + SFTP auth
./scripts/e2e-run.sh --tier 2   # + camera-simulator capture/upload
```

Published ports: upload HTTPS `127.0.0.1:18443`, SFTP `127.0.0.1:12223`, bridge web `127.0.0.1:1231`, simulator `127.0.0.1:18080`. Upload hostname in fixtures is `upload.e2e.test`; bridge trusts the harness CA via `AVIATIONWX_UPLOAD_ROSTER_CA_FILE` (see [CONFIG_SCHEMA.md](CONFIG_SCHEMA.md)).

E2E Go tests live under `test/e2e/` with `//go:build e2e` and are excluded from `go test ./...`. Pin the aviationwx.org image with `AVIATIONWX_ORG_SHA=<ref>` when reproducing a specific server version.

## Config

Create `docker/data/` with `global.json` and `cameras/*.json`. See CONFIG_SCHEMA.md.

## Dependency updates (Dependabot)

Version updates are configured in [`.github/dependabot.yml`](../.github/dependabot.yml) for Go modules, GitHub Actions, and Docker images. Enable **Dependabot version updates** in the repository **Settings → Code security** if it is not already on. `package.json` lists dev-only tooling (e.g. Bats for shell tests); add an `npm` ecosystem entry in Dependabot if you want automated updates for those.

## Host recovery (supervised install)

Fresh installs: `install.sh` enables `aviationwx-watchdog.timer` (1 min) and `aviationwx-capture-restart.timer` (5 min).

Existing supervised installs: `aviationwx-supervisor.sh` enables the capture-restart timer on `boot-update` / daily update (supervisor v2.2+, release `min_host_version` 2.2). Watchdog also invokes `aviationwx-capture-restart.sh` each minute.

See [DEPLOYMENT.md](DEPLOYMENT.md) (Self-recovery) for behavior.
