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

For Raspberry Pi: `GOOS=linux GOARCH=arm64 go build -o bridge ./cmd/bridge`

## Docker

```bash
cd docker
docker compose up --build
docker compose logs -f
```

Use `docker-compose.test.yml` for isolated test runs (port 1230, separate data dir).

## Config

Create `docker/data/` with `global.json` and `cameras/*.json`. See CONFIG_SCHEMA.md.

## Dependency updates (Dependabot)

Version updates are configured in [`.github/dependabot.yml`](../.github/dependabot.yml) for Go modules, GitHub Actions, and Docker images. Enable **Dependabot version updates** in the repository **Settings → Code security** if it is not already on. `package.json` lists dev-only tooling (e.g. Bats for shell tests); add an `npm` ecosystem entry in Dependabot if you want automated updates for those.
