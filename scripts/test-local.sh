#!/bin/bash
# Local CI validation script
# This script runs the same checks as GitHub Actions CI workflow

set -e

echo "🧪 Running Local CI Validation"
echo "=============================="
echo ""

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Track results
FAILED=0

# Function to report step
step() {
    echo ""
    echo "📋 $1"
    echo "---"
}

# Function to check result
check_result() {
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ $1 passed${NC}"
    else
        echo -e "${RED}✗ $1 failed${NC}"
        FAILED=1
        if [ "${2:-}" != "continue" ]; then
            exit 1
        fi
    fi
}

cd "$(dirname "$0")/.."

# 1. Check Go version
step "Checking Go version"
GO_VERSION=$(go version | grep -oE 'go1\.[0-9]+')
if [[ "$GO_VERSION" == "go1.24" ]] || [[ "$GO_VERSION" == "go1.25" ]]; then
    echo "Go version: $GO_VERSION"
    check_result "Go version"
else
    echo "Warning: Using $GO_VERSION, CI uses Go 1.24"
fi

# 2. Check exiftool
step "Checking exiftool"
if command -v exiftool >/dev/null 2>&1; then
    EXIFTOOL_VERSION=$(exiftool -ver)
    echo "exiftool version: $EXIFTOOL_VERSION"
    check_result "exiftool installed"
else
    echo -e "${RED}✗ exiftool not found${NC}"
    echo "Install with: brew install exiftool (macOS) or apt-get install libimage-exiftool-perl (Linux)"
    FAILED=1
fi

# 3. Download dependencies
step "Downloading dependencies"
go mod download
check_result "Download dependencies"

# 4. Verify dependencies
step "Verifying dependencies"
go mod verify
check_result "Verify dependencies"

# 5. Run go vet
step "Running go vet"
go vet ./...
check_result "go vet"

# 6. Run gofmt check
step "Running gofmt check"
UNFORMATTED=$(gofmt -s -l . | grep -v vendor | grep -v ".pb.go" | wc -l | tr -d ' ')
if [ "$UNFORMATTED" -gt 0 ]; then
    echo -e "${RED}❌ Code is not formatted. Run 'gofmt -s -w .'${NC}"
    gofmt -s -d .
    FAILED=1
else
    echo -e "${GREEN}✓ Code is properly formatted${NC}"
fi

# 7. Run golangci-lint (if installed)
step "Running golangci-lint"
if command -v golangci-lint >/dev/null 2>&1; then
    golangci-lint run ./...
    check_result "golangci-lint" "continue"
else
    echo -e "${YELLOW}⚠ golangci-lint not installed, skipping${NC}"
    echo "Install with: brew install golangci-lint (macOS)"
    echo "CI will run this check"
fi

# 8. Run tests with race detection and coverage
step "Running tests (with -race and coverage)"
go test -v -race -coverprofile=coverage.out ./...
check_result "Tests with race detection"

# 9. Display coverage summary
step "Coverage Summary"
go tool cover -func=coverage.out | tail -20

# 10. Check for required files
step "Checking for required files"
REQUIRED_FILES=(
    "cmd/bridge/main.go"
    "configs/config.json.example"
    "docker/Dockerfile"
    "README.md"
    "LICENSE"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [ ! -f "$file" ]; then
        echo -e "${RED}❌ Required file missing: $file${NC}"
        FAILED=1
    else
        echo "✓ $file"
    fi
done

# 11. Validate JSON config
step "Validating JSON config"
if [ -f "configs/config.json.example" ]; then
    python3 -m json.tool configs/config.json.example > /dev/null
    check_result "config.json.example validation"
fi

# 12. Test build
step "Testing build"
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
CGO_ENABLED=0 go build -ldflags="-w -s -X main.Version=dev -X main.GitCommit=${GIT_COMMIT}" -o bridge ./cmd/bridge
check_result "Build"
rm -f bridge

# Summary
echo ""
echo "=============================="
if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}✓ All checks passed!${NC}"
    echo ""
    echo "Your code is ready for CI/CD 🚀"
    exit 0
else
    echo -e "${RED}✗ Some checks failed${NC}"
    echo ""
    echo "Please fix the issues above before pushing"
    exit 1
fi

