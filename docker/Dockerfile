# Multi-stage build for AviationWX.org Bridge
# Stage 1: Build Go binary
FROM golang:1.26.4-alpine3.23 AS builder

# Build arguments for multi-arch support
# These are automatically set by Docker buildx when building multi-platform images
# Do NOT set defaults - let buildx inject them automatically
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
# CGO_ENABLED=0 for static binary, no C dependencies
# GOOS/GOARCH for multi-arch support (automatically set by buildx)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.Version=${VERSION} -X main.GitCommit=${GIT_COMMIT}" \
    -o /bridge \
    ./cmd/bridge

# Stage 2: Runtime
FROM alpine:3.24.1

# Install runtime dependencies
# - ffmpeg: RTSP capture
# - exiftool: EXIF reading/writing (same tool as aviationwx.org server)
# - ca-certificates: TLS for outbound connections
# - tzdata: timezone support
RUN apk add --no-cache \
    ffmpeg \
    exiftool \
    ca-certificates \
    tzdata

# Create non-root user
RUN addgroup -g 1000 bridge && \
    adduser -D -u 1000 -G bridge bridge

# Create directories
# - /data: configuration and persistent storage
# - /dev/shm/aviationwx: tmpfs queue storage (memory-backed)
RUN mkdir -p /data /dev/shm/aviationwx && \
    chown -R bridge:bridge /data /dev/shm/aviationwx

# Copy binary from builder
COPY --from=builder /bridge /usr/local/bin/bridge

# Set working directory
WORKDIR /data

# Switch to non-root user
USER bridge

# Expose web console port
EXPOSE 1229

# Health check
# - interval: Check every 30 seconds
# - timeout: Health check must complete within 5 seconds
# - start-period: Grace period for app startup (30 seconds)
# - retries: Mark unhealthy after 3 consecutive failures (90 seconds total)
# Triggers: Container marked unhealthy → systemd can restart if configured
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://127.0.0.1:1229/readyz || exit 1

# Environment variables for configuration
# AVIATIONWX_CONFIG_DIR: Base directory for config files (v2.0+)
# AVIATIONWX_CONFIG: Legacy config path for auto-migration
# AVIATIONWX_QUEUE_PATH: Queue storage location
ENV AVIATIONWX_CONFIG_DIR=/data
ENV AVIATIONWX_CONFIG=/data/config.json
ENV AVIATIONWX_QUEUE_PATH=/dev/shm/aviationwx

# Run bridge daemon
ENTRYPOINT ["/usr/local/bin/bridge"]
