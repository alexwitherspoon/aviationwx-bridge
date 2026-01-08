# Local Development Setup

This guide helps you set up your local development environment to match CI/CD requirements.

## Prerequisites

### Required Tools

#### 1. Go 1.24+
```bash
# macOS (using Homebrew)
brew install go@1.24

# Or download from: https://go.dev/dl/
```

#### 2. exiftool (REQUIRED)
**This is a hard requirement.** Tests will fail if exiftool is not installed.

```bash
# macOS
brew install exiftool

# Ubuntu/Debian
sudo apt-get update && sudo apt-get install -y libimage-exiftool-perl

# Verify installation
exiftool -ver
```

#### 3. golangci-lint (Optional but recommended)
```bash
# macOS
brew install golangci-lint

# Linux
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
```

## Quick Start

### 1. Clone and Setup
```bash
git clone https://github.com/alexwitherspoon/aviationwx-bridge.git
cd aviationwx-bridge
go mod download
```

### 2. Verify Setup
```bash
./scripts/test-local.sh
```

This script runs **all the same checks as CI/CD** including:
- Go version check
- exiftool availability
- Dependency verification
- go vet
- gofmt
- golangci-lint (if installed)
- Tests with race detection
- Coverage report
- Build verification

## Running Tests

### Run All Tests
```bash
go test ./...
```

### Run Tests with Race Detection (like CI)
```bash
go test -race ./...
```

### Run Tests with Coverage
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Run Specific Package Tests
```bash
go test -v ./internal/queue
go test -v ./internal/time
```

### Run Specific Test
```bash
go test -v -run TestQueue_CapturePause ./internal/queue
```

## Common Issues

### "exiftool not available"
Tests will **FAIL** (not skip) if exiftool is missing. This is intentional (fail-closed approach).

**Solution:** Install exiftool as shown above.

### "go version mismatch"
If you have Go 1.25+, tests should still work, but CI uses Go 1.24.

**Solution:** Either install Go 1.24 or accept the warning.

### "golangci-lint not found"
The local test script will warn but continue. CI **will** run golangci-lint.

**Solution:** Install golangci-lint to catch issues before pushing.

## Development Workflow

### Before Committing
```bash
# 1. Format code
gofmt -s -w .

# 2. Run local CI checks
./scripts/test-local.sh

# 3. If all passes, commit
git add .
git commit -m "your message"
```

### Before Pushing
```bash
# Run the full local CI validation
./scripts/test-local.sh

# This ensures your code will pass CI/CD
```

## CI/CD Environment

Our CI/CD uses Ubuntu 24.04 with:
- Go 1.24
- exiftool (libimage-exiftool-perl)
- golangci-lint (latest)
- Race detector enabled
- Coverage reporting

The `test-local.sh` script mirrors this environment as closely as possible.

## Debugging Test Failures

### Flaky Tests
Some tests involve timing. Run multiple times to catch flakiness:
```bash
go test -count=20 -run TestQueue_CapturePause ./internal/queue
```

### Race Conditions
Always run tests with `-race` flag:
```bash
go test -race ./...
```

### Verbose Output
Add `-v` for detailed test output:
```bash
go test -v -race ./...
```

## Docker Development

For development that matches production exactly:
```bash
cd docker
docker-compose up --build
```

This runs the same Docker image used in production.

## Additional Resources

- **CI/CD Workflow:** `.github/workflows/ci.yml`
- **Local Test Script:** `scripts/test-local.sh`
- **Code Style Guide:** `CODE_STYLE.md`
- **Contributing Guide:** `CONTRIBUTING.md`

## Getting Help

If you're stuck:
1. Run `./scripts/test-local.sh` to see exactly what's failing
2. Check the error messages for installation commands
3. Compare your environment to CI requirements above
4. Open an issue with the output of `go version` and `exiftool -ver`

