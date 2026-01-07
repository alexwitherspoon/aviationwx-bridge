#!/bin/bash
#
# Test CI workflows locally using 'act'
# https://github.com/nektos/act
#
# Usage:
#   ./scripts/test-ci-local.sh           # Run all CI tests
#   ./scripts/test-ci-local.sh test      # Run just tests
#   ./scripts/test-ci-local.sh lint      # Run just linting
#   ./scripts/test-ci-local.sh build     # Run just build
#   ./scripts/test-ci-local.sh docker    # Run Docker build
#   ./scripts/test-ci-local.sh release   # Simulate release
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[OK]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }

# Check if act is installed
check_act() {
    if ! command -v act &> /dev/null; then
        log_error "'act' is not installed"
        echo ""
        echo "Install with:"
        echo "  brew install act          # macOS"
        echo "  curl -s https://raw.githubusercontent.com/nektos/act/master/install.sh | sudo bash  # Linux"
        echo ""
        exit 1
    fi
    log_success "act is installed: $(act --version)"
}

# Check if Docker is running
check_docker() {
    if ! docker info &> /dev/null; then
        log_error "Docker is not running"
        exit 1
    fi
    log_success "Docker is running"
}

# Run gofmt check (same as CI)
run_gofmt_check() {
    log_info "Running gofmt check..."
    
    if [ "$(gofmt -s -l . | wc -l)" -gt 0 ]; then
        log_error "Code is not formatted. Run 'gofmt -s -w .'"
        gofmt -s -l .
        return 1
    fi
    
    log_success "Code is properly formatted"
}

# Run tests locally (without act, faster)
run_tests_native() {
    log_info "Running tests natively (faster than act)..."
    
    # Check for exiftool
    if command -v exiftool &> /dev/null; then
        log_success "exiftool available: $(exiftool -ver)"
    else
        log_warn "exiftool not installed - some tests will be skipped"
    fi
    
    go test -v ./...
    log_success "All tests passed"
}

# Run linting locally
run_lint_native() {
    log_info "Running golangci-lint..."
    
    if ! command -v golangci-lint &> /dev/null; then
        log_warn "golangci-lint not installed, installing..."
        go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    fi
    
    golangci-lint run ./...
    log_success "Linting passed"
}

# Run build locally
run_build_native() {
    log_info "Building for multiple architectures..."
    
    local platforms=("linux/amd64" "linux/arm64" "linux/arm")
    local git_commit
    git_commit=$(git rev-parse --short HEAD)
    
    for platform in "${platforms[@]}"; do
        IFS='/' read -r goos goarch <<< "$platform"
        log_info "Building for ${goos}/${goarch}..."
        
        GOOS=$goos GOARCH=$goarch CGO_ENABLED=0 go build \
            -ldflags="-w -s -X main.Version=dev -X main.GitCommit=${git_commit}" \
            -o "/dev/null" \
            ./cmd/bridge
        
        log_success "Built for ${goos}/${goarch}"
    done
    
    log_success "All builds succeeded"
}

# Run Docker build locally
run_docker_build() {
    log_info "Building Docker image locally..."
    
    local git_commit
    git_commit=$(git rev-parse --short HEAD)
    
    docker build \
        -f docker/Dockerfile \
        --build-arg VERSION=dev \
        --build-arg GIT_COMMIT="${git_commit}" \
        -t aviationwx-bridge:local-test \
        .
    
    log_success "Docker image built: aviationwx-bridge:local-test"
    
    # Quick smoke test
    log_info "Running smoke test..."
    docker run --rm aviationwx-bridge:local-test --version 2>/dev/null || \
        docker run --rm aviationwx-bridge:local-test exiftool -ver
    
    log_success "Docker smoke test passed"
}

# Run full CI via act
run_ci_via_act() {
    log_info "Running CI workflow via act..."
    
    # Use medium image for better compatibility
    act push \
        -W .github/workflows/ci.yml \
        -P ubuntu-latest=catthehacker/ubuntu:act-latest \
        --container-architecture linux/amd64
    
    log_success "CI workflow completed"
}

# Simulate release via act
run_release_simulation() {
    log_info "Simulating release workflow..."
    log_warn "Note: This won't actually push to registry or create release"
    
    # Create event payload
    local event_file
    event_file=$(mktemp)
    cat > "$event_file" << 'EOF'
{
  "ref": "refs/tags/v0.0.0-test",
  "ref_type": "tag"
}
EOF
    
    act push \
        -W .github/workflows/release.yml \
        -P ubuntu-latest=catthehacker/ubuntu:act-latest \
        --container-architecture linux/amd64 \
        --eventpath "$event_file" \
        -n  # Dry run
    
    rm "$event_file"
    log_success "Release simulation completed (dry run)"
}

# Main
main() {
    local command="${1:-all}"
    
    echo ""
    echo "╔════════════════════════════════════════════════════════════╗"
    echo "║           AviationWX Bridge - Local CI Testing             ║"
    echo "╚════════════════════════════════════════════════════════════╝"
    echo ""
    
    case "$command" in
        fmt)
            run_gofmt_check
            ;;
        test)
            run_tests_native
            ;;
        lint)
            run_lint_native
            ;;
        build)
            run_build_native
            ;;
        docker)
            check_docker
            run_docker_build
            ;;
        act)
            check_act
            check_docker
            run_ci_via_act
            ;;
        release)
            check_act
            check_docker
            run_release_simulation
            ;;
        all)
            run_gofmt_check
            echo ""
            run_tests_native
            echo ""
            run_lint_native
            echo ""
            run_build_native
            echo ""
            check_docker
            run_docker_build
            ;;
        *)
            echo "Usage: $0 [fmt|test|lint|build|docker|act|release|all]"
            echo ""
            echo "Commands:"
            echo "  fmt      Check code formatting (gofmt -s)"
            echo "  test     Run Go tests natively"
            echo "  lint     Run golangci-lint"
            echo "  build    Build Go binaries for all platforms"
            echo "  docker   Build Docker image locally"
            echo "  act      Run full CI workflow via act"
            echo "  release  Simulate release workflow (dry run)"
            echo "  all      Run fmt, test, lint, build, docker (default)"
            exit 1
            ;;
    esac
    
    echo ""
    log_success "Done!"
}

main "$@"

