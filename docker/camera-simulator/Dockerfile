# Camera simulator for E2E harness: HTTP snapshots + mediamtx RTSP.
FROM golang:1.26.5-alpine3.23 AS builder
WORKDIR /src
RUN printf 'module sim\n\ngo 1.26\n' > go.mod
COPY docker/camera-simulator/main.go ./main.go
RUN CGO_ENABLED=0 go build -o /camera-simulator .

FROM bluenviron/mediamtx:1.11.3-ffmpeg
COPY --from=builder /camera-simulator /usr/local/bin/camera-simulator
COPY docker/camera-simulator/mediamtx.yml /mediamtx.yml
COPY docker/camera-simulator/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
EXPOSE 8080 8554
ENTRYPOINT ["/entrypoint.sh"]
